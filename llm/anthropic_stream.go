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
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	type toolBuffer struct {
		ID          string
		Name        string
		InputJSON   strings.Builder
		InlineInput json.RawMessage // from content_block_start, used as fallback
		RawLines    []string
	}

	var currentTool *toolBuffer
	var currentThinking *strings.Builder
	var stopReason string

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
				id := event.ContentBlock.ID
				if id == "" {
					id = fmt.Sprintf("toolu_%d", event.Index)
				}
				currentTool = &toolBuffer{
					ID:   id,
					Name: event.ContentBlock.Name,
				}
				if len(event.ContentBlock.Input) > 0 && string(event.ContentBlock.Input) != "null" && string(event.ContentBlock.Input) != "{}" {
					currentTool.InlineInput = event.ContentBlock.Input
					slog.Debug("stream: tool_use block started with inline input", "tool", event.ContentBlock.Name, "input_len", len(event.ContentBlock.Input))
				} else {
					slog.Debug("stream: tool_use block started", "tool", event.ContentBlock.Name, "id", event.ContentBlock.ID)
				}
			} else if event.ContentBlock.Type == "thinking" {
				currentThinking = &strings.Builder{}
			}

		case "content_block_delta":
			if currentTool != nil {
				// Inside a tool_use block — route ALL delta content as tool input.
				// Deepseek may use text_delta, input_json_delta, or other types.
				text := event.Delta.PartialJSON
				if text == "" {
					text = event.Delta.JSON
				}
				if text == "" {
					text = event.Delta.Text
				}
				if text == "" {
					text = event.Delta.Thinking
				}
				if text != "" {
					currentTool.InputJSON.WriteString(text)
				}
			} else if currentThinking != nil {
				text := event.Delta.Thinking
				if text == "" {
					text = event.Delta.Text
				}
				if text != "" {
					currentThinking.WriteString(text)
				}
			} else {
				if event.Delta.Text != "" {
					ch <- StreamEvent{Type: "text_delta", Text: event.Delta.Text}
				}
			}

		case "content_block_stop":
			if currentTool != nil {
				inputJSON := currentTool.InputJSON.String()
				if inputJSON == "" && len(currentTool.InlineInput) > 0 {
					// No deltas arrived — use the inline input from content_block_start
					inputJSON = string(currentTool.InlineInput)
					slog.Debug("stream: using inline input (no deltas received)", "tool", currentTool.Name)
				}
				if inputJSON == "" {
					slog.Warn("tool input JSON is empty, defaulting to {}", "tool", currentTool.Name,
						"raw_lines", len(currentTool.RawLines))
					inputJSON = "{}"
				} else if !json.Valid([]byte(inputJSON)) {
					repaired := repairTruncatedJSON(inputJSON)
					if repaired != "" {
						slog.Warn("tool input JSON was truncated, repaired", "tool", currentTool.Name, "original_len", len(inputJSON))
						inputJSON = repaired
					} else {
						slog.Warn("tool input JSON is invalid and unrepairable", "tool", currentTool.Name, "len", len(inputJSON),
							"first_100", inputJSON[:min(100, len(inputJSON))])
						inputJSON = "{}"
					}
				}
				// Unwrap string-encoded JSON (Deepseek may double-encode input)
				if len(inputJSON) > 0 && inputJSON[0] == '"' {
					var inner string
					if json.Unmarshal([]byte(inputJSON), &inner) == nil && json.Valid([]byte(inner)) {
						slog.Debug("stream: unwrapped string-encoded tool input", "tool", currentTool.Name)
						inputJSON = inner
					}
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

		case "message_delta":
			if event.Delta.StopReason != "" {
				stopReason = event.Delta.StopReason
			}

		case "message_stop":
			ch <- StreamEvent{Type: "done", StopReason: stopReason}
			return
		}
	}

	// Handle incomplete tool if stream ended mid-tool
	if currentTool != nil {
		inputJSON := currentTool.InputJSON.String()
		if inputJSON == "" && len(currentTool.InlineInput) > 0 {
			inputJSON = string(currentTool.InlineInput)
		}
		if inputJSON == "" || !json.Valid([]byte(inputJSON)) {
			repaired := repairTruncatedJSON(inputJSON)
			if repaired != "" {
				inputJSON = repaired
			} else {
				inputJSON = "{}"
			}
		}
		if len(inputJSON) > 0 && inputJSON[0] == '"' {
			var inner string
			if json.Unmarshal([]byte(inputJSON), &inner) == nil && json.Valid([]byte(inner)) {
				inputJSON = inner
			}
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

// repairTruncatedJSON attempts to recover key-value pairs from truncated JSON.
// When LLM output hits token limit mid-stream, the tool input JSON is incomplete.
// This extracts whatever complete string fields exist from the beginning.
func repairTruncatedJSON(raw string) string {
	if !strings.HasPrefix(raw, "{") {
		return ""
	}

	result := make(map[string]string)
	remaining := raw[1:] // skip opening brace

	for {
		remaining = strings.TrimSpace(remaining)
		if remaining == "" || remaining[0] == '}' {
			break
		}
		if remaining[0] == ',' {
			remaining = remaining[1:]
			remaining = strings.TrimSpace(remaining)
		}

		// expect a key: "key"
		if remaining[0] != '"' {
			break
		}
		keyEnd := findClosingQuote(remaining, 0)
		if keyEnd < 0 {
			break
		}
		key := remaining[1:keyEnd]
		remaining = remaining[keyEnd+1:]

		// expect colon
		remaining = strings.TrimSpace(remaining)
		if len(remaining) == 0 || remaining[0] != ':' {
			break
		}
		remaining = strings.TrimSpace(remaining[1:])

		// expect value (we only handle string values)
		if len(remaining) == 0 {
			break
		}
		if remaining[0] != '"' {
			// non-string value (number, bool, etc) — skip to next comma or end
			nextComma := strings.IndexByte(remaining, ',')
			nextBrace := strings.IndexByte(remaining, '}')
			if nextComma >= 0 {
				val := strings.TrimSpace(remaining[:nextComma])
				result[key] = val
				remaining = remaining[nextComma:]
			} else if nextBrace >= 0 {
				val := strings.TrimSpace(remaining[:nextBrace])
				result[key] = val
				remaining = remaining[nextBrace:]
			} else {
				break
			}
			continue
		}

		valEnd := findClosingQuote(remaining, 0)
		if valEnd < 0 {
			// String value is truncated — take what we have
			// The value started but never closed. Extract partial content.
			partial := remaining[1:]
			// Unescape what we can
			partial = strings.ReplaceAll(partial, "\\n", "\n")
			partial = strings.ReplaceAll(partial, "\\\"", "\"")
			partial = strings.ReplaceAll(partial, "\\\\", "\\")
			result[key] = partial + "\n[TRUNCATED - output token limit reached]"
			break
		}
		value := remaining[1:valEnd]
		value = strings.ReplaceAll(value, "\\n", "\n")
		value = strings.ReplaceAll(value, "\\\"", "\"")
		value = strings.ReplaceAll(value, "\\\\", "\\")
		result[key] = value
		remaining = remaining[valEnd+1:]
	}

	if len(result) == 0 {
		return ""
	}

	// Rebuild as valid JSON
	repaired, err := json.Marshal(result)
	if err != nil {
		return ""
	}
	return string(repaired)
}

// findClosingQuote finds the index of the closing quote for a JSON string starting at pos.
// Returns -1 if not found.
func findClosingQuote(s string, pos int) int {
	i := pos + 1
	for i < len(s) {
		if s[i] == '\\' {
			i += 2
			continue
		}
		if s[i] == '"' {
			return i
		}
		i++
	}
	return -1
}
