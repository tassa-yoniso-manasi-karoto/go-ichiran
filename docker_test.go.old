package ichiran

import (
	"testing"
	"time"
	"os"
	"path/filepath"
	"context"

	"github.com/docker/compose/v2/pkg/api"
	"github.com/docker/docker/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManualContainerAccess(t *testing.T) {
	if os.Getenv("CI") != "" {
		t.Skip("Skipping manual container test in CI environment")
	}

	if os.Getenv("ICHIRAN_MANUAL_TEST") != "1" {
		t.Skip(`Skipping manual container test. 
To run this test, start the containers manually and set ICHIRAN_MANUAL_TEST=1
Example:
    docker compose up -d
    ICHIRAN_MANUAL_TEST=1 go test -v -run TestManualContainerAccess
`)
	}

	// Create Docker client
	docker, err := NewDocker()
	require.NoError(t, err, "Failed to create Docker client")

	tests := []struct {
		name string
		test func(t *testing.T, d *Docker)
	}{
		{
			name: "Container Status Check",
			test: func(t *testing.T, d *Docker) {
				status, err := d.Status()
				require.NoError(t, err, "Failed to get container status")
				assert.Equal(t, api.RUNNING, status, "Containers should be in RUNNING state")
			},
		},
		{
			name: "Main Container Accessibility",
			test: func(t *testing.T, d *Docker) {
				// Try to analyze a simple text to check if main container is responsive
				tokens, err := Analyze("こんにちは")
				require.NoError(t, err, "Failed to analyze text")
				assert.NotNil(t, tokens, "Tokens should not be nil")
				assert.Greater(t, len(*tokens), 0, "Should have at least one token")
				
				// Verify the analyzed content
				firstToken := (*tokens)[0]
				assert.Equal(t, "こんにちは", firstToken.Surface, "Surface form should match input")
				assert.NotEmpty(t, firstToken.Reading, "Reading should not be empty")
				assert.NotEmpty(t, firstToken.Kana, "Kana should not be empty")
			},
		},
		{
			name: "Container Network Check",
			test: func(t *testing.T, d *Docker) {
				cli, err := client.NewClientWithOpts(client.FromEnv)
				require.NoError(t, err, "Failed to create Docker client")
				defer cli.Close()

				// Check both main and postgres containers
				containers := []string{"ichiran-main-1", "ichiran-pg-1"}
				for _, containerName := range containers {
					container, err := cli.ContainerInspect(context.Background(), containerName)
					require.NoError(t, err, "Failed to inspect container: "+containerName)
					
					// Check if container is running
					assert.True(t, container.State.Running, "Container should be running: "+containerName)
					
					// Check if container has network access
					assert.NotEmpty(t, container.NetworkSettings.Networks, 
						"Container should have network settings: "+containerName)
					
					// Check for specific network configurations
					for networkName, network := range container.NetworkSettings.Networks {
						assert.NotEmpty(t, network.IPAddress, 
							"Container should have IP address on network %s: %s", 
							networkName, containerName)
					}
				}
			},
		},
		{
			name: "Database Connection Check",
			test: func(t *testing.T, d *Docker) {
				// Try multiple analyses to ensure database connection is stable
				inputs := []string{"私", "食べる", "走る"}
				for _, input := range inputs {
					tokens, err := Analyze(input)
					require.NoError(t, err, "Failed to analyze: "+input)
					assert.NotNil(t, tokens, "Tokens should not be nil for: "+input)
					assert.Greater(t, len(*tokens), 0, 
						"Should have at least one token for: "+input)
				}
			},
		},
		{
			name: "Resource Usage Check",
			test: func(t *testing.T, d *Docker) {
				cli, err := client.NewClientWithOpts(client.FromEnv)
				require.NoError(t, err, "Failed to create Docker client")
				defer cli.Close()

				containers := []string{"ichiran-main-1", "ichiran-pg-1"}
				for _, containerName := range containers {
					stats, err := cli.ContainerStats(context.Background(), containerName, false)
					require.NoError(t, err, "Failed to get stats for: "+containerName)
					defer stats.Body.Close()

					// Just checking if we can get stats (container is responsive)
					assert.NotNil(t, stats.Body, "Should be able to get container stats: "+containerName)
				}
			},
		},
	}

	// Run all subtests
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.test(t, docker)
		})
	}
}

func TestNewDocker(t *testing.T) {
	docker, err := NewDocker()
	require.NoError(t, err)
	require.NotNil(t, docker)
	assert.NotNil(t, docker.service)
	assert.NotNil(t, docker.ctx)
	assert.NotNil(t, docker.logger)
}

func TestDockerInit(t *testing.T) {
	docker, err := NewDocker()
	require.NoError(t, err)
	
	// Test normal initialization
	err = docker.Init()
	require.NoError(t, err)
	
	// Check status after initialization
	status, err := docker.Status()
	require.NoError(t, err)
	assert.Equal(t, api.RUNNING, status)
	
	// Test second initialization (should detect running containers)
	err = docker.Init()
	require.NoError(t, err)
	
	docker.Close()
}

