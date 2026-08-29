package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"harper/internal/config"
	"harper/internal/llm"
	"harper/internal/session"
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
		"--mode", "simple",
	})
	if err != nil {
		t.Fatalf("parseRunFlags: %v", err)
	}
	if flags.WorkDir != "/workspace" || flags.Sandbox != "local" || flags.MaxTurns != 10 ||
		flags.LogPath != "session.jsonl" || flags.ConfigPath != "harper.yaml" ||
		flags.Model != "gpt-oss:20b" || flags.Effort != "high" || flags.Mode != "simple" {
		t.Fatalf("unexpected flags: %+v", flags)
	}
}

func TestParseRunFlags_ModelAndEffortDefaultEmpty(t *testing.T) {
	flags, err := parseRunFlags([]string{"do the thing"})
	if err != nil {
		t.Fatalf("parseRunFlags: %v", err)
	}
	if flags.Model != "" || flags.Effort != "" || flags.Mode != "" {
		t.Fatalf("expected empty Model/Effort/Mode by default, got %+v", flags)
	}
}

func TestApplyModeOverride_OverridesConfigMode(t *testing.T) {
	cfg := config.Default()
	cfg = applyModeOverride(cfg, "simple")
	if cfg.Mode != "simple" {
		t.Fatalf("expected Mode overridden to simple, got %q", cfg.Mode)
	}
}

func TestApplyModeOverride_LeavesConfigUntouchedWhenEmpty(t *testing.T) {
	cfg := config.Default()
	cfg.Mode = "simple"
	cfg = applyModeOverride(cfg, "")
	if cfg.Mode != "simple" {
		t.Fatalf("expected Mode unchanged when the flag is empty, got %q", cfg.Mode)
	}
}

func TestParseRunFlags_ErrorsWithNoInstruction(t *testing.T) {
	_, err := parseRunFlags([]string{"--sandbox", "local"})
	if err == nil {
		t.Fatalf("expected an error when no instruction is given")
	}
}

func TestBuildProvider_OllamaAndEmptyDefaultToOllama(t *testing.T) {
	cfg := config.Config{Ollama: config.OllamaConfig{BaseURL: "http://localhost:11434", NumCtx: 16384}}

	for _, providerName := range []string{"ollama", ""} {
		_, err := buildProvider(config.ModelConfig{Provider: providerName, Model: "qwen3-coder:30b"}, cfg)
		if err != nil {
			t.Fatalf("buildProvider(%q): unexpected error: %v", providerName, err)
		}
	}
}

func TestBuildProvider_MissingModelErrorsClearly(t *testing.T) {
	for _, providerName := range []string{"ollama", "", "anthropic", "lmstudio", "llamacpp", "vllm"} {
		_, err := buildProvider(config.ModelConfig{Provider: providerName, Model: ""}, config.Config{})
		if err == nil {
			t.Fatalf("buildProvider(%q, model=\"\"): expected an error when no model is configured", providerName)
		}
	}
}

func TestBuildProvider_UnsupportedProviderErrorsClearly(t *testing.T) {
	_, err := buildProvider(config.ModelConfig{Provider: "made-up-provider", Model: "x"}, config.Config{})
	if err == nil {
		t.Fatalf("expected an error for an unimplemented provider")
	}
}

func TestBuildProvider_AnthropicMissingAPIKeyErrorsClearly(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	_, err := buildProvider(config.ModelConfig{Provider: "anthropic", Model: "claude-sonnet-5"}, config.Config{})
	if err == nil {
		t.Fatalf("expected an error when ANTHROPIC_API_KEY is unset")
	}
}

func TestBuildProvider_AnthropicWithAPIKeySucceeds(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-test-key")
	_, err := buildProvider(config.ModelConfig{Provider: "anthropic", Model: "claude-sonnet-5"}, config.Config{})
	if err != nil {
		t.Fatalf("buildProvider: %v", err)
	}
}

