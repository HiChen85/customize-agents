package core

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/HiChen85/customize-agents/llm"
)

type ExecutorConfig struct {
	Timeout       time.Duration
	MaxRetries    int
	RetryDelay    time.Duration
	RetryableFunc func(err error) bool
}

type ToolExecutor struct {
	config ExecutorConfig
}

func NewToolExecutor(config ExecutorConfig) *ToolExecutor {
	if config.Timeout <= 0 {
		config.Timeout = 30 * time.Second
	}
	if config.RetryDelay <= 0 {
		config.RetryDelay = 1 * time.Second
	}
	return &ToolExecutor{config: config}
}

func (e *ToolExecutor) Execute(ctx context.Context, tool Tool, call llm.ToolUseBlock) llm.ToolResultBlock {
	var lastErr error
	for attempt := 0; attempt <= e.config.MaxRetries; attempt++ {
		if attempt > 0 {
			slog.Info("retrying tool", "tool", call.Name, "attempt", attempt)
			select {
			case <-ctx.Done():
				return llm.ToolResultBlock{ToolUseID: call.ID, Content: fmt.Sprintf("Error: context cancelled: %v", ctx.Err()), IsError: true}
			case <-time.After(e.config.RetryDelay):
			}
		}
		output, err := e.executeWithTimeout(ctx, tool, call)
		if err == nil {
			return llm.ToolResultBlock{ToolUseID: call.ID, Content: output, IsError: false}
		}
		lastErr = err
		if e.config.RetryableFunc == nil || !e.config.RetryableFunc(err) {
			break
		}
	}
	slog.Error("tool execution failed", "tool", call.Name, "error", lastErr)
	return llm.ToolResultBlock{ToolUseID: call.ID, Content: fmt.Sprintf("Error: %v", lastErr), IsError: true}
}

func (e *ToolExecutor) executeWithTimeout(ctx context.Context, tool Tool, call llm.ToolUseBlock) (string, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, e.config.Timeout)
	defer cancel()
	type result struct {
		output string
		err    error
	}
	ch := make(chan result, 1)
	go func() {
		out, err := tool.Execute(timeoutCtx, call.Input)
		ch <- result{out, err}
	}()
	select {
	case <-timeoutCtx.Done():
		return "", fmt.Errorf("tool '%s' timed out after %v", call.Name, e.config.Timeout)
	case r := <-ch:
		return r.output, r.err
	}
}
