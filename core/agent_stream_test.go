package core

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/HiChen85/customize-agents/llm"
	"github.com/HiChen85/customize-agents/memory"
)

func TestExecuteTools_Parallel(t *testing.T) {
	var concurrentCount atomic.Int32
	var maxConcurrent atomic.Int32

	slowTool := Tool{
		Definition: llm.ToolDef{Name: "slow_tool", Description: "slow"},
		Execute: func(ctx context.Context, input json.RawMessage) (string, error) {
			current := concurrentCount.Add(1)
			for {
				old := maxConcurrent.Load()
				if current <= old || maxConcurrent.CompareAndSwap(old, current) {
					break
				}
			}
			time.Sleep(50 * time.Millisecond)
			concurrentCount.Add(-1)
			return "done", nil
		},
	}

	mm := memory.NewMemoryManager(&mockMemStore{}, 4096)
	agent := NewAgent(&mockProv{}, mm, []Tool{slowTool}, nil)

	calls := []llm.ToolUseBlock{
		{ID: "1", Name: "slow_tool", Input: json.RawMessage(`{}`)},
		{ID: "2", Name: "slow_tool", Input: json.RawMessage(`{}`)},
		{ID: "3", Name: "slow_tool", Input: json.RawMessage(`{}`)},
	}

	start := time.Now()
	results := agent.executeTools(context.Background(), calls)
	elapsed := time.Since(start)

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	if maxConcurrent.Load() < 2 {
		t.Errorf("expected concurrent execution, max concurrent was %d", maxConcurrent.Load())
	}

	if elapsed > 120*time.Millisecond {
		t.Errorf("expected parallel execution under 120ms, took %v", elapsed)
	}
}

func TestExecuteTools_Parallel_PreservesOrder(t *testing.T) {
	tools := []Tool{
		{
			Definition: llm.ToolDef{Name: "tool_a", Description: "a"},
			Execute: func(ctx context.Context, input json.RawMessage) (string, error) {
				time.Sleep(50 * time.Millisecond)
				return "result_a", nil
			},
		},
		{
			Definition: llm.ToolDef{Name: "tool_b", Description: "b"},
			Execute: func(ctx context.Context, input json.RawMessage) (string, error) {
				return "result_b", nil
			},
		},
	}

	mm := memory.NewMemoryManager(&mockMemStore{}, 4096)
	agent := NewAgent(&mockProv{}, mm, tools, nil)

	calls := []llm.ToolUseBlock{
		{ID: "1", Name: "tool_a", Input: json.RawMessage(`{}`)},
		{ID: "2", Name: "tool_b", Input: json.RawMessage(`{}`)},
	}

	results := agent.executeTools(context.Background(), calls)

	r0 := results[0].(llm.ToolResultBlock)
	r1 := results[1].(llm.ToolResultBlock)

	if r0.Content != "result_a" {
		t.Errorf("results[0] expected 'result_a', got '%s'", r0.Content)
	}
	if r1.Content != "result_b" {
		t.Errorf("results[1] expected 'result_b', got '%s'", r1.Content)
	}
}

func TestExecuteTools_Parallel_IndependentFailure(t *testing.T) {
	tools := []Tool{
		{
			Definition: llm.ToolDef{Name: "good_tool", Description: "good"},
			Execute: func(ctx context.Context, input json.RawMessage) (string, error) {
				return "success", nil
			},
		},
		{
			Definition: llm.ToolDef{Name: "bad_tool", Description: "bad"},
			Execute: func(ctx context.Context, input json.RawMessage) (string, error) {
				return "", fmt.Errorf("tool failed")
			},
		},
	}

	mm := memory.NewMemoryManager(&mockMemStore{}, 4096)
	agent := NewAgent(&mockProv{}, mm, tools, nil)

	calls := []llm.ToolUseBlock{
		{ID: "1", Name: "good_tool", Input: json.RawMessage(`{}`)},
		{ID: "2", Name: "bad_tool", Input: json.RawMessage(`{}`)},
	}

	results := agent.executeTools(context.Background(), calls)

	r0 := results[0].(llm.ToolResultBlock)
	r1 := results[1].(llm.ToolResultBlock)

	if r0.IsError {
		t.Error("good_tool should not have error")
	}
	if !r1.IsError {
		t.Error("bad_tool should have error")
	}
}

// Test helpers
type mockProv struct{}

func (m *mockProv) CreateMessage(ctx context.Context, req llm.Request) (*llm.Response, error) {
	return &llm.Response{Content: []llm.Block{llm.TextBlock{Text: "ok"}}}, nil
}

