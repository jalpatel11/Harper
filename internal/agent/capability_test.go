package agent

import (
	"context"
	"testing"

	"harper/internal/llm"
)

func TestCheckToolCallCapability_PassesWhenModelCallsTheProbeTool(t *testing.T) {
	provider := &scriptedProvider{
		responses: []llm.Response{
			{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
				{ID: "call_0", Name: "harper_probe", Args: map[string]any{}},
			}}},
		},
	}
	if err := CheckToolCallCapability(context.Background(), provider, "brain"); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestCheckToolCallCapability_FailsWhenModelIgnoresTheTool(t *testing.T) {
	provider := &scriptedProvider{
		responses: []llm.Response{
			{Message: llm.Message{Role: llm.RoleAssistant, Content: "I don't understand tools"}},
		},
	}
	err := CheckToolCallCapability(context.Background(), provider, "subtask")
	if err == nil {
		t.Fatalf("expected an error when the model doesn't produce a tool call")
	}
}
