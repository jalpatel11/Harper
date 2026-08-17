package logging

import (
	"encoding/json"
	"io"
	"time"
)

type TurnLog struct {
	Timestamp        time.Time      `json:"timestamp"`
	Role             string         `json:"role"`
	Tool             string         `json:"tool,omitempty"`
	Input            map[string]any `json:"input,omitempty"`
	Result           string         `json:"result,omitempty"`
	LatencyMS        int64          `json:"latency_ms,omitempty"`
	Error            string         `json:"error,omitempty"`
	PromptTokens     int            `json:"prompt_tokens,omitempty"`
	CompletionTokens int            `json:"completion_tokens,omitempty"`
}

type JSONLLogger struct {
	enc *json.Encoder
}

func NewJSONLLogger(w io.Writer) *JSONLLogger {
	return &JSONLLogger{enc: json.NewEncoder(w)}
}

func (l *JSONLLogger) Log(entry TurnLog) error {
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now().UTC()
	}
	return l.enc.Encode(entry)
}
