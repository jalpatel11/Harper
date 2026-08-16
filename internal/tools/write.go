package tools

import (
	"context"
	"fmt"
	"os"
)

type WriteTool struct{}

func NewWriteTool() *WriteTool { return &WriteTool{} }

func (t *WriteTool) Name() string        { return "Write" }
func (t *WriteTool) Description() string { return "Write content to a file, creating or overwriting it." }
func (t *WriteTool) InputSchema() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{"path", "content"},
		"properties": map[string]any{
			"path":    map[string]any{"type": "string"},
			"content": map[string]any{"type": "string"},
		},
	}
}

func (t *WriteTool) Execute(ctx context.Context, input map[string]any) (string, error) {
	path, ok := input["path"].(string)
	if !ok || path == "" {
		return "", fmt.Errorf("write: missing required \"path\" argument")
	}
	content, ok := input["content"].(string)
	if !ok {
		return "", fmt.Errorf("write: missing required \"content\" argument")
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", path, err)
	}
	return fmt.Sprintf("wrote %d bytes to %s", len(content), path), nil
}
