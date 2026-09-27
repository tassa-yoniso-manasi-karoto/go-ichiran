package ichiran

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseDocumentResult_RejectsIncompleteProvenance(t *testing.T) {
	// Minimal complete envelope: mutations below must fail in the public parser,
	// not just in an isolated validation helper.
	const valid = `{"adapterVersion":1,"fragments":[{"id":1,"sourceText":"食べた","analysisText":"食べた","segments":[{"index":0,"kind":"word","start":0,"end":3,"text":"食べた","interpretations":[{"score":336,"tokens":[{"text":"食べた","type":"KANJI","start":0,"end":3,"rootCandidates":[{"dictionarySeq":1358280,"lemma":"食べる","kana":"たべる"}]}]}]}]}]}`
	cases := []struct {
		name string
		old  string
		new  string
	}{
		{"missing token position", `"type":"KANJI","start":0`, `"type":"KANJI"`},
		{"fractional position", `"start":0`, `"start":0.5`},
		{"missing segment position", `"kind":"word","start":0`, `"kind":"word"`},
		{"fractional fragment ID", `"id":1`, `"id":1.5`},
		{"wrong source type", `"sourceText":"食べた"`, `"sourceText":1`},
		{"missing analysis text", `"analysisText":"食べた",`, ``},
		{"uncovered fragment", `"analysisText":"食べた"`, `"analysisText":"食べた。"`},
		{"missing interpretations", `"interpretations":[`, `"ignored":[`},
		{"literal with tokens", `"kind":"word"`, `"kind":"literal"`},
		{"token mismatch", `"text":"食べた","type"`, `"text":"飲んだ","type"`},
		{"invented inherited span", `"type":"KANJI"`, `"type":"KANJI","spanInherited":true`},
		{"fractional dictionary ID", `"dictionarySeq":1358280`, `"dictionarySeq":1358280.5`},
		{"negative dictionary ID", `"dictionarySeq":1358280`, `"dictionarySeq":-1`},
		{"missing lemma", `"lemma":"食べる",`, ``},
		{"invalid roots type", `"rootCandidates":[{"dictionarySeq":1358280,"lemma":"食べる","kana":"たべる"}]`, `"rootCandidates":{}`},
		{"invalid gloss entry", `"kana":"たべる"`, `"kana":"たべる","gloss":[42]`},
	}
	_, err := parseDocumentResult([]byte(valid))
	require.NoError(t, err)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mutated := strings.Replace(valid, tc.old, tc.new, 1)
			require.NotEqual(t, valid, mutated, "mutation must exercise its claimed defect")
			_, err := parseDocumentResult([]byte(mutated))
			require.Error(t, err)
		})
	}
	t.Run("adapter capability failure", func(t *testing.T) {
		_, err := parseDocumentResult([]byte(`{"adapterError":"missing relation conj_source_reading"}`))
		require.ErrorContains(t, err, "conj_source_reading")
	})
}

