package ichiran

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/gookit/color"
	"github.com/k0kubun/pp"
	"github.com/robpike/nihongo"
	"github.com/tidwall/pretty"

	"github.com/docker/docker/api/types/container"
)

// IMPORTANT: jsonformatter.org is very helpful to help understand ichiran's JSON:
// 	as it both prettifies and converts unicode codepoints to literals

// AnalyzeOptions configures optional parameters for analysis.
type AnalyzeOptions struct {
	// Limit controls how many interpretation candidates ichiran returns.
	// With Limit=1 (default), only the top-scoring segmentation is returned.
	// With Limit>1, multiple alternative segmentations are returned, each
	// representing a different way to parse the input text.
	Limit int
}

// escapeLispString escapes a string for embedding inside a Common Lisp
// double-quoted string literal. Only backslash and double-quote need
// escaping in CL strings. Everything else — semicolons, newlines, tabs,
// apostrophes, leading hyphens — survives literally.
func escapeLispString(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 16)
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Analyze performs a single call to get morphological analysis, kanji-kana mappings,
// romanization, and all other relevant information using the optimized Lisp snippet.
// This is the most efficient way to analyze text as it gets all data in a single call.
func (im *IchiranManager) Analyze(ctx context.Context, text string) (*JSONTokens, error) {
	result, err := im.AnalyzeWithOptions(ctx, text, AnalyzeOptions{Limit: 1})
	if err != nil {
		return nil, err
	}
	return result.Tokens, nil
}

// AnalyzeWithOptions is like Analyze but accepts configurable options.
// Use AnalyzeOptions.Limit > 1 to get alternative sentence segmentations
// from ichiran, useful for disambiguation of ambiguous readings.
func (im *IchiranManager) AnalyzeWithOptions(ctx context.Context, text string, opts AnalyzeOptions) (*AnalysisResult, error) {
	queryCtx, cancel := context.WithTimeout(ctx, im.QueryTimeout)
	defer cancel()

	limit := opts.Limit
	if limit < 1 {
		limit = 1
	}

	// Get Docker client
	client, err := im.docker.GetClient()
	if err != nil {
		return nil, fmt.Errorf("failed to get Docker client: %w", err)
	}

	// Check container status
	containerInfo, err := client.ContainerInspect(queryCtx, im.containerName)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect container: %w", err)
	}

	if !containerInfo.State.Running {
		return nil, fmt.Errorf("container %s is not running", im.containerName)
	}

	// Build the Lisp expression with proper CL string escaping.
	// Escape only backslash and double-quote for CL string literals;
	// semicolons, newlines, tabs and other characters survive literally.
	escapedText := escapeLispString(text)

	lispExpr := fmt.Sprintf(`(progn (ql:quickload :jsown :silent t) (defmethod jsown:to-json ((word-info ichiran/dict::word-info)) (let* ((gloss-json (handler-case (ichiran::word-info-gloss-json word-info) (error (e) (declare (ignore e)) nil))) (match-json (handler-case (ichiran/kanji:match-readings-json (slot-value word-info (quote ichiran/dict::text)) (slot-value word-info (quote ichiran/dict::kana))) (error (e) (declare (ignore e)) nil))) (word-json (ichiran::word-info-json word-info))) (when gloss-json (jsown:extend-js word-json ("gloss" gloss-json))) (when match-json (jsown:extend-js word-json ("match" match-json))) (jsown:to-json word-json))) (jsown:to-json (ichiran::romanize* "%s" :limit %d)))`,
		escapedText, limit)

	// Pass ichiran-cli and its arguments directly, without bash -c.
	// This avoids shell interpretation of the Lisp code and the subtitle
	// text embedded in it.
	cmd := []string{
		"ichiran-cli",
		"-e",
		withPooledConnections(lispExpr),
	}

	// Create execution config
	execConfig := container.ExecOptions{
		User:         containerInfo.Config.User,
		Cmd:          cmd,
		AttachStdout: true,
		AttachStderr: true,
		Tty:          false,
		Privileged:   false,
	}

	// Create execution
	exec, err := client.ContainerExecCreate(queryCtx, im.containerName, execConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create exec: %w", err)
	}

	// Attach to execution
	resp, err := client.ContainerExecAttach(queryCtx, exec.ID, container.ExecStartOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to attach to exec: %w", err)
	}
	defer resp.Close()

	// Extract JSON from the output
	output, err := extractJSONFromDockerOutput(queryCtx, resp.Reader)
	if err != nil {
		if errors.Is(err, errNoJSONFound) {
			if inspect, inspectErr := client.ContainerExecInspect(queryCtx, exec.ID); inspectErr == nil {
				return nil, fmt.Errorf("failed to read exec output (exit code %d): %w", inspect.ExitCode, err)
			}
		}
		return nil, fmt.Errorf("failed to read exec output: %w", err)
	}

	// Check execution status
	inspect, err := client.ContainerExecInspect(queryCtx, exec.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect exec: %w", err)
	}

	if inspect.ExitCode != 0 {
		return nil, fmt.Errorf("command failed with exit code %d: %s",
			inspect.ExitCode, string(output))
	}

	// Parse the JSON output into tokens (with alternatives if limit > 1)
	result, err := parseAnalysisFull(output)
	if err != nil {
		return nil, fmt.Errorf("failed to parse output: %w", err)
	}

	return result, nil
}

