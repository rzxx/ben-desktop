package observability

import (
	"context"
	"fmt"
	"strings"

	apitypes "ben/desktop/api/types"
)

type contextKey struct{}

type spanContext struct {
	traceID string
	spanID  string
	sampled bool
	remote  bool
}

func FromCarrier(ctx context.Context, carrier apitypes.TraceCarrier) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	sc, ok := parseTraceparent(carrier.Traceparent)
	if !ok {
		return ctx
	}
	return context.WithValue(ctx, contextKey{}, sc)
}

func CarrierFromContext(ctx context.Context) apitypes.TraceCarrier {
	sc, ok := contextSpan(ctx)
	if !ok {
		return apitypes.TraceCarrier{}
	}
	flags := "00"
	if sc.sampled {
		flags = "01"
	}
	return apitypes.TraceCarrier{
		Traceparent: fmt.Sprintf("00-%s-%s-%s", sc.traceID, sc.spanID, flags),
	}
}

func contextSpan(ctx context.Context) (spanContext, bool) {
	if ctx == nil {
		return spanContext{}, false
	}
	sc, ok := ctx.Value(contextKey{}).(spanContext)
	if !ok || !validTraceID(sc.traceID) || !validSpanID(sc.spanID) {
		return spanContext{}, false
	}
	return sc, true
}

func withSpanContext(ctx context.Context, sc spanContext) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, contextKey{}, sc)
}

func parseTraceparent(value string) (spanContext, bool) {
	value = strings.TrimSpace(value)
	parts := strings.Split(value, "-")
	if len(parts) != 4 {
		return spanContext{}, false
	}
	if parts[0] != "00" {
		return spanContext{}, false
	}
	traceID := strings.ToLower(strings.TrimSpace(parts[1]))
	spanID := strings.ToLower(strings.TrimSpace(parts[2]))
	flags := strings.ToLower(strings.TrimSpace(parts[3]))
	if !validTraceID(traceID) || !validSpanID(spanID) || len(flags) != 2 {
		return spanContext{}, false
	}
	sampled := false
	switch flags {
	case "00":
	case "01":
		sampled = true
	default:
		parsed, ok := parseHexByte(flags)
		if !ok {
			return spanContext{}, false
		}
		if parsed&1 == 1 {
			sampled = true
		}
	}
	return spanContext{traceID: traceID, spanID: spanID, sampled: sampled, remote: true}, true
}

func parseHexByte(value string) (byte, bool) {
	if len(value) != 2 {
		return 0, false
	}
	var out byte
	for _, r := range value {
		out <<= 4
		switch {
		case r >= '0' && r <= '9':
			out |= byte(r - '0')
		case r >= 'a' && r <= 'f':
			out |= byte(r-'a') + 10
		case r >= 'A' && r <= 'F':
			out |= byte(r-'A') + 10
		default:
			return 0, false
		}
	}
	return out, true
}
