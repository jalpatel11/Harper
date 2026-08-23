package tui

import (
	"context"
	"sync"
	"testing"
	"time"

	"harper/internal/config"

	tea "github.com/charmbracelet/bubbletea"
)

// fakeSender captures sent messages instead of driving a real Bubble Tea
// program — newTUIPermissionChecker only needs something shaped like
// (*tea.Program).Send, so this stands in for it in tests. Auto-responds
// with persist:true (simulating "s", allow for session) by default, so
// existing "remembers across calls" tests still exercise real persistence
// rather than the bug this fix closes — see
// TestNewTUIPermissionChecker_AllowOnceDoesNotPersist below for the
// persist:false path.
type fakeSender struct {
	sent []tea.Msg
}

func (f *fakeSender) Send(msg tea.Msg) {
	f.sent = append(f.sent, msg)
	if req, ok := msg.(permissionRequestMsg); ok {
		req.respond <- permissionResponse{allowed: true, persist: true}
	}
}

func TestNewTUIPermissionChecker_AllowAndDenyNeedNoPrompt(t *testing.T) {
	cfg := config.PermissionsConfig{Overrides: map[string]string{"Read": "allow", "Write": "deny"}}
	sender := &fakeSender{}
	checker := newTUIPermissionChecker(cfg, sender)

	got, err := checker(context.Background(), "Read", nil)
	if err != nil || !got {
		t.Fatalf("expected Read to allow with no prompt, got %v, %v", got, err)
	}
	got, err = checker(context.Background(), "Write", nil)
	if err != nil || got {
		t.Fatalf("expected Write to deny with no prompt, got %v, %v", got, err)
	}
	if len(sender.sent) != 0 {
		t.Fatalf("expected no messages sent for allow/deny, got %d", len(sender.sent))
	}
}

func TestNewTUIPermissionChecker_AskSendsRequestAndWaitsOnRespond(t *testing.T) {
	cfg := config.PermissionsConfig{Default: "ask"}
	sender := &fakeSender{}
	checker := newTUIPermissionChecker(cfg, sender)

	got, err := checker(context.Background(), "Bash", map[string]any{"command": "ls"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got {
		t.Fatalf("expected the fakeSender's auto-approve to allow")
	}
	if len(sender.sent) != 1 {
		t.Fatalf("expected exactly 1 permissionRequestMsg sent, got %d", len(sender.sent))
	}
	req, ok := sender.sent[0].(permissionRequestMsg)
	if !ok || req.toolName != "Bash" {
		t.Fatalf("unexpected sent message: %+v", sender.sent[0])
	}
}

func TestNewTUIPermissionChecker_RemembersAllowForSession(t *testing.T) {
	cfg := config.PermissionsConfig{Default: "ask"}
	sender := &fakeSender{}
	checker := newTUIPermissionChecker(cfg, sender)

	if _, err := checker(context.Background(), "Bash", nil); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if _, err := checker(context.Background(), "Bash", nil); err != nil {
		t.Fatalf("second call: %v", err)
	}
	if len(sender.sent) != 1 {
		t.Fatalf("expected only 1 prompt sent — the second call should reuse the remembered decision, got %d prompts", len(sender.sent))
	}
}

// Regression test for a real bug caught in review: a first draft of this
// checker persisted every answer into sessionDecisions regardless of which
// button resolved it, so "allow once" silently became "allow for the rest
// of the session" — a user choosing "once" was unknowingly granting
// standing access. Only persist:true (typed from "s") may cause the next
// call to skip prompting.
func TestNewTUIPermissionChecker_AllowOnceDoesNotPersist(t *testing.T) {
	cfg := config.PermissionsConfig{Default: "ask"}
	sender := &onceSender{} // responds persist:false, simulating "o"
	checker := newTUIPermissionChecker(cfg, sender)

	if _, err := checker(context.Background(), "Bash", nil); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if _, err := checker(context.Background(), "Bash", nil); err != nil {
		t.Fatalf("second call: %v", err)
	}
	if sender.count != 2 {
		t.Fatalf("expected 2 prompts sent — 'allow once' must not be remembered for the second call, got %d", sender.count)
	}
}

type onceSender struct {
	count int
}

func (s *onceSender) Send(msg tea.Msg) {
	s.count++
	if req, ok := msg.(permissionRequestMsg); ok {
		req.respond <- permissionResponse{allowed: true, persist: false}
	}
}

// Regression test for a second real bug caught in review: Loop.Run's
// concurrent tool-call fan-out (internal/agent/loop.go) means two
// goroutines can independently resolve to "ask" at the same time. A first
// draft only guarded the sessionDecisions map with the mutex, releasing it
// before sending the prompt and waiting on the response — so a second
// concurrent "ask" could send its own permissionRequestMsg while the first
// was still awaiting an answer, and whichever the TUI's Update processed
// last would clobber m.pendingPrompt's respond channel, leaving the first
// goroutine blocked on <-respond forever (a goroutine leak, and since
// Loop.Run waits for every fanned-out call, an indefinitely hung turn).
// This test proves the fix: holding the mutex across the entire
// ask-and-wait sequence serializes concurrent askers, so at most one
// permissionRequestMsg is ever in flight at a time.
func TestNewTUIPermissionChecker_ConcurrentAsksAreSerializedNotClobbered(t *testing.T) {
	cfg := config.PermissionsConfig{Default: "ask"}
	sender := &trackingSender{}
	checker := newTUIPermissionChecker(cfg, sender)

	var wg sync.WaitGroup
	for _, tool := range []string{"Bash", "Write"} {
		wg.Add(1)
		go func(tool string) {
			defer wg.Done()
			if _, err := checker(context.Background(), tool, nil); err != nil {
				t.Errorf("checker(%q): %v", tool, err)
			}
		}(tool)
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("checker calls did not return — likely the clobbering deadlock this test guards against")
	}

	sender.mu.Lock()
	defer sender.mu.Unlock()
	if sender.maxInFlight > 1 {
		t.Fatalf("expected at most 1 permissionRequestMsg in flight at a time (mutex-serialized), got a peak of %d concurrent", sender.maxInFlight)
	}
}

// trackingSender records the peak number of concurrently in-flight
// permissionRequestMsgs — if the checker properly serializes concurrent
// askers via its mutex, this never exceeds 1.
type trackingSender struct {
	mu          sync.Mutex
	inFlight    int
	maxInFlight int
}

func (s *trackingSender) Send(msg tea.Msg) {
	req, ok := msg.(permissionRequestMsg)
	if !ok {
		return
	}
	s.mu.Lock()
	s.inFlight++
	if s.inFlight > s.maxInFlight {
		s.maxInFlight = s.inFlight
	}
	s.mu.Unlock()

	// Simulate a human taking a moment to answer, so a real clobbering bug
	// has a window to manifest instead of racing past it.
	time.Sleep(20 * time.Millisecond)
	req.respond <- permissionResponse{allowed: true, persist: false}

	s.mu.Lock()
	s.inFlight--
	s.mu.Unlock()
}
