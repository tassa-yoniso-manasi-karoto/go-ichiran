package ichiran

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractCore(t *testing.T) {
	originalToken := JSONToken{
		Surface:   "日本語",
		IsLexical: true,
		Reading:   "にほんご",
		Kana:      "にほんご",
		Romaji:    "nihongo",
		Score:     100,
		Seq:       1,
		Gloss: []Gloss{
			{Pos: "n", Gloss: "Japanese language", Info: ""},
		},
		Conj: []Conj{
			{Reading: "にほんご", ReadOk: true},
		},
	}

	expected := jsonTokenCore{
		Surface:   "日本語",
		IsLexical: true,
		Reading:   "にほんご",
		Kana:      "にほんご",
		Romaji:    "nihongo",
		Score:     100,
	}

	result := extractCore(originalToken)
	assert.Equal(t, expected, result)
}

func TestApplyCore(t *testing.T) {
	core := jsonTokenCore{
		Surface:   "日本語",
		IsLexical: true,
		Reading:   "にほんご",
		Kana:      "にほんご",
		Romaji:    "nihongo",
		Score:     100,
	}

	token := &JSONToken{
		Surface:   "",
		IsLexical: false,
		Reading:   "",
		Kana:      "",
		Romaji:    "",
		Score:     0,
		Seq:       5,
		Gloss: []Gloss{
			{Pos: "n", Gloss: "test", Info: ""},
		},
	}

	token.applyCore(core)

	// These should be updated
	assert.Equal(t, "日本語", token.Surface)
	assert.Equal(t, true, token.IsLexical)
	assert.Equal(t, "にほんご", token.Reading)
	assert.Equal(t, "にほんご", token.Kana)
	assert.Equal(t, "nihongo", token.Romaji)
	assert.Equal(t, 100, token.Score)

	// These should remain unchanged
	assert.Equal(t, 5, token.Seq)
	assert.Equal(t, "n", token.Gloss[0].Pos)
	assert.Equal(t, "test", token.Gloss[0].Gloss)
}

func TestJSONTokenGetGlosses(t *testing.T) {
	token := &JSONToken{
		Surface: "勉強する",
		Gloss: []Gloss{
			{Pos: "n", Gloss: "study", Info: ""},
			{Pos: "n", Gloss: "diligence", Info: ""},
		},
		Conj: []Conj{
			{
				Gloss: []Gloss{
					{Pos: "v5", Gloss: "to study", Info: ""},
					{Pos: "v5", Gloss: "to learn", Info: ""},
				},
			},
		},
	}

	expected := []string{"study", "diligence", "to study", "to learn"}
	result := token.getGlosses()

	assert.Equal(t, expected, result)
}

func createTestTokens() JSONTokens {
	return JSONTokens{
		&JSONToken{
			Surface:   "私",
			IsLexical: true,
			Reading:   "わたし",
			Kana:      "わたし",
			Romaji:    "watashi",
			Score:     100,
			Gloss: []Gloss{
				{Pos: "pn", Gloss: "I", Info: ""},
				{Pos: "pn", Gloss: "me", Info: ""},
			},
		},
		&JSONToken{
			Surface:   "は",
			IsLexical: true,
			Reading:   "は",
			Kana:      "は",
			Romaji:    "wa",
			Score:     100,
			Gloss: []Gloss{
				{Pos: "prt", Gloss: "topic marker", Info: ""},
			},
		},
		&JSONToken{
			Surface:   "日本語",
			IsLexical: true,
			Reading:   "にほんご",
			Kana:      "にほんご",
			Romaji:    "nihongo",
			Score:     100,
			Gloss: []Gloss{
				{Pos: "n", Gloss: "Japanese language", Info: ""},
			},
		},
		&JSONToken{
			Surface:   "を",
			IsLexical: true,
			Reading:   "を",
			Kana:      "を",
			Romaji:    "wo",
			Score:     100,
			Gloss: []Gloss{
				{Pos: "prt", Gloss: "object marker", Info: ""},
			},
		},
		&JSONToken{
			Surface:   "勉強して",
			IsLexical: true,
			Reading:   "べんきょうして",
			Kana:      "べんきょうして",
			Romaji:    "benkyou shite",
			Score:     100,
			Gloss: []Gloss{
				{Pos: "vs", Gloss: "to study", Info: ""},
			},
			Components: []JSONToken{
				{
					Surface: "勉強",
					Reading: "べんきょう",
					Kana:    "べんきょう",
					Romaji:  "benkyou",
					Gloss: []Gloss{
						{Pos: "n", Gloss: "study", Info: ""},
					},
				},
				{
					Surface: "して",
					Reading: "して",
					Kana:    "して",
					Romaji:  "shite",
					Gloss: []Gloss{
						{Pos: "vs", Gloss: "to do", Info: ""},
					},
				},
			},
		},
		&JSONToken{
			Surface:   "います",
			IsLexical: true,
			Reading:   "います",
			Kana:      "います",
			Romaji:    "imasu",
			Score:     100,
			Gloss: []Gloss{
				{Pos: "v", Gloss: "to be", Info: ""},
				{Pos: "aux", Gloss: "progressive", Info: ""},
			},
		},
		&JSONToken{
			Surface:   "。",
			IsLexical: false,
			Reading:   "。",
			Kana:      "。",
			Romaji:    ".",
			Score:     0,
		},
	}
}
