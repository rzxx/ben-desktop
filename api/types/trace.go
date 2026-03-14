package apitypes

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"time"
)

type requestTraceKey struct{}

var requestTraceSeq atomic.Uint64

func WithTraceID(ctx context.Context, traceID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	traceID = strings.TrimSpace(traceID)
	if traceID == "" {
		return ctx
	}
	return context.WithValue(ctx, requestTraceKey{}, traceID)
}

func TraceID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	traceID, _ := ctx.Value(requestTraceKey{}).(string)
	return strings.TrimSpace(traceID)
}

func EnsureTraceID(ctx context.Context) (context.Context, string) {
	if ctx == nil {
		ctx = context.Background()
	}
	if traceID := TraceID(ctx); traceID != "" {
		return ctx, traceID
	}
	traceID := NewTraceID()
	return WithTraceID(ctx, traceID), traceID
}

func NewTraceID() string {
	seq := requestTraceSeq.Add(1)
	return fmt.Sprintf("trace-%x-%x", time.Now().UTC().UnixNano(), seq)
}
