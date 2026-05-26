package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type ChatViewport struct {
	viewport viewport.Model
	banner   string
	items    []ChatItem
	width    int
	height   int
}

func NewChatViewport(width, height int) ChatViewport {
	vp := viewport.New(width, height)
	vp.Style = lipgloss.NewStyle()
	return ChatViewport{
		viewport: vp,
		width:    width,
		height:   height,
	}
}

func (cv *ChatViewport) SetSize(width, height int) {
	cv.width = width
	cv.height = height
	cv.viewport.Width = width
	cv.viewport.Height = height
	cv.rerender()
}

func (cv *ChatViewport) AppendItem(item ChatItem) {
	cv.items = append(cv.items, item)
	cv.rerender()
	cv.viewport.GotoBottom()
}

func (cv *ChatViewport) UpdateLastAssistant(chunk string) {
	if len(cv.items) > 0 {
		if am, ok := cv.items[len(cv.items)-1].(*AssistantMessage); ok && !am.Complete {
			am.Chunks = append(am.Chunks, chunk)
			cv.rerender()
			cv.viewport.GotoBottom()
			return
		}
	}
	am := &AssistantMessage{Chunks: []string{chunk}}
	cv.items = append(cv.items, am)
	cv.rerender()
	cv.viewport.GotoBottom()
}

func (cv *ChatViewport) FinalizeAssistant() {
	if len(cv.items) == 0 {
		return
	}
	if am, ok := cv.items[len(cv.items)-1].(*AssistantMessage); ok {
		am.Complete = true
		cv.rerender()
	}
}

func (cv *ChatViewport) AppendToolDrawer(d *ToolDrawer) {
	cv.items = append(cv.items, d)
	cv.rerender()
	cv.viewport.GotoBottom()
}

func (cv *ChatViewport) SetBanner(banner string) {
	cv.banner = banner
	cv.rerender()
}

func (cv *ChatViewport) Clear() {
	cv.items = nil
	cv.rerender()
}

func (cv *ChatViewport) rerender() {
	var sb strings.Builder

	if cv.banner != "" {
		lines := strings.Split(cv.banner, "\n")
		for _, line := range lines {
			sb.WriteString("│ " + line + "\n")
		}
		separator := "├" + strings.Repeat("─", cv.width-2) + "┤"
		sb.WriteString(StyleDimmed.Render(separator) + "\n")
	}

	for i, item := range cv.items {
		if i > 0 {
			sb.WriteString("\n│\n")
		}
		sb.WriteString(item.Render(cv.width))
	}
	cv.viewport.SetContent(sb.String())
}

func (cv *ChatViewport) Update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	cv.viewport, cmd = cv.viewport.Update(msg)
	return cmd
}

func (cv ChatViewport) View() string {
	title := " Harness Agent "
	borderLen := cv.width - len(title) - 4
	if borderLen < 2 {
		borderLen = 2
	}
	leftPad := 2
	rightPad := borderLen - leftPad
	if rightPad < 0 {
		rightPad = 0
	}
	header := lipgloss.NewStyle().
		Foreground(ColorPurple).
		Bold(true).
		Render("╭─" + strings.Repeat("─", leftPad) + title + strings.Repeat("─", rightPad) + "╮")

	return header + "\n" + cv.viewport.View()
}
