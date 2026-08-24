package tui

import (
	"context"
	"fmt"
	"os"
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
	"harper/internal/session"
	"harper/internal/tools"
)

// TestMain isolates every test in this package from a real user's
// ~/.harper/sessions directory — Update's turnDoneMsg handling now calls
// session.Save unconditionally (see saveSession), so any test that drives
// a turn through Update would otherwise write there.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "harper-tui-session-test-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	session.SetSessionsRoot(dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

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
// entered, if non-nil, is closed the first time Complete is called, letting
// a test wait for the background goroutine to actually be inside loop.Run
// instead of guessing with a fixed sleep.
type blockingStubProvider struct {
	release chan struct{}
	entered chan struct{}
}

func (p *blockingStubProvider) Complete(ctx context.Context, messages []llm.Message, tools []llm.ToolDef) (llm.Response, error) {
	if p.entered != nil {
		close(p.entered)
	}
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
	entered := make(chan struct{})
	provider := &blockingStubProvider{release: release, entered: entered}
	loop := agent.NewLoop(provider, nil, "you are harper")
	cfg := config.Config{Brain: config.ModelConfig{Provider: "ollama", Model: "old-model"}}
	buildProviderCalled := false
	m := newModel(context.Background(), loop, cfg, func(config.ModelConfig, config.Config) (llm.Provider, error) {
		buildProviderCalled = true
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

	// Wait for the background goroutine to actually be inside loop.Run
	// (signaled by Complete being entered) before firing /model, so the two
	// genuinely overlap — a fixed sleep would only guess at this timing.
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatalf("background turn never entered Complete")
	}
	m.textInput.SetValue("/model some-model")
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)

	if buildProviderCalled {
		t.Fatalf("expected buildProvider not to be called while a turn is in flight")
	}
	if m.cfg.Brain.Model != "old-model" {
		t.Fatalf("expected the model to remain unchanged while the turn was still in flight, got %q", m.cfg.Brain.Model)
	}

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
// TestModel_Update_PermissionPromptRespondsToRawKeypress covers the o/s/d
// keys resolving a pending permission prompt directly, without requiring
// the user to type a letter into textInput and press Enter.
func TestModel_Update_PermissionPromptRespondsToRawKeypress(t *testing.T) {
	tests := []struct {
		key         string
		wantAllowed bool
		wantPersist bool
	}{
		{key: "o", wantAllowed: true, wantPersist: false},
		{key: "s", wantAllowed: true, wantPersist: true},
		{key: "d", wantAllowed: false, wantPersist: false},
		{key: "enter", wantAllowed: true, wantPersist: false},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			loop := agent.NewLoop(&scriptedStubProvider{}, nil, "you are harper")
			cfg := config.Config{Brain: config.ModelConfig{Provider: "ollama", Model: "m"}}
			m := newModel(context.Background(), loop, cfg, nil)
			m.width, m.height = 80, 24

			respond := make(chan permissionResponse, 1)
			m.pendingPrompt = &pendingPrompt{kind: "permission", permissionRespond: respond}

			var keyMsg tea.KeyMsg
			if tt.key == "enter" {
				keyMsg = tea.KeyMsg{Type: tea.KeyEnter}
			} else {
				keyMsg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(tt.key)}
			}
			updated, _ := m.Update(keyMsg)
			m = updated.(*Model)

			if m.pendingPrompt != nil {
				t.Fatalf("expected pendingPrompt cleared after a single keypress, got %+v", m.pendingPrompt)
			}
			select {
			case resp := <-respond:
				if resp.allowed != tt.wantAllowed || resp.persist != tt.wantPersist {
					t.Fatalf("key %q: expected allowed=%v persist=%v, got %+v", tt.key, tt.wantAllowed, tt.wantPersist, resp)
				}
			default:
				t.Fatalf("key %q: expected a response sent on the channel", tt.key)
			}
		})
	}
}

