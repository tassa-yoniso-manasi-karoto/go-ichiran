package ichiran

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// LemmaQuery names a word by a dictionary spelling and its dictionary-form
// reading, the way a reviewer proposes a word that analysis did not offer,
// and carries the reading the word has where it occurs.
type LemmaQuery struct {
	Lemma             string
	LemmaReading      string
	OccurrenceReading string
}

// LemmaResult lists the dictionary roots a query names, in ascending Seq
// order, each verified exactly as AnalyzeDocument verifies an analyzed
// word's roots. Romaji romanizes the query's occurrence reading with
// Ichiran's own word romanizer; it is empty when that reading is empty.
type LemmaResult struct {
	Candidates []RootCandidate
	Romaji     string
}

// ResolveLemmas resolves every query in one Ichiran call. A spelling and
// reading that match no dictionary root give a result without candidates;
// adapter and database failures are errors.
func (im *IchiranManager) ResolveLemmas(ctx context.Context, queries []LemmaQuery) ([]LemmaResult, error) {
	if len(queries) == 0 {
		return nil, nil
	}
	for i, query := range queries {
		if strings.TrimSpace(query.Lemma) == "" || strings.TrimSpace(query.LemmaReading) == "" {
			return nil, fmt.Errorf("lemma query %d needs a lemma and its reading", i)
		}
	}
	output, err := im.runLispJSON(ctx, buildLemmaLispExpr(queries))
	if err != nil {
		return nil, err
	}
	results, err := parseLemmaResults(output, len(queries))
	if err != nil {
		return nil, fmt.Errorf("failed to parse lemma results: %w", err)
	}
	return results, nil
}

// ResolveLemmasContext is the default-manager version of ResolveLemmas.
func ResolveLemmasContext(ctx context.Context, queries []LemmaQuery) ([]LemmaResult, error) {
	mgr, err := getOrCreateDefaultManager(ctx)
	if err != nil {
		return nil, err
	}
	return mgr.ResolveLemmas(ctx, queries)
}

// RootKey names a dictionary root by its entry number and one kana reading
// of it, the way a saved analysis records a word.
type RootKey struct {
	Seq  int
	Kana string
}

// rootBatchSize bounds the keys of one Ichiran call, so that the
// expression fits in one command-line argument.
const rootBatchSize = 400

// ResolveRoots verifies each key exactly as AnalyzeDocument verifies an
// analyzed word's roots, choosing the same canonical spelling and gloss,
// and returns the root each key names, in order, or nil when the entry no
// longer has that kana reading. Adapter and database failures are errors.
func (im *IchiranManager) ResolveRoots(ctx context.Context, keys []RootKey) ([]*RootCandidate, error) {
	for i, key := range keys {
		if key.Seq <= 0 || strings.TrimSpace(key.Kana) == "" {
			return nil, fmt.Errorf("root key %d needs an entry number and a kana reading", i)
		}
	}
	results := make([]*RootCandidate, 0, len(keys))
	for start := 0; start < len(keys); start += rootBatchSize {
		batch := keys[start:min(start+rootBatchSize, len(keys))]
		output, err := im.runLispJSON(ctx, buildRootLispExpr(batch))
		if err != nil {
			return nil, err
		}
		roots, err := parseRootResults(output, len(batch))
		if err != nil {
			return nil, fmt.Errorf("failed to parse root results: %w", err)
		}
		results = append(results, roots...)
	}
	return results, nil
}

// ResolveRootsContext is the default-manager version of ResolveRoots.
func ResolveRootsContext(ctx context.Context, keys []RootKey) ([]*RootCandidate, error) {
	mgr, err := getOrCreateDefaultManager(ctx)
	if err != nil {
		return nil, err
	}
	return mgr.ResolveRoots(ctx, keys)
}

