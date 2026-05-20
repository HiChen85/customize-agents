package memory

import (
	"fmt"

	"github.com/haichen-zhang/customize-agents/llm"
)

type WorkingMemory struct {
	messages   []llm.Message
	maxTokens  int
	tokenCount int
	tokenizer  Tokenizer
	keepRecent int
}

func NewWorkingMemory(maxTokens int, tokenizer Tokenizer) *WorkingMemory {
	return &WorkingMemory{
		messages:   make([]llm.Message, 0),
		maxTokens:  maxTokens,
		tokenCount: 0,
		tokenizer:  tokenizer,
		keepRecent: 10,
	}
}

func (wm *WorkingMemory) Append(msg llm.Message) {
	tokens := wm.countMessage(msg)
	wm.messages = append(wm.messages, msg)
	wm.tokenCount += tokens

	if wm.tokenCount > wm.maxTokens {
		wm.compact()
	}
}

func (wm *WorkingMemory) GetMessages() []llm.Message {
	result := make([]llm.Message, len(wm.messages))
	copy(result, wm.messages)
	return result
}

func (wm *WorkingMemory) TokenCount() int {
	return wm.tokenCount
}

func (wm *WorkingMemory) MaxTokens() int {
	return wm.maxTokens
}

func (wm *WorkingMemory) Clear() {
	wm.messages = wm.messages[:0]
	wm.tokenCount = 0
}

func (wm *WorkingMemory) compact() {
	if len(wm.messages) <= wm.keepRecent {
		return
	}

	keep := wm.keepRecent
	if keep > len(wm.messages) {
		keep = len(wm.messages)
	}

	oldMessages := wm.messages[:len(wm.messages)-keep]
	summary := wm.summarizeMessages(oldMessages)

	summaryMsg := llm.Message{
		Role:    "user",
		Content: []llm.Block{llm.TextBlock{Text: "[Earlier conversation summary]: " + summary}},
	}

	newMessages := make([]llm.Message, 0, 1+keep)
	newMessages = append(newMessages, summaryMsg)
	newMessages = append(newMessages, wm.messages[len(wm.messages)-keep:]...)

	wm.messages = newMessages
	wm.recountTokens()
}

func (wm *WorkingMemory) summarizeMessages(messages []llm.Message) string {
	var summary string
	for _, msg := range messages {
		for _, block := range msg.Content {
			if tb, ok := block.(llm.TextBlock); ok {
				if len(tb.Text) > 50 {
					summary += msg.Role + ": " + tb.Text[:50] + "... "
				} else {
					summary += msg.Role + ": " + tb.Text + " "
				}
			}
		}
	}
	if len(summary) > 200 {
		summary = summary[:200]
	}
	return fmt.Sprintf("(%d messages summarized) %s", len(messages), summary)
}

func (wm *WorkingMemory) countMessage(msg llm.Message) int {
	total := wm.tokenizer.Count(msg.Role)
	for _, block := range msg.Content {
		switch b := block.(type) {
		case llm.TextBlock:
			total += wm.tokenizer.Count(b.Text)
		case llm.ToolUseBlock:
			total += wm.tokenizer.Count(b.Name) + wm.tokenizer.Count(string(b.Input))
		case llm.ToolResultBlock:
			total += wm.tokenizer.Count(b.Content)
		}
	}
	return total
}

func (wm *WorkingMemory) recountTokens() {
	wm.tokenCount = 0
	for _, msg := range wm.messages {
		wm.tokenCount += wm.countMessage(msg)
	}
}
