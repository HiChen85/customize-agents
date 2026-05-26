package core

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/HiChen85/customize-agents/llm"
	"github.com/HiChen85/customize-agents/memory"
	"github.com/HiChen85/customize-agents/skill"
)

type Agent struct {
	llm               llm.Provider
	memory            *memory.MemoryManager
	tools             []Tool
	skillRegistry     *skill.SkillRegistry
	permissionHandler *PermissionHandler
	executor          *ToolExecutor
	hooks             *HookRegistry
	lifecycle         *Lifecycle
}

func NewAgent(provider llm.Provider, mm *memory.MemoryManager, tools []Tool, registry *skill.SkillRegistry) *Agent {
	return &Agent{
		llm:           provider,
		memory:        mm,
		tools:         tools,
		skillRegistry: registry,
	}
}

func (a *Agent) SetHookRegistry(r *HookRegistry) { a.hooks = r }
func (a *Agent) SetLifecycle(l *Lifecycle)        { a.lifecycle = l }
func (a *Agent) Lifecycle() *Lifecycle             { return a.lifecycle }

func (a *Agent) checkPausePoint(ctx context.Context) error {
	if a.lifecycle == nil {
		return nil
	}
	return a.lifecycle.WaitIfPaused(ctx)
}

func (a *Agent) fireHook(ctx context.Context, payload HookPayload) error {
	if a.hooks == nil {
		return nil
	}
	return a.hooks.Fire(ctx, payload)
}

func (a *Agent) Run(ctx context.Context, userInput string) (string, error) {
	if a.lifecycle != nil {
		if a.lifecycle.State() == StateStopped {
			return "", fmt.Errorf("agent is stopped")
		}
		if err := a.lifecycle.Transition(StateRunning); err != nil {
			return "", fmt.Errorf("lifecycle transition failed: %w", err)
		}
		defer func() {
			if a.lifecycle.State() == StateRunning {
				a.lifecycle.Transition(StateIdle)
			}
			a.lifecycle.MarkDone()
		}()
	}

	if err := a.fireHook(ctx, HookPayload{Event: OnSessionStart, UserInput: userInput}); err != nil {
		return "", fmt.Errorf("session start hook aborted: %w", err)
	}

	userMsg := llm.Message{
		Role:    "user",
		Content: []llm.Block{llm.TextBlock{Text: userInput}},
	}
	a.memory.AppendMessage(userMsg)

	for {
		if err := a.checkPausePoint(ctx); err != nil {
			return "", fmt.Errorf("paused: %w", err)
		}

		req := a.buildRequest(ctx, userInput)

		if err := a.fireHook(ctx, HookPayload{Event: BeforeLLMCall, Request: &req}); err != nil {
			return "", fmt.Errorf("before LLM call hook aborted: %w", err)
		}

		start := time.Now()
		resp, err := a.llm.CreateMessage(ctx, req)
		duration := time.Since(start)

		if err != nil {
			a.fireHook(ctx, HookPayload{Event: OnError, Error: err})
			a.fireHook(ctx, HookPayload{Event: AfterLLMCall, Duration: duration, Error: err})
			return "", fmt.Errorf("LLM call failed: %w", err)
		}

		a.fireHook(ctx, HookPayload{Event: AfterLLMCall, Response: resp, Duration: duration})

		assistantMsg := llm.Message{Role: "assistant", Content: resp.Content}
		a.memory.AppendMessage(assistantMsg)

		toolCalls := extractToolUse(resp.Content)
		if len(toolCalls) == 0 {
			return extractText(resp.Content), nil
		}

		results := a.executeTools(ctx, toolCalls)
		toolResultMsg := llm.Message{Role: "user", Content: results}
		a.memory.AppendMessage(toolResultMsg)
	}
}

