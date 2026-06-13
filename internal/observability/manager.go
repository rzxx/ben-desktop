package observability

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	apitypes "ben/desktop/api/types"
)

const (
	defaultMaxSessionBytes = int64(250 * 1024 * 1024)
	defaultMaxEventBytes   = 32 * 1024
	defaultMaxDurationSec  = 15 * 60
)

type Config struct {
	AppName       string
	AppVersion    string
	BuildCommit   string
	BuildTime     string
	LogLevel      slog.Level
	RecentRecords int
}

type Manager struct {
	paths Paths
	info  processInfo

	level slog.LevelVar
	logs  *rotatingJSONLWriter

	recent *recentRing

	mu      sync.Mutex
	session *traceSession
}

type processInfo struct {
	AppName     string `json:"appName"`
	AppVersion  string `json:"appVersion,omitempty"`
	BuildCommit string `json:"buildCommit,omitempty"`
	BuildTime   string `json:"buildTime,omitempty"`
	GoVersion   string `json:"goVersion"`
	OS          string `json:"os"`
	Arch        string `json:"arch"`
	PID         int    `json:"pid"`
}

type traceSession struct {
	sessionID string
	dir       string
	started   time.Time
	stopped   *time.Time
	config    apitypes.TraceSessionConfig

	spans    *jsonlWriter
	events   *jsonlWriter
	logs     *jsonlWriter
	frontend *jsonlWriter
	metrics  *jsonlWriter

	bytesWritten   atomic.Int64
	recordsWritten atomic.Int64
	droppedRecords atomic.Int64

	droppedMu       sync.Mutex
	droppedByReason map[string]int64

	stopTimer *time.Timer
}

var defaultManager atomic.Pointer[Manager]

func Initialize(config Config) (*Manager, *slog.Logger, error) {
	paths, err := DefaultPaths(config.AppName)
	if err != nil {
		return nil, nil, err
	}
	manager, err := NewManager(paths, config)
	if err != nil {
		return nil, nil, err
	}
	logger := slog.New(newHandler(manager, nil, nil))
	manager.level.Set(config.LogLevel)
	defaultManager.Store(manager)
	slog.SetDefault(logger)
	return manager, logger, nil
}

func Default() *Manager {
	if manager := defaultManager.Load(); manager != nil {
		return manager
	}
	paths, err := DefaultPaths("ben-desktop")
	if err != nil {
		return nil
	}
	manager, err := NewManager(paths, Config{AppName: "ben-desktop"})
	if err != nil {
		return nil
	}
	defaultManager.Store(manager)
	return manager
}

func NewManager(paths Paths, config Config) (*Manager, error) {
	if strings.TrimSpace(config.AppName) == "" {
		config.AppName = "ben-desktop"
	}
	logs, err := newRotatingJSONLWriter(paths.Logs, "app", 10*1024*1024, 10)
	if err != nil {
		return nil, err
	}
	manager := &Manager{
		paths:  paths,
		logs:   logs,
		recent: newRecentRing(config.RecentRecords),
		info: processInfo{
			AppName:     config.AppName,
			AppVersion:  strings.TrimSpace(config.AppVersion),
			BuildCommit: strings.TrimSpace(config.BuildCommit),
			BuildTime:   strings.TrimSpace(config.BuildTime),
			GoVersion:   runtime.Version(),
			OS:          runtime.GOOS,
			Arch:        runtime.GOARCH,
			PID:         os.Getpid(),
		},
	}
	manager.level.Set(config.LogLevel)
	return manager, nil
}

func (m *Manager) Logger() *slog.Logger {
	if m == nil {
		return slog.Default()
	}
	return slog.New(newHandler(m, nil, nil))
}

func (m *Manager) Level() slog.Level {
	if m == nil {
		return slog.LevelInfo
	}
	return m.level.Level()
}

func (m *Manager) SetLevel(level slog.Level) {
	if m == nil {
		return
	}
	m.level.Set(level)
}

func (m *Manager) Status() apitypes.ObservabilityStatus {
	if m == nil {
		return apitypes.ObservabilityStatus{}
	}
	return apitypes.ObservabilityStatus{
		LogLevel:      levelName(m.Level()),
		LogDirectory:  m.paths.Logs,
		TraceRoot:     m.paths.Traces,
		SupportRoot:   m.paths.Support,
		TraceSession:  m.SessionStatus(),
		RecentRecords: m.recent.Len(),
	}
}

