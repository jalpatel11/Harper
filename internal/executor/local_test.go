package executor

import (
	"context"
	"testing"
	"time"
)

func TestLocalExecutor_Exec_CapturesOutput(t *testing.T) {
	e := NewLocalExecutor(t.TempDir())
	res, err := e.Exec(context.Background(), "echo hello", 5*time.Second)
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if res.Stdout != "hello\n" {
		t.Fatalf("unexpected stdout: %q", res.Stdout)
	}
	if res.ExitCode != 0 {
		t.Fatalf("unexpected exit code: %d", res.ExitCode)
	}
}

func TestLocalExecutor_Exec_NonZeroExit(t *testing.T) {
	e := NewLocalExecutor(t.TempDir())
	res, err := e.Exec(context.Background(), "exit 3", 5*time.Second)
	if err != nil {
		t.Fatalf("Exec should not return a Go error for a nonzero exit: %v", err)
	}
	if res.ExitCode != 3 {
		t.Fatalf("unexpected exit code: %d", res.ExitCode)
	}
}

func TestLocalExecutor_Exec_Timeout(t *testing.T) {
	e := NewLocalExecutor(t.TempDir())
	_, err := e.Exec(context.Background(), "sleep 5", 100*time.Millisecond)
	if err == nil {
		t.Fatalf("expected a timeout error")
	}
}
