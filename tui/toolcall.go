package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type ToolState int

const (
	ToolStatePending ToolState = iota
	ToolStateDone
	ToolStateError
)

type ToolDrawer struct {
	id       string
	name     string
	input    json.RawMessage
	output   string
	state    ToolState
	expanded bool
	duration time.Duration
	start    time.Time
	spinner  spinner.Model
	focused  bool
}

func NewToolDrawer(id, name string, input json.RawMessage) *ToolDrawer {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = StyleRunning
	return &ToolDrawer{
		id:       id,
		name:     name,
		input:    input,
		state:    ToolStatePending,
		expanded: true,
		start:    time.Now(),
		spinner:  s,
	}
}

func (d *ToolDrawer) Complete(output string, duration time.Duration, isError bool) {
	d.output = output
	d.duration = duration
	d.expanded = false
	if isError {
		d.state = ToolStateError
	} else {
		d.state = ToolStateDone
	}
}

func (d *ToolDrawer) ToggleExpand() {
	d.expanded = !d.expanded
}

func (d *ToolDrawer) Update(msg tea.Msg) tea.Cmd {
	if d.state == ToolStatePending {
		var cmd tea.Cmd
		d.spinner, cmd = d.spinner.Update(msg)
		return cmd
	}
	return nil
}

func (d *ToolDrawer) Render(width int) string {
	return d.View(width)
}

func (d *ToolDrawer) IsEphemeral() bool { return false }

func (d *ToolDrawer) View(width int) string {
	var sb strings.Builder

	icon := "▸"
	if d.expanded {
		icon = "▾"
	}

	header := StyleToolIcon.Render(icon) + " " + StyleToolName.Render(d.name)

	switch d.state {
	case ToolStatePending:
		elapsed := time.Since(d.start).Truncate(100 * time.Millisecond)
		header += " " + d.spinner.View() + " " + StyleMuted.Render(fmt.Sprintf("(%s)", elapsed))
	case ToolStateDone:
		summary := truncateOutput(d.output, 60)
		header += " " + StyleSuccess.Render("✓") + " " + StyleMuted.Render(fmt.Sprintf("%s (%s)", summary, d.duration.Truncate(time.Millisecond)))
	case ToolStateError:
		summary := truncateOutput(d.output, 60)
		header += " " + StyleError.Render("✗") + " " + StyleMuted.Render(summary)
	}

	if d.focused {
		sb.WriteString("│ " + lipgloss.NewStyle().Bold(true).Render(header))
	} else {
		sb.WriteString("│ " + header)
	}

	if d.expanded {
		sb.WriteString("\n")
		detail := d.renderDetail(width - 4)
		sb.WriteString(detail)
	}

	return sb.String()
}

func (d *ToolDrawer) renderDetail(width int) string {
	var sb strings.Builder
	indent := "│   "

	sb.WriteString(indent + StyleDimmed.Render("INPUT:") + "\n")
	inputStr := formatJSON(d.input, width-6)
	for _, line := range strings.Split(inputStr, "\n") {
		sb.WriteString(indent + "  " + line + "\n")
	}

	if d.output != "" {
		sb.WriteString(indent + StyleDimmed.Render("OUTPUT:") + "\n")
		lines := strings.Split(d.output, "\n")
		maxLines := 20
		if len(lines) > maxLines {
			lines = append(lines[:maxLines], StyleMuted.Render(fmt.Sprintf("... (%d more lines)", len(lines)-maxLines)))
		}
		for _, line := range lines {
			sb.WriteString(indent + "  " + line + "\n")
		}
	}

	return sb.String()
}

func truncateOutput(s string, maxLen int) string {
	s = strings.Split(s, "\n")[0]
	if len(s) > maxLen {
		return s[:maxLen-3] + "..."
	}
	return s
}

func formatJSON(data json.RawMessage, maxWidth int) string {
	_ = maxWidth // reserved for future width-aware formatting
	var obj map[string]interface{}
	if err := json.Unmarshal(data, &obj); err != nil {
		return string(data)
	}
	var parts []string
	for k, v := range obj {
		parts = append(parts, fmt.Sprintf("%s: %v", k, v))
	}
	return strings.Join(parts, ", ")
}
