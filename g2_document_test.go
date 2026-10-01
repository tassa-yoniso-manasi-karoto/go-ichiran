package ichiran

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Unit tests — no Docker required
// ---------------------------------------------------------------------------

func TestParseDocumentResult_VersionCheck(t *testing.T) {
	t.Run("valid version", func(t *testing.T) {
		data := []byte(`{"adapterVersion":1,"fragments":[]}`)
		result, err := parseDocumentResult(data)
		require.NoError(t, err)
		assert.Equal(t, 1, result.AdapterVersion)
	})

	t.Run("missing version", func(t *testing.T) {
		data := []byte(`{"fragments":[]}`)
		_, err := parseDocumentResult(data)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "missing adapterVersion")
	})

	t.Run("unsupported version", func(t *testing.T) {
		data := []byte(`{"adapterVersion":99,"fragments":[]}`)
		_, err := parseDocumentResult(data)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported adapter version")
	})

	t.Run("fractional version rejected", func(t *testing.T) {
		data := []byte(`{"adapterVersion":1.5,"fragments":[]}`)
		_, err := parseDocumentResult(data)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not an integer")
	})

	t.Run("fragments wrong type rejected", func(t *testing.T) {
		data := []byte(`{"adapterVersion":1,"fragments":"not an array"}`)
		_, err := parseDocumentResult(data)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not an array")
	})

	t.Run("invalid JSON", func(t *testing.T) {
		data := []byte(`{not valid json}`)
		_, err := parseDocumentResult(data)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid JSON")
	})
}

func TestParseDocumentResult_Fragments(t *testing.T) {
	t.Run("empty fragments", func(t *testing.T) {
		data := []byte(`{"adapterVersion":1,"fragments":[]}`)
		result, err := parseDocumentResult(data)
		require.NoError(t, err)
		assert.Empty(t, result.Fragments)
	})

	t.Run("null fragments", func(t *testing.T) {
		data := []byte(`{"adapterVersion":1}`)
		result, err := parseDocumentResult(data)
		require.NoError(t, err)
		assert.Empty(t, result.Fragments)
	})

	t.Run("fragment fields", func(t *testing.T) {
		data := []byte(`{"adapterVersion":1,"fragments":[
			{"id":42,"sourceText":"食べた","analysisText":"食べた","segments":[
				{"index":0,"kind":"word","start":0,"end":3,"text":"食べた",
				 "interpretations":[{"score":336,"tokens":[{"text":"食べた","start":0,"end":3}]}]}
			]}
		]}`)
		result, err := parseDocumentResult(data)
		require.NoError(t, err)
		require.Len(t, result.Fragments, 1)
		f := result.Fragments[0]
		assert.Equal(t, 42, f.ID)
		assert.Equal(t, "食べた", f.SourceText)
		assert.Equal(t, "食べた", f.AnalysisText)
		require.Len(t, f.Segments, 1)
	})

	t.Run("non-object fragment rejected", func(t *testing.T) {
		data := []byte(`{"adapterVersion":1,"fragments":["not an object"]}`)
		_, err := parseDocumentResult(data)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not a JSON object")
	})
}

