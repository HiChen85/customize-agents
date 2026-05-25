package llm

import (
	"context"
	"fmt"
	"testing"
	"time"
)

type failingProvider struct {
	failCount int
	callCount int
	lastResp  *Response
}

func (f *failingProvider) CreateMessage(ctx context.Context, req Request) (*Response, error) {
	f.callCount++
	if f.callCount <= f.failCount {
		return nil, fmt.Errorf("API error (status 529): overloaded")
	}
	return f.lastResp, nil
}

func TestRetryProvider_SucceedsAfterRetries(t *testing.T) {
	inner := &failingProvider{
		failCount: 2,
		lastResp:  &Response{Content: []Block{TextBlock{Text: "hello"}}, StopReason: "end_turn"},
	}
	provider := NewRetryProvider(inner, RetryConfig{MaxRetries: 3, BaseDelay: 10 * time.Millisecond, MaxDelay: 100 * time.Millisecond, RetryableFunc: DefaultRetryable})
	resp, err := provider.CreateMessage(context.Background(), Request{})
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if resp.Content[0].(TextBlock).Text != "hello" {
		t.Error("wrong response")
	}
	if inner.callCount != 3 {
		t.Errorf("expected 3 calls, got %d", inner.callCount)
	}
}

func TestRetryProvider_ExhaustsRetries(t *testing.T) {
	inner := &failingProvider{failCount: 10, lastResp: &Response{}}
	provider := NewRetryProvider(inner, RetryConfig{MaxRetries: 2, BaseDelay: 5 * time.Millisecond, MaxDelay: 50 * time.Millisecond, RetryableFunc: DefaultRetryable})
	_, err := provider.CreateMessage(context.Background(), Request{})
	if err == nil {
		t.Fatal("expected error")
	}
	if inner.callCount != 3 {
		t.Errorf("expected 3 calls, got %d", inner.callCount)
	}
}

func TestRetryProvider_NonRetryableError(t *testing.T) {
	inner := &failingProvider{failCount: 10, lastResp: &Response{}}
	provider := NewRetryProvider(inner, RetryConfig{MaxRetries: 3, BaseDelay: 5 * time.Millisecond, RetryableFunc: func(err error) bool { return false }})
	_, err := provider.CreateMessage(context.Background(), Request{})
	if err == nil {
		t.Fatal("expected error")
	}
	if inner.callCount != 1 {
		t.Errorf("expected 1 call, got %d", inner.callCount)
	}
}

// --- Streaming tests ---

type mockStreamProvider struct {
	streamFunc func(ctx context.Context, req Request) (<-chan StreamEvent, error)
}

func (m *mockStreamProvider) CreateMessage(ctx context.Context, req Request) (*Response, error) {
	return &Response{Content: []Block{TextBlock{Text: "non-stream"}}}, nil
}

func (m *mockStreamProvider) CreateMessageStream(ctx context.Context, req Request) (<-chan StreamEvent, error) {
	return m.streamFunc(ctx, req)
}

type mockNonStreamProvider struct {
	response *Response
}

func (m *mockNonStreamProvider) CreateMessage(ctx context.Context, req Request) (*Response, error) {
	return m.response, nil
}

func TestRetryProvider_CreateMessageStream_Success(t *testing.T) {
	callCount := 0
	inner := &mockStreamProvider{
		streamFunc: func(ctx context.Context, req Request) (<-chan StreamEvent, error) {
			callCount++
			ch := make(chan StreamEvent, 2)
			ch <- StreamEvent{Type: "text_delta", Text: "hello"}
			ch <- StreamEvent{Type: "done"}
			close(ch)
			return ch, nil
		},
	}

	retry := NewRetryProvider(inner, RetryConfig{MaxRetries: 3, BaseDelay: 1 * time.Millisecond})
	ch, err := retry.CreateMessageStream(context.Background(), Request{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var events []StreamEvent
	for e := range ch {
		events = append(events, e)
	}

	if callCount != 1 {
		t.Errorf("expected 1 call, got %d", callCount)
	}
	if len(events) != 2 {
		t.Errorf("expected 2 events, got %d", len(events))
	}
}

func TestRetryProvider_CreateMessageStream_RetriesOnConnectionError(t *testing.T) {
	callCount := 0
	inner := &mockStreamProvider{
		streamFunc: func(ctx context.Context, req Request) (<-chan StreamEvent, error) {
			callCount++
			if callCount < 3 {
				return nil, fmt.Errorf("connection refused")
			}
			ch := make(chan StreamEvent, 1)
			ch <- StreamEvent{Type: "done"}
			close(ch)
			return ch, nil
		},
	}

	retry := NewRetryProvider(inner, RetryConfig{MaxRetries: 3, BaseDelay: 1 * time.Millisecond})
	ch, err := retry.CreateMessageStream(context.Background(), Request{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	events := []StreamEvent{}
	for e := range ch {
		events = append(events, e)
	}

	if callCount != 3 {
		t.Errorf("expected 3 calls, got %d", callCount)
	}
}

func TestRetryProvider_CreateMessageStream_ExhaustedRetries(t *testing.T) {
	inner := &mockStreamProvider{
		streamFunc: func(ctx context.Context, req Request) (<-chan StreamEvent, error) {
			return nil, fmt.Errorf("connection refused")
		},
	}

	retry := NewRetryProvider(inner, RetryConfig{MaxRetries: 2, BaseDelay: 1 * time.Millisecond})
	_, err := retry.CreateMessageStream(context.Background(), Request{})
	if err == nil {
		t.Fatal("expected error after exhausted retries")
	}
}

func TestRetryProvider_CreateMessageStream_FallbackToNonStream(t *testing.T) {
	inner := &mockNonStreamProvider{
		response: &Response{Content: []Block{TextBlock{Text: "fallback"}}},
	}

	retry := NewRetryProvider(inner, RetryConfig{MaxRetries: 2, BaseDelay: 1 * time.Millisecond})
	ch, err := retry.CreateMessageStream(context.Background(), Request{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var events []StreamEvent
	for e := range ch {
		events = append(events, e)
	}

	if len(events) != 2 {
		t.Fatalf("expected 2 events (text_delta + done), got %d", len(events))
	}
	if events[0].Text != "fallback" {
		t.Errorf("expected 'fallback', got '%s'", events[0].Text)
	}
}
