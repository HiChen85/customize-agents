package tui

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/HiChen85/customize-agents/core"
	"github.com/HiChen85/customize-agents/llm"
	"github.com/HiChen85/customize-agents/memory"
	"github.com/HiChen85/customize-agents/skill"
)

// programRef holds a shared reference to tea.Program.
// Needed because Bubble Tea copies the model by value, so a plain
// *tea.Program field set after NewProgram would be nil inside the running model.
type programRef struct {
	p *tea.Program
}

type AppModel struct {
	chatView  ChatViewport
	input     InputModel
	statusbar StatusBarModel

	agent     *core.Agent
	memoryMgr *memory.MemoryManager
	registry  *skill.SkillRegistry
	modelName string

	agentCancel context.CancelFunc
	focus       FocusArea
	running     bool
	width       int
	height      int
	pRef        *programRef

	toolDrawers map[string]*ToolDrawer
	toolIndex   []string
	toolFocus   int
}

func NewApp(agent *core.Agent, mm *memory.MemoryManager, registry *skill.SkillRegistry, modelName string, maxTokens int) AppModel {
	cv := NewChatViewport(80, 20)

	banner := "  ╦ ╦╔═╗╦═╗╔╗╔╔═╗╔═╗╔═╗\n" +
		"  ╠═╣╠═╣╠╦╝║║║║╣ ╚═╗╚═╗\n" +
		"  ╩ ╩╩ ╩╩╚═╝╚╝╚═╝╚═╝╚═╝  Agent\n" +
		"\n" +
		"  Model: " + modelName + "\n" +
		"  Type /help for commands, or start chatting.\n" +
		"  Type /tools to see available tools, /skills for skills."
	cv.SetBanner(banner)

	return AppModel{
		chatView:    cv,
		input:       NewInput(),
		statusbar:   NewStatusBar(modelName, maxTokens),
		agent:       agent,
		memoryMgr:   mm,
		registry:    registry,
		modelName:   modelName,
		focus:       FocusInput,
		toolDrawers: make(map[string]*ToolDrawer),
		pRef:        &programRef{},
	}
}

func (m AppModel) Init() tea.Cmd {
	return tea.Batch(
		m.input.textarea.Focus(),
		m.tickElapsed(),
	)
}

type tickMsg time.Time

