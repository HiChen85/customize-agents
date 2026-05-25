package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/haichen-zhang/customize-agents/config"
	"github.com/haichen-zhang/customize-agents/core"
	"github.com/haichen-zhang/customize-agents/llm"
	"github.com/haichen-zhang/customize-agents/mcp"
	"github.com/haichen-zhang/customize-agents/memory"
	"github.com/haichen-zhang/customize-agents/skill"
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

	allSkills, err := skill.LoadAllSkills(cfg.SkillsDir)
	if err != nil {
		slog.Warn("failed to load skills", "error", err)
	}

	var activeSkills []*skill.Skill
	activeNames := cfg.ActiveSkills
	if *skillsFlag != "" {
		activeNames = strings.Split(*skillsFlag, ",")
	}
	for _, name := range activeNames {
		if s := skill.FindSkillByName(allSkills, strings.TrimSpace(name)); s != nil {
			activeSkills = append(activeSkills, s)
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
	}

	agent := core.NewAgent(llmProvider, mm, tools, activeSkills)

	agent.SetExecutor(core.NewToolExecutor(core.ExecutorConfig{
		Timeout: 30 * time.Second, MaxRetries: 2, RetryDelay: 1 * time.Second,
		RetryableFunc: func(err error) bool {
			return strings.Contains(err.Error(), "timeout") || strings.Contains(err.Error(), "temporary")
		},
	}))
	agent.SetPermissionHandler(core.NewPermissionHandler(core.PermissionConfig{
		AutoApprove: []string{"read_file", "list_dir", "grep", "web_search", "web_fetch", "memory_save", "memory_search", "memory_context"},
		PromptFunc: func(toolName string, input json.RawMessage) bool {
			fmt.Printf("[Permission] Tool '%s' wants to execute. Allow? (y/n): ", toolName)
			var answer string
			fmt.Scanln(&answer)
			return answer == "y" || answer == "Y"
		},
	}))

	if cfg.Hooks != nil {
		hookRegistry := core.NewHookRegistry()
		if err := hookRegistry.LoadFromConfig(cfg.Hooks); err != nil {
			slog.Error("failed to load hooks config", "error", err)
			os.Exit(1)
		}
		agent.SetHookRegistry(hookRegistry)
	}

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

		if len(mcpTools) > 0 {
			names := make([]string, 0, len(mcpTools))
			for _, t := range mcpTools {
				names = append(names, t.Definition.Name)
			}
			fmt.Printf("MCP tools: %s\n", strings.Join(names, ", "))
		}
	}

	fmt.Println("Agent ready. Type /help for commands, or start chatting.")
	fmt.Printf("Provider: %s | Model: %s\n", cfg.ActiveProvider, cfg.Model)
	if len(activeSkills) > 0 {
		names := make([]string, 0, len(activeSkills))
		for _, s := range activeSkills {
			names = append(names, s.Name)
		}
		fmt.Printf("Active skills: %s\n", strings.Join(names, ", "))
	}
	fmt.Println()

	ctx := context.Background()
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Print("> ")
	for scanner.Scan() {
		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			fmt.Print("> ")
			continue
		}

		if strings.HasPrefix(input, "/") {
			handleCommand(agent, allSkills, mm, input)
			fmt.Print("> ")
			continue
		}

		onEvent := func(event llm.StreamEvent) {
			if event.Type == "text_delta" {
				fmt.Print(event.Text)
			}
		}
		_, err := agent.RunStream(ctx, input, onEvent)
		if err != nil {
			fmt.Printf("\nError: %v\n", err)
		} else {
			fmt.Println()
		}
		fmt.Print("> ")
	}
}

func handleCommand(agent *core.Agent, allSkills []*skill.Skill, mm *memory.MemoryManager, input string) {
	parts := strings.Fields(input)
	cmd := parts[0]

	switch cmd {
	case "/help":
		fmt.Println("Commands:")
		fmt.Println("  /skill list             - List available skills")
		fmt.Println("  /skill activate <name>  - Activate a skill")
		fmt.Println("  /memory search <query>  - Search long-term memory")
		fmt.Println("  /status                 - Show context window usage")
		fmt.Println("  /pause                  - Pause the agent")
		fmt.Println("  /resume                 - Resume the agent")
		fmt.Println("  /quit                   - Exit")

	case "/skill":
		if len(parts) < 2 {
			fmt.Println("Usage: /skill list | /skill activate <name>")
			return
		}
		switch parts[1] {
		case "list":
			fmt.Println("Available skills:")
			for _, s := range allSkills {
				active := ""
				for _, as := range agent.ActiveSkills() {
					if as.Name == s.Name {
						active = " [active]"
						break
					}
				}
				fmt.Printf("  - %s: %s%s\n", s.Name, s.Description, active)
			}
		case "activate":
			if len(parts) < 3 {
				fmt.Println("Usage: /skill activate <name>")
				return
			}
			name := parts[2]
			s := skill.FindSkillByName(allSkills, name)
			if s == nil {
				fmt.Printf("Skill '%s' not found\n", name)
				return
			}
			agent.ActivateSkill(s)
			fmt.Printf("Activated skill: %s\n", s.Name)
		}

	case "/memory":
		if len(parts) < 3 {
			fmt.Println("Usage: /memory search <query>")
			return
		}
		if parts[1] == "search" {
			query := strings.Join(parts[2:], " ")
			entries, err := mm.RetrieveRelevant(context.Background(), query, 5)
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				return
			}
			if len(entries) == 0 {
				fmt.Println("No memories found.")
				return
			}
			for _, e := range entries {
				fmt.Printf("  [%s] %s (tags: %s)\n", e.ID, e.Content, strings.Join(e.Tags, ", "))
			}
		}

	case "/status":
		used, max := mm.TokenUsage()
		fmt.Printf("Context: %d / %d tokens (%.1f%%)\n", used, max, float64(used)/float64(max)*100)

	case "/pause":
		if err := agent.Lifecycle().Pause(); err != nil {
			fmt.Printf("Error: %v\n", err)
		} else {
			fmt.Println("Agent paused. Type /resume to continue.")
		}

	case "/resume":
		if err := agent.Lifecycle().Resume(); err != nil {
			fmt.Printf("Error: %v\n", err)
		} else {
			fmt.Println("Agent resumed.")
		}

	case "/quit":
		fmt.Println("Goodbye!")
		os.Exit(0)

	default:
		fmt.Printf("Unknown command: %s (type /help for commands)\n", cmd)
	}
}
