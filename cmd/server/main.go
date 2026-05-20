package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/haichen-zhang/customize-agents/config"
	"github.com/haichen-zhang/customize-agents/core"
	"github.com/haichen-zhang/customize-agents/llm"
	"github.com/haichen-zhang/customize-agents/memory"
	"github.com/haichen-zhang/customize-agents/skill"
)

func main() {
	cfgPath := flag.String("config", "agent.yaml", "Path to config file")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	providerCfg := cfg.Providers[cfg.ActiveProvider]
	llmProvider := llm.NewAnthropicProvider(providerCfg.APIKey, providerCfg.BaseURL, cfg.Model)

	store, err := memory.NewFileStore(cfg.Memory.Dir)
	if err != nil {
		slog.Error("failed to init memory store", "error", err)
		os.Exit(1)
	}
	mm := memory.NewMemoryManager(store, cfg.MaxTokens)

	allSkills, _ := skill.LoadAllSkills(cfg.SkillsDir)
	var activeSkills []*skill.Skill
	for _, name := range cfg.ActiveSkills {
		if s := skill.FindSkillByName(allSkills, name); s != nil {
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

	r := gin.Default()

	v1 := r.Group("/v1")
	{
		v1.POST("/chat", chatHandler(agent))
		v1.GET("/skills", listSkillsHandler(allSkills, agent))
		v1.POST("/skills/activate", activateSkillHandler(allSkills, agent))
		v1.GET("/memory/search", memorySearchHandler(mm))
		v1.GET("/status", statusHandler(mm))
	}

	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	slog.Info("starting server", "addr", addr)
	if err := r.Run(addr); err != nil {
		slog.Error("server failed", "error", err)
		os.Exit(1)
	}
}

type ChatRequest struct {
	Message string   `json:"message" binding:"required"`
	Skills  []string `json:"skills,omitempty"`
}

type ChatResponse struct {
	Reply     string   `json:"reply"`
	ToolsUsed []string `json:"tools_used,omitempty"`
}

func chatHandler(agent *core.Agent) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req ChatRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		reply, err := agent.Run(context.Background(), req.Message)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, ChatResponse{Reply: reply})
	}
}

func listSkillsHandler(allSkills []*skill.Skill, agent *core.Agent) gin.HandlerFunc {
	return func(c *gin.Context) {
		type skillInfo struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Active      bool   `json:"active"`
		}

		var result []skillInfo
		for _, s := range allSkills {
			active := false
			for _, as := range agent.ActiveSkills() {
				if as.Name == s.Name {
					active = true
					break
				}
			}
			result = append(result, skillInfo{Name: s.Name, Description: s.Description, Active: active})
		}
		c.JSON(http.StatusOK, gin.H{"skills": result})
	}
}

func activateSkillHandler(allSkills []*skill.Skill, agent *core.Agent) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Name string `json:"name" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		s := skill.FindSkillByName(allSkills, strings.TrimSpace(req.Name))
		if s == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "skill not found"})
			return
		}

		agent.ActivateSkill(s)
		c.JSON(http.StatusOK, gin.H{"message": "activated", "skill": s.Name})
	}
}

func memorySearchHandler(mm *memory.MemoryManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		query := c.Query("q")
		if query == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "query parameter 'q' required"})
			return
		}

		entries, err := mm.RetrieveRelevant(context.Background(), query, 5)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{"results": entries})
	}
}

func statusHandler(mm *memory.MemoryManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		used, max := mm.TokenUsage()
		c.JSON(http.StatusOK, gin.H{
			"token_used":    used,
			"token_max":     max,
			"usage_percent": float64(used) / float64(max) * 100,
		})
	}
}
