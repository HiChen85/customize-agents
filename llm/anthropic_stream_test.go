package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAnthropicProvider_CreateMessageStream_TextOnly(t *testing.T) {
	sseResponse := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"claude-sonnet-4-20250514\",\"stop_reason\":null,\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}\n\nevent: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\nevent: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello\"}}\n\nevent: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\" world\"}}\n\nevent: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":5}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, sseResponse)
	}))
	defer server.Close()

	provider := NewAnthropicProvider("test-key", server.URL, "claude-sonnet-4-20250514")

	ch, err := provider.CreateMessageStream(context.Background(), Request{
		System:    "You are helpful.",
		Messages:  []Message{{Role: "user", Content: []Block{TextBlock{Text: "hi"}}}},
		MaxTokens: 100,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var texts []string
	var gotDone bool
	for event := range ch {
		switch event.Type {
		case "text_delta":
			texts = append(texts, event.Text)
		case "done":
			gotDone = true
		case "error":
			t.Fatalf("unexpected error event: %v", event.Error)
		}
	}

	fullText := strings.Join(texts, "")
	if fullText != "Hello world" {
		t.Errorf("expected 'Hello world', got '%s'", fullText)
	}
	if !gotDone {
		t.Error("expected done event")
	}
}

func TestAnthropicProvider_CreateMessageStream_WithToolUse(t *testing.T) {
	sseResponse := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_2\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"claude-sonnet-4-20250514\",\"stop_reason\":null,\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}\n\nevent: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\nevent: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Let me read that.\"}}\n\nevent: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\nevent: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"tool_use\",\"id\":\"toolu_1\",\"name\":\"read_file\"}}\n\nevent: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"path\\\":\"}}\n\nevent: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"\\\"test.txt\\\"}\"}}\n\nevent: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":1}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\"},\"usage\":{\"output_tokens\":20}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, sseResponse)
	}))
	defer server.Close()

	provider := NewAnthropicProvider("test-key", server.URL, "claude-sonnet-4-20250514")

	ch, err := provider.CreateMessageStream(context.Background(), Request{
		System:    "You are helpful.",
		Messages:  []Message{{Role: "user", Content: []Block{TextBlock{Text: "read test.txt"}}}},
		MaxTokens: 100,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var texts []string
	var toolUses []*ToolUseBlock
	for event := range ch {
		switch event.Type {
		case "text_delta":
			texts = append(texts, event.Text)
		case "tool_use":
			toolUses = append(toolUses, event.ToolUse)
		case "error":
			t.Fatalf("unexpected error event: %v", event.Error)
		}
	}

	if strings.Join(texts, "") != "Let me read that." {
		t.Errorf("unexpected text: %s", strings.Join(texts, ""))
	}
	if len(toolUses) != 1 {
		t.Fatalf("expected 1 tool_use, got %d", len(toolUses))
	}
	if toolUses[0].Name != "read_file" {
		t.Errorf("expected tool name 'read_file', got '%s'", toolUses[0].Name)
	}
	if toolUses[0].ID != "toolu_1" {
		t.Errorf("expected tool ID 'toolu_1', got '%s'", toolUses[0].ID)
	}
}

func TestAnthropicProvider_CreateMessageStream_DeepseekTextDeltaToolInput(t *testing.T) {
	// Deepseek may send tool input via text_delta events instead of input_json_delta
	sseResponse := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_3\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"deepseek-v4-flash\",\"stop_reason\":null,\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}\n\nevent: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\nevent: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Let me check.\"}}\n\nevent: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\nevent: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"tool_use\",\"id\":\"call_001\",\"name\":\"read_file\",\"input\":{}}}\n\nevent: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"text_delta\",\"text\":\"{\\\"path\\\":\\\"/tmp/test.txt\\\"}\"}}\n\nevent: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":1}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\"},\"usage\":{\"output_tokens\":20}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, sseResponse)
	}))
	defer server.Close()

	provider := NewAnthropicProvider("test-key", server.URL, "deepseek-v4-flash")

	ch, err := provider.CreateMessageStream(context.Background(), Request{
		System:    "You are helpful.",
		Messages:  []Message{{Role: "user", Content: []Block{TextBlock{Text: "read /tmp/test.txt"}}}},
		MaxTokens: 100,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var texts []string
	var toolUses []*ToolUseBlock
	for event := range ch {
		switch event.Type {
		case "text_delta":
			texts = append(texts, event.Text)
		case "tool_use":
			toolUses = append(toolUses, event.ToolUse)
		case "error":
			t.Fatalf("unexpected error event: %v", event.Error)
		}
	}

	if strings.Join(texts, "") != "Let me check." {
		t.Errorf("unexpected text: %s", strings.Join(texts, ""))
	}
	if len(toolUses) != 1 {
		t.Fatalf("expected 1 tool_use, got %d", len(toolUses))
	}
	if toolUses[0].Name != "read_file" {
		t.Errorf("expected tool name 'read_file', got '%s'", toolUses[0].Name)
	}
	if string(toolUses[0].Input) != `{"path":"/tmp/test.txt"}` {
		t.Errorf("expected tool input with path, got '%s'", string(toolUses[0].Input))
	}
}

func TestAnthropicProvider_CreateMessageStream_DeepseekInlineInput(t *testing.T) {
	// Deepseek may send the full input inline in content_block_start with no deltas
	sseResponse := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_4\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"deepseek-v4-flash\",\"stop_reason\":null,\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}\n\nevent: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"call_002\",\"name\":\"exec\",\"input\":{\"command\":\"ls -la\"}}}\n\nevent: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\"},\"usage\":{\"output_tokens\":5}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, sseResponse)
	}))
	defer server.Close()

	provider := NewAnthropicProvider("test-key", server.URL, "deepseek-v4-flash")

	ch, err := provider.CreateMessageStream(context.Background(), Request{
		Messages:  []Message{{Role: "user", Content: []Block{TextBlock{Text: "run ls"}}}},
		MaxTokens: 100,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var toolUses []*ToolUseBlock
	for event := range ch {
		if event.Type == "tool_use" {
			toolUses = append(toolUses, event.ToolUse)
		} else if event.Type == "error" {
			t.Fatalf("unexpected error: %v", event.Error)
		}
	}

	if len(toolUses) != 1 {
		t.Fatalf("expected 1 tool_use, got %d", len(toolUses))
	}
	if toolUses[0].Name != "exec" {
		t.Errorf("expected tool name 'exec', got '%s'", toolUses[0].Name)
	}
	if string(toolUses[0].Input) != `{"command":"ls -la"}` {
		t.Errorf("expected input {\"command\":\"ls -la\"}, got '%s'", string(toolUses[0].Input))
	}
}

func TestAnthropicProvider_CreateMessageStream_StringWrappedInput(t *testing.T) {
	// Some providers may double-encode tool input as a JSON string
	sseResponse := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_5\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"deepseek-v4-flash\",\"stop_reason\":null,\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}\n\nevent: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"call_003\",\"name\":\"exec\",\"input\":{}}}\n\nevent: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"\\\"{ \\\\\\\"command\\\\\\\": \\\\\\\"pwd\\\\\\\" }\\\"\"}}\n\nevent: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, sseResponse)
	}))
	defer server.Close()

	provider := NewAnthropicProvider("test-key", server.URL, "deepseek-v4-flash")

	ch, err := provider.CreateMessageStream(context.Background(), Request{
		Messages:  []Message{{Role: "user", Content: []Block{TextBlock{Text: "pwd"}}}},
		MaxTokens: 100,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var toolUses []*ToolUseBlock
	for event := range ch {
		if event.Type == "tool_use" {
			toolUses = append(toolUses, event.ToolUse)
		} else if event.Type == "error" {
			t.Fatalf("unexpected error: %v", event.Error)
		}
	}

	if len(toolUses) != 1 {
		t.Fatalf("expected 1 tool_use, got %d", len(toolUses))
	}
	// The string-wrapped input should be unwrapped to a proper JSON object
	var params struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(toolUses[0].Input, &params); err != nil {
		t.Fatalf("failed to unmarshal unwrapped input: %v (raw: %s)", err, string(toolUses[0].Input))
	}
	if params.Command != "pwd" {
		t.Errorf("expected command 'pwd', got '%s'", params.Command)
	}
}

func TestAnthropicProvider_CreateMessageStream_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"error":{"type":"rate_limit","message":"too many requests"}}`)
	}))
	defer server.Close()

	provider := NewAnthropicProvider("test-key", server.URL, "claude-sonnet-4-20250514")

	_, err := provider.CreateMessageStream(context.Background(), Request{
		Messages:  []Message{{Role: "user", Content: []Block{TextBlock{Text: "hi"}}}},
		MaxTokens: 100,
	})
	if err == nil {
		t.Fatal("expected error for 429 response")
	}
}
