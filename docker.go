
package ichiran

import (
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
	dockerInstance *dockerutil.DockerManager
	dockerOnce sync.Once
	dockerMu sync.Mutex
)

type Ichiran struct {
	docker *dockerutil.DockerManager
	logger *dockerutil.ContainerLogConsumer
}

// NewDocker creates or returns an existing Docker manager instance
func NewDocker() (*dockerutil.DockerManager, error) {
	dockerMu.Lock()
	defer dockerMu.Unlock()

	var initErr error
	dockerOnce.Do(func() {
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

		dockerInstance, initErr = dockerutil.NewDockerManager(cfg)
	})

	return dockerInstance, initErr
}

// Init initializes the ichiran service
func (i *Ichiran) Init() error {
	return i.docker.Init()
}

// InitQuiet initializes the ichiran service with reduced logging
func (i *Ichiran) InitQuiet() error {
	return i.docker.InitQuiet()
}

// InitForce initializes the ichiran service with forced rebuild
func (i *Ichiran) InitForce() error {
	return i.docker.InitForce()
}

// Stop stops the ichiran service
func (i *Ichiran) Stop() error {
	return i.docker.Stop()
}

// Close implements io.Closer
func (i *Ichiran) Close() error {
	i.logger.Close()
	return i.docker.Close()
}

// Status returns the current status of the ichiran service
func (i *Ichiran) Status() (string, error) {
	return i.docker.Status()
}

// SetLogLevel updates the logging level
func (i *Ichiran) SetLogLevel(level zerolog.Level) {
	i.logger.SetLogLevel(level)
}




func placeholder3456543() {
	color.Redln(" 𝒻*** 𝓎ℴ𝓊 𝒸ℴ𝓂𝓅𝒾𝓁ℯ𝓇")
	pp.Println("𝓯*** 𝔂𝓸𝓾 𝓬𝓸𝓶𝓹𝓲𝓵𝓮𝓻")
}
