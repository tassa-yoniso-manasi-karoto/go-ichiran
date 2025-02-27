package ichiran

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	// ICHIRAN_ADVANCED_TEST should be set to "1" to run advanced tests
	// These tests use longer and more complex Japanese text
	advancedTestEnvVar = "ICHIRAN_ADVANCED_TEST"
)

// skipIfNotAdvancedTest skips the test if ICHIRAN_ADVANCED_TEST is not set to "1"
func skipIfNotAdvancedTest(t *testing.T) {
	if os.Getenv(advancedTestEnvVar) != "1" {
		t.Skip("skipping advanced test; set ICHIRAN_ADVANCED_TEST=1 to run")
	}

	// Also skip if manual tests are not enabled (we need Docker)
	if os.Getenv("ICHIRAN_MANUAL_TEST") != "1" {
		t.Skip("skipping test that requires Docker; set ICHIRAN_MANUAL_TEST=1 to run")
	}
}

// TestComplexSentenceWithNestedClauses tests a complex sentence with nested clauses
func TestComplexSentenceWithNestedClauses(t *testing.T) {
	skipIfNotAdvancedTest(t)

	// Initialize Ichiran
	err := Init()
	require.NoError(t, err)

	// Complex sentence with nested clauses, quotes, and multiple types of punctuation
	// "Yesterday, I suddenly dropped by a bookstore, found a difficult philosophy book, and my heart trembled."
	japaneseText := "昨日、ふと立ち寄った本屋で、難解な哲学書を見つけ、心が震えました。"

	// Analyze the text
	tokensPtr, err := Analyze(japaneseText)
	require.NoError(t, err)

	// Verify we have a reasonable number of tokens
	tokens := *tokensPtr
	assert.GreaterOrEqual(t, len(tokens), 15, "Should have at least 15 tokens for complex sentence")

	// Count punctuation marks
	var commaCount, periodCount int
	for _, token := range tokens {
		if token.Surface == "、" || token.Surface == ", " {
			commaCount++
		} else if token.Surface == "。" || token.Surface == ". " {
			periodCount++
		}
	}

	// Should have 3 commas and 1 period
	assert.Equal(t, 3, commaCount, "Should have 3 commas in the sentence")
	assert.Equal(t, 1, periodCount, "Should have 1 period in the sentence")

	// Test various transformations
	t.Run("Complex Transformations", func(t *testing.T) {
		// Check selective transliteration with different thresholds
		lowResult, err := tokensPtr.SelectiveTranslit(50)
		require.NoError(t, err)

		mediumResult, err := tokensPtr.SelectiveTranslit(500)
		require.NoError(t, err)

		highResult, err := tokensPtr.SelectiveTranslit(2000)
		require.NoError(t, err)

		// Results should differ
		assert.NotEqual(t, lowResult, highResult, "Low and high threshold results should differ")
		assert.NotEqual(t, mediumResult, highResult, "Medium and high threshold results should differ")

		// Verify complete mapping
		mapping, err := tokensPtr.SelectiveTranslitFullMapping(1000)
		require.NoError(t, err)

		// Should have tokens for each significant part
		assert.NotEmpty(t, mapping.Text)
		assert.NotEmpty(t, mapping.Tokens)

		// Count various token statuses
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

		// With this complex sentence, we should have tokens in each category
		assert.Greater(t, preserved, 0, "Should have preserved tokens")
		assert.Greater(t, nonKanji, 0, "Should have non-kanji tokens")

		// Romanization should contain spaces and punctuation
		roman := tokensPtr.Roman()
		assert.Contains(t, roman, " ") // Should have spaces
		assert.Contains(t, roman, ",") // Should represent commas
		assert.Contains(t, roman, ".") // Should represent periods
	})
}

