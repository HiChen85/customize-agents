package tui

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// Tea messages sent from agent goroutine to TUI
type StreamTextMsg struct {
	Text string
}

type ToolStartMsg struct {
	ID    string
	Name  string
	Input json.RawMessage
}

type ToolDoneMsg struct {
	ID       string
	Name     string
	Output   string
	Duration time.Duration
	IsError  bool
}

type AgentDoneMsg struct {
	Text string
}

type AgentErrorMsg struct {
	Err error
}

type AgentStateMsg struct {
	State string
}

type HookMsg struct {
	Text string
}

// Focus area
type FocusArea int

const (
	FocusInput FocusArea = iota
	FocusViewport
	FocusTool
)

// ChatItem represents one item in the conversation
type ChatItem interface {
	Render(width int) string
	IsEphemeral() bool
}

type UserMessage struct {
	Text string
}

func (m *UserMessage) Render(width int) string {
	maxWidth := width * 3 / 4
	prefix := StyleUserPrefix.Render("You: ")
	content := lipgloss.NewStyle().Width(maxWidth).Render(m.Text)
	return wrapWithBorder(prefix + content)
}

func (m *UserMessage) IsEphemeral() bool { return false }

type AssistantMessage struct {
	Chunks   []string
	Complete bool
}

func (m *AssistantMessage) Render(width int) string {
	maxWidth := width * 3 / 4
	prefix := StyleAgentPrefix.Render("Agent: ")
	text := strings.Join(m.Chunks, "")
	content := lipgloss.NewStyle().Width(maxWidth).Render(text)
	return wrapWithBorder(prefix + content)
}

func (m *AssistantMessage) IsEphemeral() bool { return false }

type SystemMessage struct {
	Text string
}

func (m *SystemMessage) Render(width int) string {
	content := StyleSystemMsg.Render(m.Text)
	return wrapWithBorder(content)
}

func (m *SystemMessage) IsEphemeral() bool { return true }

type ErrorMessage struct {
	Text string
}

func (m *ErrorMessage) Render(width int) string {
	content := StyleError.Render("Error: " + m.Text)
	return wrapWithBorder(content)
}

func (m *ErrorMessage) IsEphemeral() bool { return true }

func wrapWithBorder(content string) string {
	lines := strings.Split(content, "\n")
	var sb strings.Builder
	for i, line := range lines {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString("│ " + line)
	}
	return sb.String()
}
