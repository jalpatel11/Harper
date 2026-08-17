package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGrepTool_FindsMatchingLines(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("func Foo() {}\nfunc Bar() {}\n"), 0o644)
	os.MkdirAll(filepath.Join(dir, "sub"), 0o755)
	os.WriteFile(filepath.Join(dir, "sub", "b.go"), []byte("func Baz() {}\n"), 0o644)

	tool := NewGrepTool()
	out, err := tool.Execute(context.Background(), map[string]any{"pattern": "func (Foo|Baz)", "path": dir})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "a.go") || !strings.Contains(out, "Foo") {
		t.Fatalf("expected a.go/Foo match in output, got: %q", out)
	}
	if !strings.Contains(out, "b.go") || !strings.Contains(out, "Baz") {
		t.Fatalf("expected sub/b.go/Baz match in output, got: %q", out)
	}
	if strings.Contains(out, "Bar") {
		t.Fatalf("did not expect a Bar match, got: %q", out)
	}
}

func TestGrepTool_NotesTruncationWhenMaxEntriesExceeded(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 10; i++ {
		os.WriteFile(filepath.Join(dir, string(rune('a'+i))+".txt"), []byte("match\n"), 0o644)
	}

	tool := &GrepTool{maxEntries: 3, timeout: time.Minute}
	out, err := tool.Execute(context.Background(), map[string]any{"pattern": "match", "path": dir})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "truncated") {
		t.Fatalf("expected a truncation notice in output, got: %q", out)
	}
}
