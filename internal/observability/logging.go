package observability

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	apitypes "ben/desktop/api/types"
)

type handler struct {
	manager *Manager
	attrs   []slog.Attr
	groups  []string
}

func newHandler(manager *Manager, attrs []slog.Attr, groups []string) *handler {
	return &handler{
		manager: manager,
		attrs:   append([]slog.Attr(nil), attrs...),
		groups:  append([]string(nil), groups...),
	}
}

func (h *handler) Enabled(_ context.Context, level slog.Level) bool {
	if h == nil || h.manager == nil {
		return true
	}
	return level >= h.manager.level.Level()
}

func (h *handler) Handle(ctx context.Context, record slog.Record) error {
	if h == nil || h.manager == nil {
		return nil
	}
	attrs := make([]slog.Attr, 0, len(h.attrs)+record.NumAttrs()+4)
	attrs = append(attrs, h.attrs...)
	record.Attrs(func(attr slog.Attr) bool {
		attrs = append(attrs, attr)
		return true
	})
	fields := attrsToMap(attrs)
	service := stringField(fields, "service")
	if service == "" {
		service = stringField(fields, "ben.service")
	}
	component := stringField(fields, "component")
	if component == "" {
		component = stringField(fields, "ben.component")
	}
	sc, ok := contextSpan(ctx)
	traceID := ""
	spanID := ""
	if ok {
		traceID = sc.traceID
		spanID = sc.spanID
	}
	logRecord := apitypes.TraceRecord{
		SchemaVersion: 1,
		Signal:        "log",
		TimeUnixNano:  record.Time.UTC().UnixNano(),
		TraceID:       traceID,
		SpanID:        spanID,
		Severity:      levelName(record.Level),
		Message:       strings.TrimSpace(record.Message),
		Service:       service,
		Component:     component,
		Attrs:         fields,
	}
	if logRecord.TimeUnixNano == 0 {
		logRecord.TimeUnixNano = time.Now().UTC().UnixNano()
	}
	h.manager.writeLogRecord(ctx, logRecord)
	return nil
}

func (h *handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if h == nil {
		return newHandler(nil, attrs, nil)
	}
	next := append([]slog.Attr(nil), h.attrs...)
	next = append(next, attrs...)
	return newHandler(h.manager, next, h.groups)
}

func (h *handler) WithGroup(name string) slog.Handler {
	if h == nil {
		return newHandler(nil, nil, []string{name})
	}
	nextGroups := append([]string(nil), h.groups...)
	if strings.TrimSpace(name) != "" {
		nextGroups = append(nextGroups, name)
	}
	return newHandler(h.manager, h.attrs, nextGroups)
}

type LegacyLogger struct {
	Logger  *slog.Logger
	Service string
}

func NewLegacyLogger(logger *slog.Logger, service string) LegacyLogger {
	if logger == nil {
		logger = slog.Default()
	}
	service = strings.TrimSpace(service)
	if service == "" {
		service = "app"
	}
	return LegacyLogger{Logger: logger, Service: service}
}

func (l LegacyLogger) Printf(format string, args ...any) {
	l.Logger.Info(fmt.Sprintf(format, args...), slog.String("service", l.Service))
}

func (l LegacyLogger) Errorf(format string, args ...any) {
	l.Logger.Error(fmt.Sprintf(format, args...), slog.String("service", l.Service))
}

func LogAttrs(ctx context.Context, level slog.Level, msg string, attrs ...Attr) {
	logger := slog.Default()
	switch {
	case level >= slog.LevelError:
		logger.LogAttrs(ctx, slog.LevelError, msg, attrs...)
	case level >= slog.LevelWarn:
		logger.LogAttrs(ctx, slog.LevelWarn, msg, attrs...)
	case level <= slog.LevelDebug:
		logger.LogAttrs(ctx, slog.LevelDebug, msg, attrs...)
	default:
		logger.LogAttrs(ctx, slog.LevelInfo, msg, attrs...)
	}
}

func stringField(fields map[string]any, key string) string {
	if len(fields) == 0 {
		return ""
	}
	value, ok := fields[key]
	if !ok {
		return ""
	}
	text, _ := value.(string)
	return strings.TrimSpace(text)
}
