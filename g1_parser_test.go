package ichiran

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// 7.2.1  Structural fields on JSONToken
// ---------------------------------------------------------------------------

func TestParseWordNode_BasicFields(t *testing.T) {
	wordData := map[string]interface{}{
		"type":     "KANJI",
		"text":     "食べる",
		"truetext": "食べる",
		"kana":     "たべる",
		"seq":      float64(1358280),
		"score":    float64(100),
		"start":    float64(0),
		"end":      float64(3),
		"primary":  true,
	}

	token, err := parseWordNode(wordData, "taberu", nil, "")
	require.NoError(t, err)

	assert.Equal(t, "食べる", token.Surface)
	assert.Equal(t, "食べる", token.TrueText)
	assert.Equal(t, "たべる", token.Kana)
	assert.Equal(t, 1358280, token.Seq)
	assert.Equal(t, 100, token.Score)
	assert.Equal(t, "taberu", token.Romaji)
	assert.Equal(t, "KANJI", token.LexicalType)
	assert.True(t, token.IsLexical)

	// Nullable positions: present and non-zero
	require.NotNil(t, token.Start)
	require.NotNil(t, token.End)
	assert.Equal(t, 0, *token.Start)
	assert.Equal(t, 3, *token.End)

	// Primary flag
	require.NotNil(t, token.IsPrimary)
	assert.True(t, *token.IsPrimary)
}

func TestParseWordNode_NullablePositionAbsentVsZero(t *testing.T) {
	// When start/end are absent, pointers must be nil — not zero.
	wordData := map[string]interface{}{
		"type": "KANA",
		"text": "は",
		"kana": "は",
		"seq":  float64(0),
	}

	token, err := parseWordNode(wordData, "", nil, "")
	require.NoError(t, err)
	assert.Nil(t, token.Start, "absent start must be nil, not *0")
	assert.Nil(t, token.End, "absent end must be nil, not *0")
}

func TestParseWordNode_InheritedType(t *testing.T) {
	// When wordData has no type, inheritedType is used.
	wordData := map[string]interface{}{
		"text": "食",
		"kana": "た",
		"seq":  float64(123),
	}

	token, err := parseWordNode(wordData, "", nil, "KANJI")
	require.NoError(t, err)
	assert.Equal(t, "KANJI", token.LexicalType)
	assert.True(t, token.IsLexical)
}

// ---------------------------------------------------------------------------
// 7.2.1  Counter data: structural [value, ordinal] and object {value, ordinal}
// ---------------------------------------------------------------------------

func TestParseWordNode_CounterArrayFormat(t *testing.T) {
	wordData := map[string]interface{}{
		"type":    "KANJI",
		"text":    "三つ",
		"kana":    "みっつ",
		"seq":     float64(100),
		"counter": []interface{}{"三つ", true},
	}

	token, err := parseWordNode(wordData, "", nil, "")
	require.NoError(t, err)
	require.NotNil(t, token.CounterData)
	assert.Equal(t, "三つ", token.CounterData.Value)
	assert.True(t, token.CounterData.Ordinal)
}

func TestParseWordNode_CounterObjectFormat(t *testing.T) {
	wordData := map[string]interface{}{
		"type": "KANJI",
		"text": "三つ",
		"kana": "みっつ",
		"seq":  float64(100),
		"counter": map[string]interface{}{
			"value":   "三つ",
			"ordinal": false,
		},
	}

	token, err := parseWordNode(wordData, "", nil, "")
	require.NoError(t, err)
	require.NotNil(t, token.CounterData)
	assert.Equal(t, "三つ", token.CounterData.Value)
	assert.False(t, token.CounterData.Ordinal)
}

func TestParseWordNode_CounterFromGlossSource(t *testing.T) {
	// Counter absent on structural wordData, present in externalGloss.
	wordData := map[string]interface{}{
		"type": "KANJI",
		"text": "三つ",
		"kana": "みっつ",
		"seq":  float64(100),
	}
	richGloss := map[string]interface{}{
		"text": "三つ",
		"kana": "みっつ",
		"seq":  float64(100),
		"counter": map[string]interface{}{
			"value":   "3",
			"ordinal": false,
		},
	}

	token, err := parseWordNode(wordData, "", richGloss, "")
	require.NoError(t, err)
	require.NotNil(t, token.CounterData)
	assert.Equal(t, "3", token.CounterData.Value)
}

// ---------------------------------------------------------------------------
// 7.2.1  ConjSelector: "ROOT" string and array of IDs
// ---------------------------------------------------------------------------

func TestParseWordNode_ConjSelectorRoot(t *testing.T) {
	wordData := map[string]interface{}{
		"type":         "KANJI",
		"text":         "食べる",
		"kana":         "たべる",
		"seq":          float64(1358280),
		"conjugations": "ROOT",
	}

	token, err := parseWordNode(wordData, "", nil, "")
	require.NoError(t, err)
	require.NotNil(t, token.ConjSelector)
	assert.True(t, token.ConjSelector.IsRoot)
	assert.Empty(t, token.ConjSelector.IDs)
}

func TestParseWordNode_ConjSelectorIDs(t *testing.T) {
	wordData := map[string]interface{}{
		"type":         "KANJI",
		"text":         "食べた",
		"kana":         "たべた",
		"seq":          float64(1358280),
		"conjugations": []interface{}{float64(5), float64(12)},
	}

	token, err := parseWordNode(wordData, "", nil, "")
	require.NoError(t, err)
	require.NotNil(t, token.ConjSelector)
	assert.False(t, token.ConjSelector.IsRoot)
	assert.Equal(t, []int{5, 12}, token.ConjSelector.IDs)
}

