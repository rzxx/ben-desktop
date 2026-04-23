package desktopcore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	apitypes "ben/desktop/api/types"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	defaultSyncBatchSize          = 500
	incrementalSyncBacklogCutover = 5000
	maxSyncCatchupRounds          = 64
	syncCatchupProtocolRetryDelay = 3 * time.Second
	syncCatchupStreamRetryDelay   = 2 * time.Second

	jobKindSyncNow     = "sync-now"
	jobKindConnectPeer = "connect-peer"
)

type transientCatchupError struct {
	err        error
	retryAfter time.Duration
	kind       string
	message    string
}

func (e *transientCatchupError) Error() string {
	if e == nil || e.err == nil {
		return ""
	}
	return e.err.Error()
}

func (e *transientCatchupError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

func classifyTransientCatchupError(syncErr error) *transientCatchupError {
	if syncErr == nil {
		return nil
	}
	message := strings.ToLower(strings.TrimSpace(syncErr.Error()))
	switch {
	case strings.Contains(message, "peer does not advertise sync protocol"):
		return &transientCatchupError{
			err:        syncErr,
			retryAfter: syncCatchupProtocolRetryDelay,
			kind:       "sync.catchup.protocol_unavailable",
			message:    "Peer connected before advertising the sync protocol; catch-up will retry after backoff",
		}
	case strings.Contains(message, "failed to negotiate protocol") && strings.Contains(message, "protocols not supported"):
		return &transientCatchupError{
			err:        syncErr,
			retryAfter: syncCatchupProtocolRetryDelay,
			kind:       "sync.catchup.protocol_unavailable",
			message:    "Peer rejected the sync protocol during negotiation; catch-up will retry after backoff",
		}
	case strings.Contains(message, "stream reset") && strings.Contains(message, "remote"):
		return &transientCatchupError{
			err:        syncErr,
			retryAfter: syncCatchupStreamRetryDelay,
			kind:       "sync.catchup.transport_reset",
			message:    "Peer closed the sync stream during negotiation; catch-up will retry after backoff",
		}
	default:
		return nil
	}
}

type SyncTransport interface {
	ListPeers(ctx context.Context, local apitypes.LocalContext) ([]SyncPeer, error)
	ResolvePeer(ctx context.Context, local apitypes.LocalContext, peerAddr string) (SyncPeer, error)
}

type SyncPeer interface {
	Address() string
	DeviceID() string
	PeerID() string
	Sync(ctx context.Context, req SyncRequest) (SyncResponse, error)
	NotifyLibraryChanged(ctx context.Context, req LibraryChangedRequest) (LibraryChangedResponse, error)
	FetchCheckpoint(ctx context.Context, req CheckpointFetchRequest) (CheckpointFetchResponse, error)
	FetchPlaybackAsset(ctx context.Context, req PlaybackAssetRequest) (PlaybackAssetResponse, error)
	FetchArtworkBlob(ctx context.Context, req ArtworkBlobRequest) (ArtworkBlobResponse, error)
	RefreshMembership(ctx context.Context, req MembershipRefreshRequest) (MembershipRefreshResponse, error)
}

type SyncRequest struct {
	LibraryID             string
	DeviceID              string
	PeerID                string
	Auth                  transportPeerAuth
	Clocks                map[string]int64
	InstalledCheckpointID string
	MaxOps                int
}

type SyncResponse struct {
	LibraryID      string
	DeviceID       string
	PeerID         string
	Auth           transportPeerAuth
	Ops            []checkpointOplogEntry
	HasMore        bool
	RemainingOps   int64
	NeedCheckpoint bool
	Checkpoint     *apitypes.LibraryCheckpointManifest
}

type LibraryChangedRequest struct {
	LibraryID string
	DeviceID  string
	PeerID    string
	Auth      transportPeerAuth
}

type LibraryChangedResponse struct {
	LibraryID string
	DeviceID  string
	PeerID    string
	Auth      transportPeerAuth
	Error     string
}

type CheckpointFetchRequest struct {
	LibraryID    string
	CheckpointID string
	Auth         transportPeerAuth
}

type CheckpointFetchResponse struct {
	Record checkpointTransferRecord
	Auth   transportPeerAuth
	Error  string
}

type PlaybackAssetRequest struct {
	LibraryID        string
	DeviceID         string
	PeerID           string
	RecordingID      string
	PreferredProfile string
	Auth             transportPeerAuth
}

type PlaybackAssetResponse struct {
	LibraryID string
	DeviceID  string
	PeerID    string
	Auth      transportPeerAuth
	Asset     PlaybackAssetTransfer
	Error     string
}

type PlaybackAssetTransfer struct {
	OptimizedAssetID  string
	SourceFileID      string
	TrackVariantID    string
	Profile           string
	BlobID            string
	MIME              string
	DurationMS        int64
	Bitrate           int
	Codec             string
	Container         string
	CreatedByDeviceID string
	Data              []byte
}

type ArtworkBlobRequest struct {
	LibraryID string
	DeviceID  string
	PeerID    string
	Auth      transportPeerAuth
	ScopeType string
	ScopeID   string
	Variant   string
	BlobID    string
	MIME      string
	FileExt   string
}

type ArtworkBlobResponse struct {
	LibraryID string
	DeviceID  string
	PeerID    string
	Auth      transportPeerAuth
	Artwork   ArtworkBlobTransfer
	Available bool
	Error     string
}

type ArtworkBlobTransfer struct {
	ScopeType string
	ScopeID   string
	Variant   string
	BlobID    string
	MIME      string
	FileExt   string
	Data      []byte
}

type syncBatch struct {
	Ops          []checkpointOplogEntry
	HasMore      bool
	RemainingOps int64
	TotalMissing int64
}

type checkpointTransferRecord struct {
	Manifest apitypes.LibraryCheckpointManifest
	Chunks   []checkpointChunk
}

func (a *SyncService) SetSyncTransport(transport SyncTransport) {
	if a == nil {
		return
	}
	a.transportMu.Lock()
	a.transport = transport
	a.transportMu.Unlock()
	if transport != nil && a.transportService != nil {
		a.transportService.Stop()
	}
}

func (a *SyncService) SyncNow(ctx context.Context) error {
	local, err := a.requireActiveContext(ctx)
	if err != nil {
		return err
	}
	return a.syncNowForLocalContext(ctx, local, nil)
}

func (a *SyncService) StartSyncNow(ctx context.Context) (JobSnapshot, error) {
	local, err := a.requireActiveContext(ctx)
	if err != nil {
		return JobSnapshot{}, err
	}
	transport := a.activeSyncTransport()
	if transport == nil {
		return JobSnapshot{}, fmt.Errorf("peer transport is not configured")
	}

	jobID := "sync:" + local.LibraryID
	return a.startActiveLibraryJob(
		ctx,
		jobID,
		jobKindSyncNow,
		local.LibraryID,
		"queued manual sync",
		"manual sync canceled because the library is no longer active",
		func(runCtx context.Context) {
			_ = a.syncNowForLocalContext(runCtx, local, a.jobs.Track(jobID, jobKindSyncNow, local.LibraryID))
		},
	)
}

func (a *SyncService) syncNowForLocalContext(ctx context.Context, local apitypes.LocalContext, job *JobTracker) error {
	local, err := a.ensureLocalPeerContext(ctx, local)
	if err != nil {
		if job != nil {
			if errors.Is(err, context.Canceled) {
				job.Fail(1, "manual sync canceled because the library is no longer active", nil)
				return err
			}
			job.Fail(1, "manual sync failed", err)
		}
		return err
	}
	if job != nil {
		job.Queued(0, "queued manual sync")
		job.Running(0.05, "discovering peers")
	}
	if err := a.catchupAllPeers(ctx, local, apitypes.NetworkSyncReasonManual, job, true); err != nil {
		if job != nil {
			if errors.Is(err, context.Canceled) {
				job.Fail(1, "manual sync canceled because the library is no longer active", nil)
				return err
			}
			job.Fail(1, "manual sync failed", err)
		}
		return err
	}
	if job != nil {
		job.Complete(1, "manual sync completed")
	}
	return nil
}

func (a *SyncService) catchupAllPeers(ctx context.Context, local apitypes.LocalContext, reason apitypes.NetworkSyncReason, job *JobTracker, failIfNoPeers bool) error {
	transport := a.activeSyncTransport()
	if transport == nil {
		return fmt.Errorf("peer transport is not configured")
	}

	peers, err := a.discoverCatchupPeers(ctx, local, transport, reason)
	if err != nil {
		return err
	}
	if len(peers) == 0 {
		if failIfNoPeers {
			return fmt.Errorf("no connected peers")
		}
		return nil
	}

	successes := 0
	failures := 0
	var firstErr error
	seen := make(map[string]struct{}, len(peers))
	processed := 0
	totalPeers := len(peers)
	for _, peer := range peers {
		key := strings.TrimSpace(peer.Address())
		if key == "" {
			key = strings.TrimSpace(peer.PeerID())
		}
		if key == "" {
			key = strings.TrimSpace(peer.DeviceID())
		}
		if key != "" {
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
		}
		processed++
		if job != nil {
			progress := 0.1
			if totalPeers > 0 {
				progress += (float64(processed-1) / float64(totalPeers)) * 0.8
			}
			job.Running(progress, syncPeerJobMessage(processed, totalPeers, peer))
		}
		if _, err := a.syncPeerCatchup(ctx, local, peer, reason, job); err != nil {
			failures++
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		successes++
	}

	if successes == 0 {
		if firstErr != nil {
			return firstErr
		}
		if failIfNoPeers {
			return fmt.Errorf("all peer sync attempts failed")
		}
		return nil
	}
	if failures > 0 && firstErr != nil && failIfNoPeers {
		return fmt.Errorf("%d peer sync attempts failed: %w", failures, firstErr)
	}
	return nil
}

func (a *SyncService) discoverCatchupPeers(ctx context.Context, local apitypes.LocalContext, transport SyncTransport, reason apitypes.NetworkSyncReason) ([]SyncPeer, error) {
	if transport == nil {
		return nil, fmt.Errorf("peer transport is not configured")
	}
	connectedPeers, err := transport.ListPeers(ctx, local)
	if err != nil {
		return nil, err
	}
	out := make([]SyncPeer, 0, len(connectedPeers))
	seen := make(map[string]struct{}, len(connectedPeers))
	appendPeer := func(candidate SyncPeer) {
		if candidate == nil {
			return
		}
		key := strings.TrimSpace(candidate.PeerID())
		if key == "" {
			key = strings.TrimSpace(candidate.DeviceID())
		}
		if key == "" {
			key = strings.TrimSpace(candidate.Address())
		}
		if key == "" {
			return
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, candidate)
	}
	for _, candidate := range connectedPeers {
		appendPeer(candidate)
	}

	resolver, ok := transport.(transportPeerIdentityResolver)
	if !ok {
		return out, nil
	}
	hints, err := a.listMemberPeerHints(ctx, local.LibraryID, local.DeviceID)
	if err != nil {
		return out, err
	}
	for _, hint := range hints {
		if hint.peerID == "" && hint.deviceID == "" {
			continue
		}
		if !shouldAttemptCatchupHint(reason, hint.lastSeenAt) {
			continue
		}
		if _, exists := seen[firstNonEmpty(hint.peerID, hint.deviceID)]; exists {
			continue
		}
		peer, err := resolver.ResolvePeerByIdentity(ctx, local, hint.peerID, hint.deviceID)
		if err != nil {
			a.recordNetworkDebug(apitypes.NetworkDebugTraceEntry{
				Level:     "warn",
				Kind:      "peer.discovery.lookup_failed",
				Message:   "Peer identity lookup failed",
				LibraryID: local.LibraryID,
				DeviceID:  local.DeviceID,
				PeerID:    hint.peerID,
				Error:     err.Error(),
			})
			continue
		}
		appendPeer(peer)
	}
	return out, nil
}

type memberPeerHint struct {
	deviceID   string
	peerID     string
	lastSeenAt *time.Time
}

func (a *SyncService) listMemberPeerHints(ctx context.Context, libraryID, localDeviceID string) ([]memberPeerHint, error) {
	type row struct {
		DeviceID   string
		PeerID     string
		LastSeenAt *time.Time
	}
	var rows []row
	err := a.storage.WithContext(ctx).
		Table("memberships AS m").
		Select("m.device_id AS device_id, COALESCE(d.peer_id, '') AS peer_id, d.last_seen_at AS last_seen_at").
		Joins("LEFT JOIN devices d ON d.device_id = m.device_id").
		Where("m.library_id = ? AND m.device_id <> ?", strings.TrimSpace(libraryID), strings.TrimSpace(localDeviceID)).
		Order("m.device_id ASC").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]memberPeerHint, 0, len(rows))
	for _, row := range rows {
		out = append(out, memberPeerHint{
			deviceID:   strings.TrimSpace(row.DeviceID),
			peerID:     strings.TrimSpace(row.PeerID),
			lastSeenAt: cloneTimePtr(row.LastSeenAt),
		})
	}
	return out, nil
}

