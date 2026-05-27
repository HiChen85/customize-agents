package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/HiChen85/customize-agents/config"
	"github.com/HiChen85/customize-agents/core"
	"github.com/HiChen85/customize-agents/llm"
	"github.com/HiChen85/customize-agents/mcp"
	"github.com/HiChen85/customize-agents/memory"
	"github.com/HiChen85/customize-agents/skill"
	"github.com/gin-gonic/gin"
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
	for _, name := range cfg.ActiveSkills {
		registry.Activate(name)
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

	// MCP tools
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
		tools = append(tools, mcpTools...)
	}

	// Sandbox
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

	// Hook registry
	var hookRegistry *core.HookRegistry
	if cfg.Hooks != nil {
		hookRegistry = core.NewHookRegistry()
		if err := hookRegistry.LoadFromConfig(cfg.Hooks); err != nil {
			slog.Error("failed to load hooks config", "error", err)
			os.Exit(1)
		}
	}

	// Metrics collector
	if hookRegistry == nil {
		hookRegistry = core.NewHookRegistry()
	}
	metricsCollector := core.NewMetricsCollector()
	core.RegisterMetricsHooks(hookRegistry, metricsCollector)

	factory := &core.SessionFactory{
		Provider:      llmProvider,
		Tools:         tools,
		SkillRegistry: registry,
		Store:         store,
		Hooks:         hookRegistry,
		MaxTokens:     cfg.MaxTokens,
	}

	sessionMgr := core.NewSessionManager(core.SessionConfig{
		MaxSessions:     cfg.Sessions.MaxSessions,
		TTL:             cfg.Sessions.TTL,
		CleanupInterval: cfg.Sessions.CleanupInterval,
	}, factory)

	// Config hot reload
	cfgWatcher, err := config.NewConfigWatcher(*cfgPath, 1*time.Second)
	if err != nil {
		slog.Warn("config watcher creation failed", "error", err)
	} else {
		cfgWatcher.OnReload(func(oldCfg, newCfg *config.Config) {
			if sandbox != nil {
				sandbox.UpdateConfig(core.SandboxConfig{
					AllowedCommands: newCfg.Sandbox.AllowedCommands,
					BlockedCommands: newCfg.Sandbox.BlockedCommands,
					AllowedPaths:    newCfg.Sandbox.AllowedPaths,
					BlockedPaths:    newCfg.Sandbox.BlockedPaths,
					MaxOutputSize:   newCfg.Sandbox.MaxOutputSize,
				})
				slog.Info("sandbox config reloaded")
			}
			if hookRegistry != nil && newCfg.Hooks != nil {
				if err := hookRegistry.Reload(newCfg.Hooks); err != nil {
					slog.Warn("hooks reload failed", "error", err)
				} else {
					slog.Info("hooks config reloaded")
				}
			}
		})
		if err := cfgWatcher.Start(); err != nil {
			slog.Warn("config watcher start failed", "error", err)
		} else {
			defer cfgWatcher.Stop()
		}
	}

	r := gin.Default()

	r.GET("/metrics", func(c *gin.Context) {
		c.String(http.StatusOK, metricsCollector.PrometheusFormat())
	})

	v1 := r.Group("/v1")
	{
		v1.POST("/chat", chatHandler(sessionMgr))
		v1.POST("/chat/stream", streamChatHandler(sessionMgr))
		v1.GET("/sessions", listSessionsHandler(sessionMgr))
		v1.DELETE("/sessions/:id", deleteSessionHandler(sessionMgr))
		v1.GET("/memory/search", memorySearchHandler(mm))
		v1.GET("/status", statusHandler(mm))
		v1.GET("/metrics", func(c *gin.Context) {
			snap := metricsCollector.Snapshot()
			c.JSON(http.StatusOK, snap)
		})
	}

	addr := fmt.Sprintf(":%d", cfg.Server.Port)

	srv := &http.Server{Addr: addr, Handler: r}

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server failed", "error", err)
			os.Exit(1)
		}
	}()

	slog.Info("server started", "addr", addr)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutting down server...")

	shutdownTimeout := cfg.Lifecycle.ShutdownTimeout
	if err := sessionMgr.Shutdown(shutdownTimeout); err != nil {
		slog.Warn("session shutdown had errors", "error", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("server forced shutdown", "error", err)
	}

	slog.Info("server exited")
}

