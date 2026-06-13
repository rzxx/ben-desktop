package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	apitypes "ben/desktop/api/types"
	"ben/desktop/internal/observability"
)

type ObservabilityFacade struct {
	manager *observability.Manager
}

func NewObservabilityFacade(manager *observability.Manager) *ObservabilityFacade {
	return &ObservabilityFacade{manager: manager}
}

func (s *ObservabilityFacade) ServiceName() string { return "ObservabilityFacade" }

func (s *ObservabilityFacade) GetStatus(ctx context.Context, carrier apitypes.TraceCarrier) apitypes.ObservabilityStatus {
	ctx = observability.FromCarrier(ctx, carrier)
	_, span := observability.Start(ctx, "wails.observability.get_status", observability.String("service", "observability"))
	defer span.End()
	return s.requireManager().Status()
}

func (s *ObservabilityFacade) SetLogLevel(ctx context.Context, carrier apitypes.TraceCarrier, level string) (apitypes.ObservabilityStatus, error) {
	ctx = observability.FromCarrier(ctx, carrier)
	_, span := observability.Start(ctx, "wails.observability.set_log_level", observability.String("service", "observability"))
	defer span.End()
	parsed, err := parseSlogLevel(level)
	if err != nil {
		span.RecordError(err)
		return apitypes.ObservabilityStatus{}, err
	}
	manager := s.requireManager()
	manager.SetLevel(parsed)
	return manager.Status(), nil
}

func (s *ObservabilityFacade) StartTraceSession(ctx context.Context, carrier apitypes.TraceCarrier, config apitypes.TraceSessionConfig) (apitypes.TraceSessionStatus, error) {
	ctx = observability.FromCarrier(ctx, carrier)
	ctx, span := observability.Start(ctx, "wails.observability.start_trace_session", observability.String("service", "observability"))
	defer span.End()
	span.SetInput(observability.Summary("start trace session", map[string]any{
		"mode":     config.Mode,
		"services": config.Services,
		"trigger":  config.Trigger,
	}))
	status, err := s.requireManager().StartSession(ctx, config)
	if err != nil {
		span.RecordError(err)
		return apitypes.TraceSessionStatus{}, err
	}
	span.SetOutput(observability.Summary("trace session started", map[string]any{
		"session_id": status.SessionID,
		"mode":       status.Mode,
	}))
	return status, nil
}

func (s *ObservabilityFacade) StopTraceSession(ctx context.Context, carrier apitypes.TraceCarrier, sessionID string) (apitypes.TraceSessionStatus, error) {
	ctx = observability.FromCarrier(ctx, carrier)
	ctx, span := observability.Start(ctx, "wails.observability.stop_trace_session", observability.String("service", "observability"))
	defer span.End()
	status, err := s.requireManager().StopSession(ctx, sessionID)
	if err != nil {
		span.RecordError(err)
		return apitypes.TraceSessionStatus{}, err
	}
	span.SetOutput(observability.Summary("trace session stopped", map[string]any{
		"session_id": status.SessionID,
		"records":    status.RecordsWritten,
		"dropped":    status.DroppedRecords,
	}))
	return status, nil
}

func (s *ObservabilityFacade) ListTraceSessions(ctx context.Context, carrier apitypes.TraceCarrier, query apitypes.TraceSessionQuery) ([]apitypes.TraceSessionSummary, error) {
	ctx = observability.FromCarrier(ctx, carrier)
	_, span := observability.Start(ctx, "wails.observability.list_trace_sessions", observability.String("service", "observability"))
	defer span.End()
	sessions, err := s.requireManager().ListSessions(query)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}
	return sessions, nil
}

func (s *ObservabilityFacade) ExportTraceSession(ctx context.Context, carrier apitypes.TraceCarrier, sessionID string, options apitypes.TraceExportOptions) (apitypes.TraceExportResult, error) {
	ctx = observability.FromCarrier(ctx, carrier)
	_, span := observability.Start(ctx, "wails.observability.export_trace_session", observability.String("service", "observability"))
	defer span.End()
	result, err := s.requireManager().ExportSession(sessionID, options)
	if err != nil {
		span.RecordError(err)
		return apitypes.TraceExportResult{}, err
	}
	span.SetOutput(observability.Summary("trace session exported", map[string]any{
		"session_id": result.SessionID,
		"bytes":      result.Bytes,
		"path":       result.Path,
	}))
	return result, nil
}

func (s *ObservabilityFacade) RecordFrontendEvents(ctx context.Context, carrier apitypes.TraceCarrier, batch apitypes.FrontendTraceBatch) error {
	ctx = observability.FromCarrier(ctx, carrier)
	_, span := observability.Start(ctx, "wails.observability.record_frontend_events", observability.String("service", "observability"))
	defer span.End()
	if err := s.requireManager().RecordFrontendBatch(ctx, batch); err != nil {
		span.RecordError(err)
		return err
	}
	return nil
}

func (s *ObservabilityFacade) GetRecentRecords(ctx context.Context, carrier apitypes.TraceCarrier, filter apitypes.RecentTraceFilter) []apitypes.TraceRecord {
	ctx = observability.FromCarrier(ctx, carrier)
	_, span := observability.Start(ctx, "wails.observability.get_recent_records", observability.String("service", "observability"))
	defer span.End()
	return s.requireManager().Recent(filter)
}

func (s *ObservabilityFacade) requireManager() *observability.Manager {
	if s != nil && s.manager != nil {
		return s.manager
	}
	return observability.Default()
}

func parseSlogLevel(level string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "", "info":
		return slog.LevelInfo, nil
	case "debug":
		return slog.LevelDebug, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf("unknown log level %q", level)
	}
}
