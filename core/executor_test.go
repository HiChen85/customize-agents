package core

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/haichen-zhang/customize-agents/llm"
)

func TestToolExecutor_Timeout(t *testing.T) {
	slowTool := Tool{
		Definition: llm.ToolDef{Name: "slow_tool"},
		Execute: func(ctx context.Context, input json.RawMessage) (string, error) {
			select {
			case <-time.After(5 * time.Second):
				return "done", nil
			case <-ctx.Done():
				return "", ctx.Err()
			}
		},
	}
	executor := NewToolExecutor(ExecutorConfig{Timeout: 50 * time.Millisecond, MaxRetries: 0})
	result := executor.Execute(context.Background(), slowTool, llm.ToolUseBlock{ID: "t1", Name: "slow_tool", Input: json.RawMessage(`{}`)})
	if !result.IsError {
		t.Error("expected error for timeout")
	}
}

func TestToolExecutor_RetryOnFailure(t *testing.T) {
	var calls int32
	flakeyTool := Tool{
		Definition: llm.ToolDef{Name: "flakey"},
		Execute: func(ctx context.Context, input json.RawMessage) (string, error) {
			if atomic.AddInt32(&calls, 1) <= 2 {
				return "", fmt.Errorf("temporary failure")
			}
			return "success", nil
		},
	}
	executor := NewToolExecutor(ExecutorConfig{Timeout: 1 * time.Second, MaxRetries: 3, RetryDelay: 10 * time.Millisecond, RetryableFunc: func(err error) bool { return true }})
	result := executor.Execute(context.Background(), flakeyTool, llm.ToolUseBlock{ID: "t1", Name: "flakey", Input: json.RawMessage(`{}`)})
	if result.IsError {
		t.Errorf("expected success, got: %s", result.Content)
	}
	if result.Content != "success" {
		t.Errorf("expected 'success', got '%s'", result.Content)
	}
}

func TestToolExecutor_NoRetryOnSuccess(t *testing.T) {
	var calls int32
	goodTool := Tool{
		Definition: llm.ToolDef{Name: "good"},
		Execute: func(ctx context.Context, input json.RawMessage) (string, error) {
			atomic.AddInt32(&calls, 1)
			return "ok", nil
		},
	}
	executor := NewToolExecutor(ExecutorConfig{Timeout: 1 * time.Second, MaxRetries: 3})
	result := executor.Execute(context.Background(), goodTool, llm.ToolUseBlock{ID: "t1", Name: "good", Input: json.RawMessage(`{}`)})
	if result.IsError {
		t.Errorf("unexpected error: %s", result.Content)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Errorf("expected 1 call, got %d", calls)
	}
}
