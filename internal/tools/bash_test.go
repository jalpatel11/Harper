package tools

import (
	"context"
	"testing"
	"time"

	"harper/internal/executor"
)

type fakeExecutor struct {
	lastCommand string
	result      executor.ExecResult
	err         error
}

func (f *fakeExecutor) Exec(ctx context.Context, command string, timeout time.Duration) (executor.ExecResult, error) {
	f.lastCommand = command
	return f.result, f.err
}

func TestBashTool_ReturnsStdoutOnSuccess(t *testing.T) {
	fake := &fakeExecutor{result: executor.ExecResult{Stdout: "hi\n", ExitCode: 0}}
	tool := NewBashTool(fake, 5*time.Second)

	out, err := tool.Execute(context.Background(), map[string]any{"command": "echo hi"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out != "hi\n" {
		t.Fatalf("unexpected output: %q", out)
	}
	if fake.lastCommand != "echo hi" {
		t.Fatalf("expected command to be forwarded to executor, got %q", fake.lastCommand)
	}
}

func TestBashTool_NonZeroExitReturnedAsContentNotError(t *testing.T) {
	fake := &fakeExecutor{result: executor.ExecResult{Stdout: "", Stderr: "boom", ExitCode: 1}}
	tool := NewBashTool(fake, 5*time.Second)

	out, err := tool.Execute(context.Background(), map[string]any{"command": "false"})
	if err != nil {
		t.Fatalf("expected no Go error for a nonzero exit, got: %v", err)
	}
	if out == "" {
		t.Fatalf("expected the error content (stderr/exit code) in the returned string")
	}
}
