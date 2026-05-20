package mcp

import (
	"encoding/json"
	"io"
	"testing"
)

func TestJSONRPCEncodeDecode(t *testing.T) {
	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/list",
		Params:  json.RawMessage(`{}`),
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded JSONRPCRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if decoded.Method != "tools/list" {
		t.Errorf("expected method 'tools/list', got '%s'", decoded.Method)
	}
	if decoded.ID != 1 {
		t.Errorf("expected id 1, got %v", decoded.ID)
	}
}

func TestStdioTransport_SendReceive(t *testing.T) {
	clientRead, serverWrite := io.Pipe()
	serverRead, clientWrite := io.Pipe()

	serverTransport := NewStdioTransport(serverRead, serverWrite)
	clientTransport := NewStdioTransport(clientRead, clientWrite)

	go func() {
		req := JSONRPCRequest{
			JSONRPC: "2.0",
			ID:      1,
			Method:  "tools/list",
			Params:  json.RawMessage(`{}`),
		}
		clientTransport.Send(req)
	}()

	msg, err := serverTransport.Receive()
	if err != nil {
		t.Fatalf("receive error: %v", err)
	}

	var req JSONRPCRequest
	if err := json.Unmarshal(msg, &req); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if req.Method != "tools/list" {
		t.Errorf("expected method 'tools/list', got '%s'", req.Method)
	}
}