// AnalyzeWithContext is the context-aware version for analyzing text
func AnalyzeWithContext(ctx context.Context, text string) (*JSONTokens, error) {
	mgr, err := getOrCreateDefaultManager(ctx)
	if err != nil {
		return nil, err
	}
	return mgr.Analyze(ctx, text)
}

// AnalyzeWithOptionsContext is the context-aware version with configurable options.
func AnalyzeWithOptionsContext(ctx context.Context, text string, opts AnalyzeOptions) (*AnalysisResult, error) {
	mgr, err := getOrCreateDefaultManager(ctx)
	if err != nil {
		return nil, err
	}
	return mgr.AnalyzeWithOptions(ctx, text, opts)
}

// Analyze is the backward compatible version that creates a new background context
func Analyze(text string) (*JSONTokens, error) {
	return AnalyzeWithContext(context.Background(), text)
}

// safe escapes special characters in the input text for shell command usage.
// NOTE: Retained for existing callers outside ichiran.go (selective.go etc.).
// The analysis path no longer uses this — see escapeLispString instead.
func safe(s string) string {
	// leading "-" causes the string to be identified by the CLI as a serie of short flags
	return strings.TrimPrefix(s, "-")
}

func stringCapLen(s string, max int) string {
	trimmed := false
	for len(s) > max {
		s = s[:len(s)-1]
		trimmed = true
	}
	if trimmed {
		s += "…"
	}
	return s
}

// parseGlossEntry extracts a Gloss from a raw JSON map.
func parseGlossEntry(glossMap map[string]interface{}) Gloss {
	gloss := Gloss{}
	if pos, ok := glossMap["pos"].(string); ok {
		gloss.Pos = pos
	}
	if glossText, ok := glossMap["gloss"].(string); ok {
		gloss.Gloss = glossText
	}
	if info, ok := glossMap["info"].(string); ok {
		gloss.Info = info
	}
	return gloss
}

// parseConjugation extracts a Conj from a raw JSON map, including recursive
// via chains and the fml (formal) flag.
func parseConjugation(conjMap map[string]interface{}) Conj {
	conj := Conj{}
	if reading, ok := conjMap["reading"].(string); ok {
		conj.Reading = reading
	}
	if readOk, ok := conjMap["readok"].(bool); ok {
		conj.ReadOk = readOk
	}
	if fml, ok := conjMap["fml"].(bool); ok {
		conj.Fml = fml
	}
	if propData, ok := conjMap["prop"].([]interface{}); ok {
		for _, p := range propData {
			if propMap, ok := p.(map[string]interface{}); ok {
				prop := Prop{}
				if pos, ok := propMap["pos"].(string); ok {
					prop.Pos = pos
				}
				if propType, ok := propMap["type"].(string); ok {
					prop.Type = propType
				}
				if neg, ok := propMap["neg"].(bool); ok {
					prop.Neg = neg
				}
				if fml, ok := propMap["fml"].(bool); ok {
					prop.Fml = fml
				}
				conj.Prop = append(conj.Prop, prop)
			}
		}
	}
	if glossEntries, ok := conjMap["gloss"].([]interface{}); ok {
		for _, g := range glossEntries {
			if glossMap, ok := g.(map[string]interface{}); ok {
				conj.Gloss = append(conj.Gloss, parseGlossEntry(glossMap))
			}
		}
	}
	// Recurse through via entries, preserving all siblings
	if viaEntries, ok := conjMap["via"].([]interface{}); ok {
		for _, v := range viaEntries {
			if viaMap, ok := v.(map[string]interface{}); ok {
				conj.Via = append(conj.Via, parseConjugation(viaMap))
			}
		}
	}
	return conj
}

