package mcp

import (
	"encoding/json"
	"io"
	"testing"
)

func TestMCPClient_ListTools(t *testing.T) {
	clientRead, serverWrite := io.Pipe()
	serverRead, clientWrite := io.Pipe()

	client := &Client{
		transport: NewStdioTransport(clientRead, clientWrite),
		nextID:    1,
	}

	go func() {
		transport := NewStdioTransport(serverRead, serverWrite)
		msg, _ := transport.Receive()
		var req JSONRPCRequest
		json.Unmarshal(msg, &req)

		result := ListToolsResult{
			Tools: []ToolDefinition{
				{Name: "read_file", Description: "Read a file", InputSchema: json.RawMessage(`{"type":"object"}`)},
			},
		}
		resultData, _ := json.Marshal(result)
		resp := JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: resultData}
		transport.Send(resp)
	}()

	tools, err := client.ListTools()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if tools[0].Name != "read_file" {
		t.Errorf("expected tool 'read_file', got '%s'", tools[0].Name)
	}
}

func TestMCPClient_CallTool(t *testing.T) {
	clientRead, serverWrite := io.Pipe()
	serverRead, clientWrite := io.Pipe()

	client := &Client{
		transport: NewStdioTransport(clientRead, clientWrite),
		nextID:    1,
	}

	go func() {
		transport := NewStdioTransport(serverRead, serverWrite)
		msg, _ := transport.Receive()
		var req JSONRPCRequest
		json.Unmarshal(msg, &req)

		result := ToolCallResult{
			Content: []ContentBlock{{Type: "text", Text: "file contents here"}},
		}
		resultData, _ := json.Marshal(result)
		resp := JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: resultData}
		transport.Send(resp)
	}()

	result, err := client.CallTool("read_file", json.RawMessage(`{"path":"/tmp/test"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "file contents here" {
		t.Errorf("expected 'file contents here', got '%s'", result)
	}
}