func (m *Manager) StartSession(ctx context.Context, config apitypes.TraceSessionConfig) (apitypes.TraceSessionStatus, error) {
	if m == nil {
		return apitypes.TraceSessionStatus{}, errors.New("observability manager is unavailable")
	}
	config = normalizeSessionConfig(config)
	now := time.Now().UTC()
	sessionID := now.Format("20060102-150405") + "-" + newSpanID()
	dir := filepath.Join(m.paths.Traces, sessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return apitypes.TraceSessionStatus{}, err
	}

	session, err := openTraceSession(dir, sessionID, now, config)
	if err != nil {
		return apitypes.TraceSessionStatus{}, err
	}

	m.mu.Lock()
	if m.session != nil && m.session.stopped == nil {
		m.mu.Unlock()
		_ = session.close()
		return apitypes.TraceSessionStatus{}, errors.New("trace session already active")
	}
	m.session = session
	m.mu.Unlock()

	if config.MaxDurationSec > 0 {
		session.stopTimer = time.AfterFunc(time.Duration(config.MaxDurationSec)*time.Second, func() {
			_, _ = m.StopSession(context.Background(), sessionID)
		})
	}

	m.writeManifest(session, nil)
	m.Event(ctx, "observability.session.started",
		String("service", "observability"),
		String("session_id", sessionID),
		String("mode", config.Mode),
	)
	return m.SessionStatus(), nil
}

func (m *Manager) StopSession(ctx context.Context, sessionID string) (apitypes.TraceSessionStatus, error) {
	if m == nil {
		return apitypes.TraceSessionStatus{}, errors.New("observability manager is unavailable")
	}
	m.mu.Lock()
	session := m.session
	if session == nil || session.stopped != nil {
		status := m.sessionStatusLocked()
		m.mu.Unlock()
		return status, nil
	}
	if sessionID = strings.TrimSpace(sessionID); sessionID != "" && sessionID != session.sessionID {
		m.mu.Unlock()
		return apitypes.TraceSessionStatus{}, fmt.Errorf("trace session %s is not active", sessionID)
	}
	now := time.Now().UTC()
	session.stopped = &now
	if session.stopTimer != nil {
		session.stopTimer.Stop()
	}
	status := m.sessionStatusLocked()
	m.mu.Unlock()

	m.Event(ctx, "observability.session.stopped",
		String("service", "observability"),
		String("session_id", session.sessionID),
	)
	closeErr := session.close()
	m.writeManifest(session, session.stopped)
	m.writeSummary(session)
	return status, closeErr
}

func (m *Manager) SessionStatus() apitypes.TraceSessionStatus {
	if m == nil {
		return apitypes.TraceSessionStatus{}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessionStatusLocked()
}

func (m *Manager) sessionStatusLocked() apitypes.TraceSessionStatus {
	if m.session == nil {
		return apitypes.TraceSessionStatus{Config: normalizeSessionConfig(apitypes.TraceSessionConfig{})}
	}
	session := m.session
	started := session.started
	status := apitypes.TraceSessionStatus{
		Active:          session.stopped == nil,
		SessionID:       session.sessionID,
		Mode:            session.config.Mode,
		StartedAt:       &started,
		StoppedAt:       session.stopped,
		Directory:       session.dir,
		Config:          session.config,
		BytesWritten:    session.bytesWritten.Load(),
		RecordsWritten:  session.recordsWritten.Load(),
		DroppedRecords:  session.droppedRecords.Load(),
		DroppedByReason: session.droppedSnapshot(),
	}
	return status
}

func (m *Manager) ListSessions(query apitypes.TraceSessionQuery) ([]apitypes.TraceSessionSummary, error) {
	if m == nil {
		return nil, errors.New("observability manager is unavailable")
	}
	entries, err := os.ReadDir(m.paths.Traces)
	if err != nil {
		return nil, err
	}
	out := make([]apitypes.TraceSessionSummary, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		summary, err := readSessionManifest(filepath.Join(m.paths.Traces, entry.Name()))
		if err != nil {
			continue
		}
		out = append(out, summary)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].StartedAt.After(out[j].StartedAt)
	})
	if query.Limit > 0 && len(out) > query.Limit {
		out = out[:query.Limit]
	}
	return out, nil
}