func containsJapaneseScript(s string) bool {
	for _, r := range s {
		if unicode.In(r, unicode.Hiragana, unicode.Katakana, unicode.Han) {
			return true
		}
	}
	return false
}

func containsLatinLetter(s string) bool {
	for _, r := range s {
		if unicode.In(r, unicode.Latin) && unicode.IsLetter(r) {
			return true
		}
	}
	return false
}

// repairRomajiFromKana backfills token.Romaji from token.Kana when ichiran did
// not provide a usable Latin romanization for this token.
func repairRomajiFromKana(token *JSONToken) {
	if token == nil || token.Kana == "" {
		return
	}
	romaji := strings.TrimSpace(token.Romaji)
	if romaji != "" && !containsJapaneseScript(romaji) && containsLatinLetter(romaji) {
		return
	}
	// Strip ZWNJ only for romaji derivation — do not modify the stored Kana.
	kana := strings.ReplaceAll(token.Kana, "\u200c", "")
	token.Romaji = strings.TrimSpace(nihongo.RomajiString(kana))
}

// parseKanjiReadings extracts kanji-kana mapping from match data.
// Strings are used as-is from json.Unmarshal — no second decode pass.
func parseKanjiReadings(matchData []interface{}) []KanjiReading {
	var readings []KanjiReading
	for _, m := range matchData {
		matchMap, ok := m.(map[string]interface{})
		if !ok {
			continue
		}
		reading := KanjiReading{}
		if kanji, ok := matchMap["kanji"].(string); ok {
			reading.Kanji = kanji
		}
		if kana, ok := matchMap["reading"].(string); ok {
			reading.Reading = kana
		}
		if text, ok := matchMap["text"].(string); ok {
			reading.Text = text
		}
		if readingType, ok := matchMap["type"].(string); ok {
			reading.Type = readingType
		}
		if link, ok := matchMap["link"].(bool); ok {
			reading.Link = link
		}
		if gem, ok := matchMap["geminated"].(string); ok {
			reading.Geminated = gem
		}
		if stats, ok := matchMap["stats"].(bool); ok {
			reading.Stats = stats
		}
		if sample, ok := matchMap["sample"].(float64); ok {
			reading.Sample = int(sample)
		}
		if total, ok := matchMap["total"].(float64); ok {
			reading.Total = int(total)
		}
		if perc, ok := matchMap["perc"].(string); ok {
			reading.Perc = perc
		}
		if grade, ok := matchMap["grade"].(float64); ok {
			reading.Grade = int(grade)
		}
		readings = append(readings, reading)
	}
	return readings
}

// parseCounterData extracts counter data from the two wire formats:
// structural [value, ordinal] array and rich {value, ordinal} object.
func parseCounterData(v interface{}) *CounterData {
	switch cv := v.(type) {
	case []interface{}:
		cd := &CounterData{}
		if len(cv) >= 1 {
			if val, ok := cv[0].(string); ok {
				cd.Value = val
			}
		}
		if len(cv) >= 2 {
			if ord, ok := cv[1].(bool); ok {
				cd.Ordinal = ord
			}
		}
		return cd
	case map[string]interface{}:
		cd := &CounterData{}
		if val, ok := cv["value"].(string); ok {
			cd.Value = val
		}
		if ord, ok := cv["ordinal"].(bool); ok {
			cd.Ordinal = ord
		}
		return cd
	}
	return nil
}

