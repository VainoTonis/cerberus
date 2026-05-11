package docker

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// Mount represents a volume mount from host to container.
type Mount struct {
	Host      string // Host path
	Container string // Container path
	ReadOnly  bool   // Read-only flag
}

// RunArgs configures a docker run invocation.
type RunArgs struct {
	Image    string    // Docker image name/tag
	Workdir  string    // Working directory in container (-w)
	Mounts   []Mount   // Volume mounts
	Cmd      []string  // Command to run in container
	EnvFile  string    // Path to env file (--env-file), empty if unused
	Networks []string  // Networks to attach (--network)
	Stdout   io.Writer // Stdout destination
	Stderr   io.Writer // Stderr destination
}

// Run executes a docker run command in the foreground, capturing output and container ID.
// Returns the container ID (may be empty if cidfile unreadable), exit code, and error.
// The container is left running on the host after docker run exits (no --rm flag).
// Docker run exits when the container process exits; output is streamed to args.Stdout/Stderr.
func Run(ctx context.Context, args RunArgs) (containerID string, exitCode int, err error) {
	// Create temporary cidfile to capture container ID.
	cidfile, err := os.CreateTemp("", "docker-cid-*.txt")
	if err != nil {
		return "", 0, fmt.Errorf("create cidfile: %w", err)
	}
	cidfilePath := cidfile.Name()
	cidfile.Close()
	defer os.Remove(cidfilePath)

	// Build docker run command.
	cmd := exec.CommandContext(ctx, "docker", "run")

	// Add cidfile flag.
	cmd.Args = append(cmd.Args, "--cidfile", cidfilePath)

	// Add mounts.
	for _, m := range args.Mounts {
		mount := fmt.Sprintf("%s:%s", m.Host, m.Container)
		if m.ReadOnly {
			mount = fmt.Sprintf("%s:ro", mount)
		}
		cmd.Args = append(cmd.Args, "-v", mount)
	}

	// Add env file if provided.
	if args.EnvFile != "" {
		cmd.Args = append(cmd.Args, "--env-file", args.EnvFile)
	}

	// Add networks.
	for _, net := range args.Networks {
		cmd.Args = append(cmd.Args, "--network", net)
	}

	// Add workdir.
	if args.Workdir != "" {
		cmd.Args = append(cmd.Args, "-w", args.Workdir)
	}

	// Add image.
	cmd.Args = append(cmd.Args, args.Image)

	// Add container command.
	cmd.Args = append(cmd.Args, args.Cmd...)

	// Set stdout/stderr.
	cmd.Stdout = args.Stdout
	cmd.Stderr = args.Stderr

	// Run the command.
	err = cmd.Run()
	exitCode = 0
	if err != nil {
		exitErr, ok := err.(*exec.ExitError)
		if ok {
			exitCode = exitErr.ExitCode()
		} else {
			return "", 0, fmt.Errorf("docker run: %w", err)
		}
	}

	// Read container ID from cidfile.
	cidBytes, err := os.ReadFile(cidfilePath)
	if err != nil {
		// cidfile unreadable; return empty containerID but preserve exit code and error from docker run.
		return "", exitCode, nil
	}
	containerID = strings.TrimSpace(string(cidBytes))

	return containerID, exitCode, nil
}

// Stop stops and removes a container by ID.
// Ignores errors if the container is not found or already stopped.
func Stop(containerID string) error {
	if containerID == "" {
		return nil
	}

	cmd := exec.Command("docker", "rm", "-f", containerID)
	err := cmd.Run()

	// docker rm -f returns non-zero if container not found. Ignore this case.
	if err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			// Command exited with non-zero; treat as "container not found" and ignore.
			return nil
		}
		return fmt.Errorf("docker rm: %w", err)
	}

	return nil
}
