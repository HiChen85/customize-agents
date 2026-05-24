package core

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/haichen-zhang/customize-agents/llm"
)

type EventType string

const (
	OnSessionStart     EventType = "on_session_start"
	BeforeLLMCall      EventType = "before_llm_call"
	AfterLLMCall       EventType = "after_llm_call"
	BeforeToolCall     EventType = "before_tool_call"
	AfterToolCall      EventType = "after_tool_call"
	OnPermissionDenied EventType = "on_permission_denied"
	OnError            EventType = "on_error"
)

var abortableEvents = map[EventType]bool{
	OnSessionStart: true,
	BeforeLLMCall:  true,
	BeforeToolCall: true,
}

func IsAbortable(event EventType) bool {
	return abortableEvents[event]
}

type HookPayload struct {
	Event     EventType
	ToolName  string
	Input     json.RawMessage
	Output    string
	Error     error
	Duration  time.Duration
	Request   *llm.Request
	Response  *llm.Response
	UserInput string
}

type HookHandler interface {
	Handle(ctx context.Context, payload HookPayload) error
}

type GoHook struct {
	fn func(context.Context, HookPayload) error
}

func NewGoHook(fn func(context.Context, HookPayload) error) *GoHook {
	return &GoHook{fn: fn}
}

func (h *GoHook) Handle(ctx context.Context, payload HookPayload) error {
	return h.fn(ctx, payload)
}

type HookRegistry struct {
	hooks map[EventType][]HookHandler
	mu    sync.RWMutex
}

func NewHookRegistry() *HookRegistry {
	return &HookRegistry{
		hooks: make(map[EventType][]HookHandler),
	}
}

func (r *HookRegistry) Register(event EventType, handler HookHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hooks[event] = append(r.hooks[event], handler)
}

func (r *HookRegistry) Fire(ctx context.Context, payload HookPayload) error {
	r.mu.RLock()
	handlers := r.hooks[payload.Event]
	r.mu.RUnlock()

	if len(handlers) == 0 {
		return nil
	}

	abortable := IsAbortable(payload.Event)
	for _, handler := range handlers {
		err := handler.Handle(ctx, payload)
		if err != nil {
			if abortable {
				return err
			}
			slog.Warn("hook handler error (non-abortable)", "event", payload.Event, "error", err)
		}
	}
	return nil
}