// parseWordNode is the single recursive parser for normal words, compound
// children, and alternative candidates. It extracts all fields from wordData,
// optionally enriching with externalGloss when the caller has paired this
// node with a rich gloss source.
//
// Parameters:
//   - wordData: the structural word-data map (from a word entry, structural
//     component, or a rich alternative used as the sole source).
//   - parentRomaji: romaji from the parent word-entry tuple (empty for children).
//   - externalGloss: optional rich gloss (from parent's gloss.components or
//     gloss.alternative) that enriches this node's reading/gloss/conj/match.
//     Correspondence is validated here; mismatch is an error.
//   - inheritedType: parent's lexical type, used when wordData has no own type.
func parseWordNode(wordData map[string]interface{}, parentRomaji string, externalGloss map[string]interface{}, inheritedType string) (*JSONToken, error) {
	token := &JSONToken{}

	// --- 1. Type determination ---
	tokenType, typeOk := wordData["type"].(string)
	if !typeOk {
		tokenType = inheritedType
	}
	token.LexicalType = tokenType
	switch tokenType {
	case "KANA", "KANJI":
		token.IsLexical = true
	case "":
		token.IsLexical = true // assume lexical when type absent
	default:
		token.IsLexical = false
	}

	// --- 2. Basic fields ---
	if text, ok := wordData["text"].(string); ok {
		token.Surface = text
	}
	if truetext, ok := wordData["truetext"].(string); ok {
		token.TrueText = truetext
	}

	// kana: string for normal, array for alternative=true parent (NOT zipped!)
	switch kv := wordData["kana"].(type) {
	case string:
		token.Kana = kv
	case []interface{}:
		if len(kv) > 0 {
			if s, ok := kv[0].(string); ok {
				token.Kana = s
			}
		}
	}

	if score, ok := wordData["score"].(float64); ok {
		token.Score = int(score)
	}

	// seq: number or array (NOT zipped with kana)
	switch sv := wordData["seq"].(type) {
	case float64:
		token.Seq = int(sv)
	case []interface{}:
		if len(sv) > 0 {
			if s, ok := sv[0].(float64); ok {
				token.Seq = int(s)
			}
		}
	}

	token.Romaji = parentRomaji

	// --- 3. Nullable positions ---
	if v, ok := wordData["start"].(float64); ok {
		token.Start = intPtr(int(v))
	}
	if v, ok := wordData["end"].(float64); ok {
		token.End = intPtr(int(v))
	}

	// --- 4. Primary flag ---
	if v, ok := wordData["primary"].(bool); ok {
		token.IsPrimary = boolPtr(v)
	}

	// --- 5. Counter: structural [value, ordinal] or object {value, ordinal} ---
	if cv, exists := wordData["counter"]; exists {
		token.CounterData = parseCounterData(cv)
	}

	// --- 6. Conjugation selector: "ROOT" string or array of IDs ---
	switch cv := wordData["conjugations"].(type) {
	case string:
		if cv == "ROOT" {
			token.ConjSelector = &ConjSelector{IsRoot: true}
		}
	case []interface{}:
		var ids []int
		for _, c := range cv {
			if cID, ok := c.(float64); ok {
				ids = append(ids, int(cID))
			}
		}
		if len(ids) > 0 {
			token.ConjSelector = &ConjSelector{IDs: ids}
		}
	}

	// --- 7. Determine gloss source ---
	// externalGloss (from parent's gloss.components/alternative) is authoritative
	// when provided and correspondence holds. Otherwise use wordData's own gloss.
	isAlternative, _ := wordData["alternative"].(bool)
	ownGloss, _ := wordData["gloss"].(map[string]interface{})

	var glossSource map[string]interface{}
	if externalGloss != nil {
		if !correspondenceValid(wordData, externalGloss) {
			return nil, fmt.Errorf("structural/rich gloss correspondence mismatch for '%s'", token.Surface)
		}
		glossSource = externalGloss
	} else {
		glossSource = ownGloss
	}

	// --- 8. Reading, glosses, conjugations from gloss source ---
	if glossSource != nil && !isAlternative {
		if reading, ok := glossSource["reading"].(string); ok {
			token.Reading = reading
		}
		if glossEntries, ok := glossSource["gloss"].([]interface{}); ok {
			for _, g := range glossEntries {
				if gm, ok := g.(map[string]interface{}); ok {
					token.Gloss = append(token.Gloss, parseGlossEntry(gm))
				}
			}
		}

		// Conjugation: prefer gloss source's conj (authoritative). Only fall
		// back to top-level conj when the gloss has no "conj" key at all.
		if conjArray, conjKeyExists := glossSource["conj"]; conjKeyExists {
			if conjEntries, ok := conjArray.([]interface{}); ok {
				for _, c := range conjEntries {
					if cm, ok := c.(map[string]interface{}); ok {
						token.Conj = append(token.Conj, parseConjugation(cm))
					}
				}
			}
		} else {
			// Legacy fallback: try top-level
			if conjData, ok := wordData["conj"].([]interface{}); ok {
				for _, c := range conjData {
					if cm, ok := c.(map[string]interface{}); ok {
						token.Conj = append(token.Conj, parseConjugation(cm))
					}
				}
			}
		}

		// Counter from gloss source (object format) as fallback
		if token.CounterData == nil {
			if cv, exists := glossSource["counter"]; exists {
				token.CounterData = parseCounterData(cv)
			}
		}
	} else if !isAlternative {
		// Flat schema: reading, gloss, and conj live at wordData's top level
		// rather than inside a gloss map. This covers rich alternative entries
		// where "gloss" is a direct array and "reading" is a top-level string,
		// as well as the legacy top-level conj fallback.
		if reading, ok := wordData["reading"].(string); ok {
			token.Reading = reading
		}
		if glossEntries, ok := wordData["gloss"].([]interface{}); ok {
			for _, g := range glossEntries {
				if gm, ok := g.(map[string]interface{}); ok {
					token.Gloss = append(token.Gloss, parseGlossEntry(gm))
				}
			}
		}
		if conjData, ok := wordData["conj"].([]interface{}); ok {
			for _, c := range conjData {
				if cm, ok := c.(map[string]interface{}); ok {
					token.Conj = append(token.Conj, parseConjugation(cm))
				}
			}
		}
	}

	// --- 9. Match data (kanji readings) ---
	// Prefer gloss source, fall back to wordData directly.
	if glossSource != nil {
		if matchData, ok := glossSource["match"].([]interface{}); ok {
			token.KanjiReadings = parseKanjiReadings(matchData)
		}
	}
	if len(token.KanjiReadings) == 0 {
		if matchData, ok := wordData["match"].([]interface{}); ok {
			token.KanjiReadings = parseKanjiReadings(matchData)
		}
	}

	// --- 10. Alternatives or compounds ---
	if isAlternative {
		alts, altErr := parseAlternativeChildren(wordData, glossSource, tokenType)
		if altErr != nil {
			return nil, fmt.Errorf("failed to parse alternatives: %w", altErr)
		}
		token.Alternative = alts

		// Romaji distribution (only when count matches)
		if token.Romaji != "" && len(token.Alternative) > 0 {
			parts := strings.Split(token.Romaji, "/")
			if len(parts) == len(token.Alternative) {
				for i := range token.Alternative {
					candidate := strings.TrimSpace(parts[i])
					if candidate != "" {
						token.Alternative[i].Romaji = candidate
					}
				}
			}
		}
		for i := range token.Alternative {
			repairRomajiFromKana(&token.Alternative[i])
		}

		// Initialize from candidate 0 — deep-copies all fields
		if len(token.Alternative) > 0 {
			first := token.Alternative[0]
			token.selectCandidate(&first)
		}
		// Surface stays as parent's raw text (selectCandidate doesn't copy it)
	} else {
		if componentsData, ok := wordData["components"].([]interface{}); ok {
			var richComps []map[string]interface{}
			if glossSource != nil {
				richComps = getGlossComponents(glossSource)
			}
			for ci, comp := range componentsData {
				compMap, ok := comp.(map[string]interface{})
				if !ok {
					return nil, fmt.Errorf("compound child %d is not a map: %T", ci, comp)
				}
				var rich map[string]interface{}
				if ci < len(richComps) {
					rich = richComps[ci]
				}
				// Recurse through the single parser
				child, err := parseWordNode(compMap, "", rich, tokenType)
				if err != nil {
					return nil, fmt.Errorf("compound child %d: %w", ci, err)
				}
				token.Components = append(token.Components, *child)
			}
		}
	}

	repairRomajiFromKana(token)
	return token, nil
}

