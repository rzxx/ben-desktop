package observability

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"

	apitypes "ben/desktop/api/types"
)

type Span interface {
	End()
	Event(name string, attrs ...Attr)
	SetInput(summary apitypes.TraceSummary)
	SetOutput(summary apitypes.TraceSummary)
	RecordError(err error, attrs ...Attr)
	Carrier() apitypes.TraceCarrier
}

type activeSpan struct {
	manager *Manager

	mu     sync.Mutex
	ended  bool
	record apitypes.TraceRecord
}

type noopSpan struct{}

func Start(ctx context.Context, name string, attrs ...Attr) (context.Context, Span) {
	manager := Default()
	if manager == nil {
		if ctx == nil {
			ctx = context.Background()
		}
		return ctx, noopSpan{}
	}
	return manager.Start(ctx, name, attrs...)
}

func (m *Manager) Start(ctx context.Context, name string, attrs ...Attr) (context.Context, Span) {
	if ctx == nil {
		ctx = context.Background()
	}
	if m == nil || !m.tracingActive() {
		return ctx, noopSpan{}
	}
	parent, hasParent := contextSpan(ctx)
	traceID := parent.traceID
	parentSpanID := ""
	if hasParent {
		parentSpanID = parent.spanID
	}
	if traceID == "" {
		traceID = newTraceID()
	}
	spanID := newSpanID()
	sc := spanContext{
		traceID: traceID,
		spanID:  spanID,
		sampled: true,
	}
	fields := attrsToMap(attrs)
	service := stringField(fields, "service")
	if service == "" {
		service = stringField(fields, "ben.service")
	}
	component := stringField(fields, "component")
	if component == "" {
		component = stringField(fields, "ben.component")
	}
	record := apitypes.TraceRecord{
		SchemaVersion: 1,
		Signal:        "span",
		TraceID:       traceID,
		SpanID:        spanID,
		ParentSpanID:  parentSpanID,
		Name:          strings.TrimSpace(name),
		Service:       service,
		Component:     component,
		Kind:          "internal",
		StartUnixNano: time.Now().UTC().UnixNano(),
		Status:        "ok",
		Attrs:         fields,
	}
	span := &activeSpan{manager: m, record: record}
	return withSpanContext(ctx, sc), span
}

func (m *Manager) tracingActive() bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.session != nil && m.session.stopped == nil
}

func (s *activeSpan) End() {
	if s == nil || s.manager == nil {
		return
	}
	s.mu.Lock()
	if s.ended {
		s.mu.Unlock()
		return
	}
	s.ended = true
	record := s.record
	end := time.Now().UTC()
	record.EndUnixNano = end.UnixNano()
	if record.StartUnixNano > 0 {
		record.DurationMS = float64(record.EndUnixNano-record.StartUnixNano) / float64(time.Millisecond)
	}
	s.mu.Unlock()
	s.manager.writeRecord(withSpanContext(context.Background(), spanContext{
		traceID: record.TraceID,
		spanID:  record.SpanID,
		sampled: true,
	}), "spans", record)
}

func (s *activeSpan) Event(name string, attrs ...Attr) {
	if s == nil || s.manager == nil {
		return
	}
	s.mu.Lock()
	if s.ended {
		s.mu.Unlock()
		return
	}
	record := s.record
	s.mu.Unlock()
	event := apitypes.TraceRecord{
		SchemaVersion: 1,
		Signal:        "event",
		TimeUnixNano:  time.Now().UTC().UnixNano(),
		TraceID:       record.TraceID,
		SpanID:        record.SpanID,
		Name:          strings.TrimSpace(name),
		Service:       record.Service,
		Component:     record.Component,
		Attrs:         attrsToMap(attrs),
	}
	s.manager.writeRecord(context.Background(), "events", event)
}

func (s *activeSpan) SetInput(summary apitypes.TraceSummary) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ended {
		return
	}
	s.record.Input = &summary
}

func (s *activeSpan) SetOutput(summary apitypes.TraceSummary) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ended {
		return
	}
	s.record.Output = &summary
}

func (s *activeSpan) RecordError(err error, attrs ...Attr) {
	if s == nil || err == nil {
		return
	}
	fields := attrsToMap(attrs)
	s.mu.Lock()
	if s.ended {
		s.mu.Unlock()
		return
	}
	s.record.Status = "error"
	s.record.Error = &apitypes.TraceError{
		Type:    reflect.TypeOf(err).String(),
		Message: sanitizeString("error", err.Error()),
		Attrs:   fields,
	}
	s.mu.Unlock()
	s.Event("error", append(attrs, String("error", err.Error()))...)
}

func (s *activeSpan) Carrier() apitypes.TraceCarrier {
	if s == nil {
		return apitypes.TraceCarrier{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return apitypes.TraceCarrier{
		Traceparent: fmt.Sprintf("00-%s-%s-01", s.record.TraceID, s.record.SpanID),
	}
}

func (noopSpan) End() {}

func (noopSpan) Event(string, ...Attr) {}

func (noopSpan) SetInput(apitypes.TraceSummary) {}

func (noopSpan) SetOutput(apitypes.TraceSummary) {}

func (noopSpan) RecordError(error, ...Attr) {}

func (noopSpan) Carrier() apitypes.TraceCarrier {
	return apitypes.TraceCarrier{}
}