func TestParseSegmentResult_KindValidation(t *testing.T) {
	t.Run("word segment", func(t *testing.T) {
		raw := map[string]interface{}{
			"index":           float64(0),
			"kind":            "word",
			"start":           float64(0),
			"end":             float64(3),
			"text":            "食べた",
			"interpretations": []interface{}{},
		}
		seg, err := parseSegmentResult(raw)
		require.NoError(t, err)
		assert.Equal(t, SegmentWord, seg.Kind)
		assert.Equal(t, 0, seg.Start)
		assert.Equal(t, 3, seg.End)
		assert.Equal(t, "食べた", seg.Text)
	})

	t.Run("literal segment", func(t *testing.T) {
		raw := map[string]interface{}{
			"index": float64(1),
			"kind":  "literal",
			"start": float64(3),
			"end":   float64(4),
			"text":  "。",
		}
		seg, err := parseSegmentResult(raw)
		require.NoError(t, err)
		assert.Equal(t, SegmentLiteral, seg.Kind)
		assert.Empty(t, seg.Interpretations)
	})

	t.Run("unsupported kind rejected", func(t *testing.T) {
		raw := map[string]interface{}{
			"index": float64(0),
			"kind":  "unknown",
			"start": float64(0),
			"end":   float64(1),
			"text":  "x",
		}
		_, err := parseSegmentResult(raw)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported kind")
	})

	t.Run("missing kind rejected", func(t *testing.T) {
		raw := map[string]interface{}{
			"index": float64(0),
			"start": float64(0),
			"end":   float64(1),
			"text":  "x",
		}
		_, err := parseSegmentResult(raw)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "missing kind")
	})

	t.Run("end < start rejected", func(t *testing.T) {
		raw := map[string]interface{}{
			"index": float64(0),
			"kind":  "word",
			"start": float64(5),
			"end":   float64(3),
			"text":  "x",
		}
		_, err := parseSegmentResult(raw)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "end")
	})

	t.Run("non-object rejected", func(t *testing.T) {
		_, err := parseSegmentResult("not an object")
		require.Error(t, err)
	})
}

func TestParseInterpretationResult(t *testing.T) {
	t.Run("basic interpretation", func(t *testing.T) {
		raw := map[string]interface{}{
			"score": float64(336),
			"tokens": []interface{}{
				map[string]interface{}{
					"type": "KANJI",
					"text": "食べた",
					"kana": "たべた",
					"seq":  float64(10091238),
				},
			},
		}
		interp, err := parseInterpretationResult(raw)
		require.NoError(t, err)
		assert.Equal(t, 336, interp.Score)
		require.Len(t, interp.Tokens, 1)
		assert.Equal(t, "食べた", interp.Tokens[0].Surface)
	})

	t.Run("non-object rejected", func(t *testing.T) {
		_, err := parseInterpretationResult("not an object")
		require.Error(t, err)
	})

	t.Run("non-object token rejected", func(t *testing.T) {
		raw := map[string]interface{}{
			"score":  float64(100),
			"tokens": []interface{}{"bad"},
		}
		_, err := parseInterpretationResult(raw)
		require.Error(t, err)
	})
}

func TestParseEnrichedToken_RootCandidates(t *testing.T) {
	tokMap := map[string]interface{}{
		"type":     "KANJI",
		"text":     "食べた",
		"kana":     "たべた",
		"seq":      float64(10091238),
		"romaji":   "tabeta",
		"rootCandidates": []interface{}{
			map[string]interface{}{
				"dictionarySeq": float64(1358280),
				"lemma":         "食べる",
				"kana":          "たべる",
				"gloss": []interface{}{
					map[string]interface{}{
						"pos":   "[v1,vt]",
						"gloss": "to eat",
					},
				},
			},
		},
	}

	tok, err := parseEnrichedToken(tokMap)
	require.NoError(t, err)
	assert.Equal(t, "食べた", tok.Surface)
	assert.Equal(t, "tabeta", tok.Romaji)
	require.Len(t, tok.RootCandidates, 1)
	rc := tok.RootCandidates[0]
	assert.Equal(t, 1358280, rc.DictionarySeq)
	assert.Equal(t, "食べる", rc.Lemma)
	assert.Equal(t, "たべる", rc.Kana)
	require.Len(t, rc.Gloss, 1)
	assert.Equal(t, "to eat", rc.Gloss[0].Gloss)
}

