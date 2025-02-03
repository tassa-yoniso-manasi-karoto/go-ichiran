
package ichiran

import (
	"fmt"
	"time"
	"sync"
	"context"

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
	LogLevel = zerolog.TraceLevel // FIXME still broken idk why
)

var (
	instance *docker
	once sync.Once
	mu sync.Mutex
	Ctx = context.TODO()
	QueryTimeout = 45 * time.Minute
)

type docker struct {
	docker *dockerutil.DockerManager
	logger  *dockerutil.ContainerLogConsumer
}

// NewDocker creates or returns an existing docker instance
func newDocker() (*docker, error) {
	mu.Lock()
	defer mu.Unlock()

	var initErr error
	once.Do(func() {
		logConfig := dockerutil.LogConfig{
			Prefix:      projectName,
			ShowService: true,
			ShowType:    true,
			LogLevel:    LogLevel,
			InitMessage: "All set, awaiting commands",
		}
		
		logger := dockerutil.NewContainerLogConsumer(logConfig)

		cfg := dockerutil.Config{
			ProjectName:      projectName,
			ComposeFile:      "docker-compose.yml",
			RemoteRepo:       remote,
			RequiredServices: []string{"main", "pg"},
			LogConsumer:      logger,
			Timeout:	  dockerutil.Timeout{
				Create:		200 * time.Second,
				Recreate:	25 * time.Minute,
				Start:		60 * time.Second,
			},
		}

		manager, err := dockerutil.NewDockerManager(Ctx, cfg)
		if err != nil {
			initErr = err
			return
		}

		instance = &docker{
			docker: manager,
			logger:  logger,
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



func placeholder3456543() {
	fmt.Print("")
	color.Redln(" 𝒻*** 𝓎ℴ𝓊 𝒸ℴ𝓂𝓅𝒾𝓁ℯ𝓇")
	pp.Println("𝓯*** 𝔂𝓸𝓾 𝓬𝓸𝓶𝓹𝓲𝓵𝓮𝓻")
}
