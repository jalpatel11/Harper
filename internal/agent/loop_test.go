package agent

import (
	"context"
	"testing"

	"harper/internal/llm"
	"harper/internal/tools"
)

type scriptedProvider struct {
	responses []llm.Response
	calls     int
}

func (p *scriptedProvider) Complete(ctx context.Context, messages []llm.Message, toolDefs []llm.ToolDef) (llm.Response, error) {
	resp := p.responses[p.calls]
	p.calls++
	return resp, nil
}

type stubTool struct {
	name   string
	result string
}

func (t *stubTool) Name() string                 { return t.name }
func (t *stubTool) Description() string          { return "stub" }
func (t *stubTool) InputSchema() map[string]any   { return map[string]any{"type": "object"} }
func (t *stubTool) Execute(ctx context.Context, input map[string]any) (string, error) {
	return t.result, nil
}

func TestLoop_Run_ExecutesToolThenReturnsFinalText(t *testing.T) {
	provider := &scriptedProvider{
		responses: []llm.Response{
			{
				Message: llm.Message{
					Role: llm.RoleAssistant,
					ToolCalls: []llm.ToolCall{
						{ID: "call_0", Name: "Read", Args: map[string]any{"path": "f.txt"}},
					},
				},
			},
			{
				Message: llm.Message{Role: llm.RoleAssistant, Content: "the file says hi"},
			},
		},
	}
	toolset := []tools.Tool{&stubTool{name: "Read", result: "hi"}}

	loop := NewLoop(provider, toolset, "you are a test agent")
	var stepped []llm.Message
	history, err := loop.Run(context.Background(), []llm.Message{{Role: llm.RoleUser, Content: "read f.txt"}}, 10, func(m llm.Message) {
		stepped = append(stepped, m)
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if provider.calls != 2 {
		t.Fatalf("expected 2 Complete calls, got %d", provider.calls)
	}
	if len(stepped) != 3 {
		t.Fatalf("expected onStep to fire 3 times (assistant tool-call, tool result, final assistant), got %d", len(stepped))
	}

	last := history[len(history)-1]
	if last.Role != llm.RoleAssistant || last.Content != "the file says hi" {
		t.Fatalf("unexpected final message: %+v", last)
	}

	foundToolResult := false
	for _, m := range history {
		if m.Role == llm.RoleTool && m.ToolCallID == "call_0" && m.Content == "hi" {
			foundToolResult = true
		}
	}
	if !foundToolResult {
		t.Fatalf("expected a tool-result message with content %q linked to call_0", "hi")
	}
}

func TestLoop_Run_ErrorsWhenMaxTurnsExceeded(t *testing.T) {
	provider := &scriptedProvider{
		responses: []llm.Response{
			{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
				{ID: "call_0", Name: "Read", Args: map[string]any{"path": "f.txt"}},
			}}},
			{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
				{ID: "call_1", Name: "Read", Args: map[string]any{"path": "f.txt"}},
			}}},
		},
	}
	toolset := []tools.Tool{&stubTool{name: "Read", result: "hi"}}

	loop := NewLoop(provider, toolset, "you are a test agent")
	_, err := loop.Run(context.Background(), []llm.Message{{Role: llm.RoleUser, Content: "loop forever"}}, 1, nil)
	if err == nil {
		t.Fatalf("expected an error when max turns is exceeded")
	}
}