func shouldAttemptCatchupHint(reason apitypes.NetworkSyncReason, lastSeenAt *time.Time) bool {
	switch reason {
	case apitypes.NetworkSyncReasonManual, apitypes.NetworkSyncReasonJoin:
		return true
	default:
		return lastSeenAt != nil && lastSeenAt.UTC().After(time.Now().UTC().Add(-availabilityOnlineWindow))
	}
}

func syncPeerJobMessage(index, total int, peer SyncPeer) string {
	if total <= 0 {
		total = 1
	}
	target := strings.TrimSpace(peer.Address())
	if target == "" {
		target = strings.TrimSpace(peer.PeerID())
	}
	if target == "" {
		target = strings.TrimSpace(peer.DeviceID())
	}
	if target == "" {
		return fmt.Sprintf("syncing peer %d of %d", index, total)
	}
	return fmt.Sprintf("syncing peer %d of %d: %s", index, total, target)
}

func (a *SyncService) ConnectPeer(ctx context.Context, peerAddr string) error {
	local, err := a.requireActiveContext(ctx)
	if err != nil {
		return err
	}
	local, err = a.ensureLocalPeerContext(ctx, local)
	if err != nil {
		return err
	}
	peerAddr = strings.TrimSpace(peerAddr)
	if peerAddr == "" {
		return fmt.Errorf("peer address is required")
	}
	transport := a.activeSyncTransport()
	if transport == nil {
		return fmt.Errorf("peer transport is not configured")
	}

	a.recordNetworkDebug(apitypes.NetworkDebugTraceEntry{
		Level:     "info",
		Kind:      "connect.resolve.start",
		Message:   "Resolving peer address",
		LibraryID: local.LibraryID,
		DeviceID:  local.DeviceID,
		Address:   peerAddr,
		Reason:    string(apitypes.NetworkSyncReasonConnect),
	})

	peer, err := transport.ResolvePeer(ctx, local, peerAddr)
	if err != nil {
		a.recordNetworkDebug(apitypes.NetworkDebugTraceEntry{
			Level:     "error",
			Kind:      "connect.resolve.failed",
			Message:   "Peer resolution failed",
			LibraryID: local.LibraryID,
			DeviceID:  local.DeviceID,
			Address:   peerAddr,
			Reason:    string(apitypes.NetworkSyncReasonConnect),
			Error:     err.Error(),
		})
		return err
	}
	a.recordNetworkDebug(apitypes.NetworkDebugTraceEntry{
		Level:     "info",
		Kind:      "connect.resolve.succeeded",
		Message:   "Peer resolved successfully",
		LibraryID: local.LibraryID,
		DeviceID:  local.DeviceID,
		PeerID:    peer.PeerID(),
		Address:   firstNonEmpty(peer.Address(), peerAddr),
		Reason:    string(apitypes.NetworkSyncReasonConnect),
	})
	applied, err := a.syncPeerCatchup(ctx, local, peer, apitypes.NetworkSyncReasonConnect, nil)
	if err != nil {
		a.recordNetworkDebug(apitypes.NetworkDebugTraceEntry{
			Level:     "error",
			Kind:      "connect.catchup.failed",
			Message:   "Peer catch-up failed",
			LibraryID: local.LibraryID,
			DeviceID:  local.DeviceID,
			PeerID:    peer.PeerID(),
			Address:   firstNonEmpty(peer.Address(), peerAddr),
			Reason:    string(apitypes.NetworkSyncReasonConnect),
			Error:     err.Error(),
		})
		return err
	}
	a.recordNetworkDebug(apitypes.NetworkDebugTraceEntry{
		Level:     "info",
		Kind:      "connect.catchup.succeeded",
		Message:   fmt.Sprintf("Peer catch-up completed with %d applied ops", applied),
		LibraryID: local.LibraryID,
		DeviceID:  local.DeviceID,
		PeerID:    peer.PeerID(),
		Address:   firstNonEmpty(peer.Address(), peerAddr),
		Reason:    string(apitypes.NetworkSyncReasonConnect),
	})
	return nil
}