func TestParseEnrichedToken_RecursiveComponents(t *testing.T) {
	// Synthetic parser fixture, not a claim that Ichiran splits 食べ物 this way.
	// The live compound regression below uses the observed 勉強しています output.
	tokMap := map[string]interface{}{
		"type": "KANJI",
		"text": "食べ物",
		"kana": "たべもの",
		"seq":  float64(0),
		"components": []interface{}{
			map[string]interface{}{
				"type": "KANJI",
				"text": "食べ",
				"kana": "たべ",
				"seq":  float64(1358280),
				"rootCandidates": []interface{}{
					map[string]interface{}{
						"dictionarySeq": float64(1358280),
						"lemma":         "食べる",
						"kana":          "たべる",
					},
				},
			},
			map[string]interface{}{
				"type": "KANJI",
				"text": "物",
				"kana": "もの",
				"seq":  float64(1502390),
				"rootCandidates": []interface{}{
					map[string]interface{}{
						"dictionarySeq": float64(1502390),
						"lemma":         "物",
						"kana":          "もの",
					},
				},
			},
		},
	}

	tok, err := parseEnrichedToken(tokMap)
	require.NoError(t, err)
	assert.Equal(t, "食べ物", tok.Surface)
	// Parent has no rootCandidates (it's a compound)
	assert.Empty(t, tok.RootCandidates)
	// Children have their own rootCandidates
	require.Len(t, tok.Components, 2)
	require.Len(t, tok.Components[0].RootCandidates, 1)
	assert.Equal(t, "食べる", tok.Components[0].RootCandidates[0].Lemma)
	require.Len(t, tok.Components[1].RootCandidates, 1)
	assert.Equal(t, "物", tok.Components[1].RootCandidates[0].Lemma)
}

func TestParseEnrichedToken_RecursiveAlternatives(t *testing.T) {
	// An alternative token with two enriched alternatives, each with rootCandidates.
	tokMap := map[string]interface{}{
		"type": "KANJI",
		"text": "止めた",
		"kana": []interface{}{"とめた", "やめた"},
		"seq":  []interface{}{float64(10370482), float64(10841006)},
		"alternative": []interface{}{
			map[string]interface{}{
				"type":   "KANJI",
				"text":   "止めた",
				"kana":   "とめた",
				"seq":    float64(10370482),
				"romaji": "tometa",
				"rootCandidates": []interface{}{
					map[string]interface{}{
						"dictionarySeq": float64(1310670),
						"lemma":         "止める",
						"kana":          "とめる",
					},
				},
			},
			map[string]interface{}{
				"type":   "KANJI",
				"text":   "止めた",
				"kana":   "やめた",
				"seq":    float64(10841006),
				"romaji": "yameta",
				"rootCandidates": []interface{}{
					map[string]interface{}{
						"dictionarySeq": float64(1310640),
						"lemma":         "止む",
						"kana":          "やむ",
					},
				},
			},
		},
	}

	tok, err := parseEnrichedToken(tokMap)
	require.NoError(t, err)
	assert.Equal(t, "止めた", tok.Surface)
	// Parent has no rootCandidates (it has alternatives)
	assert.Empty(t, tok.RootCandidates)
	// Alternatives have their own rootCandidates
	require.Len(t, tok.Alternative, 2)
	assert.Equal(t, "tometa", tok.Alternative[0].Romaji)
	require.Len(t, tok.Alternative[0].RootCandidates, 1)
	assert.Equal(t, "止める", tok.Alternative[0].RootCandidates[0].Lemma)
	assert.Equal(t, "yameta", tok.Alternative[1].Romaji)
	require.Len(t, tok.Alternative[1].RootCandidates, 1)
	assert.Equal(t, "止む", tok.Alternative[1].RootCandidates[0].Lemma)
}

func TestParseRootCandidate(t *testing.T) {
	t.Run("full fields", func(t *testing.T) {
		m := map[string]interface{}{
			"dictionarySeq": float64(1358280),
			"lemma":         "食べる",
			"kana":          "たべる",
			"gloss": []interface{}{
				map[string]interface{}{
					"pos":   "[v1,vt]",
					"gloss": "to eat",
				},
				map[string]interface{}{
					"pos":   "[vt,v1]",
					"gloss": "to live on",
				},
			},
		}
		rc, err := parseRootCandidate(m)
		require.NoError(t, err)
		assert.Equal(t, 1358280, rc.DictionarySeq)
		assert.Equal(t, "食べる", rc.Lemma)
		assert.Equal(t, "たべる", rc.Kana)
		require.Len(t, rc.Gloss, 2)
		assert.Equal(t, "to eat", rc.Gloss[0].Gloss)
	})

	t.Run("no gloss", func(t *testing.T) {
		m := map[string]interface{}{
			"dictionarySeq": float64(12345),
			"lemma":         "何か",
			"kana":          "なにか",
		}
		rc, err := parseRootCandidate(m)
		require.NoError(t, err)
		assert.Equal(t, 12345, rc.DictionarySeq)
		assert.Empty(t, rc.Gloss)
	})

	t.Run("empty map rejected", func(t *testing.T) {
		_, err := parseRootCandidate(map[string]interface{}{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "dictionarySeq")
	})

	t.Run("missing kana rejected", func(t *testing.T) {
		m := map[string]interface{}{
			"dictionarySeq": float64(12345),
			"lemma":         "何か",
		}
		_, err := parseRootCandidate(m)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "kana")
	})
}

