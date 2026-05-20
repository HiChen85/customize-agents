package core

import (
	"encoding/json"
	"testing"
)

func TestPermissionHandler_AutoApprove(t *testing.T) {
	handler := NewPermissionHandler(PermissionConfig{AutoApprove: []string{"read_file", "memory_search"}})
	if !handler.CheckPermission("read_file", json.RawMessage(`{}`)) {
		t.Error("expected auto-approve for read_file")
	}
	if handler.CheckPermission("exec", json.RawMessage(`{}`)) {
		t.Error("exec should not be auto-approved")
	}
}

func TestPermissionHandler_PromptFunc(t *testing.T) {
	handler := NewPermissionHandler(PermissionConfig{
		AutoApprove: []string{},
		PromptFunc:  func(tool string, input json.RawMessage) bool { return tool == "exec" },
	})
	if !handler.CheckPermission("exec", json.RawMessage(`{}`)) {
		t.Error("expected approval via prompt func")
	}
	if handler.CheckPermission("dangerous", json.RawMessage(`{}`)) {
		t.Error("expected denial")
	}
}
