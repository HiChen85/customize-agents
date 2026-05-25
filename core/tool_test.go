package core

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/HiChen85/customize-agents/memory"
)

func TestExecTool(t *testing.T) {
	tool := NewExecTool()

	input, _ := json.Marshal(map[string]string{"command": "echo hello"})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "hello\n" {
		t.Errorf("expected 'hello\\n', got %q", result)
	}
}

func TestReadFileTool(t *testing.T) {
	dir := t.TempDir()
	testFile := filepath.Join(dir, "test.txt")
	os.WriteFile(testFile, []byte("file content here"), 0644)

	tool := NewReadFileTool()
	input, _ := json.Marshal(map[string]string{"path": testFile})
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "file content here" {
		t.Errorf("expected 'file content here', got %q", result)
	}
}

func TestMemorySaveTool(t *testing.T) {
	dir := t.TempDir()
	store, _ := memory.NewFileStore(dir)

	tool := NewMemorySaveTool(store)
	input, _ := json.Marshal(map[string]any{
		"content": "user likes Go",
		"tags":    []string{"preference"},
	})

	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty result")
	}

	entries, _ := store.List(context.Background())
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Content != "user likes Go" {
		t.Errorf("expected content 'user likes Go', got '%s'", entries[0].Content)
	}
}

func TestMemorySearchTool(t *testing.T) {
	dir := t.TempDir()
	store, _ := memory.NewFileStore(dir)
	store.Save(context.Background(), memory.Entry{
		ID: "e1", Content: "user prefers dark mode", Tags: []string{"preference"},
	})

	tool := NewMemorySearchTool(store)
	input, _ := json.Marshal(map[string]any{"query": "dark", "limit": 5})

	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty result")
	}
}