func (a *SyncService) StartConnectPeer(ctx context.Context, peerAddr string) (JobSnapshot, error) {
	local, err := a.requireActiveContext(ctx)
	if err != nil {
		return JobSnapshot{}, err
	}
	peerAddr = strings.TrimSpace(peerAddr)
	if peerAddr == "" {
		return JobSnapshot{}, fmt.Errorf("peer address is required")
	}

	jobID := "connect-peer:" + local.LibraryID + ":" + peerAddr
	return a.startActiveLibraryJob(
		ctx,
		jobID,
		jobKindConnectPeer,
		local.LibraryID,
		"queued peer connect",
		"peer connect canceled because the library is no longer active",
		func(runCtx context.Context) {
			job := a.jobs.Track(jobID, jobKindConnectPeer, local.LibraryID)
			if job != nil {
				job.Running(0.1, "resolving peer")
			}
			err := a.ConnectPeer(runCtx, peerAddr)
			if job == nil {
				return
			}
			if err != nil {
				if errors.Is(err, context.Canceled) {
					job.Fail(1, "peer connect canceled because the library is no longer active", nil)
					return
				}
				job.Fail(1, "peer connect failed", err)
				return
			}
			job.Complete(1, "peer connect completed")
		},
	)
}

func (a *SyncService) syncPeerCatchup(ctx context.Context, local apitypes.LocalContext, peer SyncPeer, reason apitypes.NetworkSyncReason, job *JobTracker) (int, error) {
	if peer == nil {
		return 0, fmt.Errorf("sync peer is required")
	}
	startedAt := time.Now().UTC()
	totalApplied := 0
	needsCatalogRebuild := false
	defer func() {
		if totalApplied > 0 {
			a.emitAvailabilityInvalidateAllForActiveLibrary(local.LibraryID)
		}
	}()
	remoteDeviceID := strings.TrimSpace(peer.DeviceID())
	remotePeerID := strings.TrimSpace(peer.PeerID())
	a.recordNetworkDebug(apitypes.NetworkDebugTraceEntry{
		Level:     "info",
		Kind:      "sync.catchup.start",
		Message:   "Starting peer catch-up",
		LibraryID: local.LibraryID,
		DeviceID:  local.DeviceID,
		PeerID:    remotePeerID,
		Address:   peer.Address(),
		Reason:    string(reason),
	})

	for round := 0; round < maxSyncCatchupRounds; round++ {
		if err := ctx.Err(); err != nil {
			if !errors.Is(err, context.Canceled) {
				a.recordPeerSyncFailure(ctx, local.LibraryID, remoteDeviceID, remotePeerID, err)
			}
			return totalApplied, err
		}

		req, err := a.buildSyncRequest(ctx, local.LibraryID, local.DeviceID, local.PeerID, defaultSyncBatchSize)
		if err != nil {
			a.recordPeerSyncFailure(ctx, local.LibraryID, remoteDeviceID, remotePeerID, err)
			return totalApplied, err
		}
		a.noteNetworkSyncPeer(local.LibraryID, firstNonEmpty(peer.PeerID(), remotePeerID))

		resp, err := peer.Sync(ctx, req)
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				if transient := classifyTransientCatchupError(err); transient != nil {
					a.recordNetworkDebug(apitypes.NetworkDebugTraceEntry{
						Level:     "warn",
						Kind:      transient.kind,
						Message:   transient.message,
						LibraryID: local.LibraryID,
						DeviceID:  local.DeviceID,
						PeerID:    remotePeerID,
						Address:   peer.Address(),
						Reason:    string(reason),
						Error:     transient.Error(),
					})
					err = transient
				}
				a.recordPeerSyncFailure(ctx, local.LibraryID, remoteDeviceID, remotePeerID, err)
			}
			return totalApplied, err
		}
		if strings.TrimSpace(resp.LibraryID) != strings.TrimSpace(local.LibraryID) {
			err := fmt.Errorf("remote library mismatch")
			a.recordPeerSyncFailure(ctx, local.LibraryID, remoteDeviceID, remotePeerID, err)
			return totalApplied, err
		}
		if _, err := a.verifyTransportPeerAuth(ctx, local.LibraryID, resp.DeviceID, resp.PeerID, firstNonEmpty(peer.PeerID(), remotePeerID), resp.Auth); err != nil {
			a.recordPeerSyncFailure(ctx, local.LibraryID, remoteDeviceID, remotePeerID, err)
			return totalApplied, err
		}
		remoteDeviceID = firstNonEmpty(resp.DeviceID, remoteDeviceID)
		remotePeerID = firstNonEmpty(resp.PeerID, remotePeerID)
		_ = a.updateDevicePeerID(ctx, local.LibraryID, remoteDeviceID, remotePeerID, remoteDeviceID)

		if resp.NeedCheckpoint {
			backlogEstimate := int64(0)
			if resp.Checkpoint != nil {
				backlogEstimate = int64(resp.Checkpoint.EntryCount)
			}
			a.noteNetworkSyncProgress(local.LibraryID, remotePeerID, apitypes.NetworkSyncActivityCheckpointInstall, backlogEstimate, totalApplied)
			if resp.Checkpoint == nil || strings.TrimSpace(resp.Checkpoint.CheckpointID) == "" {
				err := fmt.Errorf("remote checkpoint response missing checkpoint summary")
				a.recordPeerSyncFailure(ctx, local.LibraryID, remoteDeviceID, remotePeerID, err)
				return totalApplied, err
			}
			if job != nil {
				job.Running(0.55, "fetching published checkpoint")
			}
			installJobID := checkpointInstallJobID(local.LibraryID, resp.Checkpoint.CheckpointID)
			_, _ = a.jobs.Begin(installJobID, jobKindInstallCheckpoint, local.LibraryID, "queued checkpoint install")
			installJob := a.jobs.Track(installJobID, jobKindInstallCheckpoint, local.LibraryID)
			if installJob != nil {
				installJob.Running(0.2, fmt.Sprintf("fetching checkpoint %s", resp.Checkpoint.CheckpointID))
			}
			fetchResp, err := peer.FetchCheckpoint(ctx, CheckpointFetchRequest{
				LibraryID:    local.LibraryID,
				CheckpointID: resp.Checkpoint.CheckpointID,
				Auth:         req.Auth,
			})
			if err != nil {
				if installJob != nil {
					if errors.Is(err, context.Canceled) {
						installJob.Fail(1, "checkpoint install canceled", nil)
					} else {
						installJob.Fail(1, "checkpoint install failed", err)
					}
				}
				if !errors.Is(err, context.Canceled) {
					a.recordPeerSyncFailure(ctx, local.LibraryID, remoteDeviceID, remotePeerID, err)
				}
				return totalApplied, err
			}
			if _, err := a.verifyTransportPeerAuth(ctx, local.LibraryID, remoteDeviceID, remotePeerID, firstNonEmpty(peer.PeerID(), remotePeerID), fetchResp.Auth); err != nil {
				if installJob != nil {
					installJob.Fail(1, "checkpoint install failed", err)
				}
				a.recordPeerSyncFailure(ctx, local.LibraryID, remoteDeviceID, remotePeerID, err)
				return totalApplied, err
			}
			if job != nil {
				job.Running(0.7, "installing published checkpoint")
			}
			applied, err := a.installCheckpointRecordWithJob(ctx, local.DeviceID, fetchResp.Record, installJob)
			if err != nil {
				if installJob != nil {
					if errors.Is(err, context.Canceled) {
						installJob.Fail(1, "checkpoint install canceled", nil)
					} else {
						installJob.Fail(1, "checkpoint install failed", err)
					}
				}
				if !errors.Is(err, context.Canceled) {
					a.recordPeerSyncFailure(ctx, local.LibraryID, remoteDeviceID, remotePeerID, err)
				}
				return totalApplied, err
			}
			if installJob != nil {
				installJob.Complete(1, fmt.Sprintf("installed checkpoint %s", resp.Checkpoint.CheckpointID))
			}
			totalApplied += applied
			a.noteNetworkSyncProgress(local.LibraryID, remotePeerID, apitypes.NetworkSyncActivityCheckpointInstall, 0, applied)
			a.recordPeerSyncSuccess(ctx, local.LibraryID, remoteDeviceID, remotePeerID, int64(applied))
			continue
		}

		a.noteNetworkSyncProgress(local.LibraryID, remotePeerID, apitypes.NetworkSyncActivityOps, resp.RemainingOps+int64(len(resp.Ops)), totalApplied)
		applied, batchNeedsCatalogRebuild, err := a.applyRemoteOpsSummary(ctx, local.LibraryID, resp.Ops)
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				a.recordPeerSyncFailure(ctx, local.LibraryID, remoteDeviceID, remotePeerID, err)
			}
			return totalApplied, err
		}
		needsCatalogRebuild = needsCatalogRebuild || batchNeedsCatalogRebuild
		totalApplied += applied
		a.noteNetworkSyncProgress(local.LibraryID, remotePeerID, apitypes.NetworkSyncActivityOps, resp.RemainingOps, applied)
		a.recordPeerSyncSuccess(ctx, local.LibraryID, remoteDeviceID, remotePeerID, int64(applied))

		if !resp.HasMore || (len(resp.Ops) == 0 && applied == 0) {
			if needsCatalogRebuild {
				if err := a.rebuildCatalogMaterializationFull(ctx, local.LibraryID, nil); err != nil {
					if !errors.Is(err, context.Canceled) {
						a.recordPeerSyncFailure(ctx, local.LibraryID, remoteDeviceID, remotePeerID, err)
					}
					return totalApplied, err
				}
			}
			if _, err := a.syncMissingArtworkBlobsFromPeer(ctx, local, peer); err != nil {
				if !errors.Is(err, context.Canceled) {
					a.recordPeerSyncFailure(ctx, local.LibraryID, remoteDeviceID, remotePeerID, err)
				}
				return totalApplied, err
			}
			_ = startedAt
			a.recordNetworkDebug(apitypes.NetworkDebugTraceEntry{
				Level:     "info",
				Kind:      "sync.catchup.succeeded",
				Message:   fmt.Sprintf("Peer catch-up finished in %d rounds with %d applied ops", round+1, totalApplied),
				LibraryID: local.LibraryID,
				DeviceID:  local.DeviceID,
				PeerID:    remotePeerID,
				Address:   peer.Address(),
				Reason:    string(reason),
			})
			return totalApplied, nil
		}
	}

	err := fmt.Errorf("catch-up session budget exhausted")
	a.recordPeerSyncFailure(ctx, local.LibraryID, remoteDeviceID, remotePeerID, err)
	return totalApplied, err
}

