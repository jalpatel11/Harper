package tui

import (
	"context"
	"fmt"
	"strconv"
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

// pendingPrompt marks the TUI as waiting on a special answer to the next
// submitted line, rather than a new agent turn — used by both the
// permission bridge (this task) and /model's no-arg picker (Task 7), so
// there is exactly one mechanism for "modal, not a normal turn."
type pendingPrompt struct {
	kind              string // "permission" or "model-pick"
	permissionRespond chan permissionResponse
	modelPickRespond  chan string
	modelPickOptions  []string
}

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
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "enter":
			input := strings.TrimSpace(m.textInput.Value())
			if m.pendingPrompt != nil {
				m.textInput.Reset()
				return m.resolvePendingPrompt(input)
			}
			if strings.HasPrefix(input, "/") {
				m.textInput.Reset()
				if m.turnInFlight {
					m.conversation = append(m.conversation, conversationEntry{kind: "error", text: "cannot change model while a turn is in flight"})
					return m, nil
				}
				return m.handleSlashCommand(input)
			}
			if m.turnInFlight || input == "" {
				return m, nil
			}
			m.textInput.Reset()
			m.conversation = append(m.conversation, conversationEntry{kind: "user", text: input})
			m.history = append(m.history, llm.Message{Role: llm.RoleUser, Content: input})
			m.turnInFlight = true
			return m, m.runTurn()
		}

	case turnDoneMsg:
		m.turnInFlight = false
		if msg.err != nil {
			m.conversation = append(m.conversation, conversationEntry{kind: "error", text: fmt.Sprintf("error: %v", msg.err)})
			return m, nil
		}
		// Carry the exact returned history forward, the same way RunREPL
		// does (`history = updated`) — this is what preserves Delegate's
		// tool_use/tool_result message pairs for the next turn. Rebuilding
		// history from the display log instead would silently drop them.
		m.history = msg.history
		final := msg.history[len(msg.history)-1]
		m.conversation = append(m.conversation, conversationEntry{kind: "assistant", text: final.Content})
		return m, nil

	case toolStepMsg:
		if msg.message.Role == llm.RoleTool {
			m.conversation = append(m.conversation, conversationEntry{kind: "tool", text: msg.message.Content})
		} else if msg.message.Role == llm.RoleAssistant {
			for _, call := range msg.message.ToolCalls {
				if call.Name != "Delegate" {
					continue
				}
				task, ok := call.Args["task"].(string)
				if !ok {
					continue
				}
				card, ok := m.subtasks[call.ID]
				if !ok {
					card = &subtaskCard{toolCallID: call.ID}
					m.subtasks[call.ID] = card
				}
				card.task = task
			}
		}
		return m, nil

	case subtaskStepMsg:
		card, ok := m.subtasks[msg.toolCallID]
		if !ok {
			card = &subtaskCard{toolCallID: msg.toolCallID}
			m.subtasks[msg.toolCallID] = card
		}
		if msg.message.Content != "" {
			card.lastStep = msg.message.Content
		} else if len(msg.message.ToolCalls) > 0 {
			card.lastStep = msg.message.ToolCalls[0].Name
		}
		return m, nil

	case subtaskDoneMsg:
		delete(m.subtasks, msg.toolCallID)
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd

	case permissionRequestMsg:
		m.pendingPrompt = &pendingPrompt{kind: "permission", permissionRespond: msg.respond}
		m.conversation = append(m.conversation, conversationEntry{
			kind: "assistant",
			text: fmt.Sprintf("[permission] %s wants to run: %v\nallow once / allow for session / deny? [o/s/d]", msg.toolName, msg.input),
		})
		return m, nil
	}

	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

// runTurn captures m.history (already updated with the new user message by
// the "enter" case in Update) at the moment it's called, then runs
// loop.Run over that snapshot in the background.
func (m *Model) runTurn() tea.Cmd {
	history := m.history
	return func() tea.Msg {
		ctx := agent.WithSubtaskReporter(m.ctx, func(toolCallID string, step llm.Message) {
			if m.program != nil {
				m.program.Send(subtaskStepMsg{toolCallID: toolCallID, message: step})
			}
		})
		updated, err := m.loop.Run(ctx, history, 30, func(step llm.Message) {
			if m.program != nil {
				m.program.Send(toolStepMsg{message: step})
				if step.Role == llm.RoleTool {
					// This top-level tool result is the Delegate call
					// finishing (Delegate is the brain's only tool in
					// orchestrator mode) — clear its card. In simple mode
					// there is no Delegate/subtask panel content to clear,
					// so this is a harmless no-op delete-of-nothing.
					m.program.Send(subtaskDoneMsg{toolCallID: step.ToolCallID})
				}
			}
		})
		return turnDoneMsg{history: updated, err: err}
	}
}

