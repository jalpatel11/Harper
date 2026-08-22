package tui

import "github.com/charmbracelet/lipgloss"

var (
	statusBarStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#2c3440")).
			Foreground(lipgloss.Color("#7dd3fc")).
			Padding(0, 1)

	panelBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.NormalBorder()).
				BorderForeground(lipgloss.Color("#9aa5b1")).
				Padding(0, 1)

	userInputStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#7dd3fc"))
	mutedStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#666666"))
	errorStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#f87171"))
	spinnerStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#f5d76e"))
)
