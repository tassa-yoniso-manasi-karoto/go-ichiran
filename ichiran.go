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
	"time"

	"github.com/gookit/color"
	"github.com/k0kubun/pp"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/tidwall/pretty"

	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/flags"
	"github.com/docker/docker/api/types"
	//"github.com/docker/docker/client"
)

const (
	ContainerName = "ichiran-main-1"
)

var (
	reMultipleSpacesSeq = regexp.MustCompile(`\s{2,}`)
	QueryTO =  1 * time.Hour
)

func init() {
	zerolog.SetGlobalLevel(zerolog.TraceLevel)
	pp.BufferFoldThreshold = 10000
	//log.Logger = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.TimeOnly}).With().Timestamp().Logger()
	log.Logger = zerolog.Nop()
}

// JSONToken represents a single token with all its analysis information
type JSONToken struct {
	Surface     string `json:"text"`		// Original text
	IsToken     bool				// Whether this is a Japanese token or non-Japanese text
	Reading     string      `json:"reading"`	// Reading with kanji and kana
	Kana        string      `json:"kana"`		// Kana reading
	Romaji      string				// Romanized form from ichiran
	Score       int         `json:"score"`          // Analysis score
	Seq         int         `json:"seq"`            // Sequence number
	Gloss       []Gloss     `json:"gloss"`          // English meanings
	Conj        []Conj      `json:"conj,omitempty"` // Conjugation information
	Alternative []JSONToken `json:"alternative"`    // Alternative interpretations
	//Compound	[]string   `json:"compound"`		  // Delineable elements of compound expressions
	//Components  []JSONToken	`json:"components"`		// Details of delineable elements of compound expressions
	Raw []byte `json:"-"`				// Raw JSON for future processing
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

// FIXME update the methods to target *JSONTokens

// TokenizedStr returns a string of all tokens separated by spaces or commas.
func (tokens JSONTokens) TokenizedStr() string {
	var parts []string
	for _, token := range tokens {
		if token.IsToken {
			parts = append(parts, token.Surface)
		} else {
			parts = append(parts, token.Surface) // non-Japanese content
		}
	}
	s := strings.Join(parts, " ")
	return reMultipleSpacesSeq.ReplaceAllString(s, ", ")
}

// TokenizedParts returns a slice of all token surfaces.
func (tokens JSONTokens) TokenizedParts() []string {
	var parts []string
	for _, token := range tokens {
		parts = append(parts, token.Surface)
	}
	return parts
}

// Kana returns a string of all tokens in kana form where available.
func (tokens JSONTokens) Kana() string {
	var parts []string
	for _, token := range tokens {
		if token.IsToken {
			if token.Kana != "" {
				parts = append(parts, token.Kana)
			} else {
				parts = append(parts, token.Surface)
			}
		} else {
			parts = append(parts, token.Surface)
		}
	}
	s := strings.Join(parts, " ")
	return reMultipleSpacesSeq.ReplaceAllString(s, ", ")
}

// KanaParts returns a slice of all tokens in kana form where available.
func (tokens JSONTokens) KanaParts() []string {
	var parts []string
	for _, token := range tokens {
		if token.IsToken {
			if token.Kana != "" {
				parts = append(parts, token.Kana)
			} else {
				parts = append(parts, token.Surface)
			}
		} else {
			parts = append(parts, token.Surface)
		}
	}
	return parts
}

// Roman returns a string of all tokens in romanized form.
func (tokens JSONTokens) Roman() string {
	var parts []string
	for _, token := range tokens {
		if token.IsToken {
			if token.Romaji != "" {
				parts = append(parts, token.Romaji)
			}
		} else {
			parts = append(parts, token.Surface)
		}
	}
	s := strings.Join(parts, " ")
	return reMultipleSpacesSeq.ReplaceAllString(s, ", ")
}

// RomanParts returns a slice of all tokens in romanized form.
func (tokens JSONTokens) RomanParts() []string {
	var parts []string
	for _, token := range tokens {
		parts = append(parts, token.Romaji)
	}
	return parts
}

// GlossString returns a formatted string containing tokens and their English glosses.
func (tokens JSONTokens) GlossString() string {
	var parts []string
	for _, token := range tokens {
		if token.IsToken {
			var glosses []string
			for _, g := range token.Gloss {
				glosses = append(glosses, g.Gloss)
			}
			if len(glosses) > 0 {
				parts = append(parts, fmt.Sprintf("%s(%s)",
					token.Surface,
					strings.Join(glosses, "; ")))
			}
		} else {
			parts = append(parts, token.Surface)
		}
	}
	return strings.Join(parts, " ")
}

//############################################################################

// Analyze performs morphological analysis on the input Japanese text.
// Returns parsed tokens or an error if analysis fails.
func Analyze(text string) (*JSONTokens, error) {
	ctx, cancel := context.WithTimeout(context.Background(), QueryTO)
	defer cancel()
	
	// Initialize Docker CLI
	cli, err := command.NewDockerCli(
		command.WithStandardStreams(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create Docker CLI: %w", err)
	}

	if err := cli.Initialize(flags.NewClientOptions()); err != nil {
		return nil, fmt.Errorf("failed to initialize Docker CLI: %w", err)
	}
	docker := cli.Client()

	// Check container status
	containerInfo, err := docker.ContainerInspect(ctx, ContainerName)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect container: %w", err)
	}

	if !containerInfo.State.Running {
		return nil, fmt.Errorf("container %s is not running", ContainerName)
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
	exec, err := docker.ContainerExecCreate(ctx, ContainerName, execConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create exec: %w", err)
	}

	// Attach to execution
	resp, err := docker.ContainerExecAttach(ctx, exec.ID, types.ExecStartCheck{})
	if err != nil {
		return nil, fmt.Errorf("failed to attach to exec: %w", err)
	}
	defer resp.Close()

	// Read output
	output, err := readDockerOutput(resp.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to read exec output: %w", err)
	}

	// Check execution status
	inspect, err := docker.ContainerExecInspect(ctx, exec.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect exec: %w", err)
	}

	if inspect.ExitCode != 0 {
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
func safe(text string) (s string) {
	// FIXME probably for robust approach with a lib
	s = strings.Replace(text, "\"", "\\\"", -1)
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

	// Trim any trailing whitespace or newlines
	return bytes.TrimSpace(output.Bytes()), nil
}


// extractTokens converts raw JSON data into structured token information.
func extractTokens(group []interface{}) []JSONToken {
	var tokens []JSONToken

	if len(group) < 1 {
		log.Error().Msgf("group too short: %v", group)
		return tokens
	}

	layer, ok := group[0].([]interface{})
	if !ok {
		log.Error().Msgf("failed to assert layer: expected []interface{}, got %s with value: %#v",
			reflect.TypeOf(group[0]), group[0])
		return tokens
	}

	tokenGroups, ok := layer[0].([]interface{})
	if !ok {
		log.Error().Msgf("failed to assert tokenGroups: expected []interface{}, got %s with value: %#v",
			reflect.TypeOf(group[0]), group[0])
		return tokens
	}

	// Process each token group
	for _, tokenGroup := range tokenGroups {
		// Skip if it's a number (like the "192" in your example)
		if _, ok := tokenGroup.(json.Number); ok {
			continue
		}

		tokenEntry, ok := tokenGroup.([]interface{})
		if !ok {
			log.Error().Msgf("failed to assert tokenEntry: expected []interface{}, got %s with value: %#v",
				reflect.TypeOf(tokenGroup), tokenGroup)
			continue
		}

		if len(tokenEntry) < 3 {
			log.Error().Msgf("tokenEntry too short: %#v", tokenEntry)
			continue
		}

		// First element is romaji
		romaji, ok := tokenEntry[0].(string)
		if !ok {
			pp.Println(tokenEntry[0])
			log.Error().Msgf("failed to assert romaji: expected string, got %s with value: %#v",
				reflect.TypeOf(tokenEntry[0]), tokenEntry[0])
			continue
		}

		// Second element can be either direct token data or an object with alternatives
		data, ok := tokenEntry[1].(map[string]interface{})
		if !ok {
			log.Error().Msgf("failed to assert token map: expected map[string]interface{}, got %s with value: %#v",
				reflect.TypeOf(tokenEntry[1]), tokenEntry[1])
			continue
		}

		var token JSONToken

		// Check if this is an alternative structure
		if altInterface, hasAlt := data["alternative"]; hasAlt {
			altArray, ok := altInterface.([]interface{})
			if !ok {
				log.Error().Msgf("failed to assert alternative array: got %T", altInterface)
				continue
			}

			// Parse all alternatives
			var alternatives []JSONToken
			for _, alt := range altArray {
				altBytes, err := json.Marshal(alt)
				if err != nil {
					log.Error().Err(err).Msg("failed to marshal alternative")
					continue
				}

				var altToken JSONToken
				if err := json.Unmarshal(altBytes, &altToken); err != nil {
					// ERR failed to unmarshal alternative error="json: cannot unmarshal array into Go struct field Conj.conj.readok of type bool"
					log.Error().Str("altBytes", string(pretty.Pretty(altBytes))).Err(err).Msg("failed to unmarshal alternative")
					continue
				}

				if err := decodeToken(&altToken); err != nil {
					log.Error().Err(err).Msg("failed to decode alternative token")
					continue
				}

				alternatives = append(alternatives, altToken)
			}

			if len(alternatives) > 0 {
				token = alternatives[0] // Use first alternative as main token
				token.Alternative = alternatives[1:]
			} else {
				continue // Skip if no valid alternatives
			}
		} else {
			// Direct token data
			tokenBytes, err := json.Marshal(data)
			if err != nil {
				log.Error().Err(err).Msgf("failed to marshal token data of type %s: %#v",
					reflect.TypeOf(data), data)
				continue
			}

			if err := json.Unmarshal(tokenBytes, &token); err != nil {
				log.Error().Err(err).Msgf("failed to unmarshal token data: %s", string(tokenBytes))
				continue
			}

			if err := decodeToken(&token); err != nil {
				log.Error().Err(err).Msgf("failed to decode token: %#v", token)
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
		log.Fatal().Err(err).Msg("failed to decode JSON")
		return nil, fmt.Errorf("failed to decode JSON: %w", err)
	}

	//log.Debug().Msgf("Raw result structure type: %s", reflect.TypeOf(rawResult))
	/*for i, item := range rawResult {
		log.Trace().Msgf("Item %d type: %s, value: %#v", i, reflect.TypeOf(item), item)
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
				IsToken: false,
			})
		case []interface{}:
			extracted := extractTokens(v)
			if len(extracted) > 0 {
				for _, t := range extracted {
					t.IsToken = true
					tokens = append(tokens, &t)
				}
			} else {
				log.Error().Msgf("No tokens extracted from type %s: %#v",
					reflect.TypeOf(v), v)
			}
		default:
			log.Debug().Msgf("Unexpected type in rawResult: %s value: %#v",
				reflect.TypeOf(item), item)
		}
	}

	return tokens, nil
}

// decodeToken processes Unicode escapes and other encodings in token fields.
func decodeToken(token *JSONToken) error {
	var err error

	if token.Surface, err = unescapeUnicodeString(token.Surface); err != nil {
		log.Debug().Err(err).Msgf("failed to decode Surface: %s", token.Surface)
		return fmt.Errorf("failed to decode Surface: %w", err)
	}
	if token.Reading, err = unescapeUnicodeString(token.Reading); err != nil {
		log.Debug().Err(err).Msgf("failed to decode Reading: %s", token.Reading)
		return fmt.Errorf("failed to decode Reading: %w", err)
	}
	if token.Kana, err = unescapeUnicodeString(token.Kana); err != nil {
		log.Debug().Err(err).Msgf("failed to decode Kana: %s", token.Kana)
		return fmt.Errorf("failed to decode Kana: %w", err)
	}

	return nil
}

// unescapeUnicodeString converts JSON Unicode escapes (\uXXXX) to actual characters
func unescapeUnicodeString(s string) (string, error) {
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
