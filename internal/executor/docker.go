package executor

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

type DockerOptions struct {
	Image   string
	WorkDir string
	Network string // "none" or "bridge"
	Memory  string // e.g. "2g", empty = no limit
	CPUs    string // e.g. "2", empty = no limit
}

type DockerExecutor struct {
	containerID string
}

func NewDockerExecutor(ctx context.Context, opts DockerOptions) (*DockerExecutor, error) {
	network := opts.Network
	if network == "" {
		network = "none"
	}

	args := []string{
		"run", "-d",
		"--network", network,
		"-v", fmt.Sprintf("%s:/workspace", opts.WorkDir),
		"-w", "/workspace",
	}
	if opts.Memory != "" {
		args = append(args, "--memory", opts.Memory)
	}
	if opts.CPUs != "" {
		args = append(args, "--cpus", opts.CPUs)
	}
	args = append(args, opts.Image, "sleep", "infinity")

	cmd := exec.CommandContext(ctx, "docker", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("docker run: %w (%s)", err, stderr.String())
	}

	return &DockerExecutor{containerID: strings.TrimSpace(stdout.String())}, nil
}

func (d *DockerExecutor) Exec(ctx context.Context, command string, timeout time.Duration) (ExecResult, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", "exec", "-w", "/workspace", d.containerID, "sh", "-c", command)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return ExecResult{}, ctx.Err()
	}

	exitCode := 0
	if exitErr, ok := err.(*exec.ExitError); ok {
		exitCode = exitErr.ExitCode()
	} else if err != nil {
		return ExecResult{}, err
	}

	return ExecResult{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: exitCode}, nil
}

func (d *DockerExecutor) Close(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "docker", "rm", "-f", d.containerID)
	return cmd.Run()
}
