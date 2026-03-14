package apitypes

import (
	"context"
	"time"
)

type NetworkSyncMode string

const (
	NetworkSyncModeIdle     NetworkSyncMode = "idle"
	NetworkSyncModePeriodic NetworkSyncMode = "periodic"
	NetworkSyncModeCatchup  NetworkSyncMode = "catchup"
)

type NetworkSyncReason string

const (
	NetworkSyncReasonStartup NetworkSyncReason = "startup"
	NetworkSyncReasonJoin    NetworkSyncReason = "join"
	NetworkSyncReasonConnect NetworkSyncReason = "connect"
	NetworkSyncReasonManual  NetworkSyncReason = "manual"
	NetworkSyncReasonTimer   NetworkSyncReason = "timer"
)

type NetworkSyncState struct {
	Mode             NetworkSyncMode
	Activity         NetworkSyncActivity
	Reason           NetworkSyncReason
	ActivePeerID     string
	BacklogEstimate  int64
	LastBatchApplied int
	StartedAt        *time.Time
	CompletedAt      *time.Time
	LastSyncError    string
}

type NetworkSyncActivity string

const (
	NetworkSyncActivityOps               NetworkSyncActivity = "ops"
	NetworkSyncActivityCheckpointInstall NetworkSyncActivity = "checkpoint_install"
	NetworkSyncActivityCheckpointMirror  NetworkSyncActivity = "checkpoint_mirror"
)

type NetworkStatus struct {
	Running     bool
	LibraryID   string
	DeviceID    string
	PeerID      string
	ListenAddrs []string
	ServiceTag  string
	NetworkSyncState
}

type CheckpointDeviceCoverage struct {
	DeviceID string
	Role     string
	State    string
}

type LibraryCheckpointManifest struct {
	LibraryID         string
	CheckpointID      string
	CreatedByDeviceID string
	CreatedAt         time.Time
	BaseClocks        map[string]int64
	ChunkCount        int
	EntryCount        int
	ContentHash       string
	Status            string
	PublishedAt       *time.Time
}

type LibraryCheckpointStatus struct {
	LibraryID        string
	CheckpointID     string
	ChunkCount       int
	EntryCount       int
	AckedDevices     int
	TotalDevices     int
	Compactable      bool
	LastCheckpointAt *time.Time
	PublishedAt      *time.Time
	Devices          []CheckpointDeviceCoverage
	LastError        string
}

type CheckpointCompactionResult struct {
	LibraryID      string
	CheckpointID   string
	Compactable    bool
	Forced         bool
	DeletedOps     int64
	PendingDevices []CheckpointDeviceCoverage
}

// NetworkAdminSurface exposes peer/network coordination and checkpoint
// operations for hosts that own sync orchestration or diagnostics.
type NetworkAdminSurface interface {
	NetworkStatus() NetworkStatus
	SyncNow(ctx context.Context) error
	ConnectPeer(ctx context.Context, peerAddr string) error
	CheckpointStatus(ctx context.Context) (LibraryCheckpointStatus, error)
	PublishCheckpoint(ctx context.Context) (LibraryCheckpointManifest, error)
	CompactCheckpoint(ctx context.Context, force bool) (CheckpointCompactionResult, error)
}
