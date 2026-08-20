package agent

import (
	"context"
	"fmt"

	"harper/internal/llm"
	"harper/internal/tools"
)

type DelegateTool struct {
	subtaskLoop *Loop
	maxTurns    int
}

func NewDelegateTool(subtaskProvider llm.Provider, subtaskTools []tools.Tool, subtaskSystemPrompt string, maxTurns int) *DelegateTool {
	return &DelegateTool{
		subtaskLoop: NewLoop(subtaskProvider, subtaskTools, subtaskSystemPrompt),
		maxTurns:    maxTurns,
	}
}

func (t *DelegateTool) Name() string { return "Delegate" }
func (t *DelegateTool) Description() string {
	return "Hand off a self-contained, tedious, or token-heavy task to the subtask model. " +
		"Use it early for multi-step investigation, tracing, or analysis that would otherwise " +
		"take many of your own turns — don't grind through that kind of work yourself and " +
		"delegate only as a last resort; delegate as soon as you recognize the shape. Returns its final answer."
}
func (t *DelegateTool) InputSchema() map[string]any {
	return map[string]any{
		"type":       "object",
		"required":   []string{"task"},
		"properties": map[string]any{"task": map[string]any{"type": "string"}},
	}
}

func (t *DelegateTool) Execute(ctx context.Context, input map[string]any) (string, error) {
	task, ok := input["task"].(string)
	if !ok || task == "" {
		return "", fmt.Errorf("delegate: missing required \"task\" argument")
	}

	history, err := t.subtaskLoop.Run(ctx, []llm.Message{{Role: llm.RoleUser, Content: task}}, t.maxTurns, nil)
	if err != nil {
		return "", fmt.Errorf("delegate: subtask loop: %w", err)
	}

	final := history[len(history)-1]
	return final.Content, nil
}
