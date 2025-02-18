
package ichiran

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"slices"
	"unicode"

	"github.com/gookit/color"
	"github.com/k0kubun/pp"
	"github.com/tidwall/pretty"

	"github.com/docker/docker/api/types"
)

// KanjiReading represents the reading information for a single kanji character
type KanjiReading struct {
	Kanji     string `json:"kanji"`     // The kanji character
	Reading   string `json:"reading"`    // The reading in hiragana
	Type      string `json:"type"`       // Reading type (ja_on, ja_kun)
	Link      bool   `json:"link"`       // Whether the reading links to adjacent characters
	Geminated string `json:"geminated"`  // Geminated sound (っ) if present
	Stats     bool   `json:"stats"`      // Whether statistics are available
	Sample    int    `json:"sample"`     // Sample size for statistics
	Total     int    `json:"total"`      // Total occurrences
	Perc      string `json:"perc"`       // Percentage of usage
	Grade     int    `json:"grade"`      // School grade level
}

// TextSegment represents non-kanji text segments in the analysis
type TextSegment struct {
	Text string `json:"text"` // The text content
}

// kanjiReadingResult is a union type that can hold either a KanjiReading or a TextSegment
type kanjiReadingResult struct {
	*KanjiReading
	*TextSegment
}

// getKanjiReadings performs analysis to get readings for individual kanji characters
func getKanjiReadings(text string) ([]kanjiReadingResult, error) {
	// First get the kana reading using existing Analyze function
	tokens, err := Analyze(text)
	if err != nil {
		return nil, fmt.Errorf("failed to analyze text: %w", err)
	}

	// Get the kana reading
	kanaReading := tokens.Kana()
	
	// Kana may have space in them with causes the match reading command below to fail:
	// 	Surface:     "だから",
	// 	IsLexical:   true,
	//	Reading:     "だから",
	//	Kana:        "だ から",
	// 	Romaji:      "da kara",
	kanaReading = strings.ReplaceAll(kanaReading, " ", "")

	ctx, cancel := context.WithTimeout(Ctx, QueryTimeout)
	defer cancel()

	mu.Lock()
	docker := instance
	mu.Unlock()

	if docker == nil {
		return nil, fmt.Errorf("Docker manager not initialized. Call Init() first")
	}

	// Get Docker client
	client, err := docker.docker.GetClient()
	if err != nil {
		return nil, fmt.Errorf("failed to get Docker client: %w", err)
	}

	// Prepare command for kanji readings
	cmd := []string{
		"bash",
		"-c",
		fmt.Sprintf("ichiran-cli -e '(jsown:to-json (ichiran/kanji:match-readings-json \"%s\" \"%s\"))'",
			safe(text), safe(kanaReading)),
	}

	// Create execution config
	execConfig := types.ExecConfig{
		Cmd:          cmd,
		AttachStdout: true,
		AttachStderr: true,
	}

	// Create and start execution
	exec, err := client.ContainerExecCreate(ctx, ContainerName, execConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create exec: %w", err)
	}

	resp, err := client.ContainerExecAttach(ctx, exec.ID, types.ExecStartCheck{})
	if err != nil {
		return nil, fmt.Errorf("failed to attach to exec: %w", err)
	}
	defer resp.Close()

	// Read output
	output, err := readDockerOutput(resp.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to read exec output: %w", err)
	}
	
	
	// First unescape the JSON string
	var jsonStr string
	if err := json.Unmarshal(output, &jsonStr); err != nil {
		return nil, fmt.Errorf("failed to unmarshal outer JSON string: %w", err)
	}

	// Now parse the actual results
	var results []kanjiReadingResult
	if err := json.Unmarshal([]byte(jsonStr), &results); err != nil {
		return nil, fmt.Errorf("failed to unmarshal kanji readings: %w\nJSON: %s", err, jsonStr)
	}
	//color.Redf("len(output)=%d\tjsonStr=\"%s\"\n", len(output), jsonStr)

	// Process the results to handle Unicode escapes
	for i := range results {
		if results[i].KanjiReading != nil {
			results[i].KanjiReading.Kanji, err = unescapeUnicodeString(results[i].KanjiReading.Kanji)
			if err != nil {
				return nil, fmt.Errorf("failed to unescape kanji: %w", err)
			}
			results[i].KanjiReading.Reading, err = unescapeUnicodeString(results[i].KanjiReading.Reading)
			if err != nil {
				return nil, fmt.Errorf("failed to unescape reading: %w", err)
			}
		}
		if results[i].TextSegment != nil {
			results[i].TextSegment.Text, err = unescapeUnicodeString(results[i].TextSegment.Text)
			if err != nil {
				return nil, fmt.Errorf("failed to unescape text segment: %w", err)
			}
		}
	}

	return results, nil
}






