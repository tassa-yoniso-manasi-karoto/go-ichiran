package ichiran

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/gookit/color"
	"github.com/k0kubun/pp"
	"github.com/tidwall/pretty"
)

type ProcessingStatus int

const (
	StatusPreserved  ProcessingStatus = iota // Kanji was preserved (regular reading & under frequency threshold)
	StatusIrregular                          // Kanji was transliterated due to irregular reading
	StatusInfrequent                         // Kanji was transliterated due to being over frequency threshold
	StatusUnmappable                         // Kanji was transliterated due to inability to map reading
	StatusNotKanji                           // Token was not a kanji character
)

// isRegularReading checks if the kanji has a regular reading pattern
func isRegularReading(reading KanjiReading) bool {
	// A reading is considered regular if:
	// 1. It has a direct link between kanji and reading (link=true)
	// 2. It doesn't have special modifications (geminated is empty)
	return reading.Link && reading.Geminated == ""
}

// SelectiveTranslit performs selective transliteration of the tokens based on kanji frequency.
// It preserves kanji that are both:
//   - Below the specified frequency threshold (lower number = more frequent)
//   - Have regular readings (no special phonetic modifications)
//
// Other kanji are converted to their hiragana readings.
//
// Parameter freqThreshold: Maximum frequency rank to preserve (1-3000, lower = more frequent)
func (tokens JSONTokens) SelectiveTranslit(freqThreshold int) (string, error) {
	tlitStruct, err := tokens.selectiveTranslit(freqThreshold, false)
	return tlitStruct.Text, err
}

// SelectiveTranslitTokenized performs selective transliteration with spaces between tokens.
// It uses the same kanji preservation rules as SelectiveTranslit but adds spaces between
// morphological units to improve readability for learners.
//
// Parameter freqThreshold: Maximum frequency rank to preserve (1-3000, lower = more frequent)
func (tokens JSONTokens) SelectiveTranslitTokenized(freqThreshold int) (string, error) {
	tlitStruct, err := tokens.selectiveTranslit(freqThreshold, true)
	return tlitStruct.Text, err
}

func (tokens JSONTokens) SelectiveTranslitFullMapping(freqThreshold int) (*TransliterationResult, error) {
	return tokens.selectiveTranslit(freqThreshold, false)
}

// SelectiveTranslitFullMappingTokenized is similar to SelectiveTranslitFullMapping but
// adds spaces between tokens in the resulting text.
func (tokens JSONTokens) SelectiveTranslitFullMappingTokenized(freqThreshold int) (*TransliterationResult, error) {
	return tokens.selectiveTranslit(freqThreshold, true)
}

