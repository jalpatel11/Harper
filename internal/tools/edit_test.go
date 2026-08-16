package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestEditTool_ReplacesUniqueMatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.go")
	os.WriteFile(path, []byte("func Foo() { return 1 }"), 0o644)

	tool := NewEditTool()
	_, err := tool.Execute(context.Background(), map[string]any{
		"path": path, "old_string": "return 1", "new_string": "return 2",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "func Foo() { return 2 }" {
		t.Fatalf("unexpected content: %q", string(data))
	}
}

func TestEditTool_ErrorsWhenStringNotFound(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.go")
	os.WriteFile(path, []byte("func Foo() {}"), 0o644)

	tool := NewEditTool()
	_, err := tool.Execute(context.Background(), map[string]any{
		"path": path, "old_string": "does not appear", "new_string": "x",
	})
	if err == nil {
		t.Fatalf("expected an error when old_string is not found")
	}
}

func TestEditTool_ErrorsWhenStringNotUnique(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.go")
	os.WriteFile(path, []byte("x := 1\nx := 1\n"), 0o644)

	tool := NewEditTool()
	_, err := tool.Execute(context.Background(), map[string]any{
		"path": path, "old_string": "x := 1", "new_string": "x := 2",
	})
	if err == nil {
		t.Fatalf("expected an error when old_string matches more than once")
	}
}