// buildRootLispExpr constructs the Lisp expression for ResolveRoots: each
// key goes through langkit-verify-root, with no spelling to verify.
func buildRootLispExpr(keys []RootKey) string {
	var b strings.Builder

	b.WriteString(`(progn`)
	b.WriteString(` (ql:quickload :jsown :silent t)`)
	writeRootLispDefinitions(&b)

	b.WriteString(` (setf *langkit-root-memo* (make-hash-table :test 'equal))`)
	b.WriteString(` (setf *langkit-warnings* nil)`)
	b.WriteString(` (handler-case`)
	b.WriteString(` (let ((results nil))`)
	// Keys travel as quoted data, like document fragments.
	b.WriteString(` (dolist (key '(`)
	for _, key := range keys {
		b.WriteString(fmt.Sprintf(`(%d "%s")`, key.Seq, escapeLispString(key.Kana)))
	}
	b.WriteString(`))`)
	b.WriteString(` (let ((root (langkit-verify-root (first key) (second key) nil)))`)
	b.WriteString(` (push (jsown:new-js ("candidates" (when root (list root)))) results)))`)
	b.WriteString(` (let ((result (jsown:new-js ("adapterVersion" 1) ("results" (nreverse results)))))`)
	b.WriteString(` (when *langkit-warnings* (jsown:extend-js result ("warnings" (nreverse *langkit-warnings*))))`)
	b.WriteString(` (jsown:to-json result)))`)
	b.WriteString(` (error (e) (jsown:to-json (jsown:new-js ("adapterError" (princ-to-string e))))))`)
	b.WriteString(`)`)

	return b.String()
}

// parseRootResults parses the envelope printed by buildRootLispExpr.
func parseRootResults(data []byte, count int) ([]*RootCandidate, error) {
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("invalid JSON envelope: %w", err)
	}
	if message, ok := raw["adapterError"].(string); ok {
		return nil, fmt.Errorf("Ichiran root adapter failed: %s", message)
	}
	version, err := documentInt(raw, "adapterVersion")
	if err != nil {
		return nil, err
	}
	if version != adapterVersion {
		return nil, fmt.Errorf("unsupported adapter version %d", version)
	}
	items, _ := raw["results"].([]interface{})
	if len(items) != count {
		return nil, fmt.Errorf("root adapter returned %d results for %d keys", len(items), count)
	}

	results := make([]*RootCandidate, count)
	for i, item := range items {
		fields, ok := item.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("root result %d is not an object", i)
		}
		var candidates []interface{}
		switch value := fields["candidates"].(type) {
		case nil, bool:
			// jsown may print an empty list as null or false.
		case []interface{}:
			candidates = value
		default:
			return nil, fmt.Errorf("root result %d candidates are not an array", i)
		}
		if len(candidates) > 1 {
			return nil, fmt.Errorf("root result %d has %d roots", i, len(candidates))
		}
		if len(candidates) == 1 {
			candidateFields, ok := candidates[0].(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("root result %d is not an object", i)
			}
			rc, err := parseRootCandidate(candidateFields)
			if err != nil {
				return nil, fmt.Errorf("root result %d: %w", i, err)
			}
			results[i] = &rc
		}
	}
	if warnings, ok := raw["warnings"].([]interface{}); ok {
		for _, warning := range warnings {
			if message, ok := warning.(string); ok {
				Logger.Warn().Str("adapter", "ichiran-root").Msg(message)
			}
		}
	}
	return results, nil
}

