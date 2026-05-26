package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
)

func (p *AnthropicProvider) CreateMessageStream(ctx context.Context, req Request) (<-chan StreamEvent, error) {
	body := p.buildRequestBody(req)
	body["stream"] = true
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

	if httpResp.StatusCode != http.StatusOK {
		defer httpResp.Body.Close()
		respBody, _ := io.ReadAll(httpResp.Body)
		return nil, fmt.Errorf("API error (status %d): %s", httpResp.StatusCode, string(respBody))
	}

	ch := make(chan StreamEvent, 16)
	go p.parseSSEStream(ctx, httpResp.Body, ch)
	return ch, nil
}

func (p *AnthropicProvider) parseSSEStream(ctx context.Context, body io.ReadCloser, ch chan<- StreamEvent) {
	defer close(ch)
	defer body.Close()

	scanner := bufio.NewScanner(body)

	type toolBuffer struct {
		ID        string
		Name      string
		InputJSON strings.Builder
		RawLines  []string
	}

	var currentTool *toolBuffer
	var currentThinking *strings.Builder

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			ch <- StreamEvent{Type: "error", Error: ctx.Err()}
			return
		default:
		}

		line := scanner.Text()

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")

		if currentTool != nil {
			currentTool.RawLines = append(currentTool.RawLines, data)
		}

		var event struct {
			Type         string `json:"type"`
			Index        int    `json:"index"`
			ContentBlock struct {
				Type  string          `json:"type"`
				ID    string          `json:"id"`
				Name  string          `json:"name"`
				Text  string          `json:"text"`
				Input json.RawMessage `json:"input"`
			} `json:"content_block"`
			Delta struct {
				Type        string `json:"type"`
				Text        string `json:"text"`
				Thinking    string `json:"thinking"`
				PartialJSON string `json:"partial_json"`
				JSON        string `json:"json"`
				StopReason  string `json:"stop_reason"`
			} `json:"delta"`
		}

		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		switch event.Type {
		case "content_block_start":
			if event.ContentBlock.Type == "tool_use" {
				currentTool = &toolBuffer{
					ID:   event.ContentBlock.ID,
					Name: event.ContentBlock.Name,
				}
				if len(event.ContentBlock.Input) > 0 && string(event.ContentBlock.Input) != "null" && string(event.ContentBlock.Input) != "{}" {
					currentTool.InputJSON.Write(event.ContentBlock.Input)
					slog.Debug("stream: tool_use block started with inline input", "tool", event.ContentBlock.Name, "input_len", len(event.ContentBlock.Input))
				} else {
					slog.Debug("stream: tool_use block started", "tool", event.ContentBlock.Name, "id", event.ContentBlock.ID)
				}
			} else if event.ContentBlock.Type == "thinking" {
				currentThinking = &strings.Builder{}
			}

		case "content_block_delta":
			if event.Delta.Type == "text_delta" {
				ch <- StreamEvent{Type: "text_delta", Text: event.Delta.Text}
			} else if event.Delta.Type == "input_json_delta" && currentTool != nil {
				if event.Delta.PartialJSON != "" {
					currentTool.InputJSON.WriteString(event.Delta.PartialJSON)
				} else if event.Delta.JSON != "" {
					currentTool.InputJSON.WriteString(event.Delta.JSON)
				} else if event.Delta.Text != "" {
					currentTool.InputJSON.WriteString(event.Delta.Text)
				}
			} else if event.Delta.Type == "thinking_delta" && currentThinking != nil {
				currentThinking.WriteString(event.Delta.Thinking)
			} else if currentTool != nil {
				if event.Delta.PartialJSON != "" {
					currentTool.InputJSON.WriteString(event.Delta.PartialJSON)
				} else if event.Delta.JSON != "" {
					currentTool.InputJSON.WriteString(event.Delta.JSON)
				} else if event.Delta.Type != "" {
					slog.Warn("stream: unrecognized delta type during tool input", "type", event.Delta.Type, "tool", currentTool.Name)
				}
			}

		case "content_block_stop":
			if currentTool != nil {
				inputJSON := currentTool.InputJSON.String()
				if inputJSON == "" {
					slog.Warn("tool input JSON is empty, defaulting to {}", "tool", currentTool.Name, "raw_lines_count", len(currentTool.RawLines))
					for i, rl := range currentTool.RawLines {
						if i < 10 {
							slog.Warn("  raw SSE line", "index", i, "data", rl)
						}
					}
					inputJSON = "{}"
				} else if !json.Valid([]byte(inputJSON)) {
					slog.Warn("tool input JSON is invalid, defaulting to {}", "tool", currentTool.Name, "raw", inputJSON)
					inputJSON = "{}"
				}
				ch <- StreamEvent{
					Type: "tool_use",
					ToolUse: &ToolUseBlock{
						ID:    currentTool.ID,
						Name:  currentTool.Name,
						Input: json.RawMessage(inputJSON),
					},
				}
				currentTool = nil
			} else if currentThinking != nil {
				ch <- StreamEvent{
					Type:     "thinking",
					Thinking: &ThinkingBlock{Thinking: currentThinking.String()},
				}
				currentThinking = nil
			}

		case "message_stop":
			ch <- StreamEvent{Type: "done"}
			return
		}
	}

	// Handle incomplete tool if stream ended mid-tool
	if currentTool != nil {
		inputJSON := currentTool.InputJSON.String()
		if inputJSON == "" || !json.Valid([]byte(inputJSON)) {
			inputJSON = "{}"
		}
		ch <- StreamEvent{
			Type: "tool_use",
			ToolUse: &ToolUseBlock{
				ID:    currentTool.ID,
				Name:  currentTool.Name,
				Input: json.RawMessage(inputJSON),
			},
		}
	}

	if err := scanner.Err(); err != nil {
		ch <- StreamEvent{Type: "error", Error: fmt.Errorf("stream read error: %w", err)}
	}
}
