package mcp

import (
	"context"
	"testing"

	"harper/internal/config"
	"harper/internal/llm"

	"github.com/google/jsonschema-go/jsonschema"
)

type fakeSession struct {
	tools      []llm.ToolDef
	lastCall   string
	lastArgs   map[string]any
	callResult string
}

func (f *fakeSession) ListTools(ctx context.Context) ([]llm.ToolDef, error) {
	return f.tools, nil
}

func (f *fakeSession) CallTool(ctx context.Context, name string, args map[string]any) (string, error) {
	f.lastCall = name
	f.lastArgs = args
	return f.callResult, nil
}

func TestMergeTools_NamespacesToolsPerServer(t *testing.T) {
	fake := &fakeSession{
		tools:      []llm.ToolDef{{Name: "list_files", Description: "list files"}},
		callResult: "a.txt\nb.txt",
	}
	connect := func(ctx context.Context, cfg config.MCPServerConfig) (session, error) {
		return fake, nil
	}

	merged, err := MergeTools(context.Background(), []config.MCPServerConfig{
		{Name: "fs", Command: "npx"},
	}, connect)
	if err != nil {
		t.Fatalf("MergeTools: %v", err)
	}
	if len(merged) != 1 {
		t.Fatalf("expected 1 merged tool, got %d", len(merged))
	}
	if merged[0].Name() != "mcp_fs_list_files" {
		t.Fatalf("expected namespaced tool name, got %q", merged[0].Name())
	}

	out, err := merged[0].Execute(context.Background(), map[string]any{"dir": "."})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out != "a.txt\nb.txt" {
		t.Fatalf("unexpected output: %q", out)
	}
	if fake.lastCall != "list_files" {
		t.Fatalf("expected the underlying (unnamespaced) tool name to be called, got %q", fake.lastCall)
	}
}

func TestMergeTools_MultipleServersDoNotCollide(t *testing.T) {
	fakeA := &fakeSession{tools: []llm.ToolDef{{Name: "search"}}}
	fakeB := &fakeSession{tools: []llm.ToolDef{{Name: "search"}}}
	connect := func(ctx context.Context, cfg config.MCPServerConfig) (session, error) {
		if cfg.Name == "a" {
			return fakeA, nil
		}
		return fakeB, nil
	}

	merged, err := MergeTools(context.Background(), []config.MCPServerConfig{
		{Name: "a"}, {Name: "b"},
	}, connect)
	if err != nil {
		t.Fatalf("MergeTools: %v", err)
	}
	if len(merged) != 2 {
		t.Fatalf("expected 2 merged tools, got %d", len(merged))
	}
	if merged[0].Name() == merged[1].Name() {
		t.Fatalf("expected distinct namespaced names, got %q twice", merged[0].Name())
	}
}

func TestSchemaToParameters_ConvertsTypedSchemaToMap(t *testing.T) {
	schema := &jsonschema.Schema{
		Type:     "object",
		Required: []string{"path"},
		Properties: map[string]*jsonschema.Schema{
			"path": {Type: "string"},
		},
	}

	got := schemaToParameters(schema)
	if got["type"] != "object" {
		t.Fatalf("expected type=object in converted map, got: %v", got)
	}
	props, ok := got["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected properties to survive conversion, got: %v", got["properties"])
	}
	if _, ok := props["path"]; !ok {
		t.Fatalf("expected a \"path\" property, got: %v", props)
	}
}

func TestSchemaToParameters_NilSchemaReturnsNil(t *testing.T) {
	if got := schemaToParameters(nil); got != nil {
		t.Fatalf("expected nil for a nil schema, got: %v", got)
	}
}