// ---------------------------------------------------------------------------
// 7.2.3  Gloss.conj is authoritative; legacy fallback only when key absent
// ---------------------------------------------------------------------------

func TestParseWordNode_GlossConjAuthoritative(t *testing.T) {
	// When gloss has a "conj" key (even empty), top-level conj must not be used.
	wordData := map[string]interface{}{
		"type":  "KANJI",
		"text":  "食べた",
		"kana":  "たべた",
		"seq":   float64(1358280),
		"score": float64(100),
		// Legacy top-level conj — should be ignored because gloss has "conj".
		"conj": []interface{}{
			map[string]interface{}{
				"reading": "LEGACY",
			},
		},
		"gloss": map[string]interface{}{
			"reading": "食べた 【たべた】",
			"conj":    []interface{}{}, // Explicitly empty — authoritative.
		},
	}

	token, err := parseWordNode(wordData, "", nil, "")
	require.NoError(t, err)
	assert.Empty(t, token.Conj, "explicitly empty gloss.conj must not resurrect legacy conj")
}

func TestParseWordNode_LegacyConjFallback(t *testing.T) {
	// When gloss has NO "conj" key at all, fall back to top-level.
	wordData := map[string]interface{}{
		"type":  "KANJI",
		"text":  "食べた",
		"kana":  "たべた",
		"seq":   float64(1358280),
		"score": float64(100),
		"conj": []interface{}{
			map[string]interface{}{
				"reading": "fallback",
			},
		},
		"gloss": map[string]interface{}{
			"reading": "食べた 【たべた】",
			// No "conj" key — fallback must be used.
		},
	}

	token, err := parseWordNode(wordData, "", nil, "")
	require.NoError(t, err)
	require.Len(t, token.Conj, 1)
	assert.Equal(t, "fallback", token.Conj[0].Reading)
}

// ---------------------------------------------------------------------------
// 7.2.4  Structural+rich alternative pairing
// ---------------------------------------------------------------------------

func TestParseAlternativeChildren_StructuralPlusRich(t *testing.T) {
	wordData := map[string]interface{}{
		"type":        "KANJI",
		"text":        "止めた",
		"alternative": true,
		"components": []interface{}{
			map[string]interface{}{
				"text":    "止めた",
				"kana":    "とめた",
				"seq":     float64(10370482),
				"type":    "KANJI",
				"primary": true,
			},
			map[string]interface{}{
				"text":    "止めた",
				"kana":    "やめた",
				"seq":     float64(10841006),
				"type":    "KANJI",
				"primary": false,
			},
		},
	}
	glossSource := map[string]interface{}{
		"alternative": []interface{}{
			map[string]interface{}{
				"text":    "止めた",
				"kana":    "とめた",
				"seq":     float64(10370482),
				"reading": "止めた 【とめた】",
				"gloss":   []interface{}{map[string]interface{}{"pos": "v1", "gloss": "to stop"}},
			},
			map[string]interface{}{
				"text":    "止めた",
				"kana":    "やめた",
				"seq":     float64(10841006),
				"reading": "止めた 【やめた】",
				"gloss":   []interface{}{map[string]interface{}{"pos": "v1", "gloss": "to quit"}},
			},
		},
	}

	alts, err := parseAlternativeChildren(wordData, glossSource, "KANJI")
	require.NoError(t, err)
	require.Len(t, alts, 2)

	// Structural fields preserved
	assert.Equal(t, "とめた", alts[0].Kana)
	assert.Equal(t, 10370482, alts[0].Seq)
	require.NotNil(t, alts[0].IsPrimary)
	assert.True(t, *alts[0].IsPrimary)

	assert.Equal(t, "やめた", alts[1].Kana)
	assert.Equal(t, 10841006, alts[1].Seq)
	require.NotNil(t, alts[1].IsPrimary)
	assert.False(t, *alts[1].IsPrimary)

	// Rich gloss data enriches reading and glosses
	assert.Equal(t, "止めた 【とめた】", alts[0].Reading)
	require.Len(t, alts[0].Gloss, 1)
	assert.Equal(t, "to stop", alts[0].Gloss[0].Gloss)

	assert.Equal(t, "止めた 【やめた】", alts[1].Reading)
	require.Len(t, alts[1].Gloss, 1)
	assert.Equal(t, "to quit", alts[1].Gloss[0].Gloss)
}

func TestParseAlternativeChildren_RichOnlyFallback(t *testing.T) {
	// No structural components — falls back to parsing rich alternatives directly.
	wordData := map[string]interface{}{
		"type":        "KANJI",
		"text":        "止めた",
		"alternative": true,
		// No "components" key.
	}
	glossSource := map[string]interface{}{
		"alternative": []interface{}{
			map[string]interface{}{
				"text":    "止めた",
				"kana":    "とめた",
				"seq":     float64(10370482),
				"reading": "止めた 【とめた】",
			},
		},
	}

	alts, err := parseAlternativeChildren(wordData, glossSource, "KANJI")
	require.NoError(t, err)
	require.Len(t, alts, 1)
	assert.Equal(t, "とめた", alts[0].Kana)
	assert.Equal(t, "KANJI", alts[0].LexicalType, "inherited type from parent")
	assert.Equal(t, "止めた 【とめた】", alts[0].Reading, "flat-schema reading must be preserved")
}

