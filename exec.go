package ichiran

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/docker/docker/api/types/container"
)

// queryTagVariable names the environment variable that tags the processes
// of one query in the container. The Docker API cannot kill a process it
// started with exec; the tag lets another exec find and kill it.
const queryTagVariable = "LANGKIT_ICHIRAN_QUERY"

// queryKillGrace is how long a query's process may outlive its deadline
// before timeout(1) kills it, when nothing killed it sooner.
const queryKillGrace = 5 * time.Second

// killQueryScript kills every process of the container whose environment
// holds the query tag given as $0.
const killQueryScript = `for p in /proc/[0-9]*; do ` +
	`grep -qzx "` + queryTagVariable + `=$0" "$p/environ" 2>/dev/null && kill -KILL "${p#/proc/}" 2>/dev/null; ` +
	`done; true`

// runLispJSON evaluates one Lisp expression with ichiran-cli inside the
// running container and returns the JSON line it printed.
//
// The process lives no longer than queryCtx. Waiting for its output ends
// as soon as queryCtx is canceled or expires, and the process is then
// killed: reading blocks while Ichiran computes in silence, and a process
// left computing in the container holds a CPU until the container stops.
// It also runs under timeout(1), set to queryCtx's deadline, which ends it
// should this program die first.
func (im *IchiranManager) runLispJSON(queryCtx context.Context, lispExpr string) ([]byte, error) {
	client, err := im.docker.GetClient()
	if err != nil {
		return nil, fmt.Errorf("failed to get Docker client: %w", err)
	}

	containerInfo, err := client.ContainerInspect(queryCtx, im.containerName)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect container: %w", err)
	}
	if !containerInfo.State.Running {
		return nil, fmt.Errorf("container %s is not running", im.containerName)
	}

	tag, err := newQueryTag()
	if err != nil {
		return nil, err
	}
	cmd := []string{"ichiran-cli", "-e", withPooledConnections(lispExpr)}
	if deadline, ok := queryCtx.Deadline(); ok {
		seconds := int(math.Ceil((time.Until(deadline) + queryKillGrace).Seconds()))
		cmd = append([]string{"timeout", "--signal=KILL", strconv.Itoa(seconds)}, cmd...)
	}
	execConfig := container.ExecOptions{
		User:         containerInfo.Config.User,
		Cmd:          cmd,
		Env:          []string{queryTagVariable + "=" + tag},
		AttachStdout: true,
		AttachStderr: true,
		Tty:          false,
		Privileged:   false,
	}

	exec, err := client.ContainerExecCreate(queryCtx, im.containerName, execConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create exec: %w", err)
	}

	resp, err := client.ContainerExecAttach(queryCtx, exec.ID, container.ExecStartOptions{})
	if err != nil {
		// The process may have started all the same.
		im.killQuery(tag)
		return nil, fmt.Errorf("failed to attach to exec: %w", err)
	}
	defer resp.Close()

	type readResult struct {
		output []byte
		err    error
	}
	read := make(chan readResult, 1)
	go func() {
		output, err := extractJSONFromDockerOutput(queryCtx, resp.Reader)
		read <- readResult{output, err}
	}()
	var result readResult
	select {
	case result = <-read:
	case <-queryCtx.Done():
		// Closing the connection is what ends a read blocked on silence.
		resp.Close()
		<-read
	}
	if queryCtx.Err() != nil {
		im.killQuery(tag)
		return nil, stoppedQueryError(queryCtx.Err())
	}

	output, err := result.output, result.err
	if err != nil {
		if errors.Is(err, errNoJSONFound) {
			if inspect, inspectErr := client.ContainerExecInspect(queryCtx, exec.ID); inspectErr == nil {
				return nil, fmt.Errorf("failed to read exec output (exit code %d): %w", inspect.ExitCode, err)
			}
		}
		return nil, fmt.Errorf("failed to read exec output: %w", err)
	}

	inspect, err := client.ContainerExecInspect(queryCtx, exec.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect exec: %w", err)
	}
	if inspect.ExitCode != 0 {
		return nil, fmt.Errorf("command failed with exit code %d: %s",
			inspect.ExitCode, string(output))
	}
	return output, nil
}

// stoppedQueryError reports a query whose context ended before Ichiran
// answered. It wraps the context's error, so callers can tell a
// cancellation from a deadline.
func stoppedQueryError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("Ichiran gave no result before the query's deadline and was stopped: %w", err)
	}
	return fmt.Errorf("Ichiran query canceled: %w", err)
}

// killQuery kills the processes of a query in the container. The query's
// context is over by then, so the kill has its own short deadline; should
// it fail, timeout(1) still ends the process at the query's deadline.
func (im *IchiranManager) killQuery(tag string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client, err := im.docker.GetClient()
	if err == nil {
		var exec container.ExecCreateResponse
		exec, err = client.ContainerExecCreate(ctx, im.containerName, container.ExecOptions{
			Cmd: []string{"sh", "-c", killQueryScript, tag},
		})
		if err == nil {
			err = client.ContainerExecStart(ctx, exec.ID, container.ExecStartOptions{Detach: true})
		}
	}
	if err != nil {
		Logger.Warn().Err(err).Msg("Could not kill a stopped Ichiran query; timeout(1) ends it at its deadline")
	}
}

// newQueryTag returns a random tag for the processes of one query.
func newQueryTag() (string, error) {
	tag := make([]byte, 12)
	if _, err := rand.Read(tag); err != nil {
		return "", fmt.Errorf("tagging the Ichiran query: %w", err)
	}
	return hex.EncodeToString(tag), nil
}
