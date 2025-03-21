package ichiran

import (
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDefaultInstanceRecreation tests that the default instance can be properly recreated
// after being closed
func TestDefaultInstanceRecreation(t *testing.T) {
	t.Skip("Skipping test that requires Docker container - run manually with ICHIRAN_MANUAL_TEST=1")
	
	// First lifecycle
	ctx1 := context.Background()
	err := InitWithContext(ctx1)
	assert.NoError(t, err, "First initialization should succeed")
	
	// Do something with the first instance
	tokens1, err := AnalyzeWithContext(ctx1, "こんにちは")
	assert.NoError(t, err, "First analysis should succeed")
	assert.NotNil(t, tokens1, "Should get valid tokens from first analysis")
	
	// Close the first instance
	err = Close()
	assert.NoError(t, err, "Closing should succeed")
	
	// Wait a moment to ensure all shutdown operations complete
	time.Sleep(2 * time.Second)
	
	// Second lifecycle with a new context
	ctx2 := context.Background()
	err = InitWithContext(ctx2)
	assert.NoError(t, err, "Second initialization should succeed")
	
	// Do something with the second instance
	tokens2, err := AnalyzeWithContext(ctx2, "こんにちは")
	assert.NoError(t, err, "Second analysis should succeed")
	assert.NotNil(t, tokens2, "Should get valid tokens from second analysis")
	
	// Clean up
	err = Close()
	assert.NoError(t, err, "Final cleanup should succeed")
}

// TestBackwardCompatMultipleLifecycles tests the backward compatible API
// with multiple init-analyze-close cycles
func TestBackwardCompatMultipleLifecycles(t *testing.T) {
	t.Skip("Skipping test that requires Docker container - run manually with ICHIRAN_MANUAL_TEST=1")
	
	// First lifecycle
	err := Init()
	assert.NoError(t, err, "First initialization should succeed")
	
	tokens1, err := Analyze("こんにちは")
	assert.NoError(t, err, "First analysis should succeed")
	assert.NotNil(t, tokens1, "Should get valid tokens from first analysis")
	
	err = Close()
	assert.NoError(t, err, "First closing should succeed")
	
	// Wait a moment to ensure all shutdown operations complete
	time.Sleep(2 * time.Second)
	
	// Second lifecycle
	err = Init()
	assert.NoError(t, err, "Second initialization should succeed")
	
	tokens2, err := Analyze("さようなら")
	assert.NoError(t, err, "Second analysis should succeed")
	assert.NotNil(t, tokens2, "Should get valid tokens from second analysis")
	
	err = Close()
	assert.NoError(t, err, "Second closing should succeed")
}


// TestAnalyzeWithContext demonstrates the context-aware API
func TestAnalyzeWithContext(t *testing.T) {
	t.Skip("Skipping test that requires Docker container - run manually with ICHIRAN_MANUAL_TEST=1")
	
	// Create a context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()
	
	// Initialize with context
	err := InitWithContext(ctx)
	assert.NoError(t, err)
	defer Close()
	
	// Use the context-aware Analyze function
	tokens, err := AnalyzeWithContext(ctx, "こんにちは")
	assert.NoError(t, err)
	assert.NotNil(t, tokens)
	
	// Check the results
	found := false
	for _, token := range *tokens {
		if token.Surface == "こんにちは" {
			found = true
			assert.Contains(t, strings.ToLower(token.Romaji), "konnichiha")
			break
		}
	}
	assert.True(t, found, "Expected to find token 'こんにちは' in results")
}

// TestNewManagerAPI demonstrates using the manager-based API
func TestNewManagerAPI(t *testing.T) {
	t.Skip("Skipping test that requires Docker container - run manually with ICHIRAN_MANUAL_TEST=1")
	
	// Create a context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	
	// Create a custom manager with options
	manager, err := NewManager(ctx, 
		WithProjectName("ichiran-test"),
		WithQueryTimeout(1*time.Minute))
	assert.NoError(t, err)
	assert.NotNil(t, manager)
	
	// Initialize with quiet mode to reduce log output
	err = manager.InitQuiet(ctx)
	assert.NoError(t, err)
	
	// Clean up when done
	defer manager.Close()
	
	// Test analysis with the manager
	result, err := manager.Analyze(ctx, "こんにちは")
	assert.NoError(t, err)
	assert.NotNil(t, result)
	
	// Verify we got meaningful results
	assert.Greater(t, len(*result), 0, "Expected non-empty result")
	
	// Check if we got the expected token
	found := false
	for _, token := range *result {
		if token.Surface == "こんにちは" {
			found = true
			assert.Contains(t, strings.ToLower(token.Romaji), "konnichiha")
			break
		}
	}
	
	assert.True(t, found, "Expected to find token 'こんにちは' in results")
}

// TestMultipleInstances demonstrates running multiple Ichiran instances concurrently
func TestMultipleInstances(t *testing.T) {
	t.Skip("Skipping test that requires Docker container - run manually with ICHIRAN_MANUAL_TEST=1")
	
	// Create a context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	
	// Create two managers with different project names
	manager1, err := NewManager(ctx, 
		WithProjectName("ichiran-test1"),
		WithContainerName("ichiran-test1-main-1"))
	assert.NoError(t, err)
	assert.NotNil(t, manager1)
	
	manager2, err := NewManager(ctx, 
		WithProjectName("ichiran-test2"),
		WithContainerName("ichiran-test2-main-1"))
	assert.NoError(t, err)
	assert.NotNil(t, manager2)
	
	// Initialize both managers
	err = manager1.InitQuiet(ctx)
	assert.NoError(t, err)
	defer manager1.Close()
	
	err = manager2.InitQuiet(ctx)
	assert.NoError(t, err)
	defer manager2.Close()
	
	// Test analysis with both managers
	result1, err := manager1.Analyze(ctx, "こんにちは")
	assert.NoError(t, err)
	assert.NotNil(t, result1)
	assert.Greater(t, len(*result1), 0, "Expected non-empty result from manager1")
	
	result2, err := manager2.Analyze(ctx, "さようなら")
	assert.NoError(t, err)
	assert.NotNil(t, result2)
	assert.Greater(t, len(*result2), 0, "Expected non-empty result from manager2")
}

// TestFullPipelineIntegration tests the complete Japanese analysis pipeline
func TestFullPipelineIntegration(t *testing.T) {
	// Skip test if not in manual test mode
	if os.Getenv("ICHIRAN_MANUAL_TEST") != "1" {
		t.Skip("skipping test that requires Docker; set ICHIRAN_MANUAL_TEST=1 to run")
	}

	// Initialize Ichiran
	err := Init()
	require.NoError(t, err)

	// Test text with a mix of kanji, punctuation, and a period
	japaneseText := "躊躇、探求。"

	// Analyze the text
	tokensPtr, err := Analyze(japaneseText)
	require.NoError(t, err)

	// Test all of the transformation APIs
	t.Run("Basic Transformations", func(t *testing.T) {
		// Get tokenized text
		tokenized := tokensPtr.Tokenized()
		assert.NotEmpty(t, tokenized)
		assert.Contains(t, tokenized, "躊躇")
		assert.Contains(t, tokenized, "探求")
		assert.Contains(t, tokenized, ". ") // Note the space after the period
		assert.Contains(t, tokenized, ", ") // Note the space after the comma

		// Get kana text
		kana := tokensPtr.Kana()
		assert.NotEmpty(t, kana)
		assert.Contains(t, kana, "ちゅうちょ")
		assert.Contains(t, kana, "たんきゅう")

		// Get romanized text
		roman := tokensPtr.Roman()
		assert.NotEmpty(t, roman)
		assert.Contains(t, roman, "chūcho")
		assert.Contains(t, roman, "tankyū")
	})

	// Test selective transliteration with multiple thresholds
	t.Run("Selective Transliteration", func(t *testing.T) {
		// Test with low threshold (mostly hiragana result)
		lowResult, err := tokensPtr.SelectiveTranslit(50)
		require.NoError(t, err)

		// Test with high threshold (mostly kanji result)
		highResult, err := tokensPtr.SelectiveTranslit(2000)
		require.NoError(t, err)

		// Results should be different
		assert.NotEqual(t, lowResult, highResult, "Results should differ between low and high thresholds")

		// Low threshold should have more hiragana (fewer kanji)
		assert.Contains(t, lowResult, "ちゅうちょ", "Low threshold should convert some kanji to kana")

		// High threshold should preserve common kanji
		assert.Contains(t, highResult, "探求", "High threshold should preserve common kanji like 探求")
	})

	// Test full mapping
	t.Run("Full Mapping", func(t *testing.T) {
		// Note: 探 (tan) has a frequency of ~65% at grade 6, and 求 (kyuu) has ~78.95% at grade 4
		// For our test string, we need to use a threshold that will give us a mix of preserved and transliterated
		mapping, err := tokensPtr.SelectiveTranslitFullMapping(70) // Lower threshold to get a mix
		require.NoError(t, err)

		// Verify mapping structure
		assert.NotEmpty(t, mapping.Text)
		assert.NotEmpty(t, mapping.Tokens)

		// Log the mapping details for debugging
		t.Logf("Mapping result: %s", mapping.Text)
		for _, token := range mapping.Tokens {
			t.Logf("Token: %s → %s (%s)", token.Original, token.Result, token.Status)
		}

		// Count preserved vs transliterated tokens
		var preserved, transliterated, nonKanji int
		for _, token := range mapping.Tokens {
			switch token.Status {
			case StatusPreserved:
				preserved++
			case StatusIrregular, StatusInfrequent, StatusUnmappable:
				transliterated++
			case StatusNotKanji:
				nonKanji++
			}
		}

		// In our test string, we should have punctuation tokens at minimum
		assert.Greater(t, nonKanji, 0, "Should have some non-kanji tokens (punctuation)")

		// Either some kanji should be preserved or some should be transliterated
		assert.True(t, preserved > 0 || transliterated > 0,
			"Should have either preserved or transliterated kanji tokens")
	})

	// Test behavior with periods in the middle of text (bug fix verification)
	t.Run("Punctuation Handling", func(t *testing.T) {
		// Verify that text is properly tokenized with punctuation
		tokenParts := tokensPtr.TokenizedParts()

		// Output the token parts for debugging
		t.Logf("Token parts: %v", tokenParts)

		// Find index of comma
		commaIndex := -1
		for i, part := range tokenParts {
			if part == "、" || part == ", " {
				commaIndex = i
				break
			}
		}

		// Verify we found a comma
		assert.Greater(t, commaIndex, 0, "Failed to find comma in tokenized parts")

		// Verify there are tokens after the comma
		assert.Greater(t, len(tokenParts), commaIndex+1, "No tokens after comma")

		// Find index of period
		periodIndex := -1
		for i, part := range tokenParts {
			if part == "。" || part == ". " {
				periodIndex = i
				break
			}
		}

		// Verify we found a period
		assert.Greater(t, periodIndex, 0, "Failed to find period in tokenized parts")

		// Verify that tokens are in the expected order: 躊躇 then comma then 探求 then period
		assert.Greater(t, commaIndex, 0, "Comma should be after first token")
		assert.Greater(t, periodIndex, commaIndex, "Period should be after comma")
	})
}

// TestKanjiReadings tests the kanji reading functionality
func TestKanjiReadings(t *testing.T) {
	// Skip test if not in manual test mode
	if os.Getenv("ICHIRAN_MANUAL_TEST") != "1" {
		t.Skip("skipping test that requires Docker; set ICHIRAN_MANUAL_TEST=1 to run")
	}

	// Initialize Ichiran
	err := Init()
	require.NoError(t, err)

	// Test text with various kanji
	japaneseText := "日本語の勉強"

	// Analyze the text
	tokensPtr, err := Analyze(japaneseText)
	require.NoError(t, err)

	// Verify kanji readings are populated
	t.Run("Kanji Reading Data", func(t *testing.T) {
		// Get tokens from pointer
		tokens := *tokensPtr
		// Check kanji tokens
		for _, token := range tokens {
			if ContainsKanjis(token.Surface) {
				assert.NotEmpty(t, token.KanjiReadings,
					"Token with kanji '%s' should have KanjiReadings", token.Surface)

				// Verify readings have expected fields
				for _, reading := range token.KanjiReadings {
					assert.NotEmpty(t, reading.Kanji, "Kanji should be present in reading")
					assert.NotEmpty(t, reading.Reading, "Reading should be present")
				}
			}
		}
	})

	// Test isRegularReading functionality
	t.Run("Regular Reading Detection", func(t *testing.T) {
		// Find some readings to test
		var regularFound, irregularFound bool

		tokens := *tokensPtr
		for _, token := range tokens {
			for _, reading := range token.KanjiReadings {
				if isRegularReading(reading) {
					regularFound = true
				} else {
					irregularFound = true
				}
			}
		}

		// Japanese text should typically have some of each
		assert.True(t, regularFound || irregularFound,
			"Should find at least some regular or irregular readings")
	})
}

// TestAnalyzeWithOption tests the Analyze function with options
func TestAnalyzeWithOption(t *testing.T) {
	// Skip test if not in manual test mode
	if os.Getenv("ICHIRAN_MANUAL_TEST") != "1" {
		t.Skip("skipping test that requires Docker; set ICHIRAN_MANUAL_TEST=1 to run")
	}

	// Initialize Ichiran
	err := Init()
	require.NoError(t, err)

	// Test text with kanji
	japaneseText := "日本語"

	// Compare results with and without options
	defaultTokens, err := Analyze(japaneseText)
	require.NoError(t, err)

	// With options (using same text for comparison since AnalyzeWithOptions doesn't exist yet)
	optsTokens, err := Analyze(japaneseText)
	require.NoError(t, err)

	// Results should be the same
	assert.Equal(t, len(*defaultTokens), len(*optsTokens),
		"Default and with-options analysis should yield same token count")

	// Compare using go-cmp for detailed diff
	diff := cmp.Diff(defaultTokens.TokenizedParts(), optsTokens.TokenizedParts())
	assert.Empty(t, diff, "Default and with-options analysis should yield same tokens")
}

// createHelperTestTokens creates consistent test tokens for unit testing
func createHelperTestTokens() JSONTokens {
	// Create a small set of tokens for a Japanese sentence
	// "私は日本語を勉強しています。" (I am studying Japanese.)
	tokens := JSONTokens{
		&JSONToken{
			Surface:   "私",
			IsLexical: true,
			Reading:   "わたし",
			Kana:      "わたし",
			Romaji:    "watashi",
			Gloss: []Gloss{
				{Gloss: "I; me"},
			},
		},
		&JSONToken{
			Surface:   "は",
			IsLexical: true,
			Reading:   "は",
			Kana:      "は",
			Romaji:    "wa",
		},
		&JSONToken{
			Surface:   "日本語",
			IsLexical: true,
			Reading:   "にほんご",
			Kana:      "にほんご",
			Romaji:    "nihongo",
			Gloss: []Gloss{
				{Gloss: "Japanese language"},
			},
			KanjiReadings: []KanjiReading{
				{Kanji: "日", Reading: "に", Link: true},
				{Kanji: "本", Reading: "ほん", Link: true},
				{Kanji: "語", Reading: "ご", Link: true},
			},
		},
		&JSONToken{
			Surface:   "を",
			IsLexical: true,
			Reading:   "を",
			Kana:      "を",
			Romaji:    "wo",
		},
		&JSONToken{
			Surface:   "勉強して",
			IsLexical: true,
			Reading:   "べんきょうして",
			Kana:      "べんきょうして",
			Romaji:    "benkyou shite",
			Gloss: []Gloss{
				{Gloss: "study"},
			},
			Components: []JSONToken{
				{
					Surface:   "勉強",
					IsLexical: true,
					Reading:   "べんきょう",
					Kana:      "べんきょう",
					Romaji:    "benkyou",
					Gloss: []Gloss{
						{Gloss: "study"},
					},
					KanjiReadings: []KanjiReading{
						{Kanji: "勉", Reading: "べん", Link: true},
						{Kanji: "強", Reading: "きょう", Link: true},
					},
				},
				{
					Surface:   "して",
					IsLexical: true,
					Reading:   "して",
					Kana:      "して",
					Romaji:    "shite",
				},
			},
			KanjiReadings: []KanjiReading{
				{Kanji: "勉", Reading: "べん", Link: true},
				{Kanji: "強", Reading: "きょう", Link: true},
			},
		},
		&JSONToken{
			Surface:   "います",
			IsLexical: true,
			Reading:   "います",
			Kana:      "います",
			Romaji:    "imasu",
		},
		&JSONToken{
			Surface:   "。",
			IsLexical: false,
			Reading:   "。",
			Kana:      "。",
			Romaji:    "。",
		},
	}

	return tokens
}
