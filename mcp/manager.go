package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/haichen-zhang/customize-agents/config"
	"github.com/haichen-zhang/customize-agents/core"
	"github.com/haichen-zhang/customize-agents/llm"
)

type MCPManager struct {
	clients    map[string]*ReconnectingClient
	toolRoutes map[string]string
	toolDefs   []ToolDefinition
	mu         sync.RWMutex
}

func NewMCPManager() *MCPManager {
	return &MCPManager{
		clients:    make(map[string]*ReconnectingClient),
		toolRoutes: make(map[string]string),
	}
}

func (m *MCPManager) Initialize(ctx context.Context, servers []config.MCPServerConfig) error {
	var lastErr error
	for _, srv := range servers {
		client, err := m.connectServer(srv)
		if err != nil {
			slog.Warn("MCP server connection failed, skipping", "name", srv.Name, "error", err)
			lastErr = err
			continue
		}
		m.clients[srv.Name] = client

		tools, err := client.ListTools()
		if err != nil {
			slog.Warn("MCP server list tools failed", "name", srv.Name, "error", err)
			lastErr = err
			continue
		}

		for _, td := range tools {
			m.toolRoutes[td.Name] = srv.Name
			m.toolDefs = append(m.toolDefs, td)
		}
		slog.Info("MCP server connected", "name", srv.Name, "tools", len(tools))
	}
	if lastErr != nil && len(m.clients) == 0 {
		return fmt.Errorf("all MCP servers failed to connect: %w", lastErr)
	}
	return nil
}

func (m *MCPManager) connectServer(srv config.MCPServerConfig) (*ReconnectingClient, error) {
	timeout := srv.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	connectFunc := func() (Transport, error) {
		cmd := exec.Command("bash", "-c", srv.Command)
		if len(srv.Env) > 0 {
			cmd.Env = os.Environ()
			for k, v := range srv.Env {
				cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
			}
		}
		stdin, err := cmd.StdinPipe()
		if err != nil {
			return nil, fmt.Errorf("get stdin pipe: %w", err)
		}
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return nil, fmt.Errorf("get stdout pipe: %w", err)
		}
		if err := cmd.Start(); err != nil {
			return nil, fmt.Errorf("start MCP server '%s': %w", srv.Name, err)
		}
		return NewStdioTransport(stdout, stdin), nil
	}

	client := NewReconnectingClient(connectFunc, ReconnectConfig{
		MaxRetries: 3,
		RetryDelay: 1 * time.Second,
	})
	if client.transport == nil {
		return nil, fmt.Errorf("failed to establish initial connection to '%s'", srv.Name)
	}
	return client, nil
}

func (m *MCPManager) GetTools(existingTools map[string]bool) []core.Tool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var tools []core.Tool
	for _, td := range m.toolDefs {
		serverName := m.toolRoutes[td.Name]
		tool := m.convertToolWithConflictCheck(serverName, td, existingTools, m.clients[serverName])
		tools = append(tools, tool)
	}
	return tools
}

func (m *MCPManager) convertTool(serverName string, td ToolDefinition, client *ReconnectingClient) core.Tool {
	return core.Tool{
		Definition: llm.ToolDef{
			Name:        td.Name,
			Description: td.Description,
			InputSchema: td.InputSchema,
		},
		Execute: func(ctx context.Context, input json.RawMessage) (string, error) {
			if client == nil {
				return "", fmt.Errorf("MCP server '%s' not connected", serverName)
			}
			return client.CallTool(td.Name, input)
		},
	}
}

func (m *MCPManager) convertToolWithConflictCheck(serverName string, td ToolDefinition, existingTools map[string]bool, client *ReconnectingClient) core.Tool {
	name := td.Name
	if existingTools != nil && existingTools[name] {
		name = serverName + "_" + name
	}

	return core.Tool{
		Definition: llm.ToolDef{
			Name:        name,
			Description: td.Description,
			InputSchema: td.InputSchema,
		},
		Execute: func(ctx context.Context, input json.RawMessage) (string, error) {
			if client == nil {
				return "", fmt.Errorf("MCP server '%s' not connected", serverName)
			}
			return client.CallTool(td.Name, input)
		},
	}
}

func (m *MCPManager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var lastErr error
	for name, client := range m.clients {
		if err := client.Close(); err != nil {
			slog.Warn("failed to close MCP client", "name", name, "error", err)
			lastErr = err
		}
	}
	m.clients = make(map[string]*ReconnectingClient)
	return lastErr
}
