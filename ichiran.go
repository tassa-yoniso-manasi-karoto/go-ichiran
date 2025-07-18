package ichiran

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"al.essio.dev/pkg/shellescape"
	"github.com/gookit/color"
	"github.com/k0kubun/pp"
	"github.com/tidwall/pretty"

	"github.com/docker/docker/api/types"
)

// IMPORTANT: jsonformatter.org is very helpful to help understand ichiran's JSON:
// 	as it both prettifies and converts unicode codepoints to literals

// Analyze performs a single call to get morphological analysis, kanji-kana mappings,
// romanization, and all other relevant information using the optimized Lisp snippet.
// This is the most efficient way to analyze text as it gets all data in a single call.
func (im *IchiranManager) Analyze(ctx context.Context, text string) (*JSONTokens, error) {
	queryCtx, cancel := context.WithTimeout(ctx, im.QueryTimeout)
	defer cancel()

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
    
    (jsown:to-json (ichiran::romanize* "%s" :limit 1)))`, text)

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
	execConfig := types.ExecConfig{
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
	resp, err := client.ContainerExecAttach(queryCtx, exec.ID, types.ExecStartCheck{})
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

	// Parse the JSON output into tokens
	tokens, err := parseAnalysis(output)
	if err != nil {
		return nil, fmt.Errorf("failed to parse output: %w", err)
	}

	return tokens, nil
}

// AnalyzeWithContext is the context-aware version for analyzing text
func AnalyzeWithContext(ctx context.Context, text string) (*JSONTokens, error) {
	mgr, err := getOrCreateDefaultManager(ctx)
	if err != nil {
		return nil, err
	}
	return mgr.Analyze(ctx, text)
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

// parseAnalysis parses the JSON output from the enhanced Lisp snippet
// This function handles the complex nested JSON structure including readings,
// translations, and kanji-kana mappings.
func parseAnalysis(output []byte) (*JSONTokens, error) {
	// First, unmarshal the JSON into a nested structure
	var rawData interface{}
	if err := json.Unmarshal(output, &rawData); err != nil {
		return nil, fmt.Errorf("failed to decode JSON output: %w", err)
	}

	// Debug view of the JSON structure
	Logger.Debug().Msgf("Raw JSON structure type: %T", rawData)

	var tokens JSONTokens

	// Try to detect the structure format and process it
	// The JSON structure is deeply nested with mixed arrays and objects

	// Extract the main words array which is deeply nested
	// We need to navigate through multiple layers of arrays to get to the tokens
	wordsArray, err := extractWordsArray(rawData)
	if err != nil {
		return nil, fmt.Errorf("failed to extract words: %w", err)
	}

	// Now process each word entry
	for _, wordEntry := range wordsArray {
		// A word entry is typically ["romanji", {word data...}, []]
		wordSlice, ok := wordEntry.([]interface{})
		if !ok || len(wordSlice) < 2 {
			// Skip entries that don't match expected format
			continue
		}

		// The word data is in the second position of the slice
		wordData, ok := wordSlice[1].(map[string]interface{})
		if !ok {
			// Skip entries with invalid word data
			continue
		}

		// Create and populate a new token
		token := &JSONToken{
			IsLexical: true, // Assume true until proven otherwise
			Raw:       nil,  // Store raw JSON for future use
		}

		// Extract the type - determines if lexical or not
		tokenType, _ := wordData["type"].(string)
		if tokenType == "KANA" {
			// Kana tokens are lexical
			token.IsLexical = true
		} else if tokenType == "KANJI" {
			// Kanji tokens are lexical
			token.IsLexical = true
		} else if tokenType == "PUNCT" {
			// Punctuation tokens are not lexical
			token.IsLexical = false
		} else {
			// Other token types may not be lexical
			token.IsLexical = false
		}

		// Extract basic fields
		if text, ok := wordData["text"].(string); ok {
			token.Surface = text
		}
		if kana, ok := wordData["kana"].(string); ok {
			token.Kana = kana
		}
		if score, ok := wordData["score"].(float64); ok {
			token.Score = int(score)
		}
		if seq, ok := wordData["seq"].(float64); ok {
			token.Seq = int(seq)
		}

		// Get romanized form - usually in position 0 of the entry
		if romanji, ok := wordSlice[0].(string); ok {
			token.Romaji = romanji
		}

		// Extract the reading from the gloss if available
		if glossData, ok := wordData["gloss"].(map[string]interface{}); ok {
			// The reading is sometimes in the gloss object
			if reading, ok := glossData["reading"].(string); ok {
				token.Reading = reading
			}

			// Extract gloss entries
			if glossEntries, ok := glossData["gloss"].([]interface{}); ok {
				for _, g := range glossEntries {
					if glossMap, ok := g.(map[string]interface{}); ok {
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

						token.Gloss = append(token.Gloss, gloss)
					}
				}
			}
		}

		// Extract conjugation information if available
		if conjData, ok := wordData["conj"].([]interface{}); ok {
			for _, c := range conjData {
				if conjMap, ok := c.(map[string]interface{}); ok {
					conj := Conj{}

					if reading, ok := conjMap["reading"].(string); ok {
						conj.Reading = reading
					}
					if readOk, ok := conjMap["readok"].(bool); ok {
						conj.ReadOk = readOk
					}

					// Extract properties
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

					// Extract gloss entries for this conjugation
					if glossEntries, ok := conjMap["gloss"].([]interface{}); ok {
						for _, g := range glossEntries {
							if glossMap, ok := g.(map[string]interface{}); ok {
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

								conj.Gloss = append(conj.Gloss, gloss)
							}
						}
					}

					token.Conj = append(token.Conj, conj)
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
				} else if text, ok := matchMap["text"].(string); ok {
					// This is likely a full text segment, not a kanji reading
					// We can create a special entry if needed
					_ = text // Currently not used
				}
			}

			// Decode Unicode escapes in the readings
			for i := range readings {
				readings[i].Kanji, _ = unescapeUnicodeString(readings[i].Kanji)
				readings[i].Reading, _ = unescapeUnicodeString(readings[i].Reading)
			}
			token.KanjiReadings = readings
		}

		// Extract components data if available (for compound words)
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

					// Extract gloss for the component
					if glossData, ok := compMap["gloss"].(map[string]interface{}); ok {
						if glossEntries, ok := glossData["gloss"].([]interface{}); ok {
							for _, g := range glossEntries {
								if glossMap, ok := g.(map[string]interface{}); ok {
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

									component.Gloss = append(component.Gloss, gloss)
								}
							}
						}
					}

					token.Components = append(token.Components, component)
				}
			}
		}

		// Decode Unicode escapes in strings
		if err := decodeToken(token); err != nil {
			return nil, fmt.Errorf("failed to decode token: %w", err)
		}

		tokens = append(tokens, token)
	}

	return &tokens, nil
}

// extractWordsArray traverses the JSON structure to find all words and punctuation
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