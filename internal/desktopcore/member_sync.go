package desktopcore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	apitypes "ben/desktop/api/types"
	"gorm.io/gorm"
)

const (
	memberProjectionPreferredProfile = "mobile_v1_96k"
	memberCheckpointChunkSize        = 1000
	memberCheckpointStatusPublished  = "published"
)

type MemberSyncService struct {
	app *App
}

type MemberSyncRequest struct {
	LibraryID             string
	DeviceID              string
	PeerID                string
	Auth                  transportPeerAuth
	LastVersion           int64
	InstalledCheckpointID string
	MaxOps                int
}

type MemberSyncResponse struct {
	LibraryID      string                    `json:"libraryId"`
	DeviceID       string                    `json:"deviceId"`
	PeerID         string                    `json:"peerId"`
	Auth           transportPeerAuth         `json:"auth"`
	Ops            []memberProjectionEntry   `json:"ops,omitempty"`
	HasMore        bool                      `json:"hasMore"`
	RemainingOps   int64                     `json:"remainingOps"`
	NeedCheckpoint bool                      `json:"needCheckpoint"`
	Checkpoint     *memberCheckpointManifest `json:"checkpoint,omitempty"`
	LatestVersion  int64                     `json:"latestVersion"`
}

type MemberCheckpointFetchRequest struct {
	LibraryID    string
	CheckpointID string
	Auth         transportPeerAuth
}

type MemberCheckpointFetchResponse struct {
	Record memberCheckpointTransferRecord `json:"record"`
	Auth   transportPeerAuth              `json:"auth"`
	Error  string                         `json:"error,omitempty"`
}

type memberProjectionEntry struct {
	Version     int64           `json:"version"`
	TSNS        int64           `json:"tsns"`
	EntityType  string          `json:"entityType"`
	EntityID    string          `json:"entityId"`
	OpKind      string          `json:"opKind"`
	PayloadJSON json.RawMessage `json:"payloadJson,omitempty"`
}

type memberCheckpointManifest struct {
	LibraryID      string     `json:"libraryId"`
	TargetDeviceID string     `json:"targetDeviceId"`
	CheckpointID   string     `json:"checkpointId"`
	BaseVersion    int64      `json:"baseVersion"`
	CreatedAt      time.Time  `json:"createdAt"`
	ChunkCount     int        `json:"chunkCount"`
	EntryCount     int        `json:"entryCount"`
	ContentHash    string     `json:"contentHash"`
	Status         string     `json:"status"`
	PublishedAt    *time.Time `json:"publishedAt,omitempty"`
}

type memberCheckpointChunk struct {
	ChunkIndex  int                     `json:"chunkIndex"`
	EntryCount  int                     `json:"entryCount"`
	ContentHash string                  `json:"contentHash"`
	Entries     []memberProjectionEntry `json:"entries"`
}

type memberCheckpointTransferRecord struct {
	Manifest memberCheckpointManifest `json:"manifest"`
	Chunks   []memberCheckpointChunk  `json:"chunks"`
}

type memberSyncBatch struct {
	Ops          []memberProjectionEntry
	HasMore      bool
	RemainingOps int64
	TotalMissing int64
}

type memberProjectionSnapshotRow struct {
	EntityType  string
	EntityID    string
	PayloadJSON string
	ContentHash string
}

func newMemberSyncService(app *App) *MemberSyncService {
	return &MemberSyncService{app: app}
}

func (s *MemberSyncService) buildSyncResponse(ctx context.Context, req MemberSyncRequest) (MemberSyncResponse, error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return MemberSyncResponse{}, err
	}
	local, err = s.app.ensureLocalPeerContext(ctx, local)
	if err != nil {
		return MemberSyncResponse{}, err
	}
	if strings.TrimSpace(req.LibraryID) != strings.TrimSpace(local.LibraryID) {
		return MemberSyncResponse{}, fmt.Errorf("remote library mismatch")
	}

	targetDeviceID := strings.TrimSpace(req.DeviceID)
	if targetDeviceID == "" {
		return MemberSyncResponse{}, fmt.Errorf("device id is required")
	}

	meta, err := s.ensureProjectionCurrent(ctx, req.LibraryID, targetDeviceID)
	if err != nil {
		return MemberSyncResponse{}, err
	}

	lastVersion := req.LastVersion
	published, hasPublished, err := s.loadPublishedCheckpointRecord(ctx, req.LibraryID, targetDeviceID)
	if err != nil {
		return MemberSyncResponse{}, err
	}
	if hasPublished &&
		strings.TrimSpace(req.InstalledCheckpointID) == strings.TrimSpace(published.Manifest.CheckpointID) &&
		lastVersion < published.Manifest.BaseVersion {
		lastVersion = published.Manifest.BaseVersion
	}

	batch, err := s.selectSyncBatch(ctx, req.LibraryID, targetDeviceID, lastVersion, req.MaxOps, meta.LastVersion)
	if err != nil {
		return MemberSyncResponse{}, err
	}

	resp := MemberSyncResponse{
		LibraryID:     req.LibraryID,
		DeviceID:      local.DeviceID,
		PeerID:        local.PeerID,
		Ops:           batch.Ops,
		HasMore:       batch.HasMore,
		RemainingOps:  batch.RemainingOps,
		LatestVersion: meta.LastVersion,
	}
	auth, err := s.app.ensureLocalTransportMembershipAuth(ctx, local, local.PeerID)
	if err != nil {
		return MemberSyncResponse{}, fmt.Errorf("build local transport auth: %w", err)
	}
	resp.Auth = auth

	needCheckpoint := hasPublished && lastVersion < published.Manifest.BaseVersion
	if !needCheckpoint && batch.TotalMissing >= incrementalSyncBacklogCutover {
		if !hasPublished || meta.LastVersion-published.Manifest.BaseVersion >= incrementalSyncBacklogCutover {
			published, err = s.publishCheckpointForTargetDevice(ctx, req.LibraryID, targetDeviceID)
			if err != nil {
				return MemberSyncResponse{}, err
			}
			hasPublished = true
		}
		needCheckpoint = hasPublished
	}
	if needCheckpoint && hasPublished {
		resp.Ops = nil
		resp.HasMore = true
		resp.RemainingOps = 0
		resp.NeedCheckpoint = true
		resp.Checkpoint = &published.Manifest
	}
	return resp, nil
}

