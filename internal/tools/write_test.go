package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteTool_CreatesFileWithContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")

	tool := NewWriteTool()
	_, err := tool.Execute(context.Background(), map[string]any{"path": path, "content": "written content"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(data) != "written content" {
		t.Fatalf("unexpected content: %q", string(data))
	}
}

func TestWriteTool_OverwritesExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")
	os.WriteFile(path, []byte("old"), 0o644)

	tool := NewWriteTool()
	_, err := tool.Execute(context.Background(), map[string]any{"path": path, "content": "new"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "new" {
		t.Fatalf("unexpected content: %q", string(data))
	}
}