func TestValidateFragmentPositions(t *testing.T) {
	t.Run("valid positions", func(t *testing.T) {
		frag := &FragmentResult{
			AnalysisText: "食べた。",
			Segments: []SegmentResult{
				{Index: 0, Start: 0, End: 3, Text: "食べた"},
				{Index: 1, Start: 3, End: 4, Text: "。"},
			},
		}
		err := validateFragmentPositions(frag)
		assert.NoError(t, err)
	})

	t.Run("out of bounds", func(t *testing.T) {
		frag := &FragmentResult{
			AnalysisText: "食べた",
			Segments: []SegmentResult{
				{Index: 0, Start: 0, End: 5, Text: "食べたxx"},
			},
		}
		err := validateFragmentPositions(frag)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid position")
	})

	t.Run("negative start", func(t *testing.T) {
		frag := &FragmentResult{
			AnalysisText: "食べた",
			Segments: []SegmentResult{
				{Index: 0, Start: -1, End: 3, Text: "食べた"},
			},
		}
		err := validateFragmentPositions(frag)
		require.Error(t, err)
	})

	t.Run("text mismatch", func(t *testing.T) {
		frag := &FragmentResult{
			AnalysisText: "食べた",
			Segments: []SegmentResult{
				{Index: 0, Start: 0, End: 3, Text: "飲んだ"},
			},
		}
		err := validateFragmentPositions(frag)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "yields")
	})

	t.Run("empty fragment no segments", func(t *testing.T) {
		frag := &FragmentResult{
			AnalysisText: "",
			Segments:     nil,
		}
		err := validateFragmentPositions(frag)
		assert.NoError(t, err)
	})
}

func TestBuildDocumentLispExpr_ParenBalance(t *testing.T) {
	inputs := []DocumentInput{
		{Fragments: []FragmentInput{{ID: 1, Text: "食べた"}}},
		{Fragments: []FragmentInput{
			{ID: 1, Text: "食べた"},
			{ID: 2, Text: "止めた方がいい"},
		}},
		{Fragments: []FragmentInput{{ID: 1, Text: ""}}},
	}

	for i, input := range inputs {
		expr := buildDocumentLispExpr(input, 5)
		opens := strings.Count(expr, "(")
		closes := strings.Count(expr, ")")
		assert.Equal(t, opens, closes,
			"paren imbalance in input %d: %d opens, %d closes", i, opens, closes)
	}
}

func TestBuildDocumentLispExpr_ContainsExpectedSymbols(t *testing.T) {
	input := DocumentInput{
		Fragments: []FragmentInput{{ID: 1, Text: "食べた"}},
	}
	expr := buildDocumentLispExpr(input, 3)

	// Core Lisp symbols from the adapter
	expectedSymbols := []string{
		"langkit-walk-conj",
		"langkit-verify-root",
		"langkit-resolve-roots",
		"langkit-enrich-word-info",
		"*langkit-root-memo*",
		"ichiran/dict::with-connection",
		"ichiran/dict::strip-hints",
		"ichiran/dict::match-kana-kanji",
		"ichiran/dict::get-senses",
		"ichiran::romanize-word-info",
		"ichiran::normalize",
		"ichiran::basic-split",
		"ichiran/dict::dict-segment",
		"ichiran/dict::root-p",
		"ichiran/dict::nokanji",
		"langkit-root-gloss",
		"adapterVersion",
		"sourceText",
		"analysisText",
	}
	for _, sym := range expectedSymbols {
		assert.Contains(t, expr, sym, "expected Lisp symbol %q not found", sym)
	}
}

