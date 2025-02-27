// Package ichiran provides functionality for Japanese text analysis using Docker containers
// and the ichiran morphological analyzer.

package ichiran

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/gookit/color"
	"github.com/k0kubun/pp"
	"github.com/rs/zerolog"
)

const (
	ContainerName = "ichiran-main-1"
)

var (
	reMultipleSpacesSeq = regexp.MustCompile(`\s{2,}`)
	Logger              = zerolog.Nop()
	// Logger = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.TimeOnly}).With().Timestamp().Logger()
	errNoJSONFound = fmt.Errorf("no valid JSON line found in output")
)

// TokenizedStr returns a string of all tokens separated by spaces or commas.
func (tokens JSONTokens) Tokenized() string {
	parts := tokens.TokenizedParts()
	// Debug the token parts to see what we got
	Logger.Debug().Msgf("Tokenized parts: %v", parts)
	s := strings.Join(parts, " ")
	return reMultipleSpacesSeq.ReplaceAllString(s, ", ")
}

// TokenizedParts returns a slice of all token surfaces.
func (tokens JSONTokens) TokenizedParts() (parts []string) {
	// Debug the raw tokens
	Logger.Debug().Msgf("Total tokens: %d", len(tokens))

	for i, token := range tokens {
		// Log detailed token information
		Logger.Debug().Msgf("Token #%d: Surface: '%s', IsLexical: %v, Kana: '%s'",
			i, token.Surface, token.IsLexical, token.Kana)

		// Always include the token's surface
		parts = append(parts, token.Surface)
	}
	return
}

// Kana returns a string of all tokens in kana form where available.
func (tokens JSONTokens) Kana() string {
	parts := tokens.KanaParts()
	s := strings.Join(parts, "")
	return reMultipleSpacesSeq.ReplaceAllString(s, ", ")
}

// KanaParts returns a slice of all tokens in kana form where available.
func (tokens JSONTokens) KanaParts() (parts []string) {
	for _, token := range tokens {
		if token.IsLexical && token.Kana != "" {
			parts = append(parts, token.Kana)
		} else {
			parts = append(parts, token.Surface)
		}
	}
	return
}

// Roman returns a string of all tokens in romanized form.
func (tokens JSONTokens) Roman() string {
	parts := tokens.RomanParts()
	s := strings.Join(parts, " ")
	return reMultipleSpacesSeq.ReplaceAllString(s, ", ")
}

// RomanParts returns a slice of all tokens in romanized form.
func (tokens JSONTokens) RomanParts() (parts []string) {
	for _, token := range tokens {
		if token.IsLexical && token.Romaji != "" {
			parts = append(parts, token.Romaji)
		} else {
			parts = append(parts, token.Surface)
		}
	}
	return
}

// ToMorphemes returns a new slice of tokens where compound tokens are replaced by their constituent morphemes
func (tokens JSONTokens) ToMorphemes() JSONTokens {
	var morphemes JSONTokens

	for _, token := range tokens {
		// If token has components, add them instead of the original token
		if len(token.Components) > 0 {
			for _, component := range token.Components {
				// Create a copy of the component
				morpheme := &JSONToken{
					Surface:     component.Surface,
					IsLexical:   true, // Components are always lexical content
					Reading:     component.Reading,
					Kana:        component.Kana,
					Romaji:      component.Romaji,
					Score:       component.Score,
					Seq:         component.Seq,
					Gloss:       component.Gloss,
					Conj:        component.Conj,
					Alternative: component.Alternative,
					Compound:    component.Compound,
					Components:  component.Components,
					Raw:         component.Raw,
				}
				morphemes = append(morphemes, morpheme)
			}
		} else {
			// If no components, add the original token
			morphemes = append(morphemes, token)
		}
	}

	return morphemes
}

// Gloss returns a formatted string containing tokens and their English glosses
// including morphemes and alternative interpretations.
func (tokens JSONTokens) Gloss() string {
	parts := tokens.GlossParts()
	return strings.Join(parts, " ")
}

// GlossParts returns a slice of strings containing tokens and their English glosses,
// including morphemes and alternative interpretations.
func (tokens JSONTokens) GlossParts() (parts []string) {
	morphemes := tokens.ToMorphemes()

	for _, token := range morphemes {
		if !token.IsLexical {
			parts = append(parts, token.Surface)
			continue
		}

		// Handle tokens with alternatives
		if len(token.Alternative) > 0 {
			var altGlosses []string

			// Add glosses from each alternative
			for i, alt := range token.Alternative {
				glosses := alt.getGlosses()
				if len(glosses) > 0 {
					altGlosses = append(altGlosses, fmt.Sprintf("ALT%d: %s",
						i+1,
						strings.Join(glosses, "; ")))
				}
			}

			if len(altGlosses) > 0 {
				parts = append(parts, fmt.Sprintf("%s (%s)",
					token.Surface,
					strings.Join(altGlosses, " | ")))
			} else {
				parts = append(parts, token.Surface)
			}
			continue
		}

		// Handle regular tokens
		glosses := token.getGlosses()
		if len(glosses) > 0 {
			parts = append(parts, fmt.Sprintf("%s(%s)",
				token.Surface,
				strings.Join(glosses, "; ")))
		} else {
			parts = append(parts, token.Surface)
		}
	}

	return
}

// / getGlosses extracts all glosses from both direct Gloss field and Conj field
func (token *JSONToken) getGlosses() []string {
	var glosses []string

	// Add direct glosses
	for _, g := range token.Gloss {
		glosses = append(glosses, g.Gloss)
	}

	// Add glosses from conjugations
	for _, c := range token.Conj {
		for _, g := range c.Gloss {
			glosses = append(glosses, g.Gloss)
		}
	}

	return glosses
}

func placeholder345446543() {
	fmt.Print("")
	color.Redln(" 𝒻*** 𝓎ℴ𝓊 𝒸ℴ𝓂𝓅𝒾𝓁ℯ𝓇")
	pp.Println("𝓯*** 𝔂𝓸𝓾 𝓬𝓸𝓶𝓹𝓲𝓵𝓮𝓻")
}
