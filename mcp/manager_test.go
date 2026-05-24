package mcp

import (
	"encoding/json"
	"testing"
)

func TestMCPManager_ConvertTool(t *testing.T) {
	mgr := NewMCPManager()

	td := ToolDefinition{
		Name:        "read_file",
		Description: "Read a file from disk",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`),
	}

	tool := mgr.convertTool("filesystem", td, nil)

	if tool.Definition.Name != "read_file" {
		t.Errorf("expected name 'read_file', got '%s'", tool.Definition.Name)
	}
	if tool.Definition.Description != "Read a file from disk" {
		t.Errorf("unexpected description: %s", tool.Definition.Description)
	}
}

func TestMCPManager_ConvertTool_WithConflict(t *testing.T) {
	mgr := NewMCPManager()

	existingTools := map[string]bool{"read_file": true}

	td := ToolDefinition{
		Name:        "read_file",
		Description: "Read a file",
		InputSchema: json.RawMessage(`{}`),
	}

	tool := mgr.convertToolWithConflictCheck("filesystem", td, existingTools, nil)

	if tool.Definition.Name != "filesystem_read_file" {
		t.Errorf("expected prefixed name 'filesystem_read_file', got '%s'", tool.Definition.Name)
	}
}

func TestMCPManager_GetTools_Empty(t *testing.T) {
	mgr := NewMCPManager()
	tools := mgr.GetTools(nil)
	if len(tools) != 0 {
		t.Errorf("expected 0 tools, got %d", len(tools))
	}
}

func TestMCPManager_Close_Empty(t *testing.T) {
	mgr := NewMCPManager()
	err := mgr.Close()
	if err != nil {
		t.Fatalf("close on empty manager should not error: %v", err)
	}
}
