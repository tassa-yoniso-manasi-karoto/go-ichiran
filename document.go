package ichiran

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"unicode/utf8"
)

// adapterVersion is the current version of the Lisp→Go analysis envelope.
// The Go parser rejects envelopes with a different version.
const adapterVersion = 1

// AnalyzeDocument performs batch morphological analysis with structured
// segment provenance and root candidate resolution. Each fragment is
// independently normalized and analyzed; positions are rune offsets into
// the per-fragment analysisText.
//
// This is the G2 entrypoint. The existing Analyze/AnalyzeWithOptions
// methods remain for backward compatibility; they use the romanize* path.
func (im *IchiranManager) AnalyzeDocument(ctx context.Context, input DocumentInput, opts DocumentOptions) (*DocumentResult, error) {
	ids := make(map[int]bool, len(input.Fragments))
	for _, fragment := range input.Fragments {
		if ids[fragment.ID] {
			return nil, fmt.Errorf("duplicate fragment ID %d", fragment.ID)
		}
		ids[fragment.ID] = true
	}
	queryCtx, cancel := context.WithTimeout(ctx, im.QueryTimeout)
	defer cancel()

	limit := opts.Limit
	if limit < 1 {
		limit = 5
	}

	if len(input.Fragments) == 0 {
		return &DocumentResult{AdapterVersion: adapterVersion}, nil
	}

	output, err := im.runLispJSON(queryCtx, buildDocumentLispExpr(input, limit))
	if err != nil {
		return nil, err
	}

	result, err := parseDocumentResult(output)
	if err != nil {
		return nil, fmt.Errorf("failed to parse document result: %w", err)
	}
	if len(result.Fragments) != len(input.Fragments) {
		return nil, fmt.Errorf("document adapter returned %d fragments for %d inputs", len(result.Fragments), len(input.Fragments))
	}
	for i, fragment := range result.Fragments {
		if fragment.ID != input.Fragments[i].ID || fragment.SourceText != input.Fragments[i].Text {
			return nil, fmt.Errorf("document adapter changed source text or ID for fragment %d", i)
		}
	}
	for _, warning := range result.Warnings {
		Logger.Warn().Str("adapter", "ichiran-document").Msg(warning)
	}

	return result, nil
}

// withPooledConnections wraps a Lisp form so that the database connections
// Ichiran opens while evaluating it come from a pool. Ichiran opens one per
// word and per kanji reading and closes it after each query; every close
// left a socket in TIME_WAIT for a minute, until a container serving
// several analyses ran out of local ports and refused new connections
// ("Cannot assign requested address"). Pooled, a process reuses one.
func withPooledConnections(form string) string {
	return `(let ((ichiran/dict::*connection* (if (and (consp ichiran/dict::*connection*)` +
		` (not (member :pooled-p ichiran/dict::*connection*)))` +
		` (append ichiran/dict::*connection* (list :pooled-p t)) ichiran/dict::*connection*))) ` +
		form + `)`
}

// AnalyzeDocumentContext is the context-aware default-manager version.
func AnalyzeDocumentContext(ctx context.Context, input DocumentInput, opts DocumentOptions) (*DocumentResult, error) {
	mgr, err := getOrCreateDefaultManager(ctx)
	if err != nil {
		return nil, err
	}
	return mgr.AnalyzeDocument(ctx, input, opts)
}

