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

// newTUIPermissionChecker bridges agent.PermissionChecker (a synchronous
// function called from a background goroutine inside Loop.Run) to Bubble
// Tea's Update loop, which cannot block. "ask" sends a permissionRequestMsg
// and blocks this goroutine (not Update) on the response channel; Update
// answers it once the user's next keypress resolves the modal prompt.
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
		if decision, remembered := sessionDecisions[toolName]; remembered {
			mu.Unlock()
			return decision, nil
		}
		mu.Unlock()

		respond := make(chan bool, 1)
		program.Send(permissionRequestMsg{toolName: toolName, input: input, respond: respond})
		allowed := <-respond

		mu.Lock()
		sessionDecisions[toolName] = allowed
		mu.Unlock()
		return allowed, nil
	}
}