func (m *Manager) ExportSession(sessionID string, options apitypes.TraceExportOptions) (apitypes.TraceExportResult, error) {
	if m == nil {
		return apitypes.TraceExportResult{}, errors.New("observability manager is unavailable")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		m.mu.Lock()
		if m.session != nil {
			sessionID = m.session.sessionID
		}
		m.mu.Unlock()
	}
	if sessionID == "" {
		return apitypes.TraceExportResult{}, errors.New("session id is required")
	}
	sessionDir := filepath.Join(m.paths.Traces, filepath.Base(sessionID))
	if _, err := os.Stat(sessionDir); err != nil {
		return apitypes.TraceExportResult{}, err
	}
	outPath := filepath.Join(m.paths.Support, sessionID+".zip")
	bytes, err := zipDirectory(sessionDir, outPath, options)
	if err != nil {
		return apitypes.TraceExportResult{}, err
	}
	return apitypes.TraceExportResult{SessionID: sessionID, Path: outPath, Bytes: bytes}, nil
}

func (m *Manager) Recent(filter apitypes.RecentTraceFilter) []apitypes.TraceRecord {
	if m == nil {
		return nil
	}
	if filter.Limit <= 0 {
		filter.Limit = 200
	}
	return m.recent.Snapshot(filter)
}

func (m *Manager) RecordFrontendBatch(ctx context.Context, batch apitypes.FrontendTraceBatch) error {
	if m == nil || len(batch.Records) == 0 {
		return nil
	}
	for _, record := range batch.Records {
		record.SchemaVersion = 1
		if record.TimeUnixNano == 0 {
			record.TimeUnixNano = time.Now().UTC().UnixNano()
		}
		if record.Component == "" {
			record.Component = "frontend"
		}
		if record.Signal == "" {
			record.Signal = "event"
		}
		record.Attrs = sanitizeFields(record.Attrs)
		m.writeRecord(ctx, "frontend", record)
	}
	return nil
}

func (m *Manager) writeRecord(ctx context.Context, stream string, record apitypes.TraceRecord) {
	if m == nil {
		return
	}
	if record.SchemaVersion == 0 {
		record.SchemaVersion = 1
	}
	if record.TimeUnixNano == 0 && record.StartUnixNano == 0 {
		record.TimeUnixNano = time.Now().UTC().UnixNano()
	}
	if sc, ok := contextSpan(ctx); ok {
		if record.TraceID == "" {
			record.TraceID = sc.traceID
		}
		if record.SpanID == "" {
			record.SpanID = sc.spanID
		}
	}
	record.Attrs = sanitizeFields(record.Attrs)
	m.recent.Add(record)

	m.mu.Lock()
	session := m.session
	active := session != nil && session.stopped == nil
	m.mu.Unlock()
	if !active {
		return
	}
	if !session.serviceEnabled(record.Service) {
		session.drop("service_filtered")
		return
	}
	if !session.canWrite(record) {
		session.drop("limit")
		return
	}
	var writer *jsonlWriter
	switch stream {
	case "spans":
		writer = session.spans
	case "frontend":
		writer = session.frontend
	case "logs":
		if !session.config.IncludeLogs {
			return
		}
		writer = session.logs
	case "metrics":
		writer = session.metrics
	default:
		writer = session.events
	}
	n, err := writer.WriteJSON(record)
	if err != nil {
		session.drop("write_failed")
		return
	}
	session.bytesWritten.Add(int64(n))
	session.recordsWritten.Add(1)
}

func (m *Manager) writeLogRecord(ctx context.Context, record apitypes.TraceRecord) {
	if m == nil {
		return
	}
	if record.SchemaVersion == 0 {
		record.SchemaVersion = 1
	}
	if record.Signal == "" {
		record.Signal = "log"
	}
	if record.TimeUnixNano == 0 {
		record.TimeUnixNano = time.Now().UTC().UnixNano()
	}
	record.Attrs = sanitizeFields(record.Attrs)
	if _, err := m.logs.WriteJSON(record); err != nil {
		return
	}
	m.writeRecord(ctx, "logs", record)
}

func (m *Manager) Event(ctx context.Context, name string, attrs ...Attr) {
	if m == nil {
		return
	}
	record := apitypes.TraceRecord{
		SchemaVersion: 1,
		Signal:        "event",
		TimeUnixNano:  time.Now().UTC().UnixNano(),
		Name:          strings.TrimSpace(name),
		Service:       attrString(attrs, "service"),
		Component:     attrString(attrs, "component"),
		Attrs:         attrsToMap(attrs),
	}
	m.writeRecord(ctx, "events", record)
}