// buildLemmaLispExpr constructs the Lisp expression for ResolveLemmas.
//
// An entry is a candidate when one of its kana records spells the reading,
// in either kana script, and, unless the lemma is that same reading in kana,
// one of its kanji records spells the lemma. langkit-verify-root, shared with
// document analysis, then requires a root entry, checks reading
// restrictions and chooses the canonical spelling and gloss.
func buildLemmaLispExpr(queries []LemmaQuery) string {
	var b strings.Builder

	b.WriteString(`(progn`)
	b.WriteString(` (ql:quickload :jsown :silent t)`)
	writeRootLispDefinitions(&b)

	b.WriteString(` (defun langkit-resolve-lemma (lemma reading)`)
	b.WriteString(` (ichiran/dict::with-connection ichiran/dict::*connection*`)
	b.WriteString(` (let* ((variants (remove-duplicates (list reading (ichiran::as-hiragana reading) (ichiran::as-katakana reading)) :test #'equal))`)
	b.WriteString(` (seqs (remove-duplicates (postmodern:query (:select 'seq :from 'kana-text :where (:in 'text (:set variants))) :column)))`)
	b.WriteString(` (candidates nil))`)
	b.WriteString(` (unless (equal (langkit-kana-key lemma) (langkit-kana-key reading))`)
	b.WriteString(` (setf seqs (intersection seqs (postmodern:query (:select 'seq :from 'kanji-text :where (:= 'text lemma)) :column))))`)
	b.WriteString(` (dolist (seq (sort (copy-list seqs) #'<))`)
	b.WriteString(` (let ((c (langkit-verify-root seq reading lemma))) (when c (push c candidates))))`)
	b.WriteString(` (nreverse candidates))))`)

	b.WriteString(` (setf *langkit-root-memo* (make-hash-table :test 'equal))`)
	b.WriteString(` (setf *langkit-warnings* nil)`)
	b.WriteString(` (handler-case`)
	b.WriteString(` (let ((results nil))`)
	// Queries travel as quoted data, like document fragments.
	b.WriteString(` (dolist (query '(`)
	for _, query := range queries {
		b.WriteString(fmt.Sprintf(`("%s" "%s" "%s")`,
			escapeLispString(query.Lemma),
			escapeLispString(query.LemmaReading),
			escapeLispString(query.OccurrenceReading)))
	}
	b.WriteString(`))`)
	b.WriteString(` (destructuring-bind (lemma reading occurrence) query`)
	b.WriteString(` (let ((js (jsown:new-js ("candidates" (langkit-resolve-lemma lemma reading)))))`)
	b.WriteString(` (when (plusp (length occurrence)) (jsown:extend-js js ("romaji" (ichiran:romanize-word (copy-seq occurrence)))))`)
	b.WriteString(` (push js results))))`)
	b.WriteString(` (let ((result (jsown:new-js ("adapterVersion" 1) ("results" (nreverse results)))))`)
	b.WriteString(` (when *langkit-warnings* (jsown:extend-js result ("warnings" (nreverse *langkit-warnings*))))`)
	b.WriteString(` (jsown:to-json result)))`)
	b.WriteString(` (error (e) (jsown:to-json (jsown:new-js ("adapterError" (princ-to-string e))))))`)
	b.WriteString(`)`)

	return b.String()
}

// parseLemmaResults parses the envelope printed by buildLemmaLispExpr.
func parseLemmaResults(data []byte, count int) ([]LemmaResult, error) {
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("invalid JSON envelope: %w", err)
	}
	if message, ok := raw["adapterError"].(string); ok {
		return nil, fmt.Errorf("Ichiran lemma adapter failed: %s", message)
	}
	version, err := documentInt(raw, "adapterVersion")
	if err != nil {
		return nil, err
	}
	if version != adapterVersion {
		return nil, fmt.Errorf("unsupported adapter version %d", version)
	}
	items, _ := raw["results"].([]interface{})
	if len(items) != count {
		return nil, fmt.Errorf("lemma adapter returned %d results for %d queries", len(items), count)
	}

	results := make([]LemmaResult, count)
	for i, item := range items {
		fields, ok := item.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("lemma result %d is not an object", i)
		}
		if romaji, ok := fields["romaji"].(string); ok {
			results[i].Romaji = romaji
		}
		var candidates []interface{}
		switch value := fields["candidates"].(type) {
		case nil, bool:
			// jsown may print an empty list as null or false.
		case []interface{}:
			candidates = value
		default:
			return nil, fmt.Errorf("lemma result %d candidates are not an array", i)
		}
		for j, candidate := range candidates {
			candidateFields, ok := candidate.(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("lemma result %d candidate %d is not an object", i, j)
			}
			rc, err := parseRootCandidate(candidateFields)
			if err != nil {
				return nil, fmt.Errorf("lemma result %d candidate %d: %w", i, j, err)
			}
			results[i].Candidates = append(results[i].Candidates, rc)
		}
	}
	if warnings, ok := raw["warnings"].([]interface{}); ok {
		for _, warning := range warnings {
			if message, ok := warning.(string); ok {
				Logger.Warn().Str("adapter", "ichiran-lemma").Msg(message)
			}
		}
	}
	return results, nil
}
