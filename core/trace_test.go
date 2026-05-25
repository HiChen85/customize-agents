package core

import (
	"context"
	"testing"
)

func TestWithTraceID_AddsID(t *testing.T) {
	ctx := WithTraceID(context.Background())
	id := TraceIDFromContext(ctx)
	if id == "" {
		t.Fatal("expected non-empty trace ID")
	}
	if len(id) != 32 {
		t.Errorf("expected 32-char hex trace ID, got len %d: %s", len(id), id)
	}
}

func TestTraceIDFromContext_Empty(t *testing.T) {
	id := TraceIDFromContext(context.Background())
	if id != "" {
		t.Errorf("expected empty trace ID, got: %s", id)
	}
}

func TestWithTraceID_UniquePerCall(t *testing.T) {
	ctx1 := WithTraceID(context.Background())
	ctx2 := WithTraceID(context.Background())
	id1 := TraceIDFromContext(ctx1)
	id2 := TraceIDFromContext(ctx2)
	if id1 == id2 {
		t.Error("expected unique trace IDs")
	}
}