func TestParseAlternativeChildren_CountMismatchError(t *testing.T) {
	wordData := map[string]interface{}{
		"alternative": true,
		"components": []interface{}{
			map[string]interface{}{"text": "a", "kana": "a", "seq": float64(1)},
		},
	}
	glossSource := map[string]interface{}{
		"alternative": []interface{}{
			map[string]interface{}{"text": "a", "kana": "a", "seq": float64(1)},
			map[string]interface{}{"text": "b", "kana": "b", "seq": float64(2)},
		},
	}

	_, err := parseAlternativeChildren(wordData, glossSource, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "count mismatch")
}

// ---------------------------------------------------------------------------
// 7.2.4  correspondenceValid
// ---------------------------------------------------------------------------

func TestCorrespondenceValid(t *testing.T) {
	t.Run("agree", func(t *testing.T) {
		s := map[string]interface{}{"text": "食", "kana": "た", "seq": float64(100)}
		r := map[string]interface{}{"text": "食", "kana": "た", "seq": float64(100)}
		assert.True(t, correspondenceValid(s, r))
	})
	t.Run("text disagree", func(t *testing.T) {
		s := map[string]interface{}{"text": "食", "seq": float64(100)}
		r := map[string]interface{}{"text": "飲", "seq": float64(100)}
		assert.False(t, correspondenceValid(s, r))
	})
	t.Run("kana disagree", func(t *testing.T) {
		s := map[string]interface{}{"text": "食", "kana": "た"}
		r := map[string]interface{}{"text": "食", "kana": "の"}
		assert.False(t, correspondenceValid(s, r))
	})
	t.Run("seq disagree", func(t *testing.T) {
		s := map[string]interface{}{"text": "食", "seq": float64(100)}
		r := map[string]interface{}{"text": "食", "seq": float64(200)}
		assert.False(t, correspondenceValid(s, r))
	})
	t.Run("one side missing fields is ok", func(t *testing.T) {
		s := map[string]interface{}{"text": "食"}
		r := map[string]interface{}{"text": "食", "kana": "た", "seq": float64(100)}
		assert.True(t, correspondenceValid(s, r))
	})
}

func TestParseWordNode_CorrespondenceMismatchReturnsError(t *testing.T) {
	wordData := map[string]interface{}{
		"type": "KANJI",
		"text": "食",
		"kana": "た",
		"seq":  float64(100),
	}
	badGloss := map[string]interface{}{
		"text": "飲", // different text
		"kana": "の",
		"seq":  float64(200),
	}

	_, err := parseWordNode(wordData, "", badGloss, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "correspondence mismatch")
}

// ---------------------------------------------------------------------------
// 7.2.5  selectCandidate deep-copies — no shared mutable storage
// ---------------------------------------------------------------------------

func TestSelectCandidate_DeepCopy(t *testing.T) {
	src := &JSONToken{
		Kana:    "たべる",
		Reading: "食べる 【たべる】",
		Seq:     1358280,
		Gloss:   []Gloss{{Pos: "v1", Gloss: "to eat"}},
		Conj: []Conj{
			{
				Reading: "base",
				Prop:    []Prop{{Pos: "v1", Type: "Past"}},
				Via: []Conj{
					{Reading: "via-leaf", Prop: []Prop{{Pos: "v1", Type: "Te-form"}}},
				},
			},
		},
		Components: []JSONToken{
			{Surface: "child", Kana: "こ"},
		},
		KanjiReadings: []KanjiReading{{Kanji: "食", Reading: "た"}},
		ConjSelector:  &ConjSelector{IDs: []int{5, 12}},
		IsPrimary:     boolPtr(true),
		CounterData:   &CounterData{Value: "3", Ordinal: true},
	}

	target := &JSONToken{Surface: "parent", Alternative: []JSONToken{{Surface: "alt"}}}
	target.selectCandidate(src)

	// Surface and Alternative are NOT copied.
	assert.Equal(t, "parent", target.Surface)
	require.Len(t, target.Alternative, 1)

	// Fields are copied.
	assert.Equal(t, "たべる", target.Kana)
	assert.Equal(t, 1358280, target.Seq)

	// Mutate target's copied slices and pointers — src must be unaffected.
	target.Gloss[0].Gloss = "MUTATED"
	assert.Equal(t, "to eat", src.Gloss[0].Gloss, "Gloss slice must be independent")

	target.Conj[0].Reading = "MUTATED"
	assert.Equal(t, "base", src.Conj[0].Reading, "Conj slice must be independent")

	target.Conj[0].Via[0].Reading = "MUTATED"
	assert.Equal(t, "via-leaf", src.Conj[0].Via[0].Reading, "Via chain must be independent")

	target.Conj[0].Prop[0].Type = "MUTATED"
	assert.Equal(t, "Past", src.Conj[0].Prop[0].Type, "Prop slice must be independent")

	target.Components[0].Surface = "MUTATED"
	assert.Equal(t, "child", src.Components[0].Surface, "Components must be independent")

	target.KanjiReadings[0].Kanji = "MUTATED"
	assert.Equal(t, "食", src.KanjiReadings[0].Kanji, "KanjiReadings must be independent")

	target.ConjSelector.IDs[0] = 999
	assert.Equal(t, 5, src.ConjSelector.IDs[0], "ConjSelector.IDs must be independent")

	*target.IsPrimary = false
	assert.True(t, *src.IsPrimary, "IsPrimary pointer must be independent")

	target.CounterData.Value = "MUTATED"
	assert.Equal(t, "3", src.CounterData.Value, "CounterData must be independent")
}