func (m AppModel) tickElapsed() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m AppModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		viewportHeight := m.height - 6
		m.chatView.SetSize(msg.Width, viewportHeight)
		m.input.SetWidth(msg.Width)
		m.statusbar.SetWidth(msg.Width)
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case tickMsg:
		if m.running && m.memoryMgr != nil {
			m.statusbar.SetTokens(m.memoryMgr.TokenUsage())
		}
		for _, id := range m.toolIndex {
			if d, ok := m.toolDrawers[id]; ok && d.state == ToolStatePending {
				cmd := d.Update(msg)
				if cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
		}
		m.chatView.rerender()
		cmds = append(cmds, m.tickElapsed())
		return m, tea.Batch(cmds...)

	case spinner.TickMsg:
		for _, id := range m.toolIndex {
			if d, ok := m.toolDrawers[id]; ok && d.state == ToolStatePending {
				cmd := d.Update(msg)
				if cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
		}
		m.chatView.rerender()
		return m, tea.Batch(cmds...)

	case InputSubmitMsg:
		return m.handleInput(msg.Text)

	case StreamTextMsg:
		m.chatView.UpdateLastAssistant(msg.Text)
		return m, nil

	case ToolStartMsg:
		d := NewToolDrawer(msg.ID, msg.Name, msg.Input)
		m.toolDrawers[msg.ID] = d
		m.toolIndex = append(m.toolIndex, msg.ID)
		m.chatView.AppendToolDrawer(d)
		m.statusbar.IncrToolCalls()
		cmds = append(cmds, d.spinner.Tick)
		return m, tea.Batch(cmds...)

	case ToolDoneMsg:
		if d, ok := m.toolDrawers[msg.ID]; ok {
			d.Complete(msg.Output, msg.Duration, msg.IsError)
			m.chatView.rerender()
		}
		return m, nil

	case AgentDoneMsg:
		m.chatView.FinalizeAssistant()
		m.running = false
		m.input.SetDisabled(false)
		m.statusbar.SetState("idle")
		cmds = append(cmds, m.input.Focus())
		return m, tea.Batch(cmds...)

	case AgentErrorMsg:
		m.chatView.AppendItem(&ErrorMessage{Text: msg.Err.Error()})
		m.running = false
		m.input.SetDisabled(false)
		m.statusbar.SetState("idle")
		cmds = append(cmds, m.input.Focus())
		return m, tea.Batch(cmds...)

	case AgentStateMsg:
		m.statusbar.SetState(msg.State)
		return m, nil
	}

	switch m.focus {
	case FocusInput:
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		cmds = append(cmds, cmd)
	case FocusViewport:
		cmd := m.chatView.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m AppModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+d":
		return m, tea.Quit

	case "ctrl+c":
		if m.running && m.agentCancel != nil {
			m.agentCancel()
			m.running = false
			m.input.SetDisabled(false)
			m.statusbar.SetState("idle")
			m.chatView.AppendItem(&SystemMessage{Text: "Agent cancelled."})
			return m, m.input.Focus()
		}
		return m, tea.Quit

	case "tab":
		switch m.focus {
		case FocusInput:
			if len(m.toolIndex) > 0 {
				m.focus = FocusTool
				m.input.Blur()
				m.toolFocus = len(m.toolIndex) - 1
				m.setToolFocus()
			}
		case FocusTool:
			m.focus = FocusInput
			m.clearToolFocus()
			return m, m.input.Focus()
		case FocusViewport:
			m.focus = FocusInput
			return m, m.input.Focus()
		}
		m.chatView.rerender()
		return m, nil

	case "esc":
		if m.focus != FocusInput {
			m.focus = FocusInput
			m.clearToolFocus()
			m.chatView.rerender()
			return m, m.input.Focus()
		}
		if !m.running {
			m.chatView.Clear()
		}
		return m, nil

	case "enter":
		if m.focus == FocusTool && m.toolFocus >= 0 && m.toolFocus < len(m.toolIndex) {
			id := m.toolIndex[m.toolFocus]
			if d, ok := m.toolDrawers[id]; ok {
				d.ToggleExpand()
				m.chatView.rerender()
			}
			return m, nil
		}

	case "up":
		if m.focus == FocusTool && m.toolFocus > 0 {
			m.clearToolFocus()
			m.toolFocus--
			m.setToolFocus()
			m.chatView.rerender()
			return m, nil
		}

	case "down":
		if m.focus == FocusTool && m.toolFocus < len(m.toolIndex)-1 {
			m.clearToolFocus()
			m.toolFocus++
			m.setToolFocus()
			m.chatView.rerender()
			return m, nil
		}
	}

	if m.focus == FocusInput {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	if m.focus == FocusViewport {
		cmd := m.chatView.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *AppModel) setToolFocus() {
	if m.toolFocus >= 0 && m.toolFocus < len(m.toolIndex) {
		if d, ok := m.toolDrawers[m.toolIndex[m.toolFocus]]; ok {
			d.focused = true
		}
	}
}

func (m *AppModel) clearToolFocus() {
	for _, d := range m.toolDrawers {
		d.focused = false
	}
}

func (m AppModel) handleInput(text string) (tea.Model, tea.Cmd) {
	if strings.HasPrefix(text, "/") {
		return m.handleCommand(text)
	}

	m.chatView.AppendItem(&UserMessage{Text: text})
	m.running = true
	m.input.SetDisabled(true)
	m.statusbar.SetState("running")
	m.statusbar.ResetToolCalls()
	m.toolDrawers = make(map[string]*ToolDrawer)
	m.toolIndex = nil

	ctx, cancel := context.WithCancel(context.Background())
	m.agentCancel = cancel

	return m, m.runAgent(ctx, text)
}

func (m AppModel) runAgent(ctx context.Context, input string) tea.Cmd {
	pRef := m.pRef
	return func() tea.Msg {
		onEvent := func(event llm.StreamEvent) {
			if event.Type == "text_delta" && pRef != nil && pRef.p != nil {
				pRef.p.Send(StreamTextMsg{Text: event.Text})
			}
		}

		_, err := m.agent.RunStream(ctx, input, onEvent)
		if err != nil {
			return AgentErrorMsg{Err: err}
		}
		return AgentDoneMsg{}
	}
}

func (m AppModel) handleCommand(input string) (tea.Model, tea.Cmd) {
	parts := strings.Fields(input)
	cmd := parts[0]

	switch cmd {
	case "/help":
		help := "Commands:\n" +
			"  /tools                  - List available tools\n" +
			"  /skills                 - List available skills\n" +
			"  /skill activate <name>  - Activate a skill\n" +
			"  /memory search <query>  - Search long-term memory\n" +
			"  /status                 - Show context window usage\n" +
			"  /pause                  - Pause the agent\n" +
			"  /resume                 - Resume the agent\n" +
			"  /clear                  - Clear conversation\n" +
			"  /quit                   - Exit\n\n" +
			"  (Press Esc to clear)"
		m.chatView.AppendItem(&SystemMessage{Text: help})

	case "/tools":
		if m.agent == nil {
			m.chatView.AppendItem(&ErrorMessage{Text: "no agent available"})
			return m, nil
		}
		tools := m.agent.Tools()
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Available tools (%d):\n", len(tools)))
		for _, t := range tools {
			sb.WriteString(fmt.Sprintf("  - %s: %s\n", t.Definition.Name, t.Definition.Description))
		}
		sb.WriteString("\n  (Press Esc to clear)")
		m.chatView.AppendItem(&SystemMessage{Text: sb.String()})

	case "/skills":
		if m.registry == nil {
			m.chatView.AppendItem(&SystemMessage{Text: "No skill registry available."})
			return m, nil
		}
		var sb strings.Builder
		sb.WriteString("Available skills:\n")
		index := m.registry.GetIndex()
		for _, idx := range index {
			active := ""
			if m.registry.IsActive(idx.Name) {
				active = " [active]"
			}
			sb.WriteString(fmt.Sprintf("  - %s: %s%s\n", idx.Name, idx.Description, active))
		}
		sb.WriteString("\n  (Press Esc to clear)")
		m.chatView.AppendItem(&SystemMessage{Text: sb.String()})

	case "/skill":
		if len(parts) < 2 {
			m.chatView.AppendItem(&SystemMessage{Text: "Usage: /skill activate <name>"})
			return m, nil
		}
		switch parts[1] {
		case "activate":
			if len(parts) < 3 {
				m.chatView.AppendItem(&SystemMessage{Text: "Usage: /skill activate <name>"})
				return m, nil
			}
			if m.registry == nil {
				m.chatView.AppendItem(&ErrorMessage{Text: "no skill registry available"})
				return m, nil
			}
			s, err := m.registry.Activate(parts[2])
			if err != nil {
				m.chatView.AppendItem(&ErrorMessage{Text: err.Error()})
			} else {
				m.chatView.AppendItem(&SystemMessage{Text: fmt.Sprintf("Activated skill: %s", s.Name)})
				m.statusbar.SetSkill(s.Name)
			}
		}

	case "/memory":
		if len(parts) < 3 || parts[1] != "search" {
			m.chatView.AppendItem(&SystemMessage{Text: "Usage: /memory search <query>"})
			return m, nil
		}
		if m.memoryMgr == nil {
			m.chatView.AppendItem(&ErrorMessage{Text: "no memory manager available"})
			return m, nil
		}
		query := strings.Join(parts[2:], " ")
		entries, err := m.memoryMgr.RetrieveRelevant(context.Background(), query, 5)
		if err != nil {
			m.chatView.AppendItem(&ErrorMessage{Text: err.Error()})
		} else if len(entries) == 0 {
			m.chatView.AppendItem(&SystemMessage{Text: "No memories found."})
		} else {
			var sb strings.Builder
			for _, e := range entries {
				sb.WriteString(fmt.Sprintf("  [%s] %s (tags: %s)\n", e.ID, e.Content, strings.Join(e.Tags, ", ")))
			}
			m.chatView.AppendItem(&SystemMessage{Text: sb.String()})
		}

	case "/status":
		if m.memoryMgr == nil {
			m.chatView.AppendItem(&SystemMessage{Text: "No memory manager available."})
			return m, nil
		}
		used, max := m.memoryMgr.TokenUsage()
		msg := fmt.Sprintf("Context: %d / %d tokens (%.1f%%)", used, max, float64(used)/float64(max)*100)
		m.chatView.AppendItem(&SystemMessage{Text: msg})

	case "/pause":
		if m.agent == nil || m.agent.Lifecycle() == nil {
			m.chatView.AppendItem(&ErrorMessage{Text: "no lifecycle available"})
			return m, nil
		}
		if err := m.agent.Lifecycle().Pause(); err != nil {
			m.chatView.AppendItem(&ErrorMessage{Text: err.Error()})
		} else {
			m.chatView.AppendItem(&SystemMessage{Text: "Agent paused. Type /resume to continue."})
			m.statusbar.SetState("paused")
		}

	case "/resume":
		if m.agent == nil || m.agent.Lifecycle() == nil {
			m.chatView.AppendItem(&ErrorMessage{Text: "no lifecycle available"})
			return m, nil
		}
		if err := m.agent.Lifecycle().Resume(); err != nil {
			m.chatView.AppendItem(&ErrorMessage{Text: err.Error()})
		} else {
			m.chatView.AppendItem(&SystemMessage{Text: "Agent resumed."})
			m.statusbar.SetState("idle")
		}

	case "/clear":
		m.chatView.Clear()

	case "/quit":
		return m, tea.Quit

	default:
		m.chatView.AppendItem(&SystemMessage{Text: fmt.Sprintf("Unknown command: %s (type /help)", cmd)})
	}

	return m, nil
}

func (m AppModel) View() string {
	return m.chatView.View() + "\n" + m.input.View() + "\n" + m.statusbar.View()
}

// Run starts the TUI application
func Run(agent *core.Agent, mm *memory.MemoryManager, registry *skill.SkillRegistry, modelName string, maxTokens int) error {
	app := NewApp(agent, mm, registry, modelName, maxTokens)

	hookRegistry := core.NewHookRegistry()
	var p *tea.Program
	var toolCounter int64
	var toolCounterMu sync.Mutex
	var activeToolIDs sync.Map

	hookRegistry.Register(core.BeforeToolCall, core.NewGoHook(func(ctx context.Context, payload core.HookPayload) error {
		if p != nil {
			toolCounterMu.Lock()
			toolCounter++
			id := fmt.Sprintf("%s-%d", payload.ToolName, toolCounter)
			toolCounterMu.Unlock()
			activeToolIDs.Store(payload.ToolName+"-active", id)
			p.Send(ToolStartMsg{
				ID:    id,
				Name:  payload.ToolName,
				Input: payload.Input,
			})
		}
		return nil
	}))

	hookRegistry.Register(core.AfterToolCall, core.NewGoHook(func(ctx context.Context, payload core.HookPayload) error {
		if p != nil {
			idVal, ok := activeToolIDs.LoadAndDelete(payload.ToolName + "-active")
			if !ok {
				return nil
			}
			id := idVal.(string)
			isError := payload.Error != nil
			output := payload.Output
			if isError {
				output = payload.Error.Error()
			}
			p.Send(ToolDoneMsg{
				ID:       id,
				Name:     payload.ToolName,
				Output:   output,
				Duration: payload.Duration,
				IsError:  isError,
			})
		}
		return nil
	}))

	agent.SetHookRegistry(hookRegistry)

	p = tea.NewProgram(app, tea.WithAltScreen())
	app.pRef.p = p

	_, err := p.Run()
	return err
}
