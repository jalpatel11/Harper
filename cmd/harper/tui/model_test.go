package tui

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
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

// blockingStubProvider never returns until release is closed, so callers
// can hold a turn "in flight" for as long as a test needs — used to build
// scenarios that exercise the concurrent /model-during-a-turn path (Bug 1).
type blockingStubProvider struct {
	release chan struct{}
}

func (p *blockingStubProvider) Complete(ctx context.Context, messages []llm.Message, tools []llm.ToolDef) (llm.Response, error) {
	<-p.release
	return llm.Response{Message: llm.Message{Role: llm.RoleAssistant, Content: "done"}}, nil
}

// TestModel_Update_ModelCommandBlockedWhileTurnInFlight is the regression
// test for Bug 1: /model (both the direct-arg form here, and the
// model-pick form in the sibling test below) must be refused while a turn
// is in flight, rather than reaching applyModelChoice and racing
// Loop.SetProvider against the background loop.Run goroutine's read of
// loop.provider.
func TestModel_Update_ModelCommandBlockedWhileTurnInFlight(t *testing.T) {
	provider := &blockingStubProvider{release: make(chan struct{})}
	loop := agent.NewLoop(provider, nil, "you are harper")
	cfg := config.Config{Brain: config.ModelConfig{Provider: "ollama", Model: "old-model"}}
	buildProviderCalled := false
	m := newModel(context.Background(), loop, cfg, func(config.ModelConfig, config.Config) (llm.Provider, error) {
		buildProviderCalled = true
		t.Errorf("buildProvider must not be called while a turn is in flight")
		return nil, nil
	})
	m.width, m.height = 80, 24

	// Start a turn and leave it in flight: don't execute the returned Cmd,
	// which is what would normally deliver turnDoneMsg and clear
	// turnInFlight. This mirrors TestModel_Update_EnterSubmitsTurnAndRendersFinalAnswer
	// up to the point where the turn is submitted, but stops there.
	m.textInput.SetValue("what is 2+3")
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)
	if !m.turnInFlight {
		t.Fatalf("expected turnInFlight to be true after submitting")
	}
	if cmd == nil {
		t.Fatalf("expected a command to run the turn in the background")
	}

	// Now attempt /model while the turn is still in flight.
	m.textInput.SetValue("/model some-model")
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)

	if buildProviderCalled {
		t.Fatalf("expected buildProvider not to be called while a turn is in flight")
	}
	if m.cfg.Brain.Model != "old-model" {
		t.Fatalf("expected the model to remain unchanged, got %q", m.cfg.Brain.Model)
	}
	found := false
	for _, e := range m.conversation {
		if e.kind == "error" && strings.Contains(e.text, "in flight") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected an explanatory error message in the conversation, got: %+v", m.conversation)
	}

	// Release the blocked provider so the background goroutine can finish
	// (avoids leaking a goroutine past the end of the test).
	close(provider.release)
	_ = cmd()
}

// TestModel_Update_ModelPickBlockedWhileTurnInFlight covers the
// "model-pick" branch of resolvePendingPrompt directly (Bug 1's second
// entry point into applyModelChoice), by driving m.pendingPrompt/
// m.turnInFlight into that state without going through a real Update
// dispatch race (Update itself is single-threaded; the race being guarded
// against is between Update's goroutine and runTurn's background
// goroutine inside loop.Run, which is exercised separately below under
// -race).
func TestModel_Update_ModelPickBlockedWhileTurnInFlight(t *testing.T) {
	loop := agent.NewLoop(&scriptedStubProvider{}, nil, "you are harper")
	cfg := config.Config{Brain: config.ModelConfig{Provider: "ollama", Model: "old-model"}}
	m := newModel(context.Background(), loop, cfg, func(config.ModelConfig, config.Config) (llm.Provider, error) {
		t.Errorf("buildProvider must not be called while a turn is in flight")
		return nil, nil
	})
	m.width, m.height = 80, 24

	m.turnInFlight = true
	m.pendingPrompt = &pendingPrompt{kind: "model-pick", modelPickOptions: []string{"model-a", "model-b"}}

	m.textInput.SetValue("1")
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)

	if m.cfg.Brain.Model != "old-model" {
		t.Fatalf("expected the model to remain unchanged, got %q", m.cfg.Brain.Model)
	}
	found := false
	for _, e := range m.conversation {
		if e.kind == "error" && strings.Contains(e.text, "in flight") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected an explanatory error message in the conversation, got: %+v", m.conversation)
	}
}

