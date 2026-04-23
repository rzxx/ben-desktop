package desktopcore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	apitypes "ben/desktop/api/types"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type remotePlaybackCandidate struct {
	peer       SyncPeer
	deviceID   string
	peerID     string
	cached     bool
	lastSeenAt *time.Time
}

func (s *PlaybackService) ensureRemotePlaybackRecording(ctx context.Context, local apitypes.LocalContext, recordingID, profile string) (apitypes.PlaybackRecordingResult, bool, error) {
	local, err := s.app.ensureLocalPeerContext(ctx, local)
	if err != nil {
		return apitypes.PlaybackRecordingResult{}, false, err
	}
	candidate, state, err := s.selectRemotePlaybackCandidate(ctx, local, recordingID, profile)
	if err != nil {
		return apitypes.PlaybackRecordingResult{}, false, err
	}
	if candidate.peer == nil {
		return apitypes.PlaybackRecordingResult{}, false, nil
	}

	auth, err := s.app.ensureLocalTransportMembershipAuth(ctx, local, local.PeerID)
	if err != nil {
		return apitypes.PlaybackRecordingResult{}, false, fmt.Errorf("build playback asset auth: %w", err)
	}
	resp, err := candidate.peer.FetchPlaybackAsset(ctx, PlaybackAssetRequest{
		LibraryID:        local.LibraryID,
		DeviceID:         local.DeviceID,
		PeerID:           local.PeerID,
		RecordingID:      strings.TrimSpace(recordingID),
		PreferredProfile: strings.TrimSpace(profile),
		Auth:             auth,
	})
	if err != nil {
		return apitypes.PlaybackRecordingResult{}, false, err
	}
	if _, err := s.app.verifyTransportPeerAuth(ctx, local.LibraryID, resp.DeviceID, resp.PeerID, firstNonEmpty(candidate.peer.PeerID(), candidate.peerID), resp.Auth); err != nil {
		return apitypes.PlaybackRecordingResult{}, false, err
	}
	_ = s.app.updateDevicePeerID(ctx, local.LibraryID, firstNonEmpty(resp.DeviceID, candidate.deviceID), firstNonEmpty(resp.PeerID, candidate.peerID), firstNonEmpty(resp.DeviceID, candidate.deviceID))

	asset, err := s.storeFetchedPlaybackAsset(ctx, local, resp.Asset)
	if err != nil {
		return apitypes.PlaybackRecordingResult{}, false, err
	}

	path, err := s.pathForBlob(asset.BlobID)
	if err != nil {
		return apitypes.PlaybackRecordingResult{}, false, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return apitypes.PlaybackRecordingResult{}, false, err
	}

	sourceKind := apitypes.PlaybackSourceRemoteOpt
	if state == apitypes.AvailabilityPlayableCachedOpt {
		sourceKind = apitypes.PlaybackSourceCachedOpt
	}
	return apitypes.PlaybackRecordingResult{
		EncodingID: asset.OptimizedAssetID,
		BlobID:     asset.BlobID,
		Profile:    strings.TrimSpace(asset.Profile),
		Bitrate:    asset.Bitrate,
		Bytes:      int(info.Size()),
		FromLocal:  false,
		SourceKind: sourceKind,
	}, true, nil
}

