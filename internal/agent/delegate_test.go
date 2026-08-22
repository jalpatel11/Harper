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

func TestDelegateTool_Execute_ReportsSubtaskStepsWhenReporterPresent(t *testing.T) {
	provider := &scriptedProvider{
		responses: []llm.Response{
			{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
				{ID: "sub_call_0", Name: "Read", Args: map[string]any{"path": "f.txt"}},
			}}},
			{Message: llm.Message{Role: llm.RoleAssistant, Content: "done"}},
		},
	}
	subtaskTools := []tools.Tool{&stubTool{name: "Read", result: "file contents"}}
	delegate := NewDelegateTool(provider, subtaskTools, "you are a subtask agent", 10)

	var reported []llm.Message
	ctx := WithSubtaskReporter(context.Background(), func(toolCallID string, m llm.Message) {
		if toolCallID != "call_outer" {
			t.Fatalf("unexpected tool call ID reported: %q", toolCallID)
		}
		reported = append(reported, m)
	})
	ctx = WithToolCallID(ctx, "call_outer")

	_, err := delegate.Execute(ctx, map[string]any{"task": "read f.txt"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(reported) == 0 {
		t.Fatalf("expected subtask steps reported when a reporter is present in context")
	}
}

func TestDelegateTool_Execute_NoReporterIsUnaffected(t *testing.T) {
	// The existing, no-reporter behavior (run mode, plain REPL) must be
	// completely unchanged — this is the "additive, nothing lost" check.
	provider := &scriptedProvider{
		responses: []llm.Response{
			{Message: llm.Message{Role: llm.RoleAssistant, Content: "done"}},
		},
	}
	delegate := NewDelegateTool(provider, nil, "you are a subtask agent", 10)

	result, err := delegate.Execute(context.Background(), map[string]any{"task": "say hi"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result != "done" {
		t.Fatalf("unexpected result: %q", result)
	}
}
