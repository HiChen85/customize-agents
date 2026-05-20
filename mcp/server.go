package mcp

import (
	"encoding/json"
	"fmt"
	"log/slog"
)

type ToolHandler interface {
	ListTools() []ToolDefinition
	CallTool(name string, arguments json.RawMessage) (string, error)
}

type Server struct {
	handler   ToolHandler
	transport Transport
}

func NewServer(handler ToolHandler, transport Transport) *Server {
	return &Server{
		handler:   handler,
		transport: transport,
	}
}

func (s *Server) Serve() error {
	for {
		if err := s.HandleOne(); err != nil {
			return err
		}
	}
}

func (s *Server) HandleOne() error {
	data, err := s.transport.Receive()
	if err != nil {
		return fmt.Errorf("receive: %w", err)
	}

	var req JSONRPCRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return fmt.Errorf("unmarshal request: %w", err)
	}

	var result any
	var rpcErr *JSONRPCError

	switch req.Method {
	case "tools/list":
		tools := s.handler.ListTools()
		result = ListToolsResult{Tools: tools}
	case "tools/call":
		var params ToolCallParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			rpcErr = &JSONRPCError{Code: -32602, Message: "invalid params"}
			break
		}
		output, err := s.handler.CallTool(params.Name, params.Arguments)
		if err != nil {
			result = ToolCallResult{
				Content: []ContentBlock{{Type: "text", Text: err.Error()}},
				IsError: true,
			}
		} else {
			result = ToolCallResult{
				Content: []ContentBlock{{Type: "text", Text: output}},
			}
		}
	default:
		rpcErr = &JSONRPCError{Code: -32601, Message: "method not found"}
	}

	resp := JSONRPCResponse{JSONRPC: "2.0", ID: req.ID}
	if rpcErr != nil {
		resp.Error = rpcErr
	} else {
		resultData, err := json.Marshal(result)
		if err != nil {
			slog.Error("marshal result failed", "error", err)
			resp.Error = &JSONRPCError{Code: -32603, Message: "internal error"}
		} else {
			resp.Result = resultData
		}
	}

	return s.transport.Send(resp)
}
