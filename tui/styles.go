package tui

import "github.com/charmbracelet/lipgloss"

var (
	ColorPurple = lipgloss.Color("#a78bfa")
	ColorBlue   = lipgloss.Color("#60a5fa")
	ColorYellow = lipgloss.Color("#fbbf24")
	ColorGreen  = lipgloss.Color("#4ade80")
	ColorRed    = lipgloss.Color("#ef4444")
	ColorCyan   = lipgloss.Color("#38bdf8")
	ColorMuted  = lipgloss.Color("#888888")
	ColorDimmed = lipgloss.Color("#555555")

	StyleBorder = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(ColorPurple)

	StyleUserPrefix = lipgloss.NewStyle().
			Foreground(ColorBlue).
			Bold(true)

	StyleAgentPrefix = lipgloss.NewStyle().
			Foreground(ColorPurple).
			Bold(true)

	StyleToolIcon = lipgloss.NewStyle().
			Foreground(ColorYellow)

	StyleToolName = lipgloss.NewStyle().
			Foreground(ColorYellow).
			Bold(true)

	StyleSuccess = lipgloss.NewStyle().
			Foreground(ColorGreen)

	StyleError = lipgloss.NewStyle().
			Foreground(ColorRed)

	StyleRunning = lipgloss.NewStyle().
			Foreground(ColorCyan)

	StyleMuted = lipgloss.NewStyle().
			Foreground(ColorMuted)

	StyleDimmed = lipgloss.NewStyle().
			Foreground(ColorDimmed)

	StyleStatusBar = lipgloss.NewStyle().
			Foreground(ColorMuted).
			BorderStyle(lipgloss.NormalBorder()).
			BorderTop(true).
			BorderForeground(ColorDimmed)

	StyleSystemMsg = lipgloss.NewStyle().
			Foreground(ColorMuted).
			Italic(true)
)