func g2AnalyzeFragments(t *testing.T, texts ...string) *DocumentResult {
	t.Helper()
	g2EnsureInit(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	input := DocumentInput{}
	for i, text := range texts {
		input.Fragments = append(input.Fragments, FragmentInput{ID: i, Text: text})
	}
	result, err := AnalyzeDocumentContext(ctx, input, DocumentOptions{Limit: 3})
	require.NoError(t, err)
	require.Len(t, result.Fragments, len(texts))
	for i, fragment := range result.Fragments {
		assert.Equal(t, i, fragment.ID)
		assert.Equal(t, texts[i], fragment.SourceText)
	}
	return result
}

func g2FirstToken(t *testing.T, fragment FragmentResult) *JSONToken {
	t.Helper()
	for _, segment := range fragment.Segments {
		if segment.Kind == SegmentWord {
			require.NotEmpty(t, segment.Interpretations)
			require.NotEmpty(t, segment.Interpretations[0].Tokens)
			return segment.Interpretations[0].Tokens[0]
		}
	}
	t.Fatal("missing word segment")
	return nil
}

func TestAnalyzeDocument_InflectionsShareCanonicalMetadata(t *testing.T) {
	result := g2AnalyzeFragments(t, "食べる", "食べた", "食べさせられた")
	base := g2FirstToken(t, result.Fragments[0])
	require.Len(t, base.RootCandidates, 1)
	assert.Equal(t, 1358280, base.RootCandidates[0].DictionarySeq)
	assert.Equal(t, "食べる", base.RootCandidates[0].Lemma)
	assert.Equal(t, "たべる", base.RootCandidates[0].Kana)
	for _, fragment := range result.Fragments[1:] {
		token := g2FirstToken(t, fragment)
		assert.NotEqual(t, base.Seq, token.Seq, "generated Seq is not the dictionary identity")
		assert.Equal(t, base.RootCandidates, token.RootCandidates, "POS and gloss must also be canonical")
	}
	viaToken := g2FirstToken(t, result.Fragments[2])
	hasVia := false
	for _, conj := range viaToken.Conj {
		if len(conj.Via) > 0 {
			hasVia = true
		}
	}
	assert.True(t, hasVia, "this fixture must actually exercise a via chain")
}

func TestAnalyzeDocument_CanonicalSpellingAndRootOrdering(t *testing.T) {
	result := g2AnalyzeFragments(t, "あり", "在る", "有る", "ある")
	ari := g2FirstToken(t, result.Fragments[0])
	require.Len(t, ari.RootCandidates, 2)
	assert.Equal(t, 2150170, ari.RootCandidates[0].DictionarySeq, "eligible direct root precedes smaller conjugation root")
	assert.Equal(t, 1296400, ari.RootCandidates[1].DictionarySeq)
	canonical := ari.RootCandidates[1]
	assert.Equal(t, "ある", canonical.Lemma, "dictionary uk policy prefers kana over 有る")
	assert.Equal(t, "ある", canonical.Kana)
	for _, fragment := range result.Fragments[1:] {
		token := g2FirstToken(t, fragment)
		candidates := append([]RootCandidate(nil), token.RootCandidates...)
		for _, alt := range token.Alternative {
			candidates = append(candidates, alt.RootCandidates...)
		}
		found := false
		for _, candidate := range candidates {
			if candidate.DictionarySeq == canonical.DictionarySeq {
				assert.Equal(t, canonical, candidate)
				found = true
			}
		}
		assert.True(t, found, "expected canonical ある entry for %q", fragment.SourceText)
	}
}

func TestAnalyzeDocument_HomographsKeepDistinctIdentities(t *testing.T) {
	result := g2AnalyzeFragments(t, "生物", "開いた")
	wants := []map[string]RootCandidate{
		{"せいぶつ": {DictionarySeq: 1379430, Lemma: "生物", Kana: "せいぶつ"}, "なまもの": {DictionarySeq: 1379440, Lemma: "生物", Kana: "なまもの"}},
		{"ひらいた": {DictionarySeq: 1202440, Lemma: "開く", Kana: "ひらく"}, "あいた": {DictionarySeq: 1586270, Lemma: "開く", Kana: "あく"}},
	}
	for i, fragment := range result.Fragments {
		token := g2FirstToken(t, fragment)
		require.Len(t, token.Alternative, len(wants[i]))
		for _, alternative := range token.Alternative {
			want, ok := wants[i][alternative.Kana]
			require.True(t, ok)
			require.Len(t, alternative.RootCandidates, 1)
			got := alternative.RootCandidates[0]
			assert.Equal(t, want.DictionarySeq, got.DictionarySeq)
			assert.Equal(t, want.Lemma, got.Lemma)
			assert.Equal(t, want.Kana, got.Kana)
		}
	}
}

func TestAnalyzeDocument_NormalizedCompoundProvenance(t *testing.T) {
	// Supplementary characters count as one code point, not two UTF-16 units.
	result := g2AnalyzeFragments(t, "𠮷。勉強しています", "ﾀﾍﾞﾀ。 ガッツ！", "")
	fragment := result.Fragments[0]
	assert.Equal(t, "𠮷. 勉強しています", fragment.AnalysisText)
	token := g2FirstToken(t, fragment)
	require.NotNil(t, token.Start)
	assert.Equal(t, 3, *token.Start)
	assert.Equal(t, 10, *token.End)
	require.Len(t, token.Components, 3)
	for i, child := range token.Components {
		assert.True(t, child.SpanInherited)
		assert.Equal(t, token.Start, child.Start)
		assert.Equal(t, token.End, child.End)
		assert.Equal(t, []int{i}, child.ComponentPath)
		require.NotEmpty(t, child.RootCandidates)
	}
	assert.Equal(t, "勉強", token.Components[0].RootCandidates[0].Lemma)
	assert.Equal(t, "する", token.Components[1].RootCandidates[0].Lemma)
	assert.Equal(t, "いる", token.Components[2].RootCandidates[0].Lemma)
	assert.NotEqual(t, result.Fragments[1].SourceText, result.Fragments[1].AnalysisText)
	assert.Empty(t, result.Fragments[2].Segments)
}

// Exercise resolver branches using verified records in the live dictionary.
// Only process-local word-info objects are changed; no database writes occur.
func TestAnalyzeDocument_ResolverSelectorsAndRestrictions(t *testing.T) {
	expr := `(progn ` + buildDocumentLispExpr(DocumentInput{}, 1) + `
 (jsown:to-json
  (list
   (langkit-verify-root 1367020 "ひとげ" "ひと気")
   (langkit-verify-root 1367020 "ひとげ" "人け")
   (langkit-verify-root 1367020 "ヒトゲ" nil)
   (langkit-verify-root 1367020 nil "人気")
   (langkit-resolve-roots (let ((w (ichiran/dict::simple-word-info 10091238 "食べた" "たべた" :kanji))) (setf (ichiran/dict::word-info-conjugations w) :root) w))
   (langkit-resolve-roots (let ((w (ichiran/dict::simple-word-info 10091238 "食べた" "たべた" :kanji))) (setf (ichiran/dict::word-info-conjugations w) '(91715)) w))
   (langkit-resolve-roots (let ((w (ichiran/dict::simple-word-info 10091238 "食べた" "たべた" :kanji))) (setf (ichiran/dict::word-info-conjugations w) '(471571)) w))
   (langkit-verify-root 1171680 "はね" "羽根")
   (langkit-verify-root 1171680 "はね" "羽")
   (langkit-verify-root 1367020 "ヒトゲ" "does-not-exist"))))`
	payload := g2EvaluateLisp(t, expr)
	var results []json.RawMessage
	require.NoError(t, json.Unmarshal(payload, &results))
	require.Len(t, results, 10)
	var candidate RootCandidate
	require.NoError(t, json.Unmarshal(results[0], &candidate))
	assert.Equal(t, 1367020, candidate.DictionarySeq)
	assert.Equal(t, "人気", candidate.Lemma)
	assert.Equal(t, "ひとげ", candidate.Kana)
	assert.JSONEq(t, string(results[0]), string(results[2]), "script normalization preserves the verified reading")
	for _, i := range []int{1, 3, 4, 6, 9} {
		assert.JSONEq(t, `[]`, string(results[i]))
	}
	var roots []RootCandidate
	require.NoError(t, json.Unmarshal(results[5], &roots))
	require.Len(t, roots, 1)
	assert.Equal(t, 1358280, roots[0].DictionarySeq)
	assert.JSONEq(t, string(results[7]), string(results[8]), "canonical senses cannot depend on occurrence spelling")
	var feather RootCandidate
	require.NoError(t, json.Unmarshal(results[7], &feather))
	assert.Equal(t, "羽", feather.Lemma)
	require.NotEmpty(t, feather.Gloss)
	for _, gloss := range feather.Gloss {
		assert.NotContains(t, gloss.Gloss, "blade", "this sense is restricted to 羽根, not canonical 羽")
	}
}

func g2EvaluateLisp(t *testing.T, expr string) []byte {
	t.Helper()
	g2EnsureInit(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	mgr, err := getOrCreateDefaultManager(ctx)
	require.NoError(t, err)
	cmd := exec.CommandContext(ctx, "docker", "exec", mgr.GetContainerName(), "ichiran-cli", "-e", expr)
	output, err := cmd.Output()
	if failure, ok := err.(*exec.ExitError); ok {
		t.Log(string(failure.Stderr))
	}
	require.NoError(t, err)
	var payload string
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(string(output))), &payload))
	return []byte(payload)
}

