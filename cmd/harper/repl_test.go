package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"harper/internal/agent"
	"harper/internal/config"
	"harper/internal/llm"
	"harper/internal/session"
	"harper/internal/tools"
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "harper-repl-session-test-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	session.SetSessionsRoot(dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

type replStubProvider struct {
	responses []llm.Response
	calls     int
}

func (p *replStubProvider) Complete(ctx context.Context, messages []llm.Message, toolDefs []llm.ToolDef) (llm.Response, error) {
	resp := p.responses[p.calls]
	p.calls++
	return resp, nil
}

func TestRunREPL_EchoesFinalAnswerAndExitsOnEOF(t *testing.T) {
	provider := &replStubProvider{
		responses: []llm.Response{
			{Message: llm.Message{Role: llm.RoleAssistant, Content: "hi there"}},
		},
	}
	loop := agent.NewLoop(provider, nil, "you are harper")

	in := strings.NewReader("hello\n")
	var out bytes.Buffer

	if err := RunREPL(context.Background(), loop, in, &out, config.Config{}, nil, ""); err != nil {
		t.Fatalf("RunREPL: %v", err)
	}
	if !strings.Contains(out.String(), "hi there") {
		t.Fatalf("expected final answer in output, got: %q", out.String())
	}
}

func TestRunREPL_PrintsToolActivity(t *testing.T) {
	provider := &replStubProvider{
		responses: []llm.Response{
			{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
				{ID: "call_0", Name: "Read", Args: map[string]any{"path": "f.txt"}},
			}}},
			{Message: llm.Message{Role: llm.RoleAssistant, Content: "done"}},
		},
	}
	stub := stubbedTool{name: "Read", result: "file contents"}
	loop := agent.NewLoop(provider, []tools.Tool{stub}, "you are harper")

	in := strings.NewReader("read f.txt\n")
	var out bytes.Buffer

	if err := RunREPL(context.Background(), loop, in, &out, config.Config{}, nil, ""); err != nil {
		t.Fatalf("RunREPL: %v", err)
	}
	if !strings.Contains(out.String(), "file contents") {
		t.Fatalf("expected tool result content in output, got: %q", out.String())
	}
}

func TestRunREPL_PermissionPromptDoesNotCorruptNextLineOfInput(t *testing.T) {
	provider := &replStubProvider{
		responses: []llm.Response{
			{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
				{ID: "call_0", Name: "Bash", Args: map[string]any{"command": "ls"}},
			}}},
			{Message: llm.Message{Role: llm.RoleAssistant, Content: "done"}},
			{Message: llm.Message{Role: llm.RoleAssistant, Content: "second turn answer"}},
		},
	}
	stub := stubbedTool{name: "Bash", result: "file listing"}
	loop := agent.NewLoop(provider, []tools.Tool{stub}, "you are harper")

	// Line 1: the user's first prompt. Line 2: the permission response
	// ("o" for allow-once). Line 3: the user's second prompt. If the
	// permission checker and the main loop are reading from independent
	// scanners over the same input, one of these lines gets lost or
	// misattributed.
	in := strings.NewReader("run ls\no\nsecond prompt\n")
	var out bytes.Buffer

	cfg := config.Config{Permissions: config.PermissionsConfig{Default: "ask"}}
	if err := RunREPL(context.Background(), loop, in, &out, cfg, nil, ""); err != nil {
		t.Fatalf("RunREPL: %v", err)
	}
	if !strings.Contains(out.String(), "second turn answer") {
		t.Fatalf("expected the second prompt to be processed correctly, got: %q", out.String())
	}
}