type ProcessingStatus int

const (
	StatusPreserved ProcessingStatus = iota      // Kanji was preserved (regular reading & under frequency threshold)
	StatusIrregular                              // Kanji was transliterated due to irregular reading
	StatusInfrequent                             // Kanji was transliterated due to being over frequency threshold
	StatusUnmappable                             // Kanji was transliterated due to inability to map reading
	StatusNotKanji                               // Token was not a kanji character
)

// ProcessedToken represents a single token's processing result
type ProcessedToken struct {
	Original string
	Result   string
	Status   ProcessingStatus
}

// TransliterationResult contains the complete transliteration output
type TransliterationResult struct {
	Text    string           // The final transliterated text
	Tokens  []ProcessedToken // Detailed processing information
}

// isRegularReading checks if the kanji has a regular reading pattern
func isRegularReading(reading *KanjiReading) bool {
	// A reading is considered regular if:
	// 1. It has a direct link between kanji and reading (link=true)
	// 2. It doesn't have special modifications (geminated is empty)
	return reading.Link && reading.Geminated == ""
}

// SelectiveTranslit performs selective transliteration of the tokens based on kanji frequency.
// It preserves kanji that are both:
//   - Below the specified frequency threshold (lower number = more frequent)
//   - Have regular readings (no special phonetic modifications)
// Other kanji are converted to their hiragana readings.
//
// Parameter freqThreshold: Maximum frequency rank to preserve (1-3000, lower = more frequent)
func (tokens JSONTokens) SelectiveTranslit(freqThreshold int) (string, error) {
	tlitStruct, err := tokens.selectiveTranslit(freqThreshold)
	return tlitStruct.Text, err
}

func (tokens JSONTokens) SelectiveTranslitFullMapping(freqThreshold int) (*TransliterationResult, error) {
	return tokens.selectiveTranslit(freqThreshold)
}