// ---------------------------------------------------------------------------
// 7.2.5  selectCandidate clears previous candidate's values
// ---------------------------------------------------------------------------

func TestSelectCandidate_ClearsPreviousValues(t *testing.T) {
	// Candidate A has rich metadata.
	candidateA := &JSONToken{
		Kana:          "たべた",
		Reading:       "食べる 【たべる】",
		Seq:           1358280,
		Gloss:         []Gloss{{Pos: "v1", Gloss: "to eat"}},
		Conj:          []Conj{{Reading: "食べる"}},
		KanjiReadings: []KanjiReading{{Kanji: "食", Reading: "た"}},
		ConjSelector:  &ConjSelector{IDs: []int{5}},
		IsPrimary:     boolPtr(true),
		CounterData:   &CounterData{Value: "3", Ordinal: true},
		LexicalType:   "KANJI",
	}

	// Candidate B lacks optional metadata.
	candidateB := &JSONToken{
		Kana:    "のんだ",
		Reading: "飲む 【のむ】",
		Seq:     9999,
		Gloss:   []Gloss{{Pos: "v5", Gloss: "to drink"}},
		// No Conj, no KanjiReadings, no ConjSelector, no IsPrimary,
		// no CounterData — these must be CLEARED on the target.
	}

	target := &JSONToken{Surface: "X"}
	target.selectCandidate(candidateA)

	// Verify A's metadata is present.
	require.NotNil(t, target.ConjSelector)
	require.NotNil(t, target.IsPrimary)
	require.NotNil(t, target.CounterData)
	require.Len(t, target.Conj, 1)
	require.Len(t, target.KanjiReadings, 1)
	assert.Equal(t, "KANJI", target.LexicalType)

	// Now select B — A's optional fields must be cleared, not inherited.
	target.selectCandidate(candidateB)

	assert.Equal(t, "のんだ", target.Kana)
	assert.Equal(t, 9999, target.Seq)
	assert.Equal(t, "飲む 【のむ】", target.Reading)
	require.Len(t, target.Gloss, 1)
	assert.Equal(t, "to drink", target.Gloss[0].Gloss)

	assert.Nil(t, target.ConjSelector, "ConjSelector must be cleared")
	assert.Nil(t, target.IsPrimary, "IsPrimary must be cleared")
	assert.Nil(t, target.CounterData, "CounterData must be cleared")
	assert.Empty(t, target.Conj, "Conj must be cleared")
	assert.Empty(t, target.KanjiReadings, "KanjiReadings must be cleared")
	assert.Equal(t, "", target.LexicalType, "LexicalType must be cleared")
}

// ---------------------------------------------------------------------------
// 7.2.7  Compound children with rich gloss enrichment
// ---------------------------------------------------------------------------

func TestParseWordNode_CompoundWithRichGloss(t *testing.T) {
	wordData := map[string]interface{}{
		"type":  "KANJI",
		"text":  "日本語",
		"kana":  "にほんご",
		"seq":   float64(1585900),
		"score": float64(100),
		"components": []interface{}{
			map[string]interface{}{
				"text": "日本",
				"kana": "にほん",
				"seq":  float64(1585080),
				"type": "KANJI",
			},
			map[string]interface{}{
				"text": "語",
				"kana": "ご",
				"seq":  float64(1289420),
				"type": "KANJI",
			},
		},
		"gloss": map[string]interface{}{
			"reading": "日本語 【にほんご】",
			"gloss":   []interface{}{map[string]interface{}{"pos": "n", "gloss": "Japanese language"}},
			"components": []interface{}{
				map[string]interface{}{
					"text":    "日本",
					"kana":    "にほん",
					"seq":     float64(1585080),
					"reading": "日本 【にほん】",
					"gloss":   []interface{}{map[string]interface{}{"pos": "n", "gloss": "Japan"}},
				},
				map[string]interface{}{
					"text":    "語",
					"kana":    "ご",
					"seq":     float64(1289420),
					"reading": "語 【ご】",
					"gloss":   []interface{}{map[string]interface{}{"pos": "n", "gloss": "language"}},
				},
			},
		},
	}

	token, err := parseWordNode(wordData, "nihongo", nil, "")
	require.NoError(t, err)

	// Parent token
	assert.Equal(t, "日本語", token.Surface)
	assert.Equal(t, "nihongo", token.Romaji)

	// Compound children
	require.Len(t, token.Components, 2)

	child0 := token.Components[0]
	assert.Equal(t, "日本", child0.Surface)
	assert.Equal(t, "にほん", child0.Kana)
	assert.Equal(t, 1585080, child0.Seq)
	assert.Equal(t, "日本 【にほん】", child0.Reading)
	require.Len(t, child0.Gloss, 1)
	assert.Equal(t, "Japan", child0.Gloss[0].Gloss)

	child1 := token.Components[1]
	assert.Equal(t, "語", child1.Surface)
	assert.Equal(t, "ご", child1.Kana)
	assert.Equal(t, "language", child1.Gloss[0].Gloss)
}

// ---------------------------------------------------------------------------
// 7.2.8  Whitespace strings are content, not parse failures
// ---------------------------------------------------------------------------

