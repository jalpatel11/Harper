package tools

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
)

type GrepTool struct {
	maxEntries int
	timeout    time.Duration
}

func NewGrepTool() *GrepTool { return &GrepTool{maxEntries: maxWalkEntries, timeout: walkTimeout} }

func (t *GrepTool) Name() string        { return "Grep" }
func (t *GrepTool) Description() string { return "Search file contents recursively for a regex pattern." }
func (t *GrepTool) InputSchema() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{"pattern", "path"},
		"properties": map[string]any{
			"pattern": map[string]any{"type": "string"},
			"path":    map[string]any{"type": "string"},
		},
	}
}

func (t *GrepTool) Execute(ctx context.Context, input map[string]any) (string, error) {
	patternStr, _ := input["pattern"].(string)
	root, _ := input["path"].(string)
	if patternStr == "" || root == "" {
		return "", fmt.Errorf("grep: missing required \"pattern\" or \"path\" argument")
	}
	re, err := regexp.Compile(patternStr)
	if err != nil {
		return "", fmt.Errorf("grep: invalid pattern: %w", err)
	}

	var matches []string
	truncated, err := boundedWalk(ctx, root, t.maxEntries, t.timeout, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer f.Close()

		scanner := bufio.NewScanner(f)
		lineNum := 0
		for scanner.Scan() {
			lineNum++
			line := scanner.Text()
			if re.MatchString(line) {
				matches = append(matches, fmt.Sprintf("%s:%d:%s", path, lineNum, line))
			}
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("grep: walk %s: %w", root, err)
	}

	out := strings.Join(matches, "\n")
	if truncated {
		out += fmt.Sprintf("\n... (truncated: search stopped after %d entries or %s, results may be incomplete — narrow the path)", t.maxEntries, t.timeout)
	}
	return out, nil
}
