package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"harper/internal/agent"
	"harper/internal/config"
	"harper/internal/llm"
)

func RunREPL(ctx context.Context, loop *agent.Loop, in io.Reader, out io.Writer, cfg config.Config) error {
	scanner := bufio.NewScanner(in)
	loop.SetPermissionChecker(newInteractivePermissionChecker(cfg.Permissions, scanner, out))

	var history []llm.Message

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "/") {
			if err := handleREPLCommand(loop, scanner, out, &cfg, line); err != nil {
				fmt.Fprintf(out, "error: %v\n", err)
			}
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

// handleREPLCommand dispatches a "/"-prefixed line. Unknown commands print
// a message rather than erroring, so a typo doesn't look like a crash.
func handleREPLCommand(loop *agent.Loop, scanner *bufio.Scanner, out io.Writer, cfg *config.Config, line string) error {
	fields := strings.Fields(line)
	switch fields[0] {
	case "/model":
		return handleModelCommand(loop, scanner, out, cfg, fields[1:])
	default:
		fmt.Fprintf(out, "unknown command: %s\n", fields[0])
		return nil
	}
}

// handleModelCommand switches the brain's model (and rebuilds its
// provider). It only ever changes the brain — in orchestrator mode the
// subtask model actually doing task work isn't reachable from the REPL's
// loop reference and is unaffected by this command; it's fully meaningful
// in `mode: simple`, where the brain is the only model in the loop.
//
// With no argument, already-pulled Ollama models are listed for picking by
// number (mirroring Pi's /model picker); other providers just prompt for a
// name directly, since there's no equivalent list-models call wired in for
// them yet.
func handleModelCommand(loop *agent.Loop, scanner *bufio.Scanner, out io.Writer, cfg *config.Config, args []string) error {
	chosen := ""
	if len(args) > 0 {
		chosen = args[0]
	} else {
		var options []string
		if cfg.Brain.Provider == "ollama" || cfg.Brain.Provider == "" {
			models, err := llm.ListOllamaModels(cfg.Ollama.BaseURL)
			if err != nil {
				return fmt.Errorf("list ollama models: %w", err)
			}
			options = models
			if len(options) > 0 {
				fmt.Fprintln(out, "Available Ollama models:")
				for i, m := range options {
					fmt.Fprintf(out, "  %d) %s\n", i+1, m)
				}
			}
		}
		fmt.Fprint(out, "Pick a number, or type a model name: ")
		if !scanner.Scan() {
			return fmt.Errorf("model prompt: no input available")
		}
		resp := strings.TrimSpace(scanner.Text())
		if n, err := strconv.Atoi(resp); err == nil && n >= 1 && n <= len(options) {
			chosen = options[n-1]
		} else {
			chosen = resp
		}
	}
	if chosen == "" {
		return fmt.Errorf("no model chosen")
	}

	cfg.Brain.Model = chosen
	newProvider, err := buildProvider(cfg.Brain, *cfg)
	if err != nil {
		return err
	}
	loop.SetProvider(newProvider)
	fmt.Fprintf(out, "model set to %q\n", chosen)
	return nil
}