func (tokens JSONTokens) selectiveTranslit(freqThreshold int, tokenize bool) (*TransliterationResult, error) {
	var allProcessedTokens []ProcessedToken
	var tokenResults []string // Store each token's processed result

	// Process each token
	for _, token := range tokens {
		if !token.IsLexical || !ContainsKanjis(token.Surface) {
			// Preserve non-processable tokens as-is
			processedToken := ProcessedToken{
				Original: token.Surface,
				Result:   token.Surface,
				Status:   StatusNotKanji,
			}
			tokenResults = append(tokenResults, token.Surface)
			allProcessedTokens = append(allProcessedTokens, processedToken)
			continue
		}

		// Use the already parsed kanji readings from the token
		readings := token.KanjiReadings
		if len(readings) == 0 {
			// If no readings available, preserve the token as-is
			processedToken := ProcessedToken{
				Original: token.Surface,
				Result:   token.Surface,
				Status:   StatusUnmappable,
			}
			tokenResults = append(tokenResults, token.Surface)
			allProcessedTokens = append(allProcessedTokens, processedToken)
			continue
		}

		// Process each kanji reading
		var tokenResult strings.Builder
		for _, r := range readings {
			// Check if this is a multi-character kanji reading (a compound)
			if len(r.Kanji) > 1 {
				// For compound kanji like "一二", process each individual kanji
				allPreserved := true
				individualResults := make([]string, 0, len(r.Kanji))

				// Process each individual kanji in the compound
				for _, runeValue := range r.Kanji {
					singleKanji := string(runeValue)
					freq := slices.Index(kanjiFreqSlice, singleKanji)
					exists := freq > -1
					if exists {
						freq += 1 // Convert 0-based index to 1-based frequency rank
					}

					// Check if this individual kanji should be preserved
					shouldPreserveKanji := exists && freq > 0 && freq <= freqThreshold
					if shouldPreserveKanji {
						individualResults = append(individualResults, singleKanji)
					} else {
						// If even one kanji in the compound doesn't meet the criteria,
						// we'll use the kana reading for the whole compound
						allPreserved = false
						break
					}
				}

				var processedToken ProcessedToken
				processedToken.Original = r.Kanji

				if allPreserved {
					// All individual kanji should be preserved, join them back together
					preservedCompound := strings.Join(individualResults, "")
					processedToken.Result = preservedCompound
					processedToken.Status = StatusPreserved
				} else {
					// Some kanji couldn't be preserved, use the kana reading for the whole compound
					processedToken.Result = r.Reading
					processedToken.Status = StatusInfrequent
				}

				tokenResult.WriteString(processedToken.Result)
				allProcessedTokens = append(allProcessedTokens, processedToken)

			} else {
				// Normal single kanji processing
				exists := false

				kanji := r.Kanji
				freq := slices.Index(kanjiFreqSlice, kanji)
				if freq > -1 {
					freq += 1 // Convert 0-based index to 1-based frequency rank
					exists = true
				}

				var processedToken ProcessedToken
				processedToken.Original = kanji

				isRegular := isRegularReading(r)

				shouldPreserve := exists &&
					freq > 0 && freq <= freqThreshold &&
					isRegular

				if shouldPreserve {
					processedToken.Result = kanji
					processedToken.Status = StatusPreserved
				} else {
					processedToken.Result = r.Reading
					if !exists || freq > freqThreshold {
						processedToken.Status = StatusInfrequent
					} else if !isRegularReading(r) {
						processedToken.Status = StatusIrregular
					} else {
						processedToken.Status = StatusUnmappable
					}
				}

				tokenResult.WriteString(processedToken.Result)
				allProcessedTokens = append(allProcessedTokens, processedToken)
			}
		}

		// Store the result for this token
		if tokenResult.Len() == 0 {
			tokenResults = append(tokenResults, token.Kana)
		} else {
			tokenResults = append(tokenResults, tokenResult.String())
		}
	}

	// Join the token results with or without spaces based on tokenize parameter
	var finalText string
	if tokenize {
		finalText = JoinWithSpacingRule(tokenResults)
	} else {
		finalText = strings.Join(tokenResults, "")
	}

	return &TransliterationResult{
		Text:   finalText,
		Tokens: allProcessedTokens,
	}, nil
}

// ContainsKanjis checks if a string contains any kanji characters
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

// cleanLispCode removes Lisp comments and cleans up the code for better shell execution
func cleanLispCode(code string) string {
	// Regular expression to match Lisp comments (semicolon to end of line)
	reComments := regexp.MustCompile(`;+[^\n]*`)

	// Remove all comments
	code = reComments.ReplaceAllString(code, "")

	// Normalize whitespace
	code = strings.ReplaceAll(code, "\n", " ")
	code = strings.ReplaceAll(code, "\t", " ")

	// Multiple consecutive spaces to a single space
	reSpaces := regexp.MustCompile(`\s{2,}`)
	code = reSpaces.ReplaceAllString(code, " ")

	return code
}

// PrintProcessingDetails prints a human-readable report of the transliteration process
func PrintProcessingDetails(result *TransliterationResult) {
	fmt.Printf("Final text: %s\n\n", result.Text)
	fmt.Println("Processing details:")
	for _, token := range result.Tokens {
		fmt.Printf("\tOriginal: %s\n", token.Original)
		fmt.Printf("\tResult:   %s\n", token.Result)
		fmt.Printf("\tStatus:   %s\n", token.Status)
		fmt.Println("------------------")
	}
}

func placeholder433() {
	fmt.Print("")
	pretty.Pretty([]byte{})
	color.Redln(" 𝒻*** 𝓎ℴ𝓊 𝒸ℴ𝓂𝓅𝒾𝓁ℯ𝓇")
	pp.Println("𝓯*** 𝔂𝓸𝓾 𝓬𝓸𝓶𝓹𝓲𝓵𝓮𝓻")
}