package core

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
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
	maxOutputTokens   int
}

func NewAgent(provider llm.Provider, mm *memory.MemoryManager, tools []Tool, registry *skill.SkillRegistry) *Agent {
	return &Agent{
		llm:           provider,
		memory:        mm,
		tools:         tools,
		skillRegistry: registry,
	}
}

func (a *Agent) SetHookRegistry(r *HookRegistry)    { a.hooks = r }
func (a *Agent) SetLifecycle(l *Lifecycle)           { a.lifecycle = l }
func (a *Agent) SetMaxOutputTokens(n int)            { a.maxOutputTokens = n }
func (a *Agent) Lifecycle() *Lifecycle               { return a.lifecycle }

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

	const maxIterations = 20
	for iteration := 0; ; iteration++ {
		if iteration >= maxIterations {
			return "", fmt.Errorf("agent exceeded maximum of %d tool-use iterations", maxIterations)
		}

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

		// If output was truncated, don't execute incomplete tool calls
		if resp.StopReason == "max_tokens" || resp.StopReason == "length" {
			slog.Warn("output truncated in non-stream mode", "stop_reason", resp.StopReason)
			truncMsg := llm.Message{Role: "user", Content: []llm.Block{llm.ToolResultBlock{
				ToolUseID: toolCalls[0].ID,
				Content:   "Error: your output was truncated due to token limit. The tool input was incomplete and could not be executed. Please break the task into smaller steps.",
				IsError:   true,
			}}}
			for _, tc := range toolCalls[1:] {
				truncMsg.Content = append(truncMsg.Content, llm.ToolResultBlock{
					ToolUseID: tc.ID,
					Content:   "Error: output truncated, tool call skipped.",
					IsError:   true,
				})
			}
			a.memory.AppendMessage(truncMsg)
			continue
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

	const maxAgentRetries = 2
	const maxLoopIterations = 20

	var lastText string
	iteration := 0
	for {
		iteration++
		if iteration > maxLoopIterations {
			slog.Warn("agent loop hit max iterations, stopping", "max", maxLoopIterations)
			return lastText, fmt.Errorf("agent exceeded maximum of %d tool-use iterations", maxLoopIterations)
		}

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
			if a.isRecoverableError(err) {
				recovered := false
				for retry := 0; retry < maxAgentRetries; retry++ {
					slog.Warn("recoverable error, sanitizing memory and retrying", "error", err, "attempt", retry+1)
					a.sanitizeMemory()
					req = a.buildRequest(ctx, userInput)
					ch, err = sp.CreateMessageStream(ctx, req)
					if err == nil {
						recovered = true
						break
					}
				}
				if !recovered {
					a.fireHook(ctx, HookPayload{Event: OnError, Error: err})
					a.fireHook(ctx, HookPayload{Event: AfterLLMCall, Duration: time.Since(start), Error: err})
					return "", fmt.Errorf("LLM stream failed after %d retries: %w", maxAgentRetries, err)
				}
			} else {
				a.fireHook(ctx, HookPayload{Event: OnError, Error: err})
				a.fireHook(ctx, HookPayload{Event: AfterLLMCall, Duration: time.Since(start), Error: err})
				return "", fmt.Errorf("LLM stream failed: %w", err)
			}
		}

		var fullText strings.Builder
		var toolCalls []llm.ToolUseBlock
		var blocks []llm.Block
		var streamStopReason string

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
			case "thinking":
				if event.Thinking != nil {
					blocks = append(blocks, *event.Thinking)
				}
			case "done":
				streamStopReason = event.StopReason
			case "error":
				a.fireHook(ctx, HookPayload{Event: OnError, Error: event.Error})
				return fullText.String(), event.Error
			}
		}

		duration := time.Since(start)

		if fullText.Len() > 0 {
			// Insert text block after any thinking blocks but before tool_use blocks
			textBlock := llm.TextBlock{Text: fullText.String()}
			insertIdx := 0
			for i, b := range blocks {
				if _, ok := b.(llm.ThinkingBlock); ok {
					insertIdx = i + 1
				} else {
					break
				}
			}
			blocks = append(blocks[:insertIdx], append([]llm.Block{textBlock}, blocks[insertIdx:]...)...)
		}

		resp := &llm.Response{Content: blocks}
		a.fireHook(ctx, HookPayload{Event: AfterLLMCall, Response: resp, Duration: duration})

		assistantMsg := llm.Message{Role: "assistant", Content: blocks}
		a.memory.AppendMessage(assistantMsg)

		if len(toolCalls) == 0 {
			return fullText.String(), nil
		}

		// If output was truncated (max_tokens/length), don't execute the
		// incomplete tool calls — ask the model to retry with smaller output.
		truncated := streamStopReason == "max_tokens" || streamStopReason == "length"
		if truncated {
			slog.Warn("output truncated, skipping tool execution", "stop_reason", streamStopReason, "tools", len(toolCalls))
			truncMsg := llm.Message{
				Role: "user",
				Content: []llm.Block{llm.ToolResultBlock{
					ToolUseID: toolCalls[0].ID,
					Content:   "Error: your output was truncated due to token limit. The tool input was incomplete and could not be executed. Please break the task into smaller steps — for example, write files in shorter chunks, or split the work across multiple tool calls.",
					IsError:   true,
				}},
			}
			// Add result blocks for remaining tool calls if any
			for _, tc := range toolCalls[1:] {
				truncMsg.Content = append(truncMsg.Content, llm.ToolResultBlock{
					ToolUseID: tc.ID,
					Content:   "Error: output truncated, tool call skipped.",
					IsError:   true,
				})
			}
			a.memory.AppendMessage(truncMsg)
			lastText = fullText.String()
			continue
		}

		lastText = fullText.String()
		results := a.executeTools(ctx, toolCalls)
		toolResultMsg := llm.Message{Role: "user", Content: results}
		a.memory.AppendMessage(toolResultMsg)
	}
}

