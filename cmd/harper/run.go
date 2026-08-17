package main

import (
	"context"
	"time"

	"harper/internal/agent"
	"harper/internal/llm"
	"harper/internal/logging"
)

func RunOnce(ctx context.Context, loop *agent.Loop, instruction string, maxTurns int, logger *logging.JSONLLogger) (string, error) {
	start := time.Now()

	history, err := loop.Run(ctx, []llm.Message{{Role: llm.RoleUser, Content: instruction}}, maxTurns, func(m llm.Message) {
		entry := logging.TurnLog{
			Role:      string(m.Role),
			Result:    m.Content,
			LatencyMS: time.Since(start).Milliseconds(),
		}
		if len(m.ToolCalls) > 0 {
			entry.Tool = m.ToolCalls[0].Name
			entry.Input = m.ToolCalls[0].Args
		}
		if m.Usage != nil {
			entry.PromptTokens = m.Usage.PromptTokens
			entry.CompletionTokens = m.Usage.CompletionTokens
		}
		logger.Log(entry)
	})
	if err != nil {
		logger.Log(logging.TurnLog{Role: "error", Error: err.Error()})
		return "", err
	}

	final := history[len(history)-1]
	return final.Content, nil
}
