package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type AnthropicProvider struct {
	apiKey  string
	baseURL string
	model   string
	client  *http.Client
}

func NewAnthropicProvider(apiKey, baseURL, model string) *AnthropicProvider {
	return &AnthropicProvider{
		apiKey:  apiKey,
		baseURL: baseURL,
		model:   model,
		client:  &http.Client{},
	}
}

func (p *AnthropicProvider) CreateMessage(ctx context.Context, req Request) (*Response, error) {
	body := p.buildRequestBody(req)
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/v1/messages", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (status %d): %s", httpResp.StatusCode, string(respBody))
	}

	return p.parseResponse(respBody)
}

func (p *AnthropicProvider) buildRequestBody(req Request) map[string]any {
	messages := make([]map[string]any, 0, len(req.Messages))
	for _, msg := range req.Messages {
		content := make([]map[string]any, 0, len(msg.Content))
		for _, block := range msg.Content {
			switch b := block.(type) {
			case ThinkingBlock:
				content = append(content, map[string]any{"type": "thinking", "thinking": b.Thinking})
			case TextBlock:
				content = append(content, map[string]any{"type": "text", "text": b.Text})
			case ToolUseBlock:
				content = append(content, map[string]any{"type": "tool_use", "id": b.ID, "name": b.Name, "input": b.Input})
			case ToolResultBlock:
				content = append(content, map[string]any{"type": "tool_result", "tool_use_id": b.ToolUseID, "content": b.Content, "is_error": b.IsError})
			}
		}
		messages = append(messages, map[string]any{"role": msg.Role, "content": content})
	}

	body := map[string]any{
		"model":      p.model,
		"max_tokens": req.MaxTokens,
		"messages":   messages,
	}

	if req.Model != "" {
		body["model"] = req.Model
	}
	if req.System != "" {
		body["system"] = req.System
	}
	if len(req.Tools) > 0 {
		tools := make([]map[string]any, 0, len(req.Tools))
		for _, t := range req.Tools {
			tools = append(tools, map[string]any{
				"name":         t.Name,
				"description":  t.Description,
				"input_schema": t.InputSchema,
			})
		}
		body["tools"] = tools
	}

	return body
}

func (p *AnthropicProvider) parseResponse(data []byte) (*Response, error) {
	var raw struct {
		Content    []json.RawMessage `json:"content"`
		StopReason string            `json:"stop_reason"`
		Usage      struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}

	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	blocks := make([]Block, 0, len(raw.Content))
	for _, rawBlock := range raw.Content {
		var blockType struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(rawBlock, &blockType); err != nil {
			return nil, fmt.Errorf("unmarshal block type: %w", err)
		}

		switch blockType.Type {
		case "thinking":
			var tb struct {
				Thinking string `json:"thinking"`
			}
			json.Unmarshal(rawBlock, &tb)
			blocks = append(blocks, ThinkingBlock{Thinking: tb.Thinking})
		case "text":
			var tb struct {
				Text string `json:"text"`
			}
			json.Unmarshal(rawBlock, &tb)
			blocks = append(blocks, TextBlock{Text: tb.Text})
		case "tool_use":
			var tu struct {
				ID    string          `json:"id"`
				Name  string          `json:"name"`
				Input json.RawMessage `json:"input"`
			}
			json.Unmarshal(rawBlock, &tu)
			blocks = append(blocks, ToolUseBlock{ID: tu.ID, Name: tu.Name, Input: tu.Input})
		}
	}

	return &Response{
		Content:    blocks,
		StopReason: raw.StopReason,
		Usage: Usage{
			InputTokens:  raw.Usage.InputTokens,
			OutputTokens: raw.Usage.OutputTokens,
		},
	}, nil
}
