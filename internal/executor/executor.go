package executor

import (
	"context"
	"time"
)

type ExecResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

type Executor interface {
	Exec(ctx context.Context, command string, timeout time.Duration) (ExecResult, error)
}
