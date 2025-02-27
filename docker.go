package ichiran

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/gookit/color"
	"github.com/k0kubun/pp"
	"github.com/rs/zerolog"

	"github.com/tassa-yoniso-manasi-karoto/dockerutil"
)

/*
Libraries:
https://pkg.go.dev/github.com/docker/compose/v2@v2.32.1/pkg/compose#NewComposeService
https://pkg.go.dev/github.com/docker/compose/v2@v2.32.1/pkg/api#Service
https://pkg.go.dev/github.com/docker/cli@v27.4.1+incompatible/cli/command
https://pkg.go.dev/github.com/docker/cli@v27.4.1+incompatible/cli/flags
*/

const (
	remote        = "https://github.com/tshatrov/ichiran.git"
	projectName   = "ichiran"
	containerName = "ichiran-main-1"
)

var (
	instance       *docker
	once           sync.Once
	mu             sync.Mutex
	Ctx            = context.Background()
	QueryTimeout   = 45 * time.Minute
	DockerLogLevel = zerolog.TraceLevel
)

type docker struct {
	docker *dockerutil.DockerManager
	logger *dockerutil.ContainerLogConsumer
}

// newDocker creates or returns an existing docker instance
func newDocker() (*docker, error) {
	mu.Lock()
	defer mu.Unlock()

	var initErr error
	once.Do(func() {
		logConfig := dockerutil.LogConfig{
			Prefix:      projectName,
			ShowService: true,
			ShowType:    true,
			LogLevel:    DockerLogLevel,
			InitMessage: "All set, awaiting commands",
		}

		logger := dockerutil.NewContainerLogConsumer(logConfig)

		cfg := dockerutil.Config{
			ProjectName:      projectName,
			ComposeFile:      "docker-compose.yml",
			RemoteRepo:       remote,
			RequiredServices: []string{"main", "pg"},
			LogConsumer:      logger,
			Timeout: dockerutil.Timeout{
				Create:   200 * time.Second,
				Recreate: 25 * time.Minute,
				Start:    60 * time.Second,
			},
		}

		manager, err := dockerutil.NewDockerManager(Ctx, cfg)
		if err != nil {
			initErr = err
			return
		}

		instance = &docker{
			docker: manager,
			logger: logger,
		}
	})

	if initErr != nil {
		return nil, initErr
	}
	return instance, nil
}

// Init initializes the docker service
func Init() error {
	if instance == nil {
		if _, err := newDocker(); err != nil {
			return err
		}
	}
	return instance.docker.Init()
}

// InitQuiet initializes the docker service with reduced logging
func InitQuiet() error {
	if instance == nil {
		if _, err := newDocker(); err != nil {
			return err
		}
	}
	return instance.docker.InitQuiet()
}

// InitRecreate remove existing containers (if noCache is true, downloads the lastest
// version of dependencies ignoring local cache), then builds and up the containers
func InitRecreate(noCache bool) error {
	if instance == nil {
		if _, err := newDocker(); err != nil {
			return err
		}
	}
	if noCache {
		return instance.docker.InitRecreateNoCache()
	}
	return instance.docker.InitRecreate()
}

func MustInit() {
	if instance == nil {
		newDocker()
	}
	instance.docker.InitRecreateNoCache()
}

// Stop stops the ichiran service
func Stop() error {
	if instance == nil {
		return fmt.Errorf("docker instance not initialized")
	}
	return instance.docker.Stop()
}

// Close implements io.Closer. It is just a convenience wrapper for Stop().
func Close() error {
	if instance != nil {
		instance.logger.Close()
		return instance.docker.Close()
	}
	return nil
}

func Status() (string, error) {
	if instance == nil {
		return "", fmt.Errorf("docker instance not initialized")
	}
	return instance.docker.Status()
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

	// Print raw output for debugging
	//color.Redln("RAW ICHIRAN-CLI OUTPUT:")
	//fmt.Println(string(rawOutput))

	// Use bufio.Reader so we can read arbitrarily long lines.
	r := bufio.NewReader(bytes.NewReader(rawOutput))
	for {
		line, err := r.ReadBytes('\n')
		// Trim any extra whitespace.
		line = bytes.TrimSpace(line)
		if len(line) > 0 {
			// Check if it's a JSON string wrapped in quotes (common for Lisp output)
			if line[0] == '"' && line[len(line)-1] == '"' && len(line) > 2 {
				// The content might have escaped quotes and backslashes
				var unescaped string
				if err := json.Unmarshal(line, &unescaped); err == nil {
					// Now try to parse the unescaped content as JSON
					var tmp interface{}
					if err := json.Unmarshal([]byte(unescaped), &tmp); err == nil {
						return []byte(unescaped), nil
					}
				}
			}

			// Regular JSON check - if the line starts with a JSON array or object.
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

func placeholder3456543() {
	fmt.Print("")
	color.Redln(" 𝒻*** 𝓎ℴ𝓊 𝒸ℴ𝓂𝓅𝒾𝓁ℯ𝓇")
	pp.Println("𝓯*** 𝔂𝓸𝓾 𝓬𝓸𝓶𝓹𝓲𝓵𝓮𝓻")
}