func TestBuildProvider_OpenAICompatFrameworksSucceed(t *testing.T) {
	cfg := config.Config{
		LMStudio: config.OpenAICompatConfig{BaseURL: "http://localhost:1234/v1"},
		LlamaCpp: config.OpenAICompatConfig{BaseURL: "http://localhost:8080/v1"},
		VLLM:     config.OpenAICompatConfig{BaseURL: "http://localhost:8000/v1"},
	}

	for _, providerName := range []string{"lmstudio", "llamacpp", "vllm"} {
		_, err := buildProvider(config.ModelConfig{Provider: providerName, Model: "local-model"}, cfg)
		if err != nil {
			t.Fatalf("buildProvider(%q): unexpected error: %v", providerName, err)
		}
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

func TestBuildBrainLoop_SubtaskAlwaysGetsStaticPermissionChecker(t *testing.T) {
	// buildBrainLoop itself talks to a real llm.Provider via the startup
	// capability check, so it isn't practically unit-testable end-to-end.
	// This pins the underlying behavior buildBrainLoop wires in: an
	// "ask"-configured tool must deny without ever blocking on input.
	cfg := config.PermissionsConfig{Default: "ask"}
	checker := newStaticPermissionChecker(cfg)
	allowed, err := checker(context.Background(), "Bash", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if allowed {
		t.Fatalf("expected the subtask's static checker to deny an 'ask'-configured tool without blocking")
	}
}

func TestRunModeStartup_FailsFastWhenBrainToolAsks(t *testing.T) {
	// In run mode the brain's only tool is Delegate (see buildBrainTools) —
	// that's what validateNoAskForRunMode must be checked against, not the
	// core tool names, since the brain no longer calls those directly.
	cfg := config.PermissionsConfig{Overrides: map[string]string{"Delegate": "ask"}}
	if err := validateNoAskForRunMode(cfg, []string{"Delegate"}); err == nil {
		t.Fatalf("expected run mode startup validation to fail when the brain's Delegate tool resolves to ask")
	}
}

func TestLoadResumedSession_NoSavedSession(t *testing.T) {
	session.SetSessionsRoot(t.TempDir())

	history, notice, cfg := loadResumedSession(config.Config{Brain: config.ModelConfig{Model: "configured-model"}})
	if history != nil {
		t.Fatalf("expected nil history when nothing was saved, got %v", history)
	}
	if notice != "no saved session for this directory" {
		t.Fatalf("unexpected notice: %q", notice)
	}
	if cfg.Brain.Model != "configured-model" {
		t.Fatalf("expected cfg unchanged when nothing was saved, got %q", cfg.Brain.Model)
	}
}

func TestLoadResumedSession_FoundOverlaysBrainAndMode(t *testing.T) {
	session.SetSessionsRoot(t.TempDir())
	saved := session.Session{
		History: []llm.Message{{Role: llm.RoleUser, Content: "hi"}, {Role: llm.RoleAssistant, Content: "hello"}},
		Brain:   config.ModelConfig{Provider: "ollama", Model: "saved-model"},
		Mode:    "simple",
		SavedAt: time.Now(),
	}
	if err := session.Save(".", saved); err != nil {
		t.Fatalf("Save: %v", err)
	}

	history, notice, cfg := loadResumedSession(config.Config{Brain: config.ModelConfig{Model: "configured-model"}})
	if len(history) != 2 {
		t.Fatalf("expected 2 resumed messages, got %d", len(history))
	}
	if cfg.Brain.Model != "saved-model" || cfg.Mode != "simple" {
		t.Fatalf("expected cfg overlaid with the saved session, got brain=%q mode=%q", cfg.Brain.Model, cfg.Mode)
	}
	if !strings.Contains(notice, "resumed session from") || !strings.Contains(notice, "2 message") {
		t.Fatalf("unexpected notice: %q", notice)
	}
}

func TestLoadResumedSession_StripsLeadingSystemMessage(t *testing.T) {
	session.SetSessionsRoot(t.TempDir())
	saved := session.Session{
		History: []llm.Message{
			{Role: llm.RoleSystem, Content: "you are harper, orchestrator mode"},
			{Role: llm.RoleUser, Content: "hi"},
			{Role: llm.RoleAssistant, Content: "hello"},
		},
		Brain: config.ModelConfig{Provider: "ollama", Model: "saved-model"},
		Mode:  "simple",
	}
	if err := session.Save(".", saved); err != nil {
		t.Fatalf("Save: %v", err)
	}

	history, notice, _ := loadResumedSession(config.Config{})
	if len(history) != 2 {
		t.Fatalf("expected the leading system message stripped, leaving 2 messages, got %d: %+v", len(history), history)
	}
	if history[0].Role != llm.RoleUser {
		t.Fatalf("expected the first remaining message to be the user's, got role %q", history[0].Role)
	}
	if !strings.Contains(notice, "2 message") {
		t.Fatalf("expected the notice's message count to exclude the stripped system message, got: %q", notice)
	}
}

func TestLoadResumedSession_LoadErrorReturnsNonEmptyNotice(t *testing.T) {
	dir := t.TempDir()
	session.SetSessionsRoot(dir)
	if err := session.Save(".", session.Session{}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("expected exactly one session file, got entries=%v err=%v", entries, err)
	}
	corruptPath := filepath.Join(dir, entries[0].Name())
	if err := os.WriteFile(corruptPath, []byte("not valid json{{{"), 0o644); err != nil {
		t.Fatalf("corrupt the file: %v", err)
	}

	history, notice, cfg := loadResumedSession(config.Config{Brain: config.ModelConfig{Model: "configured-model"}})
	if history != nil {
		t.Fatalf("expected nil history on a load error, got %v", history)
	}
	if notice == "" {
		t.Fatalf("expected a non-empty notice on a load error, so both front-ends show something instead of silence")
	}
	if cfg.Brain.Model != "configured-model" {
		t.Fatalf("expected cfg unchanged on a load error, got %q", cfg.Brain.Model)
	}
}