// buildDocumentLispExpr constructs the Lisp expression for AnalyzeDocument.
//
// The Lisp adapter:
//  1. Loads jsown for JSON serialization
//  2. Defines a root-candidate resolver that walks conjugation chains carrying
//     both spelling and kana evidence, verifies terminal entries against the
//     dictionary, and honours reading restrictions and kana-preferred policy
//  3. Normalizes each fragment, runs basic-split and dict-segment
//  4. Recursively enriches every lexical leaf (including compound children and
//     alternative candidates) with root candidates, romanization, glosses and
//     kanji-reading matches
//  5. Returns a versioned envelope whose positions are character (not byte)
//     offsets into the normalized analysisText
//
// Analysis errors are returned as adapterError, never as empty root results.
// Only per-word absence of dictionary evidence is a supported empty result.
func buildDocumentLispExpr(input DocumentInput, limit int) string {
	var b strings.Builder

	b.WriteString(`(progn`)
	b.WriteString(` (ql:quickload :jsown :silent t)`)

	// defmethod jsown:to-json for word-info
	b.WriteString(` (defmethod jsown:to-json ((word-info ichiran/dict::word-info))`)
	b.WriteString(` (let* ((gloss-json (ichiran::word-info-gloss-json word-info))`)
	b.WriteString(` (match-json (when (and (stringp (ichiran/dict::word-info-text word-info)) (stringp (ichiran/dict::word-info-kana word-info))) (ichiran/kanji:match-readings-json (ichiran/dict::word-info-text word-info) (ichiran/dict::word-info-kana word-info))))`)
	b.WriteString(` (word-json (ichiran::word-info-json word-info)))`)
	b.WriteString(` (when gloss-json (jsown:extend-js word-json ("gloss" gloss-json)))`)
	b.WriteString(` (when match-json (jsown:extend-js word-json ("match" match-json)))`)
	b.WriteString(` (jsown:to-json word-json)))`) // close let*, defmethod

	writeRootLispDefinitions(&b)

	// ==================== langkit-resolve-roots ====================
	b.WriteString(` (defun langkit-resolve-roots (word-info)`)
	b.WriteString(` (ichiran/dict::with-connection ichiran/dict::*connection*`)
	b.WriteString(` (let* ((seq (ichiran/dict::word-info-seq word-info))`)
	b.WriteString(` (conjs (ichiran/dict::word-info-conjugations word-info))`)
	b.WriteString(` (true-text (ichiran/dict::word-info-true-text word-info))`)
	b.WriteString(` (raw-kana (ichiran/dict::word-info-kana word-info))`)
	b.WriteString(` (kana (if (listp raw-kana) (mapcar #'ichiran/dict::strip-hints raw-kana) (list (ichiran/dict::strip-hints raw-kana))))`)
	b.WriteString(` (spelling (if (listp true-text) true-text (if true-text (list true-text) nil))))`)
	b.WriteString(` (when (or (null seq) (eql seq 0)) (return-from langkit-resolve-roots nil))`)
	// Memo check
	b.WriteString(` (let ((memo-key (list seq conjs true-text raw-kana)))`)
	b.WriteString(` (multiple-value-bind (cached present) (gethash memo-key *langkit-root-memo*)`)
	b.WriteString(` (when present (return-from langkit-resolve-roots cached))))`)
	b.WriteString(` (let ((entry (car (postmodern:select-dao 'ichiran/dict::entry (:= 'ichiran/dict::seq seq)))))`)
	b.WriteString(` (let ((raw-results nil) (direct-root nil))`)
	b.WriteString(` (cond`)
	// ROOT selector
	b.WriteString(` ((eql conjs :root)`)
	b.WriteString(` (when (and entry (ichiran/dict::root-p entry))`)
	b.WriteString(` (let ((c (langkit-verify-root seq (car kana) (car spelling)))) (when c (setf direct-root c) (push c raw-results)))))`)
	// Unspecified — direct root + conjugation branches
	b.WriteString(` ((null conjs)`)
	b.WriteString(` (when (and entry (ichiran/dict::root-p entry))`)
	b.WriteString(` (let ((c (langkit-verify-root seq (car kana) (car spelling)))) (when c (setf direct-root c) (push c raw-results))))`)
	// Per-branch visited: pass nil (empty list) as initial visited
	b.WriteString(` (let ((triples (langkit-walk-conj seq nil nil (or spelling kana) kana nil 0)))`)
	b.WriteString(` (dolist (tr triples) (let ((c (langkit-verify-root (first tr) (second tr) (third tr)))) (when c (push c raw-results))))))`)
	// Explicit conjugation IDs
	b.WriteString(` ((listp conjs)`)
	b.WriteString(` (let ((triples (langkit-walk-conj seq conjs nil (or spelling kana) kana nil 0)))`)
	b.WriteString(` (dolist (tr triples) (let ((c (langkit-verify-root (first tr) (second tr) (third tr)))) (when c (push c raw-results)))))))`)
	// Deterministic sort by (seq, kana) before dedup
	b.WriteString(` (setf raw-results (sort (nreverse raw-results)`)
	b.WriteString(` (lambda (a b) (let ((sa (jsown:val a "dictionarySeq")) (sb (jsown:val b "dictionarySeq")))`)
	b.WriteString(` (or (< sa sb) (and (= sa sb) (string< (jsown:val a "kana") (jsown:val b "kana"))))))))`)
	b.WriteString(` (when direct-root (setf raw-results (cons direct-root (remove direct-root raw-results :test #'eq))))`)
	// Deduplicate by (seq, kana)
	b.WriteString(` (let ((seen (make-hash-table :test 'equal)) (deduped nil))`)
	b.WriteString(` (dolist (c raw-results)`)
	b.WriteString(` (let ((key (cons (jsown:val c "dictionarySeq") (langkit-kana-key (jsown:val c "kana")))))`)
	b.WriteString(` (unless (gethash key seen) (setf (gethash key seen) t) (push c deduped))))`)
	b.WriteString(` (let ((final (nreverse deduped)))`)
	b.WriteString(` (setf (gethash (list seq conjs true-text raw-kana) *langkit-root-memo*) final)`)
	b.WriteString(` final)))))))`)

	// ==================== langkit-rebase-positions ====================
	// Rebase independent spans once; positionless children inherit their parent.
	b.WriteString(` (defun langkit-rebase-positions (tok offset &optional parent-start parent-end)`)
	b.WriteString(` (let* ((start (jsown:val tok "start")) (end (jsown:val tok "end")) (inherited (and (null start) (null end))))`)
	b.WriteString(` (unless (or (and (integerp start) (integerp end)) (and inherited parent-start parent-end)) (error "langkit: missing or invalid token span"))`)
	b.WriteString(` (setf start (if inherited parent-start (+ offset start)) end (if inherited parent-end (+ offset end)))`)
	b.WriteString(` (jsown:extend-js tok ("start" start) ("end" end) ("spanInherited" (if inherited t :false)))`)
	b.WriteString(` (dolist (field '("components" "alternative")) (let ((children (cdr (assoc field (cdr tok) :test #'equal))))`)
	b.WriteString(` (when (listp children) (dolist (child children) (langkit-rebase-positions child offset start end))))) tok))`)

	// ==================== langkit-enrich-word-info ====================
	// Issue 2: when alternative=true, clear the erroneous "components"
	// from the base serialization, replacing with enriched "alternative".
	b.WriteString(` (defun langkit-enrich-word-info (word-info)`)
	b.WriteString(` (let ((parsed (jsown:parse (jsown:to-json word-info))))`)
	// Romanization
	b.WriteString(` (let ((rom (ichiran::romanize-word-info word-info)))`)
	b.WriteString(` (when rom (jsown:extend-js parsed ("romaji" rom))))`)
	// Root candidates for scalar leaves only
	b.WriteString(` (unless (or (ichiran/dict::word-info-components word-info) (ichiran/dict::word-info-alternative word-info))`)
	b.WriteString(` (let ((roots (langkit-resolve-roots word-info))) (when roots (jsown:extend-js parsed ("rootCandidates" roots)))))`)
	// Recurse into compound children (NOT alternatives)
	b.WriteString(` (when (and (ichiran/dict::word-info-components word-info) (not (ichiran/dict::word-info-alternative word-info)))`)
	b.WriteString(` (let ((enriched nil))`)
	b.WriteString(` (dolist (child (ichiran/dict::word-info-components word-info)) (push (langkit-enrich-word-info child) enriched))`)
	b.WriteString(` (jsown:extend-js parsed ("components" (nreverse enriched)))))`)
	// Recurse into alternative candidates — clear components to avoid duplication
	b.WriteString(` (when (ichiran/dict::word-info-alternative word-info)`)
	b.WriteString(` (let ((enriched nil))`)
	b.WriteString(` (dolist (alt (ichiran/dict::word-info-components word-info)) (push (langkit-enrich-word-info alt) enriched))`)
	b.WriteString(` (jsown:extend-js parsed ("alternative" (nreverse enriched))))`)
	// Remove the erroneous "components" left by word-info-json
	b.WriteString(` (setf (cdr parsed) (remove "components" (cdr parsed) :key #'car :test #'equal)))`)
	b.WriteString(` parsed))`)

	// ==================== langkit-source-offsets ====================
	// Replays Ichiran's normalize with its own tables, recording where each
	// normalized character came from. The replay must reproduce the
	// normalized text exactly, or no offsets are returned.
	b.WriteString(` (defun langkit-source-offsets (source-text normalized)`)
	b.WriteString(` (let* ((context ichiran::*default-romanization-method*)`)
	b.WriteString(` (step (map 'string (lambda (c) (or (ichiran/characters::to-normal-char c :context context) c)) source-text))`)
	b.WriteString(` (alist (loop for (from to) on (append ichiran/characters::*punctuation-marks* ichiran/characters::*dakuten-join*) by #'cddr collect (cons from to)))`)
	b.WriteString(` (scanner (ppcre:create-scanner (cons :alternation (mapcar #'car alist))))`)
	b.WriteString(` (offsets nil) (out (make-string-output-stream)) (pos 0))`)
	b.WriteString(` (ppcre:do-matches (ms me scanner step)`)
	b.WriteString(` (loop for i from pos below ms do (push i offsets) (write-char (char step i) out))`)
	b.WriteString(` (let ((to (cdr (assoc (subseq step ms me) alist :test #'equal))))`)
	b.WriteString(` (loop repeat (length to) do (push ms offsets)) (write-string to out))`)
	b.WriteString(` (setf pos me))`)
	b.WriteString(` (loop for i from pos below (length step) do (push i offsets) (write-char (char step i) out))`)
	b.WriteString(` (push (length source-text) offsets)`)
	b.WriteString(` (when (string= (get-output-stream-string out) normalized) (nreverse offsets))))`)

	// ==================== langkit-analyze-fragment ====================
	// Defined once and called per fragment.  Emitting this body once per
	// fragment made SBCL compile it N times inside one form, which
	// exhausts its heap at about a dozen fragments.
	b.WriteString(` (defun langkit-analyze-fragment (id source-text limit)`)
	b.WriteString(` (let* ((normalized (ichiran::normalize (copy-seq source-text) :context ichiran::*default-romanization-method*))`)
	b.WriteString(` (splits (ichiran::basic-split normalized))`)
	b.WriteString(` (seg-index 0)`)
	b.WriteString(` (char-offset 0)`)
	b.WriteString(` (segments nil))`)

	b.WriteString(` (dolist (split splits)`)
	b.WriteString(` (let* ((split-type (car split))`)
	b.WriteString(` (split-text (cdr split))`)
	b.WriteString(` (text-len (length split-text))`)
	b.WriteString(` (seg-start char-offset)`)
	b.WriteString(` (seg-end (+ char-offset text-len)))`)

	// Literal segment (misc)
	b.WriteString(` (if (eql split-type :misc)`)
	b.WriteString(` (push (jsown:new-js ("index" seg-index) ("kind" "literal")`)
	b.WriteString(` ("start" seg-start) ("end" seg-end) ("text" split-text)) segments)`)

	// Word segment
	b.WriteString(` (let ((interps (ichiran/dict::dict-segment split-text :limit limit)))`)
	b.WriteString(` (let ((interp-results nil))`)
	b.WriteString(` (dolist (interp interps)`)
	b.WriteString(` (let* ((word-list (car interp))`)
	b.WriteString(` (score (cdr interp))`)
	b.WriteString(` (tokens nil))`)
	b.WriteString(` (dolist (wi word-list)`)
	b.WriteString(` (let ((enriched (langkit-enrich-word-info wi)))`)
	b.WriteString(` (langkit-rebase-positions enriched seg-start)`)
	b.WriteString(` (push enriched tokens)))`)
	b.WriteString(` (push (jsown:new-js ("score" (or score 0)) ("tokens" (nreverse tokens))) interp-results)))`)
	b.WriteString(` (push (jsown:new-js ("index" seg-index) ("kind" "word")`)
	b.WriteString(` ("start" seg-start) ("end" seg-end) ("text" split-text)`)
	b.WriteString(` ("interpretations" (nreverse interp-results))) segments))))`)

	b.WriteString(` (incf char-offset text-len)`)
	b.WriteString(` (incf seg-index)))`)

	b.WriteString(` (let ((js (jsown:new-js ("id" id) ("sourceText" source-text)`)
	b.WriteString(` ("analysisText" normalized) ("segments" (nreverse segments))))`)
	b.WriteString(` (offsets (langkit-source-offsets source-text normalized)))`)
	b.WriteString(` (when offsets (jsown:extend-js js ("sourceOffsets" offsets)))`)
	b.WriteString(` js)))`)

	// ------------------------------------------------------------------
	// Main: process all fragments.
	// Clear the request-local memo before each batch.
	// ------------------------------------------------------------------
	b.WriteString(` (setf *langkit-root-memo* (make-hash-table :test 'equal))`)
	b.WriteString(` (setf *langkit-warnings* nil)`)
	b.WriteString(` (handler-case (progn`)
	b.WriteString(` (jsown:to-json`)
	b.WriteString(` (let ((result (jsown:new-js ("adapterVersion" 1) ("fragments" nil))))`)
	b.WriteString(` (let ((fragment-results nil))`)

	// Fragments travel as quoted data, which the reader takes in without
	// compiling anything per fragment.
	b.WriteString(` (dolist (fragment '(`)
	for _, frag := range input.Fragments {
		b.WriteString(fmt.Sprintf(`(%d . "%s")`, frag.ID, escapeLispString(frag.Text)))
	}
	b.WriteString(`))`)
	b.WriteString(fmt.Sprintf(` (push (langkit-analyze-fragment (car fragment) (cdr fragment) %d) fragment-results))`, limit))

	b.WriteString(` (jsown:extend-js result ("fragments" (nreverse fragment-results))))`)
	b.WriteString(` (when *langkit-warnings* (jsown:extend-js result ("warnings" (nreverse *langkit-warnings*))))`)
	b.WriteString(` result)))`)
	b.WriteString(` (error (e) (jsown:to-json (jsown:new-js ("adapterError" (princ-to-string e))))))`)
	b.WriteString(`)`)

	return b.String()
}

