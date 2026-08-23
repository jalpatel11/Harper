package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"harper/internal/agent"
	"harper/internal/config"
	"harper/internal/llm"
	"harper/internal/tools"
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

type scriptedStubProvider struct {
	responses []llm.Response
	calls     int
}

func (p *scriptedStubProvider) Complete(ctx context.Context, messages []llm.Message, tools []llm.ToolDef) (llm.Response, error) {
	resp := p.responses[p.calls]
	p.calls++
	return resp, nil
}

type stubToolForTUI struct {
	name   string
	result string
}

func (t stubToolForTUI) Name() string                { return t.name }
func (t stubToolForTUI) Description() string         { return "stub" }
func (t stubToolForTUI) InputSchema() map[string]any { return map[string]any{"type": "object"} }
func (t stubToolForTUI) Execute(ctx context.Context, input map[string]any) (string, error) {
	return t.result, nil
}

func TestModel_Update_EnterSubmitsTurnAndRendersFinalAnswer(t *testing.T) {
	provider := &scriptedStubProvider{
		responses: []llm.Response{
			{Message: llm.Message{Role: llm.RoleAssistant, Content: "the answer is 5"}},
		},
	}
	loop := agent.NewLoop(provider, nil, "you are harper")
	cfg := config.Config{Brain: config.ModelConfig{Provider: "ollama", Model: "m"}}
	m := newModel(context.Background(), loop, cfg, nil)
	m.width, m.height = 80, 24
	// m.program is deliberately left nil: runTurn only calls
	// m.program.Send when it's non-nil (RunTUI sets it once the real
	// program is running), and a Program built here without Run()
	// consuming its message channel would deadlock on the first onStep
	// send, since Loop.Run's onStep fires for the terminal message too.

	m.textInput.SetValue("what is 2+3")
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)
	if !m.turnInFlight {
		t.Fatalf("expected turnInFlight to be true immediately after submitting")
	}
	if cmd == nil {
		t.Fatalf("expected a command to run the turn in the background")
	}

	// Execute the returned Cmd synchronously (as Bubble Tea's runtime
	// would, just without the event loop) and feed its Msg back through
	// Update, the same round trip the real program performs.
	msg := cmd()
	updated, _ = m.Update(msg)
	m = updated.(*Model)

	if m.turnInFlight {
		t.Fatalf("expected turnInFlight to be false once the turn completes")
	}
	found := false
	for _, e := range m.conversation {
		if strings.Contains(e.text, "the answer is 5") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected the final answer rendered in the conversation, got: %+v", m.conversation)
	}
}

func TestModel_Update_SecondTurnHistoryPreservesToolCallFromFirstTurn(t *testing.T) {
	// Regression test: the second turn's outgoing history must include
	// the first turn's tool_use/tool_result messages verbatim, not a
	// reconstruction from display text. A provider (real Anthropic
	// especially) needs that pairing to be structurally intact; dropping
	// it would break every real orchestrator-mode session by the second
	// prompt, since the brain's only tool is Delegate.
	provider := &scriptedStubProvider{
		responses: []llm.Response{
			{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
				{ID: "call_0", Name: "Delegate", Args: map[string]any{"task": "look something up"}},
			}}},
			{Message: llm.Message{Role: llm.RoleAssistant, Content: "first answer"}},
			{Message: llm.Message{Role: llm.RoleAssistant, Content: "second answer"}},
		},
	}
	stub := stubToolForTUI{name: "Delegate", result: "looked it up"}
	loop := agent.NewLoop(provider, []tools.Tool{stub}, "you are harper")
	cfg := config.Config{Brain: config.ModelConfig{Provider: "ollama", Model: "m"}}
	m := newModel(context.Background(), loop, cfg, nil)
	m.width, m.height = 80, 24

	// First turn: triggers a tool call.
	m.textInput.SetValue("do something")
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)
	updated, _ = m.Update(cmd())
	m = updated.(*Model)

	var sawToolCall, sawToolResult bool
	for _, msg := range m.history {
		if msg.Role == llm.RoleAssistant && len(msg.ToolCalls) > 0 {
			sawToolCall = true
		}
		if msg.Role == llm.RoleTool && msg.Content == "looked it up" {
			sawToolResult = true
		}
	}
	if !sawToolCall || !sawToolResult {
		t.Fatalf("expected m.history to retain the first turn's tool_use/tool_result messages, got: %+v", m.history)
	}

	// Second turn: the outgoing history (captured by runTurn before the
	// provider responds) must include everything from the first turn.
	m.textInput.SetValue("do something else")
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)
	if len(m.history) < 4 {
		t.Fatalf("expected the second turn's history to build on the first turn's (>= 4 messages: user, assistant+toolcall, tool, user), got %d", len(m.history))
	}
}

func TestModel_Update_ModelCommandWithArgSwitchesProviderWithoutPrompting(t *testing.T) {
	var capturedModel string
	loop := agent.NewLoop(&scriptedStubProvider{}, nil, "you are harper")
	cfg := config.Config{Brain: config.ModelConfig{Provider: "ollama", Model: "old-model"}}
	m := newModel(context.Background(), loop, cfg, func(mc config.ModelConfig, _ config.Config) (llm.Provider, error) {
		capturedModel = mc.Model
		return &scriptedStubProvider{}, nil
	})
	m.width, m.height = 80, 24

	m.textInput.SetValue("/model some-new-model")
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)

	if capturedModel != "some-new-model" {
		t.Fatalf("expected buildProvider called with the new model, got %q", capturedModel)
	}
	found := false
	for _, e := range m.conversation {
		if strings.Contains(e.text, "model set to") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a confirmation message in the conversation, got: %+v", m.conversation)
	}
}

func TestModel_Update_SubtaskStepMsgAddsAndUpdatesCard(t *testing.T) {
	loop := agent.NewLoop(&scriptedStubProvider{}, nil, "you are harper")
	cfg := config.Config{Brain: config.ModelConfig{Provider: "ollama", Model: "m"}}
	m := newModel(context.Background(), loop, cfg, nil)
	m.width, m.height = 80, 24

	updated, _ := m.Update(subtaskStepMsg{toolCallID: "call_0", message: llm.Message{Content: "reading file"}})
	m = updated.(*Model)

	if len(m.subtasks) != 1 {
		t.Fatalf("expected 1 active subtask card, got %d", len(m.subtasks))
	}
	if m.subtasks["call_0"].lastStep != "reading file" {
		t.Fatalf("unexpected lastStep: %q", m.subtasks["call_0"].lastStep)
	}

	updated, _ = m.Update(subtaskDoneMsg{toolCallID: "call_0"})
	m = updated.(*Model)
	if len(m.subtasks) != 0 {
		t.Fatalf("expected the card removed once its Delegate call finishes, got %d remaining", len(m.subtasks))
	}
}