func TestDockerInitQuiet(t *testing.T) {
	docker, err := NewDocker()
	require.NoError(t, err)
	
	err = docker.InitQuiet()
	require.NoError(t, err)
	
	status, err := docker.Status()
	require.NoError(t, err)
	assert.Equal(t, api.RUNNING, status)
	
	docker.Close()
}

func TestDockerStop(t *testing.T) {
	docker, err := NewDocker()
	require.NoError(t, err)
	
	// Initialize first
	err = docker.Init()
	require.NoError(t, err)
	
	// Stop containers
	err = docker.Stop()
	require.NoError(t, err)
	
	// Give some time for containers to stop
	time.Sleep(5 * time.Second)
	
	// Check status
	status, err := docker.Status()
	require.NoError(t, err)
	assert.NotEqual(t, api.RUNNING, status)
}

func TestGetIchiranDir(t *testing.T) {
	dir, err := getIchiranDir()
	require.NoError(t, err)
	assert.NotEmpty(t, dir)
	
	// Check if directory exists
	_, err = os.Stat(dir)
	assert.NoError(t, err)
	
	// Check if it's a directory
	info, err := os.Stat(dir)
	assert.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestCheckIfBuildNeeded(t *testing.T) {
	dir, err := getIchiranDir()
	require.NoError(t, err)
	
	// Test when .git directory doesn't exist
	tempDir := filepath.Join(os.TempDir(), "ichiran-test")
	defer os.RemoveAll(tempDir)
	
	needed, err := checkIfBuildNeeded(tempDir)
	assert.NoError(t, err)
	assert.True(t, needed, "Build should be needed when .git directory doesn't exist")
	
	// Test with actual ichiran directory
	needed, err = checkIfBuildNeeded(dir)
	assert.NoError(t, err)
	// Result depends on state of local repository and containers
}

func TestStd(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"running(2)", api.RUNNING},
		{"RUNNING", api.RUNNING},
		{"starting", api.STARTING},
		{"STARTING(1)", api.STARTING},
		{"updating", api.UPDATING},
		{"removing", api.REMOVING},
		{"Unknown", api.UNKNOWN},
		{"UNKNOWN", api.UNKNOWN},
		{"invalid", api.FAILED},
		{"", api.FAILED},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := std(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// func TestDockerInitForce(t *testing.T) {
// 	docker, err := NewDocker()
// 	require.NoError(t, err)
// 	
// 	// Store original timeout
// 	originalTO := DockerStartTO
// 	defer func() {
// 		DockerStartTO = originalTO
// 	}()
// 	
// 	err = docker.InitForce()
// 	require.NoError(t, err)
// 	
// 	// Verify that timeout was changed
// 	assert.Equal(t, DockerRebuildTO, DockerStartTO)
// 	
// 	// Check status
// 	status, err := docker.Status()
// 	require.NoError(t, err)
// 	assert.Equal(t, api.RUNNING, status)
// }

// TestSetupProject tests the project setup functionality
func TestSetupProject(t *testing.T) {
	docker, err := NewDocker()
	require.NoError(t, err)
	
	// Get ichiran directory
	dir, err := getIchiranDir()
	require.NoError(t, err)
	docker.ichiranDir = dir
	
	// Test project setup
	err = docker.setupProject()
	require.NoError(t, err)
	
	// Verify project configuration
	assert.NotNil(t, docker.project)
	assert.Equal(t, "ichiran", docker.project.Name)
	assert.Contains(t, docker.project.Services, "main")
	assert.Contains(t, docker.project.Services, "pg")
	
	// Verify service labels
	for _, service := range docker.project.Services {
		assert.Contains(t, service.CustomLabels, api.ProjectLabel)
		assert.Contains(t, service.CustomLabels, api.ServiceLabel)
		assert.Contains(t, service.CustomLabels, api.VersionLabel)
		assert.Contains(t, service.CustomLabels, api.WorkingDirLabel)
		assert.Contains(t, service.CustomLabels, api.ConfigFilesLabel)
		assert.Contains(t, service.CustomLabels, api.OneoffLabel)
	}
}

// TestClose tests the Close method (alias for Stop)
func TestClose(t *testing.T) {
	docker, err := NewDocker()
	require.NoError(t, err)
	
	// Initialize first
	err = docker.Init()
	require.NoError(t, err)
	
	// Test Close
	err = docker.Close()
	require.NoError(t, err)
	
	// Give some time for containers to stop
	time.Sleep(20 * time.Second)
	
	// Check status
	status, err := docker.Status()
	require.NoError(t, err)
	assert.NotEqual(t, api.RUNNING, status)
}