// writeRootLispDefinitions writes the Lisp that resolves and verifies
// dictionary roots: the conjugation walk, root verification against the
// kana and kanji records and their reading restrictions, canonical spelling
// and gloss. Document analysis and lemma resolution share it, so a lemma
// proposed by a reviewer is verified exactly like an analyzed word.
func writeRootLispDefinitions(b *strings.Builder) {
	b.WriteString(` (defvar *langkit-root-memo* (make-hash-table :test 'equal))`)
	b.WriteString(` (defvar *langkit-warnings* nil)`)
	b.WriteString(` (defun langkit-warn (control &rest args) (push (apply #'format nil control args) *langkit-warnings*))`)
	b.WriteString(` (defun langkit-kana-key (text) (ichiran::as-hiragana (ichiran::normalize (copy-seq (ichiran/dict::strip-hints text)) :context :kana)))`)
	b.WriteString(` (defun langkit-sort-readings (records) (sort records (lambda (a b) (let ((oa (ichiran/dict::ord a)) (ob (ichiran/dict::ord b)) (ta (ichiran/dict::text a)) (tb (ichiran/dict::text b))) (or (< oa ob) (and (= oa ob) (or (string< ta tb) (and (equal ta tb) (< (ichiran/dict::id a) (ichiran/dict::id b))))))))))`)

	// ==================== langkit-walk-conj ====================
	// Per-branch visited list.  Kana evidence required.  No old-evidence fallback.
	b.WriteString(` (defun langkit-walk-conj (seq conj-ids from-filter sp kn visited hops)`)
	b.WriteString(` (when (>= hops 64) (langkit-warn "Conjugation hop limit at Seq ~A; leaving branch unresolved" seq) (return-from langkit-walk-conj nil))`)
	b.WriteString(` (ichiran/dict::with-connection ichiran/dict::*connection*`)
	b.WriteString(` (let ((conjs (cond`)
	b.WriteString(` (from-filter (postmodern:select-dao 'ichiran/dict::conjugation (:and (:= 'ichiran/dict::seq seq) (:= 'ichiran/dict::from from-filter))))`)
	b.WriteString(` ((and conj-ids (listp conj-ids)) (postmodern:select-dao 'ichiran/dict::conjugation (:and (:= 'ichiran/dict::seq seq) (:in 'ichiran/dict::id (:set conj-ids)))))`)
	b.WriteString(` (t (postmodern:select-dao 'ichiran/dict::conjugation (:= 'ichiran/dict::seq seq))))))`)
	b.WriteString(` (let ((results nil))`)
	b.WriteString(` (dolist (conj conjs)`)
	b.WriteString(` (let* ((cid (ichiran/dict::id conj)) (visit-key (cons seq cid)))`)
	b.WriteString(` (when (member visit-key visited :test 'equal) (langkit-warn "Conjugation cycle at Seq ~A, conjugation ~A; leaving branch unresolved" seq cid))`)
	b.WriteString(` (unless (member visit-key visited :test 'equal)`)
	b.WriteString(` (let* ((new-visited (cons visit-key visited))`)
	b.WriteString(` (src-map (postmodern:query (:select 'text 'source-text :from 'conj-source-reading :where (:= 'conj-id cid)))))`)
	b.WriteString(` (let ((next-sp nil) (next-kn nil))`)
	b.WriteString(` (dolist (s sp) (dolist (pair src-map) (when (equal s (car pair)) (pushnew (cadr pair) next-sp :test 'equal))))`)
	b.WriteString(` (dolist (k kn) (dolist (pair src-map) (when (equal (langkit-kana-key k) (langkit-kana-key (car pair))) (pushnew (cadr pair) next-kn :test 'equal))))`)
	// Require kana evidence — abandon branch without it
	b.WriteString(` (when next-kn`)
	b.WriteString(` (let ((via (ichiran/dict::seq-via conj)))`)
	b.WriteString(` (if (or (eql via :null) (null via))`)
	// Terminal: emit (root-seq kana spelling?) triples
	b.WriteString(` (let ((root-seq (ichiran/dict::seq-from conj)))`)
	b.WriteString(` (dolist (rk next-kn)`)
	b.WriteString(` (if next-sp`)
	b.WriteString(` (dolist (rs next-sp) (push (list root-seq rk rs) results))`)
	b.WriteString(` (push (list root-seq rk nil) results))))`)
	// Via: recurse with matched evidence only (never old evidence)
	b.WriteString(` (let ((sub (langkit-walk-conj via nil (ichiran/dict::seq-from conj) next-sp next-kn new-visited (1+ hops))))`)
	b.WriteString(` (setf results (nconc results sub)))`)
	// close: let-sub, if, let-via, when, let-next, let*-new-visited, unless, let*-cid, dolist
	b.WriteString(`))))))))`)
	// close: let-results, let-conjs, with-connection, defun
	b.WriteString(` results))))`)

	// ==================== langkit-verify-root ====================
	// Verify the recovered reading before choosing a stable dictionary spelling.
	b.WriteString(` (defun langkit-verify-root (root-seq recovered-kana recovered-spelling)`)
	// Kana is mandatory — spelling-only is not a verified root
	b.WriteString(` (unless recovered-kana (return-from langkit-verify-root nil))`)
	b.WriteString(` (ichiran/dict::with-connection ichiran/dict::*connection*`)
	b.WriteString(` (let ((root-entry (car (postmodern:select-dao 'ichiran/dict::entry (:= 'ichiran/dict::seq root-seq)))))`)
	b.WriteString(` (unless (and root-entry (ichiran/dict::root-p root-entry)) (return-from langkit-verify-root nil))`)
	// Look up the kana record — required
	b.WriteString(` (let ((kana-rec (find (langkit-kana-key recovered-kana) (langkit-sort-readings (postmodern:select-dao 'ichiran/dict::kana-text (:= 'ichiran/dict::seq root-seq))) :key (lambda (r) (langkit-kana-key (ichiran/dict::text r))) :test #'equal)))`)
	b.WriteString(` (unless kana-rec (return-from langkit-verify-root nil))`)
	// Kanji record — optional, for restricted-readings verification only
	b.WriteString(` (let ((kanji-rec (when recovered-spelling (car (postmodern:select-dao 'ichiran/dict::kanji-text (:and (:= 'ichiran/dict::seq root-seq) (:= 'ichiran/dict::text recovered-spelling)))))))`)
	b.WriteString(` (when (and recovered-spelling (null kanji-rec) (not (equal (langkit-kana-key recovered-spelling) (langkit-kana-key recovered-kana)))) (return-from langkit-verify-root nil))`)
	// Restricted readings
	b.WriteString(` (let ((restricted (postmodern:query (:select 'reading 'text :from 'restricted-readings :where (:= 'seq root-seq)))))`)
	b.WriteString(` (when (and kanji-rec restricted)`)
	b.WriteString(` (unless (ichiran/dict::match-kana-kanji kana-rec kanji-rec restricted)`)
	b.WriteString(` (return-from langkit-verify-root nil)))`)
	// Canonical lemma — always by dictionary ord, never from occurrence
	b.WriteString(` (let ((dict-kana (ichiran/dict::strip-hints (ichiran/dict::text kana-rec))) (lemma nil) (lemma-rec nil))`)
	b.WriteString(` (cond`)
	b.WriteString(` ((ichiran/dict::nokanji kana-rec) (setf lemma dict-kana))`)
	b.WriteString(` ((= (ichiran/dict::n-kanji root-entry) 0) (setf lemma dict-kana))`)
	b.WriteString(` ((postmodern:select-dao 'ichiran/dict::sense-prop (:and (:= 'ichiran/dict::seq root-seq) (:= 'ichiran/dict::tag "misc") (:= 'ichiran/dict::text "uk"))) (setf lemma dict-kana))`)
	b.WriteString(` (t`)
	b.WriteString(` (let ((kanji-recs (langkit-sort-readings (postmodern:select-dao 'ichiran/dict::kanji-text (:= 'ichiran/dict::seq root-seq)))))`)
	b.WriteString(` (dolist (kt kanji-recs)`)
	b.WriteString(` (when (ichiran/dict::match-kana-kanji kana-rec kt restricted)`)
	b.WriteString(` (setf lemma (ichiran/dict::text kt) lemma-rec kt) (return)))`)
	b.WriteString(` (unless lemma (setf lemma dict-kana)))))`)
	// A sense restricted to another spelling must not leak through a shared kana.
	b.WriteString(` (let ((gloss (langkit-root-gloss root-seq kana-rec lemma-rec)))`)
	b.WriteString(` (let ((js (jsown:new-js ("dictionarySeq" root-seq) ("lemma" (or lemma "")) ("kana" dict-kana))))`)
	b.WriteString(` (when gloss (jsown:extend-js js ("gloss" gloss)))`)
	// close: let-js, let-gloss, let-lemma, let-restricted, let-kanji-rec, let-kana-rec, let-root-entry, with-connection, defun
	b.WriteString(` js)))))))))`)
	b.WriteString(` (defun langkit-root-gloss (seq kana-rec lemma-rec)`)
	b.WriteString(` (loop with inherited-pos = "[]" for (pos gloss props) in (ichiran/dict::get-senses seq)`)
	b.WriteString(` do (unless (equal pos "[]") (setf inherited-pos pos))`)
	b.WriteString(` when (and (let ((readings (cdr (assoc "stagr" props :test #'equal)))) (or (null readings) (member (ichiran/dict::text kana-rec) readings :test #'equal)))`)
	b.WriteString(` (let ((spellings (cdr (assoc "stagk" props :test #'equal)))) (or (null spellings) (if lemma-rec (member (ichiran/dict::text lemma-rec) spellings :test #'equal) (ichiran/dict::match-sense-restrictions seq props kana-rec)))))`)
	b.WriteString(` collect (let ((js (jsown:new-js ("pos" inherited-pos) ("gloss" gloss))) (info (cdr (assoc "s_inf" props :test #'equal))) (fields (cdr (assoc "field" props :test #'equal))))`)
	b.WriteString(` (when info (jsown:extend-js js ("info" (format nil "~{~A~^; ~}" info))))`)
	b.WriteString(` (when fields (jsown:extend-js js ("field" (format nil "{~{~A~^,~}}" fields)))) js)))`)
}

