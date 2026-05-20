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