func TestBuildDocumentLispExpr_FragmentIDEmbedded(t *testing.T) {
	input := DocumentInput{
		Fragments: []FragmentInput{
			{ID: 42, Text: "テスト"},
			{ID: 99, Text: "確認"},
		},
	}
	expr := buildDocumentLispExpr(input, 1)
	assert.Contains(t, expr, `(42 . "テスト")`)
	assert.Contains(t, expr, `(99 . "確認")`)
}

func TestBuildDocumentLispExpr_LimitEmbedded(t *testing.T) {
	input := DocumentInput{
		Fragments: []FragmentInput{{ID: 1, Text: "テスト"}},
	}
	expr := buildDocumentLispExpr(input, 7)
	assert.Contains(t, expr, "(cdr fragment) 7)")
}

// The per-fragment analysis body must be emitted once, with fragments as
// data.  Emitting it once per fragment made SBCL compile it N times in one
// form and exhaust its heap at about a dozen fragments.
func TestBuildDocumentLispExpr_BodyEmittedOnce(t *testing.T) {
	input := DocumentInput{}
	for i := 0; i < 50; i++ {
		input.Fragments = append(input.Fragments, FragmentInput{ID: i, Text: "テスト"})
	}
	expr := buildDocumentLispExpr(input, 5)
	assert.Equal(t, 1, strings.Count(expr, "ichiran/dict::dict-segment"))
	assert.Equal(t, 1, strings.Count(expr, "ichiran::basic-split"))
}

func TestBuildDocumentLispExpr_EscapesSpecialChars(t *testing.T) {
	input := DocumentInput{
		Fragments: []FragmentInput{{ID: 1, Text: `He said "hello"`}},
	}
	expr := buildDocumentLispExpr(input, 1)
	// The double-quotes inside should be escaped for Lisp string embedding
	assert.Contains(t, expr, `\"hello\"`)
}

func TestParseDocumentResult_FullEnvelope(t *testing.T) {
	// A realistic envelope with word and literal segments, interpretations,
	// tokens with rootCandidates.
	envelope := map[string]interface{}{
		"adapterVersion": float64(1),
		"fragments": []interface{}{
			map[string]interface{}{
				"id":           float64(1),
				"sourceText":   "食べた。",
				"analysisText": "食べた。",
				"segments": []interface{}{
					map[string]interface{}{
						"index": float64(0),
						"kind":  "word",
						"start": float64(0),
						"end":   float64(3),
						"text":  "食べた",
						"interpretations": []interface{}{
							map[string]interface{}{
								"score": float64(336),
								"tokens": []interface{}{
									map[string]interface{}{
										"type":     "KANJI",
										"text":     "食べた",
										"kana":     "たべた",
										"seq":      float64(10091238),
										"romaji":   "tabeta",
										"start":    float64(0),
										"end":      float64(3),
										"rootCandidates": []interface{}{
											map[string]interface{}{
												"dictionarySeq": float64(1358280),
												"lemma":         "食べる",
												"kana":          "たべる",
												"gloss": []interface{}{
													map[string]interface{}{
														"pos":   "[v1,vt]",
														"gloss": "to eat",
													},
												},
											},
										},
									},
								},
							},
						},
					},
					map[string]interface{}{
						"index": float64(1),
						"kind":  "literal",
						"start": float64(3),
						"end":   float64(4),
						"text":  "。",
					},
				},
			},
		},
	}

	data, err := json.Marshal(envelope)
	require.NoError(t, err)

	result, err := parseDocumentResult(data)
	require.NoError(t, err)
	assert.Equal(t, 1, result.AdapterVersion)
	require.Len(t, result.Fragments, 1)

	frag := result.Fragments[0]
	assert.Equal(t, 1, frag.ID)
	assert.Equal(t, "食べた。", frag.SourceText)
	assert.Equal(t, "食べた。", frag.AnalysisText)
	require.Len(t, frag.Segments, 2)

	// Word segment
	wordSeg := frag.Segments[0]
	assert.Equal(t, SegmentWord, wordSeg.Kind)
	assert.Equal(t, 0, wordSeg.Start)
	assert.Equal(t, 3, wordSeg.End)
	require.Len(t, wordSeg.Interpretations, 1)
	require.Len(t, wordSeg.Interpretations[0].Tokens, 1)

	tok := wordSeg.Interpretations[0].Tokens[0]
	assert.Equal(t, "食べた", tok.Surface)
	assert.Equal(t, "tabeta", tok.Romaji)
	require.Len(t, tok.RootCandidates, 1)
	assert.Equal(t, 1358280, tok.RootCandidates[0].DictionarySeq)
	assert.Equal(t, "食べる", tok.RootCandidates[0].Lemma)
	assert.Equal(t, "たべる", tok.RootCandidates[0].Kana)

	// Literal segment
	litSeg := frag.Segments[1]
	assert.Equal(t, SegmentLiteral, litSeg.Kind)
	assert.Equal(t, "。", litSeg.Text)
	assert.Empty(t, litSeg.Interpretations)
}

