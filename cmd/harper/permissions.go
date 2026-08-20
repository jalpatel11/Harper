package main

import (
	"context"
	"fmt"

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
