package core

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/HiChen85/customize-agents/config"
	"github.com/HiChen85/customize-agents/llm"
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

var validEvents = map[string]EventType{
	"on_session_start":     OnSessionStart,
	"before_llm_call":      BeforeLLMCall,
	"after_llm_call":       AfterLLMCall,
	"before_tool_call":     BeforeToolCall,
	"after_tool_call":      AfterToolCall,
	"on_permission_denied": OnPermissionDenied,
	"on_error":             OnError,
}

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

func (r *HookRegistry) LoadFromConfig(hooksCfg map[string][]config.HookConfig) error {
	for eventName, hooks := range hooksCfg {
		event, ok := validEvents[eventName]
		if !ok {
			return fmt.Errorf("unknown hook event: %s", eventName)
		}
		for _, hcfg := range hooks {
			handler := &ShellHook{
				Command:  hcfg.Command,
				Timeout:  hcfg.Timeout,
				CanAbort: hcfg.CanAbort,
			}
			r.Register(event, handler)
		}
	}
	return nil
}

func (r *HookRegistry) Reload(hooksCfg map[string][]config.HookConfig) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	newHooks := make(map[EventType][]HookHandler)
	for event, handlers := range r.hooks {
		for _, h := range handlers {
			if _, isShell := h.(*ShellHook); !isShell {
				newHooks[event] = append(newHooks[event], h)
			}
		}
	}
	r.hooks = newHooks

	for eventName, hooks := range hooksCfg {
		event, ok := validEvents[eventName]
		if !ok {
			return fmt.Errorf("unknown hook event: %s", eventName)
		}
		for _, hcfg := range hooks {
			handler := &ShellHook{
				Command:  hcfg.Command,
				Timeout:  hcfg.Timeout,
				CanAbort: hcfg.CanAbort,
			}
			r.hooks[event] = append(r.hooks[event], handler)
		}
	}
	return nil
}
