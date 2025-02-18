// Package ichiran provides functionality for Japanese text analysis using Docker containers
// and the ichiran morphological analyzer.

package ichiran

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"regexp"
	"strings"
	"bufio"

	"github.com/gookit/color"
	"github.com/k0kubun/pp"
	"github.com/rs/zerolog"
	"github.com/tidwall/pretty"
	"github.com/docker/docker/api/types"
	"al.essio.dev/pkg/shellescape"
)

const (
	ContainerName = "ichiran-main-1"
)

var (
	reMultipleSpacesSeq = regexp.MustCompile(`\s{2,}`)
	Logger = zerolog.Nop()
	// Logger = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.TimeOnly}).With().Timestamp().Logger()
	errNoJSONFound = fmt.Errorf("no valid JSON line found in output")
)


// JSONToken represents a single token with all its analysis information
type JSONToken struct {
	Surface     string 	`json:"text"`		// Original text
	IsLexical   bool				// Whether this is a Japanese token or non-Japanese text
	Reading     string      `json:"reading"`	// Reading with kanji and kana
	Kana        string      `json:"kana"`		// Kana reading
	Romaji      string				// Romanized form from ichiran
	Score       int         `json:"score"`          // Analysis score
	Seq         int         `json:"seq"`            // Sequence number
	Gloss       []Gloss     `json:"gloss"`          // English meanings
	Conj        []Conj      `json:"conj,omitempty"` // Conjugation information
	Alternative []JSONToken `json:"alternative"`    // Alternative interpretations
	Compound    []string    `json:"compound"`	// Delineable elements of compound expressions
	Components  []JSONToken	`json:"components"`	// Details of delineable elements of compound expressions
	Raw []byte `json:"-"`				// Raw JSON for future processing
}

// in case of multiple alternative, jsonTokenCore represents the essential information that are shared,
// that will spearhead the JSONToken for consistency's sake
type jsonTokenCore struct {
	Surface     string 	`json:"text"`		// Original text
	IsLexical   bool				// Whether this is a Japanese token or non-Japanese text
	Reading     string      `json:"reading"`	// Reading with kanji and kana
	Kana        string      `json:"kana"`		// Kana reading
	Romaji      string				// Romanized form from ichiran
	Score       int         `json:"score"`          // Analysis score
}

