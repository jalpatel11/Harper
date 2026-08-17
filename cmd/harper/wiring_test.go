package main

import (
	"context"
	"testing"

	"harper/internal/executor"
	"harper/internal/tools"
)

func TestBuildCoreTools_ReturnsAllSixInOrder(t *testing.T) {
	exec := executor.NewLocalExecutor(t.TempDir())
	got := buildCoreTools(exec)

	want := []string{"Read", "Write", "Edit", "Grep", "Glob", "Bash"}
	if len(got) != len(want) {
		t.Fatalf("expected %d tools, got %d", len(want), len(got))
	}
	for i, name := range want {
		if got[i].Name() != name {
			t.Fatalf("tool %d: expected %q, got %q", i, name, got[i].Name())
		}
	}
}

type namedStubTool struct{ name string }

func (t *namedStubTool) Name() string               { return t.name }
func (t *namedStubTool) Description() string        { return "stub" }
func (t *namedStubTool) InputSchema() map[string]any { return map[string]any{"type": "object"} }
func (t *namedStubTool) Execute(ctx context.Context, input map[string]any) (string, error) {
	return "", nil
}

func TestBuildBrainTools_AppendsMCPAndDelegate(t *testing.T) {
	exec := executor.NewLocalExecutor(t.TempDir())
	core := buildCoreTools(exec)
	mcp := []tools.Tool{&namedStubTool{name: "mcp_fs_list"}}
	delegate := &namedStubTool{name: "Delegate"}

	got := buildBrainTools(core, mcp, delegate)
	if len(got) != len(core)+len(mcp)+1 {
		t.Fatalf("expected %d tools, got %d", len(core)+len(mcp)+1, len(got))
	}
	if got[len(got)-1].Name() != "Delegate" {
		t.Fatalf("expected Delegate last, got %q", got[len(got)-1].Name())
	}
}
