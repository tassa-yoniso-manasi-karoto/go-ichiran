package ichiran

import "slices"

// JSONToken represents a single token with all its analysis information
type JSONToken struct {
	Surface       string         `json:"text"` // Original text
	IsLexical     bool           // Whether this is a Japanese token or non-Japanese text
	Reading       string         `json:"reading"` // Reading with kanji and kana
	Kana          string         `json:"kana"`    // Kana reading
	Romaji        string         // Romanized form from ichiran
	Score         int            `json:"score"`          // Analysis score
	Seq           int            `json:"seq"`            // Sequence number
	Gloss         []Gloss        `json:"gloss"`          // English meanings
	Conj          []Conj         `json:"conj,omitempty"` // Conjugation information
	Alternative   []JSONToken    `json:"alternative"`    // Alternative interpretations
	Compound      []string       `json:"compound"`       // Delineable elements of compound expressions
	Components    []JSONToken    `json:"components"`     // Details of delineable elements of compound expressions
	Raw           []byte         `json:"-"`              // Raw JSON for future processing
	KanjiReadings []KanjiReading `json:"-"`              // Parsed kanji-kana mappings

	// Structural fields — preserve the full Ichiran metadata.
	TrueText     string        `json:"truetext,omitempty"`     // Ichiran's truetext (pre-normalization surface)
	ConjSelector *ConjSelector `json:"conjSelector,omitempty"` // Conjugation selector from interpretation
	IsPrimary    *bool         `json:"primary,omitempty"`      // Primary flag for compound children
	CounterData  *CounterData  `json:"counter,omitempty"`      // Counter expression metadata
	Start        *int          `json:"start,omitempty"`        // Half-open start position (nullable: absent ≠ 0)
	End          *int          `json:"end,omitempty"`          // Half-open end position (nullable: absent ≠ 0)
	LexicalType  string        `json:"type,omitempty"`         // Ichiran's word type (KANA, KANJI, etc.)

	// G2: Root resolution results — resolved dictionary roots for this leaf.
	RootCandidates []RootCandidate `json:"rootCandidates,omitempty"`
	// Positionless compound children share their parent's span, not its identity.
	SpanInherited bool  `json:"spanInherited,omitempty"`
	ComponentPath []int `json:"componentPath,omitempty"`
}

// ConjSelector identifies which conjugation branch was selected for this
// interpretation. IsRoot means the Seq is already a dictionary root. IDs
// restricts which branches are valid. A nil ConjSelector means unspecified.
type ConjSelector struct {
	IsRoot bool  `json:"isRoot,omitempty"`
	IDs    []int `json:"ids,omitempty"`
}

// CounterData preserves Ichiran's counter expression metadata.
type CounterData struct {
	Value   string `json:"value,omitempty"`
	Ordinal bool   `json:"ordinal,omitempty"`
}

// in case of multiple alternative, jsonTokenCore represents the essential information that are shared,
// that will spearhead the JSONToken for consistency's sake
type jsonTokenCore struct {
	Surface   string `json:"text"` // Original text
	IsLexical bool   // Whether this is a Japanese token or non-Japanese text
	Reading   string `json:"reading"` // Reading with kanji and kana
	Kana      string `json:"kana"`    // Kana reading
	Romaji    string // Romanized form from ichiran
	Score     int    `json:"score"` // Analysis score
}

// extractCore returns only the core fields from a JSONToken
func extractCore(token JSONToken) jsonTokenCore {
	return jsonTokenCore{
		Surface:   token.Surface,
		IsLexical: token.IsLexical,
		Reading:   token.Reading,
		Kana:      token.Kana,
		Romaji:    token.Romaji,
		Score:     token.Score,
	}
}

// applyCore applies the core fields to a JSONToken
func (token *JSONToken) applyCore(core jsonTokenCore) {
	token.Surface = core.Surface
	token.IsLexical = core.IsLexical
	token.Reading = core.Reading
	token.Kana = core.Kana
	token.Romaji = core.Romaji
	token.Score = core.Score
}

// cloneConjs deep-copies a slice of Conj, including recursive Via chains.
func cloneConjs(conjs []Conj) []Conj {
	if conjs == nil {
		return nil
	}
	out := make([]Conj, len(conjs))
	for i, c := range conjs {
		out[i] = c
		out[i].Prop = slices.Clone(c.Prop)
		out[i].Gloss = slices.Clone(c.Gloss)
		out[i].Via = cloneConjs(c.Via)
	}
	return out
}