type ChatRequest struct {
	Message   string   `json:"message" binding:"required"`
	SessionID string   `json:"session_id,omitempty"`
	Skills    []string `json:"skills,omitempty"`
}

type ChatResponse struct {
	Reply     string   `json:"reply"`
	SessionID string   `json:"session_id"`
	ToolsUsed []string `json:"tools_used,omitempty"`
}

func chatHandler(sessionMgr *core.SessionManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req ChatRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		sessionID := req.SessionID
		if sessionID == "" {
			sessionID = generateID()
		}

		session, _, err := sessionMgr.GetOrCreate(sessionID)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}

		session.Lock()
		defer session.Unlock()

		reply, err := session.Agent.Run(c.Request.Context(), req.Message)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, ChatResponse{Reply: reply, SessionID: sessionID})
	}
}

func streamChatHandler(sessionMgr *core.SessionManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req ChatRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		sessionID := req.SessionID
		if sessionID == "" {
			sessionID = generateID()
		}

		session, _, err := sessionMgr.GetOrCreate(sessionID)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}

		session.Lock()
		defer session.Unlock()

		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Header("Connection", "keep-alive")

		ctx := c.Request.Context()

		onEvent := func(event llm.StreamEvent) {
			var data []byte
			switch event.Type {
			case "text_delta":
				data, _ = json.Marshal(gin.H{"type": "text_delta", "text": event.Text})
			case "thinking":
				data, _ = json.Marshal(gin.H{"type": "thinking", "text": event.Text})
			case "tool_use":
				if event.ToolUse != nil {
					data, _ = json.Marshal(gin.H{"type": "tool_use", "tool": event.ToolUse.Name, "input": json.RawMessage(event.ToolUse.Input)})
				}
			case "tool_use_start":
				if event.ToolUse != nil {
					data, _ = json.Marshal(gin.H{"type": "tool_use_start", "tool": event.ToolUse.Name, "input": json.RawMessage(event.ToolUse.Input)})
				}
			case "tool_result":
				if event.ToolUse != nil {
					data, _ = json.Marshal(gin.H{"type": "tool_result", "tool": event.ToolUse.Name, "output": event.ToolResult})
				}
			case "error":
				errMsg := "unknown error"
				if event.Error != nil {
					errMsg = event.Error.Error()
				}
				data, _ = json.Marshal(gin.H{"type": "error", "error": errMsg})
			}
			if data != nil {
				c.SSEvent("message", string(data))
				c.Writer.Flush()
			}
		}

		reply, err := session.Agent.RunStream(ctx, req.Message, onEvent)
		if err != nil {
			errData, _ := json.Marshal(gin.H{"type": "error", "error": err.Error()})
			c.SSEvent("message", string(errData))
			c.Writer.Flush()
			return
		}

		doneData, _ := json.Marshal(gin.H{"type": "done", "reply": reply, "session_id": sessionID})
		c.SSEvent("message", string(doneData))
		c.Writer.Flush()
	}
}

func listSessionsHandler(sessionMgr *core.SessionManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessions := sessionMgr.List()
		type sessionInfo struct {
			ID        string    `json:"id"`
			CreatedAt time.Time `json:"created_at"`
			LastUsed  time.Time `json:"last_used"`
		}
		result := make([]sessionInfo, 0, len(sessions))
		for _, s := range sessions {
			result = append(result, sessionInfo{ID: s.ID, CreatedAt: s.CreatedAt, LastUsed: s.LastUsed})
		}
		c.JSON(http.StatusOK, gin.H{"sessions": result})
	}
}

func deleteSessionHandler(sessionMgr *core.SessionManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		if err := sessionMgr.Delete(id); err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "session deleted"})
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

func generateID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}
