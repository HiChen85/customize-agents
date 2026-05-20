package llm

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"
)

type RetryConfig struct {
	MaxRetries    int
	BaseDelay     time.Duration
	MaxDelay      time.Duration
	RetryableFunc func(err error) bool
}

type RetryProvider struct {
	inner  Provider
	config RetryConfig
}

func NewRetryProvider(inner Provider, config RetryConfig) *RetryProvider {
	if config.MaxRetries <= 0 {
		config.MaxRetries = 3
	}
	if config.BaseDelay <= 0 {
		config.BaseDelay = 1 * time.Second
	}
	if config.MaxDelay <= 0 {
		config.MaxDelay = 30 * time.Second
	}
	if config.RetryableFunc == nil {
		config.RetryableFunc = DefaultRetryable
	}
	return &RetryProvider{inner: inner, config: config}
}

func (r *RetryProvider) CreateMessage(ctx context.Context, req Request) (*Response, error) {
	var lastErr error
	for attempt := 0; attempt <= r.config.MaxRetries; attempt++ {
		if attempt > 0 {
			delay := r.backoffDelay(attempt)
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("context cancelled during retry: %w (last error: %v)", ctx.Err(), lastErr)
			case <-time.After(delay):
			}
		}
		resp, err := r.inner.CreateMessage(ctx, req)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if !r.config.RetryableFunc(err) {
			return nil, err
		}
	}
	return nil, fmt.Errorf("exhausted %d retries: %w", r.config.MaxRetries, lastErr)
}

func (r *RetryProvider) backoffDelay(attempt int) time.Duration {
	delay := float64(r.config.BaseDelay) * math.Pow(2, float64(attempt-1))
	if delay > float64(r.config.MaxDelay) {
		delay = float64(r.config.MaxDelay)
	}
	return time.Duration(delay)
}

func DefaultRetryable(err error) bool {
	msg := err.Error()
	if strings.Contains(msg, "status 429") || strings.Contains(msg, "status 529") {
		return true
	}
	if strings.Contains(msg, "status 500") || strings.Contains(msg, "status 502") || strings.Contains(msg, "status 503") {
		return true
	}
	if strings.Contains(msg, "connection refused") || strings.Contains(msg, "timeout") {
		return true
	}
	return false
}
