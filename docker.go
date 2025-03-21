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
	"strings"
	"regexp"
	
	"github.com/gookit/color"
	"github.com/k0kubun/pp"
	"github.com/rs/zerolog"

	"github.com/tassa-yoniso-manasi-karoto/dockerutil"
)

const (
	remote        = "https://github.com/tshatrov/ichiran.git"
	projectName   = "ichiran"
	containerName = "ichiran-main-1"
)

var (
	// Default settings for backward compatibility
	DefaultQueryTimeout = 45 * time.Minute
	DefaultDockerLogLevel = zerolog.TraceLevel
	
	reMultipleSpacesSeq = regexp.MustCompile(`\s{2,}`)
	Logger              = zerolog.Nop()
	// Logger = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.TimeOnly}).With().Timestamp().Logger()
	errNoJSONFound = fmt.Errorf("no valid JSON line found in output")
)

// IchiranManager handles Docker lifecycle for the Ichiran project
type IchiranManager struct {
	docker      *dockerutil.DockerManager
	logger      *dockerutil.ContainerLogConsumer
	projectName string
	containerName string
	QueryTimeout time.Duration
}

// ManagerOption defines function signature for options to configure IchiranManager
type ManagerOption func(*IchiranManager)

// WithQueryTimeout sets a custom query timeout
func WithQueryTimeout(timeout time.Duration) ManagerOption {
	return func(im *IchiranManager) {
		im.QueryTimeout = timeout
	}
}

// WithProjectName sets a custom project name for multiple instances
func WithProjectName(name string) ManagerOption {
	return func(im *IchiranManager) {
		im.projectName = name
		// Default container name is derived from project name
		im.containerName = name + "-main-1"
	}
}

// WithContainerName overrides the default container name
func WithContainerName(name string) ManagerOption {
	return func(im *IchiranManager) {
		im.containerName = name
	}
}

// NewManager creates a new Ichiran manager instance
func NewManager(ctx context.Context, opts ...ManagerOption) (*IchiranManager, error) {
	manager := &IchiranManager{
		projectName: projectName,
		containerName: containerName,
		QueryTimeout: DefaultQueryTimeout,
	}
	
	// Apply options
	for _, opt := range opts {
		opt(manager)
	}
	
	logConfig := dockerutil.LogConfig{
		Prefix:      manager.projectName,
		ShowService: true,
		ShowType:    true,
		LogLevel:    DefaultDockerLogLevel,
		InitMessage: "All set, awaiting commands",
	}

	logger := dockerutil.NewContainerLogConsumer(logConfig)

	cfg := dockerutil.Config{
		ProjectName:      manager.projectName,
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

	dockerManager, err := dockerutil.NewDockerManager(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create Docker manager: %w", err)
	}

	manager.docker = dockerManager
	manager.logger = logger
	
	return manager, nil
}

// Init initializes the docker service
func (im *IchiranManager) Init(ctx context.Context) error {
	return im.docker.Init()
}

// InitQuiet initializes the docker service with reduced logging
func (im *IchiranManager) InitQuiet(ctx context.Context) error {
	return im.docker.InitQuiet()
}

// InitRecreate remove existing containers then builds and up the containers
func (im *IchiranManager) InitRecreate(ctx context.Context, noCache bool) error {
	if noCache {
		return im.docker.InitRecreateNoCache()
	}
	return im.docker.InitRecreate()
}

// MustInit initializes the docker service and panics on error
func (im *IchiranManager) MustInit(ctx context.Context) {
	if err := im.InitRecreate(ctx, true); err != nil {
		panic(err)
	}
}

// Stop stops the docker service
func (im *IchiranManager) Stop(ctx context.Context) error {
	return im.docker.Stop()
}

// Close implements io.Closer
func (im *IchiranManager) Close() error {
	im.logger.Close()
	return im.docker.Close()
}

// Status returns the current status of the project
func (im *IchiranManager) Status(ctx context.Context) (string, error) {
	return im.docker.Status()
}

// GetContainerName returns the name of the main container
func (im *IchiranManager) GetContainerName() string {
	return im.containerName
}

// For backward compatibility with existing code
var (
	instance *IchiranManager
	mu sync.Mutex
	instanceClosed bool
)

// InitWithContext initializes the default docker service with a context
func InitWithContext(ctx context.Context) error {
	mgr, err := getOrCreateDefaultManager(ctx)
	if err != nil {
		return err
	}
	return mgr.Init(ctx)
}

// Init initializes the default docker service (backward compatibility)
func Init() error {
	return InitWithContext(context.Background())
}

// InitQuietWithContext initializes the docker service with reduced logging and a context
func InitQuietWithContext(ctx context.Context) error {
	mgr, err := getOrCreateDefaultManager(ctx)
	if err != nil {
		return err
	}
	return mgr.InitQuiet(ctx)
}

// InitQuiet initializes the docker service with reduced logging (backward compatibility)
func InitQuiet() error {
	return InitQuietWithContext(context.Background())
}

// InitRecreateWithContext removes existing containers with a context
func InitRecreateWithContext(ctx context.Context, noCache bool) error {
	mgr, err := getOrCreateDefaultManager(ctx)
	if err != nil {
		return err
	}
	return mgr.InitRecreate(ctx, noCache)
}

// InitRecreate removes existing containers (backward compatibility)
func InitRecreate(noCache bool) error {
	return InitRecreateWithContext(context.Background(), noCache)
}

// MustInitWithContext initializes the docker service with a context (panics on error)
func MustInitWithContext(ctx context.Context) {
	mgr, _ := getOrCreateDefaultManager(ctx)
	mgr.MustInit(ctx)
}

// MustInit initializes the docker service (backward compatibility)
func MustInit() {
	MustInitWithContext(context.Background())
}

// StopWithContext stops the docker service with a context
func StopWithContext(ctx context.Context) error {
	if instance == nil {
		return fmt.Errorf("docker instance not initialized")
	}
	return instance.Stop(ctx)
}

// Stop stops the docker service (backward compatibility)
func Stop() error {
	return StopWithContext(context.Background())
}

// StatusWithContext returns the current status of the project with a context
func StatusWithContext(ctx context.Context) (string, error) {
	if instance == nil {
		return "", fmt.Errorf("docker instance not initialized")
	}
	return instance.Status(ctx)
}

// Status returns the current status of the project (backward compatibility)
func Status() (string, error) {
	return StatusWithContext(context.Background())
}

// Close implements io.Closer (backward compatibility)
func Close() error {
	mu.Lock()
	defer mu.Unlock()
	
	if instance != nil {
		instance.logger.Close()
		err := instance.docker.Close()
		// Mark the instance as closed
		instanceClosed = true
		return err
	}
	return nil
}

// getOrCreateDefaultManager returns or creates the default manager instance
func getOrCreateDefaultManager(ctx context.Context) (*IchiranManager, error) {
	mu.Lock()
	defer mu.Unlock()
	
	// Create a new instance if it doesn't exist or was previously closed
	if instance == nil || instanceClosed {
		mgr, err := NewManager(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to create default manager: %w", err)
		}
		instance = mgr
		instanceClosed = false
	}
	
	return instance, nil
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
	if strings.Contains(string(rawOutput), "ichiran-cli: command not found") {
		return []byte{}, fmt.Errorf("\"%s\": "+
			"this error is associated with a temporary failure in " +
			"domain resolution during container creation, "+
			"check your network, disable any VPN and restart %s.",
			rawOutput, dockerutil.DockerBackendName())
	}
	

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