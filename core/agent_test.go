package core

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/haichen-zhang/customize-agents/llm"
	"github.com/haichen-zhang/customize-agents/memory"
	"github.com/haichen-zhang/customize-agents/skill"
)

type mockProvider struct {
	responses []*llm.Response
	callCount int
}

func (m *mockProvider) CreateMessage(ctx context.Context, req llm.Request) (*llm.Response, error) {
	idx := m.callCount
	m.callCount++
	if idx < len(m.responses) {
		return m.responses[idx], nil
	}
	return &llm.Response{
		Content:    []llm.Block{llm.TextBlock{Text: "default response"}},
		StopReason: "end_turn",
	}, nil
}

func TestAgent_SimpleResponse(t *testing.T) {
	provider := &mockProvider{
		responses: []*llm.Response{
			{
				Content:    []llm.Block{llm.TextBlock{Text: "Hello there!"}},
				StopReason: "end_turn",
				Usage:      llm.Usage{InputTokens: 10, OutputTokens: 5},
			},
		},
	}

	dir := t.TempDir()
	store, _ := memory.NewFileStore(dir)
	mm := memory.NewMemoryManager(store, 10000)

	agent := NewAgent(provider, mm, nil, nil)
	reply, err := agent.Run(context.Background(), "Hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reply != "Hello there!" {
		t.Errorf("expected 'Hello there!', got '%s'", reply)
	}
}

func TestAgent_ToolUseLoop(t *testing.T) {
	provider := &mockProvider{
		responses: []*llm.Response{
			{
				Content: []llm.Block{
					llm.TextBlock{Text: "Let me check."},
					llm.ToolUseBlock{ID: "t1", Name: "echo_tool", Input: json.RawMessage(`{"msg":"hi"}`)},
				},
				StopReason: "tool_use",
			},
			{
				Content:    []llm.Block{llm.TextBlock{Text: "The tool said: hi back"}},
				StopReason: "end_turn",
			},
		},
	}

	dir := t.TempDir()
	store, _ := memory.NewFileStore(dir)
	mm := memory.NewMemoryManager(store, 10000)

	echoTool := Tool{
		Definition: llm.ToolDef{
			Name:        "echo_tool",
			Description: "Echoes back the message",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"msg":{"type":"string"}}}`),
		},
		Execute: func(ctx context.Context, input json.RawMessage) (string, error) {
			var p struct{ Msg string `json:"msg"` }
			json.Unmarshal(input, &p)
			return p.Msg + " back", nil
		},
	}

	agent := NewAgent(provider, mm, []Tool{echoTool}, nil)
	reply, err := agent.Run(context.Background(), "echo something")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reply != "The tool said: hi back" {
		t.Errorf("expected 'The tool said: hi back', got '%s'", reply)
	}
	if provider.callCount != 2 {
		t.Errorf("expected 2 LLM calls, got %d", provider.callCount)
	}
}

func TestAgent_WithSkill(t *testing.T) {
	provider := &mockProvider{
		responses: []*llm.Response{
			{
				Content:    []llm.Block{llm.TextBlock{Text: "Reviewing your code..."}},
				StopReason: "end_turn",
			},
		},
	}

	dir := t.TempDir()
	store, _ := memory.NewFileStore(dir)
	mm := memory.NewMemoryManager(store, 10000)

	testSkill := &skill.Skill{
		Name:   "test-skill",
		Prompt: "You are a code reviewer.",
	}

	agent := NewAgent(provider, mm, nil, []*skill.Skill{testSkill})
	reply, err := agent.Run(context.Background(), "Review this")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reply != "Reviewing your code..." {
		t.Errorf("expected 'Reviewing your code...', got '%s'", reply)
	}
}
