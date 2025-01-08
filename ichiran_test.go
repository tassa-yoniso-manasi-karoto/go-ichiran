package ichiran

import (
	"testing"
	"os"
	"github.com/stretchr/testify/assert"
)

// RUN THIS TEST FILE WITH: ICHIRAN_MANUAL_TEST=1 go test ichiran_test.go ichiran.go


func TestAnalyze(t *testing.T) {
	if os.Getenv("ICHIRAN_MANUAL_TEST") != "1" {
		t.Skip(`To run these tests, start the containers manually and set ICHIRAN_MANUAL_TEST=1
Example:
    docker compose up
    ICHIRAN_MANUAL_TEST=1 go test ichiran_test.go
`)
	}
	tests := []struct {
		name     string
		input    string
		wantErr  bool
		validate func(*testing.T, *JSONTokens)
	}{
		{
			name:    "basic sentence",
			input:   "私は学生です",
			wantErr: false,
			validate: func(t *testing.T, tokens *JSONTokens) {
				assert.Equal(t, 4, len(*tokens))
				assert.Equal(t, "私", (*tokens)[0].Surface)
				assert.Equal(t, "は", (*tokens)[1].Surface)
				assert.Equal(t, "学生", (*tokens)[2].Surface)
				assert.Equal(t, "です", (*tokens)[3].Surface)
			},
		},
		{
			name:    "compound verb",
			input:   "勉強しています",
			wantErr: false,
			validate: func(t *testing.T, tokens *JSONTokens) {
				assert.Equal(t, 1, len(*tokens))
				token := (*tokens)[0]
				assert.Equal(t, "勉強しています", token.Surface)
				assert.Equal(t, 3, len(token.Components))
				assert.Equal(t, "勉強", token.Components[0].Surface)
				assert.Equal(t, "して", token.Components[1].Surface)
				assert.Equal(t, "います", token.Components[2].Surface)
			},
		},
		{
			name:    "alternatives",
			input:   "ない",
			wantErr: false,
			validate: func(t *testing.T, tokens *JSONTokens) {
				assert.Equal(t, 1, len(*tokens))
				token := (*tokens)[0]
				assert.True(t, len(token.Alternative) > 0, "should have alternatives")
			},
		},
		{
			name:    "non-Japanese characters",
			input:   "Hello世界",
			wantErr: false,
			validate: func(t *testing.T, tokens *JSONTokens) {
				assert.Equal(t, 2, len(*tokens))
				assert.Equal(t, "Hello", (*tokens)[0].Surface)
				assert.False(t, (*tokens)[0].IsToken)
				assert.Equal(t, "世界", (*tokens)[1].Surface)
				assert.True(t, (*tokens)[1].IsToken)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens, err := Analyze(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.NotNil(t, tokens)
			tt.validate(t, tokens)
		})
	}
}

func TestJSONTokens_ToMorphemes(t *testing.T) {
	if os.Getenv("ICHIRAN_MANUAL_TEST") != "1" {
		t.Skip(`To run these tests, start the containers manually and set ICHIRAN_MANUAL_TEST=1
Example:
    docker compose up
    ICHIRAN_MANUAL_TEST=1 go test ichiran_test.go
`)
	}
	tokens, err := Analyze("勉強しています")
	assert.NoError(t, err)
	
	morphemes := tokens.ToMorphemes()
	assert.Equal(t, 3, len(morphemes))
	assert.Equal(t, "勉強", morphemes[0].Surface)
	assert.Equal(t, "して", morphemes[1].Surface)
	assert.Equal(t, "います", morphemes[2].Surface)
}

func TestJSONTokens_GlossParts(t *testing.T) {
	if os.Getenv("ICHIRAN_MANUAL_TEST") != "1" {
		t.Skip(`To run these tests, start the containers manually and set ICHIRAN_MANUAL_TEST=1
Example:
    docker compose up
    ICHIRAN_MANUAL_TEST=1 go test ichiran_test.go
`)
	}
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:  "basic word",
			input: "私",
			expected: []string{
				"私(I; me)",
			},
		},
		{
			name:  "word with alternatives",
			input: "ない",
			expected: []string{
				"ない (ALT1: to be; to exist; to live; to have; to be located; to be equipped with; to happen; to come about | ALT2: nonexistent; not being (there); unowned; not had; unpossessed; unique; not; impossible; won't happen; not; to not be; to have not)",
			},
		},
		{
			name:  "compound verb",
			input: "勉強しています",
			expected: []string{
				"勉強(study; diligence; working hard; experience; lesson (for the future); discount; price reduction)",
				"して(to do; to carry out; to perform; to cause to become; to make (into); to turn (into); to serve as; to act as; to work as; to wear (clothes, a facial expression, etc.); to judge as being; to view as being; to think of as; to treat as; to use as; to decide on; to choose; to be sensed (of a smell, noise, etc.); to be (in a state, condition, etc.); to be worth; to cost; to pass (of time); to elapse; to place, or raise, person A to a post or status B; to transform A to B; to make A into B; to exchange A for B; to make use of A for B; to view A as B; to handle A as if it were B; to feel A about B; verbalizing suffix (applies to nouns noted in this dictionary with the part of speech \"vs\"); creates a humble verb (after a noun prefixed with \"o\" or \"go\"); to be just about to; to be just starting to; to try to; to attempt to)",
				"います(to be (of animate objects); to exist; to stay; to be ...-ing; to have been ...-ing)",
			},
		},
		{
			name:  "mixed Japanese and non-Japanese",
			input: "Hello世界",
			expected: []string{
				"Hello",
				"世界(the world; society; the universe; sphere; circle; world; world-renowned; world-famous; realm governed by one Buddha; space)",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens, err := Analyze(tt.input)
			assert.NoError(t, err)
			
			parts := tokens.GlossParts()
			assert.Equal(t, tt.expected, parts)
		})
	}
}

func TestJSONTokens_Kana(t *testing.T) {
	if os.Getenv("ICHIRAN_MANUAL_TEST") != "1" {
		t.Skip(`To run these tests, start the containers manually and set ICHIRAN_MANUAL_TEST=1
Example:
    docker compose up
    ICHIRAN_MANUAL_TEST=1 go test ichiran_test.go
`)
	}
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "basic sentence",
			input:    "私は学生です",
			expected: "わたし は がくせい です",
		},
		{
			name:     "compound verb",
			input:    "勉強しています",
			expected: "べんきょう しています",
		},
		{
			name:     "mixed text",
			input:    "Hello世界",
			expected: "Hello せかい",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens, err := Analyze(tt.input)
			assert.NoError(t, err)
			
			result := tokens.Kana()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestJSONTokens_Roman(t *testing.T) {
	if os.Getenv("ICHIRAN_MANUAL_TEST") != "1" {
		t.Skip(`To run these tests, start the containers manually and set ICHIRAN_MANUAL_TEST=1
Example:
    docker compose up
    ICHIRAN_MANUAL_TEST=1 go test ichiran_test.go
`)
	}
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "basic sentence",
			input:    "私は学生です",
			expected: "watashi wa gakusei desu",
		},
		{
			name:     "compound verb",
			input:    "勉強しています",
			expected: "benkyō shiteimasu",
		},
		{
			name:     "mixed text",
			input:    "Hello世界",
			expected: "Hello sekai",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens, err := Analyze(tt.input)
			assert.NoError(t, err)
			
			result := tokens.Roman()
			assert.Equal(t, tt.expected, result)
		})
	}
}