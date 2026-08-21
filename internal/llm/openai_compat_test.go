package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewOpenAICompatProvider_SetsFieldsAndDefaults(t *testing.T) {
	p := NewOpenAICompatProvider("http://localhost:1234/v1", "local-model", "")
	if p.baseURL != "http://localhost:1234/v1" || p.model != "local-model" {
		t.Fatalf("unexpected provider fields: %+v", p)
	}
	if p.client.Timeout <= 0 {
		t.Fatalf("expected a non-zero HTTP client timeout by default")
	}
}

func TestOpenAICompatProvider_Complete_TextResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body["model"] != "local-model" {
			t.Fatalf("unexpected model: %v", body["model"])
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": "hello from local model"}},
			},
			"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 5},
		})
	}))
	defer srv.Close()

	p := NewOpenAICompatProvider(srv.URL, "local-model", "")
	resp, err := p.Complete(context.Background(), []Message{{Role: RoleUser, Content: "hi"}}, nil)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Message.Content != "hello from local model" {
		t.Fatalf("unexpected content: %q", resp.Message.Content)
	}
	if resp.Message.Usage == nil || resp.Message.Usage.PromptTokens != 10 || resp.Message.Usage.CompletionTokens != 5 {
		t.Fatalf("unexpected usage: %+v", resp.Message.Usage)
	}
}

func TestOpenAICompatProvider_Complete_ToolCallResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{
					"role": "assistant", "content": "",
					"tool_calls": []map[string]any{
						{
							"id":   "call_abc",
							"type": "function",
							"function": map[string]any{
								"name":      "read",
								"arguments": `{"path":"foo.txt"}`,
							},
						},
					},
				}},
			},
		})
	}))
	defer srv.Close()

	p := NewOpenAICompatProvider(srv.URL, "local-model", "")
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
	if tc.ID != "call_abc" || tc.Name != "read" || tc.Args["path"] != "foo.txt" {
		t.Fatalf("unexpected tool call: %+v", tc)
	}
}

func TestOpenAICompatProvider_Complete_RoundTripsToolCallAndResultWithName(t *testing.T) {
	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&captured)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": "done"}},
			},
		})
	}))
	defer srv.Close()

	p := NewOpenAICompatProvider(srv.URL, "local-model", "")
	history := []Message{
		{Role: RoleUser, Content: "read foo.txt"},
		{Role: RoleAssistant, ToolCalls: []ToolCall{
			{ID: "call_abc", Name: "read", Args: map[string]any{"path": "foo.txt"}},
		}},
		{Role: RoleTool, Content: "file contents", ToolCallID: "call_abc"},
	}
	_, err := p.Complete(context.Background(), history, []ToolDef{
		{Name: "read", Description: "Read a file", Parameters: map[string]any{"type": "object"}},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}

	msgs, ok := captured["messages"].([]any)
	if !ok || len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got: %v", captured["messages"])
	}

	assistantMsg := msgs[1].(map[string]any)
	toolCalls, ok := assistantMsg["tool_calls"].([]any)
	if !ok || len(toolCalls) != 1 {
		t.Fatalf("expected 1 tool call on the assistant message, got: %v", assistantMsg["tool_calls"])
	}
	tc := toolCalls[0].(map[string]any)
	fn := tc["function"].(map[string]any)
	if fn["arguments"] != `{"path":"foo.txt"}` {
		t.Fatalf("expected arguments encoded as a JSON string, got: %v (%T)", fn["arguments"], fn["arguments"])
	}

	toolMsg := msgs[2].(map[string]any)
	if toolMsg["role"] != "tool" || toolMsg["tool_call_id"] != "call_abc" {
		t.Fatalf("unexpected tool message: %v", toolMsg)
	}
	if toolMsg["name"] != "read" {
		t.Fatalf("expected the tool message's name recovered from the original call, got: %v", toolMsg["name"])
	}
}

func TestOpenAICompatProvider_Complete_MapsToolDefsToFunctionSchema(t *testing.T) {
	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&captured)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"role": "assistant", "content": "ok"}}},
		})
	}))
	defer srv.Close()

	p := NewOpenAICompatProvider(srv.URL, "local-model", "")
	_, err := p.Complete(context.Background(), []Message{{Role: RoleUser, Content: "hi"}}, []ToolDef{
		{Name: "read", Description: "Read a file", Parameters: map[string]any{"type": "object"}},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	toolsField, ok := captured["tools"].([]any)
	if !ok || len(toolsField) != 1 {
		t.Fatalf("expected 1 tool in the outgoing request, got: %v", captured["tools"])
	}
	tool := toolsField[0].(map[string]any)
	if tool["type"] != "function" {
		t.Fatalf("expected type=function, got: %v", tool["type"])
	}
	fn := tool["function"].(map[string]any)
	if fn["name"] != "read" || fn["description"] != "Read a file" {
		t.Fatalf("unexpected function def: %v", fn)
	}
}

func TestOpenAICompatProvider_Complete_NonOKStatusIncludesBodyInError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"model not loaded"}`))
	}))
	defer srv.Close()

	p := NewOpenAICompatProvider(srv.URL, "local-model", "")
	_, err := p.Complete(context.Background(), []Message{{Role: RoleUser, Content: "hi"}}, nil)
	if err == nil {
		t.Fatalf("expected an error for a non-200 response")
	}
	if !strings.Contains(err.Error(), "model not loaded") {
		t.Fatalf("expected the response body's error message included, got: %v", err)
	}
}

func TestOpenAICompatProvider_Complete_NoChoicesErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"choices": []map[string]any{}})
	}))
	defer srv.Close()

	p := NewOpenAICompatProvider(srv.URL, "local-model", "")
	_, err := p.Complete(context.Background(), []Message{{Role: RoleUser, Content: "hi"}}, nil)
	if err == nil {
		t.Fatalf("expected an error when the response has no choices")
	}
}
