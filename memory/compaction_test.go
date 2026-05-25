package memory

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/HiChen85/customize-agents/llm"
)

func TestCompactor_Summarize_Success(t *testing.T) {
	provider := &mockCompactionProvider{
		response: "User asked about weather. Assistant provided forecast for NYC.",
	}

	compactor := NewCompactor(CompactionConfig{
		Threshold: 0.8,
		Provider:  provider,
	})

	messages := []llm.Message{
		{Role: "user", Content: []llm.Block{llm.TextBlock{Text: "What's the weather?"}}},
		{Role: "assistant", Content: []llm.Block{llm.TextBlock{Text: "The weather in NYC is sunny, 72F."}}},
	}

	summary, err := compactor.Summarize(context.Background(), messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if summary != "User asked about weather. Assistant provided forecast for NYC." {
		t.Errorf("unexpected summary: %s", summary)
	}
}

func TestCompactor_Summarize_ProviderError(t *testing.T) {
	provider := &mockCompactionProvider{
		err: fmt.Errorf("connection refused"),
	}

	compactor := NewCompactor(CompactionConfig{
		Threshold: 0.8,
		Provider:  provider,
	})

	messages := []llm.Message{
		{Role: "user", Content: []llm.Block{llm.TextBlock{Text: "hello"}}},
	}

	_, err := compactor.Summarize(context.Background(), messages)
	if err == nil {
		t.Fatal("expected error from provider failure")
	}
}

func TestCompactor_Summarize_EmptyMessages(t *testing.T) {
	provider := &mockCompactionProvider{response: ""}

	compactor := NewCompactor(CompactionConfig{
		Threshold: 0.8,
		Provider:  provider,
	})

	summary, err := compactor.Summarize(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if summary != "" {
		t.Errorf("expected empty summary for nil messages, got: %s", summary)
	}
}

type mockCompactionProvider struct {
	response string
	err      error
}

func (m *mockCompactionProvider) CreateMessage(ctx context.Context, req llm.Request) (*llm.Response, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &llm.Response{
		Content: []llm.Block{llm.TextBlock{Text: m.response}},
	}, nil
}

func TestWorkingMemory_CompactWithCompactor(t *testing.T) {
	provider := &mockCompactionProvider{
		response: "Summary of prior conversation.",
	}

	compactor := NewCompactor(CompactionConfig{
		Threshold: 0.8,
		Provider:  provider,
	})

	wm := NewWorkingMemory(100, &SimpleTokenizer{})
	wm.SetCompactor(compactor)

	for i := 0; i < 20; i++ {
		wm.AppendWithContext(context.Background(), llm.Message{
			Role:    "user",
			Content: []llm.Block{llm.TextBlock{Text: fmt.Sprintf("Message number %d with some content to fill tokens", i)}},
		})
	}

	messages := wm.GetMessages()
	foundSummary := false
	for _, msg := range messages {
		for _, block := range msg.Content {
			if tb, ok := block.(llm.TextBlock); ok {
				if strings.Contains(tb.Text, "Summary of prior conversation") {
					foundSummary = true
				}
			}
		}
	}

	if !foundSummary {
		t.Error("expected LLM-generated summary in compacted messages")
	}
}

func TestWorkingMemory_CompactFallbackOnError(t *testing.T) {
	provider := &mockCompactionProvider{
		err: fmt.Errorf("network error"),
	}

	compactor := NewCompactor(CompactionConfig{
		Threshold: 0.8,
		Provider:  provider,
	})

	wm := NewWorkingMemory(100, &SimpleTokenizer{})
	wm.SetCompactor(compactor)

	for i := 0; i < 20; i++ {
		wm.AppendWithContext(context.Background(), llm.Message{
			Role:    "user",
			Content: []llm.Block{llm.TextBlock{Text: fmt.Sprintf("Message %d filling up tokens here", i)}},
		})
	}

	messages := wm.GetMessages()
	if len(messages) == 0 {
		t.Fatal("expected messages after fallback compaction")
	}

	foundFallback := false
	for _, msg := range messages {
		for _, block := range msg.Content {
			if tb, ok := block.(llm.TextBlock); ok {
				if strings.Contains(tb.Text, "messages summarized") {
					foundFallback = true
				}
			}
		}
	}
	if !foundFallback {
		t.Error("expected fallback truncation summary when LLM fails")
	}
}
