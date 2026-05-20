package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAnthropicProvider_CreateMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "test-key" {
			t.Errorf("missing api key header")
		}
		if r.Header.Get("anthropic-version") != "2023-06-01" {
			t.Errorf("missing anthropic-version header")
		}

		resp := map[string]any{
			"id":   "msg_test",
			"type": "message",
			"role": "assistant",
			"content": []map[string]any{
				{"type": "text", "text": "Hello!"},
			},
			"stop_reason": "end_turn",
			"usage": map[string]any{
				"input_tokens":  10,
				"output_tokens": 5,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewAnthropicProvider("test-key", server.URL, "claude-sonnet-4-20250514")

	req := Request{
		System:    "You are helpful.",
		Messages:  []Message{{Role: "user", Content: []Block{TextBlock{Text: "Hi"}}}},
		MaxTokens: 100,
	}

	resp, err := provider.CreateMessage(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StopReason != "end_turn" {
		t.Errorf("expected stop_reason end_turn, got %s", resp.StopReason)
	}
	if len(resp.Content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(resp.Content))
	}
	tb, ok := resp.Content[0].(TextBlock)
	if !ok {
		t.Fatalf("expected TextBlock, got %T", resp.Content[0])
	}
	if tb.Text != "Hello!" {
		t.Errorf("expected 'Hello!', got '%s'", tb.Text)
	}
	if resp.Usage.InputTokens != 10 {
		t.Errorf("expected 10 input tokens, got %d", resp.Usage.InputTokens)
	}
}

func TestAnthropicProvider_ToolUseResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"id":   "msg_test2",
			"type": "message",
			"role": "assistant",
			"content": []map[string]any{
				{"type": "text", "text": "Let me check."},
				{"type": "tool_use", "id": "toolu_123", "name": "memory_search", "input": map[string]any{"query": "user preferences"}},
			},
			"stop_reason": "tool_use",
			"usage":       map[string]any{"input_tokens": 20, "output_tokens": 15},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewAnthropicProvider("test-key", server.URL, "claude-sonnet-4-20250514")
	req := Request{
		System:    "You are helpful.",
		Messages:  []Message{{Role: "user", Content: []Block{TextBlock{Text: "What do I like?"}}}},
		MaxTokens: 100,
	}

	resp, err := provider.CreateMessage(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StopReason != "tool_use" {
		t.Errorf("expected stop_reason tool_use, got %s", resp.StopReason)
	}
	if len(resp.Content) != 2 {
		t.Fatalf("expected 2 content blocks, got %d", len(resp.Content))
	}
	tu, ok := resp.Content[1].(ToolUseBlock)
	if !ok {
		t.Fatalf("expected ToolUseBlock, got %T", resp.Content[1])
	}
	if tu.Name != "memory_search" {
		t.Errorf("expected tool name 'memory_search', got '%s'", tu.Name)
	}
}
