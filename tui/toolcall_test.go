package tui

import (
	"encoding/json"
	"testing"
	"time"
)

func TestToolDrawer_Start(t *testing.T) {
	d := NewToolDrawer("tool-1", "read_file", json.RawMessage(`{"path":"main.go"}`))
	if d.state != ToolStatePending {
		t.Fatalf("expected pending, got %v", d.state)
	}
	if !d.expanded {
		t.Fatal("expected auto-expanded on start")
	}
}

func TestToolDrawer_Complete(t *testing.T) {
	d := NewToolDrawer("tool-1", "read_file", json.RawMessage(`{"path":"main.go"}`))
	d.Complete("file contents here", 120*time.Millisecond, false)
	if d.state != ToolStateDone {
		t.Fatalf("expected done, got %v", d.state)
	}
	if d.expanded {
		t.Fatal("expected auto-collapsed on complete")
	}
	if d.duration != 120*time.Millisecond {
		t.Fatalf("expected 120ms, got %v", d.duration)
	}
}

func TestToolDrawer_Error(t *testing.T) {
	d := NewToolDrawer("tool-1", "exec", json.RawMessage(`{"command":"ls"}`))
	d.Complete("permission denied", 50*time.Millisecond, true)
	if d.state != ToolStateError {
		t.Fatalf("expected error, got %v", d.state)
	}
	if !d.expanded {
		t.Fatal("expected auto-expanded on error to show full error message")
	}
}

func TestToolDrawer_ToggleExpand(t *testing.T) {
	d := NewToolDrawer("tool-1", "read_file", json.RawMessage(`{"path":"main.go"}`))
	d.Complete("contents", 100*time.Millisecond, false)
	if d.expanded {
		t.Fatal("should be collapsed after complete")
	}
	d.ToggleExpand()
	if !d.expanded {
		t.Fatal("should be expanded after toggle")
	}
	d.ToggleExpand()
	if d.expanded {
		t.Fatal("should be collapsed after second toggle")
	}
}