func (a *Agent) buildRequest(ctx context.Context, userInput string) llm.Request {
	system := a.buildSystemPrompt(ctx, userInput)
	messages := a.memory.GetContextMessages()
	toolDefs := a.getToolDefs()

	maxTokens := a.maxOutputTokens
	if maxTokens <= 0 {
		maxTokens = 8192
	}

	return llm.Request{
		System:    system,
		Messages:  messages,
		Tools:     toolDefs,
		MaxTokens: maxTokens,
	}
}

func (a *Agent) buildSystemPrompt(ctx context.Context, userInput string) string {
	today := time.Now().Format("2006-01-02")
	cwd, _ := os.Getwd()
	system := fmt.Sprintf("You are Harness Agent, a demo AI agent tool built to showcase agentic capabilities including tool use, skill activation, and memory management. When asked who you are, always identify yourself as Harness Agent — never claim to be Claude, GPT, or any other AI model directly. You are powered by an LLM provider but your identity is Harness Agent.\n\nToday's date is %s. Always use this date when searching for current information.\n\nWorking directory: %s\nAll file operations (read, write, list, grep) should default to paths within or relative to this working directory unless the user explicitly specifies an absolute path elsewhere. Never write to system directories like /root, /etc, or /usr.", today, cwd)

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
				hookPayload := HookPayload{Event: AfterToolCall, ToolName: call.Name, Output: result.Content, Duration: time.Since(start)}
				if result.IsError {
					hookPayload.Error = fmt.Errorf("%s", result.Content)
					a.fireHook(ctx, hookPayload)
					a.fireHook(ctx, HookPayload{Event: OnError, Error: hookPayload.Error, ToolName: call.Name})
				} else {
					a.fireHook(ctx, hookPayload)
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

func (a *Agent) Tools() []Tool                       { return a.tools }
func (a *Agent) SkillRegistry() *skill.SkillRegistry { return a.skillRegistry }

func (a *Agent) isRecoverableError(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "marshal") ||
		strings.Contains(msg, "MarshalJSON") ||
		strings.Contains(msg, "unexpected end of")
}

func (a *Agent) sanitizeMemory() {
	fixed := a.memory.SanitizeToolInputs()
	if fixed > 0 {
		slog.Info("sanitized malformed tool inputs in memory", "count", fixed)
	}
}

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
