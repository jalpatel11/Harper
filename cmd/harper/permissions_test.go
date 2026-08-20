package main

import (
	"context"
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