func (s *PlaybackService) selectRemotePlaybackCandidate(ctx context.Context, local apitypes.LocalContext, recordingID, profile string) (remotePlaybackCandidate, apitypes.RecordingAvailabilityState, error) {
	transport := s.app.activeSyncTransport()
	if transport == nil {
		return remotePlaybackCandidate{}, "", nil
	}
	peers, err := transport.ListPeers(ctx, local)
	if err != nil {
		return remotePlaybackCandidate{}, "", err
	}
	if len(peers) == 0 {
		return remotePlaybackCandidate{}, "", nil
	}
	peerByDeviceID := make(map[string]SyncPeer, len(peers))
	peerByPeerID := make(map[string]SyncPeer, len(peers))
	for _, peer := range peers {
		if peer == nil {
			continue
		}
		if deviceID := strings.TrimSpace(peer.DeviceID()); deviceID != "" {
			peerByDeviceID[deviceID] = peer
		}
		if peerID := strings.TrimSpace(peer.PeerID()); peerID != "" {
			peerByPeerID[peerID] = peer
		}
	}

	items, err := s.ListRecordingAvailability(ctx, recordingID, profile)
	if err != nil {
		return remotePlaybackCandidate{}, "", err
	}
	cached := make([]remotePlaybackCandidate, 0, len(items))
	providers := make([]remotePlaybackCandidate, 0, len(items))
	cutoff := time.Now().UTC().Add(-availabilityOnlineWindow)
	for _, item := range items {
		if strings.TrimSpace(item.DeviceID) == "" || item.DeviceID == local.DeviceID {
			continue
		}
		peer := peerByDeviceID[strings.TrimSpace(item.DeviceID)]
		if peer == nil && strings.TrimSpace(item.PeerID) != "" {
			peer = peerByPeerID[strings.TrimSpace(item.PeerID)]
		}
		if peer == nil {
			continue
		}
		if item.LastSeenAt == nil || !item.LastSeenAt.UTC().After(cutoff) {
			continue
		}
		candidate := remotePlaybackCandidate{
			peer:       peer,
			deviceID:   strings.TrimSpace(item.DeviceID),
			peerID:     strings.TrimSpace(item.PeerID),
			lastSeenAt: cloneTimePtr(item.LastSeenAt),
		}
		if item.CachedOptimized {
			candidate.cached = true
			cached = append(cached, candidate)
		}
		if item.SourcePresent && canProvideLocalMedia(item.Role) {
			providers = append(providers, candidate)
		}
	}
	sortRemotePlaybackCandidates(cached)
	sortRemotePlaybackCandidates(providers)
	if len(cached) > 0 {
		return cached[0], apitypes.AvailabilityPlayableRemoteOpt, nil
	}
	if len(providers) > 0 {
		return providers[0], apitypes.AvailabilityWaitingProviderTranscode, nil
	}
	return remotePlaybackCandidate{}, "", nil
}

func sortRemotePlaybackCandidates(items []remotePlaybackCandidate) {
	sort.Slice(items, func(i, j int) bool {
		leftSeen := time.Time{}
		if items[i].lastSeenAt != nil {
			leftSeen = items[i].lastSeenAt.UTC()
		}
		rightSeen := time.Time{}
		if items[j].lastSeenAt != nil {
			rightSeen = items[j].lastSeenAt.UTC()
		}
		if !leftSeen.Equal(rightSeen) {
			return leftSeen.After(rightSeen)
		}
		if items[i].deviceID != items[j].deviceID {
			return items[i].deviceID < items[j].deviceID
		}
		return items[i].peerID < items[j].peerID
	})
}

