package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"harper/internal/agent"
	"harper/internal/llm"
	"harper/internal/tools"
)

type replStubProvider struct {
	responses []llm.Response
	calls     int
}

func (p *replStubProvider) Complete(ctx context.Context, messages []llm.Message, toolDefs []llm.ToolDef) (llm.Response, error) {
	resp := p.responses[p.calls]
	p.calls++
	return resp, nil
}

func TestRunREPL_EchoesFinalAnswerAndExitsOnEOF(t *testing.T) {
	provider := &replStubProvider{
		responses: []llm.Response{
			{Message: llm.Message{Role: llm.RoleAssistant, Content: "hi there"}},
		},
	}
	loop := agent.NewLoop(provider, nil, "you are harper")

	in := strings.NewReader("hello\n")
	var out bytes.Buffer

	if err := RunREPL(context.Background(), loop, in, &out); err != nil {
		t.Fatalf("RunREPL: %v", err)
	}
	if !strings.Contains(out.String(), "hi there") {
		t.Fatalf("expected final answer in output, got: %q", out.String())
	}
}

func TestRunREPL_PrintsToolActivity(t *testing.T) {
	provider := &replStubProvider{
		responses: []llm.Response{
			{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
				{ID: "call_0", Name: "Read", Args: map[string]any{"path": "f.txt"}},
			}}},
			{Message: llm.Message{Role: llm.RoleAssistant, Content: "done"}},
		},
	}
	stub := stubbedTool{name: "Read", result: "file contents"}
	loop := agent.NewLoop(provider, []tools.Tool{stub}, "you are harper")

	in := strings.NewReader("read f.txt\n")
	var out bytes.Buffer

	if err := RunREPL(context.Background(), loop, in, &out); err != nil {
		t.Fatalf("RunREPL: %v", err)
	}
	if !strings.Contains(out.String(), "file contents") {
		t.Fatalf("expected tool result content in output, got: %q", out.String())
	}
}

type stubbedTool struct {
	name   string
	result string
}

func (t stubbedTool) Name() string                 { return t.name }
func (t stubbedTool) Description() string          { return "stub" }
func (t stubbedTool) InputSchema() map[string]any   { return map[string]any{"type": "object"} }
func (t stubbedTool) Execute(ctx context.Context, input map[string]any) (string, error) {
	return t.result, nil
}
