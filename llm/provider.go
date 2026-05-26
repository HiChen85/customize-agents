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

type ThinkingBlock struct {
	Thinking string
}

func (ThinkingBlock) blockType() string { return "thinking" }

type ToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type StreamEvent struct {
	Type     string         // "text_delta", "tool_use", "thinking", "error", "done"
	Text     string         // populated for text_delta
	ToolUse  *ToolUseBlock  // populated for tool_use (complete block)
	Thinking *ThinkingBlock // populated for thinking (complete block)
	Error    error          // populated for error type
}

type StreamProvider interface {
	Provider
	CreateMessageStream(ctx context.Context, req Request) (<-chan StreamEvent, error)
}
