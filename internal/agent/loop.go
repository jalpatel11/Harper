package agent

import (
	"context"
	"fmt"

	"harper/internal/llm"
	"harper/internal/tools"
)

type Loop struct {
	provider     llm.Provider
	tools        map[string]tools.Tool
	toolDefs     []llm.ToolDef
	systemPrompt string
}

func NewLoop(provider llm.Provider, toolset []tools.Tool, systemPrompt string) *Loop {
	l := &Loop{
		provider:     provider,
		tools:        make(map[string]tools.Tool, len(toolset)),
		systemPrompt: systemPrompt,
	}
	for _, t := range toolset {
		l.tools[t.Name()] = t
		l.toolDefs = append(l.toolDefs, llm.ToolDef{
			Name:        t.Name(),
			Description: t.Description(),
			Parameters:  t.InputSchema(),
		})
	}
	return l
}

type StepFunc func(llm.Message)

func (l *Loop) Run(ctx context.Context, history []llm.Message, maxTurns int, onStep StepFunc) ([]llm.Message, error) {
	if onStep == nil {
		onStep = func(llm.Message) {}
	}
	if l.systemPrompt != "" && (len(history) == 0 || history[0].Role != llm.RoleSystem) {
		history = append([]llm.Message{{Role: llm.RoleSystem, Content: l.systemPrompt}}, history...)
	}

	for turn := 0; turn < maxTurns; turn++ {
		resp, err := l.provider.Complete(ctx, history, l.toolDefs)
		if err != nil {
			return history, fmt.Errorf("agent loop: complete: %w", err)
		}
		history = append(history, resp.Message)
		onStep(resp.Message)

		if len(resp.Message.ToolCalls) == 0 {
			return history, nil
		}

		for _, call := range resp.Message.ToolCalls {
			tool, ok := l.tools[call.Name]
			var result string
			if !ok {
				result = fmt.Sprintf("error: unknown tool %q", call.Name)
			} else {
				out, execErr := tool.Execute(ctx, call.Args)
				if execErr != nil {
					result = fmt.Sprintf("error: %v", execErr)
				} else {
					result = out
				}
			}
			toolMsg := llm.Message{
				Role:       llm.RoleTool,
				Content:    result,
				ToolCallID: call.ID,
			}
			history = append(history, toolMsg)
			onStep(toolMsg)
		}
	}

	return history, fmt.Errorf("agent loop: exceeded max turns (%d)", maxTurns)
}
