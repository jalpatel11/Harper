package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGlobTool_FindsMatchingFiles(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.go"), []byte(""), 0o644)
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte(""), 0o644)
	os.MkdirAll(filepath.Join(dir, "sub"), 0o755)
	os.WriteFile(filepath.Join(dir, "sub", "b.go"), []byte(""), 0o644)

	tool := NewGlobTool()
	out, err := tool.Execute(context.Background(), map[string]any{"pattern": "*.go", "path": dir})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "a.go") {
		t.Fatalf("expected a.go in output, got: %q", out)
	}
	if !strings.Contains(out, "b.go") {
		t.Fatalf("expected sub/b.go in output (recursive), got: %q", out)
	}
	if strings.Contains(out, "a.txt") {
		t.Fatalf("did not expect a.txt in output, got: %q", out)
	}
}

func TestGlobTool_NotesTruncationWhenMaxEntriesExceeded(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 10; i++ {
		os.WriteFile(filepath.Join(dir, string(rune('a'+i))+".go"), []byte(""), 0o644)
	}

	tool := &GlobTool{maxEntries: 3, timeout: time.Minute}
	out, err := tool.Execute(context.Background(), map[string]any{"pattern": "*.go", "path": dir})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "truncated") {
		t.Fatalf("expected a truncation notice in output, got: %q", out)
	}
}