// parseAlternativeChildren extracts alternative reading candidates by pairing
// structural children (wordData.components when alternative=true) with their
// rich gloss descriptions (glossSource.alternative). Each pair is parsed
// through parseWordNode. When structural children are unavailable, the rich
// alternative maps are parsed directly as the sole source.
func parseAlternativeChildren(wordData map[string]interface{}, glossSource map[string]interface{}, parentType string) ([]JSONToken, error) {
	// Collect structural children (the competing interpretations)
	var structComps []map[string]interface{}
	if compsRaw, ok := wordData["components"].([]interface{}); ok {
		for _, c := range compsRaw {
			if cMap, ok := c.(map[string]interface{}); ok {
				structComps = append(structComps, cMap)
			}
		}
	}

	// Collect rich alternative descriptions
	var richAlts []map[string]interface{}
	if glossSource != nil {
		if altData, ok := glossSource["alternative"].([]interface{}); ok {
			for _, alt := range altData {
				if altMap, ok := alt.(map[string]interface{}); ok {
					richAlts = append(richAlts, altMap)
				}
			}
		}
	}

	if len(structComps) > 0 {
		// Prefer structural children enriched with rich gloss data
		if len(richAlts) > 0 && len(structComps) != len(richAlts) {
			return nil, fmt.Errorf("alternative structural/rich child count mismatch: %d structural vs %d rich", len(structComps), len(richAlts))
		}

		var alternatives []JSONToken
		for i, sc := range structComps {
			var rich map[string]interface{}
			if i < len(richAlts) {
				rich = richAlts[i]
			}
			// Delegate to the single recursive parser
			child, err := parseWordNode(sc, "", rich, parentType)
			if err != nil {
				return nil, fmt.Errorf("alternative child %d: %w", i, err)
			}
			alternatives = append(alternatives, *child)
		}
		return alternatives, nil
	}

	// Fallback: structural children unavailable — parse rich alternatives directly
	if len(richAlts) == 0 {
		return nil, nil
	}

	var alternatives []JSONToken
	for i, richAlt := range richAlts {
		// richAlt IS the wordData; no external enrichment
		child, err := parseWordNode(richAlt, "", nil, parentType)
		if err != nil {
			return nil, fmt.Errorf("rich alternative %d: %w", i, err)
		}
		alternatives = append(alternatives, *child)
	}
	return alternatives, nil
}

