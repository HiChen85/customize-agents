package core

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/google/uuid"
	"github.com/HiChen85/customize-agents/llm"
	"github.com/HiChen85/customize-agents/memory"
	"github.com/HiChen85/customize-agents/skill"
)

func NewExecTool() Tool {
	return Tool{
		Definition: llm.ToolDef{
			Name:        "exec",
			Description: "Execute a shell command and return its output",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"command":{"type":"string","description":"The shell command to execute"},"work_dir":{"type":"string","description":"Working directory (optional)"}},"required":["command"]}`),
		},
		Execute: func(ctx context.Context, input json.RawMessage) (string, error) {
			var params struct {
				Command string `json:"command"`
				WorkDir string `json:"work_dir"`
			}
			if err := json.Unmarshal(input, &params); err != nil {
				return "", fmt.Errorf("parse input: %w", err)
			}
			if params.Command == "" {
				return "", fmt.Errorf("'command' parameter is required but was empty")
			}

			cmd := exec.CommandContext(ctx, "bash", "-c", params.Command)
			if params.WorkDir != "" {
				cmd.Dir = params.WorkDir
			}
			output, err := cmd.CombinedOutput()
			if err != nil {
				return fmt.Sprintf("error: %v\noutput: %s", err, string(output)), nil
			}
			return string(output), nil
		},
	}
}

func NewReadFileTool() Tool {
	return Tool{
		Definition: llm.ToolDef{
			Name:        "read_file",
			Description: "Read the contents of a file",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Absolute path to the file"}},"required":["path"]}`),
		},
		Execute: func(ctx context.Context, input json.RawMessage) (string, error) {
			var params struct {
				Path string `json:"path"`
			}
			if err := json.Unmarshal(input, &params); err != nil {
				return "", fmt.Errorf("parse input: %w", err)
			}
			if params.Path == "" {
				return "", fmt.Errorf("path is required and cannot be empty")
			}

			data, err := os.ReadFile(params.Path)
			if err != nil {
				return "", fmt.Errorf("read file %s: %w", params.Path, err)
			}
			return string(data), nil
		},
	}
}

func NewMemorySaveTool(store memory.LongTermStore) Tool {
	return Tool{
		Definition: llm.ToolDef{
			Name:        "memory_save",
			Description: "Save important information to long-term memory for future reference",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"content":{"type":"string","description":"The information to remember"},"tags":{"type":"array","items":{"type":"string"},"description":"Tags for categorization"}},"required":["content"]}`),
		},
		Execute: func(ctx context.Context, input json.RawMessage) (string, error) {
			var params struct {
				Content string   `json:"content"`
				Tags    []string `json:"tags"`
			}
			if err := json.Unmarshal(input, &params); err != nil {
				return "", fmt.Errorf("parse input: %w", err)
			}

			entry := memory.Entry{
				ID:      uuid.New().String()[:8],
				Content: params.Content,
				Tags:    params.Tags,
			}

			if err := store.Save(ctx, entry); err != nil {
				return "", fmt.Errorf("save memory: %w", err)
			}

			return fmt.Sprintf("Saved to memory (id: %s)", entry.ID), nil
		},
	}
}

func NewMemorySearchTool(store memory.LongTermStore) Tool {
	return Tool{
		Definition: llm.ToolDef{
			Name:        "memory_search",
			Description: "Search long-term memory for relevant information",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","description":"Search query"},"limit":{"type":"integer","description":"Max results (default 5)"}},"required":["query"]}`),
		},
		Execute: func(ctx context.Context, input json.RawMessage) (string, error) {
			var params struct {
				Query string `json:"query"`
				Limit int    `json:"limit"`
			}
			if err := json.Unmarshal(input, &params); err != nil {
				return "", fmt.Errorf("parse input: %w", err)
			}
			if params.Limit <= 0 {
				params.Limit = 5
			}

			entries, err := store.Search(ctx, params.Query, params.Limit)
			if err != nil {
				return "", fmt.Errorf("search memory: %w", err)
			}

			if len(entries) == 0 {
				return "No memories found.", nil
			}

			var sb strings.Builder
			sb.WriteString(fmt.Sprintf("Found %d memories:\n", len(entries)))
			for _, e := range entries {
				sb.WriteString(fmt.Sprintf("- [%s] %s (tags: %s)\n", e.ID, e.Content, strings.Join(e.Tags, ", ")))
			}
			return sb.String(), nil
		},
	}
}

func NewMemoryContextTool(mm *memory.MemoryManager) Tool {
	return Tool{
		Definition: llm.ToolDef{
			Name:        "memory_context",
			Description: "Check current context window usage (used/remaining tokens)",
			InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
		},
		Execute: func(ctx context.Context, input json.RawMessage) (string, error) {
			used, max := mm.TokenUsage()
			return fmt.Sprintf("Context window: %d / %d tokens used (%.1f%% full)", used, max, float64(used)/float64(max)*100), nil
		},
	}
}

func NewActivateSkillTool(registry *skill.SkillRegistry) Tool {
	return Tool{
		Definition: llm.ToolDef{
			Name:        "activate_skill",
			Description: "Load and activate a skill to get specialized guidance. Use when the task matches a skill's trigger condition from the Available Skills table.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","description":"Name of the skill to activate"}},"required":["name"]}`),
		},
		Execute: func(ctx context.Context, input json.RawMessage) (string, error) {
			var params struct {
				Name string `json:"name"`
			}
			if err := json.Unmarshal(input, &params); err != nil {
				return "", fmt.Errorf("parse input: %w", err)
			}
			s, err := registry.Activate(params.Name)
			if err != nil {
				return "", err
			}
			return s.Prompt, nil
		},
	}
}