// parseDocumentResult parses the versioned envelope JSON from AnalyzeDocument.
// Malformed envelopes and unsupported versions return errors; they are not
// silently degraded.
func parseDocumentResult(data []byte) (*DocumentResult, error) {
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("invalid JSON envelope: %w", err)
	}
	if message, ok := raw["adapterError"].(string); ok {
		return nil, fmt.Errorf("Ichiran document adapter failed: %s", message)
	}

	// Version check — reject unsupported versions and non-integer values.
	versionRaw, ok := raw["adapterVersion"].(float64)
	if !ok {
		return nil, fmt.Errorf("missing adapterVersion in envelope")
	}
	if versionRaw != float64(int(versionRaw)) {
		return nil, fmt.Errorf("adapterVersion %v is not an integer", versionRaw)
	}
	if int(versionRaw) != adapterVersion {
		return nil, fmt.Errorf("unsupported adapter version %d (expected %d)", int(versionRaw), adapterVersion)
	}

	result := &DocumentResult{
		AdapterVersion: int(versionRaw),
	}
	if value := raw["warnings"]; value != nil {
		warnings, ok := value.([]interface{})
		if !ok {
			return nil, fmt.Errorf("warnings is not an array")
		}
		for _, value := range warnings {
			warning, ok := value.(string)
			if !ok {
				return nil, fmt.Errorf("adapter warning is not a string")
			}
			result.Warnings = append(result.Warnings, warning)
		}
	}

	// Distinguish absent/null fragments (valid empty) from wrong type.
	fragRaw, exists := raw["fragments"]
	if !exists || fragRaw == nil {
		return result, nil
	}
	fragments, ok := fragRaw.([]interface{})
	if !ok {
		return nil, fmt.Errorf("fragments is not an array (got %T)", fragRaw)
	}

	ids := make(map[int]bool, len(fragments))
	for fragIdx, fRaw := range fragments {
		fragMap, ok := fRaw.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("fragment %d: not a JSON object", fragIdx)
		}

		id, err := documentInt(fragMap, "id")
		if err != nil {
			return nil, fmt.Errorf("fragment %d: %w", fragIdx, err)
		}
		if ids[id] {
			return nil, fmt.Errorf("duplicate fragment ID %d", id)
		}
		ids[id] = true
		source, sourceOK := fragMap["sourceText"].(string)
		analysis, analysisOK := fragMap["analysisText"].(string)
		if !sourceOK || !analysisOK {
			return nil, fmt.Errorf("fragment %d: sourceText and analysisText must be strings", id)
		}
		frag := FragmentResult{ID: id, SourceText: source, AnalysisText: analysis}
		if offsetsRaw := fragMap["sourceOffsets"]; offsetsRaw != nil {
			offsets, err := parseSourceOffsets(offsetsRaw, source, analysis)
			if err != nil {
				result.Warnings = append(result.Warnings,
					fmt.Sprintf("fragment %d: source offsets ignored: %v", id, err))
			} else {
				frag.SourceOffsets = offsets
			}
		}

		segRaw, segExists := fragMap["segments"]
		if segExists && segRaw != nil {
			segArr, ok := segRaw.([]interface{})
			if !ok {
				return nil, fmt.Errorf("fragment %d: segments is not an array", frag.ID)
			}
			for segIdx, sRaw := range segArr {
				seg, err := parseSegmentResult(sRaw)
				if err != nil {
					return nil, fmt.Errorf("fragment %d segment %d: %w", frag.ID, segIdx, err)
				}
				frag.Segments = append(frag.Segments, *seg)
			}
		}

		// Validate positions against analysisText — reject on mismatch.
		if err := validateFragmentPositions(&frag); err != nil {
			return nil, fmt.Errorf("fragment %d: %w", frag.ID, err)
		}

		result.Fragments = append(result.Fragments, frag)
	}

	return result, nil
}

