package mcp

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"sync"
	"time"
)

type Client struct {
	transport Transport
	nextID    int
	mu        sync.Mutex
}

type ClientConfig struct {
	Name      string
	Command   string
	URL       string
	Transport string
}

func NewStdioClient(command string) (*Client, error) {
	cmd := exec.Command("bash", "-c", command)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("get stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("get stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start command: %w", err)
	}

	transport := NewStdioTransport(stdout, stdin)
	return &Client{transport: transport, nextID: 1}, nil
}

func (c *Client) ListTools() ([]ToolDefinition, error) {
	resp, err := c.call("tools/list", json.RawMessage(`{}`))
	if err != nil {
		return nil, err
	}

	var result ListToolsResult
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, fmt.Errorf("parse tools list: %w", err)
	}
	return result.Tools, nil
}

func (c *Client) CallTool(name string, arguments json.RawMessage) (string, error) {
	params := ToolCallParams{Name: name, Arguments: arguments}
	paramsData, _ := json.Marshal(params)

	resp, err := c.call("tools/call", paramsData)
	if err != nil {
		return "", err
	}

	var result ToolCallResult
	if err := json.Unmarshal(resp, &result); err != nil {
		return "", fmt.Errorf("parse tool result: %w", err)
	}

	if result.IsError {
		return "", fmt.Errorf("tool error: %s", result.Content[0].Text)
	}

	var text string
	for _, block := range result.Content {
		if block.Type == "text" {
			text += block.Text
		}
	}
	return text, nil
}

func (c *Client) Close() error {
	return c.transport.Close()
}

func (c *Client) call(method string, params json.RawMessage) (json.RawMessage, error) {
	c.mu.Lock()
	id := c.nextID
	c.nextID++
	c.mu.Unlock()

	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	if err := c.transport.Send(req); err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}

	data, err := c.transport.Receive()
	if err != nil {
		return nil, fmt.Errorf("receive response: %w", err)
	}

	var resp JSONRPCResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("RPC error %d: %s", resp.Error.Code, resp.Error.Message)
	}

	return resp.Result, nil
}

type ReconnectConfig struct {
	MaxRetries int
	RetryDelay time.Duration
}

type ConnectFunc func() (Transport, error)

type ReconnectingClient struct {
	Client
	connectFunc ConnectFunc
	config      ReconnectConfig
}

func NewReconnectingClient(connectFunc ConnectFunc, config ReconnectConfig) *ReconnectingClient {
	if config.MaxRetries <= 0 {
		config.MaxRetries = 3
	}
	if config.RetryDelay <= 0 {
		config.RetryDelay = 1 * time.Second
	}
	transport, _ := connectFunc()
	return &ReconnectingClient{
		Client:      Client{transport: transport, nextID: 1},
		connectFunc: connectFunc,
		config:      config,
	}
}

func (rc *ReconnectingClient) ListTools() ([]ToolDefinition, error) {
	tools, err := rc.Client.ListTools()
	if err == nil {
		return tools, nil
	}
	if reconnErr := rc.reconnect(); reconnErr != nil {
		return nil, fmt.Errorf("list tools failed and reconnect failed: %w (original: %v)", reconnErr, err)
	}
	return rc.Client.ListTools()
}

func (rc *ReconnectingClient) CallTool(name string, arguments json.RawMessage) (string, error) {
	result, err := rc.Client.CallTool(name, arguments)
	if err == nil {
		return result, nil
	}
	if reconnErr := rc.reconnect(); reconnErr != nil {
		return "", fmt.Errorf("call tool failed and reconnect failed: %w (original: %v)", reconnErr, err)
	}
	return rc.Client.CallTool(name, arguments)
}

func (rc *ReconnectingClient) reconnect() error {
	for attempt := 0; attempt < rc.config.MaxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(rc.config.RetryDelay)
		}
		transport, err := rc.connectFunc()
		if err == nil {
			rc.mu.Lock()
			rc.transport = transport
			rc.nextID = 1
			rc.mu.Unlock()
			return nil
		}
	}
	return fmt.Errorf("reconnect failed after %d attempts", rc.config.MaxRetries)
}