func (a *SyncService) noteNetworkSyncPeer(libraryID, peerID string) {
	if a == nil || a.transportService == nil {
		return
	}
	a.transportService.noteRuntimeSyncPeer(libraryID, peerID)
}

func (a *SyncService) noteNetworkSyncProgress(libraryID, peerID string, activity apitypes.NetworkSyncActivity, backlog int64, applied int) {
	if a == nil || a.transportService == nil {
		return
	}
	a.transportService.noteRuntimeSyncProgress(libraryID, peerID, activity, backlog, applied)
}

func (a *SyncService) ensureLocalPeerContext(ctx context.Context, local apitypes.LocalContext) (apitypes.LocalContext, error) {
	if strings.TrimSpace(local.PeerID) != "" {
		return local, nil
	}
	peerID, err := a.ensureDevicePeerID(ctx, local.DeviceID, local.Device)
	if err != nil {
		return apitypes.LocalContext{}, err
	}
	local.PeerID = peerID
	return local, nil
}

func (a *SyncService) buildSyncRequest(ctx context.Context, libraryID, deviceID, peerID string, maxOps int) (SyncRequest, error) {
	clocks, err := a.listDeviceClocks(ctx, libraryID)
	if err != nil {
		return SyncRequest{}, fmt.Errorf("list device clocks: %w", err)
	}
	ack, ok, err := a.checkpointAckForDevice(ctx, libraryID, deviceID)
	if err != nil {
		return SyncRequest{}, fmt.Errorf("load checkpoint ack: %w", err)
	}
	if maxOps <= 0 {
		maxOps = defaultSyncBatchSize
	}

	req := SyncRequest{
		LibraryID: libraryID,
		DeviceID:  deviceID,
		PeerID:    peerID,
		Clocks:    clocksToMap(clocks),
		MaxOps:    maxOps,
	}
	if ok {
		req.InstalledCheckpointID = strings.TrimSpace(ack.CheckpointID)
	}
	auth, err := a.ensureLocalTransportMembershipAuth(ctx, apitypes.LocalContext{
		LibraryID: libraryID,
		DeviceID:  deviceID,
		Role:      firstNonEmpty(a.membershipRole(ctx, libraryID, deviceID), roleMember),
		PeerID:    peerID,
	}, peerID)
	if err != nil {
		return SyncRequest{}, fmt.Errorf("build local transport auth: %w", err)
	}
	req.Auth = auth
	return req, nil
}

