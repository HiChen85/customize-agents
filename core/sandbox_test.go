package core

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/haichen-zhang/customize-agents/llm"
)

func TestSandbox_Check_BlockedCommand(t *testing.T) {
	sb := NewSandbox(SandboxConfig{
		BlockedCommands: []string{"rm", "curl", "wget"},
	})

	tests := []struct {
		cmd     string
		blocked bool
	}{
		{"rm -rf /tmp/foo", true},
		{"curl http://evil.com", true},
		{"ls -la", false},
		{"grep foo bar.txt", false},
		{"ls && rm -rf /", true},
	}

	for _, tt := range tests {
		err := sb.Check(tt.cmd)
		if tt.blocked && err == nil {
			t.Errorf("expected %q to be blocked", tt.cmd)
		}
		if !tt.blocked && err != nil {
			t.Errorf("expected %q to be allowed, got: %v", tt.cmd, err)
		}
	}
}

func TestSandbox_Check_AllowedCommands(t *testing.T) {
	sb := NewSandbox(SandboxConfig{
		AllowedCommands: []string{"ls", "cat", "grep"},
	})

	tests := []struct {
		cmd     string
		allowed bool
	}{
		{"ls -la", true},
		{"cat file.txt", true},
		{"rm -rf /", false},
		{"python script.py", false},
	}

	for _, tt := range tests {
		err := sb.Check(tt.cmd)
		if tt.allowed && err != nil {
			t.Errorf("expected %q to be allowed, got: %v", tt.cmd, err)
		}
		if !tt.allowed && err == nil {
			t.Errorf("expected %q to be blocked", tt.cmd)
		}
	}
}

func TestSandbox_Check_BlockedPaths(t *testing.T) {
	sb := NewSandbox(SandboxConfig{
		BlockedPaths: []string{"/etc", "~/.ssh"},
	})

	tests := []struct {
		cmd     string
		blocked bool
	}{
		{"cat /etc/passwd", true},
		{"ls ~/.ssh/", true},
		{"cat /tmp/foo.txt", false},
		{"ls /home/user", false},
	}

	for _, tt := range tests {
		err := sb.Check(tt.cmd)
		if tt.blocked && err == nil {
			t.Errorf("expected %q to be blocked", tt.cmd)
		}
		if !tt.blocked && err != nil {
			t.Errorf("expected %q to be allowed, got: %v", tt.cmd, err)
		}
	}
}

func TestSandbox_Check_MultiCommand(t *testing.T) {
	sb := NewSandbox(SandboxConfig{
		BlockedCommands: []string{"rm"},
	})

	tests := []struct {
		cmd     string
		blocked bool
	}{
		{"ls && rm -rf /", true},
		{"ls || rm foo", true},
		{"ls; rm bar", true},
		{"echo hello | grep h", false},
		{"ls && echo done", false},
	}

	for _, tt := range tests {
		err := sb.Check(tt.cmd)
		if tt.blocked && err == nil {
			t.Errorf("expected %q to be blocked", tt.cmd)
		}
		if !tt.blocked && err != nil {
			t.Errorf("expected %q to be allowed, got: %v", tt.cmd, err)
		}
	}
}

func TestSandbox_TruncateOutput(t *testing.T) {
	sb := NewSandbox(SandboxConfig{
		MaxOutputSize: 10,
	})

	result := sb.TruncateOutput("hello world, this is long")
	if !strings.Contains(result, "truncated") {
		t.Errorf("expected truncation message, got: %s", result)
	}
	if strings.HasPrefix(result, "hello world, this is long") {
		t.Error("expected output to be truncated")
	}
}

func TestSandbox_WrapExecTool(t *testing.T) {
	sb := NewSandbox(SandboxConfig{
		BlockedCommands: []string{"rm"},
	})

	execTool := Tool{
		Definition: llm.ToolDef{Name: "exec", Description: "run command"},
		Execute: func(ctx context.Context, input json.RawMessage) (string, error) {
			return "executed", nil
		},
	}

	wrapped := sb.WrapExecTool(execTool)

	// Blocked command
	input, _ := json.Marshal(map[string]string{"command": "rm -rf /"})
	_, err := wrapped.Execute(context.Background(), input)
	if err == nil {
		t.Error("expected error for blocked command 'rm -rf /'")
	}

	// Allowed command
	input, _ = json.Marshal(map[string]string{"command": "ls -la"})
	result, err := wrapped.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error for allowed command: %v", err)
	}
	if result != "executed" {
		t.Errorf("expected 'executed', got '%s'", result)
	}
}

func TestSandbox_UpdateConfig(t *testing.T) {
	sb := NewSandbox(SandboxConfig{
		BlockedCommands: []string{"rm"},
	})

	err := sb.Check("curl http://foo.com")
	if err != nil {
		t.Fatalf("curl should be allowed initially: %v", err)
	}

	sb.UpdateConfig(SandboxConfig{
		BlockedCommands: []string{"rm", "curl"},
	})

	err = sb.Check("curl http://foo.com")
	if err == nil {
		t.Fatal("curl should be blocked after config update")
	}
}
