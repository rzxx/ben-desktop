package apitypes

import (
	"context"
	"time"
)

type LibrarySummary struct {
	LibraryID string
	Name      string
	Role      string
	JoinedAt  time.Time
	IsActive  bool
}

type LibraryMemberStatus struct {
	LibraryID         string
	DeviceID          string
	Role              string
	PeerID            string
	LastSeenAt        *time.Time
	LastSeq           int64
	LastSyncAttemptAt *time.Time
	LastSyncSuccessAt *time.Time
	LastSyncError     string
}

type LibrarySurface interface {
	ListLibraries(ctx context.Context) ([]LibrarySummary, error)
	CreateLibrary(ctx context.Context, name string) (LibrarySummary, error)
	SelectLibrary(ctx context.Context, libraryID string) (LibrarySummary, error)
	RenameLibrary(ctx context.Context, libraryID, name string) (LibrarySummary, error)
	LeaveLibrary(ctx context.Context, libraryID string) error
	DeleteLibrary(ctx context.Context, libraryID string) error
	ActiveLibrary(ctx context.Context) (LibrarySummary, bool, error)
	ListLibraryMembers(ctx context.Context) ([]LibraryMemberStatus, error)
	UpdateLibraryMemberRole(ctx context.Context, deviceID, role string) error
	RemoveLibraryMember(ctx context.Context, deviceID string) error
}
