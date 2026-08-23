package agent

import (
	"context"
	"sync"
	"testing"

	"harper/internal/llm"
	"harper/internal/tools"
)

// constantProvider always returns the same final answer with no tool
// calls, so Loop.Run finishes after a single Complete call. Unlike
// scriptedProvider (loop_test.go), it holds no mutable per-call state, so
// it's safe for two DelegateTool.Execute calls to share it concurrently —
// exactly how Loop.Run's tool-call fan-out uses a single shared
// DelegateTool/subtaskLoop in production (see main.go's buildBrainLoop).
type constantProvider struct {
	message llm.Message
}

func (p *constantProvider) Complete(ctx context.Context, messages []llm.Message, toolDefs []llm.ToolDef) (llm.Response, error) {
	return llm.Response{Message: p.message}, nil
}

// TestDelegateTool_Execute_ConcurrentCallsRouteStepsToTheirOwnToolCallID
// covers two Delegate calls sharing one DelegateTool (the real fan-out
// shape from Loop.Run) running concurrently, each carrying its own
// reporter-context toolCallID — the reporter closure in Execute captures
// callID from a local variable per call, so steps from one call must never
// be attributed to the other's ID. Run with -race to also confirm the
// shared subtaskLoop tolerates concurrent Run calls.
func TestDelegateTool_Execute_ConcurrentCallsRouteStepsToTheirOwnToolCallID(t *testing.T) {
	provider := &constantProvider{message: llm.Message{Role: llm.RoleAssistant, Content: "done"}}
	delegate := NewDelegateTool(provider, nil, "you are a subtask agent", 5)

	var mu sync.Mutex
	reported := map[string]int{}
	reporter := SubtaskReporter(func(toolCallID string, m llm.Message) {
		mu.Lock()
		defer mu.Unlock()
		reported[toolCallID]++
	})

	run := func(callID, task string) {
		ctx := WithToolCallID(WithSubtaskReporter(context.Background(), reporter), callID)
		if _, err := delegate.Execute(ctx, map[string]any{"task": task}); err != nil {
			t.Errorf("Execute(%s): %v", callID, err)
		}
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); run("call_a", "task a") }()
	go func() { defer wg.Done(); run("call_b", "task b") }()
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if reported["call_a"] == 0 || reported["call_b"] == 0 {
		t.Fatalf("expected steps reported under both call IDs with no cross-talk, got: %+v", reported)
	}
}

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