// TestMixedLanguageText tests text that contains Japanese and non-Japanese elements
func TestMixedLanguageText(t *testing.T) {
	skipIfNotAdvancedTest(t)

	// Initialize Ichiran
	err := Init()
	require.NoError(t, err)

	// Mixed text with English, numbers, and Japanese
	// "I bought a new iPhone 13 in Tokyo last week for ¥150,000."
	japaneseText := "先週、東京で新しいiPhone 13を¥150,000で買いました。"

	// Analyze the text
	tokensPtr, err := Analyze(japaneseText)
	require.NoError(t, err)

	// Verify we get tokens for both Japanese and non-Japanese parts
	tokens := *tokensPtr

	// Check for proper handling of English words and numbers
	var foundEnglish, foundNumber bool

	// Debug log all tokens
	for i, token := range tokens {
		t.Logf("Token %d: '%s'", i, token.Surface)

		// Check for English words
		if strings.Contains(token.Surface, "iPhone") || strings.Contains(token.Surface, "13") {
			foundEnglish = true
		}

		// Check for currency and numbers (they might be combined in various ways)
		if strings.Contains(token.Surface, "¥") || strings.Contains(token.Surface, "150") ||
			strings.Contains(token.Surface, "150,000") {
			foundNumber = true
		}
	}

	assert.True(t, foundEnglish, "Should properly tokenize English words")
	assert.True(t, foundNumber, "Should properly tokenize numbers and currency symbols")

	// Romanization should contain some of the original elements
	roman := tokensPtr.Roman()
	t.Logf("Romanized text: %s", roman)

	// Check for presence of elements rather than exact strings
	assert.True(t, strings.Contains(roman, "iPhone") || strings.Contains(roman, "iphone") ||
		strings.Contains(roman, "phone"), "Should contain phone reference in romanization")
	assert.True(t, strings.Contains(roman, "150") || strings.Contains(roman, "yen") ||
		strings.Contains(roman, "¥"), "Should contain number or currency reference in romanization")
}

// TestLongArticleText tests analysis of a longer Japanese news article
func TestLongArticleText(t *testing.T) {
	skipIfNotAdvancedTest(t)

	// Initialize Ichiran
	err := Init()
	require.NoError(t, err)

	// Longer Japanese text (excerpt from a news article)
	japaneseText := `日本の科学者たちは、地球温暖化の影響で海水温が上昇していることに警鐘を鳴らしています。
最新の研究によると、過去50年間で日本周辺の海水温は約1.2度上昇しており、これによって日本の気候だけでなく、
海洋生態系にも大きな変化が起きていることが分かっています。特に、南の海域ではサンゴの白化現象が進行し、
北の海域では以前は見られなかった熱帯魚の存在が確認されています。

研究チームのリーダーである田中教授は「このまま温暖化が進めば、日本の漁業にも深刻な影響が出るでしょう」と警告しています。
実際、伝統的な漁場では魚の種類や量に変化が見られ、漁業を生業とする地域社会に影響を与え始めています。

政府は対策として、再生可能エネルギーの促進や炭素排出量の削減目標を掲げていますが、専門家たちはより迅速かつ具体的な行動を求めています。
「私たちには時間がありません。今すぐに行動を起こす必要があります」と環境NGOの代表は述べています。`

	// Analyze the text - due to Ichiran API limitations, we need to process each paragraph separately
	var allTokens JSONTokens

	// Split by paragraphs and process each
	paragraphs := strings.Split(japaneseText, "\n")
	for _, para := range paragraphs {
		if strings.TrimSpace(para) == "" {
			continue
		}

		tokensPtr, err := Analyze(para)
		require.NoError(t, err)

		// Append tokens from this paragraph
		tokens := *tokensPtr
		allTokens = append(allTokens, tokens...)
	}

	// We don't need the pointer for the assertions, just the slice directly

	// Should have a large number of tokens for this long text
	tokens := allTokens
	assert.Greater(t, len(tokens), 50, "Should have many tokens for long article")

	// Since we're processing paragraphs separately, paragraph breaks are handled manually

	// Verify quotes are properly handled
	var quoteCount int
	for _, token := range tokens {
		// Log each token for debugging
		t.Logf("Token: '%s'", token.Surface)

		// Look for quote marks in various forms
		if strings.Contains(token.Surface, "「") ||
			strings.Contains(token.Surface, "」") ||
			strings.Contains(token.Surface, "\"") {
			quoteCount++
		}
	}

	// We should find at least some quotes
	assert.Greater(t, quoteCount, 0, "Should find some quotation marks in the text")

	// Check handling of numbers and specialized terms
	var foundNumbers bool
	for _, token := range tokens {
		if strings.Contains(token.Surface, "1.2") ||
			strings.Contains(token.Surface, "50") ||
			strings.Contains(token.Surface, "1") ||
			strings.Contains(token.Surface, "2") {
			foundNumbers = true
			t.Logf("Found number token: '%s'", token.Surface)
			break
		}
	}
	assert.True(t, foundNumbers, "Should properly handle numeric values")
}

