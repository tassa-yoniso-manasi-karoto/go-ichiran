package ichiran

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/gookit/color"
	"github.com/k0kubun/pp"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/compose-spec/compose-go/v2/cli"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/flags"
	"github.com/docker/compose/v2/pkg/api"
	"github.com/docker/compose/v2/pkg/compose"
)

var DockerStartTO = 300 * time.Second

type IchiranLogConsumer struct {
	Prefix      string
	ShowService bool
	ShowType    bool
	Level       zerolog.Level
	initChan    chan struct{}
	failedChan  chan error
}

func NewIchiranLogConsumer() *IchiranLogConsumer {
	return &IchiranLogConsumer{
		Prefix:      "compose",
		ShowService: true,
		ShowType:    true,
		Level:       zerolog.DebugLevel,
		initChan:    make(chan struct{}),
		failedChan:  make(chan error),
	}
}

func (l *IchiranLogConsumer) Log(containerName, message string) {
	if strings.Contains(message, "All set, awaiting commands") {
		select {
		case l.initChan <- struct{}{}:
		default: // Channel already closed or message already sent
		}
	}

	// Regular logging
	lines := strings.Split(message, "\n")
	for _, line := range lines {
		if line = strings.TrimSpace(line); line != "" {
			event := log.Debug()
			if l.Level != zerolog.DebugLevel {
				event = log.WithLevel(l.Level)
			}

			if l.ShowService {
				event = event.Str("service", containerName)
			}
			if l.ShowType {
				event = event.Str("type", "stdout")
			}
			if l.Prefix != "" {
				event = event.Str("component", l.Prefix)
			}

			event.Msg(line)
		}
	}
}

func (l *IchiranLogConsumer) Err(containerName, message string) {
	lines := strings.Split(message, "\n")
	for _, line := range lines {
		if line = strings.TrimSpace(line); line != "" {
			event := log.Error()
			if l.ShowService {
				event = event.Str("service", containerName)
			}
			if l.ShowType {
				event = event.Str("type", "stderr")
			}
			if l.Prefix != "" {
				event = event.Str("component", l.Prefix)
			}

			event.Msg(line)
		}
	}
}

func (l *IchiranLogConsumer) Status(container, msg string) {
	event := log.Info()
	if l.ShowService {
		event = event.Str("service", container)
	}
	if l.ShowType {
		event = event.Str("type", "status")
	}
	if l.Prefix != "" {
		event = event.Str("component", l.Prefix)
	}

	event.Msg(msg)
}

func (l *IchiranLogConsumer) Register(container string) {
	log.Info().
		Str("container", container).
		Str("type", "register").
		Msg("container registered")
}

type Docker struct {
	service api.Service
	ctx     context.Context
	logger  *IchiranLogConsumer
}

func NewDocker() (*Docker, error) {
	cli, err := command.NewDockerCli()
	if err != nil {
		return nil, fmt.Errorf("failed to spawn Docker CLI: %v", err)
	}

	// Initialize with standard streams
	err = cli.Initialize(flags.NewClientOptions())
	if err != nil {
		return nil, fmt.Errorf("failed to initialize Docker CLI: %w", err)
	}

	// Create compose service
	service := compose.NewComposeService(cli)

	logger := NewIchiranLogConsumer()
	// Configure logger as needed
	logger.ShowService = true
	logger.ShowType = true
	logger.Prefix = "ichiran"
	logger.Level = zerolog.InfoLevel // Or whatever level you prefer

	return &Docker{
		service: service,
		ctx:     context.Background(),
		logger:  logger,
	}, nil
}

