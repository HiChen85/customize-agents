package memory

import (
	"testing"

	"github.com/haichen-zhang/customize-agents/llm"
)

func TestSimpleTokenizer_Count(t *testing.T) {
	tok := &SimpleTokenizer{}

	count := tok.Count("Hello world")
	if count <= 0 {
		t.Errorf("expected positive token count, got %d", count)
	}

	chineseCount := tok.Count("你好世界")
	if chineseCount <= 0 {
		t.Errorf("expected positive token count for Chinese, got %d", chineseCount)
	}
}

func TestWorkingMemory_AppendAndCompact(t *testing.T) {
	wm := NewWorkingMemory(100, &SimpleTokenizer{})

	for i := 0; i < 20; i++ {
		msg := llm.Message{
			Role:    "user",
			Content: []llm.Block{llm.TextBlock{Text: "This is a test message with some content to use tokens."}},
		}
		wm.Append(msg)
	}

	messages := wm.GetMessages()
	if len(messages) >= 20 {
		t.Errorf("expected compaction to reduce messages, still have %d", len(messages))
	}
	if len(messages) == 0 {
		t.Errorf("expected some messages to remain after compaction")
	}
}

func TestWorkingMemory_TokenCount(t *testing.T) {
	wm := NewWorkingMemory(1000, &SimpleTokenizer{})
	msg := llm.Message{
		Role:    "user",
		Content: []llm.Block{llm.TextBlock{Text: "Hello"}},
	}
	wm.Append(msg)

	if wm.TokenCount() <= 0 {
		t.Errorf("expected positive token count, got %d", wm.TokenCount())
	}
}
