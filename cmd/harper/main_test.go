package main

import (
	"testing"

	"harper/internal/config"
)

func TestParseRunFlags_AppliesDefaults(t *testing.T) {
	flags, err := parseRunFlags([]string{"do the thing"})
	if err != nil {
		t.Fatalf("parseRunFlags: %v", err)
	}
	if flags.Instruction != "do the thing" {
		t.Fatalf("unexpected instruction: %q", flags.Instruction)
	}
	if flags.Sandbox != "" {
		t.Fatalf("expected an empty Sandbox when --sandbox isn't passed (resolved later against config), got %q", flags.Sandbox)
	}
	if flags.MaxTurns != 40 {
		t.Fatalf("expected default max-turns 40, got %d", flags.MaxTurns)
	}
	if flags.WorkDir != "." {
		t.Fatalf("expected default workdir '.', got %q", flags.WorkDir)
	}
}

func TestParseRunFlags_OverridesAllFields(t *testing.T) {
	flags, err := parseRunFlags([]string{
		"fix the bug",
		"--workdir", "/workspace",
		"--sandbox", "local",
		"--max-turns", "10",
		"--log", "session.jsonl",
		"--config", "harper.yaml",
		"--model", "gpt-oss:20b",
		"--effort", "high",
	})
	if err != nil {
		t.Fatalf("parseRunFlags: %v", err)
	}
	if flags.WorkDir != "/workspace" || flags.Sandbox != "local" || flags.MaxTurns != 10 ||
		flags.LogPath != "session.jsonl" || flags.ConfigPath != "harper.yaml" ||
		flags.Model != "gpt-oss:20b" || flags.Effort != "high" {
		t.Fatalf("unexpected flags: %+v", flags)
	}
}

func TestParseRunFlags_ModelAndEffortDefaultEmpty(t *testing.T) {
	flags, err := parseRunFlags([]string{"do the thing"})
	if err != nil {
		t.Fatalf("parseRunFlags: %v", err)
	}
	if flags.Model != "" || flags.Effort != "" {
		t.Fatalf("expected empty Model/Effort by default, got %+v", flags)
	}
}

func TestParseRunFlags_ErrorsWithNoInstruction(t *testing.T) {
	_, err := parseRunFlags([]string{"--sandbox", "local"})
	if err == nil {
		t.Fatalf("expected an error when no instruction is given")
	}
}

func TestBuildProvider_OllamaAndEmptyDefaultToOllama(t *testing.T) {
	ollamaCfg := config.OllamaConfig{BaseURL: "http://localhost:11434", NumCtx: 16384}

	for _, providerName := range []string{"ollama", ""} {
		_, err := buildProvider(config.ModelConfig{Provider: providerName, Model: "qwen3-coder:30b"}, ollamaCfg)
		if err != nil {
			t.Fatalf("buildProvider(%q): unexpected error: %v", providerName, err)
		}
	}
}

func TestBuildProvider_UnsupportedProviderErrorsClearly(t *testing.T) {
	_, err := buildProvider(config.ModelConfig{Provider: "made-up-provider", Model: "x"}, config.OllamaConfig{})
	if err == nil {
		t.Fatalf("expected an error for an unimplemented provider")
	}
}

func TestBuildProvider_AnthropicMissingAPIKeyErrorsClearly(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	_, err := buildProvider(config.ModelConfig{Provider: "anthropic", Model: "claude-sonnet-5"}, config.OllamaConfig{})
	if err == nil {
		t.Fatalf("expected an error when ANTHROPIC_API_KEY is unset")
	}
}

func TestBuildProvider_AnthropicWithAPIKeySucceeds(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-test-key")
	_, err := buildProvider(config.ModelConfig{Provider: "anthropic", Model: "claude-sonnet-5"}, config.OllamaConfig{})
	if err != nil {
		t.Fatalf("buildProvider: %v", err)
	}
}

func TestResolveSandboxMode_FlagWinsOverConfig(t *testing.T) {
	if got := resolveSandboxMode("local", "docker"); got != "local" {
		t.Fatalf("expected the flag value to win, got %q", got)
	}
}

func TestResolveSandboxMode_FallsBackToConfigWhenFlagUnset(t *testing.T) {
	if got := resolveSandboxMode("", "local"); got != "local" {
		t.Fatalf("expected the config value when no flag was passed, got %q", got)
	}
}

func TestResolveSandboxMode_FallsBackToLocalWhenBothUnset(t *testing.T) {
	if got := resolveSandboxMode("", ""); got != "local" {
		t.Fatalf("expected the hardcoded 'local' fallback, got %q", got)
	}
}

func TestApplyModelOverrides_OverridesBothBrainAndSubtask(t *testing.T) {
	cfg := config.Default()
	cfg = applyModelOverrides(cfg, "gpt-oss:20b", "high")

	if cfg.Brain.Model != "gpt-oss:20b" || cfg.Subtask.Model != "gpt-oss:20b" {
		t.Fatalf("expected both roles' Model overridden, got brain=%q subtask=%q", cfg.Brain.Model, cfg.Subtask.Model)
	}
	if cfg.Brain.Effort != "high" || cfg.Subtask.Effort != "high" {
		t.Fatalf("expected both roles' Effort overridden, got brain=%q subtask=%q", cfg.Brain.Effort, cfg.Subtask.Effort)
	}
}

func TestApplyModelOverrides_LeavesConfigUntouchedWhenEmpty(t *testing.T) {
	cfg := config.Default()
	originalBrain, originalSubtask := cfg.Brain, cfg.Subtask
	cfg = applyModelOverrides(cfg, "", "")

	if cfg.Brain != originalBrain || cfg.Subtask != originalSubtask {
		t.Fatalf("expected brain/subtask config unchanged when model/effort flags are empty, got brain=%+v subtask=%+v", cfg.Brain, cfg.Subtask)
	}
}