// TestModel_Update_PermissionPromptIgnoresUnrelatedKeys is the regression
// test for the raw-keypress behavior not overreacting: a key that isn't
// o/s/d/enter/ctrl+c must be ignored, leaving the prompt pending and
// textInput untouched (so stray keystrokes can't leak into the next turn).
func TestModel_Update_PermissionPromptIgnoresUnrelatedKeys(t *testing.T) {
	loop := agent.NewLoop(&scriptedStubProvider{}, nil, "you are harper")
	cfg := config.Config{Brain: config.ModelConfig{Provider: "ollama", Model: "m"}}
	m := newModel(context.Background(), loop, cfg, nil)
	m.width, m.height = 80, 24

	respond := make(chan permissionResponse, 1)
	m.pendingPrompt = &pendingPrompt{kind: "permission", permissionRespond: respond}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	m = updated.(*Model)

	if m.pendingPrompt == nil {
		t.Fatalf("expected pendingPrompt to remain set after an unrelated key")
	}
	select {
	case resp := <-respond:
		t.Fatalf("expected no response sent for an unrelated key, got %+v", resp)
	default:
	}
}

// TestModel_Update_ExitCommandQuits covers /exit, including while a turn is
// in flight — quitting must never be blocked by the turnInFlight guard that
// blocks other slash commands.
func TestModel_Update_ExitCommandQuits(t *testing.T) {
	provider := &blockingStubProvider{release: make(chan struct{})}
	loop := agent.NewLoop(provider, nil, "you are harper")
	cfg := config.Config{Brain: config.ModelConfig{Provider: "ollama", Model: "m"}}
	m := newModel(context.Background(), loop, cfg, nil)
	m.width, m.height = 80, 24

	m.textInput.SetValue("what is 2+3")
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)
	if !m.turnInFlight {
		t.Fatalf("expected turnInFlight to be true after submitting")
	}

	m.textInput.SetValue("/exit")
	_, quitCmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if quitCmd == nil {
		t.Fatalf("expected /exit to return a non-nil Cmd even while a turn is in flight")
	}
	msg := quitCmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Fatalf("expected /exit to produce a tea.QuitMsg, got %T", msg)
	}

	close(provider.release)
	_ = cmd()
}

// TestModel_Update_CtrlDQuits covers Ctrl+D as an alternate quit key,
// alongside the existing Ctrl+C.
func TestModel_Update_CtrlDQuits(t *testing.T) {
	loop := agent.NewLoop(&scriptedStubProvider{}, nil, "you are harper")
	cfg := config.Config{Brain: config.ModelConfig{Provider: "ollama", Model: "m"}}
	m := newModel(context.Background(), loop, cfg, nil)
	m.width, m.height = 80, 24

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	if cmd == nil {
		t.Fatalf("expected ctrl+d to return a non-nil Cmd")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Fatalf("expected ctrl+d to produce a tea.QuitMsg, got %T", msg)
	}
}

// TestModel_Update_BlockedSlashCommandMessageIsGeneric covers the fix for a
// misleading error: the message shown when a slash command is blocked by
// turnInFlight must not claim specifically to be about changing the model
// (it blocks every slash command, not just /model).
func TestModel_Update_BlockedSlashCommandMessageIsGeneric(t *testing.T) {
	provider := &blockingStubProvider{release: make(chan struct{})}
	loop := agent.NewLoop(provider, nil, "you are harper")
	cfg := config.Config{Brain: config.ModelConfig{Provider: "ollama", Model: "m"}}
	m := newModel(context.Background(), loop, cfg, nil)
	m.width, m.height = 80, 24

	m.textInput.SetValue("go")
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)

	m.textInput.SetValue("/model something")
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)

	found := false
	for _, e := range m.conversation {
		if e.kind == "error" && strings.Contains(e.text, "in flight") && !strings.Contains(e.text, "change the model") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a generic (non-model-specific) blocked-command message, got: %+v", m.conversation)
	}

	close(provider.release)
	_ = cmd()
}

// TestModel_Update_NormalMessageBlockedWhileTurnInFlightGivesFeedback is the
// regression test for the silent-drop bug: submitting a normal (non-slash)
// message while a turn is already running must not vanish with no trace —
// it should append visible feedback instead of doing nothing.
func TestModel_Update_NormalMessageBlockedWhileTurnInFlightGivesFeedback(t *testing.T) {
	provider := &blockingStubProvider{release: make(chan struct{})}
	loop := agent.NewLoop(provider, nil, "you are harper")
	cfg := config.Config{Brain: config.ModelConfig{Provider: "ollama", Model: "m"}}
	m := newModel(context.Background(), loop, cfg, nil)
	m.width, m.height = 80, 24

	m.textInput.SetValue("first message")
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)

	before := len(m.conversation)
	m.textInput.SetValue("second message while busy")
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)

	if len(m.conversation) <= before {
		t.Fatalf("expected feedback appended to the conversation when a turn is blocked, got no new entries")
	}
	for _, e := range m.conversation {
		if strings.Contains(e.text, "second message while busy") {
			t.Fatalf("expected the blocked message not to be queued/submitted as a user entry, got: %+v", m.conversation)
		}
	}

	close(provider.release)
	_ = cmd()
}

