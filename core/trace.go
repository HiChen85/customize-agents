package core

import (
	"context"
	"crypto/rand"
	"fmt"
)

type contextKey string

const traceIDKey contextKey = "trace_id"

func WithTraceID(ctx context.Context) context.Context {
	return context.WithValue(ctx, traceIDKey, generateTraceID())
}

func TraceIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(traceIDKey).(string); ok {
		return id
	}
	return ""
}

func generateTraceID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}