func TestParseDocumentResult_PositionValidation(t *testing.T) {
	// Build an envelope where the segment text doesn't match the position in analysisText.
	envelope := map[string]interface{}{
		"adapterVersion": float64(1),
		"fragments": []interface{}{
			map[string]interface{}{
				"id":           float64(1),
				"sourceText":   "食べた",
				"analysisText": "食べた",
				"segments": []interface{}{
					map[string]interface{}{
						"index": float64(0),
						"kind":  "word",
						"start": float64(0),
						"end":   float64(3),
						// Deliberately wrong text to trigger validation
						"text": "飲んだ",
						"interpretations": []interface{}{},
					},
				},
			},
		},
	}

	data, err := json.Marshal(envelope)
	require.NoError(t, err)

	_, err = parseDocumentResult(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "yields")
}

// ---------------------------------------------------------------------------
// Live integration tests — require Docker + Ichiran container
// ---------------------------------------------------------------------------

var g2InitOnce sync.Once
var g2InitErr error

// g2EnsureInit initializes the Ichiran Docker container exactly once for all
// G2 live tests. Subsequent calls return the cached result.
func g2EnsureInit(t *testing.T) {
	t.Helper()
	if os.Getenv("ICHIRAN_MANUAL_TEST") != "1" {
		t.Skip("skipping test that requires Docker; set ICHIRAN_MANUAL_TEST=1 to run")
	}
	g2InitOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		g2InitErr = InitWithContext(ctx)
	})
	require.NoError(t, g2InitErr, "Ichiran init failed")
}

