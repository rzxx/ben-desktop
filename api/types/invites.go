package apitypes

import (
	"context"
	"time"
)

type InviteJoinRequestRecord struct {
	RequestID         string
	LibraryID         string
	DeviceID          string
	DeviceName        string
	PeerID            string
	DeviceFingerprint string
	Role              string
	CreatedAt         time.Time
	ExpiresAt         time.Time
}

type InviteRecord struct {
	InviteID   string
	LibraryID  string
	InviteCode string
	Role       string
	Reusable   bool
	ExpiresAt  time.Time
	CreatedAt  time.Time
}

type InviteCreateRequest struct {
	Role     string
	Reusable bool
}

type JoinFromInviteInput struct {
	InviteCode      string
	DeviceID        string
	DeviceName      string
	DiscoverTimeout time.Duration
}

type JoinAttempt struct {
	AttemptID     string
	RequestID     string
	Status        string
	Message       string
	LibraryID     string
	Role          string
	Pending       bool
	OwnerDeviceID string
	OwnerRole     string
	OwnerPeerID   string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type JoinLibraryResult struct {
	LibraryID         string
	Role              string
	DeviceID          string
	LocalPeerID       string
	DeviceFingerprint string
	OwnerDeviceID     string
	OwnerRole         string
	OwnerPeerID       string
	OwnerFingerprint  string
}

// InviteJoinSurface is the joiner-facing invite handshake API.
type InviteJoinSurface interface {
	StartJoinFromInvite(ctx context.Context, req JoinFromInviteInput) (JoinAttempt, error)
	GetJoinAttempt(ctx context.Context, attemptID string) (JoinAttempt, error)
	CancelJoinAttempt(ctx context.Context, attemptID string) error
}

// InviteAdminSurface covers invite issuance and admission moderation.
type InviteAdminSurface interface {
	CreateInvite(ctx context.Context, req InviteCreateRequest) (InviteRecord, error)
	ListActiveInvites(ctx context.Context) ([]InviteRecord, error)
	DeleteInvite(ctx context.Context, inviteID string) error
	ListJoinRequests(ctx context.Context) ([]InviteJoinRequestRecord, error)
	ApproveJoinRequest(ctx context.Context, requestID string) error
	RejectJoinRequest(ctx context.Context, requestID string) error
}