// resolvePendingPrompt answers whatever modal prompt is currently pending
// (a permission request or, since Task 7, "model-pick") with the user's
// just-submitted input line, then clears m.pendingPrompt so "enter" resumes
// its normal turn-submission behavior.
func (m *Model) resolvePendingPrompt(input string) (tea.Model, tea.Cmd) {
	p := m.pendingPrompt
	m.pendingPrompt = nil

	switch p.kind {
	case "permission":
		switch strings.ToLower(input) {
		case "s":
			p.permissionRespond <- permissionResponse{allowed: true, persist: true}
		case "o", "":
			p.permissionRespond <- permissionResponse{allowed: true, persist: false}
		default:
			p.permissionRespond <- permissionResponse{allowed: false, persist: false}
		}
	case "model-pick":
		if m.turnInFlight {
			m.conversation = append(m.conversation, conversationEntry{kind: "error", text: "cannot change model while a turn is in flight"})
			return m, nil
		}
		chosen := input
		if n, err := strconv.Atoi(input); err == nil && n >= 1 && n <= len(p.modelPickOptions) {
			chosen = p.modelPickOptions[n-1]
		}
		if chosen == "" {
			return m, nil
		}
		return m, m.applyModelChoice(chosen)
	}
	return m, nil
}

func (m *Model) handleSlashCommand(line string) (tea.Model, tea.Cmd) {
	fields := strings.Fields(line)
	switch fields[0] {
	case "/model":
		return m.handleModelCommand(fields[1:])
	default:
		m.conversation = append(m.conversation, conversationEntry{kind: "error", text: "unknown command: " + fields[0]})
		return m, nil
	}
}

// handleModelCommand mirrors cmd/harper/repl.go's handleModelCommand,
// adapted for Bubble Tea: the no-argument interactive path uses the
// pendingPrompt/modelPickRequestMsg bridge instead of scanner.Scan(), since
// Update cannot block. Some duplication with the plain REPL's version is
// deliberate — the two are separate, independently-testable front ends
// over different I/O models (see the design doc's package-separation
// rationale), not a DRY violation to fix.
func (m *Model) handleModelCommand(args []string) (tea.Model, tea.Cmd) {
	if len(args) > 0 {
		return m, m.applyModelChoice(args[0])
	}

	var options []string
	if m.cfg.Brain.Provider == "ollama" || m.cfg.Brain.Provider == "" {
		models, err := llm.ListOllamaModels(m.cfg.Ollama.BaseURL)
		if err != nil {
			m.conversation = append(m.conversation, conversationEntry{kind: "error", text: fmt.Sprintf("list ollama models: %v", err)})
			return m, nil
		}
		options = models
	}

	var b strings.Builder
	if len(options) > 0 {
		b.WriteString("Available Ollama models:\n")
		for i, opt := range options {
			fmt.Fprintf(&b, "  %d) %s\n", i+1, opt)
		}
	}
	b.WriteString("Pick a number, or type a model name:")
	m.conversation = append(m.conversation, conversationEntry{kind: "assistant", text: b.String()})
	m.pendingPrompt = &pendingPrompt{kind: "model-pick", modelPickOptions: options}
	return m, nil
}

func (m *Model) applyModelChoice(chosen string) tea.Cmd {
	m.cfg.Brain.Model = chosen
	newProvider, err := m.buildProvider(m.cfg.Brain, m.cfg)
	if err != nil {
		m.conversation = append(m.conversation, conversationEntry{kind: "error", text: err.Error()})
		return nil
	}
	m.loop.SetProvider(newProvider)
	m.conversation = append(m.conversation, conversationEntry{kind: "assistant", text: fmt.Sprintf("model set to %q", chosen)})
	return nil
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

// renderConversation renders the conversation log, tail-trimmed to fit the
// panel's available height. lipgloss.Style.Height sets a minimum, not a
// maximum, so without trimming here the rendered view grows without bound
// as turns accumulate, and Bubble Tea's renderer eventually scrolls the
// status bar and subtask panel off the top of the terminal. Keeping only
// the most recent lines that fit preserves the persistent, stable layout
// the design calls for; a full scrollback/viewport is out of scope here.
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
	return tailLines(b.String(), m.conversationLineBudget())
}

// conversationLineBudget returns how many lines of rendered conversation
// content fit in the panel. The panel's inner content area is
// Height(m.height - 4) (see View), of which one line is the "conversation"
// header renderConversation's caller prepends, leaving m.height - 5 for
// the conversation body itself.
func (m *Model) conversationLineBudget() int {
	budget := m.height - 5
	if budget < 1 {
		budget = 1
	}
	return budget
}

// tailLines keeps at most the last n lines of s, dropping the oldest ones.
func tailLines(s string, n int) string {
	if s == "" {
		return s
	}
	trimmed := strings.TrimSuffix(s, "\n")
	lines := strings.Split(trimmed, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n") + "\n"
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
	loop.SetPermissionChecker(newTUIPermissionChecker(cfg.Permissions, p))
	_, err := p.Run()
	return err
}
