package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/haichen-zhang/customize-agents/config"
	"github.com/haichen-zhang/customize-agents/core"
	"github.com/haichen-zhang/customize-agents/llm"
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

	llmProvider := llm.NewAnthropicProvider(providerCfg.APIKey, providerCfg.BaseURL, cfg.Model)

	store, err := memory.NewFileStore(cfg.Memory.Dir)
	if err != nil {
		slog.Error("failed to init memory store", "error", err)
		os.Exit(1)
	}
	mm := memory.NewMemoryManager(store, cfg.MaxTokens)

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

	tools := []core.Tool{
		core.NewExecTool(),
		core.NewReadFileTool(),
		core.NewMemorySaveTool(store),
		core.NewMemorySearchTool(store),
		core.NewMemoryContextTool(mm),
	}

	agent := core.NewAgent(llmProvider, mm, tools, activeSkills)

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

		reply, err := agent.Run(ctx, input)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
		} else {
			fmt.Println(reply)
		}
		fmt.Println()
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

	case "/quit":
		fmt.Println("Goodbye!")
		os.Exit(0)

	default:
		fmt.Printf("Unknown command: %s (type /help for commands)\n", cmd)
	}
}
