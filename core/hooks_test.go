package core

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/haichen-zhang/customize-agents/config"
	"github.com/haichen-zhang/customize-agents/llm"
	"github.com/haichen-zhang/customize-agents/memory"
)

func TestGoHook_Handle(t *testing.T) {
	called := false
	hook := NewGoHook(func(ctx context.Context, p HookPayload) error {
		called = true
		if p.Event != BeforeToolCall {
			t.Errorf("expected BeforeToolCall, got %s", p.Event)
		}
		if p.ToolName != "read_file" {
			t.Errorf("expected read_file, got %s", p.ToolName)
		}
		return nil
	})

	err := hook.Handle(context.Background(), HookPayload{
		Event:    BeforeToolCall,
		ToolName: "read_file",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("hook was not called")
	}
}

func TestHookRegistry_Fire_AbortableEvent(t *testing.T) {
	registry := NewHookRegistry()

	registry.Register(BeforeToolCall, NewGoHook(func(ctx context.Context, p HookPayload) error {
		return errors.New("blocked by policy")
	}))

	err := registry.Fire(context.Background(), HookPayload{
		Event:    BeforeToolCall,
		ToolName: "exec",
	})
	if err == nil {
		t.Fatal("expected error from abortable hook")
	}
	if err.Error() != "blocked by policy" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestHookRegistry_Fire_NonAbortableEvent(t *testing.T) {
	registry := NewHookRegistry()

	registry.Register(AfterToolCall, NewGoHook(func(ctx context.Context, p HookPayload) error {
		return errors.New("log failure")
	}))

	err := registry.Fire(context.Background(), HookPayload{
		Event:    AfterToolCall,
		ToolName: "exec",
	})
	if err != nil {
		t.Fatalf("non-abortable event should not return error, got: %v", err)
	}
}

func TestHookRegistry_Fire_MultipleHandlers_OrderPreserved(t *testing.T) {
	registry := NewHookRegistry()
	var order []int

	registry.Register(BeforeToolCall, NewGoHook(func(ctx context.Context, p HookPayload) error {
		order = append(order, 1)
		return nil
	}))
	registry.Register(BeforeToolCall, NewGoHook(func(ctx context.Context, p HookPayload) error {
		order = append(order, 2)
		return nil
	}))

	_ = registry.Fire(context.Background(), HookPayload{Event: BeforeToolCall})

	if len(order) != 2 || order[0] != 1 || order[1] != 2 {
		t.Errorf("expected [1, 2], got %v", order)
	}
}

func TestHookRegistry_Fire_AbortStopsSubsequentHandlers(t *testing.T) {
	registry := NewHookRegistry()
	secondCalled := false

	registry.Register(BeforeToolCall, NewGoHook(func(ctx context.Context, p HookPayload) error {
		return errors.New("abort")
	}))
	registry.Register(BeforeToolCall, NewGoHook(func(ctx context.Context, p HookPayload) error {
		secondCalled = true
		return nil
	}))

	_ = registry.Fire(context.Background(), HookPayload{Event: BeforeToolCall})

	if secondCalled {
		t.Fatal("second handler should not have been called after abort")
	}
}

func TestHookRegistry_Fire_NoHandlers(t *testing.T) {
	registry := NewHookRegistry()
	err := registry.Fire(context.Background(), HookPayload{Event: OnError})
	if err != nil {
		t.Fatalf("fire with no handlers should return nil, got: %v", err)
	}
}

func TestShellHook_Handle_Success(t *testing.T) {
	hook := &ShellHook{
		Command:  "cat",
		Timeout:  5 * time.Second,
		CanAbort: false,
	}

	err := hook.Handle(context.Background(), HookPayload{
		Event:    AfterToolCall,
		ToolName: "read_file",
		Output:   "file content",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestShellHook_Handle_AbortOnNonZeroExit(t *testing.T) {
	hook := &ShellHook{
		Command:  "exit 1",
		Timeout:  5 * time.Second,
		CanAbort: true,
	}

	err := hook.Handle(context.Background(), HookPayload{
		Event:    BeforeToolCall,
		ToolName: "exec",
	})
	if err == nil {
		t.Fatal("expected error from non-zero exit with can_abort=true")
	}
}

func TestShellHook_Handle_NoAbortOnNonZeroExit(t *testing.T) {
	hook := &ShellHook{
		Command:  "exit 1",
		Timeout:  5 * time.Second,
		CanAbort: false,
	}

	err := hook.Handle(context.Background(), HookPayload{
		Event:    AfterToolCall,
		ToolName: "exec",
	})
	if err != nil {
		t.Fatalf("can_abort=false should not return error, got: %v", err)
	}
}

func TestShellHook_Handle_Timeout(t *testing.T) {
	hook := &ShellHook{
		Command:  "sleep 10",
		Timeout:  100 * time.Millisecond,
		CanAbort: true,
	}

	err := hook.Handle(context.Background(), HookPayload{
		Event:    BeforeToolCall,
		ToolName: "exec",
	})
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestHookRegistry_LoadFromConfig(t *testing.T) {
	cfg := map[string][]config.HookConfig{
		"before_tool_call": {
			{Command: "echo hello", Timeout: 5 * time.Second, CanAbort: true},
		},
		"after_tool_call": {
			{Command: "echo done", Timeout: 3 * time.Second, CanAbort: false},
		},
	}

	registry := NewHookRegistry()
	err := registry.LoadFromConfig(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = registry.Fire(context.Background(), HookPayload{Event: BeforeToolCall, ToolName: "test"})
	if err != nil {
		t.Fatalf("unexpected error firing before_tool_call: %v", err)
	}
}

func TestHookRegistry_LoadFromConfig_InvalidEvent(t *testing.T) {
	cfg := map[string][]config.HookConfig{
		"invalid_event": {
			{Command: "echo hello", Timeout: 5 * time.Second},
		},
	}

	registry := NewHookRegistry()
	err := registry.LoadFromConfig(cfg)
	if err == nil {
		t.Fatal("expected error for invalid event name")
	}
}

func TestAgent_HooksFireDuringRun(t *testing.T) {
	var events []EventType

	registry := NewHookRegistry()
	registry.Register(OnSessionStart, NewGoHook(func(ctx context.Context, p HookPayload) error {
		events = append(events, p.Event)
		return nil
	}))
	registry.Register(BeforeLLMCall, NewGoHook(func(ctx context.Context, p HookPayload) error {
		events = append(events, p.Event)
		return nil
	}))
	registry.Register(AfterLLMCall, NewGoHook(func(ctx context.Context, p HookPayload) error {
		events = append(events, p.Event)
		return nil
	}))

	mockProvider := &mockLLMProvider{response: &llm.Response{
		Content: []llm.Block{llm.TextBlock{Text: "hello"}},
	}}

	mm := memory.NewMemoryManager(&mockMemoryStore{}, 4096)
	agent := NewAgent(mockProvider, mm, nil, nil)
	agent.SetHookRegistry(registry)

	reply, err := agent.Run(context.Background(), "hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reply != "hello" {
		t.Errorf("expected 'hello', got '%s'", reply)
	}

	expected := []EventType{OnSessionStart, BeforeLLMCall, AfterLLMCall}
	if len(events) != len(expected) {
		t.Fatalf("expected %d events, got %d: %v", len(expected), len(events), events)
	}
	for i, e := range expected {
		if events[i] != e {
			t.Errorf("event[%d]: expected %s, got %s", i, e, events[i])
		}
	}
}

type mockLLMProvider struct {
	response *llm.Response
}

func (m *mockLLMProvider) CreateMessage(ctx context.Context, req llm.Request) (*llm.Response, error) {
	return m.response, nil
}

type mockMemoryStore struct{}

func (m *mockMemoryStore) Save(ctx context.Context, entry memory.Entry) error { return nil }
func (m *mockMemoryStore) Search(ctx context.Context, q string, limit int) ([]memory.Entry, error) {
	return nil, nil
}
func (m *mockMemoryStore) List(ctx context.Context) ([]memory.Entry, error) { return nil, nil }
func (m *mockMemoryStore) Delete(ctx context.Context, id string) error       { return nil }