func TestAnalyzeDocument_MissingCapabilityIsNotUnknownWord(t *testing.T) {
	expr := buildDocumentLispExpr(DocumentInput{Fragments: []FragmentInput{{ID: 0, Text: "食べた"}}}, 1)
	// Simulate a missing capability only inside this disposable Lisp process.
	expr = strings.Replace(expr, " (handler-case (progn", " (fmakunbound 'langkit-verify-root) (handler-case (progn", 1)
	_, err := parseDocumentResult(g2EvaluateLisp(t, expr))
	require.ErrorContains(t, err, "LANGKIT-VERIFY-ROOT")
	require.ErrorContains(t, err, "adapter failed")
}

func TestAnalyzeDocument_DeepChildSpanRebasing(t *testing.T) {
	// Synthetic nesting exercises recursion beyond current dictionary examples.
	// It is deliberately not presented as Ichiran's segmentation of 食べた.
	expr := `(progn ` + buildDocumentLispExpr(DocumentInput{}, 1) + `
 (let* ((leaf (jsown:new-js ("text" "食べ") ("start" nil) ("end" nil)))
        (compound (jsown:new-js ("text" "食べた") ("start" 0) ("end" 3) ("components" (list leaf))))
        (parent (jsown:new-js ("text" "食べた") ("start" 0) ("end" 3) ("alternative" (list compound)))))
  (jsown:to-json (langkit-rebase-positions parent 4))))`
	var raw map[string]interface{}
	require.NoError(t, json.Unmarshal(g2EvaluateLisp(t, expr), &raw))
	token, err := parseEnrichedToken(raw)
	require.NoError(t, err)
	require.Len(t, token.Alternative, 1)
	require.Len(t, token.Alternative[0].Components, 1)
	leaf := token.Alternative[0].Components[0]
	require.NotNil(t, leaf.Start)
	require.NotNil(t, leaf.End)
	assert.Equal(t, 4, *leaf.Start)
	assert.Equal(t, 7, *leaf.End)
	assert.True(t, leaf.SpanInherited)
	assert.Equal(t, []int{0}, leaf.ComponentPath)
	require.NoError(t, validateDocumentToken(token, []rune("abcd食べた"), 4, 7, nil))
	cloned := cloneJSONTokens([]JSONToken{*token})
	cloned[0].Alternative[0].Components[0].ComponentPath[0] = 99
	assert.Equal(t, []int{0}, leaf.ComponentPath)
}

func TestAnalyzeDocument_BoundedChainsReturnWarnings(t *testing.T) {
	expr := `(progn ` + buildDocumentLispExpr(DocumentInput{}, 1) + `
 (langkit-walk-conj 10091238 '(91715) nil '("食べた") '("たべた") nil 64)
 (langkit-walk-conj 10091238 '(91715) nil '("食べた") '("たべた") '((10091238 . 91715)) 0)
 (jsown:to-json (jsown:new-js ("adapterVersion" 1) ("fragments" nil) ("warnings" (nreverse *langkit-warnings*))))))`
	result, err := parseDocumentResult(g2EvaluateLisp(t, expr))
	require.NoError(t, err)
	require.Len(t, result.Warnings, 2)
	assert.Contains(t, result.Warnings[0], "hop limit")
	assert.Contains(t, result.Warnings[1], "cycle")
}
