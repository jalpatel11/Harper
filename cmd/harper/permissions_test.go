package main

import (
	"bufio"
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"

	"harper/internal/config"
)

func TestNewStaticPermissionChecker_AllowsOnlyAllowMode(t *testing.T) {
	cfg := config.PermissionsConfig{
		Overrides: map[string]string{"Bash": "ask", "Write": "deny", "Read": "allow"},
	}
	checker := newStaticPermissionChecker(cfg)

	cases := map[string]bool{"Bash": false, "Write": false, "Read": true}
	for tool, want := range cases {
		got, err := checker(context.Background(), tool, nil)
		if err != nil {
			t.Fatalf("checker(%q): unexpected error: %v", tool, err)
		}
		if got != want {
			t.Fatalf("checker(%q) = %v, want %v", tool, got, want)
		}
	}
}

func TestNewStaticPermissionChecker_UnconfiguredToolDefaultsToAllow(t *testing.T) {
	checker := newStaticPermissionChecker(config.PermissionsConfig{})
	got, err := checker(context.Background(), "Grep", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got {
		t.Fatalf("expected an unconfigured tool to default to allow")
	}
}

func TestValidateNoAskForRunMode_ErrorsWhenAToolAsks(t *testing.T) {
	cfg := config.PermissionsConfig{Overrides: map[string]string{"Bash": "ask"}}
	err := validateNoAskForRunMode(cfg, []string{"Read", "Bash", "Write"})
	if err == nil {
		t.Fatalf("expected an error when a tool resolves to ask")
	}
}

func TestValidateNoAskForRunMode_PassesWhenNothingAsks(t *testing.T) {
	cfg := config.PermissionsConfig{Overrides: map[string]string{"Bash": "deny"}}
	err := validateNoAskForRunMode(cfg, []string{"Read", "Bash", "Write"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewInteractivePermissionChecker_AllowAndDenyNeedNoInput(t *testing.T) {
	cfg := config.PermissionsConfig{Overrides: map[string]string{"Read": "allow", "Write": "deny"}}
	scanner := bufio.NewScanner(strings.NewReader("")) // no input available
	var out bytes.Buffer
	checker := newInteractivePermissionChecker(cfg, scanner, &out)

	got, err := checker(context.Background(), "Read", nil)
	if err != nil || !got {
		t.Fatalf("expected Read to allow with no input needed, got %v, %v", got, err)
	}
	got, err = checker(context.Background(), "Write", nil)
	if err != nil || got {
		t.Fatalf("expected Write to deny with no input needed, got %v, %v", got, err)
	}
}

func TestNewInteractivePermissionChecker_AskPromptsAndReadsOnce(t *testing.T) {
	cfg := config.PermissionsConfig{Default: "ask"}
	scanner := bufio.NewScanner(strings.NewReader("o\n"))
	var out bytes.Buffer
	checker := newInteractivePermissionChecker(cfg, scanner, &out)

	got, err := checker(context.Background(), "Bash", map[string]any{"command": "ls"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got {
		t.Fatalf("expected 'o' (allow once) to allow")
	}
	if !strings.Contains(out.String(), "Bash") {
		t.Fatalf("expected the prompt to mention the tool name, got: %q", out.String())
	}
}

func TestNewInteractivePermissionChecker_DenyResponse(t *testing.T) {
	cfg := config.PermissionsConfig{Default: "ask"}
	scanner := bufio.NewScanner(strings.NewReader("d\n"))
	checker := newInteractivePermissionChecker(cfg, scanner, &bytes.Buffer{})

	got, err := checker(context.Background(), "Bash", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got {
		t.Fatalf("expected 'd' to deny")
	}
}

func TestNewInteractivePermissionChecker_AllowForSessionRemembersDecision(t *testing.T) {
	cfg := config.PermissionsConfig{Default: "ask"}
	// Only one line of input available. If the second call to the same
	// tool re-prompts, it will fail to read (Scan returns false) rather
	// than reusing the remembered decision.
	scanner := bufio.NewScanner(strings.NewReader("s\n"))
	checker := newInteractivePermissionChecker(cfg, scanner, &bytes.Buffer{})

	got1, err := checker(context.Background(), "Bash", nil)
	if err != nil || !got1 {
		t.Fatalf("expected first call to allow, got %v, %v", got1, err)
	}

	got2, err := checker(context.Background(), "Bash", nil)
	if err != nil {
		t.Fatalf("unexpected error on remembered call: %v", err)
	}
	if !got2 {
		t.Fatalf("expected the second call to reuse the remembered 'allow for session' decision without reading more input")
	}
}

func TestNewInteractivePermissionChecker_ConcurrentCallsToDifferentAllowedToolsDontRace(t *testing.T) {
	// The brain can now fire multiple tool calls (e.g. several Delegate
	// calls) within one turn, executed concurrently (see internal/agent's
	// parallel tool-call execution). "allow"/"deny" modes need no shared
	// state, but this must still be safe to call from many goroutines at
	// once — run under -race to catch a missing lock.
	cfg := config.PermissionsConfig{Default: "allow"}
	scanner := bufio.NewScanner(strings.NewReader(""))
	checker := newInteractivePermissionChecker(cfg, scanner, &bytes.Buffer{})

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := checker(context.Background(), "Delegate", nil); err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()
}
