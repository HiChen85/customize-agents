package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "agent.yaml")

	content := `
providers:
  anthropic:
    api_key: "sk-test"
    base_url: "https://api.anthropic.com"
  deepseek:
    api_key: "sk-ds-test"
    base_url: "https://api.deepseek.com"

active_provider: anthropic
model: claude-sonnet-4-20250514
max_tokens: 4096

skills_dir: "./skills"
active_skills:
  - example

memory:
  store: file
  dir: "./.agent/memory"

server:
  port: 9090

mcp_servers:
  - name: filesystem
    command: "npx @anthropic/mcp-filesystem /tmp"
    transport: stdio
`
	os.WriteFile(cfgPath, []byte(content), 0644)

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.ActiveProvider != "anthropic" {
		t.Errorf("expected active_provider 'anthropic', got '%s'", cfg.ActiveProvider)
	}
	if cfg.Model != "claude-sonnet-4-20250514" {
		t.Errorf("expected model 'claude-sonnet-4-20250514', got '%s'", cfg.Model)
	}
	if cfg.MaxTokens != 4096 {
		t.Errorf("expected max_tokens 4096, got %d", cfg.MaxTokens)
	}
	if cfg.Providers["anthropic"].APIKey != "sk-test" {
		t.Errorf("expected api_key 'sk-test', got '%s'", cfg.Providers["anthropic"].APIKey)
	}
	if cfg.SkillsDir != "./skills" {
		t.Errorf("expected skills_dir './skills', got '%s'", cfg.SkillsDir)
	}
	if len(cfg.ActiveSkills) != 1 || cfg.ActiveSkills[0] != "example" {
		t.Errorf("expected active_skills [example], got %v", cfg.ActiveSkills)
	}
	if cfg.Memory.Store != "file" {
		t.Errorf("expected memory store 'file', got '%s'", cfg.Memory.Store)
	}
	if cfg.Server.Port != 9090 {
		t.Errorf("expected server port 9090, got %d", cfg.Server.Port)
	}
	if len(cfg.MCPServers) != 1 || cfg.MCPServers[0].Name != "filesystem" {
		t.Errorf("expected 1 mcp server named 'filesystem', got %v", cfg.MCPServers)
	}
}

func TestLoad_WithHooks(t *testing.T) {
	content := `
providers:
  anthropic:
    api_key: "test-key"
    base_url: "https://api.anthropic.com"
active_provider: anthropic
model: claude-sonnet-4-20250514
hooks:
  before_tool_call:
    - command: "./audit.sh"
      timeout: 10s
      can_abort: true
  after_tool_call:
    - command: "./log.sh"
      timeout: 3s
`
	tmpFile := filepath.Join(t.TempDir(), "config.yaml")
	os.WriteFile(tmpFile, []byte(content), 0644)

	cfg, err := Load(tmpFile)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if cfg.Hooks == nil {
		t.Fatal("expected hooks to be parsed")
	}

	btc := cfg.Hooks["before_tool_call"]
	if len(btc) != 1 {
		t.Fatalf("expected 1 before_tool_call hook, got %d", len(btc))
	}
	if btc[0].Command != "./audit.sh" {
		t.Errorf("expected command './audit.sh', got '%s'", btc[0].Command)
	}
	if btc[0].Timeout != 10*time.Second {
		t.Errorf("expected timeout 10s, got %v", btc[0].Timeout)
	}
	if !btc[0].CanAbort {
		t.Error("expected can_abort=true")
	}

	atc := cfg.Hooks["after_tool_call"]
	if len(atc) != 1 {
		t.Fatalf("expected 1 after_tool_call hook, got %d", len(atc))
	}
	if atc[0].CanAbort {
		t.Error("expected can_abort=false for after_tool_call hook")
	}
}