// parseSourceOffsets reads a fragment's map from analysis text to source
// text: one source offset per analysis rune, then the source length, never
// decreasing and never past the source.
func parseSourceOffsets(raw interface{}, source, analysis string) ([]int, error) {
	items, ok := raw.([]interface{})
	if !ok {
		return nil, fmt.Errorf("not an array")
	}
	sourceRunes := utf8.RuneCountInString(source)
	if len(items) != utf8.RuneCountInString(analysis)+1 {
		return nil, fmt.Errorf("%d offsets for %d analysis runes", len(items), utf8.RuneCountInString(analysis))
	}
	offsets := make([]int, len(items))
	for i, item := range items {
		n, ok := item.(float64)
		if !ok || n != math.Trunc(n) || n < 0 || int(n) > sourceRunes {
			return nil, fmt.Errorf("offset %d is not a source position", i)
		}
		offsets[i] = int(n)
		if i > 0 && offsets[i] < offsets[i-1] {
			return nil, fmt.Errorf("offsets decrease at %d", i)
		}
	}
	if offsets[len(offsets)-1] != sourceRunes {
		return nil, fmt.Errorf("offsets end at %d, not at the source length %d", offsets[len(offsets)-1], sourceRunes)
	}
	return offsets, nil
}

// JSON numbers must not be silently truncated into source positions or IDs.
func documentInt(m map[string]interface{}, field string) (int, error) {
	n, ok := m[field].(float64)
	if !ok || math.IsNaN(n) || math.IsInf(n, 0) || n != math.Trunc(n) || n >= float64(int(^uint(0)>>1)) || n < -float64(int(^uint(0)>>1)) {
		return 0, fmt.Errorf("%s must be an in-range integer", field)
	}
	return int(n), nil
}