func (s *MemberSyncService) buildCheckpointFetchResponse(ctx context.Context, req MemberCheckpointFetchRequest) (MemberCheckpointFetchResponse, error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return MemberCheckpointFetchResponse{}, err
	}
	local, err = s.app.ensureLocalPeerContext(ctx, local)
	if err != nil {
		return MemberCheckpointFetchResponse{}, err
	}
	if strings.TrimSpace(req.LibraryID) != strings.TrimSpace(local.LibraryID) {
		return MemberCheckpointFetchResponse{}, fmt.Errorf("remote library mismatch")
	}

	targetDeviceID := strings.TrimSpace(req.Auth.Cert.DeviceID)
	if targetDeviceID == "" {
		return MemberCheckpointFetchResponse{}, fmt.Errorf("device id is required")
	}
	record, ok, err := s.loadCheckpointRecord(ctx, req.LibraryID, targetDeviceID, req.CheckpointID, false)
	if err != nil {
		return MemberCheckpointFetchResponse{}, err
	}
	if !ok {
		return MemberCheckpointFetchResponse{}, fmt.Errorf("checkpoint not found")
	}
	auth, err := s.app.ensureLocalTransportMembershipAuth(ctx, local, local.PeerID)
	if err != nil {
		return MemberCheckpointFetchResponse{}, fmt.Errorf("build local transport auth: %w", err)
	}
	return MemberCheckpointFetchResponse{Record: record, Auth: auth}, nil
}

func (s *MemberSyncService) backgroundCheckpointMaintenance(ctx context.Context, libraryID string) error {
	libraryID = strings.TrimSpace(libraryID)
	if libraryID == "" {
		return nil
	}

	deviceIDs, err := s.listMemberDeviceIDs(ctx, libraryID)
	if err != nil {
		return err
	}
	for _, deviceID := range deviceIDs {
		meta, err := s.ensureProjectionCurrent(ctx, libraryID, deviceID)
		if err != nil {
			return err
		}
		if meta.LastVersion == 0 {
			continue
		}
		record, ok, err := s.loadPublishedCheckpointRecord(ctx, libraryID, deviceID)
		if err != nil {
			return err
		}
		if !ok || meta.LastVersion-record.Manifest.BaseVersion >= incrementalSyncBacklogCutover {
			if _, err := s.publishCheckpointForTargetDevice(ctx, libraryID, deviceID); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *MemberSyncService) ensureProjectionCurrent(ctx context.Context, libraryID, targetDeviceID string) (MemberProjectionMeta, error) {
	snapshot, err := s.buildProjectionSnapshot(ctx, libraryID, targetDeviceID)
	if err != nil {
		return MemberProjectionMeta{}, err
	}
	sort.Slice(snapshot, func(i, j int) bool {
		if snapshot[i].EntityType == snapshot[j].EntityType {
			return snapshot[i].EntityID < snapshot[j].EntityID
		}
		return snapshot[i].EntityType < snapshot[j].EntityType
	})

	var meta MemberProjectionMeta
	err = s.app.storage.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("library_id = ? AND target_device_id = ?", strings.TrimSpace(libraryID), strings.TrimSpace(targetDeviceID)).
			Take(&meta).Error; err != nil {
			if err != gorm.ErrRecordNotFound {
				return err
			}
			meta = MemberProjectionMeta{
				LibraryID:      strings.TrimSpace(libraryID),
				TargetDeviceID: strings.TrimSpace(targetDeviceID),
			}
		}

		var existingRows []MemberProjectionState
		if err := tx.Where("library_id = ? AND target_device_id = ?", strings.TrimSpace(libraryID), strings.TrimSpace(targetDeviceID)).
			Find(&existingRows).Error; err != nil {
			return err
		}
		existingByKey := make(map[string]MemberProjectionState, len(existingRows))
		for _, row := range existingRows {
			existingByKey[memberProjectionKey(row.EntityType, row.EntityID)] = row
		}

		now := time.Now().UTC()
		opOffset := int64(0)
		for _, row := range snapshot {
			key := memberProjectionKey(row.EntityType, row.EntityID)
			if existing, ok := existingByKey[key]; ok &&
				strings.TrimSpace(existing.ContentHash) == strings.TrimSpace(row.ContentHash) &&
				strings.TrimSpace(existing.PayloadJSON) == strings.TrimSpace(row.PayloadJSON) {
				delete(existingByKey, key)
				continue
			}

			meta.LastVersion++
			opOffset++
			if err := tx.Save(&MemberProjectionState{
				LibraryID:      strings.TrimSpace(libraryID),
				TargetDeviceID: strings.TrimSpace(targetDeviceID),
				EntityType:     strings.TrimSpace(row.EntityType),
				EntityID:       strings.TrimSpace(row.EntityID),
				PayloadJSON:    defaultProjectionPayload(row.PayloadJSON),
				ContentHash:    strings.TrimSpace(row.ContentHash),
				UpdatedAt:      now,
			}).Error; err != nil {
				return err
			}
			if err := tx.Create(&MemberProjectionOp{
				LibraryID:      strings.TrimSpace(libraryID),
				TargetDeviceID: strings.TrimSpace(targetDeviceID),
				Version:        meta.LastVersion,
				TSNS:           now.UnixNano() + opOffset,
				EntityType:     strings.TrimSpace(row.EntityType),
				EntityID:       strings.TrimSpace(row.EntityID),
				OpKind:         "upsert",
				PayloadJSON:    defaultProjectionPayload(row.PayloadJSON),
				ContentHash:    strings.TrimSpace(row.ContentHash),
			}).Error; err != nil {
				return err
			}
			delete(existingByKey, key)
		}

		deletedKeys := make([]string, 0, len(existingByKey))
		for key := range existingByKey {
			deletedKeys = append(deletedKeys, key)
		}
		sort.Strings(deletedKeys)
		for _, key := range deletedKeys {
			existing := existingByKey[key]
			meta.LastVersion++
			opOffset++
			if err := tx.Create(&MemberProjectionOp{
				LibraryID:      strings.TrimSpace(libraryID),
				TargetDeviceID: strings.TrimSpace(targetDeviceID),
				Version:        meta.LastVersion,
				TSNS:           now.UnixNano() + opOffset,
				EntityType:     strings.TrimSpace(existing.EntityType),
				EntityID:       strings.TrimSpace(existing.EntityID),
				OpKind:         "delete",
				PayloadJSON:    defaultProjectionPayload(existing.PayloadJSON),
				ContentHash:    strings.TrimSpace(existing.ContentHash),
			}).Error; err != nil {
				return err
			}
			if err := tx.Where("library_id = ? AND target_device_id = ? AND entity_type = ? AND entity_id = ?",
				libraryID, targetDeviceID, existing.EntityType, existing.EntityID).
				Delete(&MemberProjectionState{}).Error; err != nil {
				return err
			}
		}

		meta.UpdatedAt = now
		return tx.Save(&meta).Error
	})
	if err != nil {
		return MemberProjectionMeta{}, err
	}
	return meta, nil
}

