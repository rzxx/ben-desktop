package observability

import (
	"log/slog"
	"time"

	apitypes "ben/desktop/api/types"
)

type Attr = slog.Attr

func String(key, value string) Attr {
	return slog.String(key, value)
}

func Int(key string, value int) Attr {
	return slog.Int(key, value)
}

func Int64(key string, value int64) Attr {
	return slog.Int64(key, value)
}

func Bool(key string, value bool) Attr {
	return slog.Bool(key, value)
}

func Float64(key string, value float64) Attr {
	return slog.Float64(key, value)
}

func Duration(key string, value time.Duration) Attr {
	return slog.Duration(key, value)
}

func Any(key string, value any) Attr {
	return slog.Any(key, value)
}

func Summary(text string, fields map[string]any) apitypes.TraceSummary {
	return apitypes.TraceSummary{
		Summary: text,
		Fields:  sanitizeFields(fields),
	}
}

func RedactedSummary(text string, fields map[string]any) apitypes.TraceSummary {
	return apitypes.TraceSummary{
		Summary:  text,
		Fields:   sanitizeFields(fields),
		Redacted: true,
	}
}

func attrsToMap(attrs []Attr) map[string]any {
	if len(attrs) == 0 {
		return nil
	}
	out := make(map[string]any, len(attrs))
	for _, attr := range attrs {
		appendAttr(out, attr)
	}
	return sanitizeFields(out)
}

func appendAttr(out map[string]any, attr Attr) {
	if attr.Key == "" {
		return
	}
	value := attr.Value.Resolve()
	switch value.Kind() {
	case slog.KindString:
		out[attr.Key] = value.String()
	case slog.KindBool:
		out[attr.Key] = value.Bool()
	case slog.KindInt64:
		out[attr.Key] = value.Int64()
	case slog.KindUint64:
		out[attr.Key] = value.Uint64()
	case slog.KindFloat64:
		out[attr.Key] = value.Float64()
	case slog.KindDuration:
		out[attr.Key] = value.Duration().String()
	case slog.KindTime:
		out[attr.Key] = value.Time().UTC().Format(time.RFC3339Nano)
	case slog.KindGroup:
		group := make(map[string]any)
		for _, item := range value.Group() {
			appendAttr(group, item)
		}
		out[attr.Key] = group
	default:
		out[attr.Key] = value.Any()
	}
}