func TestParseWordEntriesToTokens_PreservesWhitespaceStrings(t *testing.T) {
	entries := []interface{}{
		" ",  // whitespace-only string
		"、", // punctuation
	}

	tokens, err := parseWordEntriesToTokens(entries)
	require.NoError(t, err)
	require.Len(t, tokens, 2, "both strings must become tokens")
	assert.Equal(t, " ", tokens[0].Surface)
	assert.False(t, tokens[0].IsLexical)
	assert.Equal(t, "、", tokens[1].Surface)
	assert.False(t, tokens[1].IsLexical)
}

// ---------------------------------------------------------------------------
// 7.2.9  Source preservation — no double JSON-unescaping
// ---------------------------------------------------------------------------

func TestSourcePreservation_NoDoubleUnescape(t *testing.T) {
	// Simulate what json.Unmarshal produces: a literal backslash-u sequence
	// in the Go string means the JSON had \\u (escaped backslash + u).
	// The parser must NOT re-decode this into a Unicode character.
	wordData := map[string]interface{}{
		"type":  "KANA",
		"text":  `test\u0041`, // literal backslash + u + 0041 in the Go string
		"kana":  "てすと",
		"seq":   float64(0),
		"score": float64(0),
	}

	token, err := parseWordNode(wordData, "", nil, "")
	require.NoError(t, err)
	assert.Equal(t, `test\u0041`, token.Surface,
		"literal \\u sequence must be preserved, not re-decoded to 'A'")
}

func TestSourcePreservation_ZWNJPreservedInKana(t *testing.T) {
	// ZWNJ (\u200c) in Kana must be preserved on the token, not stripped.
	kanaWithZWNJ := "た\u200cべる"
	wordData := map[string]interface{}{
		"type":  "KANA",
		"text":  "たべる",
		"kana":  kanaWithZWNJ,
		"seq":   float64(0),
		"score": float64(0),
	}

	token, err := parseWordNode(wordData, "", nil, "")
	require.NoError(t, err)
	assert.Equal(t, kanaWithZWNJ, token.Kana,
		"ZWNJ must be preserved in stored Kana field")
	// But romaji derivation strips ZWNJ before transliterating.
	assert.NotContains(t, token.Romaji, "\u200c",
		"derived romaji must not contain ZWNJ")
}

// ---------------------------------------------------------------------------
// 7.2.9  escapeLispString
// ---------------------------------------------------------------------------

func TestEscapeLispString(t *testing.T) {
	tests := []struct {
		name, input, expected string
	}{
		{"plain text", "hello", "hello"},
		{"backslash", `a\b`, `a\\b`},
		{"double quote", `say "hi"`, `say \"hi\"`},
		{"both", `"path\to"`, `\"path\\to\"`},
		{"semicolons survive", "a;b;c", "a;b;c"},
		{"newlines survive", "a\nb", "a\nb"},
		{"tabs survive", "a\tb", "a\tb"},
		{"apostrophes survive", "it's", "it's"},
		{"leading hyphen survives", "-e", "-e"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, escapeLispString(tt.input))
		})
	}
}

// ---------------------------------------------------------------------------
// 7.2.6  Per-candidate romaji — no cross-reading contamination
// ---------------------------------------------------------------------------

func TestParseWordEntry_RomajiOnlyDistributedWhenCountsMatch(t *testing.T) {
	// Parent has 3 slash-parts but only 2 alternatives — romaji should NOT
	// be distributed (counts disagree).
	word := []interface{}{
		"a/b/c",
		map[string]interface{}{
			"type":        "KANJI",
			"text":        "止めた",
			"kana":        []interface{}{"とめた", "やめた"},
			"seq":         []interface{}{float64(1), float64(2)},
			"score":       float64(100),
			"alternative": true,
			"gloss": map[string]interface{}{
				"alternative": []interface{}{
					map[string]interface{}{"text": "止めた", "kana": "とめた", "seq": float64(1)},
					map[string]interface{}{"text": "止めた", "kana": "やめた", "seq": float64(2)},
				},
			},
		},
		[]interface{}{},
	}

	token, err := parseWordEntry(word)
	require.NoError(t, err)
	require.Len(t, token.Alternative, 2)
	// Romaji NOT distributed because 3 parts ≠ 2 alternatives.
	// Each alternative gets repairRomajiFromKana instead.
	assert.NotEqual(t, "a", token.Alternative[0].Romaji)
	assert.NotEqual(t, "b", token.Alternative[1].Romaji)
}

// ---------------------------------------------------------------------------
// 7.2.6  Per-candidate match data belongs to that candidate
// ---------------------------------------------------------------------------

func TestParseWordNode_PerCandidateMatchData(t *testing.T) {
	wordData := map[string]interface{}{
		"type":        "KANJI",
		"text":        "明日",
		"kana":        []interface{}{"あした", "あす"},
		"seq":         []interface{}{float64(1), float64(2)},
		"alternative": true,
		"gloss": map[string]interface{}{
			"alternative": []interface{}{
				map[string]interface{}{
					"text": "明日",
					"kana": "あした",
					"seq":  float64(1),
					"match": []interface{}{
						map[string]interface{}{"kanji": "明", "reading": "あ"},
					},
				},
				map[string]interface{}{
					"text": "明日",
					"kana": "あす",
					"seq":  float64(2),
					"match": []interface{}{
						map[string]interface{}{"kanji": "明", "reading": "あ"},
						map[string]interface{}{"kanji": "日", "reading": "す"},
					},
				},
			},
		},
	}

	token, err := parseWordNode(wordData, "", nil, "")
	require.NoError(t, err)
	require.Len(t, token.Alternative, 2)

	// Each candidate has its OWN match data, not the other's.
	assert.Len(t, token.Alternative[0].KanjiReadings, 1)
	assert.Len(t, token.Alternative[1].KanjiReadings, 2)
}