func (id *Docker) Initialize() error {
	stacks, err := id.service.List(id.ctx, api.ListOptions{All: true})
	if err != nil {
		return fmt.Errorf("failed to list stacks: %w", err)
	}

	for _, stack := range stacks {
		if stack.Name == "ichiran" && stack.Status == api.RUNNING {
			log.Info().Msg("ichiran containers already running")
			return nil
		}
	}
	/*
		if _, err := os.Stat(tmpDir); err != nil {
			if err = os.Mkdir(tmpDir, os.ModeDir); err != nil {
				log.Fatal().Err(err).Msg("can't create tmpDir")
			}
		}*/
	tmpDir, err := os.MkdirTemp("", "ichiran") // FIXME MkdirTemp is not suitable and must be changed
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)
	log.Info().Str("dir", tmpDir).Msg("created temp directory")
	mustBuild := true // FIXME
	if mustBuild {
		log.Info().Msg("downloading ichiran repository...")
		// Check if the directory already exists
		if _, err := os.Stat(tmpDir); os.IsNotExist(err) {
			// Directory does not exist, clone the repository
			log.Info().Msg("Local repository does not exist. Cloning...")
			cloneRepo("https://github.com/tshatrov/ichiran.git", tmpDir)
		} else {
			// Directory exists, pull changes
			log.Info().Msg("Local repository exists. Pulling changes...")
			pullRepo(tmpDir)
		}
	}
	options, err := cli.NewProjectOptions(
		[]string{filepath.Join(tmpDir, "docker-compose.yml")},
		cli.WithOsEnv,
		cli.WithDotEnv,
		cli.WithName("ichiran"),
		cli.WithWorkingDirectory(tmpDir),
	)
	if err != nil {
		return fmt.Errorf("failed to create project options: %w", err)
	}

	project, err := cli.ProjectFromOptions(id.ctx, options)
	if err != nil {
		return fmt.Errorf("failed to load project: %w", err)
	}
	x := 0
	// Add required labels to services
	color.Redf("project: %s,\tWorkingDir:%s\n", project.Name, project.WorkingDir)
	for name, s := range project.Services {
		color.Redf("service: %d %s_%s_%#v\n", x, name, s.ContainerName, s.Entrypoint)
		x += 1
		s.CustomLabels = map[string]string{
			api.ProjectLabel:     project.Name,
			api.ServiceLabel:     name,
			api.VersionLabel:     api.ComposeVersion,
			api.WorkingDirLabel:  project.WorkingDir,
			api.ConfigFilesLabel: strings.Join(project.ComposeFiles, ","),
			api.OneoffLabel:      "False",
		}
		project.Services[name] = s
	}

	buildOpts := api.BuildOptions{
		Pull:     true,
		Push:     false,
		Progress: "auto",
		NoCache:  false, //TODO
		Quiet:    false,
		Services: project.ServiceNames(),
		Deps:     false,
	}
	if mustBuild {
		log.Info().Msg("building containers...")
		err = id.service.Build(id.ctx, project, buildOpts)
		if err != nil {
			log.Error().
				Err(err).
				Str("type", fmt.Sprintf("%T", err)).
				Msg("build failed")
			return fmt.Errorf("build failed: %w", err)
		}
	}
	log.Info().Msg("up-ing containers...")
	go func() {
		err = id.service.Up(id.ctx, project, api.UpOptions{
			Create: api.CreateOptions{
				Build:         &buildOpts,
				Services:      project.ServiceNames(),
				RemoveOrphans: true,
				IgnoreOrphans: false,
				Recreate:      api.RecreateNever,
				Inherit:       false,
				QuietPull:     false,
			},
			Start: api.StartOptions{
				Wait:         true,
				WaitTimeout:  DockerStartTO,
				Project:      project,
				Services:     project.ServiceNames(),
				ExitCodeFrom: "main",
				Attach:       id.logger,
				//AttachTo: project.ServiceNames(),
			},
		})
		if err != nil {
			id.logger.failedChan <- fmt.Errorf("container startup failed: %v", err)
		}
	}()

	// Wait for initialization
	log.Info().Msg("waiting for ichiran to initialize...")
	select {
	case <-id.logger.initChan:
		log.Info().Msg("ichiran initialization complete")
	case err := <-id.logger.failedChan:
		log.Info().Msg("ichiran initialization FAILED")
		return err
	case <-time.After(DockerStartTO):
		return fmt.Errorf("timeout waiting for ichiran to initialize")
	}
	close(id.logger.initChan)
	close(id.logger.failedChan)

	status, err := id.Status()
	if err != nil {
		return fmt.Errorf("status check failed: %w", err)
	}
	if std(status) != api.RUNNING {
		return fmt.Errorf("services failed to reach running state, current raw status: %s", status)
	}
	return nil
}

func cloneRepo(repoURL, localPath string) {
	_, err := git.PlainClone(localPath, false, &git.CloneOptions{
		URL:      repoURL,
		Progress: os.Stdout,
	})
	if err != nil {
		log.Error().Err(err).Msg("Failed to clone repository")
		return
	}
	log.Info().Msg("Repository cloned successfully")
}

func pullRepo(tmpDir string) {
	// Open the existing repository
	repo, err := git.PlainOpen(tmpDir)
	if err != nil {
		log.Error().Err(err).Msg("Failed to open repository")
		return
	}

	// Get the working tree
	worktree, err := repo.Worktree()
	if err != nil {
		log.Error().Err(err).Msg("Failed to get worktree")
		return
	}

	// Pull the latest changes
	err = worktree.Pull(&git.PullOptions{
		RemoteName: "origin",
		Progress:   os.Stdout,
	})
	if err != nil {
		if err == git.NoErrAlreadyUpToDate {
			log.Info().Msg("Repository is already up-to-date")
		} else {
			log.Error().Err(err).Msg("Failed to pull repository")
		}
		return
	}
	log.Info().Msg("Repository updated successfully")
}

func (id *Docker) Stop() error {
	log.Info().Msg("stopping ichiran containers...")
	return id.service.Stop(id.ctx, "ichiran", api.StopOptions{
		Timeout: nil, // Use default timeout
	})
}

func (id *Docker) Close() error {
	return id.Stop()
}

func (id *Docker) Down() error {
	log.Info().Msg("removing ichiran containers and resources...")
	return id.service.Down(id.ctx, "ichiran", api.DownOptions{
		RemoveOrphans: true,
		Volumes:       true,    // Remove volumes as well
		Images:        "local", // Remove locally built images
	})
}

func (id *Docker) Status() (string, error) {
	stacks, err := id.service.List(id.ctx, api.ListOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to list stacks: %w", err)
	}

	for _, stack := range stacks {
		if stack.Name == "ichiran" {
			return stack.Status, nil
		}
	}
	return api.UNKNOWN, nil
}

// fmt of status isn't that of api constants, I've had: running(2), Unknown
func std(status string) string {
	status = strings.ToUpper(status)
	switch {
	case strings.HasPrefix(status, "RUNNING"):
		return api.RUNNING
	case strings.HasPrefix(status, "STARTING"):
		return api.STARTING
	case strings.HasPrefix(status, "UPDATING"):
		return api.UPDATING
	case strings.HasPrefix(status, "REMOVING"):
		return api.REMOVING
	case strings.HasPrefix(status, "UNKNOWN"):
		return api.UNKNOWN
	}
	return api.FAILED
}

func placeholder3456543() {
	color.Redln(" 𝒻*** 𝓎ℴ𝓊 𝒸ℴ𝓂𝓅𝒾𝓁ℯ𝓇")
	pp.Println("𝓯*** 𝔂𝓸𝓾 𝓬𝓸𝓶𝓹𝓲𝓵𝓮𝓻")
}
