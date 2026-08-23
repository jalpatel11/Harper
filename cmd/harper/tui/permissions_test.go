package tui

import (
	"context"
	"testing"

	"harper/internal/config"

	tea "github.com/charmbracelet/bubbletea"
)

// fakeSender captures sent messages instead of driving a real Bubble Tea
// program — newTUIPermissionChecker only needs something shaped like
// (*tea.Program).Send, so this stands in for it in tests.
type fakeSender struct {
	sent []tea.Msg
}

func (f *fakeSender) Send(msg tea.Msg) {
	f.sent = append(f.sent, msg)
	if req, ok := msg.(permissionRequestMsg); ok {
		req.respond <- true // auto-approve, for tests that don't need to control the answer
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