func (a *SyncService) buildSyncResponse(ctx context.Context, req SyncRequest) (SyncResponse, error) {
	local, err := a.requireActiveContext(ctx)
	if err != nil {
		return SyncResponse{}, err
	}
	local, err = a.ensureLocalPeerContext(ctx, local)
	if err != nil {
		return SyncResponse{}, err
	}
	if err := a.ensureLocalOplogSignatures(ctx, local); err != nil {
		return SyncResponse{}, fmt.Errorf("ensure local oplog signatures: %w", err)
	}
	if strings.TrimSpace(req.LibraryID) != strings.TrimSpace(local.LibraryID) {
		return SyncResponse{}, fmt.Errorf("remote library mismatch")
	}

	published, hasPublished, err := a.loadCheckpointTransferRecord(ctx, req.LibraryID, "", true)
	if err != nil {
		return SyncResponse{}, fmt.Errorf("load published checkpoint: %w", err)
	}
	if hasPublished {
		now := time.Now().UTC()
		switch {
		case strings.TrimSpace(req.InstalledCheckpointID) == strings.TrimSpace(published.Manifest.CheckpointID):
			if err := a.storage.Transaction(ctx, func(tx *gorm.DB) error {
				return recordCheckpointAckTx(tx, req.LibraryID, req.DeviceID, published.Manifest.CheckpointID, checkpointAckSourceInstalled, now)
			}); err != nil {
				return SyncResponse{}, err
			}
		case clocksCoverCheckpoint(req.Clocks, published.Manifest.BaseClocks):
			if err := a.storage.Transaction(ctx, func(tx *gorm.DB) error {
				return recordCheckpointAckTx(tx, req.LibraryID, req.DeviceID, published.Manifest.CheckpointID, checkpointAckSourceCovered, now)
			}); err != nil {
				return SyncResponse{}, err
			}
		}
	}

	batch, err := a.selectSyncBatch(ctx, req.LibraryID, req.Clocks, req.MaxOps)
	if err != nil {
		return SyncResponse{}, fmt.Errorf("select sync batch: %w", err)
	}

	resp := SyncResponse{
		LibraryID:    req.LibraryID,
		DeviceID:     local.DeviceID,
		PeerID:       local.PeerID,
		Ops:          batch.Ops,
		HasMore:      batch.HasMore,
		RemainingOps: batch.RemainingOps,
	}
	auth, err := a.ensureLocalTransportMembershipAuth(ctx, local, local.PeerID)
	if err != nil {
		return SyncResponse{}, fmt.Errorf("build local transport auth: %w", err)
	}
	resp.Auth = auth
	if hasPublished && (requesterNeedsCheckpoint(req.Clocks, published.Manifest.BaseClocks) || batch.TotalMissing >= incrementalSyncBacklogCutover) {
		resp.Ops = nil
		resp.HasMore = true
		resp.NeedCheckpoint = true
		resp.Checkpoint = &published.Manifest
		resp.RemainingOps = 0
	}
	return resp, nil
}

func (a *SyncService) buildCheckpointFetchResponse(ctx context.Context, req CheckpointFetchRequest) (CheckpointFetchResponse, error) {
	local, err := a.requireActiveContext(ctx)
	if err != nil {
		return CheckpointFetchResponse{}, err
	}
	local, err = a.ensureLocalPeerContext(ctx, local)
	if err != nil {
		return CheckpointFetchResponse{}, err
	}
	if strings.TrimSpace(req.LibraryID) != strings.TrimSpace(local.LibraryID) {
		return CheckpointFetchResponse{}, fmt.Errorf("remote library mismatch")
	}

	record, ok, err := a.loadCheckpointTransferRecord(ctx, req.LibraryID, req.CheckpointID, false)
	if err != nil {
		return CheckpointFetchResponse{}, err
	}
	if !ok {
		return CheckpointFetchResponse{}, fmt.Errorf("checkpoint not found")
	}
	auth, err := a.ensureLocalTransportMembershipAuth(ctx, local, local.PeerID)
	if err != nil {
		return CheckpointFetchResponse{}, fmt.Errorf("build local transport auth: %w", err)
	}
	return CheckpointFetchResponse{Record: record, Auth: auth}, nil
}

func (a *SyncService) buildLibraryChangedResponse(ctx context.Context, libraryID, deviceID, peerID string) (LibraryChangedResponse, error) {
	libraryID = strings.TrimSpace(libraryID)
	deviceID = strings.TrimSpace(deviceID)
	peerID = strings.TrimSpace(peerID)
	if libraryID == "" || deviceID == "" || peerID == "" {
		return LibraryChangedResponse{}, fmt.Errorf("library id, device id, and peer id are required")
	}
	auth, err := a.ensureLocalTransportMembershipAuth(ctx, apitypes.LocalContext{
		LibraryID: libraryID,
		DeviceID:  deviceID,
		PeerID:    peerID,
		Role:      firstNonEmpty(a.membershipRole(ctx, libraryID, deviceID), roleMember),
	}, peerID)
	if err != nil {
		return LibraryChangedResponse{}, fmt.Errorf("build local transport auth: %w", err)
	}
	return LibraryChangedResponse{
		LibraryID: libraryID,
		DeviceID:  deviceID,
		PeerID:    peerID,
		Auth:      auth,
	}, nil
}

func (a *SyncService) membershipRole(ctx context.Context, libraryID, deviceID string) string {
	var row Membership
	if err := a.storage.WithContext(ctx).
		Select("role").
		Where("library_id = ? AND device_id = ?", strings.TrimSpace(libraryID), strings.TrimSpace(deviceID)).
		Take(&row).Error; err != nil {
		return ""
	}
	return normalizeRole(row.Role)
}

