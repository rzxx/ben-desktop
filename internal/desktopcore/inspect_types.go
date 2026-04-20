package desktopcore

import "time"

const inspectSchemaVersion = 1

type InspectConfig struct {
	DBPath           string
	BlobRoot         string
	SettingsAppName  string
	FFmpegPath       string
	PreferredProfile string
}

type InspectContext struct {
	LibraryID        string `json:"library_id"`
	DeviceID         string `json:"device_id"`
	PreferredProfile string `json:"preferred_profile"`
	NetworkRunning   bool   `json:"network_running"`
}

type InspectLibraryCandidate struct {
	LibraryID string `json:"library_id"`
	Name      string `json:"name"`
}

type InspectDeviceCandidate struct {
	DeviceID   string     `json:"device_id"`
	Name       string     `json:"name"`
	PeerID     string     `json:"peer_id,omitempty"`
	Role       string     `json:"role,omitempty"`
	LastSeenAt *time.Time `json:"last_seen_at,omitempty"`
}

type ContextResolution struct {
	Selected           InspectContext            `json:"selected"`
	AvailableLibraries []InspectLibraryCandidate `json:"available_libraries,omitempty"`
	AvailableDevices   []InspectDeviceCandidate  `json:"available_devices,omitempty"`
	InferenceSource    string                    `json:"inference_source,omitempty"`
	Ambiguous          bool                      `json:"ambiguous"`
}

type ResolveInspectContextRequest struct {
	LibraryID        string `json:"library_id,omitempty"`
	DeviceID         string `json:"device_id,omitempty"`
	PreferredProfile string `json:"preferred_profile,omitempty"`
	NetworkRunning   *bool  `json:"network_running,omitempty"`
}

type TraceRecordingRequest struct {
	ID string `json:"id"`
	ResolveInspectContextRequest
}

type TraceAlbumRequest struct {
	ID string `json:"id"`
	ResolveInspectContextRequest
}

type TracePlaybackContextRequest struct {
	Kind string `json:"kind"`
	ID   string `json:"id,omitempty"`
	ResolveInspectContextRequest
}

type TraceRecordingCacheRequest struct {
	ID string `json:"id"`
	ResolveInspectContextRequest
}

type TraceBlobRequest struct {
	BlobID string `json:"blob_id"`
	ResolveInspectContextRequest
}

type HealthCheckRequest struct {
	Date              string `json:"date,omitempty"`
	Limit             int    `json:"limit,omitempty"`
	Decode            bool   `json:"decode"`
	IncludeFilesystem bool   `json:"include_filesystem"`
	ResolveInspectContextRequest
}

type SourceFileHealthItem struct {
	SourceFileID           string   `json:"source_file_id"`
	TrackVariantID         string   `json:"track_variant_id"`
	Title                  string   `json:"title,omitempty"`
	LocalPath              string   `json:"local_path"`
	IndexedAt              string   `json:"indexed_at,omitempty"`
	Exists                 bool     `json:"exists"`
	DBSizeBytes            int64    `json:"db_size_bytes"`
	ActualSizeBytes        *int64   `json:"actual_size_bytes,omitempty"`
	DBDurationMS           int64    `json:"db_duration_ms"`
	ProbeDurationSeconds   *float64 `json:"probe_duration_seconds,omitempty"`
	DecodedDurationSeconds *float64 `json:"decoded_duration_seconds,omitempty"`
	DurationDeltaSeconds   *float64 `json:"duration_delta_seconds,omitempty"`
	ProbeError             string   `json:"probe_error,omitempty"`
	DecodeError            string   `json:"decode_error,omitempty"`
	Status                 string   `json:"status"`
	Problems               []string `json:"problems,omitempty"`
}

type HealthCheckSummary struct {
	CandidateCount     int    `json:"candidate_count"`
	CheckedCount       int    `json:"checked_count"`
	ProblemCount       int    `json:"problem_count"`
	OKCount            int    `json:"ok_count"`
	MissingFromDBCount int    `json:"missing_from_db_count"`
	DecodeEnabled      bool   `json:"decode_enabled"`
	FilesystemCompared bool   `json:"filesystem_compared"`
	DateFilter         string `json:"date_filter,omitempty"`
	Limit              int    `json:"limit,omitempty"`
}

type HealthCheckReport struct {
	SchemaVersion int                    `json:"schema_version"`
	Request       any                    `json:"request"`
	Context       ContextResolution      `json:"context"`
	Summary       HealthCheckSummary     `json:"summary"`
	Problems      []SourceFileHealthItem `json:"problems"`
	MissingFromDB []string               `json:"missing_from_db,omitempty"`
	Checked       []SourceFileHealthItem `json:"checked"`
}

type InspectDecision struct {
	Step   string         `json:"step"`
	Inputs map[string]any `json:"inputs,omitempty"`
	Result map[string]any `json:"result,omitempty"`
	Reason string         `json:"reason,omitempty"`
}

type InspectAnomaly struct {
	Code     string         `json:"code"`
	Severity string         `json:"severity"`
	Message  string         `json:"message"`
	Evidence map[string]any `json:"evidence,omitempty"`
}

type RecordingTrace struct {
	SchemaVersion  int               `json:"schema_version"`
	Request        any               `json:"request"`
	Context        ContextResolution `json:"context"`
	Identity       map[string]any    `json:"identity"`
	RawRows        map[string]any    `json:"raw_rows"`
	Decisions      []InspectDecision `json:"decisions"`
	ComputedOutput map[string]any    `json:"computed_outputs"`
	Anomalies      []InspectAnomaly  `json:"anomalies"`
}

type AlbumTrace struct {
	SchemaVersion  int               `json:"schema_version"`
	Request        any               `json:"request"`
	Context        ContextResolution `json:"context"`
	Identity       map[string]any    `json:"identity"`
	RawRows        map[string]any    `json:"raw_rows"`
	Decisions      []InspectDecision `json:"decisions"`
	ComputedOutput map[string]any    `json:"computed_outputs"`
	Anomalies      []InspectAnomaly  `json:"anomalies"`
}

type PlaybackContextTrace struct {
	SchemaVersion  int               `json:"schema_version"`
	Request        any               `json:"request"`
	Context        ContextResolution `json:"context"`
	Identity       map[string]any    `json:"identity"`
	RawRows        map[string]any    `json:"raw_rows"`
	Decisions      []InspectDecision `json:"decisions"`
	ComputedOutput map[string]any    `json:"computed_outputs"`
	Anomalies      []InspectAnomaly  `json:"anomalies"`
}

type RecordingCacheTrace struct {
	SchemaVersion  int               `json:"schema_version"`
	Request        any               `json:"request"`
	Context        ContextResolution `json:"context"`
	Identity       map[string]any    `json:"identity"`
	RawRows        map[string]any    `json:"raw_rows"`
	Decisions      []InspectDecision `json:"decisions"`
	ComputedOutput map[string]any    `json:"computed_outputs"`
	Anomalies      []InspectAnomaly  `json:"anomalies"`
}

type BlobTrace struct {
	SchemaVersion  int               `json:"schema_version"`
	Request        any               `json:"request"`
	Context        ContextResolution `json:"context"`
	Identity       map[string]any    `json:"identity"`
	RawRows        map[string]any    `json:"raw_rows"`
	Decisions      []InspectDecision `json:"decisions"`
	ComputedOutput map[string]any    `json:"computed_outputs"`
	Anomalies      []InspectAnomaly  `json:"anomalies"`
}

type Inspector struct {
	cfg          InspectConfig
	app          *App
	mediaChecker inspectMediaChecker
}
