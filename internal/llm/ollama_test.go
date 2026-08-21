package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOllamaProvider_Complete_TextResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body["model"] != "qwen3-coder:30b" {
			t.Fatalf("unexpected model: %v", body["model"])
		}
		opts, ok := body["options"].(map[string]any)
		if !ok || opts["num_ctx"] != float64(16384) {
			t.Fatalf("expected num_ctx=16384 in options, got: %v", body["options"])
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"message": map[string]any{
				"role":    "assistant",
				"content": "hello from ollama",
			},
			"done": true,
		})
	}))
	defer srv.Close()

	p := NewOllamaProvider(srv.URL, "qwen3-coder:30b", 16384, "")
	resp, err := p.Complete(context.Background(), []Message{
		{Role: RoleUser, Content: "hi"},
	}, nil)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Message.Content != "hello from ollama" {
		t.Fatalf("unexpected content: %q", resp.Message.Content)
	}
	if len(resp.Message.ToolCalls) != 0 {
		t.Fatalf("expected no tool calls, got %v", resp.Message.ToolCalls)
	}
}

func TestOllamaProvider_Complete_ToolCallResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"message": map[string]any{
				"role":    "assistant",
				"content": "",
				"tool_calls": []map[string]any{
					{
						"function": map[string]any{
							"name":      "read",
							"arguments": map[string]any{"path": "foo.txt"},
						},
					},
				},
			},
			"done": true,
		})
	}))
	defer srv.Close()

	p := NewOllamaProvider(srv.URL, "qwen3-coder:30b", 16384, "")
	resp, err := p.Complete(context.Background(), []Message{{Role: RoleUser, Content: "read foo.txt"}}, []ToolDef{
		{Name: "read", Description: "Read a file", Parameters: map[string]any{"type": "object"}},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if len(resp.Message.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.Message.ToolCalls))
	}
	tc := resp.Message.ToolCalls[0]
	if tc.Name != "read" {
		t.Fatalf("unexpected tool name: %q", tc.Name)
	}
	if tc.Args["path"] != "foo.txt" {
		t.Fatalf("unexpected args: %v", tc.Args)
	}
	if tc.ID == "" {
		t.Fatalf("expected a synthesized non-empty ID")
	}
}

func TestOllamaProvider_Complete_RoundTripsPriorToolCallInHistory(t *testing.T) {
	var capturedRequest map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&capturedRequest)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"message": map[string]any{"role": "assistant", "content": "ok, done"},
			"done":    true,
		})
	}))
	defer srv.Close()

	p := NewOllamaProvider(srv.URL, "qwen3-coder:30b", 16384, "")

	// Simulate what Loop.Run passes on a second turn: a history where an
	// earlier assistant message already called a tool, followed by that
	// tool's result. This single Complete() call's outgoing request must
	// still carry the assistant's tool call, or the model loses all memory
	// of having made it.
	history := []Message{
		{Role: RoleUser, Content: "read foo.txt"},
		{Role: RoleAssistant, ToolCalls: []ToolCall{
			{ID: "call_0", Name: "read", Args: map[string]any{"path": "foo.txt"}},
		}},
		{Role: RoleTool, Content: "file contents", ToolCallID: "call_0"},
	}

	_, err := p.Complete(context.Background(), history, []ToolDef{
		{Name: "read", Description: "Read a file", Parameters: map[string]any{"type": "object"}},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}

	msgs, ok := capturedRequest["messages"].([]any)
	if !ok || len(msgs) != 3 {
		t.Fatalf("expected 3 messages in the outgoing request, got: %v", capturedRequest["messages"])
	}

	assistantMsg, ok := msgs[1].(map[string]any)
	if !ok {
		t.Fatalf("expected message[1] to be an object, got: %v", msgs[1])
	}
	toolCalls, ok := assistantMsg["tool_calls"].([]any)
	if !ok || len(toolCalls) != 1 {
		t.Fatalf("expected the assistant's prior tool call to round-trip in the outgoing request, got: %v", assistantMsg["tool_calls"])
	}
}

func TestOllamaProvider_Complete_SendsThinkFieldWhenEffortSet(t *testing.T) {
	var capturedRequest map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&capturedRequest)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"message": map[string]any{"role": "assistant", "content": "ok"},
			"done":    true,
		})
	}))
	defer srv.Close()

	p := NewOllamaProvider(srv.URL, "gpt-oss:20b", 16384, "high")
	_, err := p.Complete(context.Background(), []Message{{Role: RoleUser, Content: "hi"}}, nil)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}

	if capturedRequest["think"] != "high" {
		t.Fatalf("expected think=%q in the outgoing request, got: %v", "high", capturedRequest["think"])
	}
}

func TestOllamaProvider_Complete_OmitsThinkFieldWhenEffortUnset(t *testing.T) {
	var capturedRequest map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&capturedRequest)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"message": map[string]any{"role": "assistant", "content": "ok"},
			"done":    true,
		})
	}))
	defer srv.Close()

	p := NewOllamaProvider(srv.URL, "qwen3-coder:30b", 16384, "")
	_, err := p.Complete(context.Background(), []Message{{Role: RoleUser, Content: "hi"}}, nil)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}

	if _, present := capturedRequest["think"]; present {
		t.Fatalf("expected no think field in the outgoing request, got: %v", capturedRequest["think"])
	}
}

func TestNewOllamaProvider_SetsClientTimeout(t *testing.T) {
	p := NewOllamaProvider("http://localhost:11434", "model", 16384, "")
	if p.client.Timeout <= 0 {
		t.Fatalf("expected a non-zero HTTP client timeout by default, got %v", p.client.Timeout)
	}
}

func TestOllamaProvider_Complete_TimesOutOnSlowServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"message": map[string]any{"role": "assistant", "content": "too slow"},
			"done":    true,
		})
	}))
	defer srv.Close()

	// Constructed directly (not via NewOllamaProvider) so the test can use a
	// tiny timeout instead of waiting out the real default.
	p := &OllamaProvider{baseURL: srv.URL, model: "x", numCtx: 1024, client: &http.Client{Timeout: 10 * time.Millisecond}}
	_, err := p.Complete(context.Background(), []Message{{Role: RoleUser, Content: "hi"}}, nil)
	if err == nil {
		t.Fatalf("expected a timeout error from the slow server")
	}
}

func TestListOllamaModels_ReturnsNames(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"models": []map[string]any{
				{"name": "qwen3-coder:30b"},
				{"name": "muse-glimmer:latest"},
			},
		})
	}))
	defer srv.Close()

	names, err := ListOllamaModels(srv.URL)
	if err != nil {
		t.Fatalf("ListOllamaModels: %v", err)
	}
	if len(names) != 2 || names[0] != "qwen3-coder:30b" || names[1] != "muse-glimmer:latest" {
		t.Fatalf("unexpected names: %v", names)
	}
}

func TestListOllamaModels_NonOKStatusErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := ListOllamaModels(srv.URL)
	if err == nil {
		t.Fatalf("expected an error for a non-200 response")
	}
}
