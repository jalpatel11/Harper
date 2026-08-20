package agent

import (
	"context"
	"fmt"
	"sync"

	"harper/internal/llm"
	"harper/internal/tools"
)

type Loop struct {
	provider          llm.Provider
	tools             map[string]tools.Tool
	toolDefs          []llm.ToolDef
	systemPrompt      string
	permissionChecker PermissionChecker
}

// PermissionChecker is consulted immediately before a tool executes.
// Returning false denies the call without running it; a returned error is
// treated the same as a denial, with the error surfaced as the tool's
// result content so the model can see and adapt to it.
type PermissionChecker func(ctx context.Context, toolName string, input map[string]any) (bool, error)

func (l *Loop) SetPermissionChecker(pc PermissionChecker) {
	l.permissionChecker = pc
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

		// Multiple tool calls in one response — most often multiple
		// Delegate calls, the brain fanning independent subtasks out to
		// the subtask model — run concurrently rather than one at a time.
		// Results are still published to history/onStep in the original
		// call order once every call finishes, so callers (loggers, REPL
		// output) never see interleaved or out-of-order output.
		toolMsgs := make([]llm.Message, len(resp.Message.ToolCalls))
		var wg sync.WaitGroup
		for i, call := range resp.Message.ToolCalls {
			wg.Add(1)
			go func(i int, call llm.ToolCall) {
				defer wg.Done()
				toolMsgs[i] = llm.Message{
					Role:       llm.RoleTool,
					Content:    l.executeToolCall(ctx, call),
					ToolCallID: call.ID,
				}
			}(i, call)
		}
		wg.Wait()

		for _, toolMsg := range toolMsgs {
			history = append(history, toolMsg)
			onStep(toolMsg)
		}
	}

	return history, fmt.Errorf("agent loop: exceeded max turns (%d)", maxTurns)
}

func (l *Loop) executeToolCall(ctx context.Context, call llm.ToolCall) string {
	tool, ok := l.tools[call.Name]
	if !ok {
		return fmt.Sprintf("error: unknown tool %q", call.Name)
	}

	if l.permissionChecker != nil {
		allowed, err := l.permissionChecker(ctx, call.Name, call.Args)
		if err != nil {
			return fmt.Sprintf("error: permission check failed: %v", err)
		}
		if !allowed {
			return fmt.Sprintf("permission denied: %s was not approved to run", call.Name)
		}
	}

	out, execErr := tool.Execute(ctx, call.Args)
	if execErr != nil {
		return fmt.Sprintf("error: %v", execErr)
	}
	return out
}
