package apitypes

import (
	"context"
	"time"
)

type CacheKind string

const (
	CacheKindOptimizedAudio CacheKind = "optimized_audio"
	CacheKindThumbnail      CacheKind = "thumbnail"
	CacheKindUnknown        CacheKind = "unknown"
)

type CacheCleanupMode string

const (
	CacheCleanupOverLimitOnly CacheCleanupMode = "over_limit_only"
	CacheCleanupAllUnpinned   CacheCleanupMode = "all_unpinned"
	CacheCleanupBlobIDs       CacheCleanupMode = "blob_ids"
)

type CacheUsageBreakdown struct {
	Kind        CacheKind
	Bytes       int64
	Entries     int
	PinnedBytes int64
}

type CachePinScopeSummary struct {
	Scope     string
	ScopeID   string
	Durable   bool
	BlobCount int
	Bytes     int64
}

type CachePinScopeRef struct {
	Scope   string
	ScopeID string
	Durable bool
}

type CacheOverview struct {
	LimitBytes       int64
	UsedBytes        int64
	FreeBytes        int64
	EntryCount       int
	PinnedBytes      int64
	UnpinnedBytes    int64
	PinnedEntries    int
	UnpinnedEntries  int
	ReclaimableBytes int64
	ByKind           []CacheUsageBreakdown
	PinScopes        []CachePinScopeSummary
}

type CacheEntryListRequest struct {
	PageRequest
}

type CacheEntryItem struct {
	BlobID           string
	Kind             CacheKind
	SizeBytes        int64
	LastAccessed     time.Time
	Pinned           bool
	PinCount         int
	PinScopes        []CachePinScopeRef
	EncodingID       string
	Profile          string
	RecordingID      string
	AlbumID          string
	PlaylistID       string
	ThumbnailScope   string
	ThumbnailScopeID string
}

type CacheCleanupRequest struct {
	Mode    CacheCleanupMode
	BlobIDs []string
}

type CacheCleanupResult struct {
	DeletedBlobs   []string
	DeletedBytes   int64
	RemainingBytes int64
}

type CacheSurface interface {
	GetCacheOverview(ctx context.Context) (CacheOverview, error)
	ListCacheEntries(ctx context.Context, req CacheEntryListRequest) (Page[CacheEntryItem], error)
	CleanupCache(ctx context.Context, req CacheCleanupRequest) (CacheCleanupResult, error)
}
