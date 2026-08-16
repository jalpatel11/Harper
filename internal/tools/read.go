package tools

import (
	"context"
	"fmt"
	"os"
)

type ReadTool struct{}

func NewReadTool() *ReadTool { return &ReadTool{} }

func (t *ReadTool) Name() string        { return "Read" }
func (t *ReadTool) Description() string { return "Read the contents of a file at the given path." }
func (t *ReadTool) InputSchema() map[string]any {
	return map[string]any{
		"type":       "object",
		"required":   []string{"path"},
		"properties": map[string]any{"path": map[string]any{"type": "string"}},
	}
}

func (t *ReadTool) Execute(ctx context.Context, input map[string]any) (string, error) {
	path, ok := input["path"].(string)
	if !ok || path == "" {
		return "", fmt.Errorf("read: missing required \"path\" argument")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	return string(data), nil
}
