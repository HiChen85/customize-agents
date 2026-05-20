package core

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/haichen-zhang/customize-agents/llm"
	"github.com/haichen-zhang/customize-agents/memory"
	"github.com/haichen-zhang/customize-agents/skill"
)

func TestIntegration_AgentWithMemoryAndSkills(t *testing.T) {
	dir := t.TempDir()
	store, _ := memory.NewFileStore(dir)
	mm := memory.NewMemoryManager(store, 10000)

	provider := &mockProvider{
		responses: []*llm.Response{
			{
				Content: []llm.Block{
					llm.TextBlock{Text: "I'll remember that."},
					llm.ToolUseBlock{ID: "t1", Name: "memory_save", Input: json.RawMessage(`{"content":"user prefers Go","tags":["preference"]}`)},
				},
				StopReason: "tool_use",
			},
			{
				Content:    []llm.Block{llm.TextBlock{Text: "Got it! I've saved that you prefer Go."}},
				StopReason: "end_turn",
			},
		},
	}

	testSkill := &skill.Skill{
		Name:   "helper",
		Prompt: "Remember user preferences when they share them.",
	}

	tools := []Tool{
		NewMemorySaveTool(store),
		NewMemorySearchTool(store),
	}

	agent := NewAgent(provider, mm, tools, []*skill.Skill{testSkill})

	reply, err := agent.Run(context.Background(), "I prefer Go over Python")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if reply != "Got it! I've saved that you prefer Go." {
		t.Errorf("unexpected reply: %s", reply)
	}

	entries, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("list error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 memory entry, got %d", len(entries))
	}
	if entries[0].Content != "user prefers Go" {
		t.Errorf("expected memory content 'user prefers Go', got '%s'", entries[0].Content)
	}
}

func TestIntegration_MemoryRetrievalInContext(t *testing.T) {
	dir := t.TempDir()
	store, _ := memory.NewFileStore(dir)

	store.Save(context.Background(), memory.Entry{
		ID:      "pref1",
		Content: "user likes dark mode and vim keybindings",
		Tags:    []string{"preference"},
	})

	mm := memory.NewMemoryManager(store, 10000)

	provider := &mockProvider{
		responses: []*llm.Response{
			{
				Content:    []llm.Block{llm.TextBlock{Text: "Based on your preferences, I recommend dark theme."}},
				StopReason: "end_turn",
			},
		},
	}

	agent := NewAgent(provider, mm, nil, nil)
	reply, err := agent.Run(context.Background(), "What theme should I use?")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if reply != "Based on your preferences, I recommend dark theme." {
		t.Errorf("unexpected reply: %s", reply)
	}
}
