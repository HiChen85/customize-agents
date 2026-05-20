package llm

import (
	"context"
	"encoding/json"
)

type Provider interface {
	CreateMessage(ctx context.Context, req Request) (*Response, error)
}

type Request struct {
	Model     string
	System    string
	Messages  []Message
	Tools     []ToolDef
	MaxTokens int
}

type Response struct {
	Content    []Block
	StopReason string
	Usage      Usage
}

type Usage struct {
	InputTokens  int
	OutputTokens int
}

type Message struct {
	Role    string
	Content []Block
}

type Block interface {
	blockType() string
}

type TextBlock struct {
	Text string
}

func (TextBlock) blockType() string { return "text" }

type ToolUseBlock struct {
	ID    string
	Name  string
	Input json.RawMessage
}

func (ToolUseBlock) blockType() string { return "tool_use" }

type ToolResultBlock struct {
	ToolUseID string
	Content   string
	IsError   bool
}

func (ToolResultBlock) blockType() string { return "tool_result" }

type ToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}