func TestRunREPL_ModelCommandWithArgSwitchesProviderWithoutPrompting(t *testing.T) {
	var capturedModel string
	ollamaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		capturedModel, _ = body["model"].(string)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"message": map[string]any{"role": "assistant", "content": "from new model"},
			"done":    true,
		})
	}))
	defer ollamaSrv.Close()

	// The original provider has no responses queued — if /model failed to
	// switch and the loop fell through to this one, the test would panic
	// on an out-of-range access rather than silently pass.
	loop := agent.NewLoop(&replStubProvider{}, nil, "you are harper")
	cfg := config.Config{
		Brain:  config.ModelConfig{Provider: "ollama"},
		Ollama: config.OllamaConfig{BaseURL: ollamaSrv.URL, NumCtx: 4096},
	}

	in := strings.NewReader("/model some-new-model\nhello\n")
	var out bytes.Buffer

	if err := RunREPL(context.Background(), loop, in, &out, cfg, nil, ""); err != nil {
		t.Fatalf("RunREPL: %v", err)
	}
	if capturedModel != "some-new-model" {
		t.Fatalf("expected the new provider to receive model %q, got %q", "some-new-model", capturedModel)
	}
	if !strings.Contains(out.String(), "from new model") {
		t.Fatalf("expected the switched provider's response, got: %q", out.String())
	}
}

func TestRunREPL_ModelCommandListsAndPicksOllamaModelByNumber(t *testing.T) {
	var capturedModel string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"models": []map[string]any{{"name": "model-a"}, {"name": "model-b"}},
			})
			return
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		capturedModel, _ = body["model"].(string)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"message": map[string]any{"role": "assistant", "content": "picked"},
			"done":    true,
		})
	}))
	defer srv.Close()

	loop := agent.NewLoop(&replStubProvider{}, nil, "you are harper")
	cfg := config.Config{
		Brain:  config.ModelConfig{Provider: "ollama"},
		Ollama: config.OllamaConfig{BaseURL: srv.URL, NumCtx: 4096},
	}

	in := strings.NewReader("/model\n2\nhello\n")
	var out bytes.Buffer

	if err := RunREPL(context.Background(), loop, in, &out, cfg, nil, ""); err != nil {
		t.Fatalf("RunREPL: %v", err)
	}
	if capturedModel != "model-b" {
		t.Fatalf("expected model-b picked by number, got %q", capturedModel)
	}
	if !strings.Contains(out.String(), "model-a") || !strings.Contains(out.String(), "model-b") {
		t.Fatalf("expected the model list printed, got: %q", out.String())
	}
}

func TestRunREPL_ModelCommandNonOllamaProviderPromptsWithoutListing(t *testing.T) {
	loop := agent.NewLoop(&replStubProvider{}, nil, "you are harper")
	cfg := config.Config{Brain: config.ModelConfig{Provider: "anthropic"}}
	t.Setenv("ANTHROPIC_API_KEY", "sk-test")

	in := strings.NewReader("/model\nclaude-haiku-4-5-20251001\n")
	var out bytes.Buffer

	if err := RunREPL(context.Background(), loop, in, &out, cfg, nil, ""); err != nil {
		t.Fatalf("RunREPL: %v", err)
	}
	if strings.Contains(out.String(), "Available Ollama models") {
		t.Fatalf("expected no ollama listing for a non-ollama provider, got: %q", out.String())
	}
	if !strings.Contains(out.String(), "model set to") {
		t.Fatalf("expected confirmation the model was set, got: %q", out.String())
	}
}

func TestRunREPL_UnknownCommandPrintsMessageInsteadOfErroring(t *testing.T) {
	loop := agent.NewLoop(&replStubProvider{responses: []llm.Response{
		{Message: llm.Message{Role: llm.RoleAssistant, Content: "hi"}},
	}}, nil, "you are harper")

	in := strings.NewReader("/nonsense\nhello\n")
	var out bytes.Buffer

	if err := RunREPL(context.Background(), loop, in, &out, config.Config{}, nil, ""); err != nil {
		t.Fatalf("RunREPL: %v", err)
	}
	if !strings.Contains(out.String(), "unknown command") {
		t.Fatalf("expected an unknown-command message, got: %q", out.String())
	}
	if !strings.Contains(out.String(), "hi") {
		t.Fatalf("expected the REPL to continue working after an unknown command, got: %q", out.String())
	}
}

type stubbedTool struct {
	name   string
	result string
}

func (t stubbedTool) Name() string                { return t.name }
func (t stubbedTool) Description() string         { return "stub" }
func (t stubbedTool) InputSchema() map[string]any { return map[string]any{"type": "object"} }
func (t stubbedTool) Execute(ctx context.Context, input map[string]any) (string, error) {
	return t.result, nil
}