func (a *SyncService) listDeviceClocks(ctx context.Context, libraryID string) ([]DeviceClock, error) {
	var rows []DeviceClock
	if err := a.storage.WithContext(ctx).
		Where("library_id = ?", strings.TrimSpace(libraryID)).
		Order("device_id ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func clocksToMap(rows []DeviceClock) map[string]int64 {
	if len(rows) == 0 {
		return map[string]int64{}
	}
	out := make(map[string]int64, len(rows))
	for _, row := range rows {
		out[strings.TrimSpace(row.DeviceID)] = row.LastSeqSeen
	}
	return out
}

func requesterNeedsCheckpoint(requestClocks, baseClocks map[string]int64) bool {
	for deviceID, seq := range baseClocks {
		if seq > 0 && requestClocks[strings.TrimSpace(deviceID)] < seq {
			return true
		}
	}
	return false
}

func clocksCoverCheckpoint(clocks, baseClocks map[string]int64) bool {
	for deviceID, seq := range baseClocks {
		if clocks[strings.TrimSpace(deviceID)] < seq {
			return false
		}
	}
	return true
}

func (a *SyncService) selectSyncBatch(ctx context.Context, libraryID string, clocks map[string]int64, limit int) (syncBatch, error) {
	if limit <= 0 {
		limit = defaultSyncBatchSize
	}
	deviceClocks, err := a.listDeviceClocks(ctx, libraryID)
	if err != nil {
		return syncBatch{}, err
	}

	type deviceBacklog struct {
		entries    []checkpointOplogEntry
		nextCursor int
	}

	backlogs := make([]deviceBacklog, 0, len(deviceClocks))
	totalMissing := int64(0)
	for _, clock := range deviceClocks {
		deviceID := strings.TrimSpace(clock.DeviceID)
		since := int64(0)
		if clocks != nil {
			since = clocks[deviceID]
		}
		missing := clock.LastSeqSeen - since
		if missing <= 0 {
			continue
		}
		totalMissing += missing
		entries, err := a.listOplogByDevice(ctx, libraryID, deviceID, since, limit)
		if err != nil {
			return syncBatch{}, err
		}
		backlogs = append(backlogs, deviceBacklog{entries: entries})
	}

	ops := make([]checkpointOplogEntry, 0, limit)
	for len(ops) < limit {
		progress := false
		for i := range backlogs {
			if len(ops) >= limit {
				break
			}
			if backlogs[i].nextCursor >= len(backlogs[i].entries) {
				continue
			}
			ops = append(ops, backlogs[i].entries[backlogs[i].nextCursor])
			backlogs[i].nextCursor++
			progress = true
		}
		if !progress {
			break
		}
	}
	sortCheckpointEntries(ops)

	remaining := totalMissing - int64(len(ops))
	if remaining < 0 {
		remaining = 0
	}
	return syncBatch{
		Ops:          ops,
		HasMore:      remaining > 0,
		RemainingOps: remaining,
		TotalMissing: totalMissing,
	}, nil
}

func (a *SyncService) listOplogByDevice(ctx context.Context, libraryID, deviceID string, sinceSeq int64, limit int) ([]checkpointOplogEntry, error) {
	if limit <= 0 {
		limit = defaultSyncBatchSize
	}
	var rows []OplogEntry
	query := a.storage.WithContext(ctx).
		Where("library_id = ? AND device_id = ?", strings.TrimSpace(libraryID), strings.TrimSpace(deviceID)).
		Order("seq ASC").
		Limit(limit)
	if sinceSeq > 0 {
		query = query.Where("seq > ?", sinceSeq)
	}
	if err := query.Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]checkpointOplogEntry, 0, len(rows))
	for _, row := range rows {
		out = append(out, checkpointEntryFromRow(row))
	}
	return out, nil
}

func sortCheckpointEntries(entries []checkpointOplogEntry) {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].TSNS == entries[j].TSNS {
			if entries[i].DeviceID == entries[j].DeviceID {
				if entries[i].Seq == entries[j].Seq {
					return entries[i].OpID < entries[j].OpID
				}
				return entries[i].Seq < entries[j].Seq
			}
			return entries[i].DeviceID < entries[j].DeviceID
		}
		return entries[i].TSNS < entries[j].TSNS
	})
}

func (a *SyncService) checkpointAckForDevice(ctx context.Context, libraryID, deviceID string) (DeviceCheckpointAck, bool, error) {
	var row DeviceCheckpointAck
	err := a.storage.WithContext(ctx).
		Where("library_id = ? AND device_id = ?", strings.TrimSpace(libraryID), strings.TrimSpace(deviceID)).
		Take(&row).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return DeviceCheckpointAck{}, false, nil
		}
		return DeviceCheckpointAck{}, false, err
	}
	return row, true, nil
}

func (a *SyncService) loadCheckpointTransferRecord(ctx context.Context, libraryID, checkpointID string, publishedOnly bool) (checkpointTransferRecord, bool, error) {
	libraryID = strings.TrimSpace(libraryID)
	checkpointID = strings.TrimSpace(checkpointID)
	if libraryID == "" {
		return checkpointTransferRecord{}, false, fmt.Errorf("library id is required")
	}

	var row LibraryCheckpoint
	query := a.storage.WithContext(ctx).Where("library_id = ?", libraryID)
	if publishedOnly {
		query = query.Where("published_at IS NOT NULL").Order("published_at DESC")
	} else {
		query = query.Where("checkpoint_id = ?", checkpointID)
	}
	if err := query.Limit(1).Take(&row).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return checkpointTransferRecord{}, false, nil
		}
		return checkpointTransferRecord{}, false, err
	}

	manifest, err := checkpointManifestFromRow(row)
	if err != nil {
		return checkpointTransferRecord{}, false, err
	}

	var chunkRows []LibraryCheckpointChunk
	if err := a.storage.WithContext(ctx).
		Where("library_id = ? AND checkpoint_id = ?", row.LibraryID, row.CheckpointID).
		Order("chunk_index ASC").
		Find(&chunkRows).Error; err != nil {
		return checkpointTransferRecord{}, false, err
	}
	chunks := make([]checkpointChunk, 0, len(chunkRows))
	for _, chunkRow := range chunkRows {
		var entries []checkpointOplogEntry
		if strings.TrimSpace(chunkRow.PayloadJSON) != "" {
			if err := json.Unmarshal([]byte(chunkRow.PayloadJSON), &entries); err != nil {
				return checkpointTransferRecord{}, false, fmt.Errorf("decode checkpoint chunk %d: %w", chunkRow.ChunkIndex, err)
			}
		}
		chunks = append(chunks, checkpointChunk{
			ChunkIndex:  chunkRow.ChunkIndex,
			EntryCount:  chunkRow.EntryCount,
			ContentHash: strings.TrimSpace(chunkRow.ContentHash),
			Entries:     entries,
		})
	}
	return checkpointTransferRecord{Manifest: manifest, Chunks: chunks}, true, nil
}

func checkpointManifestFromRow(row LibraryCheckpoint) (apitypes.LibraryCheckpointManifest, error) {
	baseClocks := make(map[string]int64)
	if strings.TrimSpace(row.BaseClocksJSON) != "" {
		if err := json.Unmarshal([]byte(row.BaseClocksJSON), &baseClocks); err != nil {
			return apitypes.LibraryCheckpointManifest{}, fmt.Errorf("decode checkpoint clocks: %w", err)
		}
	}
	return apitypes.LibraryCheckpointManifest{
		LibraryID:         strings.TrimSpace(row.LibraryID),
		CheckpointID:      strings.TrimSpace(row.CheckpointID),
		CreatedByDeviceID: strings.TrimSpace(row.CreatedByDeviceID),
		CreatedAt:         row.CreatedAt,
		BaseClocks:        baseClocks,
		ChunkCount:        row.ChunkCount,
		EntryCount:        row.EntryCount,
		ContentHash:       strings.TrimSpace(row.ContentHash),
		Status:            strings.TrimSpace(row.Status),
		PublishedAt:       cloneTimePtr(row.PublishedAt),
	}, nil
}

func (a *SyncService) installCheckpointRecord(ctx context.Context, localDeviceID string, record checkpointTransferRecord) (int, error) {
	return a.installCheckpointRecordWithJob(ctx, localDeviceID, record, nil)
}