// parseSegmentResult parses one segment from the envelope JSON.
func parseSegmentResult(raw interface{}) (*SegmentResult, error) {
	segMap, ok := raw.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("segment is not a JSON object")
	}

	index, err := documentInt(segMap, "index")
	if err != nil {
		return nil, err
	}
	seg := &SegmentResult{Index: index}
	kind, ok := segMap["kind"].(string)
	if !ok {
		return nil, fmt.Errorf("segment %d: missing kind", seg.Index)
	}
	seg.Kind = SegmentKind(kind)
	if seg.Kind != SegmentWord && seg.Kind != SegmentLiteral {
		return nil, fmt.Errorf("segment %d: unsupported kind %q", seg.Index, kind)
	}
	seg.Start, err = documentInt(segMap, "start")
	if err != nil {
		return nil, err
	}
	seg.End, err = documentInt(segMap, "end")
	if err != nil {
		return nil, err
	}
	if seg.End < seg.Start {
		return nil, fmt.Errorf("segment %d: end %d < start %d", seg.Index, seg.End, seg.Start)
	}
	seg.Text, ok = segMap["text"].(string)
	if !ok {
		return nil, fmt.Errorf("segment %d: text must be a string", seg.Index)
	}
	if seg.Kind == SegmentLiteral {
		if interps := segMap["interpretations"]; interps != nil {
			array, ok := interps.([]interface{})
			if !ok || len(array) != 0 {
				return nil, fmt.Errorf("literal segment %d has interpretations", seg.Index)
			}
		}
	}

	if seg.Kind == SegmentWord {
		interpRaw, interpExists := segMap["interpretations"]
		if interpExists && interpRaw != nil {
			interps, ok := interpRaw.([]interface{})
			if !ok {
				return nil, fmt.Errorf("segment %d: interpretations is not an array", seg.Index)
			}
			for interpIdx, iRaw := range interps {
				interp, err := parseInterpretationResult(iRaw)
				if err != nil {
					return nil, fmt.Errorf("segment %d interpretation %d: %w", seg.Index, interpIdx, err)
				}
				seg.Interpretations = append(seg.Interpretations, *interp)
			}
		}
	}

	return seg, nil
}

