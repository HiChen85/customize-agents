package core

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"
)

type ShellHook struct {
	Command  string
	Timeout  time.Duration
	CanAbort bool
}

type shellPayload struct {
	Event    string `json:"event"`
	ToolName string `json:"tool_name,omitempty"`
	Input    string `json:"input,omitempty"`
	Output   string `json:"output,omitempty"`
	Error    string `json:"error,omitempty"`
	Duration string `json:"duration,omitempty"`
}

func (h *ShellHook) Handle(ctx context.Context, payload HookPayload) error {
	timeout := h.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(timeoutCtx, "bash", "-c", h.Command)

	sp := shellPayload{
		Event:    string(payload.Event),
		ToolName: payload.ToolName,
		Output:   payload.Output,
		Duration: payload.Duration.String(),
	}
	if payload.Input != nil {
		sp.Input = string(payload.Input)
	}
	if payload.Error != nil {
		sp.Error = payload.Error.Error()
	}

	stdinData, _ := json.Marshal(sp)
	cmd.Stdin = bytes.NewReader(stdinData)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		if timeoutCtx.Err() != nil {
			if h.CanAbort {
				return fmt.Errorf("hook command timed out after %v", timeout)
			}
			return nil
		}
		if h.CanAbort {
			return fmt.Errorf("hook command failed: %s (stderr: %s)", err, stderr.String())
		}
		return nil
	}
	return nil
}
