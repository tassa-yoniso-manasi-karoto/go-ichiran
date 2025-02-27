package ichiran

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTokenizedMethods(t *testing.T) {
	tokens := createTestTokens()

	t.Run("Tokenized", func(t *testing.T) {
		expected := "私 は 日本語 を 勉強して います 。"
		result := tokens.Tokenized()
		assert.Equal(t, expected, result)
	})

	t.Run("TokenizedParts", func(t *testing.T) {
		expected := []string{"私", "は", "日本語", "を", "勉強して", "います", "。"}
		result := tokens.TokenizedParts()
		assert.Equal(t, expected, result)
	})
}

func TestKanaMethods(t *testing.T) {
	tokens := createTestTokens()

	t.Run("Kana", func(t *testing.T) {
		expected := "わたしはにほんごをべんきょうしています。"
		result := tokens.Kana()
		assert.Equal(t, expected, result)
	})

	t.Run("KanaParts", func(t *testing.T) {
		expected := []string{"わたし", "は", "にほんご", "を", "べんきょうして", "います", "。"}
		result := tokens.KanaParts()
		assert.Equal(t, expected, result)
	})
}

func TestRomanMethods(t *testing.T) {
	tokens := createTestTokens()

	t.Run("Roman", func(t *testing.T) {
		expected := "watashi wa nihongo wo benkyou shite imasu 。"
		result := tokens.Roman()
		assert.Equal(t, expected, result)
	})

	t.Run("RomanParts", func(t *testing.T) {
		expected := []string{"watashi", "wa", "nihongo", "wo", "benkyou shite", "imasu", "。"}
		result := tokens.RomanParts()
		assert.Equal(t, expected, result)
	})
}

func TestToMorphemes(t *testing.T) {
	tokens := createTestTokens()
	result := tokens.ToMorphemes()

	// Original has 7 tokens, but one token has 2 components
	// so morphemes should have 8 tokens
	assert.Equal(t, 8, len(result))

	// Check if the compound token was expanded
	compoundIdx := 4 // Index of "勉強して" in the original tokens
	assert.Equal(t, 2, len(tokens[compoundIdx].Components))

	// Verify the morphemes contain the expanded components
	morphemeIdx := 4 // Where the components start in the morphemes
	assert.Equal(t, "勉強", result[morphemeIdx].Surface)
	assert.Equal(t, "して", result[morphemeIdx+1].Surface)
}

func TestGlossMethods(t *testing.T) {
	tokens := createTestTokens()

	t.Run("GlossParts", func(t *testing.T) {
		parts := tokens.GlossParts()
		assert.Equal(t, 8, len(parts))

		// Check the first token's gloss
		assert.Contains(t, parts[0], "私(I; me)")

		// Check a token with compound components
		assert.Contains(t, parts[4], "勉強(study)")
	})

	t.Run("Gloss", func(t *testing.T) {
		result := tokens.Gloss()
		assert.Contains(t, result, "私(I; me)")
		assert.Contains(t, result, "日本語(Japanese language)")
	})
}