// getGlossComponents extracts the rich gloss.components array when present.
func getGlossComponents(glossData map[string]interface{}) []map[string]interface{} {
	if glossData == nil {
		return nil
	}
	comps, ok := glossData["components"].([]interface{})
	if !ok {
		return nil
	}
	var result []map[string]interface{}
	for _, c := range comps {
		if cMap, ok := c.(map[string]interface{}); ok {
			result = append(result, cMap)
		}
	}
	return result
}

// correspondenceValid checks whether a structural child and its rich gloss
// component correspond by validating text, kana, and Seq agreement.
func correspondenceValid(structural, rich map[string]interface{}) bool {
	// If text is present on both, they must match
	if sText, ok := structural["text"].(string); ok {
		if rText, ok := rich["text"].(string); ok {
			if sText != rText {
				return false
			}
		}
	}
	// If kana is present on both as strings, they must match
	if sKana, ok := structural["kana"].(string); ok {
		if rKana, ok := rich["kana"].(string); ok {
			if sKana != rKana {
				return false
			}
		}
	}
	// If seq is present on both, they must match
	if sSeq, ok := structural["seq"].(float64); ok {
		if rSeq, ok := rich["seq"].(float64); ok {
			if int(sSeq) != int(rSeq) {
				return false
			}
		}
	}
	return true
}

// parseWordEntry parses a single word entry from ichiran's JSON output
// into a JSONToken. A word entry has the format ["romaji", {word data}, []].
// Delegates to parseWordNode.
func parseWordEntry(wordSlice []interface{}) (*JSONToken, error) {
	if len(wordSlice) < 2 {
		return nil, fmt.Errorf("word entry too short: %d elements", len(wordSlice))
	}

	romaji, ok := wordSlice[0].(string)
	if !ok {
		return nil, fmt.Errorf("word entry first element is not string: %T", wordSlice[0])
	}

	wordData, ok := wordSlice[1].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid word data type: %T", wordSlice[1])
	}

	return parseWordNode(wordData, romaji, nil, "")
}