// cloneJSONTokens deep-copies a slice of JSONToken, recursively cloning all
// nested slices and pointer fields so that no mutable storage is shared.
func cloneJSONTokens(tokens []JSONToken) []JSONToken {
	if tokens == nil {
		return nil
	}
	out := make([]JSONToken, len(tokens))
	for i, t := range tokens {
		out[i] = t
		out[i].Gloss = slices.Clone(t.Gloss)
		out[i].Conj = cloneConjs(t.Conj)
		out[i].Components = cloneJSONTokens(t.Components)
		out[i].Alternative = cloneJSONTokens(t.Alternative)
		out[i].Compound = slices.Clone(t.Compound)
		out[i].ComponentPath = slices.Clone(t.ComponentPath)
		out[i].KanjiReadings = slices.Clone(t.KanjiReadings)
		if t.RootCandidates != nil {
			rc := make([]RootCandidate, len(t.RootCandidates))
			for j, c := range t.RootCandidates {
				rc[j] = c
				rc[j].Gloss = slices.Clone(c.Gloss)
			}
			out[i].RootCandidates = rc
		}
		out[i].Raw = slices.Clone(t.Raw)
		if t.ConjSelector != nil {
			cs := *t.ConjSelector
			cs.IDs = slices.Clone(t.ConjSelector.IDs)
			out[i].ConjSelector = &cs
		}
		if t.IsPrimary != nil {
			v := *t.IsPrimary
			out[i].IsPrimary = &v
		}
		if t.CounterData != nil {
			cd := *t.CounterData
			out[i].CounterData = &cd
		}
		if t.Start != nil {
			v := *t.Start
			out[i].Start = &v
		}
		if t.End != nil {
			v := *t.End
			out[i].End = &v
		}
	}
	return out
}

// selectCandidate replaces all interpretation-dependent fields from src with
// deep copies, so that no mutable storage is shared between the selected
// token and the candidate it was copied from. The occurrence address
// (Surface, Start, End) and the Alternative list are NOT copied.
func (token *JSONToken) selectCandidate(src *JSONToken) {
	token.Kana = src.Kana
	token.Reading = src.Reading
	token.Romaji = src.Romaji
	token.Seq = src.Seq
	token.Score = src.Score
	token.Gloss = slices.Clone(src.Gloss)
	token.Conj = cloneConjs(src.Conj)
	token.Components = cloneJSONTokens(src.Components)
	token.Compound = slices.Clone(src.Compound)
	token.KanjiReadings = slices.Clone(src.KanjiReadings)
	if src.RootCandidates != nil {
		rc := make([]RootCandidate, len(src.RootCandidates))
		for j, c := range src.RootCandidates {
			rc[j] = c
			rc[j].Gloss = slices.Clone(c.Gloss)
		}
		token.RootCandidates = rc
	} else {
		token.RootCandidates = nil
	}
	token.TrueText = src.TrueText
	if src.ConjSelector != nil {
		cs := *src.ConjSelector
		cs.IDs = slices.Clone(src.ConjSelector.IDs)
		token.ConjSelector = &cs
	} else {
		token.ConjSelector = nil
	}
	if src.IsPrimary != nil {
		v := *src.IsPrimary
		token.IsPrimary = &v
	} else {
		token.IsPrimary = nil
	}
	if src.CounterData != nil {
		cd := *src.CounterData
		token.CounterData = &cd
	} else {
		token.CounterData = nil
	}
	token.LexicalType = src.LexicalType
	token.IsLexical = src.IsLexical
}

// JSONTokens is a slice of token pointers representing a complete analysis result.
type JSONTokens []*JSONToken

// AnalysisResult wraps the primary analysis with optional alternative
// segmentations returned when AnalyzeOptions.Limit > 1.
type AnalysisResult struct {
	Tokens       *JSONTokens            // Primary (highest-scoring) interpretation
	Alternatives []ScoredInterpretation // Other segmentations, scored and deduplicated
}

// ScoredInterpretation is an alternative sentence segmentation with its score.
type ScoredInterpretation struct {
	Tokens JSONTokens
	Score  int
}

// Gloss represents the English glosses and part of speech
type Gloss struct {
	Pos   string `json:"pos"`   // Part of speech
	Gloss string `json:"gloss"` // English meaning
	Info  string `json:"info"`  // Additional information
}

// Conj represents conjugation information
type Conj struct {
	Prop    []Prop  `json:"prop"`          // Conjugation properties
	Reading string  `json:"reading"`       // Base form reading
	Gloss   []Gloss `json:"gloss"`         // Base form meanings
	ReadOk  bool    `json:"readok"`        // Reading validity flag
	Via     []Conj  `json:"via,omitempty"` // Recursive via chain
	Fml     bool    `json:"fml,omitempty"` // Formal flag
}

