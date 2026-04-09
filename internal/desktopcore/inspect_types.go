package desktopcore

import "time"

const inspectSchemaVersion = 1

type InspectConfig struct {
	DBPath           string
	BlobRoot         string
	SettingsAppName  string
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
	cfg InspectConfig
	app *App
}
