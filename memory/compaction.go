package memory

import (
	"context"
	"fmt"
	"strings"

	"github.com/haichen-zhang/customize-agents/llm"
)

const defaultCompactionPrompt = `Summarize the following conversation concisely. Preserve:
- Key facts and decisions made
- File paths, variable names, or technical details mentioned
- User preferences and constraints stated
- The current task/goal being worked on

Conversation:
%s

Provide a concise summary in 2-4 sentences.`

type CompactionConfig struct {
	Threshold float64
	Provider  llm.Provider
	Model     string
	Prompt    string
}

type Compactor struct {
	config CompactionConfig
	prompt string
}

func NewCompactor(config CompactionConfig) *Compactor {
	prompt := config.Prompt
	if prompt == "" {
		prompt = defaultCompactionPrompt
	}
	if config.Threshold <= 0 {
		config.Threshold = 0.8
	}
	return &Compactor{config: config, prompt: prompt}
}

func (c *Compactor) Threshold() float64 {
	return c.config.Threshold
}

func (c *Compactor) Summarize(ctx context.Context, messages []llm.Message) (string, error) {
	if len(messages) == 0 {
		return "", nil
	}

	conversationText := c.formatMessages(messages)
	prompt := fmt.Sprintf(c.prompt, conversationText)

	req := llm.Request{
		System:    "You are a conversation summarizer. Be concise and preserve important details.",
		Messages:  []llm.Message{{Role: "user", Content: []llm.Block{llm.TextBlock{Text: prompt}}}},
		MaxTokens: 500,
	}
	if c.config.Model != "" {
		req.Model = c.config.Model
	}

	resp, err := c.config.Provider.CreateMessage(ctx, req)
	if err != nil {
		return "", fmt.Errorf("summarization failed: %w", err)
	}

	for _, block := range resp.Content {
		if tb, ok := block.(llm.TextBlock); ok {
			return tb.Text, nil
		}
	}

	return "", fmt.Errorf("no text in summarization response")
}

func (c *Compactor) formatMessages(messages []llm.Message) string {
	var sb strings.Builder
	for _, msg := range messages {
		for _, block := range msg.Content {
			if tb, ok := block.(llm.TextBlock); ok {
				sb.WriteString(msg.Role)
				sb.WriteString(": ")
				sb.WriteString(tb.Text)
				sb.WriteString("\n")
			}
		}
	}
	return sb.String()
}
