package tools

import (
	"context"
	"fmt"
	"os"
	"strings"
)

type EditTool struct{}

func NewEditTool() *EditTool { return &EditTool{} }

func (t *EditTool) Name() string        { return "Edit" }
func (t *EditTool) Description() string { return "Replace an exact, unique string match in a file." }
func (t *EditTool) InputSchema() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{"path", "old_string", "new_string"},
		"properties": map[string]any{
			"path":       map[string]any{"type": "string"},
			"old_string": map[string]any{"type": "string"},
			"new_string": map[string]any{"type": "string"},
		},
	}
}

func (t *EditTool) Execute(ctx context.Context, input map[string]any) (string, error) {
	path, _ := input["path"].(string)
	oldStr, _ := input["old_string"].(string)
	newStr, _ := input["new_string"].(string)
	if path == "" || oldStr == "" {
		return "", fmt.Errorf("edit: missing required \"path\" or \"old_string\" argument")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("edit %s: %w", path, err)
	}
	content := string(data)

	count := strings.Count(content, oldStr)
	if count == 0 {
		return "", fmt.Errorf("edit %s: old_string not found", path)
	}
	if count > 1 {
		return "", fmt.Errorf("edit %s: old_string is not unique (%d matches)", path, count)
	}

	updated := strings.Replace(content, oldStr, newStr, 1)
	if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
		return "", fmt.Errorf("edit %s: write back: %w", path, err)
	}
	return fmt.Sprintf("edited %s", path), nil
}
