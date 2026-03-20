package ichiran

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseWordEntryRepairsNonLatinRomajiFromKana(t *testing.T) {
	word := []interface{}{
		"飛",
		map[string]interface{}{
			"type":       "KANJI",
			"text":       "飛",
			"kana":       "ひ",
			"seq":        float64(0),
			"score":      float64(0),
			"components": []interface{}{},
		},
		[]interface{}{},
	}

	token, err := parseWordEntry(word)
	assert.NoError(t, err)
	assert.Equal(t, "hi", token.Romaji)
}

func TestParseWordEntryDistributesAlternativeRomaji(t *testing.T) {
	word := []interface{}{
		"tometa/yameta",
		map[string]interface{}{
			"type":        "KANJI",
			"text":        "止めた",
			"kana":        []interface{}{"とめた", "やめた"},
			"seq":         []interface{}{float64(10370482), float64(10841006)},
			"score":       float64(336),
			"alternative": true,
			"gloss": map[string]interface{}{
				"alternative": []interface{}{
					map[string]interface{}{
						"text":    "止めた",
						"kana":    "とめた",
						"reading": "止めた 【とめた】",
						"score":   float64(336),
						"seq":     float64(10370482),
					},
					map[string]interface{}{
						"text":    "止めた",
						"kana":    "やめた",
						"reading": "止めた 【やめた】",
						"score":   float64(240),
						"seq":     float64(10841006),
					},
				},
			},
		},
		[]interface{}{},
	}

	token, err := parseWordEntry(word)
	assert.NoError(t, err)
	if assert.Len(t, token.Alternative, 2) {
		assert.Equal(t, "tometa", token.Alternative[0].Romaji)
		assert.Equal(t, "yameta", token.Alternative[1].Romaji)
	}
	assert.Equal(t, "tometa", token.Romaji)
}