func (s *PlaybackService) storeFetchedPlaybackAsset(ctx context.Context, local apitypes.LocalContext, transfer PlaybackAssetTransfer) (OptimizedAssetModel, error) {
	if strings.TrimSpace(transfer.OptimizedAssetID) == "" {
		return OptimizedAssetModel{}, fmt.Errorf("remote playback asset id is required")
	}
	if strings.TrimSpace(transfer.BlobID) == "" {
		return OptimizedAssetModel{}, fmt.Errorf("remote playback blob id is required")
	}
	if len(transfer.Data) == 0 {
		return OptimizedAssetModel{}, fmt.Errorf("remote playback data is required")
	}

	if err := verifyBlobIDBytes(transfer.BlobID, transfer.Data); err != nil {
		return OptimizedAssetModel{}, fmt.Errorf("remote playback %w", err)
	}
	storedBlobID, err := s.app.transcode.storeBlobBytes(transfer.Data)
	if err != nil {
		return OptimizedAssetModel{}, err
	}
	if strings.TrimSpace(storedBlobID) != strings.TrimSpace(transfer.BlobID) {
		return OptimizedAssetModel{}, fmt.Errorf("remote playback blob hash mismatch")
	}

	now := time.Now().UTC()
	asset := OptimizedAssetModel{
		LibraryID:         local.LibraryID,
		OptimizedAssetID:  strings.TrimSpace(transfer.OptimizedAssetID),
		SourceFileID:      strings.TrimSpace(transfer.SourceFileID),
		TrackVariantID:    strings.TrimSpace(transfer.TrackVariantID),
		Profile:           strings.TrimSpace(transfer.Profile),
		BlobID:            strings.TrimSpace(transfer.BlobID),
		MIME:              strings.TrimSpace(transfer.MIME),
		DurationMS:        transfer.DurationMS,
		Bitrate:           transfer.Bitrate,
		Codec:             strings.TrimSpace(transfer.Codec),
		Container:         strings.TrimSpace(transfer.Container),
		CreatedByDeviceID: strings.TrimSpace(transfer.CreatedByDeviceID),
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if err := s.app.storage.Transaction(ctx, func(tx *gorm.DB) error {
		if err := mirrorOptimizedAssetTx(tx, asset); err != nil {
			return err
		}
		lastVerified := now
		return s.app.upsertDeviceAssetCacheTx(tx, local, DeviceAssetCacheModel{
			LibraryID:        local.LibraryID,
			DeviceID:         local.DeviceID,
			OptimizedAssetID: asset.OptimizedAssetID,
			IsCached:         true,
			LastVerifiedAt:   &lastVerified,
			UpdatedAt:        now,
		})
	}); err != nil {
		return OptimizedAssetModel{}, err
	}
	return asset, nil
}

func mirrorOptimizedAssetTx(tx *gorm.DB, row OptimizedAssetModel) error {
	now := time.Now().UTC()
	row.LibraryID = strings.TrimSpace(row.LibraryID)
	row.OptimizedAssetID = strings.TrimSpace(row.OptimizedAssetID)
	row.SourceFileID = strings.TrimSpace(row.SourceFileID)
	row.TrackVariantID = strings.TrimSpace(row.TrackVariantID)
	row.Profile = strings.TrimSpace(row.Profile)
	row.BlobID = strings.TrimSpace(row.BlobID)
	row.MIME = strings.TrimSpace(row.MIME)
	row.Codec = strings.TrimSpace(row.Codec)
	row.Container = strings.TrimSpace(row.Container)
	row.CreatedByDeviceID = strings.TrimSpace(row.CreatedByDeviceID)
	if row.CreatedAt.IsZero() {
		row.CreatedAt = now
	}
	if row.UpdatedAt.IsZero() {
		row.UpdatedAt = now
	}
	return tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "library_id"},
			{Name: "optimized_asset_id"},
		},
		DoUpdates: clause.AssignmentColumns([]string{"source_file_id", "track_variant_id", "profile", "blob_id", "mime", "duration_ms", "bitrate", "codec", "container", "created_by_device_id", "updated_at"}),
	}).Create(&row).Error
}

func (a *App) buildPlaybackAssetResponse(ctx context.Context, req PlaybackAssetRequest) (PlaybackAssetResponse, error) {
	local, err := a.requireActiveContext(ctx)
	if err != nil {
		return PlaybackAssetResponse{}, err
	}
	local, err = a.ensureLocalPeerContext(ctx, local)
	if err != nil {
		return PlaybackAssetResponse{}, err
	}
	if strings.TrimSpace(req.LibraryID) != strings.TrimSpace(local.LibraryID) {
		return PlaybackAssetResponse{}, fmt.Errorf("remote library mismatch")
	}

	asset, err := a.playback.resolvePlaybackAssetTransfer(
		ctx,
		local,
		strings.TrimSpace(req.RecordingID),
		strings.TrimSpace(req.PreferredProfile),
		strings.TrimSpace(req.DeviceID),
	)
	if err != nil {
		return PlaybackAssetResponse{}, err
	}
	auth, err := a.ensureLocalTransportMembershipAuth(ctx, local, local.PeerID)
	if err != nil {
		return PlaybackAssetResponse{}, fmt.Errorf("build local transport auth: %w", err)
	}
	return PlaybackAssetResponse{
		LibraryID: local.LibraryID,
		DeviceID:  local.DeviceID,
		PeerID:    local.PeerID,
		Auth:      auth,
		Asset:     asset,
	}, nil
}