// extractCore returns only the core fields from a JSONToken
func extractCore(token JSONToken) jsonTokenCore {
	return jsonTokenCore{
		Surface:	token.Surface,
		IsLexical:	token.IsLexical,
		Reading:	token.Reading,
		Kana:		token.Kana,
		Romaji:		token.Romaji,
		Score:		token.Score,
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


// TokenizedStr returns a string of all tokens separated by spaces or commas.
func (tokens JSONTokens) Tokenized() string {
	parts := tokens.TokenizedParts()
	s := strings.Join(parts, " ")
	return reMultipleSpacesSeq.ReplaceAllString(s, ", ")
}

// TokenizedParts returns a slice of all token surfaces.
func (tokens JSONTokens) TokenizedParts() (parts []string) {
	for _, token := range tokens {
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
					Raw:        component.Raw,
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


/// getGlosses extracts all glosses from both direct Gloss field and Conj field
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



//############################################################################

// Analyze performs morphological analysis on the input Japanese text.
// Returns parsed tokens or an error if analysis fails.
// Analyze performs Japanese text analysis using ichiran
func Analyze(text string) (*JSONTokens, error) {
	ctx, cancel := context.WithTimeout(Ctx, QueryTimeout)
	defer cancel()

	mu.Lock()
	docker := instance
	mu.Unlock()

	if docker == nil {
		return nil, fmt.Errorf("Docker manager not initialized. Call Init() first")
	}

	// Get Docker client from dockerutil
	client, err := docker.docker.GetClient()
	if err != nil {
		return nil, fmt.Errorf("failed to get Docker client: %w", err)
	}

	// Check container status
	containerInfo, err := client.ContainerInspect(ctx, containerName)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect container: %w", err)
	}

	if !containerInfo.State.Running {
		return nil, fmt.Errorf("container %s is not running", containerName)
	}

	// Prepare command
	cmd := []string{
		"bash",
		"-c",
		fmt.Sprintf("ichiran-cli -f \"%s\"", safe(text)),
	}

	// Create execution config
	execConfig := types.ExecConfig{
		User:         containerInfo.Config.User,
		Cmd:          cmd,
		AttachStdout: true,
		AttachStderr: true,
		Tty:          false,
		Privileged:   false,
	}

	// Create execution
	exec, err := client.ContainerExecCreate(ctx, containerName, execConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create exec: %w", err)
	}

	// Attach to execution
	resp, err := client.ContainerExecAttach(ctx, exec.ID, types.ExecStartCheck{})
	if err != nil {
		return nil, fmt.Errorf("failed to attach to exec: %w", err)
	}
	defer resp.Close()

	output, err := extractJSONFromDockerOutput(resp.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to read exec output: %w", err)
	}

	// Check execution status
	inspect, err := client.ContainerExecInspect(ctx, exec.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect exec: %w", err)
	}

	switch inspect.ExitCode{
	case 0:
	default:
		return nil, fmt.Errorf("command failed with exit code %d: %s",
			inspect.ExitCode, string(output))
	}

	// Parse output
	tokens, err := parseOutput(output)
	if err != nil {
		return nil, fmt.Errorf("failed to parse output: %w", err)
	}

	return &tokens, nil
}


// safe escapes special characters in the input text for shell command usage.
func safe(s string) string {
	s = shellescape.Quote(s)
	//s = strings.ReplaceAll(s, "\"", "\\\"")
	// leading "-" causes the string to be identified by the CLI as a serie of short flags
	return strings.TrimPrefix(s, "-")
}


// readDockerOutput reads and processes multiplexed output from Docker.
func readDockerOutput(reader io.Reader) ([]byte, error) {
	var output bytes.Buffer
	header := make([]byte, 8)
	for {
		_, err := io.ReadFull(reader, header)
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("failed to read header: %w", err)
		}
		// Get the payload size from the header
		payloadSize := binary.BigEndian.Uint32(header[4:])
		if payloadSize == 0 {
			continue
		}
		// Read the payload
		payload := make([]byte, payloadSize)
		_, err = io.ReadFull(reader, payload)
		if err != nil {
			return nil, fmt.Errorf("failed to read payload: %w", err)
		}
		// Append to output buffer
		output.Write(payload)
	}
	return bytes.TrimSpace(output.Bytes()), nil
}

// extractJSONFromDockerOutput combines reading Docker output and extracting JSON
func extractJSONFromDockerOutput(reader io.Reader) ([]byte, error) {
	// First, read the Docker multiplexed output.
	rawOutput, err := readDockerOutput(reader)
	if err != nil {
		return nil, fmt.Errorf("error reading docker output: %w", err)
	}

	// Use bufio.Reader so we can read arbitrarily long lines.
	r := bufio.NewReader(bytes.NewReader(rawOutput))
	for {
		line, err := r.ReadBytes('\n')
		// Trim any extra whitespace.
		line = bytes.TrimSpace(line)
		if len(line) > 0 {
			// Check if the line starts with a JSON array or object.
			if line[0] == '[' || line[0] == '{' {
				var tmp interface{}
				// Validate that it's actually JSON.
				if err := json.Unmarshal(line, &tmp); err == nil {
					return line, nil
				}
			}
		}

		if err == io.EOF {
			break
		} else if err != nil {
			return nil, fmt.Errorf("error reading line: %w", err)
		}
	}

	return nil, errNoJSONFound
}



// IMPORTANT: jsonformatter.org is very helpful to help understand ichiran's JSON:
// 	as it both prettifies and converts unicode codepoints to literals

// extractTokens converts raw JSON data into structured token information.
func extractTokens(group []interface{}) []JSONToken {
	var tokens []JSONToken

	if len(group) < 1 {
		Logger.Error().Msgf("group too short: %v", group)
		return tokens
	}

	layer, ok := group[0].([]interface{})
	if !ok {
		Logger.Error().Msgf("failed to assert layer: expected []interface{}, got %s with value: %#v",
			reflect.TypeOf(group[0]), group[0])
		return tokens
	}

	tokenGroups, ok := layer[0].([]interface{})
	if !ok {
		Logger.Error().Msgf("failed to assert tokenGroups: expected []interface{}, got %s with value: %#v",
			reflect.TypeOf(group[0]), group[0])
		return tokens
	}

	// Process each token group
	for _, tokenGroup := range tokenGroups {
		// Skip if it's a number, I guess it's something internal to ichiran idk
		if _, ok := tokenGroup.(json.Number); ok {
			continue
		}

		tokenEntry, ok := tokenGroup.([]interface{})
		if !ok {
			Logger.Error().Msgf("failed to assert tokenEntry: expected []interface{}, got %s with value: %#v",
				reflect.TypeOf(tokenGroup), tokenGroup)
			continue
		}

		if len(tokenEntry) < 3 {
			Logger.Error().Msgf("tokenEntry too short: %#v", tokenEntry)
			continue
		}

		// First element is romaji
		romaji, ok := tokenEntry[0].(string)
		if !ok {
			pp.Println(tokenEntry[0])
			Logger.Error().Msgf("failed to assert romaji: expected string, got %s with value: %#v",
				reflect.TypeOf(tokenEntry[0]), tokenEntry[0])
			continue
		}

		// Second element can be either direct token data or an object with alternatives
		data, ok := tokenEntry[1].(map[string]interface{})
		if !ok {
			Logger.Error().Msgf("failed to assert token map: expected map[string]interface{}, got %s with value: %#v",
				reflect.TypeOf(tokenEntry[1]), tokenEntry[1])
			continue
		}

		var token JSONToken

		// Check if this is an alternative structure
		if altInterface, hasAlt := data["alternative"]; hasAlt {
			altArray, ok := altInterface.([]interface{})
			if !ok {
				Logger.Error().Msgf("failed to assert alternative array: got %T", altInterface)
				continue
			}

			// Parse all alternatives
			var alternatives []JSONToken
			for _, alt := range altArray {
				altBytes, err := json.Marshal(alt)
				if err != nil {
					Logger.Error().Err(err).Msg("failed to marshal alternative")
					continue
				}

				var altToken JSONToken
				if err := json.Unmarshal(altBytes, &altToken); err != nil {
					// ERR failed to unmarshal alternative error="json: cannot unmarshal array into Go struct field Conj.conj.readok of type bool"
					Logger.Error().Str("altBytes", string(pretty.Pretty(altBytes))).Err(err).Msg("failed to unmarshal alternative")
					continue
				}

				if err := decodeToken(&altToken); err != nil {
					Logger.Error().Err(err).Msg("failed to decode alternative token")
					continue
				}

				alternatives = append(alternatives, altToken)
			}

			if len(alternatives) > 0 {
				// Extract core fields from first alternative
				core := extractCore(alternatives[0])
				
				// Create new token with only core fields: it will be the "main"/visible token in JSONTokens
				token = JSONToken{}
				token.applyCore(core)
				
				// Store all alternatives
				token.Alternative = alternatives
			} else {
				continue // Skip if no valid alternatives
			}
		} else {
			// Direct token data
			tokenBytes, err := json.Marshal(data)
			if err != nil {
				Logger.Error().Err(err).Msgf("failed to marshal token data of type %s: %#v",
					reflect.TypeOf(data), data)
				continue
			}

			if err := json.Unmarshal(tokenBytes, &token); err != nil {
				Logger.Error().Err(err).Msgf("failed to unmarshal token data: %s", string(tokenBytes))
				continue
			}

			if err := decodeToken(&token); err != nil {
				Logger.Error().Err(err).Msgf("failed to decode token: %#v", token)
				continue
			}
		}

		token.Romaji = romaji
		tokens = append(tokens, token)
	}

	return tokens
}

// parseOutput converts raw Docker output into structured token data.
func parseOutput(output []byte) (JSONTokens, error) {
	//fmt.Println(string(pretty.Pretty(output)))
	var rawResult []interface{}
	decoder := json.NewDecoder(bytes.NewReader(output))
	decoder.UseNumber()
	if err := decoder.Decode(&rawResult); err != nil {
		println(stringCapLen(string(output), 1000))
		Logger.Fatal().Err(err).Msg("failed to decode JSON")
		return nil, fmt.Errorf("failed to decode JSON: %w", err)
	}

	//Logger.Debug().Msgf("Raw result structure type: %s", reflect.TypeOf(rawResult))
	/*for i, item := range rawResult {
		Logger.Trace().Msgf("Item %d type: %s, value: %#v", i, reflect.TypeOf(item), item)
	}*/

	var tokens JSONTokens

	for _, item := range rawResult {
		switch v := item.(type) {
		case string:
			unescaped, err := unescapeUnicodeString(v)
			if err != nil {
				return nil, fmt.Errorf("failed to decode segment: %w", err)
			}
			tokens = append(tokens, &JSONToken{
				Surface: unescaped,
				IsLexical: false,
			})
		case []interface{}:
			extracted := extractTokens(v)
			if len(extracted) > 0 {
				for _, t := range extracted {
					t.IsLexical = true
					tokens = append(tokens, &t)
				}
			} else {
				Logger.Error().Msgf("No tokens extracted from type %s: %#v",
					reflect.TypeOf(v), v)
			}
		default:
			Logger.Debug().Msgf("Unexpected type in rawResult: %s value: %#v",
				reflect.TypeOf(item), item)
		}
	}

	return tokens, nil
}

// decodeToken processes Unicode escapes and other encodings in token fields.
func decodeToken(token *JSONToken) error {
	var err error
	if token.Surface, err = unescapeUnicodeString(token.Surface); err != nil {
		Logger.Debug().Err(err).Msgf("failed to decode Surface: %s", token.Surface)
		return fmt.Errorf("failed to decode Surface: %w", err)
	}
	if token.Reading, err = unescapeUnicodeString(token.Reading); err != nil {
		Logger.Debug().Err(err).Msgf("failed to decode Reading: %s", token.Reading)
		return fmt.Errorf("failed to decode Reading: %w", err)
	}
	if token.Kana, err = unescapeUnicodeString(token.Kana); err != nil {
		Logger.Debug().Err(err).Msgf("failed to decode Kana: %s", token.Kana)
		return fmt.Errorf("failed to decode Kana: %w", err)
	}

	return nil
}

// unescapeUnicodeString converts Unicode escapes (\uXXXX) to actual characters
func unescapeUnicodeString(s string) (string, error) {
	// Kana field can contain a forbidden jutsu: \u200c = ZERO WIDTH NON-JOINER
	// however it is (apparently) automatically rendered by JSON decoder from its codepoint into a literal in Go
	// so it must replaced manually. 
	s = strings.ReplaceAll(s, /*ZERO WIDTH NON-JOINER*/"‌", "")
	// If the string doesn't contain any \u, return as is
	if !strings.Contains(s, "\\u") {
		return s, nil
	}

	// Add quotes and decode as JSON string which handles Unicode escapes
	quoted := `"` + strings.Replace(s, `"`, `\"`, -1) + `"`
	var unquoted string
	if err := json.Unmarshal([]byte(quoted), &unquoted); err != nil {
		return "", fmt.Errorf("failed to unescape Unicode: %w", err)
	}
	return unquoted, nil
}



func stringCapLen(s string, max int) string {
	trimmed := false
	for len(s) > max {
		s = s[:len(s)-1]
		trimmed = true
	}
	if trimmed {
		s += "…"
	}
	return s
}


func placeholder() {
	pretty.Pretty([]byte{})
	color.Redln(" 𝒻*** 𝓎ℴ𝓊 𝒸ℴ𝓂𝓅𝒾𝓁ℯ𝓇")
	pp.Println("𝓯*** 𝔂𝓸𝓾 𝓬𝓸𝓶𝓹𝓲𝓵𝓮𝓻")
}