type mockMemStore struct{}

func (m *mockMemStore) Save(ctx context.Context, entry memory.Entry) error { return nil }
func (m *mockMemStore) Search(ctx context.Context, q string, limit int) ([]memory.Entry, error) {
	return nil, nil
}
func (m *mockMemStore) List(ctx context.Context) ([]memory.Entry, error) { return nil, nil }
func (m *mockMemStore) Delete(ctx context.Context, id string) error      { return nil }

// Streaming tests

func TestAgent_RunStream_TextOnly(t *testing.T) {
	mockStream := &mockStreamProv{
		streamFunc: func(ctx context.Context, req llm.Request) (<-chan llm.StreamEvent, error) {
			ch := make(chan llm.StreamEvent, 3)
			ch <- llm.StreamEvent{Type: "text_delta", Text: "Hello"}
			ch <- llm.StreamEvent{Type: "text_delta", Text: " world"}
			ch <- llm.StreamEvent{Type: "done"}
			close(ch)
			return ch, nil
		},
	}

	mm := memory.NewMemoryManager(&mockMemStore{}, 4096)
	agent := NewAgent(mockStream, mm, nil, nil)

	var collected []string
	onEvent := func(event llm.StreamEvent) {
		if event.Type == "text_delta" {
			collected = append(collected, event.Text)
		}
	}

	reply, err := agent.RunStream(context.Background(), "hi", onEvent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if reply != "Hello world" {
		t.Errorf("expected 'Hello world', got '%s'", reply)
	}
	if len(collected) != 2 {
		t.Errorf("expected 2 events, got %d", len(collected))
	}
}

func TestAgent_RunStream_WithToolUse(t *testing.T) {
	callCount := 0
	mockStream := &mockStreamProv{
		streamFunc: func(ctx context.Context, req llm.Request) (<-chan llm.StreamEvent, error) {
			callCount++
			ch := make(chan llm.StreamEvent, 4)
			if callCount == 1 {
				ch <- llm.StreamEvent{Type: "text_delta", Text: "Reading..."}
				ch <- llm.StreamEvent{Type: "tool_use", ToolUse: &llm.ToolUseBlock{
					ID: "t1", Name: "read_file", Input: json.RawMessage(`{"path":"test.txt"}`),
				}}
				ch <- llm.StreamEvent{Type: "done"}
			} else {
				ch <- llm.StreamEvent{Type: "text_delta", Text: "File content: hello"}
				ch <- llm.StreamEvent{Type: "done"}
			}
			close(ch)
			return ch, nil
		},
	}

	readTool := Tool{
		Definition: llm.ToolDef{Name: "read_file", Description: "read"},
		Execute: func(ctx context.Context, input json.RawMessage) (string, error) {
			return "hello", nil
		},
	}

	mm := memory.NewMemoryManager(&mockMemStore{}, 4096)
	agent := NewAgent(mockStream, mm, []Tool{readTool}, nil)

	var events []llm.StreamEvent
	onEvent := func(event llm.StreamEvent) {
		events = append(events, event)
	}

	reply, err := agent.RunStream(context.Background(), "read test.txt", onEvent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if reply != "File content: hello" {
		t.Errorf("expected 'File content: hello', got '%s'", reply)
	}
	if callCount != 2 {
		t.Errorf("expected 2 LLM calls, got %d", callCount)
	}
}

func TestAgent_RunStream_FallbackToRun(t *testing.T) {
	nonStreamProv := &mockProv{}

	mm := memory.NewMemoryManager(&mockMemStore{}, 4096)
	agent := NewAgent(nonStreamProv, mm, nil, nil)

	var events []llm.StreamEvent
	onEvent := func(event llm.StreamEvent) {
		events = append(events, event)
	}

	reply, err := agent.RunStream(context.Background(), "hi", onEvent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if reply != "ok" {
		t.Errorf("expected 'ok', got '%s'", reply)
	}
	if len(events) != 1 || events[0].Text != "ok" {
		t.Errorf("expected fallback event with text 'ok', got %v", events)
	}
}

type mockStreamProv struct {
	streamFunc func(ctx context.Context, req llm.Request) (<-chan llm.StreamEvent, error)
}

func (m *mockStreamProv) CreateMessage(ctx context.Context, req llm.Request) (*llm.Response, error) {
	return &llm.Response{Content: []llm.Block{llm.TextBlock{Text: "non-stream"}}}, nil
}

func (m *mockStreamProv) CreateMessageStream(ctx context.Context, req llm.Request) (<-chan llm.StreamEvent, error) {
	return m.streamFunc(ctx, req)
}
