package agent

import (
	"context"
	"testing"

	"harper/internal/llm"
	"harper/internal/tools"
)

func TestDelegateTool_RunsSubtaskLoopAndReturnsFinalAnswer(t *testing.T) {
	subtaskProvider := &scriptedProvider{
		responses: []llm.Response{
			{Message: llm.Message{Role: llm.RoleAssistant, Content: "subtask done: found 3 matches"}},
		},
	}

	delegate := NewDelegateTool(subtaskProvider, []tools.Tool{}, "you are a subtask agent", 5)

	out, err := delegate.Execute(context.Background(), map[string]any{"task": "search for TODOs"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out != "subtask done: found 3 matches" {
		t.Fatalf("unexpected delegate result: %q", out)
	}
	if subtaskProvider.calls != 1 {
		t.Fatalf("expected exactly 1 Complete call on the subtask provider, got %d", subtaskProvider.calls)
	}
}

func TestDelegateTool_MissingTaskArgumentErrors(t *testing.T) {
	delegate := NewDelegateTool(&scriptedProvider{}, []tools.Tool{}, "you are a subtask agent", 5)
	_, err := delegate.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Fatalf("expected an error when \"task\" is missing")
	}
}
