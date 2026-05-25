package core

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/haichen-zhang/customize-agents/llm"
	"github.com/haichen-zhang/customize-agents/memory"
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