// TestModel_RenderConversation_DistinguishesToolFromAssistant covers the
// fix for tool and assistant entries rendering identically — a "tool" kind
// entry must not look the same as the assistant's own answer text.
func TestModel_RenderConversation_DistinguishesToolFromAssistant(t *testing.T) {
	loop := agent.NewLoop(&scriptedStubProvider{}, nil, "you are harper")
	cfg := config.Config{Brain: config.ModelConfig{Provider: "ollama", Model: "m"}}
	m := newModel(context.Background(), loop, cfg, nil)
	m.width, m.height = 80, 24

	m.conversation = []conversationEntry{
		{kind: "tool", text: "ran a command"},
		{kind: "assistant", text: "the final answer"},
	}

	rendered := m.renderConversation()
	toolLine := ""
	assistantLine := ""
	for _, line := range strings.Split(rendered, "\n") {
		if strings.Contains(line, "ran a command") {
			toolLine = line
		}
		if strings.Contains(line, "the final answer") {
			assistantLine = line
		}
	}
	if toolLine == "" || assistantLine == "" {
		t.Fatalf("expected both entries rendered, got:\n%s", rendered)
	}
	if toolLine == "ran a command" {
		t.Fatalf("expected the tool entry to be visually distinguished (not rendered as bare text), got: %q", toolLine)
	}
}

// TestModel_RenderSubtasks_PreservesInsertionOrder is the regression test
// for cards reordering on screen: map iteration order is randomized, so
// renderSubtasks must walk a stable, insertion-ordered list of IDs rather
// than ranging the map directly. Runs enough cards that a map-order bug
// would show up as flakiness across repeated runs.
func TestModel_RenderSubtasks_PreservesInsertionOrder(t *testing.T) {
	loop := agent.NewLoop(&scriptedStubProvider{}, nil, "you are harper")
	cfg := config.Config{Brain: config.ModelConfig{Provider: "ollama", Model: "m"}}
	m := newModel(context.Background(), loop, cfg, nil)
	m.width, m.height = 80, 24

	ids := []string{"call_a", "call_b", "call_c", "call_d", "call_e"}
	for _, id := range ids {
		updated, _ := m.Update(subtaskStepMsg{toolCallID: id, message: llm.Message{Content: "step for " + id}})
		m = updated.(*Model)
	}

	rendered := m.renderSubtasks()
	lastIndex := -1
	for _, id := range ids {
		idx := strings.Index(rendered, "step for "+id)
		if idx == -1 {
			t.Fatalf("expected %q rendered, got:\n%s", id, rendered)
		}
		if idx < lastIndex {
			t.Fatalf("expected cards in insertion order, %q rendered out of order:\n%s", id, rendered)
		}
		lastIndex = idx
	}
}

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

func TestConversationEntriesFromHistory_ReconstructsDisplayableEntries(t *testing.T) {
	history := []llm.Message{
		{Role: llm.RoleSystem, Content: "you are harper"},
		{Role: llm.RoleUser, Content: "do the thing"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "call_0", Name: "Delegate"}}},
		{Role: llm.RoleTool, Content: "looked it up", ToolCallID: "call_0"},
		{Role: llm.RoleAssistant, Content: "here's the answer"},
	}

	entries := conversationEntriesFromHistory(history)

	if len(entries) != 3 {
		t.Fatalf("expected 3 entries (system and empty-content assistant skipped), got %d: %+v", len(entries), entries)
	}
	if entries[0].kind != "user" || entries[0].text != "do the thing" {
		t.Fatalf("unexpected entry[0]: %+v", entries[0])
	}
	if entries[1].kind != "tool" || entries[1].text != "looked it up" {
		t.Fatalf("unexpected entry[1]: %+v", entries[1])
	}
	if entries[2].kind != "assistant" || entries[2].text != "here's the answer" {
		t.Fatalf("unexpected entry[2]: %+v", entries[2])
	}
}