func (s *MemberSyncService) buildProjectionSnapshot(ctx context.Context, libraryID, targetDeviceID string) ([]memberProjectionSnapshotRow, error) {
	role := firstNonEmpty(s.app.sync.membershipRole(ctx, libraryID, targetDeviceID), roleMember)
	out := make([]memberProjectionSnapshotRow, 0, 64)

	libraryRow, err := s.buildLibrarySnapshot(ctx, libraryID, targetDeviceID)
	if err != nil {
		return nil, err
	}
	out = append(out, libraryRow)

	memberRows, err := s.buildLibraryMemberSnapshots(ctx, libraryID)
	if err != nil {
		return nil, err
	}
	out = append(out, memberRows...)

	artistRows, err := s.buildArtistSnapshots(ctx, libraryID)
	if err != nil {
		return nil, err
	}
	out = append(out, artistRows...)

	albumRows, preferredAlbumByID, albumIDs, err := s.buildAlbumSnapshots(ctx, libraryID, targetDeviceID)
	if err != nil {
		return nil, err
	}
	out = append(out, albumRows...)

	recordingRows, recordingIDs, err := s.buildRecordingSnapshots(ctx, libraryID, targetDeviceID)
	if err != nil {
		return nil, err
	}
	out = append(out, recordingRows...)

	local := apitypes.LocalContext{
		LibraryID: libraryID,
		DeviceID:  targetDeviceID,
		Role:      role,
	}
	albumAvailabilityRows, err := s.buildAlbumAvailabilitySnapshots(ctx, local, albumIDs, preferredAlbumByID)
	if err != nil {
		return nil, err
	}
	out = append(out, albumAvailabilityRows...)

	recordingAvailabilityRows, err := s.buildRecordingAvailabilitySnapshots(ctx, local, recordingIDs)
	if err != nil {
		return nil, err
	}
	out = append(out, recordingAvailabilityRows...)

	playlistRows, playlistIDs, err := s.buildPlaylistSnapshots(ctx, libraryID)
	if err != nil {
		return nil, err
	}
	out = append(out, playlistRows...)

	playlistTrackRows, err := s.buildPlaylistTrackSnapshots(ctx, libraryID, targetDeviceID, playlistIDs)
	if err != nil {
		return nil, err
	}
	out = append(out, playlistTrackRows...)

	likedRows, err := s.buildLikedRecordingSnapshots(ctx, libraryID, targetDeviceID)
	if err != nil {
		return nil, err
	}
	out = append(out, likedRows...)

	return out, nil
}

func (s *MemberSyncService) buildLibrarySnapshot(ctx context.Context, libraryID, targetDeviceID string) (memberProjectionSnapshotRow, error) {
	type libraryRow struct {
		Name     string
		Role     string
		JoinedAt time.Time
	}
	var result libraryRow
	query := `
SELECT
	l.name,
	m.role,
	m.joined_at
FROM libraries l
JOIN memberships m ON m.library_id = l.library_id
WHERE l.library_id = ? AND m.device_id = ?
LIMIT 1`
	if err := s.app.storage.WithContext(ctx).Raw(query, strings.TrimSpace(libraryID), strings.TrimSpace(targetDeviceID)).Scan(&result).Error; err != nil {
		return memberProjectionSnapshotRow{}, err
	}
	payload := map[string]any{
		"library_id": libraryID,
		"name":       strings.TrimSpace(result.Name),
		"role":       normalizeRole(result.Role),
		"joined_at":  result.JoinedAt.UTC().Format(time.RFC3339Nano),
		"is_active":  false,
	}
	return newMemberProjectionSnapshotRow("library", libraryID, payload)
}