func (a *Agent) RunStream(ctx context.Context, userInput string, onEvent func(llm.StreamEvent)) (string, error) {
	sp, ok := a.llm.(llm.StreamProvider)
	if !ok {
		reply, err := a.Run(ctx, userInput)
		if err != nil {
			return "", err
		}
		onEvent(llm.StreamEvent{Type: "text_delta", Text: reply})
		return reply, nil
	}

	if a.lifecycle != nil {
		if a.lifecycle.State() == StateStopped {
			return "", fmt.Errorf("agent is stopped")
		}
		if err := a.lifecycle.Transition(StateRunning); err != nil {
			return "", fmt.Errorf("lifecycle transition failed: %w", err)
		}
		defer func() {
			if a.lifecycle.State() == StateRunning {
				a.lifecycle.Transition(StateIdle)
			}
			a.lifecycle.MarkDone()
		}()
	}

	if err := a.fireHook(ctx, HookPayload{Event: OnSessionStart, UserInput: userInput}); err != nil {
		return "", fmt.Errorf("session start hook aborted: %w", err)
	}

	userMsg := llm.Message{
		Role:    "user",
		Content: []llm.Block{llm.TextBlock{Text: userInput}},
	}
	a.memory.AppendMessage(userMsg)

	for {
		if err := a.checkPausePoint(ctx); err != nil {
			return "", fmt.Errorf("paused: %w", err)
		}

		req := a.buildRequest(ctx, userInput)

		if err := a.fireHook(ctx, HookPayload{Event: BeforeLLMCall, Request: &req}); err != nil {
			return "", fmt.Errorf("before LLM call hook aborted: %w", err)
		}

		start := time.Now()
		ch, err := sp.CreateMessageStream(ctx, req)
		if err != nil {
			a.fireHook(ctx, HookPayload{Event: OnError, Error: err})
			a.fireHook(ctx, HookPayload{Event: AfterLLMCall, Duration: time.Since(start), Error: err})
			return "", fmt.Errorf("LLM stream failed: %w", err)
		}

		var fullText strings.Builder
		var toolCalls []llm.ToolUseBlock
		var blocks []llm.Block

		for event := range ch {
			switch event.Type {
			case "text_delta":
				onEvent(event)
				fullText.WriteString(event.Text)
			case "tool_use":
				if event.ToolUse != nil {
					toolCalls = append(toolCalls, *event.ToolUse)
					blocks = append(blocks, *event.ToolUse)
				}
			case "error":
				a.fireHook(ctx, HookPayload{Event: OnError, Error: event.Error})
				return fullText.String(), event.Error
			}
		}

		duration := time.Since(start)

		if fullText.Len() > 0 {
			blocks = append([]llm.Block{llm.TextBlock{Text: fullText.String()}}, blocks...)
		}

		resp := &llm.Response{Content: blocks}
		a.fireHook(ctx, HookPayload{Event: AfterLLMCall, Response: resp, Duration: duration})

		assistantMsg := llm.Message{Role: "assistant", Content: blocks}
		a.memory.AppendMessage(assistantMsg)

		if len(toolCalls) == 0 {
			return fullText.String(), nil
		}

		results := a.executeTools(ctx, toolCalls)
		toolResultMsg := llm.Message{Role: "user", Content: results}
		a.memory.AppendMessage(toolResultMsg)
	}
}

func (a *Agent) buildRequest(ctx context.Context, userInput string) llm.Request {
	system := a.buildSystemPrompt(ctx, userInput)
	messages := a.memory.GetContextMessages()
	toolDefs := a.getToolDefs()

	return llm.Request{
		System:    system,
		Messages:  messages,
		Tools:     toolDefs,
		MaxTokens: 4096,
	}
}

func (a *Agent) buildSystemPrompt(ctx context.Context, userInput string) string {
	system := "You are a helpful assistant."

	if a.skillRegistry != nil {
		system += a.skillRegistry.BuildIndexPrompt()

		for _, s := range a.skillRegistry.ActiveSkills() {
			system += "\n\n" + s.Prompt
		}
	}

	memoryPrompt := a.memory.BuildMemoryPrompt(ctx, userInput)
	system += memoryPrompt

	return system
}

func (a *Agent) getToolDefs() []llm.ToolDef {
	defs := make([]llm.ToolDef, 0, len(a.tools))
	for _, t := range a.tools {
		defs = append(defs, t.Definition)
	}
	return defs
}

