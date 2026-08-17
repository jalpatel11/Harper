package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type GlobTool struct {
	maxEntries int
	timeout    time.Duration
}

func NewGlobTool() *GlobTool { return &GlobTool{maxEntries: maxWalkEntries, timeout: walkTimeout} }

func (t *GlobTool) Name() string        { return "Glob" }
func (t *GlobTool) Description() string { return "Find files recursively matching a glob pattern." }
func (t *GlobTool) InputSchema() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{"pattern", "path"},
		"properties": map[string]any{
			"pattern": map[string]any{"type": "string"},
			"path":    map[string]any{"type": "string"},
		},
	}
}

func (t *GlobTool) Execute(ctx context.Context, input map[string]any) (string, error) {
	pattern, _ := input["pattern"].(string)
	root, _ := input["path"].(string)
	if pattern == "" || root == "" {
		return "", fmt.Errorf("glob: missing required \"pattern\" or \"path\" argument")
	}

	var matches []string
	truncated, err := boundedWalk(ctx, root, t.maxEntries, t.timeout, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		ok, matchErr := filepath.Match(pattern, filepath.Base(path))
		if matchErr == nil && ok {
			matches = append(matches, path)
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("glob: walk %s: %w", root, err)
	}

	out := strings.Join(matches, "\n")
	if truncated {
		out += fmt.Sprintf("\n... (truncated: search stopped after %d entries or %s, results may be incomplete — narrow the path)", t.maxEntries, t.timeout)
	}
	return out, nil
}
