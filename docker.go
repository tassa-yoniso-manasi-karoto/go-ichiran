
package ichiran

import (
	"fmt"
	"time"
	"sync"

	"github.com/rs/zerolog"
	"github.com/gookit/color"
	"github.com/k0kubun/pp"
	
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
	remote = "https://github.com/tshatrov/ichiran.git"
	projectName = "ichiran"
	containerName = "ichiran-main-1"
)

var (
	QueryTO = 1 * time.Hour
	instance *Docker
	once sync.Once
	mu sync.Mutex
)

type Docker struct {
	docker *dockerutil.DockerManager
	logger  *dockerutil.ContainerLogConsumer
}

// NewDocker creates or returns an existing Docker instance
func NewDocker() (*Docker, error) {
	mu.Lock()
	defer mu.Unlock()

	var initErr error
	once.Do(func() {
		logConfig := dockerutil.LogConfig{
			Prefix:      projectName,
			ShowService: true,
			ShowType:    true,
			LogLevel:    zerolog.Disabled,
			InitMessage: "All set, awaiting commands",
		}
		
		logger := dockerutil.NewContainerLogConsumer(logConfig)

		cfg := dockerutil.Config{
			ProjectName:      projectName,
			ComposeFile:     "docker-compose.yml",
			RemoteRepo:      remote,
			RequiredServices: []string{"main", "pg"},
			LogConsumer:     logger,
		}

		manager, err := dockerutil.NewDockerManager(cfg)
		if err != nil {
			initErr = err
			return
		}

		instance = &Docker{
			docker: manager,
			logger:  logger,
		}
	})

	if initErr != nil {
		return nil, initErr
	}
	return instance, nil
}

// Init initializes the ichiran service
func Init() error {
	if instance == nil {
		if _, err := NewDocker(); err != nil {
			return err
		}
	}
	return instance.docker.Init()
}

// InitQuiet initializes the ichiran service with reduced logging
func InitQuiet() error {
	if instance == nil {
		if _, err := NewDocker(); err != nil {
			return err
		}
	}
	return instance.docker.InitQuiet()
}

// InitForce initializes the ichiran service with forced rebuild
func InitForce() error {
	if instance == nil {
		if _, err := NewDocker(); err != nil {
			return err
		}
	}
	return instance.docker.InitForce()
}

// Same as InitForce but will throw a runtime panic if an error is an error occur.
func MustInit() {
	if instance == nil {
		NewDocker()
	}
	instance.docker.InitForce()
}

// Stop stops the ichiran service
func Stop() error {
	if instance == nil {
		return fmt.Errorf("docker instance not initialized")
	}
	return instance.docker.Stop()
}

// Close implements io.Closer
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

func SetLogLevel(level zerolog.Level) {
	if instance != nil {
		instance.logger.SetLogLevel(level)
	}
}




func placeholder3456543() {
	fmt.Print("")
	color.Redln(" 𝒻*** 𝓎ℴ𝓊 𝒸ℴ𝓂𝓅𝒾𝓁ℯ𝓇")
	pp.Println("𝓯*** 𝔂𝓸𝓾 𝓬𝓸𝓶𝓹𝓲𝓵𝓮𝓻")
}
