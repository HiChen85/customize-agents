package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

type StatusBarModel struct {
	agentState string
	model      string
	tokenUsed  int
	tokenMax   int
	toolCalls  int
	skill      string
	startTime  time.Time
	width      int
}

func NewStatusBar(model string, maxTokens int) StatusBarModel {
	return StatusBarModel{
		agentState: "idle",
		model:      model,
		tokenMax:   maxTokens,
		skill:      "none",
	}
}

func (s *StatusBarModel) SetWidth(w int) {
	s.width = w
}

func (s *StatusBarModel) SetState(state string) {
	s.agentState = state
	if state == "running" {
		s.startTime = time.Now()
	}
}

func (s *StatusBarModel) SetTokens(used, max int) {
	s.tokenUsed = used
	s.tokenMax = max
}

func (s *StatusBarModel) IncrToolCalls() {
	s.toolCalls++
}

func (s *StatusBarModel) ResetToolCalls() {
	s.toolCalls = 0
}

func (s *StatusBarModel) SetSkill(name string) {
	s.skill = name
}

func (s StatusBarModel) View() string {
	dot := s.stateIndicator()
	elapsed := s.elapsed()

	tokenStr := fmt.Sprintf("%d/%dk tokens", s.tokenUsed, s.tokenMax/1000)
	toolStr := fmt.Sprintf("tools: %d", s.toolCalls)

	parts := []string{
		dot,
		s.model,
		tokenStr,
		toolStr,
		"skill: " + s.skill,
		elapsed,
	}

	content := strings.Join(parts, " │ ")
	bar := fmt.Sprintf("╰─ %s ─╯", content)

	return StyleStatusBar.Width(s.width).Render(bar)
}

func (s StatusBarModel) stateIndicator() string {
	switch s.agentState {
	case "running":
		return lipgloss.NewStyle().Foreground(ColorCyan).Render("●") + " " +
			lipgloss.NewStyle().Foreground(ColorCyan).Render("running")
	case "paused":
		return lipgloss.NewStyle().Foreground(ColorYellow).Render("●") + " " +
			lipgloss.NewStyle().Foreground(ColorYellow).Render("paused")
	case "stopped":
		return lipgloss.NewStyle().Foreground(ColorRed).Render("●") + " " +
			lipgloss.NewStyle().Foreground(ColorRed).Render("stopped")
	default:
		return lipgloss.NewStyle().Foreground(ColorGreen).Render("●") + " " +
			lipgloss.NewStyle().Foreground(ColorGreen).Render("idle")
	}
}

func (s StatusBarModel) elapsed() string {
	if s.agentState != "running" || s.startTime.IsZero() {
		return "0.0s"
	}
	d := time.Since(s.startTime).Truncate(100 * time.Millisecond)
	return fmt.Sprintf("%.1fs", d.Seconds())
}
