package ichiran

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

// JSONTokens is a slice of token pointers representing a complete analysis result.
type JSONTokens []*JSONToken

// Gloss represents the English glosses and part of speech
type Gloss struct {
	Pos   string `json:"pos"`   // Part of speech
	Gloss string `json:"gloss"` // English meaning
	Info  string `json:"info"`  // Additional information
}

// Conj represents conjugation information
type Conj struct {
	Prop    []Prop  `json:"prop"`    // Conjugation properties
	Reading string  `json:"reading"` // Base form reading
	Gloss   []Gloss `json:"gloss"`   // Base form meanings
	ReadOk  bool    `json:"readok"`  // Reading validity flag
}

// Prop represents grammatical properties
type Prop struct {
	Pos  string `json:"pos"`  // Part of speech
	Type string `json:"type"` // Type of conjugation
	Neg  bool   `json:"neg"`  // Negation flag
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
