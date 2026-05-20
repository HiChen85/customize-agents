package memory

type Tokenizer interface {
	Count(text string) int
}

type SimpleTokenizer struct{}

func (t *SimpleTokenizer) Count(text string) int {
	runes := []rune(text)
	return (len(runes)*2 + 2) / 3
}
