package ichiran

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"errors"

	"github.com/go-git/go-git/v5"
	"github.com/gookit/color"
	"github.com/k0kubun/pp"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/adrg/xdg"
	
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"

	"github.com/compose-spec/compose-go/v2/cli"
	"github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/flags"
	"github.com/docker/compose/v2/pkg/api"
	"github.com/docker/compose/v2/pkg/compose"
)

/*
Libraries:
https://pkg.go.dev/github.com/docker/compose/v2@v2.32.1/pkg/compose#NewComposeService
https://pkg.go.dev/github.com/docker/compose/v2@v2.32.1/pkg/api#Service
https://pkg.go.dev/github.com/docker/cli@v27.4.1+incompatible/cli/command
https://pkg.go.dev/github.com/docker/cli@v27.4.1+incompatible/cli/flags
*/

const remote = "https://github.com/tshatrov/ichiran.git"


var (
	// DockerStartTO is the timeout duration for starting Docker containers.
	DockerStartTO = 300 * time.Second
	DockerRebuildTO = 30 * time.Minute
	errNotInitialized = errors.New("project not initialized, was Init() called?")
)


func init() {
	// logger internal to the library. For the logger relaying docker's log see logger.go's IchiranLogConsumer.Level
	log.Logger = zerolog.Nop()
	//log.Logger = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.TimeOnly}).With().Timestamp().Logger()
}

// Docker represents a Docker service manager for ichiran containers.
type Docker struct {
	service		api.Service
	ctx		context.Context
	logger		*ichiranLogConsumer
	project		*types.Project
	ichiranDir	string
}

// NewDocker creates a new Docker service manager instance.
// It initializes the Docker CLI and compose service.
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

	return &Docker{
		service: service,
		ctx:     context.Background(),
		logger:  newIchiranLogConsumer(),
	}, nil
}

// Init initializes the ichiran Docker environment.
// It sets up the project directory and starts containers if needed.
func (id *Docker) Init() error {
	return id.initialize(false, false)
}

// Similar to Init but reduce verbosity of progress output of Docker management (Build, Up,...)
func (id *Docker) InitQuiet() error {
	return id.initialize(false, true)
}

// InitForce initializes the ichiran Docker environment with forced rebuild.
// Similar to Init but forces container rebuilding.
func (id *Docker) InitForce() error {
	DockerStartTO = DockerRebuildTO
	return id.initialize(true, false)
}


func (id *Docker) initialize(NoCache, Quiet bool) (err error) {
	if id.ichiranDir, err = getIchiranDir(); err != nil {
		return fmt.Errorf("failed to get ichiran directory: %w", err)
	}
	
	var needsBuild bool
	if err := id.setupProject(); err != nil {
		log.Warn().Err(err).Msg("setupProject() returned an error")
		needsBuild = true
	}
	
	// Check if ichiran is already running
	stacks, err := id.service.List(id.ctx, api.ListOptions{All: true})
	if err != nil {
		return fmt.Errorf("failed to list stacks: %w", err)
	}

	for _, stack := range stacks {
		if stack.Name == "ichiran" && std(stack.Status) == api.RUNNING {
			log.Info().Msg("ichiran containers already running")
			return nil
		}
	}

	if !needsBuild {
		needsBuild, err = checkIfBuildNeeded(id.ichiranDir)
		if err != nil {
			return fmt.Errorf("failed to check build status: %w", err)
		}
	}
	log.Warn().
		Bool("needsBuild", needsBuild).
		Bool("NoCache", NoCache).
		Msg("init state")

	if needsBuild {
		DockerStartTO = DockerRebuildTO
		if err := id.build(NoCache, Quiet); err != nil {
			return fmt.Errorf("build failed: %w", err)
		}
	}
	
	if err := id.up(); err != nil {
		return fmt.Errorf("up failed: %w", err)
	}
	
	return nil
}

// build downloads/updates the ichiran repository and builds the Docker containers.
// NoCache parameter determines whether to use Docker build cache or not.
func (id *Docker) build(NoCache, Quiet bool) error {
	if id.project == nil {
		return errNotInitialized
	}
	log.Info().Msg("downloading ichiran repository...")
	if _, err := os.Stat(filepath.Join(id.ichiranDir, ".git")); os.IsNotExist(err) {
		log.Info().Msg("Local repository does not exist. Cloning...")
		if err := cloneRepo(remote, id.ichiranDir); err != nil {
			return fmt.Errorf("failed to clone repository: %w", err)
		}
	} else {
		log.Info().Msg("Local repository exists. Pulling changes...")
		if err := pullRepo(id.ichiranDir); err != nil {
			return fmt.Errorf("failed to pull repository: %w", err)
		}
	}
	if err := id.setupProject(); err != nil {
		return fmt.Errorf("failed to setup project: %w", err)
	}
	buildOpts := api.BuildOptions{
		NoCache:  NoCache,
		Quiet:    Quiet,
		Services: id.project.ServiceNames(),
		Deps:     false,
	}

	log.Info().Msg("building containers...")
	if err := id.service.Build(id.ctx, id.project, buildOpts); err != nil {
		log.Error().
			Err(err).
			Str("type", fmt.Sprintf("%T", err)).
			Msg("build failed")
		return fmt.Errorf("build failed: %w", err)
	}

	return nil
}