func TestModel_SeedFromSession_PopulatesHistoryConversationAndNotice(t *testing.T) {
	loop := agent.NewLoop(&scriptedStubProvider{}, nil, "you are harper")
	cfg := config.Config{Brain: config.ModelConfig{Provider: "ollama", Model: "m"}}
	m := newModel(context.Background(), loop, cfg, nil)

	history := []llm.Message{
		{Role: llm.RoleUser, Content: "earlier question"},
		{Role: llm.RoleAssistant, Content: "earlier answer"},
	}
	m.seedFromSession(history, "resumed session from earlier, 2 messages")

	if len(m.history) != 2 {
		t.Fatalf("expected m.history seeded with 2 messages, got %d", len(m.history))
	}
	found := false
	for _, e := range m.conversation {
		if strings.Contains(e.text, "resumed session from earlier") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected the resume notice appended to the conversation, got: %+v", m.conversation)
	}
	if !strings.Contains(m.conversation[0].text, "earlier question") {
		t.Fatalf("expected the resumed history reconstructed into the conversation, got: %+v", m.conversation)
	}
}

func TestModel_SeedFromSession_NoNoticeWhenEmpty(t *testing.T) {
	loop := agent.NewLoop(&scriptedStubProvider{}, nil, "you are harper")
	cfg := config.Config{Brain: config.ModelConfig{Provider: "ollama", Model: "m"}}
	m := newModel(context.Background(), loop, cfg, nil)

	m.seedFromSession(nil, "")

	if len(m.conversation) != 0 {
		t.Fatalf("expected no conversation entries when there's nothing to seed and no notice, got: %+v", m.conversation)
	}
}

func TestModel_Update_TurnDoneMsg_SavesSession(t *testing.T) {
	session.SetSessionsRoot(t.TempDir())

	loop := agent.NewLoop(&scriptedStubProvider{}, nil, "you are harper")
	cfg := config.Config{Brain: config.ModelConfig{Provider: "ollama", Model: "saved-model"}, Mode: "simple"}
	m := newModel(context.Background(), loop, cfg, nil)
	m.width, m.height = 80, 24

	history := []llm.Message{
		{Role: llm.RoleUser, Content: "hi"},
		{Role: llm.RoleAssistant, Content: "hello back"},
	}
	updated, _ := m.Update(turnDoneMsg{history: history, err: nil})
	m = updated.(*Model)

	got, found, err := session.Load(".")
	if err != nil || !found {
		t.Fatalf("expected a saved session after turnDoneMsg, found=%v err=%v", found, err)
	}
	if len(got.History) != 2 || got.Brain.Model != "saved-model" || got.Mode != "simple" {
		t.Fatalf("unexpected saved session: %+v", got)
	}
}

func TestModel_Update_TurnDoneMsg_SavesSessionEvenOnError(t *testing.T) {
	// Regression test: before this task, an errored turn left m.history
	// untouched (unlike RunREPL, which always keeps the returned history —
	// see repl.go's `history = updated` on the error path too). That
	// inconsistency would mean an errored turn's tool_use/tool_result pairs
	// are silently lost from what gets persisted, breaking the "one shared
	// format" guarantee between the two front-ends.
	session.SetSessionsRoot(t.TempDir())

	loop := agent.NewLoop(&scriptedStubProvider{}, nil, "you are harper")
	cfg := config.Config{Brain: config.ModelConfig{Provider: "ollama", Model: "m"}}
	m := newModel(context.Background(), loop, cfg, nil)
	m.width, m.height = 80, 24

	history := []llm.Message{{Role: llm.RoleUser, Content: "hi"}}
	updated, _ := m.Update(turnDoneMsg{history: history, err: fmt.Errorf("boom")})
	m = updated.(*Model)

	if len(m.history) != 1 {
		t.Fatalf("expected m.history updated even when the turn errored, got %d messages", len(m.history))
	}
	_, found, err := session.Load(".")
	if err != nil || !found {
		t.Fatalf("expected a saved session even after an errored turn, found=%v err=%v", found, err)
	}
}
