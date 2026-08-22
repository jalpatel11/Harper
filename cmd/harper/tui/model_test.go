package tui

import (
	"context"
	"strings"
	"testing"

	"harper/internal/agent"
	"harper/internal/config"
	"harper/internal/llm"
)

type stubProvider struct{}

func (stubProvider) Complete(ctx context.Context, messages []llm.Message, tools []llm.ToolDef) (llm.Response, error) {
	return llm.Response{Message: llm.Message{Role: llm.RoleAssistant, Content: "ok"}}, nil
}

func TestModel_View_ShowsStatusBarWithModelAndMode(t *testing.T) {
	loop := agent.NewLoop(stubProvider{}, nil, "you are harper")
	cfg := config.Config{
		Brain: config.ModelConfig{Provider: "ollama", Model: "qwen3-coder:30b"},
		Mode:  "simple",
	}
	m := newModel(context.Background(), loop, cfg, func(config.ModelConfig, config.Config) (llm.Provider, error) {
		return stubProvider{}, nil
	})
	m.width, m.height = 80, 24

	view := m.View()
	if !strings.Contains(view, "qwen3-coder:30b") {
		t.Fatalf("expected the status bar to show the brain model, got:\n%s", view)
	}
	if !strings.Contains(view, "simple") {
		t.Fatalf("expected the status bar to show the mode, got:\n%s", view)
	}
}

func TestModel_View_RendersEmptySubtaskPanelWhenNoneInFlight(t *testing.T) {
	loop := agent.NewLoop(stubProvider{}, nil, "you are harper")
	cfg := config.Config{Brain: config.ModelConfig{Provider: "ollama", Model: "m"}}
	m := newModel(context.Background(), loop, cfg, func(config.ModelConfig, config.Config) (llm.Provider, error) {
		return stubProvider{}, nil
	})
	m.width, m.height = 80, 24

	view := m.View()
	if !strings.Contains(view, "active subtasks") {
		t.Fatalf("expected the subtasks panel header even with nothing in flight, got:\n%s", view)
	}
}
