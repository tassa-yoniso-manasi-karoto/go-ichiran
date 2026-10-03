package ichiran

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ReadingQuery names a written word and one reading of it.
type ReadingQuery struct {
	Text    string
	Reading string
}

// alignBatchSize bounds the queries of one Ichiran call, so that the
// expression fits in one command-line argument.
const alignBatchSize = 400

// AlignReadings aligns the reading of each query with the kanji of its
// text, the way Analyze aligns an analyzed word with its own reading
// (JSONToken.KanjiReadings). Kana compare whatever their script: a
// hiragana reading aligns with katakana in the text, and the entries keep
// the text as written. A query whose text has no kanji, or whose reading
// does not fit the text, gets no readings. Adapter and database failures
// are errors.
func (im *IchiranManager) AlignReadings(ctx context.Context, queries []ReadingQuery) ([][]KanjiReading, error) {
	results := make([][]KanjiReading, len(queries))
	for start := 0; start < len(queries); start += alignBatchSize {
		end := min(start+alignBatchSize, len(queries))
		batch, err := im.alignReadingBatch(ctx, queries[start:end])
		if err != nil {
			return nil, err
		}
		copy(results[start:end], batch)
	}
	return results, nil
}

func (im *IchiranManager) alignReadingBatch(ctx context.Context, queries []ReadingQuery) ([][]KanjiReading, error) {
	queryCtx, cancel := context.WithTimeout(ctx, im.QueryTimeout)
	defer cancel()

	var b strings.Builder
	b.WriteString(`(progn`)
	b.WriteString(` (ql:quickload :jsown :silent t)`)
	b.WriteString(` (handler-case`)
	b.WriteString(` (ichiran/dict::with-connection ichiran/dict::*connection*`)
	b.WriteString(` (jsown:to-json (jsown:new-js ("adapterVersion" 1) ("results" (mapcar (lambda (query)`)
	b.WriteString(` (handler-case (ichiran/kanji:match-readings-json (first query) (second query)) (error () nil))) '(`)
	// Queries travel as quoted data, katakana folded so that a hiragana
	// reading matches it.
	for _, query := range queries {
		b.WriteString(fmt.Sprintf(`("%s" "%s")`,
			escapeLispString(foldKatakana(query.Text)),
			escapeLispString(foldKatakana(query.Reading))))
	}
	b.WriteString(`))))))`)
	b.WriteString(` (error (e) (jsown:to-json (jsown:new-js ("adapterError" (princ-to-string e))))))`)
	b.WriteString(`)`)

	output, err := im.runLispJSON(queryCtx, b.String())
	if err != nil {
		return nil, err
	}
	var envelope struct {
		AdapterVersion int               `json:"adapterVersion"`
		AdapterError   string            `json:"adapterError"`
		Results        []json.RawMessage `json:"results"`
	}
	if err := json.Unmarshal(output, &envelope); err != nil {
		return nil, fmt.Errorf("invalid alignment envelope: %w", err)
	}
	if envelope.AdapterError != "" {
		return nil, fmt.Errorf("Ichiran reading alignment failed: %s", envelope.AdapterError)
	}
	if envelope.AdapterVersion != adapterVersion {
		return nil, fmt.Errorf("unsupported adapter version %d", envelope.AdapterVersion)
	}
	if len(envelope.Results) != len(queries) {
		return nil, fmt.Errorf("alignment returned %d results for %d queries", len(envelope.Results), len(queries))
	}

	results := make([][]KanjiReading, len(queries))
	for i, raw := range envelope.Results {
		var value interface{}
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, fmt.Errorf("alignment result %d: %w", i, err)
		}
		switch entries := value.(type) {
		case nil, bool:
			// jsown may print an empty list as null or false.
		case []interface{}:
			results[i] = restoreWrittenText(queries[i].Text, parseKanjiReadings(entries))
		default:
			return nil, fmt.Errorf("alignment result %d is not an array", i)
		}
	}
	return results, nil
}

// foldKatakana writes katakana as hiragana, one character for one, so
// that positions in the folded text are positions in the original. ヵ and
// ヶ stay: Ichiran reads ヶ as a kanji.
func foldKatakana(s string) string {
	return strings.Map(func(r rune) rune {
		if (r >= 'ァ' && r <= 'ヴ') || r == 'ヽ' || r == 'ヾ' {
			return r - 0x60
		}
		return r
	}, s)
}

// restoreWrittenText gives the aligned entries the characters of text at
// their positions, as written. Entries that do not cover the text exactly
// give no alignment.
func restoreWrittenText(text string, readings []KanjiReading) []KanjiReading {
	runes := []rune(text)
	pos := 0
	for i := range readings {
		entry := &readings[i]
		part := &entry.Text
		if entry.Kanji != "" {
			part = &entry.Kanji
		}
		n := utf8.RuneCountInString(*part)
		if n == 0 || pos+n > len(runes) {
			return nil
		}
		*part = string(runes[pos : pos+n])
		pos += n
	}
	if pos != len(runes) {
		return nil
	}
	return readings
}

// UnalignedReadings returns the kanji readings that make selective
// transliteration handle a word whose reading could not be aligned like
// an irregular reading: kept as written while every kanji in it is within
// freqThreshold, written as its reading otherwise. Other characters do not
// count, so 12歳 stays as written while 歳 is within the threshold.
func UnalignedReadings(text, reading string, freqThreshold int) []KanjiReading {
	if reading == "" {
		return nil
	}
	for _, r := range text {
		if !unicode.Is(unicode.Han, r) {
			continue
		}
		if rank := slices.Index(kanjiFreqSlice, string(r)) + 1; rank == 0 || rank > freqThreshold {
			return []KanjiReading{{Kanji: text, Reading: reading, Type: "irr"}}
		}
	}
	return nil
}
