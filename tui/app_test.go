package tui

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestApp_HandleCommand_Help(t *testing.T) {
	app := NewApp(nil, nil, nil, "test-model", 4096)
	app.chatView = NewChatViewport(80, 20)

	result, _ := app.handleCommand("/help")
	updated := result.(AppModel)
	if len(updated.chatView.items) == 0 {
		t.Fatal("expected help message in viewport")
	}
	sys, ok := updated.chatView.items[0].(*SystemMessage)
	if !ok {
		t.Fatal("expected SystemMessage")
	}
	if !strings.Contains(sys.Text, "/skill") {
		t.Fatal("help should mention /skill command")
	}
}

func TestApp_HandleCommand_Clear(t *testing.T) {
	app := NewApp(nil, nil, nil, "test-model", 4096)
	app.chatView = NewChatViewport(80, 20)
	app.chatView.AppendItem(&UserMessage{Text: "hello"})

	if len(app.chatView.items) != 1 {
		t.Fatal("expected 1 item before clear")
	}

	result, _ := app.handleCommand("/clear")
	updated := result.(AppModel)
	if len(updated.chatView.items) != 0 {
		t.Fatal("expected 0 items after clear")
	}
}

func TestApp_HandleCommand_Unknown(t *testing.T) {
	app := NewApp(nil, nil, nil, "test-model", 4096)
	app.chatView = NewChatViewport(80, 20)

	result, _ := app.handleCommand("/foobar")
	updated := result.(AppModel)
	if len(updated.chatView.items) == 0 {
		t.Fatal("expected error message for unknown command")
	}
	sys, ok := updated.chatView.items[0].(*SystemMessage)
	if !ok {
		t.Fatal("expected SystemMessage")
	}
	if !strings.Contains(sys.Text, "Unknown command") {
		t.Fatalf("unexpected message: %s", sys.Text)
	}
}

func TestToolTracking_ConcurrentTools(t *testing.T) {
	app := NewApp(nil, nil, nil, "test-model", 4096)

	app.toolDrawers = make(map[string]*ToolDrawer)
	app.toolIndex = nil

	id1 := "read_file-1"
	id2 := "exec-2"

	d1 := NewToolDrawer(id1, "read_file", json.RawMessage(`{"path":"a.go"}`))
	d2 := NewToolDrawer(id2, "exec", json.RawMessage(`{"command":"ls"}`))

	app.toolDrawers[id1] = d1
	app.toolIndex = append(app.toolIndex, id1)
	app.toolDrawers[id2] = d2
	app.toolIndex = append(app.toolIndex, id2)

	d1.Complete("contents", 100*time.Millisecond, false)
	if d1.state != ToolStateDone {
		t.Fatal("d1 should be done")
	}
	if d2.state != ToolStatePending {
		t.Fatal("d2 should still be pending")
	}

	d2.Complete("not found", 50*time.Millisecond, true)
	if d2.state != ToolStateError {
		t.Fatal("d2 should be error")
	}
}
