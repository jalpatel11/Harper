package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"harper/internal/agent"
	"harper/internal/config"
	"harper/internal/llm"
)

// conversationEntry is one rendered line in the conversation panel: a user
// prompt, an assistant answer, a tool result, or an error.
type conversationEntry struct {
	kind string // "user", "assistant", "tool", "error"
	text string
}

// subtaskCard is one live-updating entry in the active-subtasks panel,
// keyed by the Delegate tool call's ID.
type subtaskCard struct {
	toolCallID string
	task       string
	lastStep   string
}

// pendingPrompt holds a permission or confirmation prompt awaiting the
// user's response. Empty for now — a later task populates it when wiring up
// permission prompts; it exists here only so Model's field compiles.
type pendingPrompt struct{}

type Model struct {
	ctx           context.Context
	loop          *agent.Loop
	cfg           config.Config
	buildProvider func(config.ModelConfig, config.Config) (llm.Provider, error)
	program       *tea.Program

	width, height int

	textInput textinput.Model
	spin      spinner.Model

	conversation []conversationEntry // display log — rendering only, never sent to a provider
	history      []llm.Message       // the real, authoritative message history sent to loop.Run, carried forward exactly as loop.Run returns it — the same pattern RunREPL uses (`history = updated`), so tool_use/tool_result pairs from Delegate calls are never dropped
	subtasks     map[string]*subtaskCard
	turnInFlight bool

	pendingPrompt *pendingPrompt
}

func newModel(ctx context.Context, loop *agent.Loop, cfg config.Config, buildProvider func(config.ModelConfig, config.Config) (llm.Provider, error)) *Model {
	ti := textinput.New()
	ti.Focus()
	ti.Prompt = "> "

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = spinnerStyle

	return &Model{
		ctx:           ctx,
		loop:          loop,
		cfg:           cfg,
		buildProvider: buildProvider,
		textInput:     ti,
		spin:          sp,
		subtasks:      make(map[string]*subtaskCard),
	}
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, m.spin.Tick)
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
	}

	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

func (m *Model) View() string {
	status := statusBarStyle.Width(m.width).Render(fmt.Sprintf(
		"harper  brain: %s  mode: %s  permissions: %s",
		m.cfg.Brain.Model, modeLabel(m.cfg.Mode), permissionsLabel(m.cfg.Permissions),
	))

	conversation := panelBorderStyle.
		Width(m.width*2/3 - 2).
		Height(m.height - 4).
		Render("conversation\n" + m.renderConversation())

	subtasks := panelBorderStyle.
		Width(m.width/3 - 2).
		Height(m.height - 4).
		Render("active subtasks\n" + m.renderSubtasks())

	body := lipgloss.JoinHorizontal(lipgloss.Top, conversation, subtasks)

	return lipgloss.JoinVertical(lipgloss.Left, status, body, m.textInput.View())
}

func (m *Model) renderConversation() string {
	var b strings.Builder
	for _, e := range m.conversation {
		switch e.kind {
		case "user":
			b.WriteString(userInputStyle.Render("> "+e.text) + "\n")
		case "error":
			b.WriteString(errorStyle.Render(e.text) + "\n")
		default:
			b.WriteString(e.text + "\n")
		}
	}
	return b.String()
}

func (m *Model) renderSubtasks() string {
	if len(m.subtasks) == 0 {
		return mutedStyle.Render("(none in flight)")
	}
	var b strings.Builder
	for _, card := range m.subtasks {
		b.WriteString(fmt.Sprintf("%s %s\n   %s\n", m.spin.View(), card.task, card.lastStep))
	}
	return b.String()
}

func modeLabel(mode string) string {
	if mode == "" {
		return "orchestrator"
	}
	return mode
}

func permissionsLabel(p config.PermissionsConfig) string {
	if p.Default == "" && len(p.Overrides) == 0 {
		return "allow"
	}
	if p.Default != "" {
		return p.Default
	}
	return "custom"
}

// RunTUI is the TUI's entry point, called from main.go in place of RunREPL
// when both stdin and stdout are real terminals. buildProvider is passed
// in rather than imported, since cmd/harper/tui cannot import package main.
func RunTUI(ctx context.Context, loop *agent.Loop, cfg config.Config, buildProvider func(config.ModelConfig, config.Config) (llm.Provider, error)) error {
	m := newModel(ctx, loop, cfg, buildProvider)
	p := tea.NewProgram(m)
	m.program = p
	_, err := p.Run()
	return err
}