// parseWordEntriesToTokens parses a list of raw word entries into JSONTokens.
// Malformed structure errors are always returned alongside any valid tokens.
// Valid whitespace and punctuation strings are preserved as content tokens.
func parseWordEntriesToTokens(wordEntries []interface{}) (JSONTokens, error) {
	var tokens JSONTokens
	var parseErr error
	for _, wordEntry := range wordEntries {
		// Plain strings (punctuation/whitespace) are content, not parse failures.
		if s, ok := wordEntry.(string); ok {
			punctToken := &JSONToken{
				Surface:     s,
				IsLexical:   false,
				Reading:     s,
				Kana:        s,
				Romaji:      s,
				LexicalType: "PUNCT",
			}
			tokens = append(tokens, punctToken)
			continue
		}

		wordSlice, ok := wordEntry.([]interface{})
		if !ok {
			if parseErr == nil {
				parseErr = fmt.Errorf("word entry is neither string nor array: %T", wordEntry)
			}
			continue
		}

		token, err := parseWordEntry(wordSlice)
		if err != nil {
			if parseErr == nil {
				parseErr = err
			}
			continue
		}
		tokens = append(tokens, token)
	}
	return tokens, parseErr
}

// parseAnalysis parses the JSON output from the enhanced Lisp snippet.
// Returns the primary (highest-scoring) interpretation and any parse error.
func parseAnalysis(output []byte) (*JSONTokens, error) {
	var rawData interface{}
	if err := json.Unmarshal(output, &rawData); err != nil {
		return nil, fmt.Errorf("failed to decode JSON output: %w", err)
	}

	Logger.Debug().Msgf("Raw JSON structure type: %T", rawData)

	wordsArray, err := extractWordsArray(rawData)
	if err != nil {
		return nil, fmt.Errorf("failed to extract words: %w", err)
	}

	tokens, parseErr := parseWordEntriesToTokens(wordsArray)
	if parseErr != nil && len(tokens) == 0 {
		return nil, fmt.Errorf("failed to parse word entries: %w", parseErr)
	}

	return &tokens, parseErr
}

// parseAnalysisFull parses all interpretations from ichiran output.
// The primary (first) interpretation becomes Tokens; additional
// interpretations are stored in Alternatives. Parse errors propagate.
func parseAnalysisFull(output []byte) (*AnalysisResult, error) {
	// Always parse the primary interpretation via the existing path
	primaryTokens, err := parseAnalysis(output)
	if err != nil {
		return nil, err
	}

	result := &AnalysisResult{
		Tokens: primaryTokens,
	}

	// Try to extract alternative interpretations
	var rawData interface{}
	if err := json.Unmarshal(output, &rawData); err != nil {
		return result, nil
	}

	allInterps, err := extractAllInterpretations(rawData)
	if err != nil || len(allInterps) <= 1 {
		return result, nil
	}

	// Parse alternative interpretations (skip the first, already parsed)
	for i := 1; i < len(allInterps); i++ {
		altTokens, altErr := parseWordEntriesToTokens(allInterps[i].words)
		if altErr != nil {
			continue
		}
		result.Alternatives = append(result.Alternatives, ScoredInterpretation{
			Tokens: altTokens,
			Score:  allInterps[i].score,
		})
	}

	return result, nil
}

// isInterpretation checks if an item looks like an ichiran interpretation,
// which has the format [words_array, score_number].
func isInterpretation(item interface{}) bool {
	arr, ok := item.([]interface{})
	if !ok || len(arr) != 2 {
		return false
	}
	_, firstIsArray := arr[0].([]interface{})
	_, secondIsNumber := arr[1].(float64)
	return firstIsArray && secondIsNumber
}

// scoredWordEntries holds raw word entries for a single interpretation.
type scoredWordEntries struct {
	words []interface{}
	score int
}