// ---------------------------------------------------------------------------
// 7.2.7  Nested alternatives within compound children
// ---------------------------------------------------------------------------

func TestParseWordNode_NestedAlternativeInCompound(t *testing.T) {
	wordData := map[string]interface{}{
		"type": "KANJI",
		"text": "大食い",
		"kana": "おおぐい",
		"seq":  float64(999),
		"components": []interface{}{
			// First component is a simple child
			map[string]interface{}{
				"text": "大",
				"kana": "おお",
				"seq":  float64(100),
				"type": "KANJI",
			},
			// Second component has nested alternatives
			map[string]interface{}{
				"text":        "食い",
				"kana":        []interface{}{"くい", "ぐい"},
				"seq":         []interface{}{float64(200), float64(201)},
				"type":        "KANJI",
				"alternative": true,
				"gloss": map[string]interface{}{
					"alternative": []interface{}{
						map[string]interface{}{"text": "食い", "kana": "くい", "seq": float64(200)},
						map[string]interface{}{"text": "食い", "kana": "ぐい", "seq": float64(201)},
					},
				},
			},
		},
		"gloss": map[string]interface{}{
			"reading": "大食い 【おおぐい】",
		},
	}

	token, err := parseWordNode(wordData, "", nil, "")
	require.NoError(t, err)
	require.Len(t, token.Components, 2)

	// Second component has alternatives
	child1 := token.Components[1]
	assert.Equal(t, "食い", child1.Surface)
	require.Len(t, child1.Alternative, 2)
	assert.Equal(t, "くい", child1.Alternative[0].Kana)
	assert.Equal(t, "ぐい", child1.Alternative[1].Kana)

	// Candidate 0 selected onto the child
	assert.Equal(t, "くい", child1.Kana)
	assert.Equal(t, 200, child1.Seq)
}

// ===========================================================================
// JSON fixture tests — full parsing path from raw JSON
// ===========================================================================

// ---------------------------------------------------------------------------
// Fixture 1: 食べた — Seq, conjugation selector, populated gloss.conj
// ---------------------------------------------------------------------------

func TestFixture_Tabeta_ConjugationPreservation(t *testing.T) {
	// Realistic ichiran output for 食べた (past tense of 食べる)
	fixture := []interface{}{
		[]interface{}{
			[]interface{}{
				[]interface{}{
					"tabeta",
					map[string]interface{}{
						"type":         "KANJI",
						"text":         "食べた",
						"kana":         "たべた",
						"seq":          float64(1358280),
						"score":        float64(252),
						"start":        float64(0),
						"end":          float64(3),
						"conjugations": []interface{}{float64(5)},
						"gloss": map[string]interface{}{
							"reading": "食べる 【たべる】",
							"gloss":   []interface{}{map[string]interface{}{"pos": "v1", "gloss": "to eat", "info": ""}},
							"conj": []interface{}{
								map[string]interface{}{
									"prop": []interface{}{
										map[string]interface{}{
											"pos":  "v1",
											"type": "Past (~ta)",
											"neg":  false,
											"fml":  false,
										},
									},
									"reading": "食べる 【たべる】",
									"readok":  true,
									"gloss":   []interface{}{map[string]interface{}{"pos": "v1", "gloss": "to eat", "info": ""}},
								},
							},
							"match": []interface{}{
								map[string]interface{}{"kanji": "食", "reading": "た", "type": "ja_kun"},
							},
						},
					},
					[]interface{}{},
				},
			},
			float64(252),
		},
	}

	raw, err := json.Marshal(fixture)
	require.NoError(t, err)

	tokens, parseErr := parseAnalysis(raw)
	require.NoError(t, parseErr)
	require.NotNil(t, tokens)
	require.Len(t, *tokens, 1)

	tok := (*tokens)[0]

	// Structural Seq
	assert.Equal(t, 1358280, tok.Seq)

	// Conjugation selector
	require.NotNil(t, tok.ConjSelector, "ConjSelector must be preserved")
	assert.Equal(t, []int{5}, tok.ConjSelector.IDs)

	// Populated gloss.conj
	require.Len(t, tok.Conj, 1, "gloss.conj must be preserved")
	assert.Equal(t, "食べる 【たべる】", tok.Conj[0].Reading)
	assert.True(t, tok.Conj[0].ReadOk)
	require.Len(t, tok.Conj[0].Prop, 1)
	assert.Equal(t, "Past (~ta)", tok.Conj[0].Prop[0].Type)
	assert.False(t, tok.Conj[0].Prop[0].Neg)
	require.Len(t, tok.Conj[0].Gloss, 1)
	assert.Equal(t, "to eat", tok.Conj[0].Gloss[0].Gloss)

	// Reading and gloss from gloss source
	assert.Equal(t, "食べる 【たべる】", tok.Reading)
	require.Len(t, tok.Gloss, 1)
	assert.Equal(t, "to eat", tok.Gloss[0].Gloss)

	// Kanji readings from match data
	require.Len(t, tok.KanjiReadings, 1)
	assert.Equal(t, "食", tok.KanjiReadings[0].Kanji)
	assert.Equal(t, "た", tok.KanjiReadings[0].Reading)
}

