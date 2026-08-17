package agent

import (
	"context"
	"fmt"

	"harper/internal/llm"
)

func CheckToolCallCapability(ctx context.Context, provider llm.Provider, roleName string) error {
	probeTool := llm.ToolDef{
		Name:        "harper_probe",
		Description: "Call this tool with no arguments to confirm tool-calling support.",
		Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
	}

	resp, err := provider.Complete(ctx, []llm.Message{
		{Role: llm.RoleUser, Content: "Call the harper_probe tool now, with no arguments."},
	}, []llm.ToolDef{probeTool})
	if err != nil {
		return fmt.Errorf("capability check (%s): %w", roleName, err)
	}

	if len(resp.Message.ToolCalls) == 0 {
		return fmt.Errorf("capability check (%s): model did not produce a native tool call; this model is not supported without a prompt-based fallback (out of scope for v1)", roleName)
	}
	return nil
}