func TestAnalyzeDocument_SimpleConjugation(t *testing.T) {
	g2EnsureInit(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	input := DocumentInput{
		Fragments: []FragmentInput{
			{ID: 1, Text: "食べた"},
		},
	}
	opts := DocumentOptions{Limit: 1}

	result, err := AnalyzeDocumentContext(ctx, input, opts)
	require.NoError(t, err)
	assert.Equal(t, 1, result.AdapterVersion)
	require.Len(t, result.Fragments, 1)

	frag := result.Fragments[0]
	assert.Equal(t, 1, frag.ID)
	assert.Equal(t, "食べた", frag.SourceText)
	assert.NotEmpty(t, frag.AnalysisText)

	// At least one word segment
	require.NotEmpty(t, frag.Segments)
	wordSeg := frag.Segments[0]
	assert.Equal(t, SegmentWord, wordSeg.Kind)

	// Token with rootCandidates pointing to 食べる
	require.NotEmpty(t, wordSeg.Interpretations)
	require.NotEmpty(t, wordSeg.Interpretations[0].Tokens)
	tok := wordSeg.Interpretations[0].Tokens[0]
	assert.Equal(t, "食べた", tok.Surface)
	assert.NotEmpty(t, tok.Romaji, "romaji should be populated")

	require.NotEmpty(t, tok.RootCandidates, "rootCandidates should be resolved")
	rc := tok.RootCandidates[0]
	assert.Equal(t, 1358280, rc.DictionarySeq)
	assert.Equal(t, "食べる", rc.Lemma)
	assert.Equal(t, "たべる", rc.Kana)
	assert.NotEmpty(t, rc.Gloss, "root gloss should be populated")
}

func TestAnalyzeDocument_AlternativeReadings(t *testing.T) {
	g2EnsureInit(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	input := DocumentInput{
		Fragments: []FragmentInput{
			{ID: 1, Text: "止めた"},
		},
	}
	opts := DocumentOptions{Limit: 2}

	result, err := AnalyzeDocumentContext(ctx, input, opts)
	require.NoError(t, err)
	require.Len(t, result.Fragments, 1)

	frag := result.Fragments[0]
	require.NotEmpty(t, frag.Segments)

	// Find the word segment for 止めた
	var wordSeg *SegmentResult
	for i := range frag.Segments {
		if frag.Segments[i].Kind == SegmentWord {
			wordSeg = &frag.Segments[i]
			break
		}
	}
	require.NotNil(t, wordSeg, "should have a word segment")
	require.NotEmpty(t, wordSeg.Interpretations)

	tok := wordSeg.Interpretations[0].Tokens[0]

	// 止めた MUST have alternatives — failing to produce them is a defect
	require.NotEmpty(t, tok.Alternative, "止めた should have alternative readings")

	// Alternatives must NOT also appear as compound components (issue 2)
	assert.Empty(t, tok.Components,
		"alternatives should not be duplicated as compound components")

	// Each alternative should have its own romaji and rootCandidates
	for _, alt := range tok.Alternative {
		assert.NotEmpty(t, alt.Romaji,
			"alternative with kana %q should have romaji", alt.Kana)
		require.NotEmpty(t, alt.RootCandidates,
			"alternative with kana %q should have rootCandidates", alt.Kana)
		rc := alt.RootCandidates[0]
		assert.NotZero(t, rc.DictionarySeq)
		assert.NotEmpty(t, rc.Lemma)
		assert.NotEmpty(t, rc.Kana, "root candidate must have kana (not spelling-only)")
	}

	// These are the branches returned by the installed dictionary, not a
	// linguistic assertion that every やめた occurrence means 止む.
	expected := map[string]RootCandidate{
		"とめた": {DictionarySeq: 1310670, Lemma: "止める", Kana: "とめる"},
		"やめた": {DictionarySeq: 1310640, Lemma: "止む", Kana: "やむ"},
	}
	require.Len(t, tok.Alternative, len(expected))
	for _, alt := range tok.Alternative {
		want, ok := expected[alt.Kana]
		require.True(t, ok, "unexpected occurrence reading %q", alt.Kana)
		require.Len(t, alt.RootCandidates, 1)
		assert.Equal(t, want.DictionarySeq, alt.RootCandidates[0].DictionarySeq)
		assert.Equal(t, want.Lemma, alt.RootCandidates[0].Lemma)
		assert.Equal(t, want.Kana, alt.RootCandidates[0].Kana)
		assert.Equal(t, tok.Start, alt.Start)
		assert.Equal(t, tok.End, alt.End)
	}
}

func TestAnalyzeDocument_LiteralSegments(t *testing.T) {
	g2EnsureInit(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	input := DocumentInput{
		Fragments: []FragmentInput{
			{ID: 1, Text: "食べた。飲んだ！"},
		},
	}
	opts := DocumentOptions{Limit: 1}

	result, err := AnalyzeDocumentContext(ctx, input, opts)
	require.NoError(t, err)
	require.Len(t, result.Fragments, 1)

	frag := result.Fragments[0]
	// Should have both word and literal segments
	hasWord := false
	hasLiteral := false
	for _, seg := range frag.Segments {
		switch seg.Kind {
		case SegmentWord:
			hasWord = true
		case SegmentLiteral:
			hasLiteral = true
		}
	}
	assert.True(t, hasWord, "should have word segments")
	assert.True(t, hasLiteral, "punctuation should produce literal segments")

	// Verify all positions are valid
	totalRunes := len([]rune(frag.AnalysisText))
	lastEnd := 0
	for _, seg := range frag.Segments {
		assert.GreaterOrEqual(t, seg.Start, lastEnd,
			"segment %d start should be >= previous end", seg.Index)
		assert.LessOrEqual(t, seg.End, totalRunes,
			"segment %d end should be <= total runes", seg.Index)
		actualText := string([]rune(frag.AnalysisText)[seg.Start:seg.End])
		assert.Equal(t, seg.Text, actualText,
			"segment %d text should match analysisText slice", seg.Index)
		lastEnd = seg.End
	}
}

func TestAnalyzeDocument_MultipleFragments(t *testing.T) {
	g2EnsureInit(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	input := DocumentInput{
		Fragments: []FragmentInput{
			{ID: 1, Text: "食べた"},
			{ID: 2, Text: "飲んだ"},
			{ID: 3, Text: "走った"},
		},
	}
	opts := DocumentOptions{Limit: 1}

	result, err := AnalyzeDocumentContext(ctx, input, opts)
	require.NoError(t, err)
	assert.Equal(t, 1, result.AdapterVersion)
	require.Len(t, result.Fragments, 3)

	for _, frag := range result.Fragments {
		assert.Contains(t, []int{1, 2, 3}, frag.ID)
		assert.NotEmpty(t, frag.SourceText)
		assert.NotEmpty(t, frag.AnalysisText)
		require.NotEmpty(t, frag.Segments)

		// Each fragment should have resolved rootCandidates
		for _, seg := range frag.Segments {
			if seg.Kind != SegmentWord {
				continue
			}
			for _, interp := range seg.Interpretations {
				for _, tok := range interp.Tokens {
					if tok.Seq > 0 && len(tok.Alternative) == 0 && len(tok.Components) == 0 {
						assert.NotEmpty(t, tok.RootCandidates,
							"scalar leaf %q (seq=%d) in fragment %d should have rootCandidates",
							tok.Surface, tok.Seq, frag.ID)
					}
				}
			}
		}
	}
}

func TestAnalyzeDocument_EmptyInput(t *testing.T) {
	g2EnsureInit(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Empty fragments list
	input := DocumentInput{Fragments: []FragmentInput{}}
	opts := DocumentOptions{Limit: 1}

	result, err := AnalyzeDocumentContext(ctx, input, opts)
	require.NoError(t, err)
	assert.Equal(t, 1, result.AdapterVersion)
	assert.Empty(t, result.Fragments)
}

func TestAnalyzeDocument_RootCandidateReading(t *testing.T) {
	g2EnsureInit(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// 走った should resolve to 走る (はしる). This verifies exact dictionary
	// metadata; it is not a restricted-readings example.
	input := DocumentInput{
		Fragments: []FragmentInput{
			{ID: 1, Text: "走った"},
		},
	}
	opts := DocumentOptions{Limit: 1}

	result, err := AnalyzeDocumentContext(ctx, input, opts)
	require.NoError(t, err)
	require.Len(t, result.Fragments, 1)

	frag := result.Fragments[0]
	require.NotEmpty(t, frag.Segments)

	var rootTok *JSONToken
	for _, seg := range frag.Segments {
		if seg.Kind != SegmentWord {
			continue
		}
		for _, interp := range seg.Interpretations {
			for _, tok := range interp.Tokens {
				if tok.Surface == "走った" || tok.Surface == frag.AnalysisText {
					rootTok = tok
					break
				}
			}
		}
	}
	require.NotNil(t, rootTok, "should find the token for 走った")

	// Check root resolution through alternatives or directly
	var candidates []RootCandidate
	if len(rootTok.Alternative) > 0 {
		for _, alt := range rootTok.Alternative {
			candidates = append(candidates, alt.RootCandidates...)
		}
	} else {
		candidates = rootTok.RootCandidates
	}

	require.NotEmpty(t, candidates, "should have root candidates")
	foundRoot := false
	for _, rc := range candidates {
		if rc.Lemma == "走る" && rc.Kana == "はしる" {
			foundRoot = true
		}
	}
	assert.True(t, foundRoot, "expected 走る/はしる, got %#v", candidates)
}