// ---------------------------------------------------------------------------
// Fixture 2: Recursive via — dictionary data only at deepest leaf
//
// 食べさせられた (causative-passive-past of 食べる)
// conj chain: Past → Passive → Causative → (leaf has reading/gloss)
// ---------------------------------------------------------------------------

func TestFixture_RecursiveVia_LeafDictionary(t *testing.T) {
	fixture := []interface{}{
		[]interface{}{
			[]interface{}{
				[]interface{}{
					"tabesaserareta",
					map[string]interface{}{
						"type":         "KANJI",
						"text":         "食べさせられた",
						"kana":         "たべさせられた",
						"seq":          float64(1358280),
						"score":        float64(100),
						"conjugations": []interface{}{float64(5), float64(12), float64(7)},
						"gloss": map[string]interface{}{
							"reading": "食べる 【たべる】",
							"conj": []interface{}{
								map[string]interface{}{
									"prop": []interface{}{
										map[string]interface{}{"pos": "v1", "type": "Past (~ta)", "neg": false},
									},
									"reading": "",
									"readok":  false,
									// No gloss at this level — only at the leaf.
									"via": []interface{}{
										map[string]interface{}{
											"prop": []interface{}{
												map[string]interface{}{"pos": "v1", "type": "Passive", "neg": false},
											},
											"reading": "",
											"readok":  false,
											"via": []interface{}{
												map[string]interface{}{
													"prop": []interface{}{
														map[string]interface{}{"pos": "v1", "type": "Causative", "neg": false},
													},
													"reading": "食べる 【たべる】",
													"readok":  true,
													"gloss": []interface{}{
														map[string]interface{}{"pos": "v1", "gloss": "to eat", "info": ""},
													},
													// No further via — this is the root.
												},
											},
										},
									},
								},
							},
						},
					},
					[]interface{}{},
				},
			},
			float64(100),
		},
	}

	raw, err := json.Marshal(fixture)
	require.NoError(t, err)

	tokens, parseErr := parseAnalysis(raw)
	require.NoError(t, parseErr)
	require.NotNil(t, tokens)
	require.Len(t, *tokens, 1)

	tok := (*tokens)[0]

	// Top-level conj
	require.Len(t, tok.Conj, 1)
	topConj := tok.Conj[0]
	assert.Equal(t, "Past (~ta)", topConj.Prop[0].Type)
	assert.Empty(t, topConj.Reading)
	assert.False(t, topConj.ReadOk)
	assert.Empty(t, topConj.Gloss, "no gloss at this level")

	// First via level: Passive
	require.Len(t, topConj.Via, 1)
	passive := topConj.Via[0]
	assert.Equal(t, "Passive", passive.Prop[0].Type)
	assert.Empty(t, passive.Gloss, "no gloss at Passive level")

	// Second via level (leaf): Causative — has the dictionary data
	require.Len(t, passive.Via, 1)
	causative := passive.Via[0]
	assert.Equal(t, "Causative", causative.Prop[0].Type)
	assert.Equal(t, "食べる 【たべる】", causative.Reading)
	assert.True(t, causative.ReadOk)
	require.Len(t, causative.Gloss, 1, "dictionary gloss must be at leaf")
	assert.Equal(t, "to eat", causative.Gloss[0].Gloss)
	assert.Empty(t, causative.Via, "no further via beyond the root")
}

// ---------------------------------------------------------------------------
// Fixture 3: 止めた — separate candidate IDs, readings, glosses
// ---------------------------------------------------------------------------

func TestFixture_Tometa_SeparateCandidates(t *testing.T) {
	fixture := []interface{}{
		[]interface{}{
			[]interface{}{
				[]interface{}{
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
									"seq":     float64(10370482),
									"reading": "止める 【とめる】",
									"gloss":   []interface{}{map[string]interface{}{"pos": "v1", "gloss": "to stop"}},
									"conj": []interface{}{
										map[string]interface{}{
											"reading": "止める 【とめる】",
											"readok":  true,
											"prop": []interface{}{
												map[string]interface{}{"pos": "v1", "type": "Past (~ta)", "neg": false},
											},
											"gloss": []interface{}{map[string]interface{}{"pos": "v1", "gloss": "to stop"}},
										},
									},
								},
								map[string]interface{}{
									"text":    "止めた",
									"kana":    "やめた",
									"seq":     float64(10841006),
									"reading": "止める 【やめる】",
									"gloss":   []interface{}{map[string]interface{}{"pos": "v1", "gloss": "to quit"}},
									"conj": []interface{}{
										map[string]interface{}{
											"reading": "止める 【やめる】",
											"readok":  true,
											"prop": []interface{}{
												map[string]interface{}{"pos": "v1", "type": "Past (~ta)", "neg": false},
											},
											"gloss": []interface{}{map[string]interface{}{"pos": "v1", "gloss": "to quit"}},
										},
									},
								},
							},
						},
					},
					[]interface{}{},
				},
			},
			float64(336),
		},
	}

	raw, err := json.Marshal(fixture)
	require.NoError(t, err)

	tokens, parseErr := parseAnalysis(raw)
	require.NoError(t, parseErr)
	require.NotNil(t, tokens)
	require.Len(t, *tokens, 1)

	tok := (*tokens)[0]
	assert.Equal(t, "止めた", tok.Surface)

	// Alternatives preserved with separate metadata
	require.Len(t, tok.Alternative, 2)

	alt0 := tok.Alternative[0]
	assert.Equal(t, "とめた", alt0.Kana)
	assert.Equal(t, 10370482, alt0.Seq)
	assert.Equal(t, "止める 【とめる】", alt0.Reading)
	require.Len(t, alt0.Gloss, 1)
	assert.Equal(t, "to stop", alt0.Gloss[0].Gloss)
	require.Len(t, alt0.Conj, 1)
	assert.Equal(t, "止める 【とめる】", alt0.Conj[0].Reading)
	assert.Equal(t, "to stop", alt0.Conj[0].Gloss[0].Gloss)

	alt1 := tok.Alternative[1]
	assert.Equal(t, "やめた", alt1.Kana)
	assert.Equal(t, 10841006, alt1.Seq)
	assert.Equal(t, "止める 【やめる】", alt1.Reading)
	require.Len(t, alt1.Gloss, 1)
	assert.Equal(t, "to quit", alt1.Gloss[0].Gloss)
	require.Len(t, alt1.Conj, 1)
	assert.Equal(t, "止める 【やめる】", alt1.Conj[0].Reading)
	assert.Equal(t, "to quit", alt1.Conj[0].Gloss[0].Gloss)

	// Candidate 0 promoted to parent — verify no mixing
	assert.Equal(t, "とめた", tok.Kana)
	assert.Equal(t, 10370482, tok.Seq)
	assert.Equal(t, "止める 【とめる】", tok.Reading)
	require.Len(t, tok.Gloss, 1)
	assert.Equal(t, "to stop", tok.Gloss[0].Gloss, "primary must be 'to stop', not 'to quit'")
}

