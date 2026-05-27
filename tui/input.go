package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type InputSubmitMsg struct {
	Text string
}

type InputModel struct {
	textarea    textarea.Model
	disabled    bool
	doneHint    bool
	width       int
	skills      []string
	showPicker  bool
	pickerIdx   int
	pickerQuery string
}

func NewInput() InputModel {
	ta := textarea.New()
	ta.Placeholder = "Send a message... (/ for commands, $ for skills)"
	ta.Focus()
	ta.CharLimit = 4096
	ta.SetHeight(1)
	ta.ShowLineNumbers = false
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ta.FocusedStyle.Placeholder = lipgloss.NewStyle().Foreground(ColorMuted)
	ta.Prompt = StyleUserPrefix.Render("❯ ")
	return InputModel{textarea: ta}
}

func (m *InputModel) SetSkills(skills []string) {
	m.skills = skills
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
	m.doneHint = false
	if disabled {
		m.textarea.Blur()
	}
}

func (m *InputModel) SetDone() {
	m.disabled = false
	m.doneHint = true
}

func (m InputModel) filteredSkills() []string {
	if m.pickerQuery == "" {
		return m.skills
	}
	var filtered []string
	for _, s := range m.skills {
		if strings.Contains(strings.ToLower(s), strings.ToLower(m.pickerQuery)) {
			filtered = append(filtered, s)
		}
	}
	return filtered
}

func (m InputModel) Update(msg tea.Msg) (InputModel, tea.Cmd) {
	if m.disabled {
		return m, nil
	}

	if m.doneHint {
		if _, ok := msg.(tea.KeyMsg); ok {
			m.doneHint = false
		}
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.showPicker {
			filtered := m.filteredSkills()
			switch msg.String() {
			case "tab", "enter":
				if len(filtered) > 0 && m.pickerIdx < len(filtered) {
					selected := filtered[m.pickerIdx]
					m.textarea.Reset()
					m.textarea.SetValue("$" + selected + " ")
					m.textarea.CursorEnd()
					m.showPicker = false
					m.pickerIdx = 0
					m.pickerQuery = ""
				}
				return m, nil
			case "up":
				if m.pickerIdx > 0 {
					m.pickerIdx--
				}
				return m, nil
			case "down":
				if m.pickerIdx < len(filtered)-1 {
					m.pickerIdx++
				}
				return m, nil
			case "esc":
				m.showPicker = false
				m.pickerIdx = 0
				m.pickerQuery = ""
				return m, nil
			default:
				var cmd tea.Cmd
				m.textarea, cmd = m.textarea.Update(msg)
				m.updatePickerState()
				return m, cmd
			}
		}

		switch msg.String() {
		case "enter":
			text := strings.TrimSpace(m.textarea.Value())
			if text == "" {
				return m, nil
			}
			m.textarea.Reset()
			m.showPicker = false
			m.pickerQuery = ""
			return m, func() tea.Msg { return InputSubmitMsg{Text: text} }
		}
	}

	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	m.updatePickerState()
	return m, cmd
}

func (m *InputModel) updatePickerState() {
	val := m.textarea.Value()
	if strings.HasPrefix(val, "$") && !strings.Contains(val, " ") {
		m.showPicker = true
		m.pickerQuery = val[1:]
		filtered := m.filteredSkills()
		if m.pickerIdx >= len(filtered) {
			m.pickerIdx = 0
		}
	} else {
		m.showPicker = false
		m.pickerQuery = ""
	}
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

	if m.doneHint {
		return border.Render(StyleSuccess.Render("  ✓ Task done") + StyleMuted.Render(" — press Enter to continue"))
	}

	inputView := border.Render(m.textarea.View())

	if m.showPicker && len(m.skills) > 0 {
		picker := m.renderPicker()
		return picker + "\n" + inputView
	}
	return inputView
}

func (m InputModel) renderPicker() string {
	filtered := m.filteredSkills()
	if len(filtered) == 0 {
		return StyleMuted.Render("  No matching skills")
	}

	var sb strings.Builder
	sb.WriteString(StyleDimmed.Render("  Select skill (↑↓ navigate, Tab confirm, Esc cancel):") + "\n")
	for i, s := range filtered {
		if i >= 8 {
			sb.WriteString(StyleMuted.Render(fmt.Sprintf("  ... and %d more", len(filtered)-8)))
			break
		}
		if i == m.pickerIdx {
			sb.WriteString(StyleCyan.Render(fmt.Sprintf("  ▸ %s", s)) + "\n")
		} else {
			sb.WriteString(StyleMuted.Render(fmt.Sprintf("    %s", s)) + "\n")
		}
	}
	return strings.TrimRight(sb.String(), "\n")
}