// TestSpecializedVocabulary tests analysis of text with technical/specialized vocabulary
func TestSpecializedVocabulary(t *testing.T) {
	skipIfNotAdvancedTest(t)

	// Initialize Ichiran
	err := Init()
	require.NoError(t, err)

	// Text with specialized medical and technical vocabulary
	japaneseText := "人工知能による画像診断システムを用いて、早期段階での悪性腫瘍の検出率が向上しました。量子コンピューティングの研究進展によって、将来的には創薬プロセスも大幅に効率化されるでしょう。"

	// Analyze the text
	tokensPtr, err := Analyze(japaneseText)
	require.NoError(t, err)

	// Check for technical terms
	tokens := *tokensPtr

	// Define technical terms to look for
	technicalTerms := []string{
		"人工知能", // artificial intelligence
		"画像診断", // image diagnosis
		"悪性腫瘍", // malignant tumor
		"量子",   // quantum
	}

	// Count how many technical terms we found
	var foundTermsCount int
	for _, term := range technicalTerms {
		for _, token := range tokens {
			if token.Surface == term {
				foundTermsCount++
				break
			}
		}
	}

	// Should find at least some of our technical terms
	assert.GreaterOrEqual(t, foundTermsCount, 2,
		"Should identify specialized technical vocabulary")

	// Gloss information should be present for technical terms
	var glossFound bool
	for _, token := range tokens {
		if len(token.Gloss) > 0 {
			glossFound = true
			break
		}
	}
	assert.True(t, glossFound, "Should have gloss information for technical terms")
}

// TestClassicalJapaneseText tests analysis of classical/literary Japanese
func TestClassicalJapaneseText(t *testing.T) {
	skipIfNotAdvancedTest(t)

	// Initialize Ichiran
	err := Init()
	require.NoError(t, err)

	// Classical Japanese text (from the Tale of Genji, opening lines)
	japaneseText := "いづれの御時にか、女御、更衣あまたさぶらひたまひける中に、いとやむごとなき際にはあらぬが、すぐれて時めきたまふありけり。"

	// Analyze the text
	tokensPtr, err := Analyze(japaneseText)
	require.NoError(t, err)

	// Even with classical Japanese, we should get tokenization
	tokens := *tokensPtr
	assert.NotEmpty(t, tokens, "Should tokenize classical Japanese")

	// Get token count
	tokenCount := len(tokens)

	// Log the number of tokens to help with debugging
	t.Logf("Found %d tokens in classical Japanese text", tokenCount)

	// Classical Japanese should still produce readings
	readings := tokensPtr.Kana()
	assert.NotEmpty(t, readings, "Should produce readings even for classical Japanese")

	// Get romanized form
	roman := tokensPtr.Roman()
	assert.NotEmpty(t, roman, "Should produce romanization even for classical Japanese")
}

