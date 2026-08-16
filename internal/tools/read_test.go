package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestReadTool_ReadsFileContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(path, []byte("hello world"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	tool := NewReadTool()
	out, err := tool.Execute(context.Background(), map[string]any{"path": path})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out != "hello world" {
		t.Fatalf("unexpected content: %q", out)
	}
}

func TestReadTool_MissingFileReturnsErrorNotPanic(t *testing.T) {
	tool := NewReadTool()
	_, err := tool.Execute(context.Background(), map[string]any{"path": "/nonexistent/path.txt"})
	if err == nil {
		t.Fatalf("expected an error for a missing file")
	}
}
