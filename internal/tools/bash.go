package tools

import (
	"context"
	"fmt"
	"time"

	"harper/internal/executor"
)

type BashTool struct {
	exec    executor.Executor
	timeout time.Duration
}

func NewBashTool(exec executor.Executor, timeout time.Duration) *BashTool {
	return &BashTool{exec: exec, timeout: timeout}
}

func (t *BashTool) Name() string        { return "Bash" }
func (t *BashTool) Description() string { return "Run a shell command in the sandbox." }
func (t *BashTool) InputSchema() map[string]any {
	return map[string]any{
		"type":       "object",
		"required":   []string{"command"},
		"properties": map[string]any{"command": map[string]any{"type": "string"}},
	}
}

func (t *BashTool) Execute(ctx context.Context, input map[string]any) (string, error) {
	command, ok := input["command"].(string)
	if !ok || command == "" {
		return "", fmt.Errorf("bash: missing required \"command\" argument")
	}

	res, err := t.exec.Exec(ctx, command, t.timeout)
	if err != nil {
		return fmt.Sprintf("command failed to run: %v", err), nil
	}
	if res.ExitCode != 0 {
		return fmt.Sprintf("exit code %d\nstdout:\n%s\nstderr:\n%s", res.ExitCode, res.Stdout, res.Stderr), nil
	}
	return res.Stdout, nil
}