// TestEdgeCases tests various edge cases and unusual inputs
func TestEdgeCases(t *testing.T) {
	skipIfNotAdvancedTest(t)

	// Initialize Ichiran
	err := Init()
	require.NoError(t, err)

	// Test cases with edge case inputs
	testCases := []struct {
		name     string
		input    string
		expected int // minimum expected token count
	}{
		{
			name:     "very short input",
			input:    "猫。",
			expected: 2, // "猫" and "。"
		},
		{
			name:     "repeating characters",
			input:    "わくわくドキドキ！",
			expected: 3, // "わくわく", "ドキドキ", "！"
		},
		{
			name:     "unusual punctuation",
			input:    "「えっ？」「はぁ…」（考え中）",
			expected: 9, // Each punctuation mark and word should be tokenized
		},
		{
			name:     "emoji and symbols",
			input:    "今日も頑張りましょう！👍✨",
			expected: 4, // Sentence + emojis (may vary based on how ichiran handles emojis)
		},
		{
			name:     "rare kanji",
			input:    "𠮷野家で食事をした。", // Uses uncommon/variant kanji for "Yoshinoya"
			expected: 5,
		},
		{
			name:     "repeated punctuation",
			input:    "えええ！？！？",
			expected: 2,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Analyze the text
			tokensPtr, err := Analyze(tc.input)
			require.NoError(t, err, "Analysis should not fail")

			// Verify token count
			tokens := *tokensPtr
			assert.GreaterOrEqual(t, len(tokens), tc.expected,
				"Should have expected minimum token count")

			// Check basic transformations
			romanized := tokensPtr.Roman()
			assert.NotEmpty(t, romanized, "Should produce some romanization")

			kana := tokensPtr.Kana()
			assert.NotEmpty(t, kana, "Should produce kana readings")
		})
	}
}

// TestComplexGrammaticalStructures tests parsing of sentences with complex grammar
func TestComplexGrammaticalStructures(t *testing.T) {
	skipIfNotAdvancedTest(t)

	// Initialize Ichiran
	err := Init()
	require.NoError(t, err)

	// Text with complex grammatical structures, conditional clauses, passive voice, etc.
	japaneseText := "もし私が誘われていなかったら、そのパーティーに行かなかったでしょうし、あなたにも会えなかったかもしれません。物事は時々、予想もしなかった形で展開するものですね。"

	// Analyze the text
	tokensPtr, err := Analyze(japaneseText)
	require.NoError(t, err)

	// Verify we get tokens
	tokens := *tokensPtr
	assert.NotEmpty(t, tokens, "Should tokenize text with complex grammar")

	// Look for grammatical elements
	var foundConditional, foundPassive, foundNegative, foundPotential bool

	for _, token := range tokens {
		// Check surface form for indicators
		if token.Surface == "たら" || token.Surface == "ば" || token.Surface == "なら" {
			foundConditional = true
		}

		if strings.Contains(token.Surface, "れ") && (strings.Contains(token.Surface, "いる") || strings.Contains(token.Surface, "いた")) {
			foundPassive = true
		}

		if strings.Contains(token.Surface, "なかっ") || strings.Contains(token.Surface, "ない") || strings.Contains(token.Surface, "ず") {
			foundNegative = true
		}

		if strings.Contains(token.Surface, "える") || strings.Contains(token.Surface, "れる") {
			foundPotential = true
		}

		// Also check conjugation data
		for _, conj := range token.Conj {
			for _, prop := range conj.Prop {
				if prop.Type == "conditional" {
					foundConditional = true
				}
				if prop.Type == "passive" {
					foundPassive = true
				}
				if prop.Neg {
					foundNegative = true
				}
				if prop.Type == "potential" {
					foundPotential = true
				}
			}
		}
	}

	// We should find at least some of these grammatical structures
	// (Note: exact identification depends on ichiran's capabilities)
	assert.True(t, foundConditional || foundPassive || foundNegative || foundPotential,
		"Should identify some complex grammatical structures")
}
