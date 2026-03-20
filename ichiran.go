package ichiran

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"al.essio.dev/pkg/shellescape"
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

	// Load the optimized Lisp snippet and replace the placeholder
	lispCode := fmt.Sprintf(`(progn
    (ql:quickload :jsown :silent t)

    (defmethod jsown:to-json ((word-info ichiran/dict::word-info))
      (let* ((gloss-json (handler-case
                            (ichiran::word-info-gloss-json word-info)
                          (error (e) (declare (ignore e)) nil)))
             (match-json (handler-case
                            (ichiran/kanji:match-readings-json
                              (slot-value word-info (quote ichiran/dict::text))
                              (slot-value word-info (quote ichiran/dict::kana)))
                          (error (e) (declare (ignore e)) nil)))

             (word-json (ichiran::word-info-json word-info)))

        (when gloss-json
          (jsown:extend-js word-json ("gloss" gloss-json)))

        (when match-json
          (jsown:extend-js word-json ("match" match-json)))

        (jsown:to-json word-json)))

    (jsown:to-json (ichiran::romanize* "%s" :limit %d)))`, text, limit)

	// Remove Lisp comments and clean up the code for the shell command
	lispCode = cleanLispCode(lispCode)

	// Prepare command
	execCommand := fmt.Sprintf("ichiran-cli -e '%s'", lispCode)
	cmd := []string{
		"bash",
		"-c",
		execCommand,
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
func safe(s string) string {
	s = shellescape.Quote(s)
	//s = strings.ReplaceAll(s, "\"", "\\\"")
	// leading "-" causes the string to be identified by the CLI as a serie of short flags
	return strings.TrimPrefix(s, "-")
}

// decodeToken processes Unicode escapes and other encodings in token fields.
func decodeToken(token *JSONToken) error {
	var err error
	if token.Surface, err = unescapeUnicodeString(token.Surface); err != nil {
		Logger.Debug().Err(err).Msgf("failed to decode Surface: %s", token.Surface)
		return fmt.Errorf("failed to decode Surface: %w", err)
	}
	if token.Reading, err = unescapeUnicodeString(token.Reading); err != nil {
		Logger.Debug().Err(err).Msgf("failed to decode Reading: %s", token.Reading)
		return fmt.Errorf("failed to decode Reading: %w", err)
	}
	if token.Kana, err = unescapeUnicodeString(token.Kana); err != nil {
		Logger.Debug().Err(err).Msgf("failed to decode Kana: %s", token.Kana)
		return fmt.Errorf("failed to decode Kana: %w", err)
	}

	return nil
}

// unescapeUnicodeString converts Unicode escapes (\uXXXX) to actual characters
func unescapeUnicodeString(s string) (string, error) {
	// Kana field can contain a forbidden jutsu: \u200c = ZERO WIDTH NON-JOINER
	// however it is (apparently) automatically rendered by JSON decoder from its codepoint into a literal in Go
	// so it must replaced manually.
	s = strings.ReplaceAll(s /*ZERO WIDTH NON-JOINER*/, "‌", "")
	// If the string doesn't contain any \u, return as is
	if !strings.Contains(s, "\\u") {
		return s, nil
	}

	// Add quotes and decode as JSON string which handles Unicode escapes
	quoted := `"` + strings.Replace(s, `"`, `\"`, -1) + `"`
	var unquoted string
	if err := json.Unmarshal([]byte(quoted), &unquoted); err != nil {
		return "", fmt.Errorf("failed to unescape Unicode: %w", err)
	}
	return unquoted, nil
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

// parseConjugation extracts a Conj from a raw JSON map.
func parseConjugation(conjMap map[string]interface{}) Conj {
	conj := Conj{}
	if reading, ok := conjMap["reading"].(string); ok {
		conj.Reading = reading
	}
	if readOk, ok := conjMap["readok"].(bool); ok {
		conj.ReadOk = readOk
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
	return conj
}

// parseReadingAlternatives extracts alternative readings from a word that
// has "alternative": true. The alternatives come from gloss.alternative
// which has rich reading/gloss/conjugation info for each possible reading.
func parseReadingAlternatives(wordData map[string]interface{}) []JSONToken {
	glossData, ok := wordData["gloss"].(map[string]interface{})
	if !ok {
		return nil
	}

	altData, ok := glossData["alternative"].([]interface{})
	if !ok {
		return nil
	}

	var alternatives []JSONToken
	for _, alt := range altData {
		altMap, ok := alt.(map[string]interface{})
		if !ok {
			continue
		}

		altToken := JSONToken{IsLexical: true}
		if text, ok := altMap["text"].(string); ok {
			altToken.Surface = text
		}
		if kana, ok := altMap["kana"].(string); ok {
			altToken.Kana = kana
		}
		if reading, ok := altMap["reading"].(string); ok {
			altToken.Reading = reading
		}
		if score, ok := altMap["score"].(float64); ok {
			altToken.Score = int(score)
		}
		if seq, ok := altMap["seq"].(float64); ok {
			altToken.Seq = int(seq)
		}

		// Parse conjugation info
		if conjData, ok := altMap["conj"].([]interface{}); ok {
			for _, c := range conjData {
				if conjMap, ok := c.(map[string]interface{}); ok {
					altToken.Conj = append(altToken.Conj, parseConjugation(conjMap))
				}
			}
		}

		// Parse direct glosses
		if glossEntries, ok := altMap["gloss"].([]interface{}); ok {
			for _, g := range glossEntries {
				if glossMap, ok := g.(map[string]interface{}); ok {
					altToken.Gloss = append(altToken.Gloss, parseGlossEntry(glossMap))
				}
			}
		}

		if err := decodeToken(&altToken); err != nil {
			Logger.Debug().Err(err).Msg("failed to decode alternative token")
			continue
		}
		repairRomajiFromKana(&altToken)
		alternatives = append(alternatives, altToken)
	}

	return alternatives
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
//
// This fallback exists because ichiran's richer analysis can legitimately give
// us the correct reading in Kana while the romaji slot is still missing,
// slash-packed at the parent word level, or even left as raw Japanese text in
// some lower-ranked sentence interpretations. LangKit's disambiguation logic
// selects readings, so after the reading is chosen we need a deterministic way
// to keep the romanized output aligned without asking an LLM to invent it.
func repairRomajiFromKana(token *JSONToken) {
	if token == nil || token.Kana == "" {
		return
	}
	romaji := strings.TrimSpace(token.Romaji)
	if romaji != "" && !containsJapaneseScript(romaji) && containsLatinLetter(romaji) {
		return
	}

	token.Romaji = strings.TrimSpace(nihongo.RomajiString(token.Kana))
}

// parseWordEntry parses a single word entry from ichiran's JSON output
// into a JSONToken. A word entry has the format ["romaji", {word data}, []].
func parseWordEntry(wordSlice []interface{}) (*JSONToken, error) {
	if len(wordSlice) < 2 {
		return nil, fmt.Errorf("word entry too short: %d elements", len(wordSlice))
	}

	wordData, ok := wordSlice[1].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid word data type: %T", wordSlice[1])
	}

	token := &JSONToken{
		IsLexical: true,
	}

	// Extract the type - determines if lexical or not
	tokenType, _ := wordData["type"].(string)
	switch tokenType {
	case "KANA", "KANJI":
		token.IsLexical = true
	default:
		token.IsLexical = false
	}

	// Extract basic fields
	if text, ok := wordData["text"].(string); ok {
		token.Surface = text
	}

	// kana can be a string or an array of strings (when alternative: true)
	switch kanaVal := wordData["kana"].(type) {
	case string:
		token.Kana = kanaVal
	case []interface{}:
		if len(kanaVal) > 0 {
			if firstKana, ok := kanaVal[0].(string); ok {
				token.Kana = firstKana
			}
		}
	}

	if score, ok := wordData["score"].(float64); ok {
		token.Score = int(score)
	}

	// seq can be a number or an array of numbers (when alternative: true)
	switch seqVal := wordData["seq"].(type) {
	case float64:
		token.Seq = int(seqVal)
	case []interface{}:
		if len(seqVal) > 0 {
			if firstSeq, ok := seqVal[0].(float64); ok {
				token.Seq = int(firstSeq)
			}
		}
	}

	// Get romanized form - usually in position 0 of the entry
	if romaji, ok := wordSlice[0].(string); ok {
		token.Romaji = romaji
	}

	// Check if this word has alternative readings
	isAlternative, _ := wordData["alternative"].(bool)

	// Extract the reading from the gloss if available
	if glossData, ok := wordData["gloss"].(map[string]interface{}); ok {
		if !isAlternative {
			// Normal word: reading and glosses are at the top level of gloss
			if reading, ok := glossData["reading"].(string); ok {
				token.Reading = reading
			}
			if glossEntries, ok := glossData["gloss"].([]interface{}); ok {
				for _, g := range glossEntries {
					if glossMap, ok := g.(map[string]interface{}); ok {
						token.Gloss = append(token.Gloss, parseGlossEntry(glossMap))
					}
				}
			}
		}
		// When isAlternative, the gloss object has an "alternative" array
		// instead of direct reading/gloss. Handled below.
	}

	// Extract conjugation information if available
	if conjData, ok := wordData["conj"].([]interface{}); ok {
		for _, c := range conjData {
			if conjMap, ok := c.(map[string]interface{}); ok {
				token.Conj = append(token.Conj, parseConjugation(conjMap))
			}
		}
	}

	// Extract kanji-kana mapping information if available
	if matchData, ok := wordData["match"].([]interface{}); ok {
		var readings []KanjiReading
		for _, m := range matchData {
			if matchMap, ok := m.(map[string]interface{}); ok {
				reading := KanjiReading{}
				if kanji, ok := matchMap["kanji"].(string); ok {
					reading.Kanji = kanji
				}
				if kana, ok := matchMap["reading"].(string); ok {
					reading.Reading = kana
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
		}
		for i := range readings {
			readings[i].Kanji, _ = unescapeUnicodeString(readings[i].Kanji)
			readings[i].Reading, _ = unescapeUnicodeString(readings[i].Reading)
		}
		token.KanjiReadings = readings
	}

	if isAlternative {
		// Word has multiple possible readings — populate Alternative from
		// the rich gloss.alternative data instead of treating components
		// as compound parts.
		token.Alternative = parseReadingAlternatives(wordData)

		// Distribute the combined romaji (e.g. "tometa/yameta") to
		// individual alternatives. ichiran joins per-reading romaji
		// with "/" at the word entry level.
		if token.Romaji != "" && len(token.Alternative) > 0 {
			romajiParts := strings.Split(token.Romaji, "/")
			for i := range token.Alternative {
				if i < len(romajiParts) {
					token.Alternative[i].Romaji = strings.TrimSpace(romajiParts[i])
				}
			}
		}

		// Per-alternative romaji is not always carried cleanly through ichiran's
		// JSON. Normalize each reading here so later disambiguation can swap in a
		// selected alternative without leaving Romaji empty or in Japanese script.
		for i := range token.Alternative {
			repairRomajiFromKana(&token.Alternative[i])
		}
		// Keep the primary token aligned with the first alternative because the
		// rest of LangKit treats the primary fields as the currently selected
		// reading until disambiguation chooses a different one.
		if len(token.Alternative) > 0 {
			token.Romaji = token.Alternative[0].Romaji
		}

		// Use the first alternative's data for the primary token fields
		// when they weren't already set from the main word data
		if len(token.Alternative) > 0 && token.Reading == "" {
			token.Reading = token.Alternative[0].Reading
		}
	} else {
		// Normal compound components (e.g. 一方通行 → 一方 + 通行)
		if componentsData, ok := wordData["components"].([]interface{}); ok {
			for _, comp := range componentsData {
				if compMap, ok := comp.(map[string]interface{}); ok {
					component := JSONToken{}
					if text, ok := compMap["text"].(string); ok {
						component.Surface = text
					}
					if kana, ok := compMap["kana"].(string); ok {
						component.Kana = kana
					}
					if reading, ok := compMap["reading"].(string); ok {
						component.Reading = reading
					}
					if score, ok := compMap["score"].(float64); ok {
						component.Score = int(score)
					}
					if glossData, ok := compMap["gloss"].(map[string]interface{}); ok {
						if glossEntries, ok := glossData["gloss"].([]interface{}); ok {
							for _, g := range glossEntries {
								if glossMap, ok := g.(map[string]interface{}); ok {
									component.Gloss = append(component.Gloss, parseGlossEntry(glossMap))
								}
							}
						}
					}
					token.Components = append(token.Components, component)
				}
			}
		}
	}

	// Some sentence-level alternatives still surface the chosen reading in Kana
	// while leaving the token's Romaji non-Latin or empty. Repairing it here
	// keeps downstream Roman()/RomanParts() deterministic after disambiguation.
	repairRomajiFromKana(token)

	if err := decodeToken(token); err != nil {
		return nil, fmt.Errorf("failed to decode token: %w", err)
	}

	return token, nil
}

// parseWordEntriesToTokens parses a list of raw word entries into JSONTokens.
func parseWordEntriesToTokens(wordEntries []interface{}) (JSONTokens, error) {
	var tokens JSONTokens
	for _, wordEntry := range wordEntries {
		wordSlice, ok := wordEntry.([]interface{})
		if !ok || len(wordSlice) < 2 {
			continue
		}
		token, err := parseWordEntry(wordSlice)
		if err != nil {
			Logger.Debug().Err(err).Msg("skipping unparseable word entry")
			continue
		}
		tokens = append(tokens, token)
	}
	return tokens, nil
}

// parseAnalysis parses the JSON output from the enhanced Lisp snippet.
// Returns only the primary (highest-scoring) interpretation.
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

	tokens, err := parseWordEntriesToTokens(wordsArray)
	if err != nil {
		return nil, fmt.Errorf("failed to parse word entries: %w", err)
	}

	return &tokens, nil
}

// parseAnalysisFull parses all interpretations from ichiran output.
// The primary (first) interpretation becomes Tokens; additional
// interpretations are stored in Alternatives.
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
		altTokens, err := parseWordEntriesToTokens(allInterps[i].words)
		if err != nil {
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
	// First level is typically an array
	outerArray, ok := data.([]interface{})
	if !ok || len(outerArray) == 0 {
		return nil, fmt.Errorf("expected outer array structure")
	}

	// We'll collect all entries (words and punctuation) here
	var allEntries []interface{}

	// Process the top-level array which contains a mix of nested word arrays and punctuation strings
	for _, item := range outerArray {
		// Check if this is a string (punctuation)
		if punctStr, isPunct := item.(string); isPunct && strings.TrimSpace(punctStr) != "" {
			// Create a token for punctuation
			punctToken := []interface{}{
				punctStr, // First element is the punctuation mark itself
				map[string]interface{}{ // Second element is token metadata
					"type":    "PUNCT",
					"text":    punctStr,
					"kana":    punctStr,
					"reading": punctStr,
				},
				[]interface{}{}, // Third element (usually alternative forms) is empty
			}
			allEntries = append(allEntries, punctToken)
			continue
		}

		// If not a punctuation string, it should be a nested array containing word data
		nestedArray, isArray := item.([]interface{})
		if !isArray {
			// Skip anything that's not a string or array
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

	// Base case: Check if current array is a word entry
	if isFormattedWordEntry(arr) {
		return []interface{}{arr}
	}

	// Recursively check each element in the array
	for _, item := range arr {
		// If item is an array, process it
		if nestedArr, isArray := item.([]interface{}); isArray {
			// Try to find word entries at this level
			wordEntries := extractAllWordEntries(nestedArr)
			if len(wordEntries) > 0 {
				entries = append(entries, wordEntries...)
			}
		}
	}

	return entries
}

// extractWordEntry tries to extract a single word entry from a nested array structure
// typically in the format [[[[["romaji", {word data}, []]], score]]]
func extractWordEntry(arr []interface{}) []interface{} {
	// Common pattern of nesting for word entries
	if len(arr) == 0 {
		return nil
	}

	// Navigate through the nested structure
	current := arr
	for len(current) > 0 {
		// Check if current is a valid word entry format
		if isFormattedWordEntry(current) {
			return current
		}

		// Go one level deeper
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

	// First element should be a string (romaji)
	_, isString := arr[0].(string)
	if !isString {
		return false
	}

	// Second element should be a map (word data)
	_, isMap := arr[1].(map[string]interface{})
	if !isMap {
		return false
	}

	return true
}

func placeholder() {
	fmt.Print("")
	pretty.Pretty([]byte{})
	color.Redln(" 𝒻*** 𝓎ℴ𝓊 𝒸ℴ𝓂𝓅𝒾𝓁ℯ𝓇")
	pp.Println("𝓯*** 𝔂𝓸𝓾 𝓬𝓸𝓶𝓹𝓲𝓵𝓮𝓻")
}
