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
	RequestedRole     string
	ApprovedRole      string
	Status            string
	Message           string
	ExpiresAt         time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type IssuedInviteRecord struct {
	InviteID        string
	LibraryID       string
	InviteCode      string
	InviteLink      string
	Role            string
	MaxUses         int
	RedemptionCount int64
	Status          string
	ExpiresAt       time.Time
	CreatedAt       time.Time
	RevokedAt       *time.Time
	RevokeReason    string
}

type InviteCodeRequest struct {
	Role    string
	Uses    int
	Expires time.Duration
}

type InviteCodeResult struct {
	LibraryID           string
	ServiceTag          string
	RegistryURL         string
	RelayBootstrapAddrs []string
	InviteCode          string
	InviteLink          string
	Role                string
	Uses                int
	ExpiresAt           time.Time
}

type JoinFromInviteInput struct {
	InviteCode      string
	DeviceID        string
	DeviceName      string
	DiscoverTimeout time.Duration
}

type JoinSession struct {
	SessionID              string
	RequestID              string
	Status                 string
	Message                string
	LibraryID              string
	Role                   string
	Pending                bool
	RegistryURL            string
	RelayBootstrapAddrs    []string
	LastResolvedOwnerAddrs []string
	OwnerDeviceID          string
	OwnerRole              string
	OwnerPeerID            string
	ExpiresAt              time.Time
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

type JoinLibraryResult struct {
	Pending             bool
	RequestID           string
	LibraryID           string
	Role                string
	DeviceID            string
	LocalPeerID         string
	DeviceFingerprint   string
	RegistryURL         string
	RelayBootstrapAddrs []string
	OwnerDeviceID       string
	OwnerRole           string
	OwnerPeerID         string
	OwnerFingerprint    string
}

// InviteJoinSurface is the joiner-facing invite handshake API.
type InviteJoinSurface interface {
	StartJoinFromInvite(ctx context.Context, req JoinFromInviteInput) (JoinSession, error)
	GetJoinSession(ctx context.Context, sessionID string) (JoinSession, error)
	FinalizeJoinSession(ctx context.Context, sessionID string) (JoinLibraryResult, error)
	CancelJoinSession(ctx context.Context, sessionID string) error
}

// InviteAdminSurface covers invite issuance and admission moderation.
type InviteAdminSurface interface {
	CreateInviteCode(ctx context.Context, req InviteCodeRequest) (InviteCodeResult, error)
	ListIssuedInvites(ctx context.Context, status string) ([]IssuedInviteRecord, error)
	RevokeIssuedInvite(ctx context.Context, inviteID, reason string) error
	ListJoinRequests(ctx context.Context, status string) ([]InviteJoinRequestRecord, error)
	ApproveJoinRequest(ctx context.Context, requestID, role string) error
	RejectJoinRequest(ctx context.Context, requestID, reason string) error
}
