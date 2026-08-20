package agent

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

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

func (t *stubTool) Name() string                { return t.name }
func (t *stubTool) Description() string         { return "stub" }
func (t *stubTool) InputSchema() map[string]any { return map[string]any{"type": "object"} }
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

type slowTool struct {
	name   string
	result string
	delay  time.Duration
}

func (t *slowTool) Name() string                { return t.name }
func (t *slowTool) Description() string         { return "stub" }
func (t *slowTool) InputSchema() map[string]any { return map[string]any{"type": "object"} }
func (t *slowTool) Execute(ctx context.Context, input map[string]any) (string, error) {
	time.Sleep(t.delay)
	return t.result, nil
}

func TestLoop_Run_ExecutesMultipleToolCallsInOneTurnConcurrently(t *testing.T) {
	const delay = 100 * time.Millisecond
	provider := &scriptedProvider{
		responses: []llm.Response{
			{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
				{ID: "call_0", Name: "SlowA", Args: nil},
				{ID: "call_1", Name: "SlowB", Args: nil},
			}}},
			{Message: llm.Message{Role: llm.RoleAssistant, Content: "both done"}},
		},
	}
	toolset := []tools.Tool{
		&slowTool{name: "SlowA", result: "a done", delay: delay},
		&slowTool{name: "SlowB", result: "b done", delay: delay},
	}

	loop := NewLoop(provider, toolset, "you are a test agent")
	start := time.Now()
	history, err := loop.Run(context.Background(), []llm.Message{{Role: llm.RoleUser, Content: "run both"}}, 10, nil)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// If the two tool calls ran sequentially, this would take ~2*delay.
	// Running concurrently, it should take much closer to a single delay.
	if elapsed >= 2*delay {
		t.Fatalf("expected concurrent execution (~%v), took %v", delay, elapsed)
	}

	results := map[string]string{}
	for _, m := range history {
		if m.Role == llm.RoleTool {
			results[m.ToolCallID] = m.Content
		}
	}
	if results["call_0"] != "a done" || results["call_1"] != "b done" {
		t.Fatalf("expected each tool result correctly matched to its call ID, got: %v", results)
	}

	// Results must still land in history in the original call order, even
	// though execution was concurrent — callers (loggers, REPL output)
	// depend on deterministic ordering.
	var toolOrder []string
	for _, m := range history {
		if m.Role == llm.RoleTool {
			toolOrder = append(toolOrder, m.ToolCallID)
		}
	}
	if len(toolOrder) != 2 || toolOrder[0] != "call_0" || toolOrder[1] != "call_1" {
		t.Fatalf("expected tool results in original call order [call_0 call_1], got %v", toolOrder)
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

func TestLoop_Run_DeniesToolCallWhenCheckerReturnsFalse(t *testing.T) {
	provider := &scriptedProvider{
		responses: []llm.Response{
			{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
				{ID: "call_0", Name: "Read", Args: map[string]any{"path": "f.txt"}},
			}}},
			{Message: llm.Message{Role: llm.RoleAssistant, Content: "got denied"}},
		},
	}
	toolset := []tools.Tool{&stubTool{name: "Read", result: "should not see this"}}

	loop := NewLoop(provider, toolset, "you are a test agent")
	loop.SetPermissionChecker(func(ctx context.Context, toolName string, input map[string]any) (bool, error) {
		return false, nil
	})

	history, err := loop.Run(context.Background(), []llm.Message{{Role: llm.RoleUser, Content: "read f.txt"}}, 10, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	var toolResult string
	for _, m := range history {
		if m.Role == llm.RoleTool && m.ToolCallID == "call_0" {
			toolResult = m.Content
		}
	}
	if !strings.Contains(toolResult, "permission denied") {
		t.Fatalf("expected a permission-denied tool result, got: %q", toolResult)
	}
	if strings.Contains(toolResult, "should not see this") {
		t.Fatalf("tool must not have executed when the checker denied it")
	}
}

func TestLoop_Run_AllowsToolCallWhenCheckerReturnsTrue(t *testing.T) {
	provider := &scriptedProvider{
		responses: []llm.Response{
			{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
				{ID: "call_0", Name: "Read", Args: map[string]any{"path": "f.txt"}},
			}}},
			{Message: llm.Message{Role: llm.RoleAssistant, Content: "done"}},
		},
	}
	toolset := []tools.Tool{&stubTool{name: "Read", result: "file contents"}}

	loop := NewLoop(provider, toolset, "you are a test agent")
	var checkerCalls int32
	loop.SetPermissionChecker(func(ctx context.Context, toolName string, input map[string]any) (bool, error) {
		atomic.AddInt32(&checkerCalls, 1)
		if toolName != "Read" {
			t.Fatalf("unexpected tool name passed to checker: %q", toolName)
		}
		return true, nil
	})

	history, err := loop.Run(context.Background(), []llm.Message{{Role: llm.RoleUser, Content: "read f.txt"}}, 10, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if atomic.LoadInt32(&checkerCalls) != 1 {
		t.Fatalf("expected the checker to be called exactly once, got %d", checkerCalls)
	}

	var toolResult string
	for _, m := range history {
		if m.Role == llm.RoleTool && m.ToolCallID == "call_0" {
			toolResult = m.Content
		}
	}
	if toolResult != "file contents" {
		t.Fatalf("expected the tool to actually execute when allowed, got: %q", toolResult)
	}
}

func TestLoop_Run_NoCheckerSetBehavesAsAlwaysAllow(t *testing.T) {
	// Existing behavior (no checker ever set) must be unchanged.
	provider := &scriptedProvider{
		responses: []llm.Response{
			{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
				{ID: "call_0", Name: "Read", Args: map[string]any{"path": "f.txt"}},
			}}},
			{Message: llm.Message{Role: llm.RoleAssistant, Content: "done"}},
		},
	}
	toolset := []tools.Tool{&stubTool{name: "Read", result: "file contents"}}
	loop := NewLoop(provider, toolset, "you are a test agent")

	history, err := loop.Run(context.Background(), []llm.Message{{Role: llm.RoleUser, Content: "read f.txt"}}, 10, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	var toolResult string
	for _, m := range history {
		if m.Role == llm.RoleTool && m.ToolCallID == "call_0" {
			toolResult = m.Content
		}
	}
	if toolResult != "file contents" {
		t.Fatalf("expected the tool to execute normally with no checker set, got: %q", toolResult)
	}
}

func TestLoop_Run_CheckerErrorReturnedAsToolContent(t *testing.T) {
	provider := &scriptedProvider{
		responses: []llm.Response{
			{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
				{ID: "call_0", Name: "Read", Args: map[string]any{"path": "f.txt"}},
			}}},
			{Message: llm.Message{Role: llm.RoleAssistant, Content: "done"}},
		},
	}
	toolset := []tools.Tool{&stubTool{name: "Read", result: "file contents"}}
	loop := NewLoop(provider, toolset, "you are a test agent")
	loop.SetPermissionChecker(func(ctx context.Context, toolName string, input map[string]any) (bool, error) {
		return false, fmt.Errorf("permission backend unreachable")
	})

	history, err := loop.Run(context.Background(), []llm.Message{{Role: llm.RoleUser, Content: "read f.txt"}}, 10, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	var toolResult string
	for _, m := range history {
		if m.Role == llm.RoleTool && m.ToolCallID == "call_0" {
			toolResult = m.Content
		}
	}
	if !strings.Contains(toolResult, "permission backend unreachable") {
		t.Fatalf("expected the checker's error surfaced as tool content, got: %q", toolResult)
	}
}
