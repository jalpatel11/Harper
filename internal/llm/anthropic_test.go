package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewAnthropicProvider_SetsFieldsAndDefaults(t *testing.T) {
	p := NewAnthropicProvider("sk-key", "claude-haiku-4-5-20251001", "")
	if p.apiKey != "sk-key" || p.model != "claude-haiku-4-5-20251001" {
		t.Fatalf("unexpected provider fields: %+v", p)
	}
	if p.baseURL != anthropicBaseURL {
		t.Fatalf("expected the default Anthropic base URL, got %q", p.baseURL)
	}
	if p.client.Timeout <= 0 {
		t.Fatalf("expected a non-zero HTTP client timeout by default")
	}
}

func TestAnthropicProvider_Complete_TextResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "sk-test-key" {
			t.Fatalf("unexpected x-api-key header: %q", r.Header.Get("x-api-key"))
		}
		if r.Header.Get("anthropic-version") == "" {
			t.Fatalf("expected an anthropic-version header")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body["model"] != "claude-haiku-4-5-20251001" {
			t.Fatalf("unexpected model: %v", body["model"])
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"role": "assistant",
			"content": []map[string]any{
				{"type": "text", "text": "hello from claude"},
			},
			"usage": map[string]any{"input_tokens": 10, "output_tokens": 5},
		})
	}))
	defer srv.Close()

	p := &AnthropicProvider{baseURL: srv.URL, apiKey: "sk-test-key", model: "claude-haiku-4-5-20251001", client: srv.Client()}
	resp, err := p.Complete(context.Background(), []Message{{Role: RoleUser, Content: "hi"}}, nil)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Message.Content != "hello from claude" {
		t.Fatalf("unexpected content: %q", resp.Message.Content)
	}
	if resp.Message.Usage == nil || resp.Message.Usage.PromptTokens != 10 || resp.Message.Usage.CompletionTokens != 5 {
		t.Fatalf("unexpected usage: %+v", resp.Message.Usage)
	}
}

func TestAnthropicProvider_Complete_ExtractsSystemMessageToTopLevelField(t *testing.T) {
	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&captured)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"role":    "assistant",
			"content": []map[string]any{{"type": "text", "text": "ok"}},
		})
	}))
	defer srv.Close()

	p := &AnthropicProvider{baseURL: srv.URL, apiKey: "k", model: "claude-haiku-4-5-20251001", client: srv.Client()}
	_, err := p.Complete(context.Background(), []Message{
		{Role: RoleSystem, Content: "you are a helpful agent"},
		{Role: RoleUser, Content: "hi"},
	}, nil)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if captured["system"] != "you are a helpful agent" {
		t.Fatalf("expected the system message extracted to the top-level field, got: %v", captured["system"])
	}
	msgs, ok := captured["messages"].([]any)
	if !ok || len(msgs) != 1 {
		t.Fatalf("expected exactly 1 message (system removed from the messages array), got: %v", captured["messages"])
	}
}

func TestAnthropicProvider_Complete_ToolCallResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"role": "assistant",
			"content": []map[string]any{
				{"type": "tool_use", "id": "toolu_01abc", "name": "read", "input": map[string]any{"path": "foo.txt"}},
			},
		})
	}))
	defer srv.Close()

	p := &AnthropicProvider{baseURL: srv.URL, apiKey: "k", model: "claude-haiku-4-5-20251001", client: srv.Client()}
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
	if tc.ID != "toolu_01abc" || tc.Name != "read" || tc.Args["path"] != "foo.txt" {
		t.Fatalf("unexpected tool call: %+v", tc)
	}
}

func TestAnthropicProvider_Complete_RoundTripsToolCallAndResultInHistory(t *testing.T) {
	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&captured)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"role":    "assistant",
			"content": []map[string]any{{"type": "text", "text": "done"}},
		})
	}))
	defer srv.Close()

	p := &AnthropicProvider{baseURL: srv.URL, apiKey: "k", model: "claude-haiku-4-5-20251001", client: srv.Client()}
	history := []Message{
		{Role: RoleUser, Content: "read foo.txt"},
		{Role: RoleAssistant, ToolCalls: []ToolCall{
			{ID: "toolu_01abc", Name: "read", Args: map[string]any{"path": "foo.txt"}},
		}},
		{Role: RoleTool, Content: "file contents", ToolCallID: "toolu_01abc"},
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
	if assistantMsg["role"] != "assistant" {
		t.Fatalf("expected message[1] role assistant, got: %v", assistantMsg["role"])
	}
	blocks := assistantMsg["content"].([]any)
	var foundToolUse bool
	for _, b := range blocks {
		block := b.(map[string]any)
		if block["type"] == "tool_use" && block["id"] == "toolu_01abc" && block["name"] == "read" {
			foundToolUse = true
		}
	}
	if !foundToolUse {
		t.Fatalf("expected a tool_use block in the assistant message, got: %v", blocks)
	}

	toolResultMsg := msgs[2].(map[string]any)
	if toolResultMsg["role"] != "user" {
		t.Fatalf("expected the tool result mapped to a user message, got role: %v", toolResultMsg["role"])
	}
	resultBlocks := toolResultMsg["content"].([]any)
	resultBlock := resultBlocks[0].(map[string]any)
	if resultBlock["type"] != "tool_result" || resultBlock["tool_use_id"] != "toolu_01abc" || resultBlock["content"] != "file contents" {
		t.Fatalf("unexpected tool_result block: %v", resultBlock)
	}
}

func TestAnthropicProvider_Complete_MapsToolDefsToInputSchema(t *testing.T) {
	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&captured)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"role":    "assistant",
			"content": []map[string]any{{"type": "text", "text": "ok"}},
		})
	}))
	defer srv.Close()

	p := &AnthropicProvider{baseURL: srv.URL, apiKey: "k", model: "m", client: srv.Client()}
	_, err := p.Complete(context.Background(), []Message{{Role: RoleUser, Content: "hi"}}, []ToolDef{
		{Name: "read", Description: "Read a file", Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{"path": map[string]any{"type": "string"}},
		}},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	toolsField, ok := captured["tools"].([]any)
	if !ok || len(toolsField) != 1 {
		t.Fatalf("expected 1 tool in the outgoing request, got: %v", captured["tools"])
	}
	tool := toolsField[0].(map[string]any)
	if tool["name"] != "read" || tool["description"] != "Read a file" {
		t.Fatalf("unexpected tool def: %v", tool)
	}
	schema, ok := tool["input_schema"].(map[string]any)
	if !ok || schema["type"] != "object" {
		t.Fatalf("expected input_schema mapped from Parameters, got: %v", tool["input_schema"])
	}
}

func TestAnthropicProvider_Complete_NonOKStatusIncludesBodyInError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":{"type":"invalid_request_error","message":"max_tokens too large"}}`))
	}))
	defer srv.Close()

	p := &AnthropicProvider{baseURL: srv.URL, apiKey: "k", model: "m", client: srv.Client()}
	_, err := p.Complete(context.Background(), []Message{{Role: RoleUser, Content: "hi"}}, nil)
	if err == nil {
		t.Fatalf("expected an error for a non-200 response")
	}
	if !strings.Contains(err.Error(), "max_tokens too large") {
		t.Fatalf("expected the response body's error message included, got: %v", err)
	}
}