// parseInterpretationResult parses one interpretation from the envelope.
// Reuses G1's parseWordNode for token parsing.
func parseInterpretationResult(raw interface{}) (*InterpretationResult, error) {
	interpMap, ok := raw.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("interpretation is not a JSON object")
	}

	interp := &InterpretationResult{}
	if score, ok := interpMap["score"].(float64); ok {
		interp.Score = int(score)
	}

	tokRaw, tokExists := interpMap["tokens"]
	if tokExists && tokRaw != nil {
		tokenArray, ok := tokRaw.([]interface{})
		if !ok {
			return nil, fmt.Errorf("tokens is not an array")
		}
		for tokIdx, tRaw := range tokenArray {
			tokMap, ok := tRaw.(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("token %d: not a JSON object", tokIdx)
			}

			tok, err := parseEnrichedToken(tokMap)
			if err != nil {
				return nil, fmt.Errorf("token %d: %w", tokIdx, err)
			}
			interp.Tokens = append(interp.Tokens, tok)
		}
	}

	return interp, nil
}

// parseEnrichedToken parses a single enriched token from the document
// envelope, including root candidates, recursive compound children, and
// alternative candidates with their own root candidates.
func parseEnrichedToken(tokMap map[string]interface{}) (*JSONToken, error) {
	return parseEnrichedTokenAt(tokMap, nil)
}

func parseEnrichedTokenAt(tokMap map[string]interface{}, componentPath []int) (*JSONToken, error) {
	romaji, _ := tokMap["romaji"].(string)
	for _, field := range []string{"start", "end"} {
		if tokMap[field] != nil {
			if _, err := documentInt(tokMap, field); err != nil {
				return nil, err
			}
		}
	}
	for _, field := range []string{"rootCandidates", "components", "alternative"} {
		if value := tokMap[field]; value != nil {
			if _, ok := value.([]interface{}); !ok {
				// Scalar nodes retain Ichiran's false alternative flag.
				if flag, ok := value.(bool); field == "alternative" && ok && !flag {
					continue
				}
				return nil, fmt.Errorf("%s is not an array", field)
			}
		}
	}

	token, err := parseWordNode(tokMap, romaji, nil, "")
	if err != nil {
		return nil, err
	}
	token.ComponentPath = append([]int(nil), componentPath...)
	if value, exists := tokMap["spanInherited"]; exists {
		inherited, ok := value.(bool)
		if !ok {
			return nil, fmt.Errorf("spanInherited is not a boolean")
		}
		token.SpanInherited = inherited
	}

	// Parse root candidates at this level — reject malformed entries.
	if rootsRaw, ok := tokMap["rootCandidates"].([]interface{}); ok {
		for i, rootRaw := range rootsRaw {
			rootMap, ok := rootRaw.(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("rootCandidate %d: not a JSON object", i)
			}
			rc, err := parseRootCandidate(rootMap)
			if err != nil {
				return nil, fmt.Errorf("rootCandidate %d: %w", i, err)
			}
			token.RootCandidates = append(token.RootCandidates, rc)
		}
	}

	// Recursively parse enriched compound children (which may have their
	// own root candidates, romaji, etc.).
	if comps, ok := tokMap["components"].([]interface{}); ok {
		var enrichedComps []JSONToken
		for i, compRaw := range comps {
			compMap, ok := compRaw.(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("compound child %d: not a JSON object", i)
			}
			path := append(append([]int(nil), componentPath...), i)
			child, err := parseEnrichedTokenAt(compMap, path)
			if err != nil {
				return nil, fmt.Errorf("compound child %d: %w", i, err)
			}
			enrichedComps = append(enrichedComps, *child)
		}
		if len(enrichedComps) > 0 {
			token.Components = enrichedComps
		}
	}

	// Recursively parse enriched alternatives.
	if alts, ok := tokMap["alternative"].([]interface{}); ok {
		var enrichedAlts []JSONToken
		for i, altRaw := range alts {
			altMap, ok := altRaw.(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("alternative %d: not a JSON object", i)
			}
			alt, err := parseEnrichedTokenAt(altMap, componentPath)
			if err != nil {
				return nil, fmt.Errorf("alternative %d: %w", i, err)
			}
			enrichedAlts = append(enrichedAlts, *alt)
		}
		if len(enrichedAlts) > 0 {
			token.Alternative = enrichedAlts
		}
	}

	return token, nil
}