func (tokens JSONTokens) selectiveTranslit(freqThreshold int) (*TransliterationResult, error) {
	// Reconstruct the original text from the tokens
	var originalText strings.Builder
	for _, t := range tokens {
		originalText.WriteString(t.Surface)
	}
	text := originalText.String()

	// Split text into processable and non-processable chunks
	chunks := splitIntoChunks(text)

	var allProcessedTokens []ProcessedToken
	var finalResult strings.Builder

	// Process each chunk
	for _, chunk := range chunks {
		if !chunk.processable || !ContainsKanjis(chunk.text) {
				// Preserve non-processable chunks as-is
				token := ProcessedToken{
					Original: chunk.text,
					Result:   chunk.text,
					Status:   StatusNotKanji,
				}
				finalResult.WriteString(chunk.text)
				allProcessedTokens = append(allProcessedTokens, token)
				continue
		}

		// Process Japanese text chunks
		readings, err := getKanjiReadings(chunk.text)
		if err != nil {
			// If processing fails, preserve the chunk as-is
			token := ProcessedToken{
				Original: chunk.text,
				Result:   chunk.text,
				Status:   StatusNotKanji,
			}
			finalResult.WriteString(chunk.text)
			allProcessedTokens = append(allProcessedTokens, token)
			continue
		}

		// Process the readings
		for _, r := range readings {
			if r.KanjiReading != nil {
				kanji := r.KanjiReading.Kanji
				freq := slices.Index(kanjiFreqSlice, kanji)
				exists := freq != -1

				var token ProcessedToken
				token.Original = kanji

				shouldPreserve := exists &&
					freq <= freqThreshold &&
					isRegularReading(r.KanjiReading)

				if shouldPreserve {
					token.Result = kanji
					token.Status = StatusPreserved
				} else {
					token.Result = r.KanjiReading.Reading
					if !exists || freq > freqThreshold {
						token.Status = StatusInfrequent
					} else if !isRegularReading(r.KanjiReading) {
						token.Status = StatusIrregular
					} else {
						token.Status = StatusUnmappable
					}
				}

				finalResult.WriteString(token.Result)
				allProcessedTokens = append(allProcessedTokens, token)

			} else if r.TextSegment != nil {
				token := ProcessedToken{
					Original: r.TextSegment.Text,
					Result:   r.TextSegment.Text,
					Status:   StatusNotKanji,
				}
				finalResult.WriteString(token.Result)
				allProcessedTokens = append(allProcessedTokens, token)
			}
		}
	}

	return &TransliterationResult{
		Text:   finalResult.String(),
		Tokens: allProcessedTokens,
	}, nil
}

// splitIntoChunks splits text into alternating chunks of processable (Japanese) and
// non-processable (punctuation, etc.) text, preserving their order
func splitIntoChunks(text string) []struct {
	text        string
	processable bool
} {
	var chunks []struct {
		text        string
		processable bool
	}
	var currentJapanese strings.Builder
	var currentOther strings.Builder

	flushJapanese := func() {
		if currentJapanese.Len() > 0 {
			chunks = append(chunks, struct {
				text        string
				processable bool
			}{
				text:        currentJapanese.String(),
				processable: true,
			})
			currentJapanese.Reset()
		}
	}

	flushOther := func() {
		if currentOther.Len() > 0 {
			chunks = append(chunks, struct {
				text        string
				processable bool
			}{
				text:        currentOther.String(),
				processable: false,
			})
			currentOther.Reset()
		}
	}

	for _, r := range text {
		if unicode.Is(unicode.Han, r) || // Kanji
			unicode.Is(unicode.Hiragana, r) ||
			unicode.Is(unicode.Katakana, r) {
			flushOther()
			currentJapanese.WriteRune(r)
		} else {
			flushJapanese()
			currentOther.WriteRune(r)
		}
	}

	// Flush any remaining content
	flushJapanese()
	flushOther()

	return chunks
}

func ContainsKanjis(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}


// String provides human-readable status descriptions
func (s ProcessingStatus) String() string {
	return map[ProcessingStatus]string{
		StatusPreserved:  "Preserved (regular reading & frequent)",
		StatusIrregular:  "Transliterated (irregular reading)",
		StatusInfrequent: "Transliterated (infrequent)",
		StatusUnmappable: "Transliterated (unmappable)",
		StatusNotKanji:   "Preserved (not kanji)",
	}[s]
}


// Helper function to print detailed processing information
func PrintProcessingDetails(result *TransliterationResult) {
	fmt.Printf("Final text:%s\n\n", result.Text)
	fmt.Println("Processing details:")
	for _, token := range result.Tokens {
		fmt.Printf("\tOriginal: %s\n", token.Original)
		fmt.Printf("\tResult:   %s\n", token.Result)
		fmt.Printf("\tStatus:   %s\n", token.Status)
		fmt.Println("------------------")
	}
}




func placeholder3456() {
	fmt.Println("")
	pretty.Pretty([]byte{})
	color.Redln(" 𝒻*** 𝓎ℴ𝓊 𝒸ℴ𝓂𝓅𝒾𝓁ℯ𝓇")
	pp.Println("𝓯*** 𝔂𝓸𝓾 𝓬𝓸𝓶𝓹𝓲𝓵𝓮𝓻")
}