func normalizeSessionConfig(config apitypes.TraceSessionConfig) apitypes.TraceSessionConfig {
	config.Mode = strings.ToLower(strings.TrimSpace(config.Mode))
	if config.Mode == "" {
		config.Mode = "support"
	}
	config.RedactionLevel = strings.ToLower(strings.TrimSpace(config.RedactionLevel))
	if config.RedactionLevel == "" {
		config.RedactionLevel = "safe"
	}
	if config.MaxDurationSec <= 0 {
		config.MaxDurationSec = defaultMaxDurationSec
	}
	if config.MaxBytes <= 0 {
		config.MaxBytes = defaultMaxSessionBytes
	}
	if config.MaxEventBytes <= 0 {
		config.MaxEventBytes = defaultMaxEventBytes
	}
	if config.Mode == "performance" && !config.IncludeRuntime {
		config.IncludeRuntime = true
	}
	config.IncludeLogs = true
	return config
}

func openTraceSession(dir, sessionID string, started time.Time, config apitypes.TraceSessionConfig) (*traceSession, error) {
	open := func(name string) (*jsonlWriter, error) {
		return newJSONLWriter(filepath.Join(dir, name), config.MaxBytes)
	}
	spans, err := open("spans.jsonl")
	if err != nil {
		return nil, err
	}
	events, err := open("events.jsonl")
	if err != nil {
		_ = spans.Close()
		return nil, err
	}
	logs, err := open("logs.jsonl")
	if err != nil {
		_ = spans.Close()
		_ = events.Close()
		return nil, err
	}
	frontend, err := open("frontend.jsonl")
	if err != nil {
		_ = spans.Close()
		_ = events.Close()
		_ = logs.Close()
		return nil, err
	}
	metrics, err := open("metrics.jsonl")
	if err != nil {
		_ = spans.Close()
		_ = events.Close()
		_ = logs.Close()
		_ = frontend.Close()
		return nil, err
	}
	return &traceSession{
		sessionID:       sessionID,
		dir:             dir,
		started:         started,
		config:          config,
		spans:           spans,
		events:          events,
		logs:            logs,
		frontend:        frontend,
		metrics:         metrics,
		droppedByReason: make(map[string]int64),
	}, nil
}

func (s *traceSession) close() error {
	if s == nil {
		return nil
	}
	var closeErr error
	for _, writer := range []*jsonlWriter{s.spans, s.events, s.logs, s.frontend, s.metrics} {
		if err := writer.Close(); err != nil && closeErr == nil {
			closeErr = err
		}
	}
	payload, _ := json.MarshalIndent(s.droppedSnapshot(), "", "  ")
	_ = os.WriteFile(filepath.Join(s.dir, "dropped.json"), append(payload, '\n'), 0o644)
	return closeErr
}

func (s *traceSession) serviceEnabled(service string) bool {
	if s == nil || len(s.config.Services) == 0 {
		return true
	}
	service = strings.TrimSpace(service)
	if service == "" {
		return true
	}
	for _, allowed := range s.config.Services {
		if strings.EqualFold(strings.TrimSpace(allowed), service) {
			return true
		}
	}
	return false
}

func (s *traceSession) canWrite(record apitypes.TraceRecord) bool {
	if s == nil {
		return false
	}
	payload, err := json.Marshal(record)
	if err != nil {
		return false
	}
	if s.config.MaxEventBytes > 0 && len(payload) > s.config.MaxEventBytes {
		return false
	}
	if s.config.MaxBytes > 0 && s.bytesWritten.Load()+int64(len(payload)) > s.config.MaxBytes {
		return false
	}
	return true
}

func (s *traceSession) drop(reason string) {
	if s == nil {
		return
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "unknown"
	}
	s.droppedRecords.Add(1)
	s.droppedMu.Lock()
	s.droppedByReason[reason]++
	s.droppedMu.Unlock()
}

func (s *traceSession) droppedSnapshot() map[string]int64 {
	if s == nil {
		return nil
	}
	s.droppedMu.Lock()
	defer s.droppedMu.Unlock()
	out := make(map[string]int64, len(s.droppedByReason))
	for key, value := range s.droppedByReason {
		out[key] = value
	}
	return out
}

