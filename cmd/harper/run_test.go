package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"harper/internal/agent"
	"harper/internal/llm"
	"harper/internal/logging"
	"harper/internal/tools"
)

type runStubProvider struct {
	responses []llm.Response
	calls     int
}

func (p *runStubProvider) Complete(ctx context.Context, messages []llm.Message, toolDefs []llm.ToolDef) (llm.Response, error) {
	resp := p.responses[p.calls]
	p.calls++
	return resp, nil
}

func TestRunOnce_ReturnsFinalAnswerAndLogsSteps(t *testing.T) {
	provider := &runStubProvider{
		responses: []llm.Response{
			{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
				{ID: "call_0", Name: "Read", Args: map[string]any{"path": "f.txt"}},
			}}},
			{
				Message: llm.Message{
					Role:    llm.RoleAssistant,
					Content: "task complete",
					Usage:   &llm.Usage{PromptTokens: 120, CompletionTokens: 8},
				},
			},
		},
	}
	loop := agent.NewLoop(provider, []tools.Tool{stubbedTool{name: "Read", result: "file contents"}}, "you are harper")

	var logBuf bytes.Buffer
	logger := logging.NewJSONLLogger(&logBuf)

	answer, err := RunOnce(context.Background(), loop, "do the thing", 10, logger)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if answer != "task complete" {
		t.Fatalf("unexpected answer: %q", answer)
	}

	lines := strings.Split(strings.TrimSpace(logBuf.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 logged steps (tool call, tool result, final answer), got %d: %q", len(lines), logBuf.String())
	}

	var toolCallEntry logging.TurnLog
	if err := json.Unmarshal([]byte(lines[0]), &toolCallEntry); err != nil {
		t.Fatalf("unmarshal first line: %v", err)
	}
	if toolCallEntry.Tool != "Read" || toolCallEntry.Input["path"] != "f.txt" {
		t.Fatalf("expected the tool call's Input to be logged, got: %+v", toolCallEntry)
	}

	var finalEntry logging.TurnLog
	if err := json.Unmarshal([]byte(lines[2]), &finalEntry); err != nil {
		t.Fatalf("unmarshal third line: %v", err)
	}
	if finalEntry.PromptTokens != 120 || finalEntry.CompletionTokens != 8 {
		t.Fatalf("expected token usage to be logged on the final step, got: %+v", finalEntry)
	}
}

func TestRunOnce_ReturnsErrorOnMaxTurnsExceeded(t *testing.T) {
	provider := &runStubProvider{
		responses: []llm.Response{
			{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
				{ID: "call_0", Name: "nonexistent", Args: map[string]any{}},
			}}},
		},
	}
	loop := agent.NewLoop(provider, nil, "you are harper")
	var logBuf bytes.Buffer
	logger := logging.NewJSONLLogger(&logBuf)

	_, err := RunOnce(context.Background(), loop, "loop forever", 1, logger)
	if err == nil {
		t.Fatalf("expected an error when max turns is exceeded")
	}
}