func (a *SyncService) installCheckpointRecordWithJob(ctx context.Context, localDeviceID string, record checkpointTransferRecord, job *JobTracker) (int, error) {
	if job != nil {
		job.Running(0.35, "validating checkpoint manifest")
	}
	if err := validateCheckpointTransferRecord(record); err != nil {
		return 0, err
	}

	totalEntries := 0
	for _, chunk := range record.Chunks {
		totalEntries += len(chunk.Entries)
	}
	if job != nil {
		job.Running(0.55, "installing checkpoint state")
	}

	err := a.storage.Transaction(ctx, func(tx *gorm.DB) error {
		preservedTail, err := selectCheckpointTailOpsTx(tx, record.Manifest.LibraryID, record.Manifest.BaseClocks)
		if err != nil {
			return err
		}
		if err := savePublishedCheckpointTx(tx, record.Manifest, record.Chunks); err != nil {
			return err
		}
		if err := clearCheckpointManagedStateTx(tx, record.Manifest.LibraryID); err != nil {
			return err
		}
		if err := insertCheckpointOpsTx(tx, record.Manifest.LibraryID, record.Chunks); err != nil {
			return err
		}

		deviceIDs := make([]string, 0, len(record.Manifest.BaseClocks))
		for deviceID := range record.Manifest.BaseClocks {
			deviceIDs = append(deviceIDs, strings.TrimSpace(deviceID))
		}
		sort.Strings(deviceIDs)
		for _, deviceID := range deviceIDs {
			if _, err := applyBufferedDeviceOplogTx(tx, record.Manifest.LibraryID, deviceID, 1); err != nil {
				return fmt.Errorf("replay checkpoint device %s: %w", deviceID, err)
			}
		}
		if job != nil {
			job.Running(0.8, "replaying post-checkpoint tail ops")
		}

		restoreDevices := make(map[string]struct{}, len(preservedTail))
		for _, op := range preservedTail {
			if err := tx.Create(&op).Error; err != nil {
				return fmt.Errorf("restore checkpoint tail op %s: %w", op.OpID, err)
			}
			deviceID := strings.TrimSpace(op.DeviceID)
			if deviceID != "" {
				restoreDevices[deviceID] = struct{}{}
			}
		}
		for deviceID := range restoreDevices {
			startSeq := record.Manifest.BaseClocks[deviceID] + 1
			if _, err := applyBufferedDeviceOplogTx(tx, record.Manifest.LibraryID, deviceID, startSeq); err != nil {
				return fmt.Errorf("replay checkpoint tail device %s: %w", deviceID, err)
			}
		}
		if _, err := a.prepareCatalogRebuildTx(tx, record.Manifest.LibraryID, nil); err != nil {
			return fmt.Errorf("prepare catalog rebuild: %w", err)
		}

		if strings.TrimSpace(localDeviceID) != "" {
			if job != nil {
				job.Running(0.95, "recording local checkpoint install ack")
			}
			if err := ensureLikedPlaylistTx(tx, record.Manifest.LibraryID, localDeviceID, time.Now().UTC()); err != nil {
				return err
			}
			if err := recordCheckpointAckTx(tx, record.Manifest.LibraryID, localDeviceID, record.Manifest.CheckpointID, checkpointAckSourceInstalled, time.Now().UTC()); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	a.emitCatalogChange(apitypes.CatalogChangeEvent{
		Kind:          apitypes.CatalogChangeInvalidateBase,
		InvalidateAll: true,
	})
	return totalEntries, nil
}

func selectCheckpointTailOpsTx(tx *gorm.DB, libraryID string, baseClocks map[string]int64) ([]OplogEntry, error) {
	var rows []OplogEntry
	if err := tx.Where("library_id = ?", strings.TrimSpace(libraryID)).
		Order("tsns ASC, device_id ASC, seq ASC, op_id ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]OplogEntry, 0, len(rows))
	for _, row := range rows {
		if row.Seq <= baseClocks[strings.TrimSpace(row.DeviceID)] {
			continue
		}
		out = append(out, row)
	}
	return out, nil
}

func clearCheckpointManagedStateTx(tx *gorm.DB, libraryID string) error {
	models := []any{
		&PinBlobRef{},
		&PinMember{},
		&Artist{},
		&Credit{},
		&AlbumVariantModel{},
		&TrackVariantModel{},
		&AlbumTrack{},
		&DeviceVariantPreference{},
		&SourceFileModel{},
		&OptimizedAssetModel{},
		&DeviceAssetCacheModel{},
		&ArtworkVariant{},
		&PlaylistItem{},
		&Playlist{},
		&DeviceClock{},
		&OplogEntry{},
	}
	for _, model := range models {
		if err := tx.Where("library_id = ?", strings.TrimSpace(libraryID)).Delete(model).Error; err != nil {
			return err
		}
	}
	return nil
}

func insertCheckpointOpsTx(tx *gorm.DB, libraryID string, chunks []checkpointChunk) error {
	for _, chunk := range chunks {
		entries := append([]checkpointOplogEntry(nil), chunk.Entries...)
		sortCheckpointEntries(entries)
		for _, entry := range entries {
			if err := verifyCheckpointOplogEntryTx(tx, libraryID, entry); err != nil {
				return err
			}
			payloadJSON := "{}"
			if len(entry.PayloadJSON) > 0 {
				payloadJSON = string(entry.PayloadJSON)
			}
			if err := tx.Create(&OplogEntry{
				LibraryID:              strings.TrimSpace(libraryID),
				OpID:                   strings.TrimSpace(entry.OpID),
				DeviceID:               strings.TrimSpace(entry.DeviceID),
				Seq:                    entry.Seq,
				TSNS:                   entry.TSNS,
				EntityType:             strings.TrimSpace(entry.EntityType),
				EntityID:               strings.TrimSpace(entry.EntityID),
				OpKind:                 strings.TrimSpace(entry.OpKind),
				PayloadJSON:            payloadJSON,
				SignerPeerID:           strings.TrimSpace(entry.SignerPeerID),
				SignerAuthorityVersion: entry.SignerAuthorityVersion,
				SignerCertSerial:       entry.SignerCertSerial,
				SignerRole:             normalizeRole(entry.SignerRole),
				SignerIssuedAt:         entry.SignerIssuedAt,
				SignerExpiresAt:        entry.SignerExpiresAt,
				SignerCertSig:          append([]byte(nil), entry.SignerCertSig...),
				Sig:                    append([]byte(nil), entry.Sig...),
			}).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

func (a *SyncService) applyRemoteOps(ctx context.Context, libraryID string, ops []checkpointOplogEntry) (int, error) {
	applied, needsCatalogRebuild, err := a.applyRemoteOpsSummary(ctx, libraryID, ops)
	if err != nil {
		return applied, err
	}
	if needsCatalogRebuild {
		if err := a.rebuildCatalogMaterializationFull(ctx, libraryID, nil); err != nil {
			return applied, err
		}
	}
	return applied, nil
}

func (a *SyncService) applyRemoteOpsSummary(ctx context.Context, libraryID string, ops []checkpointOplogEntry) (int, bool, error) {
	applied := 0
	needsCatalogRebuild := false
	for _, op := range ops {
		inserted, err := a.applyRemoteOp(ctx, libraryID, op)
		applied += inserted
		if strings.TrimSpace(op.EntityType) == entityTypeSourceFile {
			needsCatalogRebuild = true
		}
		if err != nil {
			return applied, needsCatalogRebuild, err
		}
	}
	return applied, needsCatalogRebuild, nil
}

func (a *SyncService) applyRemoteOp(ctx context.Context, libraryID string, op checkpointOplogEntry) (int, error) {
	inserted := 0
	err := a.storage.Transaction(ctx, func(tx *gorm.DB) error {
		var existing int64
		if err := tx.Model(&OplogEntry{}).
			Where("library_id = ? AND op_id = ?", strings.TrimSpace(libraryID), strings.TrimSpace(op.OpID)).
			Count(&existing).Error; err != nil {
			return err
		}
		if existing > 0 {
			return nil
		}
		if err := verifyCheckpointOplogEntryTx(tx, libraryID, op); err != nil {
			return err
		}

		lastSeq, err := deviceClockSeqTx(tx, libraryID, op.DeviceID)
		if err != nil {
			return err
		}
		payloadJSON := "{}"
		if len(op.PayloadJSON) > 0 {
			payloadJSON = string(op.PayloadJSON)
		}
		entry := OplogEntry{
			LibraryID:              strings.TrimSpace(libraryID),
			OpID:                   strings.TrimSpace(op.OpID),
			DeviceID:               strings.TrimSpace(op.DeviceID),
			Seq:                    op.Seq,
			TSNS:                   op.TSNS,
			EntityType:             strings.TrimSpace(op.EntityType),
			EntityID:               strings.TrimSpace(op.EntityID),
			OpKind:                 strings.TrimSpace(op.OpKind),
			PayloadJSON:            payloadJSON,
			SignerPeerID:           strings.TrimSpace(op.SignerPeerID),
			SignerAuthorityVersion: op.SignerAuthorityVersion,
			SignerCertSerial:       op.SignerCertSerial,
			SignerRole:             normalizeRole(op.SignerRole),
			SignerIssuedAt:         op.SignerIssuedAt,
			SignerExpiresAt:        op.SignerExpiresAt,
			SignerCertSig:          append([]byte(nil), op.SignerCertSig...),
			Sig:                    append([]byte(nil), op.Sig...),
		}
		if err := tx.Create(&entry).Error; err != nil {
			return err
		}
		inserted = 1
		if entry.Seq != lastSeq+1 {
			return nil
		}
		_, err = applyBufferedDeviceOplogTx(tx, libraryID, entry.DeviceID, lastSeq+1)
		return err
	})
	return inserted, err
}

func deviceClockSeqTx(tx *gorm.DB, libraryID, deviceID string) (int64, error) {
	var row DeviceClock
	err := tx.Where("library_id = ? AND device_id = ?", strings.TrimSpace(libraryID), strings.TrimSpace(deviceID)).Take(&row).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return 0, nil
		}
		return 0, err
	}
	return row.LastSeqSeen, nil
}

func applyBufferedDeviceOplogTx(tx *gorm.DB, libraryID, deviceID string, nextSeq int64) (int64, error) {
	appliedUntil := nextSeq - 1
	for seq := nextSeq; ; seq++ {
		var row OplogEntry
		err := tx.Where("library_id = ? AND device_id = ? AND seq = ?", strings.TrimSpace(libraryID), strings.TrimSpace(deviceID), seq).Take(&row).Error
		if err != nil {
			if err == gorm.ErrRecordNotFound {
				break
			}
			return appliedUntil, err
		}
		if err := applyOplogEntryTx(tx, row); err != nil {
			return appliedUntil, err
		}
		appliedUntil = seq
	}
	if appliedUntil >= nextSeq {
		if err := upsertDeviceClockTx(tx, libraryID, deviceID, appliedUntil); err != nil {
			return nextSeq - 1, err
		}
	}
	return appliedUntil, nil
}

func upsertDeviceClockTx(tx *gorm.DB, libraryID, deviceID string, seq int64) error {
	return tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "library_id"},
			{Name: "device_id"},
		},
		DoUpdates: clause.Assignments(map[string]any{"last_seq_seen": seq}),
	}).Create(&DeviceClock{
		LibraryID:   strings.TrimSpace(libraryID),
		DeviceID:    strings.TrimSpace(deviceID),
		LastSeqSeen: seq,
	}).Error
}

func (a *SyncService) recordPeerSyncSuccess(ctx context.Context, libraryID, deviceID, peerID string, applied int64) {
	if applied == 0 {
		if existing, ok, err := a.peerSyncState(ctx, libraryID, deviceID); err == nil && ok && existing.LastApplied > 0 {
			applied = existing.LastApplied
		}
	}
	now := time.Now().UTC()
	a.upsertPeerSyncState(ctx, libraryID, deviceID, peerID, &now, &now, "", applied)
}

func (a *SyncService) recordPeerSyncFailure(ctx context.Context, libraryID, deviceID, peerID string, syncErr error) {
	if syncErr == nil {
		return
	}
	now := time.Now().UTC()
	a.upsertPeerSyncState(ctx, libraryID, deviceID, peerID, &now, nil, syncErr.Error(), 0)
	a.recordNetworkDebug(apitypes.NetworkDebugTraceEntry{
		Level:     "error",
		Kind:      "sync.catchup.failed",
		Message:   "Peer catch-up failed",
		LibraryID: libraryID,
		DeviceID:  deviceID,
		PeerID:    peerID,
		Error:     syncErr.Error(),
	})
}

func (a *SyncService) upsertPeerSyncState(ctx context.Context, libraryID, deviceID, peerID string, attemptedAt, successAt *time.Time, lastError string, applied int64) {
	libraryID = strings.TrimSpace(libraryID)
	deviceID = strings.TrimSpace(deviceID)
	peerID = strings.TrimSpace(peerID)
	if libraryID == "" || deviceID == "" {
		return
	}
	row := PeerSyncState{
		LibraryID:     libraryID,
		DeviceID:      deviceID,
		PeerID:        peerID,
		LastAttemptAt: cloneTimePtr(attemptedAt),
		LastSuccessAt: cloneTimePtr(successAt),
		LastError:     strings.TrimSpace(lastError),
		LastApplied:   applied,
		UpdatedAt:     time.Now().UTC(),
	}
	_ = a.storage.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "library_id"},
			{Name: "device_id"},
		},
		DoUpdates: clause.Assignments(map[string]any{
			"peer_id":         row.PeerID,
			"last_attempt_at": row.LastAttemptAt,
			"last_success_at": row.LastSuccessAt,
			"last_error":      row.LastError,
			"last_applied":    row.LastApplied,
			"updated_at":      row.UpdatedAt,
		}),
	}).Create(&row).Error
}

func (a *SyncService) isLibraryMember(ctx context.Context, libraryID, deviceID string) bool {
	var count int64
	if err := a.storage.WithContext(ctx).Model(&Membership{}).
		Where("library_id = ? AND device_id = ?", strings.TrimSpace(libraryID), strings.TrimSpace(deviceID)).
		Count(&count).Error; err != nil {
		return false
	}
	return count > 0
}

func (a *SyncService) peerSyncState(ctx context.Context, libraryID, deviceID string) (PeerSyncState, bool, error) {
	var row PeerSyncState
	err := a.storage.WithContext(ctx).
		Where("library_id = ? AND device_id = ?", strings.TrimSpace(libraryID), strings.TrimSpace(deviceID)).
		Take(&row).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return PeerSyncState{}, false, nil
		}
		return PeerSyncState{}, false, err
	}
	return row, true, nil
}
