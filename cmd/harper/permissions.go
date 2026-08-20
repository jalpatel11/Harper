package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"

	"harper/internal/agent"
	"harper/internal/config"
)

// newStaticPermissionChecker never blocks on interactive input. Only
// "allow" grants access — "deny" and "ask" both resolve to denied, since a
// static checker has no way to actually ask anyone. This is what the
// subtask loop always uses (it has no terminal of its own, REPL or not),
// and what the brain uses in `run` mode, after validateNoAskForRunMode has
// already confirmed no brain-facing tool resolves to "ask" at startup.
func newStaticPermissionChecker(cfg config.PermissionsConfig) agent.PermissionChecker {
	return func(ctx context.Context, toolName string, input map[string]any) (bool, error) {
		return config.ResolvePermissionMode(toolName, cfg) == "allow", nil
	}
}

// newInteractivePermissionChecker must read from the exact same scanner
// RunREPL uses for its own input loop — a second, independent reader over
// the same underlying io.Reader would corrupt input, since both would be
// pulling from the same stream without coordination.
//
// The brain can fire multiple tool calls within one turn (e.g. several
// Delegate calls) that the agent loop runs concurrently, so this checker
// can be invoked from several goroutines at once. mu serializes access to
// the shared scanner and the session-remembered decisions — without it,
// two concurrent "ask" prompts would interleave their output and race on
// sessionDecisions.
func newInteractivePermissionChecker(cfg config.PermissionsConfig, scanner *bufio.Scanner, out io.Writer) agent.PermissionChecker {
	var mu sync.Mutex
	sessionDecisions := map[string]bool{}

	return func(ctx context.Context, toolName string, input map[string]any) (bool, error) {
		switch config.ResolvePermissionMode(toolName, cfg) {
		case "deny":
			return false, nil
		case "allow":
			return true, nil
		}

		mu.Lock()
		defer mu.Unlock()

		if decision, remembered := sessionDecisions[toolName]; remembered {
			return decision, nil
		}

		fmt.Fprintf(out, "[permission] %s wants to run: %v\n", toolName, input)
		fmt.Fprint(out, "allow once / allow for session / deny? [o/s/d] ")

		if !scanner.Scan() {
			return false, fmt.Errorf("permission prompt: no input available")
		}
		switch strings.ToLower(strings.TrimSpace(scanner.Text())) {
		case "s":
			sessionDecisions[toolName] = true
			return true, nil
		case "o", "":
			return true, nil
		default:
			return false, nil
		}
	}
}

// validateNoAskForRunMode fails fast at startup rather than letting `run`
// mode silently deny (or hang, if it somehow tried to prompt) partway
// through a task — there's no REPL to answer an "ask" in run mode.
func validateNoAskForRunMode(cfg config.PermissionsConfig, toolNames []string) error {
	for _, name := range toolNames {
		if config.ResolvePermissionMode(name, cfg) == "ask" {
			return fmt.Errorf(
				"permissions: tool %q is set to \"ask\", but harper run has no way to prompt for confirmation — set permissions.default or permissions.overrides to \"allow\" or \"deny\" for run mode",
				name,
			)
		}
	}
	return nil
}
