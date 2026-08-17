package main

import (
	"bufio"
	"context"
	"fmt"
	"io"

	"harper/internal/agent"
	"harper/internal/llm"
)

func RunREPL(ctx context.Context, loop *agent.Loop, in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	var history []llm.Message

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		history = append(history, llm.Message{Role: llm.RoleUser, Content: line})

		updated, err := loop.Run(ctx, history, 30, func(m llm.Message) {
			if m.Role == llm.RoleTool {
				fmt.Fprintf(out, "[tool result for %s]: %s\n", m.ToolCallID, m.Content)
			}
		})
		if err != nil {
			fmt.Fprintf(out, "error: %v\n", err)
			history = updated
			continue
		}
		history = updated

		final := history[len(history)-1]
		fmt.Fprintf(out, "> %s\n", final.Content)
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("repl: reading input: %w", err)
	}
	return nil
}