func (a *Agent) executeTools(ctx context.Context, calls []llm.ToolUseBlock) []llm.Block {
	results := make([]llm.Block, len(calls))
	var wg sync.WaitGroup
	wg.Add(len(calls))

	for i, call := range calls {
		go func(idx int, c llm.ToolUseBlock) {
			defer wg.Done()
			results[idx] = a.executeSingleTool(ctx, c)
		}(i, call)
	}

	wg.Wait()
	return results
}

func (a *Agent) executeSingleTool(ctx context.Context, call llm.ToolUseBlock) llm.ToolResultBlock {
	if a.permissionHandler != nil {
		if !a.permissionHandler.CheckPermission(call.Name, call.Input) {
			a.fireHook(ctx, HookPayload{Event: OnPermissionDenied, ToolName: call.Name, Input: call.Input})
			return llm.ToolResultBlock{
				ToolUseID: call.ID,
				Content:   fmt.Sprintf("Permission denied: tool '%s' requires user approval", call.Name),
				IsError:   true,
			}
		}
	}

	if err := a.fireHook(ctx, HookPayload{Event: BeforeToolCall, ToolName: call.Name, Input: call.Input}); err != nil {
		return llm.ToolResultBlock{
			ToolUseID: call.ID,
			Content:   fmt.Sprintf("Hook aborted tool '%s': %v", call.Name, err),
			IsError:   true,
		}
	}

	start := time.Now()
	for _, tool := range a.tools {
		if tool.Definition.Name == call.Name {
			if a.executor != nil {
				result := a.executor.Execute(ctx, tool, call)
				a.fireHook(ctx, HookPayload{Event: AfterToolCall, ToolName: call.Name, Output: result.Content, Duration: time.Since(start)})
				if result.IsError {
					a.fireHook(ctx, HookPayload{Event: OnError, Error: fmt.Errorf("%s", result.Content), ToolName: call.Name})
				}
				return result
			}
			output, err := tool.Execute(ctx, call.Input)
			duration := time.Since(start)
			if err != nil {
				slog.Error("tool execution failed", "tool", call.Name, "error", err)
				a.fireHook(ctx, HookPayload{Event: AfterToolCall, ToolName: call.Name, Error: err, Duration: duration})
				a.fireHook(ctx, HookPayload{Event: OnError, Error: err, ToolName: call.Name})
				return llm.ToolResultBlock{
					ToolUseID: call.ID,
					Content:   fmt.Sprintf("Error: %v", err),
					IsError:   true,
				}
			}
			a.fireHook(ctx, HookPayload{Event: AfterToolCall, ToolName: call.Name, Output: output, Duration: duration})
			return llm.ToolResultBlock{
				ToolUseID: call.ID,
				Content:   output,
				IsError:   false,
			}
		}
	}
	return llm.ToolResultBlock{
		ToolUseID: call.ID,
		Content:   fmt.Sprintf("Error: unknown tool '%s'", call.Name),
		IsError:   true,
	}
}

func (a *Agent) SetPermissionHandler(h *PermissionHandler) { a.permissionHandler = h }
func (a *Agent) SetExecutor(e *ToolExecutor)               { a.executor = e }

func (a *Agent) ExecuteTool(name string, input json.RawMessage) (string, error) {
	for _, tool := range a.tools {
		if tool.Definition.Name == name {
			return tool.Execute(context.Background(), input)
		}
	}
	return "", fmt.Errorf("unknown tool: %s", name)
}

func (a *Agent) AddTools(tools ...Tool) {
	a.tools = append(a.tools, tools...)
}

func (a *Agent) SkillRegistry() *skill.SkillRegistry { return a.skillRegistry }

func extractToolUse(blocks []llm.Block) []llm.ToolUseBlock {
	var calls []llm.ToolUseBlock
	for _, block := range blocks {
		if tu, ok := block.(llm.ToolUseBlock); ok {
			calls = append(calls, tu)
		}
	}
	return calls
}

func extractText(blocks []llm.Block) string {
	var text string
	for _, block := range blocks {
		if tb, ok := block.(llm.TextBlock); ok {
			if text != "" {
				text += "\n"
			}
			text += tb.Text
		}
	}
	return text
}