// TestModel_Race_ModelCommandDuringInFlightTurn exercises the actual
// concurrent scenario Bug 1 was found under -race: a background goroutine
// inside loop.Run reading loop.provider (via Complete) while the main
// goroutine handles a /model keystroke that, before the fix, called
// loop.SetProvider unconditionally. Run with `go test -race` to confirm
// no race is reported.
func TestModel_Race_ModelCommandDuringInFlightTurn(t *testing.T) {
	release := make(chan struct{})
	provider := &blockingStubProvider{release: release}
	loop := agent.NewLoop(provider, nil, "you are harper")
	cfg := config.Config{Brain: config.ModelConfig{Provider: "ollama", Model: "old-model"}}
	m := newModel(context.Background(), loop, cfg, func(config.ModelConfig, config.Config) (llm.Provider, error) {
		return &scriptedStubProvider{}, nil
	})
	m.width, m.height = 80, 24

	m.textInput.SetValue("what is 2+3")
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		// This is the goroutine that concurrently reads loop.provider
		// inside loop.Run (via Complete).
		_ = cmd()
	}()

	// Give the background goroutine a moment to actually enter loop.Run
	// before firing /model, so the two genuinely overlap.
	time.Sleep(10 * time.Millisecond)
	m.textInput.SetValue("/model some-model")
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)

	close(release)
	wg.Wait()
}

// TestModel_Update_ConversationPanelTrimsToFitAvailableHeight is the
// regression test for Bug 2: renderConversation must tail-trim so the
// rendered conversation panel never grows past the available height,
// keeping the status bar and subtask panel on-screen, and the entries
// that remain visible must be the most recent ones, not the oldest.
func TestModel_Update_ConversationPanelTrimsToFitAvailableHeight(t *testing.T) {
	loop := agent.NewLoop(&scriptedStubProvider{}, nil, "you are harper")
	cfg := config.Config{Brain: config.ModelConfig{Provider: "ollama", Model: "m"}}
	m := newModel(context.Background(), loop, cfg, nil)
	m.width, m.height = 80, 10 // small height to force trimming

	for i := 0; i < 30; i++ {
		m.conversation = append(m.conversation, conversationEntry{
			kind: "assistant",
			text: "entry " + strconv.Itoa(i),
		})
	}

	rendered := m.renderConversation()
	lines := strings.Split(rendered, "\n")
	budget := m.conversationLineBudget()
	if len(lines) > budget {
		t.Fatalf("expected at most %d rendered lines, got %d:\n%s", budget, len(lines), rendered)
	}

	if !strings.Contains(rendered, "entry 29") {
		t.Fatalf("expected the most recent entry (29) to be visible, got:\n%s", rendered)
	}
	if strings.Contains(rendered, "entry 0\n") {
		t.Fatalf("expected the oldest entry (0) to have been trimmed away, got:\n%s", rendered)
	}
}

// TestModel_Update_SpinnerTickMsgKeepsTickingAndUpdatesModel is the
// regression test for Bug 3: a spinner.TickMsg must be handled by
// m.spin.Update (not fall through to the generic textInput.Update, which
// discards it and returns a nil Cmd), so the tick chain that keeps the
// spinner animating never dies after the first frame.
func TestModel_Update_SpinnerTickMsgKeepsTickingAndUpdatesModel(t *testing.T) {
	loop := agent.NewLoop(&scriptedStubProvider{}, nil, "you are harper")
	cfg := config.Config{Brain: config.ModelConfig{Provider: "ollama", Model: "m"}}
	m := newModel(context.Background(), loop, cfg, nil)
	m.width, m.height = 80, 24

	// Obtain a real spinner.TickMsg the way Bubble Tea would deliver one,
	// by invoking the Cmd returned from Init/Tick.
	tickCmd := m.spin.Tick
	msg := tickCmd()
	tickMsg, ok := msg.(spinner.TickMsg)
	if !ok {
		t.Fatalf("expected spinner.Tick to produce a spinner.TickMsg, got %T", msg)
	}

	updated, cmd := m.Update(tickMsg)
	m = updated.(*Model)
	_ = m
	if cmd == nil {
		t.Fatalf("expected a non-nil Cmd after handling spinner.TickMsg, proving the tick loop continues")
	}
}

// TestModel_Update_ToolStepMsgWithDelegateCallPopulatesSubtaskCardTask is
// the regression test for Bug 4: the assistant message announcing a
// Delegate tool call (which arrives via toolStepMsg, not subtaskStepMsg)
// must pre-create the subtask card with its task description populated,
// rather than being silently discarded because toolStepMsg's handler only
// acted on llm.RoleTool messages.
func TestModel_Update_ToolStepMsgWithDelegateCallPopulatesSubtaskCardTask(t *testing.T) {
	loop := agent.NewLoop(&scriptedStubProvider{}, nil, "you are harper")
	cfg := config.Config{Brain: config.ModelConfig{Provider: "ollama", Model: "m"}}
	m := newModel(context.Background(), loop, cfg, nil)
	m.width, m.height = 80, 24

	assistantMsg := llm.Message{
		Role: llm.RoleAssistant,
		ToolCalls: []llm.ToolCall{
			{ID: "call_x", Name: "Delegate", Args: map[string]any{"task": "do the thing"}},
		},
	}
	updated, _ := m.Update(toolStepMsg{message: assistantMsg})
	m = updated.(*Model)

	card, ok := m.subtasks["call_x"]
	if !ok {
		t.Fatalf("expected a subtask card to be pre-created for call_x, got subtasks: %+v", m.subtasks)
	}
	if card.task != "do the thing" {
		t.Fatalf("expected card.task to be %q, got %q", "do the thing", card.task)
	}
}
