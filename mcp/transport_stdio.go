package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

type StdioTransport struct {
	reader *bufio.Reader
	writer io.Writer
	mu     sync.Mutex
}

func NewStdioTransport(r io.Reader, w io.Writer) *StdioTransport {
	return &StdioTransport{
		reader: bufio.NewReader(r),
		writer: w,
	}
}

func (t *StdioTransport) Send(msg any) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}

	_, err = fmt.Fprintf(t.writer, "%s\n", data)
	return err
}

func (t *StdioTransport) Receive() ([]byte, error) {
	line, err := t.reader.ReadBytes('\n')
	if err != nil {
		return nil, err
	}
	return line, nil
}

func (t *StdioTransport) Close() error {
	if closer, ok := t.writer.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}
