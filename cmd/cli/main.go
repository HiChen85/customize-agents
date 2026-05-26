package main

import (
	"context"
	"encoding/json"
	"flag"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/HiChen85/customize-agents/config"
	"github.com/HiChen85/customize-agents/core"
	"github.com/HiChen85/customize-agents/llm"
	"github.com/HiChen85/customize-agents/mcp"
	"github.com/HiChen85/customize-agents/memory"
	"github.com/HiChen85/customize-agents/skill"
	"github.com/HiChen85/customize-agents/tui"
)

func main() {
	cfgPath := flag.String("config", "agent.yaml", "Path to config file")
	provider := flag.String("provider", "", "Override active provider")
	model := flag.String("model", "", "Override model")
	skillsFlag := flag.String("skills", "", "Comma-separated skills to activate")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	if *provider != "" {
		cfg.ActiveProvider = *provider
	}
	if *model != "" {
		cfg.Model = *model
	}

	providerCfg, ok := cfg.Providers[cfg.ActiveProvider]
	if !ok {
		slog.Error("provider not found in config", "provider", cfg.ActiveProvider)
		os.Exit(1)
	}

	baseProvider := llm.NewAnthropicProvider(providerCfg.APIKey, providerCfg.BaseURL, cfg.Model)
	llmProvider := llm.NewRetryProvider(baseProvider, llm.RetryConfig{
		MaxRetries: 3, BaseDelay: 1 * time.Second, MaxDelay: 30 * time.Second, RetryableFunc: llm.DefaultRetryable,
	})

	store, err := memory.NewFileStore(cfg.Memory.Dir)
	if err != nil {
		slog.Error("failed to init memory store", "error", err)
		os.Exit(1)
	}
	mm := memory.NewMemoryManager(store, cfg.MaxTokens)

	compactor := memory.NewCompactor(memory.CompactionConfig{
		Threshold: cfg.Memory.Compaction.Threshold,
		Provider:  llmProvider,
		Model:     cfg.Memory.Compaction.Model,
	})
	mm.Working.SetCompactor(compactor)

	registry := skill.NewSkillRegistry(cfg.Skills.ProjectDir, cfg.Skills.UserDir)
	if err := registry.BuildIndex(); err != nil {
		slog.Warn("failed to build skill index", "error", err)
	}

	activeNames := cfg.ActiveSkills
	if *skillsFlag != "" {
		activeNames = strings.Split(*skillsFlag, ",")
	}
	for _, name := range activeNames {
		if _, err := registry.Activate(strings.TrimSpace(name)); err != nil {
			slog.Warn("failed to pre-activate skill", "name", name, "error", err)
		}
	}

	searchAPIKey := os.Getenv("TAVILY_API_KEY")
	tools := []core.Tool{
		core.NewExecTool(),
		core.NewReadFileTool(),
		core.NewWriteFileTool(),
		core.NewListDirTool(),
		core.NewGrepTool(),
		core.NewWebSearchTool(searchAPIKey, ""),
		core.NewWebFetchTool(),
		core.NewMemorySaveTool(store),
		core.NewMemorySearchTool(store),
		core.NewMemoryContextTool(mm),
		core.NewActivateSkillTool(registry),
	}

	var sandbox *core.Sandbox
	if len(cfg.Sandbox.BlockedCommands) > 0 || len(cfg.Sandbox.AllowedCommands) > 0 {
		sandbox = core.NewSandbox(core.SandboxConfig{
			AllowedCommands: cfg.Sandbox.AllowedCommands,
			BlockedCommands: cfg.Sandbox.BlockedCommands,
			AllowedPaths:    cfg.Sandbox.AllowedPaths,
			BlockedPaths:    cfg.Sandbox.BlockedPaths,
			MaxOutputSize:   cfg.Sandbox.MaxOutputSize,
		})
		for i, tool := range tools {
			if tool.Definition.Name == "exec" {
				tools[i] = sandbox.WrapExecTool(tool)
				break
			}
		}
	}

	agent := core.NewAgent(llmProvider, mm, tools, registry)

	agent.SetExecutor(core.NewToolExecutor(core.ExecutorConfig{
		Timeout: 30 * time.Second, MaxRetries: 2, RetryDelay: 1 * time.Second,
		RetryableFunc: func(err error) bool {
			return strings.Contains(err.Error(), "timeout") || strings.Contains(err.Error(), "temporary")
		},
	}))
	agent.SetPermissionHandler(core.NewPermissionHandler(core.PermissionConfig{
		AutoApprove: []string{"read_file", "list_dir", "grep", "web_search", "web_fetch", "memory_save", "memory_search", "memory_context"},
		PromptFunc: func(toolName string, input json.RawMessage) bool {
			return true
		},
	}))

	lc := core.NewLifecycle()
	agent.SetLifecycle(lc)

	if len(cfg.MCP.Servers) > 0 {
		mcpMgr := mcp.NewMCPManager()
		if err := mcpMgr.Initialize(context.Background(), cfg.MCP.Servers); err != nil {
			slog.Warn("MCP initialization had errors", "error", err)
		} else {
			defer mcpMgr.Close()
		}

		existingTools := make(map[string]bool, len(tools))
		for _, t := range tools {
			existingTools[t.Definition.Name] = true
		}
		mcpTools := mcpMgr.GetTools(existingTools)
		agent.AddTools(mcpTools...)
	}

	if err := tui.Run(agent, mm, registry, cfg.Model, cfg.MaxTokens); err != nil {
		slog.Error("TUI error", "error", err)
		os.Exit(1)
	}
}