// ---------------------------------------------------------------------------
// Fixture 4: Malformed entry beside valid entry — full parseAnalysis path
//
// The malformed entry is a word entry with a bad compound child (not a map).
// It passes isFormattedWordEntry but fails inside parseWordNode. This
// exercises the error path through extractWordsArray → parseWordEntriesToTokens.
// ---------------------------------------------------------------------------

func TestFixture_MalformedEntryFullPath(t *testing.T) {
	fixture := []interface{}{
		[]interface{}{
			[]interface{}{
				[]interface{}{
					// Valid word entry
					[]interface{}{
						"watashi",
						map[string]interface{}{
							"type":  "KANJI",
							"text":  "私",
							"kana":  "わたし",
							"seq":   float64(1311110),
							"score": float64(100),
						},
						[]interface{}{},
					},
					// Malformed word entry: has a compound child that is a number
					// instead of a map, which parseWordNode will reject.
					[]interface{}{
						"bad",
						map[string]interface{}{
							"type":       "KANJI",
							"text":       "壊",
							"kana":       "こわ",
							"seq":        float64(999),
							"components": []interface{}{float64(42)},
						},
						[]interface{}{},
					},
				},
				float64(100),
			},
		},
	}

	raw, err := json.Marshal(fixture)
	require.NoError(t, err)

	tokens, parseErr := parseAnalysis(raw)
	// The malformed compound child must propagate an error.
	assert.Error(t, parseErr, "malformed compound child must produce an error")
	assert.Contains(t, parseErr.Error(), "not a map")
	// The valid token must still be returned.
	require.NotNil(t, tokens)
	require.Len(t, *tokens, 1)
	assert.Equal(t, "私", (*tokens)[0].Surface)
}

// ---------------------------------------------------------------------------
// Direct helper error propagation (supplement to Fixture 4)
// ---------------------------------------------------------------------------

func TestParseWordEntriesToTokens_PropagatesErrorAlongsideTokens(t *testing.T) {
	entries := []interface{}{
		// Valid word entry
		[]interface{}{
			"watashi",
			map[string]interface{}{
				"type":  "KANJI",
				"text":  "私",
				"kana":  "わたし",
				"seq":   float64(1311110),
				"score": float64(100),
			},
			[]interface{}{},
		},
		// Malformed entry (not a string or array)
		float64(42),
	}

	tokens, err := parseWordEntriesToTokens(entries)
	assert.Error(t, err, "malformed entry must produce an error")
	assert.Contains(t, err.Error(), "neither string nor array")
	require.Len(t, tokens, 1, "valid token must still be returned")
	assert.Equal(t, "私", tokens[0].Surface)
}

func TestParseWordEntriesToTokens_PropagatesMalformedWordEntry(t *testing.T) {
	entries := []interface{}{
		// Valid entry
		[]interface{}{
			"watashi",
			map[string]interface{}{
				"type":  "KANJI",
				"text":  "私",
				"kana":  "わたし",
				"seq":   float64(1311110),
				"score": float64(100),
			},
			[]interface{}{},
		},
		// Malformed word entry: valid array but too short
		[]interface{}{"only-romaji"},
	}

	tokens, err := parseWordEntriesToTokens(entries)
	assert.Error(t, err, "malformed word entry must produce an error")
	assert.Contains(t, err.Error(), "too short")
	require.Len(t, tokens, 1, "valid token must still be returned")
	assert.Equal(t, "私", tokens[0].Surface)
}

func TestParseWordEntriesToTokens_NoTokensReturnsError(t *testing.T) {
	entries := []interface{}{
		float64(42), // malformed
	}

	tokens, err := parseWordEntriesToTokens(entries)
	assert.Error(t, err)
	assert.Empty(t, tokens)
}
