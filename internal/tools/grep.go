package tools

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type GrepTool struct{}

func NewGrepTool() *GrepTool { return &GrepTool{} }

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
	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
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

	return strings.Join(matches, "\n"), nil
}
