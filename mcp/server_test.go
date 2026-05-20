package mcp

import (
	"encoding/json"
	"io"
	"testing"
)

func TestMCPServer_HandleListTools(t *testing.T) {
	clientRead, serverWrite := io.Pipe()
	serverRead, clientWrite := io.Pipe()

	tools := []ToolDefinition{
		{Name: "ask", Description: "Ask the agent a question", InputSchema: json.RawMessage(`{"type":"object","properties":{"question":{"type":"string"}}}`)},
	}

	handler := &MockToolHandler{tools: tools}
	server := NewServer(handler, NewStdioTransport(serverRead, serverWrite))

	go server.HandleOne()

	clientTransport := NewStdioTransport(clientRead, clientWrite)
	req := JSONRPCRequest{JSONRPC: "2.0", ID: 1, Method: "tools/list", Params: json.RawMessage(`{}`)}
	clientTransport.Send(req)

	data, err := clientTransport.Receive()
	if err != nil {
		t.Fatalf("receive error: %v", err)
	}
	var resp JSONRPCResponse
	json.Unmarshal(data, &resp)

	var result ListToolsResult
	json.Unmarshal(resp.Result, &result)

	if len(result.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(result.Tools))
	}
	if result.Tools[0].Name != "ask" {
		t.Errorf("expected tool 'ask', got '%s'", result.Tools[0].Name)
	}
}

func TestMCPServer_HandleCallTool(t *testing.T) {
	clientRead, serverWrite := io.Pipe()
	serverRead, clientWrite := io.Pipe()

	tools := []ToolDefinition{
		{Name: "echo", Description: "Echo back", InputSchema: json.RawMessage(`{"type":"object"}`)},
	}
	handler := &MockToolHandler{tools: tools, callResult: "echoed: hello"}
	server := NewServer(handler, NewStdioTransport(serverRead, serverWrite))

	go server.HandleOne()

	clientTransport := NewStdioTransport(clientRead, clientWrite)
	params, _ := json.Marshal(ToolCallParams{Name: "echo", Arguments: json.RawMessage(`{"msg":"hello"}`)})
	req := JSONRPCRequest{JSONRPC: "2.0", ID: 2, Method: "tools/call", Params: params}
	clientTransport.Send(req)

	data, _ := clientTransport.Receive()
	var resp JSONRPCResponse
	json.Unmarshal(data, &resp)

	var result ToolCallResult
	json.Unmarshal(resp.Result, &result)

	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(result.Content))
	}
	if result.Content[0].Text != "echoed: hello" {
		t.Errorf("expected 'echoed: hello', got '%s'", result.Content[0].Text)
	}
}

type MockToolHandler struct {
	tools      []ToolDefinition
	callResult string
}

func (m *MockToolHandler) ListTools() []ToolDefinition {
	return m.tools
}

func (m *MockToolHandler) CallTool(name string, arguments json.RawMessage) (string, error) {
	return m.callResult, nil
}
