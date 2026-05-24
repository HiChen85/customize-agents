package core

import (
	"context"
	"errors"
	"testing"
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
