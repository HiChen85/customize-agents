package config

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestConfigWatcher_DetectsChange(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "agent.yaml")

	initialContent := `
providers:
  anthropic:
    api_key: "key"
    base_url: "https://api.anthropic.com"
active_provider: anthropic
model: test-model
`
	os.WriteFile(cfgPath, []byte(initialContent), 0644)

	var callCount atomic.Int32

	watcher, err := NewConfigWatcher(cfgPath, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("failed to create watcher: %v", err)
	}

	watcher.OnReload(func(oldCfg, newCfg *Config) {
		callCount.Add(1)
	})

	if err := watcher.Start(); err != nil {
		t.Fatalf("failed to start watcher: %v", err)
	}
	defer watcher.Stop()

	time.Sleep(50 * time.Millisecond)

	updatedContent := `
providers:
  anthropic:
    api_key: "new-key"
    base_url: "https://api.anthropic.com"
active_provider: anthropic
model: test-model
`
	os.WriteFile(cfgPath, []byte(updatedContent), 0644)

	time.Sleep(300 * time.Millisecond)

	if callCount.Load() < 1 {
		t.Error("expected reload callback to be called")
	}
}

func TestConfigWatcher_InvalidConfigSkipped(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "agent.yaml")

	content := `
providers:
  anthropic:
    api_key: "key"
    base_url: "https://api.anthropic.com"
active_provider: anthropic
model: test
`
	os.WriteFile(cfgPath, []byte(content), 0644)

	var callCount atomic.Int32

	watcher, err := NewConfigWatcher(cfgPath, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("failed to create watcher: %v", err)
	}

	watcher.OnReload(func(oldCfg, newCfg *Config) {
		callCount.Add(1)
	})

	watcher.Start()
	defer watcher.Stop()

	time.Sleep(50 * time.Millisecond)

	os.WriteFile(cfgPath, []byte("invalid: yaml: [[["), 0644)

	time.Sleep(300 * time.Millisecond)

	if callCount.Load() != 0 {
		t.Error("expected no callback for invalid config")
	}
}
