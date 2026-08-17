package logging

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestJSONLLogger_WritesOneJSONObjectPerLine(t *testing.T) {
	var buf bytes.Buffer
	logger := NewJSONLLogger(&buf)

	err := logger.Log(TurnLog{
		Timestamp: time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC),
		Role:      "tool",
		Tool:      "Bash",
		Input:     map[string]any{"command": "ls"},
		Result:    "a.go\nb.go",
		LatencyMS: 42,
	})
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	err = logger.Log(TurnLog{
		Role:             "assistant",
		Result:           "done",
		PromptTokens:     120,
		CompletionTokens: 8,
	})
	if err != nil {
		t.Fatalf("Log: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), buf.String())
	}

	var first TurnLog
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("unmarshal first line: %v", err)
	}
	if first.Tool != "Bash" || first.LatencyMS != 42 {
		t.Fatalf("unexpected first entry: %+v", first)
	}

	var second TurnLog
	if err := json.Unmarshal([]byte(lines[1]), &second); err != nil {
		t.Fatalf("unmarshal second line: %v", err)
	}
	if second.PromptTokens != 120 || second.CompletionTokens != 8 {
		t.Fatalf("expected token usage to round-trip through the log, got: %+v", second)
	}
}
