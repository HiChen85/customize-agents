package core

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/haichen-zhang/customize-agents/llm"
	"github.com/haichen-zhang/customize-agents/memory"
	"github.com/haichen-zhang/customize-agents/skill"
)

type Agent struct {
	llm               llm.Provider
	memory            *memory.MemoryManager
	tools             []Tool
	skills            []*skill.Skill
	permissionHandler *PermissionHandler
	executor          *ToolExecutor
}

func NewAgent(provider llm.Provider, mm *memory.MemoryManager, tools []Tool, skills []*skill.Skill) *Agent {
	return &Agent{
		llm:    provider,
		memory: mm,
		tools:  tools,
		skills: skills,
	}
}

func (a *Agent) Run(ctx context.Context, userInput string) (string, error) {
	userMsg := llm.Message{
		Role:    "user",
		Content: []llm.Block{llm.TextBlock{Text: userInput}},
	}
	a.memory.AppendMessage(userMsg)

	for {
		req := a.buildRequest(ctx, userInput)
		resp, err := a.llm.CreateMessage(ctx, req)
		if err != nil {
			return "", fmt.Errorf("LLM call failed: %w", err)
		}

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

	for _, s := range a.skills {
		system += "\n\n" + s.Prompt
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
	results := make([]llm.Block, 0, len(calls))
	for _, call := range calls {
		result := a.executeSingleTool(ctx, call)
		results = append(results, result)
	}
	return results
}

func (a *Agent) executeSingleTool(ctx context.Context, call llm.ToolUseBlock) llm.ToolResultBlock {
	if a.permissionHandler != nil {
		if !a.permissionHandler.CheckPermission(call.Name, call.Input) {
			return llm.ToolResultBlock{
				ToolUseID: call.ID,
				Content:   fmt.Sprintf("Permission denied: tool '%s' requires user approval", call.Name),
				IsError:   true,
			}
		}
	}
	for _, tool := range a.tools {
		if tool.Definition.Name == call.Name {
			if a.executor != nil {
				return a.executor.Execute(ctx, tool, call)
			}
			output, err := tool.Execute(ctx, call.Input)
			if err != nil {
				slog.Error("tool execution failed", "tool", call.Name, "error", err)
				return llm.ToolResultBlock{
					ToolUseID: call.ID,
					Content:   fmt.Sprintf("Error: %v", err),
					IsError:   true,
				}
			}
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

func (a *Agent) ActiveSkills() []*skill.Skill {
	return a.skills
}

func (a *Agent) ActivateSkill(s *skill.Skill) {
	a.skills = append(a.skills, s)
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
