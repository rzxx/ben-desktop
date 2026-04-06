package apitypes

import (
	"context"
	"time"
)

type PinSubjectKind string

const (
	PinSubjectRecordingCluster PinSubjectKind = "recording_cluster"
	PinSubjectRecordingVariant PinSubjectKind = "recording_variant"
	PinSubjectAlbumVariant     PinSubjectKind = "album_variant"
	PinSubjectPlaylist         PinSubjectKind = "playlist"
	PinSubjectLikedPlaylist    PinSubjectKind = "liked_playlist"
)

type PinSubjectRef struct {
	Kind PinSubjectKind
	ID   string
}

type PinIntentRequest struct {
	Subject PinSubjectRef
	Profile string
}

type PinStateRequest struct {
	Subject PinSubjectRef
	Profile string
}

type PinStateListRequest struct {
	Subjects []PinSubjectRef
	Profile  string
}

type PinSourceRef struct {
	Subject PinSubjectRef
	Direct  bool
	Profile string
}

type PinState struct {
	Subject PinSubjectRef
	Pinned  bool
	Direct  bool
	Covered bool
	Pending bool
	Sources []PinSourceRef
}

type PinChangeEvent struct {
	InvalidateAll bool
	Subjects      []PinSubjectRef
	OccurredAt    time.Time
}

// PinSurface exposes the durable pin state model. Job-starting APIs live on
// the desktop runtime layer because they return desktop job snapshots.
type PinSurface interface {
	Unpin(ctx context.Context, req PinIntentRequest) error
	ListPinStates(ctx context.Context, req PinStateListRequest) ([]PinState, error)
	GetPinState(ctx context.Context, req PinStateRequest) (PinState, error)
}