func (m *Manager) writeManifest(session *traceSession, stopped *time.Time) {
	if m == nil || session == nil {
		return
	}
	manifest := map[string]any{
		"schemaVersion":  1,
		"sessionId":      session.sessionID,
		"startedAt":      session.started,
		"stoppedAt":      stopped,
		"config":         session.config,
		"process":        m.info,
		"bytesWritten":   session.bytesWritten.Load(),
		"recordsWritten": session.recordsWritten.Load(),
		"droppedRecords": session.droppedRecords.Load(),
		"droppedReasons": session.droppedSnapshot(),
	}
	payload, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(session.dir, "manifest.json"), append(payload, '\n'), 0o644)
}

func (m *Manager) writeSummary(session *traceSession) {
	if session == nil {
		return
	}
	text := fmt.Sprintf(`# Trace Session %s

Mode: %s
Started: %s
Stopped: %s
Records: %d
Dropped: %d
Bytes: %d
`,
		session.sessionID,
		session.config.Mode,
		session.started.Format(time.RFC3339Nano),
		timeOrEmpty(session.stopped),
		session.recordsWritten.Load(),
		session.droppedRecords.Load(),
		session.bytesWritten.Load(),
	)
	_ = os.WriteFile(filepath.Join(session.dir, "summary.md"), []byte(text), 0o644)
	aiSummary := map[string]any{
		"schemaVersion":  1,
		"sessionId":      session.sessionID,
		"mode":           session.config.Mode,
		"recordsWritten": session.recordsWritten.Load(),
		"droppedRecords": session.droppedRecords.Load(),
		"droppedReasons": session.droppedSnapshot(),
		"files":          []string{"manifest.json", "spans.jsonl", "events.jsonl", "logs.jsonl", "frontend.jsonl", "metrics.jsonl", "summary.md"},
	}
	payload, err := json.MarshalIndent(aiSummary, "", "  ")
	if err == nil {
		_ = os.WriteFile(filepath.Join(session.dir, "ai-summary.json"), append(payload, '\n'), 0o644)
	}
}

func readSessionManifest(dir string) (apitypes.TraceSessionSummary, error) {
	payload, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return apitypes.TraceSessionSummary{}, err
	}
	var raw struct {
		SessionID      string                      `json:"sessionId"`
		StartedAt      time.Time                   `json:"startedAt"`
		StoppedAt      *time.Time                  `json:"stoppedAt"`
		Config         apitypes.TraceSessionConfig `json:"config"`
		BytesWritten   int64                       `json:"bytesWritten"`
		RecordsWritten int64                       `json:"recordsWritten"`
		DroppedRecords int64                       `json:"droppedRecords"`
	}
	if err := json.Unmarshal(payload, &raw); err != nil {
		return apitypes.TraceSessionSummary{}, err
	}
	return apitypes.TraceSessionSummary{
		SessionID:      raw.SessionID,
		Mode:           raw.Config.Mode,
		StartedAt:      raw.StartedAt,
		StoppedAt:      raw.StoppedAt,
		Directory:      dir,
		Config:         raw.Config,
		BytesWritten:   raw.BytesWritten,
		RecordsWritten: raw.RecordsWritten,
		DroppedRecords: raw.DroppedRecords,
	}, nil
}

func zipDirectory(sourceDir, outPath string, options apitypes.TraceExportOptions) (int64, error) {
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return 0, err
	}
	out, err := os.Create(outPath)
	if err != nil {
		return 0, err
	}
	zipWriter := zip.NewWriter(out)
	walkErr := filepath.WalkDir(sourceDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if !options.IncludeLogs && filepath.Base(path) == "logs.jsonl" {
			return nil
		}
		rel, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}
		writer, err := zipWriter.Create(filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(writer, in)
		closeErr := in.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
	closeErr := zipWriter.Close()
	fileCloseErr := out.Close()
	if walkErr != nil {
		return 0, walkErr
	}
	if closeErr != nil {
		return 0, closeErr
	}
	if fileCloseErr != nil {
		return 0, fileCloseErr
	}
	info, err := os.Stat(outPath)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

func attrString(attrs []Attr, key string) string {
	for _, attr := range attrs {
		if attr.Key == key {
			return attr.Value.Resolve().String()
		}
	}
	return ""
}

func levelName(level slog.Level) string {
	switch {
	case level <= slog.LevelDebug:
		return "DEBUG"
	case level <= slog.LevelInfo:
		return "INFO"
	case level <= slog.LevelWarn:
		return "WARN"
	default:
		return "ERROR"
	}
}

func timeOrEmpty(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.Format(time.RFC3339Nano)
}