// Prop represents grammatical properties
type Prop struct {
	Pos  string `json:"pos"`           // Part of speech
	Type string `json:"type"`          // Type of conjugation
	Neg  bool   `json:"neg"`           // Negation flag
	Fml  bool   `json:"fml,omitempty"` // Formal flag
}

// KanjiReading represents the reading information for a single kanji character
type KanjiReading struct {
	Kanji     string `json:"kanji"`     // The kanji character
	Reading   string `json:"reading"`   // The reading in hiragana
	Type      string `json:"type"`      // Reading type (ja_on, ja_kun)
	Link      bool   `json:"link"`      // Whether the reading links to adjacent characters
	Geminated string `json:"geminated"` // Geminated sound (っ) if present
	Stats     bool   `json:"stats"`     // Whether statistics are available
	Sample    int    `json:"sample"`    // Sample size for statistics
	Total     int    `json:"total"`     // Total occurrences
	Perc      string `json:"perc"`      // Percentage of usage
	Grade     int    `json:"grade"`     // School grade level
}

// TransliterationResult contains the complete transliteration output
type TransliterationResult struct {
	Text   string           // The final transliterated text
	Tokens []ProcessedToken // Detailed processing information
}

// ProcessedToken represents a single token's processing result
type ProcessedToken struct {
	Original string
	Result   string
	Status   ProcessingStatus
}

// intPtr returns a pointer to the given int value.
func intPtr(v int) *int {
	return &v
}

// boolPtr returns a pointer to the given bool value.
func boolPtr(v bool) *bool {
	return &v
}

// ===========================================================================
// G2: Structured Document Analysis Envelope
// ===========================================================================

// DocumentInput is the input for AnalyzeDocument. Each fragment has an
// integer ID and source text to be analyzed. Fragments are processed in
// order; IDs are echoed in results for correlation.
type DocumentInput struct {
	Fragments []FragmentInput
}

// FragmentInput is a single piece of text to analyze. ID is user-assigned
// and carried through to FragmentResult for correlation.
type FragmentInput struct {
	ID   int
	Text string
}

// DocumentOptions configures the AnalyzeDocument call.
type DocumentOptions struct {
	// Limit controls how many interpretation candidates ichiran returns
	// per word segment. Default is 5.
	Limit int
}

// DocumentResult is the versioned output envelope from AnalyzeDocument.
type DocumentResult struct {
	AdapterVersion int              `json:"adapterVersion"`
	Fragments      []FragmentResult `json:"fragments"`
	Warnings       []string         `json:"warnings,omitempty"`
}

// FragmentResult is one analyzed fragment in the document envelope.
type FragmentResult struct {
	ID           int             `json:"id"`
	SourceText   string          `json:"sourceText"`
	AnalysisText string          `json:"analysisText"`
	Segments     []SegmentResult `json:"segments"`

	// SourceOffsets maps AnalysisText back to SourceText, which Ichiran's
	// normalization changes (。 becomes ". ", half-width kana become
	// full-width). It holds, for each rune of AnalysisText, the rune offset
	// in SourceText it came from, then the rune length of SourceText, so
	// the analysis span [s, e) covers SourceText[SourceOffsets[s]:
	// SourceOffsets[e]]. It is nil when the adapter could not reproduce
	// Ichiran's normalization exactly.
	SourceOffsets []int `json:"sourceOffsets,omitempty"`
}

// SegmentKind distinguishes word segments from literal (punctuation/whitespace).
type SegmentKind string

const (
	SegmentWord    SegmentKind = "word"
	SegmentLiteral SegmentKind = "literal"
)

// SegmentResult is one segment produced by basic-split. Literal segments
// have no interpretations; word segments have one or more.
type SegmentResult struct {
	Index           int                    `json:"index"`
	Kind            SegmentKind            `json:"kind"`
	Start           int                    `json:"start"`
	End             int                    `json:"end"`
	Text            string                 `json:"text"`
	Interpretations []InterpretationResult `json:"interpretations,omitempty"`
}

// InterpretationResult is one possible analysis of a word segment.
// Each contains a score and the self-contained enriched tokens.
type InterpretationResult struct {
	Score  int          `json:"score"`
	Tokens []*JSONToken `json:"tokens"`
}

// RootCandidate is a dictionary root resolved from a conjugated/inflected
// occurrence. Each candidate has a verified dictionary Seq, canonical lemma,
// dictionary kana, and POS/gloss data. Multiple candidates represent
// alternative dictionary entries, not words in a compound.
type RootCandidate struct {
	DictionarySeq int     `json:"dictionarySeq"`
	Lemma         string  `json:"lemma"`
	Kana          string  `json:"kana"`
	Gloss         []Gloss `json:"gloss,omitempty"`
}