func (s *PlaybackService) resolvePlaybackAssetTransfer(ctx context.Context, local apitypes.LocalContext, recordingID, preferredProfile, requesterDeviceID string) (PlaybackAssetTransfer, error) {
	if strings.TrimSpace(recordingID) == "" {
		return PlaybackAssetTransfer{}, fmt.Errorf("recording id is required")
	}
	profile := s.resolvePlaybackProfile(preferredProfile)
	recordingRef := strings.TrimSpace(recordingID)
	clusterID, ok, err := s.app.catalog.trackClusterIDForVariant(ctx, local.LibraryID, recordingRef)
	if err != nil {
		return PlaybackAssetTransfer{}, err
	}
	if ok && strings.TrimSpace(clusterID) != "" {
		if providerRecordingID, providerOK, providerErr := s.app.catalog.explicitRecordingVariantID(ctx, local.LibraryID, local.DeviceID, strings.TrimSpace(clusterID)); providerErr != nil {
			return PlaybackAssetTransfer{}, providerErr
		} else if providerOK && strings.TrimSpace(providerRecordingID) != "" {
			recordingRef = strings.TrimSpace(providerRecordingID)
		}
	}

	blobID, encodingID, ok, err := s.bestCachedEncoding(ctx, local.LibraryID, local.DeviceID, recordingRef, profile)
	if err != nil {
		return PlaybackAssetTransfer{}, err
	}
	if !ok && canProvideLocalMedia(local.Role) {
		if _, err := s.app.transcode.EnsureRecordingEncoding(ctx, local, recordingRef, profile, requesterDeviceID); err != nil && !errors.Is(err, ErrProviderOnlyTranscode) {
			return PlaybackAssetTransfer{}, err
		}
		blobID, encodingID, ok, err = s.bestCachedEncoding(ctx, local.LibraryID, local.DeviceID, recordingRef, profile)
		if err != nil {
			return PlaybackAssetTransfer{}, err
		}
	}
	if !ok {
		return PlaybackAssetTransfer{}, fmt.Errorf("playback asset not available")
	}

	var asset OptimizedAssetModel
	if err := s.app.storage.WithContext(ctx).
		Where("library_id = ? AND optimized_asset_id = ?", local.LibraryID, encodingID).
		Take(&asset).Error; err != nil {
		return PlaybackAssetTransfer{}, err
	}
	data, err := s.app.transcode.readVerifiedBlob(blobID)
	if err != nil {
		return PlaybackAssetTransfer{}, err
	}

	return PlaybackAssetTransfer{
		OptimizedAssetID:  strings.TrimSpace(asset.OptimizedAssetID),
		SourceFileID:      strings.TrimSpace(asset.SourceFileID),
		TrackVariantID:    strings.TrimSpace(asset.TrackVariantID),
		Profile:           strings.TrimSpace(asset.Profile),
		BlobID:            strings.TrimSpace(asset.BlobID),
		MIME:              strings.TrimSpace(asset.MIME),
		DurationMS:        asset.DurationMS,
		Bitrate:           asset.Bitrate,
		Codec:             strings.TrimSpace(asset.Codec),
		Container:         strings.TrimSpace(asset.Container),
		CreatedByDeviceID: strings.TrimSpace(asset.CreatedByDeviceID),
		Data:              data,
	}, nil
}