// parseRootCandidate extracts a RootCandidate from a raw JSON map.
// Required fields: dictionarySeq (nonzero), kana (nonempty).
func parseRootCandidate(m map[string]interface{}) (RootCandidate, error) {
	rc := RootCandidate{}
	seq, err := documentInt(m, "dictionarySeq")
	if err != nil || seq <= 0 {
		return rc, fmt.Errorf("dictionarySeq must be a positive integer")
	}
	rc.DictionarySeq = seq

	kana, ok := m["kana"].(string)
	if !ok || kana == "" {
		return rc, fmt.Errorf("missing or empty kana")
	}
	rc.Kana = kana

	lemma, ok := m["lemma"].(string)
	if !ok || strings.TrimSpace(lemma) == "" {
		return rc, fmt.Errorf("missing or empty lemma")
	}
	rc.Lemma = lemma
	if gloss := m["gloss"]; gloss != nil {
		glossArray, ok := gloss.([]interface{})
		if !ok {
			return rc, fmt.Errorf("root gloss is not an array")
		}
		for i, g := range glossArray {
			gm, ok := g.(map[string]interface{})
			if !ok {
				return rc, fmt.Errorf("root gloss %d is not an object", i)
			}
			if _, ok := gm["gloss"].(string); !ok {
				return rc, fmt.Errorf("root gloss %d has no gloss string", i)
			}
			if _, ok := gm["pos"].(string); !ok {
				return rc, fmt.Errorf("root gloss %d has no POS string", i)
			}
			rc.Gloss = append(rc.Gloss, parseGlossEntry(gm))
		}
	}
	return rc, nil
}

// validateFragmentPositions validates that segment positions are consistent
// with the analysisText. Positions are half-open rune offsets.
func validateFragmentPositions(frag *FragmentResult) error {
	runeCount := utf8.RuneCountInString(frag.AnalysisText)
	runes := []rune(frag.AnalysisText)

	lastEnd := 0
	for i, seg := range frag.Segments {
		if seg.Start < 0 || seg.End < seg.Start || seg.End > runeCount {
			return fmt.Errorf("segment %d: invalid position [%d, %d) in text of %d runes",
				seg.Index, seg.Start, seg.End, runeCount)
		}
		if seg.Index != i || seg.Start != lastEnd || seg.End == seg.Start {
			return fmt.Errorf("segment %d: segments must be contiguous and indexed in order", seg.Index)
		}
		lastEnd = seg.End

		// Verify substring agreement
		expectedText := string(runes[seg.Start:seg.End])
		if expectedText != seg.Text {
			return fmt.Errorf("segment %d: position [%d, %d) yields %q but segment text is %q",
				seg.Index, seg.Start, seg.End, expectedText, seg.Text)
		}
		if seg.Kind == SegmentWord && len(seg.Interpretations) == 0 {
			return fmt.Errorf("word segment %d has no interpretations", seg.Index)
		}
		for j, interp := range seg.Interpretations {
			tokenEnd := seg.Start
			for k, tok := range interp.Tokens {
				if err := validateDocumentToken(tok, runes, seg.Start, seg.End, nil); err != nil {
					return fmt.Errorf("segment %d interpretation %d token %d: %w", seg.Index, j, k, err)
				}
				if *tok.Start != tokenEnd {
					return fmt.Errorf("segment %d interpretation %d: token spans have a gap or overlap", seg.Index, j)
				}
				tokenEnd = *tok.End
			}
			if tokenEnd != seg.End {
				return fmt.Errorf("segment %d interpretation %d does not cover the segment", seg.Index, j)
			}
		}
	}
	if lastEnd != runeCount {
		return fmt.Errorf("segments cover %d of %d analysis runes", lastEnd, runeCount)
	}

	return nil
}

func validateDocumentToken(tok *JSONToken, runes []rune, start, end int, parent *JSONToken) error {
	if tok == nil || tok.Start == nil || tok.End == nil {
		return fmt.Errorf("missing token position")
	}
	ts, te := *tok.Start, *tok.End
	if ts < start || te <= ts || te > end {
		return fmt.Errorf("invalid token position [%d, %d) within [%d, %d)", ts, te, start, end)
	}
	if tok.SpanInherited {
		if parent == nil || ts != *parent.Start || te != *parent.End {
			return fmt.Errorf("inherited token span does not match its parent")
		}
	} else if string(runes[ts:te]) != tok.Surface {
		return fmt.Errorf("token span yields %q but token text is %q", string(runes[ts:te]), tok.Surface)
	}
	for i := range tok.Components {
		if err := validateDocumentToken(&tok.Components[i], runes, ts, te, tok); err != nil {
			return fmt.Errorf("component %d: %w", i, err)
		}
	}
	for i := range tok.Alternative {
		if err := validateDocumentToken(&tok.Alternative[i], runes, ts, te, tok); err != nil {
			return fmt.Errorf("alternative %d: %w", i, err)
		}
	}
	return nil
}
