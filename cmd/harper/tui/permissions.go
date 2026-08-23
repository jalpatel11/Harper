package tui

import (
	"context"
	"sync"

	"harper/internal/agent"
	"harper/internal/config"

	tea "github.com/charmbracelet/bubbletea"
)

// sender is the one method newTUIPermissionChecker needs from *tea.Program
// — narrowing to an interface makes this testable without a real program.
type sender interface {
	Send(tea.Msg)
}

// permissionResponse carries both the decision and whether it should be
// remembered — persist is true only for "allow for session" ("s"). Using
// a single chan bool here (as an earlier draft did) collapses "allow once"
// and "allow for session" into the same outcome, since the checker has no
// way to tell them apart from a bare bool; that was a real bug (a user
// choosing "once" silently got session-wide access) caught in review.
type permissionResponse struct {
	allowed bool
	persist bool
}

// newTUIPermissionChecker bridges agent.PermissionChecker (a synchronous
// function called from a background goroutine inside Loop.Run) to Bubble
// Tea's Update loop, which cannot block. "ask" sends a permissionRequestMsg
// and blocks this goroutine (not Update) on the response channel; Update
// answers it once the user's next keypress resolves the modal prompt. The
// mutex is held across that entire wait — see the rationale above for why
// releasing it early (to just guard the map) isn't safe.
func newTUIPermissionChecker(cfg config.PermissionsConfig, program sender) agent.PermissionChecker {
	sessionDecisions := map[string]bool{}
	var mu sync.Mutex

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

		respond := make(chan permissionResponse, 1)
		program.Send(permissionRequestMsg{toolName: toolName, input: input, respond: respond})
		resp := <-respond

		if resp.persist {
			sessionDecisions[toolName] = resp.allowed
		}
		return resp.allowed, nil
	}
}