// extractAllInterpretations returns all sentence interpretations from the
// ichiran JSON output. Each interpretation contains word entries and a score.
func extractAllInterpretations(data interface{}) ([]scoredWordEntries, error) {
	outerArray, ok := data.([]interface{})
	if !ok || len(outerArray) == 0 {
		return nil, fmt.Errorf("expected outer array structure")
	}

	var allInterps []scoredWordEntries

	for _, item := range outerArray {
		nestedArray, isArray := item.([]interface{})
		if !isArray {
			continue
		}

		// Check if this nested array contains interpretations
		if len(nestedArray) > 0 && isInterpretation(nestedArray[0]) {
			for _, interpRaw := range nestedArray {
				interp, ok := interpRaw.([]interface{})
				if !ok || len(interp) != 2 {
					continue
				}
				wordsArray, ok := interp[0].([]interface{})
				if !ok {
					continue
				}
				score := 0
				if s, ok := interp[1].(float64); ok {
					score = int(s)
				}
				wordEntries := extractAllWordEntries(wordsArray)
				allInterps = append(allInterps, scoredWordEntries{
					words: wordEntries,
					score: score,
				})
			}
		}
	}

	return allInterps, nil
}

// extractWordsArray traverses the JSON structure to find all words and punctuation
// from the primary (first) interpretation.
func extractWordsArray(data interface{}) ([]interface{}, error) {
	outerArray, ok := data.([]interface{})
	if !ok || len(outerArray) == 0 {
		return nil, fmt.Errorf("expected outer array structure")
	}

	var allEntries []interface{}

	for _, item := range outerArray {
		// Strings (punctuation/whitespace) are content
		if _, isPunct := item.(string); isPunct {
			allEntries = append(allEntries, item)
			continue
		}

		nestedArray, isArray := item.([]interface{})
		if !isArray {
			continue
		}

		// If this is a segment with multiple interpretations, take the first
		if len(nestedArray) > 0 && isInterpretation(nestedArray[0]) {
			firstInterp, ok := nestedArray[0].([]interface{})
			if ok && len(firstInterp) >= 1 {
				if wordsArray, ok := firstInterp[0].([]interface{}); ok {
					wordEntries := extractAllWordEntries(wordsArray)
					if len(wordEntries) > 0 {
						allEntries = append(allEntries, wordEntries...)
						continue
					}
				}
			}
		}

		// Extract all word entries from this nested array
		wordEntries := extractAllWordEntries(nestedArray)
		if len(wordEntries) > 0 {
			allEntries = append(allEntries, wordEntries...)
			continue
		}

		// If we couldn't extract using recursive search, check if this already is a formatted word entry
		if isFormattedWordEntry(nestedArray) {
			allEntries = append(allEntries, nestedArray)
			continue
		}
	}

	if len(allEntries) == 0 {
		return nil, fmt.Errorf("could not find any tokens in the JSON structure")
	}

	Logger.Debug().Msgf("Found %d total entries (words and punctuation)", len(allEntries))
	return allEntries, nil
}

// extractAllWordEntries finds all word entries in a nested array structure recursively
func extractAllWordEntries(arr []interface{}) []interface{} {
	var entries []interface{}

	if isFormattedWordEntry(arr) {
		return []interface{}{arr}
	}

	for _, item := range arr {
		if nestedArr, isArray := item.([]interface{}); isArray {
			wordEntries := extractAllWordEntries(nestedArr)
			if len(wordEntries) > 0 {
				entries = append(entries, wordEntries...)
			}
		}
	}

	return entries
}

// extractWordEntry tries to extract a single word entry from a nested array structure
func extractWordEntry(arr []interface{}) []interface{} {
	if len(arr) == 0 {
		return nil
	}

	current := arr
	for len(current) > 0 {
		if isFormattedWordEntry(current) {
			return current
		}
		nextArr, ok := current[0].([]interface{})
		if !ok {
			break
		}
		current = nextArr
	}

	return nil
}

// isFormattedWordEntry checks if an array matches the expected format for a word entry
// Word entries have the format ["romaji", {word data}, []]
func isFormattedWordEntry(arr []interface{}) bool {
	if len(arr) < 2 {
		return false
	}
	_, isString := arr[0].(string)
	if !isString {
		return false
	}
	_, isMap := arr[1].(map[string]interface{})
	return isMap
}

func placeholder() {
	fmt.Print("")
	pretty.Pretty([]byte{})
	color.Redln(" 𝒻*** 𝓎ℴ𝓊 𝒸ℴ𝓂𝓅𝒾𝓁ℯ𝓇")
	pp.Println("𝓯*** 𝔂𝓸𝓾 𝓬𝓸𝓶𝓹𝓲𝓵𝓮𝓻")
}
