package memory

import (
	"context"

	"github.com/haichen-zhang/customize-agents/llm"
)

type MemoryManager struct {
	Working  *WorkingMemory
	LongTerm LongTermStore
}

func NewMemoryManager(longTerm LongTermStore, maxTokens int) *MemoryManager {
	return &MemoryManager{
		Working:  NewWorkingMemory(maxTokens, &SimpleTokenizer{}),
		LongTerm: longTerm,
	}
}

func (mm *MemoryManager) AppendMessage(msg llm.Message) {
	mm.Working.Append(msg)
}

func (mm *MemoryManager) RetrieveRelevant(ctx context.Context, query string, limit int) ([]Entry, error) {
	return mm.LongTerm.Search(ctx, query, limit)
}

func (mm *MemoryManager) BuildMemoryPrompt(ctx context.Context, userInput string) string {
	entries, err := mm.LongTerm.Search(ctx, userInput, 5)
	if err != nil || len(entries) == 0 {
		return ""
	}

	prompt := "\n\n## Relevant memories:\n"
	for _, e := range entries {
		prompt += "- " + e.Content + "\n"
	}
	return prompt
}

func (mm *MemoryManager) GetContextMessages() []llm.Message {
	return mm.Working.GetMessages()
}

func (mm *MemoryManager) TokenUsage() (used int, max int) {
	return mm.Working.TokenCount(), mm.Working.MaxTokens()
}