func (s *MemberSyncService) buildLibraryMemberSnapshots(ctx context.Context, libraryID string) ([]memberProjectionSnapshotRow, error) {
	type row struct {
		DeviceID      string
		Role          string
		PeerID        string
		LastSeenAt    *time.Time
		LastAttemptAt *time.Time
		LastSuccessAt *time.Time
		LastError     string
		LastApplied   int64
	}
	query := `
SELECT
	m.device_id,
	m.role,
	COALESCE(d.peer_id, '') AS peer_id,
	d.last_seen_at,
	ps.last_attempt_at,
	ps.last_success_at,
	COALESCE(ps.last_error, '') AS last_error,
	COALESCE(ps.last_applied, 0) AS last_applied
FROM memberships m
LEFT JOIN devices d ON d.device_id = m.device_id
LEFT JOIN peer_sync_states ps ON ps.library_id = m.library_id AND ps.device_id = m.device_id
WHERE m.library_id = ?
ORDER BY m.device_id ASC`
	var rows []row
	if err := s.app.storage.WithContext(ctx).Raw(query, strings.TrimSpace(libraryID)).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]memberProjectionSnapshotRow, 0, len(rows))
	for _, row := range rows {
		item, err := newMemberProjectionSnapshotRow("library_member", strings.TrimSpace(row.DeviceID), map[string]any{
			"library_id":           libraryID,
			"device_id":            strings.TrimSpace(row.DeviceID),
			"role":                 normalizeRole(row.Role),
			"peer_id":              strings.TrimSpace(row.PeerID),
			"last_seen_at":         nullableTimeString(row.LastSeenAt),
			"last_seq":             row.LastApplied,
			"last_sync_attempt_at": nullableTimeString(row.LastAttemptAt),
			"last_sync_success_at": nullableTimeString(row.LastSuccessAt),
			"last_sync_error":      strings.TrimSpace(row.LastError),
		})
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

func (s *MemberSyncService) buildArtistSnapshots(ctx context.Context, libraryID string) ([]memberProjectionSnapshotRow, error) {
	type row struct {
		ArtistID   string
		Name       string
		AlbumCount int64
		TrackCount int64
	}
	query := `
SELECT
	a.artist_id,
	a.name,
	COUNT(DISTINCT CASE WHEN ac.entity_type = 'album' THEN ac.entity_id END) AS album_count,
	COUNT(DISTINCT CASE WHEN tc.entity_type = 'track' THEN tc.entity_id END) AS track_count
FROM artists a
LEFT JOIN credits ac ON ac.library_id = a.library_id AND ac.artist_id = a.artist_id AND ac.entity_type = 'album'
LEFT JOIN credits tc ON tc.library_id = a.library_id AND tc.artist_id = a.artist_id AND tc.entity_type = 'track'
WHERE a.library_id = ?
GROUP BY a.artist_id, a.name
ORDER BY LOWER(a.name) ASC, a.artist_id ASC`
	var rows []row
	if err := s.app.storage.WithContext(ctx).Raw(query, strings.TrimSpace(libraryID)).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]memberProjectionSnapshotRow, 0, len(rows))
	for _, row := range rows {
		item, err := newMemberProjectionSnapshotRow("artist", strings.TrimSpace(row.ArtistID), map[string]any{
			"library_id":  libraryID,
			"artist_id":   strings.TrimSpace(row.ArtistID),
			"name":        strings.TrimSpace(row.Name),
			"album_count": row.AlbumCount,
			"track_count": row.TrackCount,
		})
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

func (s *MemberSyncService) buildAlbumSnapshots(ctx context.Context, libraryID, targetDeviceID string) ([]memberProjectionSnapshotRow, map[string]string, []string, error) {
	query := `
SELECT
	a.album_variant_id AS album_id,
	a.album_cluster_id AS album_cluster_id
FROM album_variants a
WHERE a.library_id = ?
ORDER BY LOWER(a.title) ASC, a.album_variant_id ASC`
	var seedRows []albumSeedRow
	if err := s.app.storage.WithContext(ctx).Raw(query, strings.TrimSpace(libraryID)).Scan(&seedRows).Error; err != nil {
		return nil, nil, nil, err
	}
	seeds := make([]albumSeedRow, 0, len(seedRows))
	seen := make(map[string]struct{}, len(seedRows))
	for _, row := range seedRows {
		groupID := firstNonEmpty(strings.TrimSpace(row.AlbumClusterID), strings.TrimSpace(row.AlbumID))
		if groupID == "" {
			continue
		}
		if _, ok := seen[groupID]; ok {
			continue
		}
		seen[groupID] = struct{}{}
		seeds = append(seeds, albumSeedRow{AlbumID: row.AlbumID, AlbumClusterID: groupID})
	}
	clusterIDs := make([]string, 0, len(seeds))
	for _, seed := range seeds {
		clusterIDs = append(clusterIDs, strings.TrimSpace(seed.AlbumClusterID))
	}
	rowsByCluster, err := s.app.catalog.listAlbumVariantRowsForClusters(ctx, libraryID, targetDeviceID, clusterIDs)
	if err != nil {
		return nil, nil, nil, err
	}
	preferredByCluster, err := s.app.catalog.preferredAlbumVariantIDsForClusters(ctx, libraryID, targetDeviceID, clusterIDs)
	if err != nil {
		return nil, nil, nil, err
	}

	out := make([]memberProjectionSnapshotRow, 0, len(seeds))
	preferredByAlbumID := make(map[string]string, len(seeds))
	albumIDs := make([]string, 0, len(seeds))
	for _, seed := range seeds {
		clusterID := strings.TrimSpace(seed.AlbumClusterID)
		variants := rowsByCluster[clusterID]
		if len(variants) == 0 {
			continue
		}
		explicitPreferred := preferredByCluster[clusterID]
		chosen := variants[0]
		for _, variant := range variants[1:] {
			if compareAlbumVariants(variant, chosen, explicitPreferred) < 0 {
				chosen = variant
			}
		}
		artists := append([]string(nil), chosen.Artists...)
		sort.Strings(artists)
		item, err := newMemberProjectionSnapshotRow("album", clusterID, map[string]any{
			"library_id":                 libraryID,
			"library_album_id":           clusterID,
			"preferred_variant_album_id": strings.TrimSpace(chosen.AlbumVariantID),
			"album_id":                   clusterID,
			"album_cluster_id":           clusterID,
			"title":                      strings.TrimSpace(chosen.Title),
			"artists":                    artists,
			"year":                       chosen.Year,
			"track_count":                chosen.TrackCount,
			"variant_count":              len(variants),
			"has_variants":               len(variants) > 1,
		})
		if err != nil {
			return nil, nil, nil, err
		}
		out = append(out, item)
		preferredByAlbumID[clusterID] = strings.TrimSpace(chosen.AlbumVariantID)
		albumIDs = append(albumIDs, clusterID)
	}
	return out, preferredByAlbumID, albumIDs, nil
}

func (s *MemberSyncService) buildRecordingSnapshots(ctx context.Context, libraryID, targetDeviceID string) ([]memberProjectionSnapshotRow, []string, error) {
	query := `
SELECT
	r.track_variant_id AS recording_id,
	r.track_cluster_id AS track_cluster_id,
	r.title,
	r.duration_ms,
	COALESCE(GROUP_CONCAT(ar.name, '` + artistSeparator + `'), '') AS artists_csv
FROM track_variants r
LEFT JOIN credits c ON c.library_id = r.library_id AND c.entity_type = 'track' AND c.entity_id = r.track_variant_id
LEFT JOIN artists ar ON ar.library_id = c.library_id AND ar.artist_id = c.artist_id
WHERE r.library_id = ?
GROUP BY r.track_variant_id, r.track_cluster_id, r.title, r.duration_ms
ORDER BY LOWER(r.title) ASC, r.track_variant_id ASC`
	var seedRows []recordingSeedRow
	if err := s.app.storage.WithContext(ctx).Raw(query, strings.TrimSpace(libraryID)).Scan(&seedRows).Error; err != nil {
		return nil, nil, err
	}
	seeds := make([]recordingSeedRow, 0, len(seedRows))
	seen := make(map[string]struct{}, len(seedRows))
	for _, row := range seedRows {
		groupID := firstNonEmpty(strings.TrimSpace(row.TrackClusterID), strings.TrimSpace(row.RecordingID))
		if groupID == "" {
			continue
		}
		if _, ok := seen[groupID]; ok {
			continue
		}
		seen[groupID] = struct{}{}
		row.TrackClusterID = groupID
		seeds = append(seeds, row)
	}

	clusterIDs := make([]string, 0, len(seeds))
	for _, seed := range seeds {
		clusterIDs = append(clusterIDs, strings.TrimSpace(seed.TrackClusterID))
	}
	rowsByCluster, err := s.app.catalog.listRecordingVariantRowsForClusters(ctx, libraryID, targetDeviceID, clusterIDs, memberProjectionPreferredProfile)
	if err != nil {
		return nil, nil, err
	}
	preferredByCluster, err := s.app.catalog.preferredRecordingVariantIDsForClusters(ctx, libraryID, targetDeviceID, clusterIDs)
	if err != nil {
		return nil, nil, err
	}
	hints, err := s.app.catalog.catalogAvailabilityHintsForClusters(ctx, libraryID, targetDeviceID, clusterIDs)
	if err != nil {
		return nil, nil, err
	}

	variantAlbumIDs := make([]string, 0, len(clusterIDs))
	chosenByCluster := make(map[string]recordingVariantRow, len(clusterIDs))
	for _, seed := range seeds {
		clusterID := strings.TrimSpace(seed.TrackClusterID)
		variants := rowsByCluster[clusterID]
		if len(variants) == 0 {
			continue
		}
		preferredID := chooseRecordingVariantID(variants, preferredByCluster[clusterID])
		chosen := variants[0]
		for _, variant := range variants {
			if variant.TrackVariantID == preferredID {
				chosen = variant
				break
			}
		}
		chosenByCluster[clusterID] = chosen
		if strings.TrimSpace(chosen.AlbumVariantID) != "" {
			variantAlbumIDs = append(variantAlbumIDs, strings.TrimSpace(chosen.AlbumVariantID))
		}
	}
	albumClusterByVariant, err := s.albumClustersByVariantID(ctx, libraryID, variantAlbumIDs)
	if err != nil {
		return nil, nil, err
	}

	out := make([]memberProjectionSnapshotRow, 0, len(chosenByCluster))
	recordingIDs := make([]string, 0, len(chosenByCluster))
	for _, seed := range seeds {
		clusterID := strings.TrimSpace(seed.TrackClusterID)
		chosen, ok := chosenByCluster[clusterID]
		if !ok {
			continue
		}
		artists := append([]string(nil), chosen.Artists...)
		sort.Strings(artists)
		item, err := newMemberProjectionSnapshotRow("recording", clusterID, map[string]any{
			"library_id":                     libraryID,
			"library_recording_id":           clusterID,
			"preferred_variant_recording_id": strings.TrimSpace(chosen.TrackVariantID),
			"track_cluster_id":               clusterID,
			"recording_id":                   clusterID,
			"album_id":                       strings.TrimSpace(albumClusterByVariant[strings.TrimSpace(chosen.AlbumVariantID)]),
			"title":                          strings.TrimSpace(chosen.Title),
			"duration_ms":                    chosen.DurationMS,
			"artists":                        artists,
			"variant_count":                  len(rowsByCluster[clusterID]),
			"has_variants":                   len(rowsByCluster[clusterID]) > 1,
			"availability_hint":              string(hints[clusterID].State),
		})
		if err != nil {
			return nil, nil, err
		}
		out = append(out, item)
		recordingIDs = append(recordingIDs, clusterID)
	}
	return out, recordingIDs, nil
}

func (s *MemberSyncService) buildAlbumAvailabilitySnapshots(ctx context.Context, local apitypes.LocalContext, albumIDs []string, preferredAlbumByID map[string]string) ([]memberProjectionSnapshotRow, error) {
	albumIDs = compactNonEmptyStrings(albumIDs)
	if len(albumIDs) == 0 {
		return nil, nil
	}
	summaries, err := s.app.playback.albumAvailabilitySummaries(ctx, local, albumIDs, memberProjectionPreferredProfile)
	if err != nil {
		return nil, err
	}
	out := make([]memberProjectionSnapshotRow, 0, len(albumIDs))
	for _, albumID := range albumIDs {
		albumID = strings.TrimSpace(albumID)
		summary := summaries[albumID]
		item, err := newMemberProjectionSnapshotRow("album_availability", memberAvailabilityEntityID(albumID, memberProjectionPreferredProfile), map[string]any{
			"library_id":        local.LibraryID,
			"album_id":          albumID,
			"library_album_id":  albumID,
			"variant_album_id":  strings.TrimSpace(preferredAlbumByID[albumID]),
			"preferred_profile": memberProjectionPreferredProfile,
			"availability": map[string]any{
				"state":                     string(summary.State),
				"track_count":               summary.TrackCount,
				"is_local":                  summary.IsLocal,
				"has_remote":                summary.HasRemote,
				"local_track_count":         summary.LocalTrackCount,
				"local_source_track_count":  summary.LocalSourceTrackCount,
				"pinned_track_count":        summary.PinnedTrackCount,
				"cached_track_count":        summary.CachedTrackCount,
				"remote_track_count":        summary.RemoteTrackCount,
				"available_track_count":     summary.AvailableTrackCount,
				"available_now_track_count": summary.AvailableNowTrackCount,
				"offline_track_count":       summary.OfflineTrackCount,
				"unavailable_track_count":   summary.UnavailableTrackCount,
			},
		})
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

func (s *MemberSyncService) buildRecordingAvailabilitySnapshots(ctx context.Context, local apitypes.LocalContext, recordingIDs []string) ([]memberProjectionSnapshotRow, error) {
	recordingIDs = compactNonEmptyStrings(recordingIDs)
	if len(recordingIDs) == 0 {
		return nil, nil
	}
	items, err := s.app.playback.batchRecordingPlaybackAvailability(ctx, local, recordingIDs, memberProjectionPreferredProfile)
	if err != nil {
		return nil, err
	}
	out := make([]memberProjectionSnapshotRow, 0, len(items))
	for _, item := range items {
		entryID := memberAvailabilityEntityID(strings.TrimSpace(item.RecordingID), strings.TrimSpace(item.PreferredProfile))
		snapshot, err := newMemberProjectionSnapshotRow("recording_playback_availability", entryID, map[string]any{
			"library_id":           local.LibraryID,
			"recording_id":         strings.TrimSpace(item.RecordingID),
			"library_recording_id": strings.TrimSpace(item.LibraryRecordingID),
			"variant_recording_id": strings.TrimSpace(item.VariantRecordingID),
			"preferred_profile":    strings.TrimSpace(item.PreferredProfile),
			"state":                string(item.State),
			"source_kind":          string(item.SourceKind),
			"local_path":           strings.TrimSpace(item.LocalPath),
			"reason":               string(item.Reason),
		})
		if err != nil {
			return nil, err
		}
		out = append(out, snapshot)
	}
	return out, nil
}

func (s *MemberSyncService) buildPlaylistSnapshots(ctx context.Context, libraryID string) ([]memberProjectionSnapshotRow, []string, error) {
	type row struct {
		PlaylistID     string
		Name           string
		Kind           string
		CreatedBy      string
		UpdatedAt      time.Time
		ItemCount      int64
		HasCustomCover int
	}
	query := `
SELECT
	p.playlist_id,
	p.name,
	p.kind,
	p.created_by,
	p.updated_at,
	COUNT(DISTINCT pi.item_id) AS item_count,
	MAX(CASE WHEN pc.playlist_id IS NULL THEN 0 ELSE 1 END) AS has_custom_cover
FROM playlists p
LEFT JOIN playlist_items pi ON pi.library_id = p.library_id AND pi.playlist_id = p.playlist_id AND pi.deleted_at IS NULL
LEFT JOIN playlist_covers pc ON pc.library_id = p.library_id AND pc.playlist_id = p.playlist_id
WHERE p.library_id = ? AND p.deleted_at IS NULL AND p.kind <> ?
GROUP BY p.playlist_id, p.name, p.kind, p.created_by, p.updated_at
ORDER BY LOWER(p.name) ASC, p.playlist_id ASC`
	var rows []row
	if err := s.app.storage.WithContext(ctx).Raw(query, strings.TrimSpace(libraryID), playlistKindLiked).Scan(&rows).Error; err != nil {
		return nil, nil, err
	}
	out := make([]memberProjectionSnapshotRow, 0, len(rows))
	playlistIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		thumb, _, err := s.app.catalog.loadPlaylistArtworkRef(ctx, libraryID, row.PlaylistID)
		if err != nil {
			return nil, nil, err
		}
		item, err := newMemberProjectionSnapshotRow("playlist", strings.TrimSpace(row.PlaylistID), map[string]any{
			"library_id":       libraryID,
			"playlist_id":      strings.TrimSpace(row.PlaylistID),
			"name":             strings.TrimSpace(row.Name),
			"kind":             strings.TrimSpace(row.Kind),
			"is_reserved":      false,
			"has_custom_cover": row.HasCustomCover > 0,
			"created_by":       strings.TrimSpace(row.CreatedBy),
			"updated_at":       row.UpdatedAt.UTC().Format(time.RFC3339Nano),
			"item_count":       row.ItemCount,
			"thumb": map[string]any{
				"blob_id":   strings.TrimSpace(thumb.BlobID),
				"mime":      strings.TrimSpace(thumb.MIME),
				"file_ext":  normalizeArtworkFileExt(thumb.FileExt, thumb.MIME),
				"variant":   firstNonEmpty(strings.TrimSpace(thumb.Variant), playlistCoverVariantCanonical),
				"width":     thumb.Width,
				"height":    thumb.Height,
				"bytes":     thumb.Bytes,
				"local_uri": "",
			},
		})
		if err != nil {
			return nil, nil, err
		}
		out = append(out, item)
		playlistIDs = append(playlistIDs, strings.TrimSpace(row.PlaylistID))
	}
	return out, playlistIDs, nil
}

func (s *MemberSyncService) buildPlaylistTrackSnapshots(ctx context.Context, libraryID, targetDeviceID string, playlistIDs []string) ([]memberProjectionSnapshotRow, error) {
	playlistIDs = compactNonEmptyStrings(playlistIDs)
	if len(playlistIDs) == 0 {
		return nil, nil
	}
	type seedRow struct {
		PlaylistID         string
		ItemID             string
		LibraryRecordingID string
		AddedAt            time.Time
	}
	query := `
SELECT
	pi.playlist_id,
	pi.item_id,
	pi.track_variant_id AS library_recording_id,
	pi.added_at
FROM playlist_items pi
JOIN playlists p ON p.library_id = pi.library_id AND p.playlist_id = pi.playlist_id
WHERE pi.library_id = ? AND p.playlist_id IN ? AND p.kind <> ? AND pi.deleted_at IS NULL AND p.deleted_at IS NULL
ORDER BY p.playlist_id ASC, pi.position_key ASC, pi.item_id ASC`
	var seeds []seedRow
	if err := s.app.storage.WithContext(ctx).Raw(query, strings.TrimSpace(libraryID), playlistIDs, playlistKindLiked).Scan(&seeds).Error; err != nil {
		return nil, err
	}

	clusterIDs := make([]string, 0, len(seeds))
	for _, seed := range seeds {
		clusterIDs = append(clusterIDs, strings.TrimSpace(seed.LibraryRecordingID))
	}
	rowsByCluster, err := s.app.catalog.listRecordingVariantRowsForClusters(ctx, libraryID, targetDeviceID, clusterIDs, memberProjectionPreferredProfile)
	if err != nil {
		return nil, err
	}
	preferredByCluster, err := s.app.catalog.preferredRecordingVariantIDsForClusters(ctx, libraryID, targetDeviceID, clusterIDs)
	if err != nil {
		return nil, err
	}

	out := make([]memberProjectionSnapshotRow, 0, len(seeds))
	positionByPlaylist := make(map[string]int, len(playlistIDs))
	for _, seed := range seeds {
		clusterID := strings.TrimSpace(seed.LibraryRecordingID)
		variants := rowsByCluster[clusterID]
		if len(variants) == 0 {
			continue
		}
		preferredID := chooseRecordingVariantID(variants, preferredByCluster[clusterID])
		chosen := variants[0]
		for _, variant := range variants {
			if variant.TrackVariantID == preferredID {
				chosen = variant
				break
			}
		}
		artists := append([]string(nil), chosen.Artists...)
		sort.Strings(artists)
		positionByPlaylist[strings.TrimSpace(seed.PlaylistID)]++
		item, err := newMemberProjectionSnapshotRow("playlist_track", strings.TrimSpace(seed.ItemID), map[string]any{
			"item_id":              strings.TrimSpace(seed.ItemID),
			"playlist_id":          strings.TrimSpace(seed.PlaylistID),
			"position":             positionByPlaylist[strings.TrimSpace(seed.PlaylistID)],
			"library_recording_id": clusterID,
			"recording_id":         strings.TrimSpace(chosen.TrackVariantID),
			"title":                strings.TrimSpace(chosen.Title),
			"duration_ms":          chosen.DurationMS,
			"artists":              artists,
			"added_at":             seed.AddedAt.UTC().Format(time.RFC3339Nano),
		})
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

func (s *MemberSyncService) buildLikedRecordingSnapshots(ctx context.Context, libraryID, targetDeviceID string) ([]memberProjectionSnapshotRow, error) {
	type seedRow struct {
		LibraryRecordingID string
		AddedAt            time.Time
	}
	query := `
SELECT
	pi.track_variant_id AS library_recording_id,
	pi.added_at
FROM playlist_items pi
JOIN playlists p ON p.library_id = pi.library_id AND p.playlist_id = pi.playlist_id
WHERE pi.library_id = ? AND pi.playlist_id = ? AND pi.deleted_at IS NULL AND p.deleted_at IS NULL
GROUP BY pi.item_id, pi.track_variant_id, pi.added_at
ORDER BY pi.added_at DESC, pi.item_id DESC`
	var seeds []seedRow
	if err := s.app.storage.WithContext(ctx).Raw(query, strings.TrimSpace(libraryID), likedPlaylistIDForLibrary(libraryID)).Scan(&seeds).Error; err != nil {
		return nil, err
	}
	clusterIDs := make([]string, 0, len(seeds))
	for _, seed := range seeds {
		clusterIDs = append(clusterIDs, strings.TrimSpace(seed.LibraryRecordingID))
	}
	rowsByCluster, err := s.app.catalog.listRecordingVariantRowsForClusters(ctx, libraryID, targetDeviceID, clusterIDs, memberProjectionPreferredProfile)
	if err != nil {
		return nil, err
	}
	preferredByCluster, err := s.app.catalog.preferredRecordingVariantIDsForClusters(ctx, libraryID, targetDeviceID, clusterIDs)
	if err != nil {
		return nil, err
	}

	out := make([]memberProjectionSnapshotRow, 0, len(seeds))
	for _, seed := range seeds {
		clusterID := strings.TrimSpace(seed.LibraryRecordingID)
		variants := rowsByCluster[clusterID]
		if len(variants) == 0 {
			continue
		}
		preferredID := chooseRecordingVariantID(variants, preferredByCluster[clusterID])
		chosen := variants[0]
		for _, variant := range variants {
			if variant.TrackVariantID == preferredID {
				chosen = variant
				break
			}
		}
		artists := append([]string(nil), chosen.Artists...)
		sort.Strings(artists)
		item, err := newMemberProjectionSnapshotRow("liked_recording", clusterID, map[string]any{
			"library_id":           libraryID,
			"library_recording_id": clusterID,
			"recording_id":         strings.TrimSpace(chosen.TrackVariantID),
			"title":                strings.TrimSpace(chosen.Title),
			"duration_ms":          chosen.DurationMS,
			"artists":              artists,
			"added_at":             seed.AddedAt.UTC().Format(time.RFC3339Nano),
		})
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

func (s *MemberSyncService) albumClustersByVariantID(ctx context.Context, libraryID string, variantIDs []string) (map[string]string, error) {
	variantIDs = compactNonEmptyStrings(variantIDs)
	if len(variantIDs) == 0 {
		return map[string]string{}, nil
	}
	type row struct {
		AlbumVariantID string
		AlbumClusterID string
	}
	var rows []row
	if err := s.app.storage.WithContext(ctx).
		Model(&AlbumVariantModel{}).
		Select("album_variant_id, album_cluster_id").
		Where("library_id = ? AND album_variant_id IN ?", strings.TrimSpace(libraryID), variantIDs).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]string, len(rows))
	for _, row := range rows {
		out[strings.TrimSpace(row.AlbumVariantID)] = strings.TrimSpace(row.AlbumClusterID)
	}
	return out, nil
}

func (s *MemberSyncService) selectSyncBatch(ctx context.Context, libraryID, targetDeviceID string, lastVersion int64, limit int, latestVersion int64) (memberSyncBatch, error) {
	if limit <= 0 {
		limit = defaultSyncBatchSize
	}
	if lastVersion < 0 {
		lastVersion = 0
	}
	var rows []MemberProjectionOp
	if err := s.app.storage.WithContext(ctx).
		Where("library_id = ? AND target_device_id = ? AND version > ?", strings.TrimSpace(libraryID), strings.TrimSpace(targetDeviceID), lastVersion).
		Order("version ASC").
		Limit(limit).
		Find(&rows).Error; err != nil {
		return memberSyncBatch{}, err
	}
	ops := make([]memberProjectionEntry, 0, len(rows))
	for _, row := range rows {
		ops = append(ops, memberProjectionEntry{
			Version:     row.Version,
			TSNS:        row.TSNS,
			EntityType:  strings.TrimSpace(row.EntityType),
			EntityID:    strings.TrimSpace(row.EntityID),
			OpKind:      strings.TrimSpace(row.OpKind),
			PayloadJSON: json.RawMessage(defaultProjectionPayload(row.PayloadJSON)),
		})
	}
	totalMissing := latestVersion - lastVersion
	if totalMissing < 0 {
		totalMissing = 0
	}
	remaining := totalMissing - int64(len(ops))
	if remaining < 0 {
		remaining = 0
	}
	return memberSyncBatch{
		Ops:          ops,
		HasMore:      remaining > 0,
		RemainingOps: remaining,
		TotalMissing: totalMissing,
	}, nil
}

func (s *MemberSyncService) publishCheckpointForTargetDevice(ctx context.Context, libraryID, targetDeviceID string) (memberCheckpointTransferRecord, error) {
	meta, err := s.ensureProjectionCurrent(ctx, libraryID, targetDeviceID)
	if err != nil {
		return memberCheckpointTransferRecord{}, err
	}
	var rows []MemberProjectionState
	if err := s.app.storage.WithContext(ctx).
		Where("library_id = ? AND target_device_id = ?", strings.TrimSpace(libraryID), strings.TrimSpace(targetDeviceID)).
		Order("entity_type ASC, entity_id ASC").
		Find(&rows).Error; err != nil {
		return memberCheckpointTransferRecord{}, err
	}
	entries := make([]memberProjectionEntry, 0, len(rows))
	for _, row := range rows {
		entries = append(entries, memberProjectionEntry{
			Version:     meta.LastVersion,
			TSNS:        meta.UpdatedAt.UnixNano(),
			EntityType:  strings.TrimSpace(row.EntityType),
			EntityID:    strings.TrimSpace(row.EntityID),
			OpKind:      "upsert",
			PayloadJSON: json.RawMessage(defaultProjectionPayload(row.PayloadJSON)),
		})
	}
	chunks, err := buildMemberCheckpointChunks(entries, memberCheckpointChunkSize)
	if err != nil {
		return memberCheckpointTransferRecord{}, err
	}
	contentHash, err := memberCheckpointContentHash(meta.LastVersion, chunks)
	if err != nil {
		return memberCheckpointTransferRecord{}, err
	}

	now := time.Now().UTC()
	record := memberCheckpointTransferRecord{
		Manifest: memberCheckpointManifest{
			LibraryID:      strings.TrimSpace(libraryID),
			TargetDeviceID: strings.TrimSpace(targetDeviceID),
			CheckpointID:   contentHash,
			BaseVersion:    meta.LastVersion,
			CreatedAt:      now,
			ChunkCount:     len(chunks),
			EntryCount:     len(entries),
			ContentHash:    contentHash,
			Status:         memberCheckpointStatusPublished,
			PublishedAt:    cloneTimePtr(&now),
		},
		Chunks: chunks,
	}
	if err := s.app.storage.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("library_id = ? AND target_device_id = ?", strings.TrimSpace(libraryID), strings.TrimSpace(targetDeviceID)).
			Delete(&MemberCheckpointChunk{}).Error; err != nil {
			return err
		}
		if err := tx.Where("library_id = ? AND target_device_id = ?", strings.TrimSpace(libraryID), strings.TrimSpace(targetDeviceID)).
			Delete(&MemberCheckpoint{}).Error; err != nil {
			return err
		}
		if err := tx.Create(&MemberCheckpoint{
			LibraryID:      strings.TrimSpace(record.Manifest.LibraryID),
			TargetDeviceID: strings.TrimSpace(record.Manifest.TargetDeviceID),
			CheckpointID:   strings.TrimSpace(record.Manifest.CheckpointID),
			BaseVersion:    record.Manifest.BaseVersion,
			ChunkCount:     record.Manifest.ChunkCount,
			EntryCount:     record.Manifest.EntryCount,
			ContentHash:    strings.TrimSpace(record.Manifest.ContentHash),
			Status:         strings.TrimSpace(record.Manifest.Status),
			CreatedAt:      record.Manifest.CreatedAt,
			UpdatedAt:      now,
			PublishedAt:    cloneTimePtr(record.Manifest.PublishedAt),
		}).Error; err != nil {
			return err
		}
		for _, chunk := range record.Chunks {
			payload, err := json.Marshal(chunk.Entries)
			if err != nil {
				return err
			}
			if err := tx.Create(&MemberCheckpointChunk{
				LibraryID:      strings.TrimSpace(record.Manifest.LibraryID),
				TargetDeviceID: strings.TrimSpace(record.Manifest.TargetDeviceID),
				CheckpointID:   strings.TrimSpace(record.Manifest.CheckpointID),
				ChunkIndex:     chunk.ChunkIndex,
				EntryCount:     chunk.EntryCount,
				ContentHash:    strings.TrimSpace(chunk.ContentHash),
				PayloadJSON:    string(payload),
			}).Error; err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return memberCheckpointTransferRecord{}, err
	}
	return record, nil
}

func (s *MemberSyncService) loadPublishedCheckpointRecord(ctx context.Context, libraryID, targetDeviceID string) (memberCheckpointTransferRecord, bool, error) {
	return s.loadCheckpointRecord(ctx, libraryID, targetDeviceID, "", true)
}

func (s *MemberSyncService) loadCheckpointRecord(ctx context.Context, libraryID, targetDeviceID, checkpointID string, publishedOnly bool) (memberCheckpointTransferRecord, bool, error) {
	libraryID = strings.TrimSpace(libraryID)
	targetDeviceID = strings.TrimSpace(targetDeviceID)
	checkpointID = strings.TrimSpace(checkpointID)
	if libraryID == "" || targetDeviceID == "" {
		return memberCheckpointTransferRecord{}, false, fmt.Errorf("library id and device id are required")
	}

	var row MemberCheckpoint
	query := s.app.storage.WithContext(ctx).
		Where("library_id = ? AND target_device_id = ?", libraryID, targetDeviceID)
	if publishedOnly {
		query = query.Where("published_at IS NOT NULL").Order("published_at DESC")
	} else {
		query = query.Where("checkpoint_id = ?", checkpointID)
	}
	if err := query.Limit(1).Take(&row).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return memberCheckpointTransferRecord{}, false, nil
		}
		return memberCheckpointTransferRecord{}, false, err
	}

	var chunkRows []MemberCheckpointChunk
	if err := s.app.storage.WithContext(ctx).
		Where("library_id = ? AND target_device_id = ? AND checkpoint_id = ?", libraryID, targetDeviceID, row.CheckpointID).
		Order("chunk_index ASC").
		Find(&chunkRows).Error; err != nil {
		return memberCheckpointTransferRecord{}, false, err
	}
	chunks := make([]memberCheckpointChunk, 0, len(chunkRows))
	for _, chunkRow := range chunkRows {
		var entries []memberProjectionEntry
		if strings.TrimSpace(chunkRow.PayloadJSON) != "" {
			if err := json.Unmarshal([]byte(chunkRow.PayloadJSON), &entries); err != nil {
				return memberCheckpointTransferRecord{}, false, fmt.Errorf("decode member checkpoint chunk %d: %w", chunkRow.ChunkIndex, err)
			}
		}
		chunks = append(chunks, memberCheckpointChunk{
			ChunkIndex:  chunkRow.ChunkIndex,
			EntryCount:  chunkRow.EntryCount,
			ContentHash: strings.TrimSpace(chunkRow.ContentHash),
			Entries:     entries,
		})
	}

	return memberCheckpointTransferRecord{
		Manifest: memberCheckpointManifest{
			LibraryID:      strings.TrimSpace(row.LibraryID),
			TargetDeviceID: strings.TrimSpace(row.TargetDeviceID),
			CheckpointID:   strings.TrimSpace(row.CheckpointID),
			BaseVersion:    row.BaseVersion,
			CreatedAt:      row.CreatedAt,
			ChunkCount:     row.ChunkCount,
			EntryCount:     row.EntryCount,
			ContentHash:    strings.TrimSpace(row.ContentHash),
			Status:         strings.TrimSpace(row.Status),
			PublishedAt:    cloneTimePtr(row.PublishedAt),
		},
		Chunks: chunks,
	}, true, nil
}

func (s *MemberSyncService) listMemberDeviceIDs(ctx context.Context, libraryID string) ([]string, error) {
	type row struct {
		DeviceID string
	}
	var rows []row
	if err := s.app.storage.WithContext(ctx).
		Model(&Membership{}).
		Select("device_id").
		Where("library_id = ?", strings.TrimSpace(libraryID)).
		Order("device_id ASC").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		deviceID := strings.TrimSpace(row.DeviceID)
		if deviceID != "" {
			out = append(out, deviceID)
		}
	}
	return out, nil
}

func buildMemberCheckpointChunks(entries []memberProjectionEntry, chunkSize int) ([]memberCheckpointChunk, error) {
	if chunkSize <= 0 {
		chunkSize = memberCheckpointChunkSize
	}
	if len(entries) == 0 {
		return nil, nil
	}
	chunks := make([]memberCheckpointChunk, 0, (len(entries)+chunkSize-1)/chunkSize)
	for start := 0; start < len(entries); start += chunkSize {
		end := start + chunkSize
		if end > len(entries) {
			end = len(entries)
		}
		part := append([]memberProjectionEntry(nil), entries[start:end]...)
		hash, err := hashMemberCheckpointChunk(part)
		if err != nil {
			return nil, err
		}
		chunks = append(chunks, memberCheckpointChunk{
			ChunkIndex:  len(chunks),
			EntryCount:  len(part),
			ContentHash: hash,
			Entries:     part,
		})
	}
	return chunks, nil
}

func hashMemberCheckpointChunk(entries []memberProjectionEntry) (string, error) {
	payload, err := json.Marshal(entries)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func memberCheckpointContentHash(baseVersion int64, chunks []memberCheckpointChunk) (string, error) {
	payload := struct {
		BaseVersion int64                   `json:"baseVersion"`
		Chunks      []memberCheckpointChunk `json:"chunks"`
	}{
		BaseVersion: baseVersion,
		Chunks:      chunks,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func newMemberProjectionSnapshotRow(entityType, entityID string, payload any) (memberProjectionSnapshotRow, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return memberProjectionSnapshotRow{}, err
	}
	sum := sha256.Sum256(encoded)
	return memberProjectionSnapshotRow{
		EntityType:  strings.TrimSpace(entityType),
		EntityID:    strings.TrimSpace(entityID),
		PayloadJSON: string(encoded),
		ContentHash: hex.EncodeToString(sum[:]),
	}, nil
}

func memberProjectionKey(entityType, entityID string) string {
	return strings.TrimSpace(entityType) + "\x00" + strings.TrimSpace(entityID)
}

func defaultProjectionPayload(payload string) string {
	payload = strings.TrimSpace(payload)
	if payload == "" {
		return "{}"
	}
	return payload
}

func nullableTimeString(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func memberAvailabilityEntityID(entityID, profile string) string {
	entityID = strings.TrimSpace(entityID)
	profile = strings.TrimSpace(profile)
	if profile == "" {
		return entityID
	}
	return entityID + "::" + profile
}
