package apitypes

import "time"

type TraceCarrier struct {
	Traceparent string            `json:"traceparent,omitempty"`
	Tracestate  string            `json:"tracestate,omitempty"`
	Baggage     map[string]string `json:"baggage,omitempty"`
}

type TraceSummary struct {
	Summary  string         `json:"summary,omitempty"`
	Fields   map[string]any `json:"fields,omitempty"`
	Redacted bool           `json:"redacted,omitempty"`
	Dropped  int            `json:"dropped,omitempty"`
}

type TraceLink struct {
	TraceID string         `json:"traceId,omitempty"`
	SpanID  string         `json:"spanId,omitempty"`
	Attrs   map[string]any `json:"attrs,omitempty"`
}

type TraceRecord struct {
	SchemaVersion int            `json:"schemaVersion"`
	Signal        string         `json:"signal"`
	TimeUnixNano  int64          `json:"timeUnixNano,omitempty"`
	TraceID       string         `json:"traceId,omitempty"`
	SpanID        string         `json:"spanId,omitempty"`
	ParentSpanID  string         `json:"parentSpanId,omitempty"`
	Name          string         `json:"name,omitempty"`
	Service       string         `json:"service,omitempty"`
	Component     string         `json:"component,omitempty"`
	Kind          string         `json:"kind,omitempty"`
	Severity      string         `json:"severity,omitempty"`
	Message       string         `json:"message,omitempty"`
	StartUnixNano int64          `json:"startUnixNano,omitempty"`
	EndUnixNano   int64          `json:"endUnixNano,omitempty"`
	DurationMS    float64        `json:"durationMs,omitempty"`
	Status        string         `json:"status,omitempty"`
	Attrs         map[string]any `json:"attrs,omitempty"`
	Input         *TraceSummary  `json:"input,omitempty"`
	Output        *TraceSummary  `json:"output,omitempty"`
	Links         []TraceLink    `json:"links,omitempty"`
	Error         *TraceError    `json:"error,omitempty"`
}

type TraceError struct {
	Type    string         `json:"type,omitempty"`
	Message string         `json:"message,omitempty"`
	Attrs   map[string]any `json:"attrs,omitempty"`
}

type TraceSessionConfig struct {
	Mode            string   `json:"mode,omitempty"`
	Services        []string `json:"services,omitempty"`
	IncludeFrontend bool     `json:"includeFrontend"`
	IncludeRuntime  bool     `json:"includeRuntime"`
	IncludeProfiles bool     `json:"includeProfiles"`
	IncludeLogs     bool     `json:"includeLogs"`
	RedactionLevel  string   `json:"redactionLevel,omitempty"`
	MaxDurationSec  int      `json:"maxDurationSec,omitempty"`
	MaxBytes        int64    `json:"maxBytes,omitempty"`
	MaxEventBytes   int      `json:"maxEventBytes,omitempty"`
	Trigger         string   `json:"trigger,omitempty"`
}

type TraceSessionStatus struct {
	Active          bool               `json:"active"`
	SessionID       string             `json:"sessionId,omitempty"`
	Mode            string             `json:"mode,omitempty"`
	StartedAt       *time.Time         `json:"startedAt,omitempty"`
	StoppedAt       *time.Time         `json:"stoppedAt,omitempty"`
	Directory       string             `json:"directory,omitempty"`
	Config          TraceSessionConfig `json:"config"`
	BytesWritten    int64              `json:"bytesWritten,omitempty"`
	RecordsWritten  int64              `json:"recordsWritten,omitempty"`
	DroppedRecords  int64              `json:"droppedRecords,omitempty"`
	DroppedByReason map[string]int64   `json:"droppedByReason,omitempty"`
}

type TraceSessionSummary struct {
	SessionID      string             `json:"sessionId"`
	Mode           string             `json:"mode,omitempty"`
	StartedAt      time.Time          `json:"startedAt"`
	StoppedAt      *time.Time         `json:"stoppedAt,omitempty"`
	Directory      string             `json:"directory,omitempty"`
	Config         TraceSessionConfig `json:"config"`
	BytesWritten   int64              `json:"bytesWritten,omitempty"`
	RecordsWritten int64              `json:"recordsWritten,omitempty"`
	DroppedRecords int64              `json:"droppedRecords,omitempty"`
}

type TraceSessionQuery struct {
	Limit int `json:"limit,omitempty"`
}

type TraceExportOptions struct {
	IncludeLogs bool `json:"includeLogs"`
}

type TraceExportResult struct {
	SessionID string `json:"sessionId"`
	Path      string `json:"path"`
	Bytes     int64  `json:"bytes"`
}

type RecentTraceFilter struct {
	Signal  string `json:"signal,omitempty"`
	Service string `json:"service,omitempty"`
	Limit   int    `json:"limit,omitempty"`
}

type FrontendTraceBatch struct {
	Records []TraceRecord `json:"records,omitempty"`
}

type ObservabilityStatus struct {
	LogLevel      string             `json:"logLevel"`
	LogDirectory  string             `json:"logDirectory"`
	TraceRoot     string             `json:"traceRoot"`
	SupportRoot   string             `json:"supportRoot"`
	TraceSession  TraceSessionStatus `json:"traceSession"`
	RecentRecords int                `json:"recentRecords"`
}

type SupportBundleOptions struct {
	IncludeRecentLogs bool   `json:"includeRecentLogs"`
	SessionID         string `json:"sessionId,omitempty"`
}
