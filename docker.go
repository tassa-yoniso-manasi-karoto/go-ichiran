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
	"github.com/adrg/xdg"
	
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"

	"github.com/compose-spec/compose-go/v2/cli"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/flags"
	"github.com/docker/compose/v2/pkg/api"
	"github.com/docker/compose/v2/pkg/compose"
)

var DockerStartTO = 300 * time.Second

const remote = "https://github.com/tshatrov/ichiran.git"

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
	// Check if ichiran is already running
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

	// Get the ichiran directory
	ichiranDir, err := getIchiranDir()
	if err != nil {
		return fmt.Errorf("failed to get ichiran directory: %w", err)
	}

	if err := os.MkdirAll(ichiranDir, 0755); err != nil {
		return fmt.Errorf("failed to create ichiran directory: %w", err)
	}

	// Check if build is necessary
	needsBuild, err := checkIfBuildNeeded(ichiranDir)
	if err != nil {
		return fmt.Errorf("failed to check build status: %w", err)
	}

	if needsBuild {
		log.Info().Msg("downloading ichiran repository...")
		// Check for .git directory instead of the directory itself
		if _, err := os.Stat(filepath.Join(ichiranDir, ".git")); os.IsNotExist(err) {
			log.Info().Msg("Local repository does not exist. Cloning...")
			cloneRepo(remote, ichiranDir)
		} else {
			log.Info().Msg("Local repository exists. Pulling changes...")
			pullRepo(ichiranDir)
		}
	}
	options, err := cli.NewProjectOptions(
		[]string{filepath.Join(ichiranDir, "docker-compose.yml")},
		cli.WithOsEnv,
		cli.WithDotEnv,
		cli.WithName("ichiran"),
		cli.WithWorkingDirectory(ichiranDir),
	)
	if err != nil {
		return fmt.Errorf("failed to create project options: %w", err)
	}

	project, err := cli.ProjectFromOptions(id.ctx, options)
	if err != nil {
		return fmt.Errorf("failed to load project: %w", err)
	}

	x := 0
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
		NoCache:  false,
		Quiet:    false,
		Services: project.ServiceNames(),
		Deps:     false,
	}

	if needsBuild {
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






func checkIfBuildNeeded(ichiranDir string) (bool, error) {
	// Check if the git repository exists by looking for .git directory
	gitDir := filepath.Join(ichiranDir, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		log.Info().Msg("Git repository not found, build needed")
		return true, nil
	}

	repo, err := git.PlainOpen(ichiranDir)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to open git repository")
		return true, nil
	}

	// Get the current HEAD
	head, err := repo.Head()
	if err != nil {
		log.Warn().Err(err).Msg("Failed to get HEAD")
		return true, nil
	}

	// Get the remote reference
	remote, err := repo.Remote("origin")
	if err != nil {
		log.Warn().Err(err).Msg("Failed to get remote")
		return true, nil
	}

	// Fetch the latest changes
	err = remote.Fetch(&git.FetchOptions{
		Force: true,
	})
	if err != nil && err != git.NoErrAlreadyUpToDate {
		log.Warn().Err(err).Msg("Failed to fetch from remote")
		return true, nil
	}

	// Get the remote HEAD
	refs, err := remote.List(&git.ListOptions{})
	if err != nil {
		log.Warn().Err(err).Msg("Failed to list refs")
		return true, nil
	}

	for _, ref := range refs {
		if ref.Name().String() == "refs/heads/master" {
			// If local and remote HEADs are different, build is needed
			if head.Hash() != ref.Hash() {
				log.Info().Msg("Local and remote HEADs differ, build needed")
				return true, nil
			}
			break
		}
	}

	// Check if docker images exist and are running
	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		return false, fmt.Errorf("failed to create Docker client: %w", err)
	}
	defer cli.Close()

	containers, err := cli.ContainerList(context.Background(), container.ListOptions{})
	if err != nil {
		return false, fmt.Errorf("failed to list containers: %w", err)
	}

	// Check for required containers
	requiredContainers := map[string]bool{
		"ichiran-main-1": false,
		"ichiran-pg-1":   false,
	}

	for _, container := range containers {
		for _, name := range container.Names {
			// Container names come with a leading slash, so we trim it
			cleanName := strings.TrimPrefix(name, "/")
			if _, exists := requiredContainers[cleanName]; exists {
				requiredContainers[cleanName] = true
			}
		}
	}

	// Check if all required containers are running
	for containerName, isRunning := range requiredContainers {
		if !isRunning {
			log.Info().Str("container", containerName).Msg("Required container not running")
			return true, nil
		}
	}

	return false, nil
}

func getIchiranDir() (string, error) {
	// Get the base config directory following platform conventions
	configPath, err := xdg.ConfigFile("ichiran")
	if err != nil {
		return "", fmt.Errorf("failed to get config directory: %w", err)
	}
	return configPath, nil
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