// up starts the ichiran containers and waits for initialization.
// Returns error if containers fail to start or initialize within timeout.
func (id *Docker) up() error {
	log.Info().Msg("up-ing containers...")
	if id.project == nil {
		return errNotInitialized
	}

	go func(){
		err := id.service.Up(id.ctx, id.project, api.UpOptions{
			Create: api.CreateOptions{
				Services:      id.project.ServiceNames(),
				RemoveOrphans: true,
				IgnoreOrphans: false,
				Recreate:      api.RecreateNever,
				Inherit:       false,
			},
			Start: api.StartOptions{
				Wait:         true,
				WaitTimeout:  DockerStartTO,
				Project:      id.project,
				Services:     id.project.ServiceNames(),
				ExitCodeFrom: "main",
				Attach:       id.logger,
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

	status, err := id.Status()
	if err != nil {
		return fmt.Errorf("status check failed: %w", err)
	}
	if status != api.RUNNING {
		return fmt.Errorf("services failed to reach running state, current raw status: %s", status)
	}

	return nil
}

// Stop stops all running ichiran containers.
func (id *Docker) Stop() error {
	log.Info().Msg("stopping ichiran containers...")
	return id.service.Stop(id.ctx, "ichiran", api.StopOptions{
		Timeout: nil, // Use default timeout
	})
}

// Close is an alias for Stop, implementing io.Closer interface.
func (id *Docker) Close() error {
	return id.Stop()
}


// Status returns the current status of ichiran containers.
// Returns one of the api status constants (RUNNING, STARTING, etc.).
func (id *Docker) Status() (string, error) {
	stacks, err := id.service.List(id.ctx, api.ListOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to list stacks: %w", err)
	}

	for _, stack := range stacks {
		if stack.Name == "ichiran" {
			return std(stack.Status), nil
		}
	}
	return api.UNKNOWN, nil
}





// ############################################################################
// ############################################################################
// ############################################################################




// setupProject initializes the Docker Compose project configuration.
// Creates necessary labels and project structure.
func (id *Docker) setupProject() error {
	if id.project != nil {
		return nil
	}

	options, err := cli.NewProjectOptions(
		[]string{filepath.Join(id.ichiranDir, "docker-compose.yml")},
		cli.WithOsEnv,
		cli.WithDotEnv,
		cli.WithName("ichiran"),
		cli.WithWorkingDirectory(id.ichiranDir),
	)
	if err != nil {
		return fmt.Errorf("failed to create project options: %w", err)
	}

	project, err := cli.ProjectFromOptions(id.ctx, options)
	if err != nil {
		return fmt.Errorf("failed to load project: %w", err)
	}

	x := 0
	//color.Redf("project: %s,\tWorkingDir:%s\n", project.Name, project.WorkingDir)
	for name, s := range project.Services {
		//color.Redf("service: %d %s_%s_%#v\n", x, name, s.ContainerName, s.Entrypoint)
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

	id.project = project
	return nil
}

// checkIfBuildNeeded determines if containers need rebuilding.
// Checks git repository status and container existence.
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


func cloneRepo(repoURL, localPath string) error {
	_, err := git.PlainClone(localPath, false, &git.CloneOptions{
		URL:      repoURL,
		Progress: os.Stdout,
	})
	if err != nil {
		return fmt.Errorf("failed to clone repository: %w", err)
	}
	log.Info().Msg("Repository cloned successfully")
	return nil
}

func pullRepo(localPath string) error {
	repo, err := git.PlainOpen(localPath)
	if err != nil {
		return fmt.Errorf("failed to open repository: %w", err)
	}

	worktree, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("failed to get worktree: %w", err)
	}

	err = worktree.Pull(&git.PullOptions{
		RemoteName: "origin",
		Progress:   os.Stdout,
	})
	if err != nil {
		if err == git.NoErrAlreadyUpToDate {
			log.Info().Msg("Repository is already up-to-date")
			return nil
		}
		return fmt.Errorf("failed to pull repository: %w", err)
	}
	log.Info().Msg("Repository updated successfully")
	return nil
}

// getIchiranDir returns the platform-specific ichiran configuration directory.
func getIchiranDir() (string, error) {
	// Get the base config directory following platform conventions
	configPath, err := xdg.ConfigFile("ichiran")
	if err != nil {
		return "", fmt.Errorf("failed to get config directory: %w", err)
	}
	if err := os.MkdirAll(configPath, 0755); err != nil {
		return "", fmt.Errorf("failed to create ichiran directory: %w", err)
	}
	return configPath, nil
}



// fmt of status isn't that of api constants, I've had: running(2), Unknown
// std standardizes container status strings to api constants.
// Converts various status formats to standard api status constants.
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
