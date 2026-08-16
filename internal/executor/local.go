package executor

import (
	"bytes"
	"context"
	"os/exec"
	"time"
)

type LocalExecutor struct {
	workDir string
}

func NewLocalExecutor(workDir string) *LocalExecutor {
	return &LocalExecutor{workDir: workDir}
}

func (e *LocalExecutor) Exec(ctx context.Context, command string, timeout time.Duration) (ExecResult, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = e.workDir

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
