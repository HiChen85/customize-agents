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
}

type UserMessage struct {
	Text string
}

func (m *UserMessage) Render(width int) string {
	prefix := StyleUserPrefix.Render("You: ")
	content := lipgloss.NewStyle().Width(width - 8).Render(m.Text)
	return wrapWithBorder(prefix+content)
}

type AssistantMessage struct {
	Chunks   []string
	Complete bool
}

func (m *AssistantMessage) Render(width int) string {
	prefix := StyleAgentPrefix.Render("Agent: ")
	text := strings.Join(m.Chunks, "")
	content := lipgloss.NewStyle().Width(width - 10).Render(text)
	return wrapWithBorder(prefix+content)
}

type SystemMessage struct {
	Text string
}

func (m *SystemMessage) Render(width int) string {
	content := StyleSystemMsg.Render(m.Text)
	return wrapWithBorder(content)
}

type ErrorMessage struct {
	Text string
}

func (m *ErrorMessage) Render(width int) string {
	content := StyleError.Render("Error: " + m.Text)
	return wrapWithBorder(content)
}

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
