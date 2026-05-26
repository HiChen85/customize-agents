package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type InputSubmitMsg struct {
	Text string
}

type InputModel struct {
	textarea textarea.Model
	disabled bool
	width    int
}

func NewInput() InputModel {
	ta := textarea.New()
	ta.Placeholder = "Send a message... (/ for commands)"
	ta.Focus()
	ta.CharLimit = 4096
	ta.SetHeight(3)
	ta.ShowLineNumbers = false
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ta.FocusedStyle.Placeholder = lipgloss.NewStyle().Foreground(ColorMuted)
	ta.Prompt = StyleUserPrefix.Render("❯ ")
	return InputModel{textarea: ta}
}

func (m *InputModel) SetWidth(w int) {
	m.width = w
	m.textarea.SetWidth(w - 2)
}

func (m *InputModel) Focus() tea.Cmd {
	return m.textarea.Focus()
}

func (m *InputModel) Blur() {
	m.textarea.Blur()
}

func (m *InputModel) SetDisabled(disabled bool) {
	m.disabled = disabled
	if disabled {
		m.textarea.Blur()
	}
}

func (m InputModel) Update(msg tea.Msg) (InputModel, tea.Cmd) {
	if m.disabled {
		return m, nil
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			text := strings.TrimSpace(m.textarea.Value())
			if text == "" {
				return m, nil
			}
			m.textarea.Reset()
			return m, func() tea.Msg { return InputSubmitMsg{Text: text} }
		}
	}

	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	return m, cmd
}

func (m InputModel) View() string {
	border := lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderTop(true).
		BorderForeground(ColorDimmed).
		Width(m.width)

	if m.disabled {
		return border.Render(StyleMuted.Render("  Agent is working..."))
	}
	return border.Render(m.textarea.View())
}
