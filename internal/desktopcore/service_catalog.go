package desktopcore

import (
	"context"
	"encoding/base64"
	"fmt"
	"sort"
	"strings"
	"time"

	apitypes "ben/desktop/api/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type CatalogService struct {
	app *App
}

func (s *CatalogService) SubscribeCatalogChanges(listener func(apitypes.CatalogChangeEvent)) func() {
	if s == nil || s.app == nil {
		return func() {}
	}
	return s.app.SubscribeCatalogChanges(listener)
}

type recordingVariantRow struct {
	TrackVariantID string
	TrackClusterID string
	SourceFileID   string
	Title          string
	DurationMS     int64
	Artists        []string
	AlbumVariantID string
	AlbumTitle     string
	TrackNo        int
	DiscNo         int
	Container      string
	Codec          string
	Bitrate        int
	SampleRate     int
	Channels       int
	IsLossless     bool
	QualityRank    int
	IsPresentLocal bool
	IsCachedLocal  bool
	LocalPath      string
}

type albumVariantRow struct {
	AlbumVariantID  string
	AlbumClusterID  string
	Title           string
	Artists         []string
	Year            *int
	Edition         string
	TrackCount      int64
	BestQualityRank int
	LocalTrackCount int64
}

type recordingSeedRow struct {
	RecordingID    string
	TrackClusterID string
	Title          string
	DurationMS     int64
	ArtistsCSV     string
}

type albumSeedRow struct {
	AlbumID        string
	AlbumClusterID string
}

type playlistTrackSeedRow struct {
	ItemID             string
	LibraryRecordingID string
	AddedAt            time.Time
	PositionKey        string
}

type likedRecordingSeedRow struct {
	ItemID             string
	LibraryRecordingID string
	AddedAt            time.Time
}

type chosenRecordingVariant struct {
	ClusterID    string
	Chosen       recordingVariantRow
	VariantCount int
}

type trackSourceSeedRow struct {
	TrackClusterID string
	SortTitle      string
}

func (s *CatalogService) ListArtists(ctx context.Context, req apitypes.ArtistListRequest) (apitypes.Page[apitypes.ArtistListItem], error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return apitypes.Page[apitypes.ArtistListItem]{}, err
	}

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
	if err := s.app.storage.ReadWithContext(ctx).Raw(query, local.LibraryID).Scan(&rows).Error; err != nil {
		return apitypes.Page[apitypes.ArtistListItem]{}, err
	}
	pagedRows, pageInfo := pageItems(rows, req.PageRequest)
	out := make([]apitypes.ArtistListItem, 0, len(pagedRows))
	for _, row := range pagedRows {
		out = append(out, apitypes.ArtistListItem{
			ArtistID:   row.ArtistID,
			Name:       row.Name,
			AlbumCount: row.AlbumCount,
			TrackCount: row.TrackCount,
		})
	}
	return apitypes.Page[apitypes.ArtistListItem]{Items: out, Page: pageInfo}, nil
}

func (s *CatalogService) GetArtist(ctx context.Context, artistID string) (apitypes.ArtistListItem, error) {
	page, err := s.ListArtists(ctx, apitypes.ArtistListRequest{PageRequest: apitypes.PageRequest{Limit: maxPageLimit}})
	if err != nil {
		return apitypes.ArtistListItem{}, err
	}
	artistID = strings.TrimSpace(artistID)
	for _, item := range page.Items {
		if item.ArtistID == artistID {
			return item, nil
		}
	}
	return apitypes.ArtistListItem{}, fmt.Errorf("artist %s not found", artistID)
}

func (s *CatalogService) ListArtistAlbums(ctx context.Context, req apitypes.ArtistAlbumListRequest) (apitypes.Page[apitypes.AlbumListItem], error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return apitypes.Page[apitypes.AlbumListItem]{}, err
	}
	query := `
SELECT
	a.album_variant_id AS album_id,
	a.album_cluster_id AS album_cluster_id
FROM credits c
JOIN album_variants a ON a.library_id = c.library_id AND a.album_variant_id = c.entity_id
WHERE a.library_id = ? AND c.entity_type = 'album' AND c.artist_id = ?
ORDER BY LOWER(a.title) ASC, a.album_variant_id ASC`
	return s.listCollapsedAlbums(ctx, local.LibraryID, local.DeviceID, req.PageRequest, query, local.LibraryID, strings.TrimSpace(req.ArtistID))
}

func (s *CatalogService) ListAlbums(ctx context.Context, req apitypes.AlbumListRequest) (apitypes.Page[apitypes.AlbumListItem], error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return apitypes.Page[apitypes.AlbumListItem]{}, err
	}
	query := `
SELECT
	a.album_variant_id AS album_id,
	a.album_cluster_id AS album_cluster_id
FROM album_variants a
WHERE a.library_id = ?
ORDER BY LOWER(a.title) ASC, a.album_variant_id ASC`
	return s.listCollapsedAlbums(ctx, local.LibraryID, local.DeviceID, req.PageRequest, query, local.LibraryID)
}

func (s *CatalogService) GetAlbum(ctx context.Context, albumID string) (apitypes.AlbumListItem, error) {
	variants, err := s.ListAlbumVariants(ctx, apitypes.AlbumVariantListRequest{
		LibraryAlbumID: strings.TrimSpace(albumID),
		PageRequest:    apitypes.PageRequest{Limit: maxPageLimit},
	})
	if err != nil {
		return apitypes.AlbumListItem{}, err
	}
	if len(variants.Items) == 0 {
		return apitypes.AlbumListItem{}, fmt.Errorf("album %s not found", albumID)
	}
	return collapsedAlbumFromVariants(variants.Items)
}

func (s *CatalogService) ListRecordings(ctx context.Context, req apitypes.RecordingListRequest) (apitypes.Page[apitypes.RecordingListItem], error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return apitypes.Page[apitypes.RecordingListItem]{}, err
	}
	seeds, pageInfo, err := s.listTrackSourceSeedsPage(ctx, local.LibraryID, req.PageRequest)
	if err != nil {
		return apitypes.Page[apitypes.RecordingListItem]{}, err
	}
	return s.listCollapsedRecordings(ctx, local.LibraryID, local.DeviceID, seeds, pageInfo)
}

func (s *CatalogService) ListRecordingsCursor(ctx context.Context, req apitypes.RecordingCursorRequest) (apitypes.CursorPage[apitypes.RecordingListItem], error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return apitypes.CursorPage[apitypes.RecordingListItem]{}, err
	}
	seeds, pageInfo, err := s.listTrackSourceSeedsCursor(ctx, local.LibraryID, req.CursorPageRequest)
	if err != nil {
		return apitypes.CursorPage[apitypes.RecordingListItem]{}, err
	}
	page, err := s.listCollapsedRecordings(ctx, local.LibraryID, local.DeviceID, seeds, apitypes.PageInfo{
		Limit:    pageInfo.Limit,
		Returned: len(seeds),
		HasMore:  pageInfo.HasMore,
	})
	if err != nil {
		return apitypes.CursorPage[apitypes.RecordingListItem]{}, err
	}
	return apitypes.CursorPage[apitypes.RecordingListItem]{
		Items: page.Items,
		Page: apitypes.CursorPageInfo{
			Limit:      pageInfo.Limit,
			Returned:   len(page.Items),
			HasMore:    pageInfo.HasMore,
			NextCursor: pageInfo.NextCursor,
		},
	}, nil
}

func (s *CatalogService) GetRecording(ctx context.Context, recordingID string) (apitypes.RecordingListItem, error) {
	variants, err := s.ListRecordingVariants(ctx, apitypes.RecordingVariantListRequest{
		LibraryRecordingID: strings.TrimSpace(recordingID),
		PageRequest:        apitypes.PageRequest{Limit: maxPageLimit},
	})
	if err != nil {
		return apitypes.RecordingListItem{}, err
	}
	if len(variants.Items) == 0 {
		return apitypes.RecordingListItem{}, fmt.Errorf("recording %s not found", recordingID)
	}
	chosen := variants.Items[0]
	for _, variant := range variants.Items {
		if variant.IsPreferred {
			chosen = variant
			break
		}
	}
	return apitypes.RecordingListItem{
		LibraryRecordingID:          chosen.TrackClusterID,
		PreferredVariantRecordingID: chosen.RecordingID,
		TrackClusterID:              chosen.TrackClusterID,
		RecordingID:                 chosen.TrackClusterID,
		AlbumID:                     chosen.AlbumID,
		Title:                       chosen.Title,
		DurationMS:                  chosen.DurationMS,
		Artists:                     append([]string(nil), chosen.Artists...),
		VariantCount:                int64(len(variants.Items)),
		HasVariants:                 len(variants.Items) > 1,
	}, nil
}

func (s *CatalogService) ListRecordingVariants(ctx context.Context, req apitypes.RecordingVariantListRequest) (apitypes.Page[apitypes.RecordingVariantItem], error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return apitypes.Page[apitypes.RecordingVariantItem]{}, err
	}
	recordingID := firstNonEmpty(strings.TrimSpace(req.LibraryRecordingID), strings.TrimSpace(req.RecordingID))
	variants, err := s.listRecordingVariantsRows(ctx, local.LibraryID, local.DeviceID, recordingID, s.app.cfg.TranscodeProfile)
	if err != nil {
		return apitypes.Page[apitypes.RecordingVariantItem]{}, err
	}
	explicitPreferredID, _, err := s.preferredRecordingVariantID(ctx, local.LibraryID, local.DeviceID, recordingID)
	if err != nil {
		return apitypes.Page[apitypes.RecordingVariantItem]{}, err
	}
	preferredID := explicitPreferredID
	if preferredID == "" && len(variants) > 0 {
		preferredID = chooseRecordingVariantID(variants, explicitPreferredID)
	}
	paged, pageInfo := pageItems(variants, req.PageRequest)
	out := make([]apitypes.RecordingVariantItem, 0, len(paged))
	for _, row := range paged {
		out = append(out, apitypes.RecordingVariantItem{
			LibraryRecordingID:  row.TrackClusterID,
			VariantRecordingID:  row.TrackVariantID,
			RecordingID:         row.TrackVariantID,
			TrackClusterID:      row.TrackClusterID,
			ContentID:           row.SourceFileID,
			Title:               row.Title,
			DurationMS:          row.DurationMS,
			Artists:             append([]string(nil), row.Artists...),
			AlbumID:             row.AlbumVariantID,
			AlbumTitle:          row.AlbumTitle,
			TrackNo:             row.TrackNo,
			DiscNo:              row.DiscNo,
			Container:           row.Container,
			Codec:               row.Codec,
			Bitrate:             row.Bitrate,
			SampleRate:          row.SampleRate,
			Channels:            row.Channels,
			IsLossless:          row.IsLossless,
			QualityRank:         row.QualityRank,
			IsPreferred:         row.TrackVariantID == preferredID,
			IsExplicitPreferred: row.TrackVariantID == explicitPreferredID,
			IsPresentLocal:      row.IsPresentLocal,
			IsCachedLocal:       row.IsCachedLocal,
			LocalPath:           row.LocalPath,
		})
	}
	return apitypes.Page[apitypes.RecordingVariantItem]{Items: out, Page: pageInfo}, nil
}

func (s *CatalogService) ListAlbumVariants(ctx context.Context, req apitypes.AlbumVariantListRequest) (apitypes.Page[apitypes.AlbumVariantItem], error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return apitypes.Page[apitypes.AlbumVariantItem]{}, err
	}
	albumID := firstNonEmpty(strings.TrimSpace(req.LibraryAlbumID), strings.TrimSpace(req.AlbumID))
	variants, err := s.listAlbumVariantsRows(ctx, local.LibraryID, local.DeviceID, albumID)
	if err != nil {
		return apitypes.Page[apitypes.AlbumVariantItem]{}, err
	}
	explicitPreferredID, _, err := s.preferredAlbumVariantID(ctx, local.LibraryID, local.DeviceID, albumID)
	if err != nil {
		return apitypes.Page[apitypes.AlbumVariantItem]{}, err
	}
	preferredID := explicitPreferredID
	if preferredID == "" && len(variants) > 0 {
		candidates := make([]apitypes.AlbumVariantItem, 0, len(variants))
		for _, row := range variants {
			thumb, _ := s.loadAlbumArtworkRef(ctx, local.LibraryID, row.AlbumVariantID)
			candidates = append(candidates, apitypes.AlbumVariantItem{
				AlbumID:         row.AlbumVariantID,
				Title:           row.Title,
				TrackCount:      row.TrackCount,
				Thumb:           thumb,
				BestQualityRank: row.BestQualityRank,
			})
		}
		preferredID = chooseAlbumVariantID(candidates, "")
	}
	paged, pageInfo := pageItems(variants, req.PageRequest)
	out := make([]apitypes.AlbumVariantItem, 0, len(paged))
	for _, row := range paged {
		thumb, _ := s.loadAlbumArtworkRef(ctx, local.LibraryID, row.AlbumVariantID)
		out = append(out, apitypes.AlbumVariantItem{
			LibraryAlbumID:      row.AlbumClusterID,
			VariantAlbumID:      row.AlbumVariantID,
			AlbumID:             row.AlbumVariantID,
			AlbumClusterID:      row.AlbumClusterID,
			Title:               row.Title,
			Artists:             append([]string(nil), row.Artists...),
			Year:                row.Year,
			Edition:             row.Edition,
			TrackCount:          row.TrackCount,
			Thumb:               thumb,
			BestQualityRank:     row.BestQualityRank,
			LocalTrackCount:     row.LocalTrackCount,
			IsPreferred:         row.AlbumVariantID == preferredID,
			IsExplicitPreferred: row.AlbumVariantID == explicitPreferredID,
		})
	}
	return apitypes.Page[apitypes.AlbumVariantItem]{Items: out, Page: pageInfo}, nil
}

func (s *CatalogService) SetPreferredRecordingVariant(ctx context.Context, recordingID, variantRecordingID string) error {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return err
	}
	clusterID, ok, err := s.trackClusterIDForVariant(ctx, local.LibraryID, strings.TrimSpace(recordingID))
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("recording cluster not found")
	}
	variantRecordingID, ok, err = s.explicitRecordingVariantID(ctx, local.LibraryID, local.DeviceID, strings.TrimSpace(variantRecordingID))
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("chosen recording is not in the same cluster")
	}
	chosenClusterID, ok, err := s.trackClusterIDForVariant(ctx, local.LibraryID, strings.TrimSpace(variantRecordingID))
	if err != nil {
		return err
	}
	if !ok || chosenClusterID != clusterID {
		return fmt.Errorf("chosen recording is not in the same cluster")
	}
	now := time.Now().UTC()
	if err := s.app.storage.Transaction(ctx, func(tx *gorm.DB) error {
		var existing DeviceVariantPreference
		err := tx.Where("library_id = ? AND device_id = ? AND scope_type = ? AND cluster_id = ?", local.LibraryID, local.DeviceID, "track", clusterID).
			Take(&existing).Error
		if err != nil && err != gorm.ErrRecordNotFound {
			return err
		}
		if err == nil && strings.TrimSpace(existing.ChosenVariantID) == strings.TrimSpace(variantRecordingID) {
			return nil
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "library_id"}, {Name: "device_id"}, {Name: "scope_type"}, {Name: "cluster_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"chosen_variant_id", "updated_at"}),
		}).Create(&DeviceVariantPreference{
			LibraryID:       local.LibraryID,
			DeviceID:        local.DeviceID,
			ScopeType:       "track",
			ClusterID:       clusterID,
			ChosenVariantID: strings.TrimSpace(variantRecordingID),
			UpdatedAt:       now,
		}).Error; err != nil {
			return err
		}
		_, err = s.app.appendLocalOplogTx(tx, local, entityTypeDeviceVariantPreference, deviceVariantPreferenceEntityID(local.DeviceID, "track", clusterID), "upsert", deviceVariantPreferenceOplogPayload{
			DeviceID:        local.DeviceID,
			ScopeType:       "track",
			ClusterID:       clusterID,
			ChosenVariantID: strings.TrimSpace(variantRecordingID),
			UpdatedAtNS:     now.UnixNano(),
		})
		return err
	}); err != nil {
		return err
	}
	s.app.emitCatalogChange(apitypes.CatalogChangeEvent{
		Kind:         apitypes.CatalogChangeInvalidateBase,
		Entity:       apitypes.CatalogChangeEntityTracks,
		QueryKey:     "tracks",
		RecordingIDs: []string{strings.TrimSpace(recordingID), strings.TrimSpace(variantRecordingID)},
	})
	return nil
}

func (s *CatalogService) SetPreferredAlbumVariant(ctx context.Context, albumID, variantAlbumID string) error {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return err
	}
	clusterID, ok, err := s.albumClusterIDForVariant(ctx, local.LibraryID, strings.TrimSpace(albumID))
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("album cluster not found")
	}
	variantAlbumID, ok, err = s.explicitAlbumVariantID(ctx, local.LibraryID, local.DeviceID, strings.TrimSpace(variantAlbumID))
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("chosen album is not in the same cluster")
	}
	chosenClusterID, ok, err := s.albumClusterIDForVariant(ctx, local.LibraryID, strings.TrimSpace(variantAlbumID))
	if err != nil {
		return err
	}
	if !ok || chosenClusterID != clusterID {
		return fmt.Errorf("chosen album is not in the same cluster")
	}
	now := time.Now().UTC()
	if err := s.app.storage.Transaction(ctx, func(tx *gorm.DB) error {
		var existing DeviceVariantPreference
		err := tx.Where("library_id = ? AND device_id = ? AND scope_type = ? AND cluster_id = ?", local.LibraryID, local.DeviceID, "album", clusterID).
			Take(&existing).Error
		if err != nil && err != gorm.ErrRecordNotFound {
			return err
		}
		if err == nil && strings.TrimSpace(existing.ChosenVariantID) == strings.TrimSpace(variantAlbumID) {
			return nil
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "library_id"}, {Name: "device_id"}, {Name: "scope_type"}, {Name: "cluster_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"chosen_variant_id", "updated_at"}),
		}).Create(&DeviceVariantPreference{
			LibraryID:       local.LibraryID,
			DeviceID:        local.DeviceID,
			ScopeType:       "album",
			ClusterID:       clusterID,
			ChosenVariantID: strings.TrimSpace(variantAlbumID),
			UpdatedAt:       now,
		}).Error; err != nil {
			return err
		}
		_, err = s.app.appendLocalOplogTx(tx, local, entityTypeDeviceVariantPreference, deviceVariantPreferenceEntityID(local.DeviceID, "album", clusterID), "upsert", deviceVariantPreferenceOplogPayload{
			DeviceID:        local.DeviceID,
			ScopeType:       "album",
			ClusterID:       clusterID,
			ChosenVariantID: strings.TrimSpace(variantAlbumID),
			UpdatedAtNS:     now.UnixNano(),
		})
		return err
	}); err != nil {
		return err
	}
	s.app.emitCatalogChange(apitypes.CatalogChangeEvent{
		Kind:          apitypes.CatalogChangeInvalidateBase,
		Entity:        apitypes.CatalogChangeEntityAlbums,
		QueryKey:      "albums",
		AlbumIDs:      []string{strings.TrimSpace(albumID), strings.TrimSpace(variantAlbumID)},
		InvalidateAll: true,
	})
	s.app.emitCatalogChange(apitypes.CatalogChangeEvent{
		Kind:          apitypes.CatalogChangeInvalidateBase,
		Entity:        apitypes.CatalogChangeEntityArtistAlbums,
		InvalidateAll: true,
		AlbumIDs:      []string{strings.TrimSpace(albumID), strings.TrimSpace(variantAlbumID)},
	})
	return nil
}

func (s *CatalogService) ListAlbumTracks(ctx context.Context, req apitypes.AlbumTrackListRequest) (apitypes.Page[apitypes.AlbumTrackItem], error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return apitypes.Page[apitypes.AlbumTrackItem]{}, err
	}
	type row struct {
		RecordingID    string
		TrackClusterID string
		Title          string
		DurationMS     int64
		DiscNo         int
		TrackNo        int
		ArtistsCSV     string
	}
	query := `
SELECT
	at.track_variant_id AS recording_id,
	r.track_cluster_id AS track_cluster_id,
	r.title,
	r.duration_ms,
	at.disc_no,
	at.track_no,
	COALESCE(GROUP_CONCAT(ar.name, '` + artistSeparator + `'), '') AS artists_csv
FROM album_tracks at
JOIN track_variants r ON r.library_id = at.library_id AND r.track_variant_id = at.track_variant_id
LEFT JOIN credits c ON c.library_id = r.library_id AND c.entity_type = 'track' AND c.entity_id = r.track_variant_id
LEFT JOIN artists ar ON ar.library_id = c.library_id AND ar.artist_id = c.artist_id
WHERE at.library_id = ? AND at.album_variant_id = ?
GROUP BY at.track_variant_id, r.track_cluster_id, r.title, r.duration_ms, at.disc_no, at.track_no
ORDER BY at.disc_no ASC, at.track_no ASC, at.track_variant_id ASC`
	var rows []row
	if err := s.app.storage.ReadWithContext(ctx).Raw(query, local.LibraryID, strings.TrimSpace(req.AlbumID)).Scan(&rows).Error; err != nil {
		return apitypes.Page[apitypes.AlbumTrackItem]{}, err
	}
	paged, pageInfo := pageItems(rows, req.PageRequest)
	out := make([]apitypes.AlbumTrackItem, 0, len(paged))
	for _, row := range paged {
		out = append(out, apitypes.AlbumTrackItem{
			LibraryRecordingID: row.TrackClusterID,
			VariantRecordingID: row.RecordingID,
			RecordingID:        row.RecordingID,
			Title:              row.Title,
			DurationMS:         row.DurationMS,
			DiscNo:             row.DiscNo,
			TrackNo:            row.TrackNo,
			Artists:            splitArtists(row.ArtistsCSV),
		})
	}
	return apitypes.Page[apitypes.AlbumTrackItem]{Items: out, Page: pageInfo}, nil
}

func (s *CatalogService) ListPlaylists(ctx context.Context, req apitypes.PlaylistListRequest) (apitypes.Page[apitypes.PlaylistListItem], error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return apitypes.Page[apitypes.PlaylistListItem]{}, err
	}
	type row struct {
		PlaylistID     string
		Name           string
		Kind           string
		CreatedBy      string
		UpdatedAt      time.Time
		ItemCount      int64
		HasCustomCover int
		ScopePinned    int
	}
	profile := strings.TrimSpace(s.app.cfg.TranscodeProfile)
	aliasProfile := normalizedPlaybackProfileAlias(profile)
	query := `
SELECT
	p.playlist_id,
	p.name,
	p.kind,
	p.created_by,
	p.updated_at,
	COUNT(DISTINCT pi.item_id) AS item_count,
	MAX(CASE WHEN aw.scope_id IS NULL THEN 0 ELSE 1 END) AS has_custom_cover,
	MAX(CASE WHEN op.scope_id IS NULL THEN 0 ELSE 1 END) AS scope_pinned
FROM playlists p
LEFT JOIN playlist_items pi ON pi.library_id = p.library_id AND pi.playlist_id = p.playlist_id AND pi.deleted_at IS NULL
LEFT JOIN artwork_variants aw ON aw.library_id = p.library_id AND aw.scope_type = 'playlist' AND aw.scope_id = p.playlist_id
LEFT JOIN pin_roots op ON op.library_id = p.library_id AND op.device_id = ? AND op.scope = 'playlist' AND op.scope_id = p.playlist_id AND (op.profile = ? OR op.profile = ?)
WHERE p.library_id = ? AND p.deleted_at IS NULL
GROUP BY p.playlist_id, p.name, p.kind, p.created_by, p.updated_at
ORDER BY CASE WHEN p.kind = ? THEN 0 ELSE 1 END ASC, LOWER(p.name) ASC, p.playlist_id ASC`
	var rows []row
	if err := s.app.storage.ReadWithContext(ctx).Raw(query, local.DeviceID, profile, aliasProfile, local.LibraryID, playlistKindLiked).Scan(&rows).Error; err != nil {
		return apitypes.Page[apitypes.PlaylistListItem]{}, err
	}
	out := make([]apitypes.PlaylistListItem, 0, len(rows))
	for _, row := range rows {
		thumb, _, thumbErr := s.loadPlaylistArtworkRef(ctx, local.LibraryID, row.PlaylistID)
		if thumbErr != nil {
			return apitypes.Page[apitypes.PlaylistListItem]{}, thumbErr
		}
		out = append(out, apitypes.PlaylistListItem{
			PlaylistID:     row.PlaylistID,
			Name:           row.Name,
			Kind:           apitypes.PlaylistKind(row.Kind),
			IsReserved:     strings.EqualFold(strings.TrimSpace(row.Kind), playlistKindLiked),
			ScopePinned:    row.ScopePinned > 0,
			Thumb:          thumb,
			HasCustomCover: row.HasCustomCover > 0,
			CreatedBy:      row.CreatedBy,
			UpdatedAt:      row.UpdatedAt,
			ItemCount:      row.ItemCount,
		})
	}
	return paginateItems(out, req.PageRequest), nil
}

func (s *CatalogService) GetPlaylistSummary(ctx context.Context, playlistID string) (apitypes.PlaylistListItem, error) {
	page, err := s.ListPlaylists(ctx, apitypes.PlaylistListRequest{PageRequest: apitypes.PageRequest{Limit: maxPageLimit}})
	if err != nil {
		return apitypes.PlaylistListItem{}, err
	}
	playlistID = strings.TrimSpace(playlistID)
	for _, item := range page.Items {
		if item.PlaylistID == playlistID {
			return item, nil
		}
	}
	return apitypes.PlaylistListItem{}, fmt.Errorf("playlist %s not found", playlistID)
}

func (s *CatalogService) ListPlaylistTracks(ctx context.Context, req apitypes.PlaylistTrackListRequest) (apitypes.Page[apitypes.PlaylistTrackItem], error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return apitypes.Page[apitypes.PlaylistTrackItem]{}, err
	}
	seeds, pageInfo, err := s.listPlaylistSourceSeedsPage(ctx, local.LibraryID, strings.TrimSpace(req.PlaylistID), req.PageRequest)
	if err != nil {
		return apitypes.Page[apitypes.PlaylistTrackItem]{}, err
	}
	out, err := s.buildPlaylistTrackItems(ctx, local.LibraryID, local.DeviceID, seeds)
	if err != nil {
		return apitypes.Page[apitypes.PlaylistTrackItem]{}, err
	}
	return apitypes.Page[apitypes.PlaylistTrackItem]{Items: out, Page: pageInfo}, nil
}

func (s *CatalogService) ListPlaylistTracksCursor(ctx context.Context, req apitypes.PlaylistTrackCursorRequest) (apitypes.CursorPage[apitypes.PlaylistTrackItem], error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return apitypes.CursorPage[apitypes.PlaylistTrackItem]{}, err
	}
	seeds, pageInfo, err := s.listPlaylistSourceSeedsCursor(ctx, local.LibraryID, strings.TrimSpace(req.PlaylistID), req.CursorPageRequest)
	if err != nil {
		return apitypes.CursorPage[apitypes.PlaylistTrackItem]{}, err
	}
	items, err := s.buildPlaylistTrackItems(ctx, local.LibraryID, local.DeviceID, seeds)
	if err != nil {
		return apitypes.CursorPage[apitypes.PlaylistTrackItem]{}, err
	}
	return apitypes.CursorPage[apitypes.PlaylistTrackItem]{
		Items: items,
		Page: apitypes.CursorPageInfo{
			Limit:      pageInfo.Limit,
			Returned:   len(items),
			HasMore:    pageInfo.HasMore,
			NextCursor: pageInfo.NextCursor,
		},
	}, nil
}

func (s *CatalogService) ListLikedRecordings(ctx context.Context, req apitypes.LikedRecordingListRequest) (apitypes.Page[apitypes.LikedRecordingItem], error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return apitypes.Page[apitypes.LikedRecordingItem]{}, err
	}
	seeds, pageInfo, err := s.listLikedSourceSeedsPage(ctx, local.LibraryID, req.PageRequest)
	if err != nil {
		return apitypes.Page[apitypes.LikedRecordingItem]{}, err
	}
	out, err := s.buildLikedRecordingItems(ctx, local.LibraryID, local.DeviceID, seeds)
	if err != nil {
		return apitypes.Page[apitypes.LikedRecordingItem]{}, err
	}
	return apitypes.Page[apitypes.LikedRecordingItem]{Items: out, Page: pageInfo}, nil
}

func (s *CatalogService) ListLikedRecordingsCursor(ctx context.Context, req apitypes.LikedRecordingCursorRequest) (apitypes.CursorPage[apitypes.LikedRecordingItem], error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return apitypes.CursorPage[apitypes.LikedRecordingItem]{}, err
	}
	seeds, pageInfo, err := s.listLikedSourceSeedsCursor(ctx, local.LibraryID, req.CursorPageRequest)
	if err != nil {
		return apitypes.CursorPage[apitypes.LikedRecordingItem]{}, err
	}
	items, err := s.buildLikedRecordingItems(ctx, local.LibraryID, local.DeviceID, seeds)
	if err != nil {
		return apitypes.CursorPage[apitypes.LikedRecordingItem]{}, err
	}
	return apitypes.CursorPage[apitypes.LikedRecordingItem]{
		Items: items,
		Page: apitypes.CursorPageInfo{
			Limit:      pageInfo.Limit,
			Returned:   len(items),
			HasMore:    pageInfo.HasMore,
			NextCursor: pageInfo.NextCursor,
		},
	}, nil
}

func (s *CatalogService) listCollapsedAlbums(ctx context.Context, libraryID, deviceID string, req apitypes.PageRequest, query string, args ...any) (apitypes.Page[apitypes.AlbumListItem], error) {
	var rows []albumSeedRow
	if err := s.app.storage.ReadWithContext(ctx).Raw(query, args...).Scan(&rows).Error; err != nil {
		return apitypes.Page[apitypes.AlbumListItem]{}, err
	}
	seeds := make([]albumSeedRow, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for _, item := range rows {
		groupID := strings.TrimSpace(item.AlbumClusterID)
		if groupID == "" {
			groupID = item.AlbumID
		}
		if _, ok := seen[groupID]; ok {
			continue
		}
		seen[groupID] = struct{}{}
		seeds = append(seeds, albumSeedRow{AlbumID: item.AlbumID, AlbumClusterID: groupID})
	}
	return s.listCollapsedAlbumsForSeeds(ctx, libraryID, deviceID, seeds, req)
}

func (s *CatalogService) listCollapsedRecordings(ctx context.Context, libraryID, deviceID string, seeds []recordingSeedRow, pageInfo apitypes.PageInfo) (apitypes.Page[apitypes.RecordingListItem], error) {
	clusterIDs := make([]string, 0, len(seeds))
	for _, seed := range seeds {
		clusterIDs = append(clusterIDs, seed.TrackClusterID)
	}
	chosenByCluster, err := s.choosePreferredRecordingVariantsForClusters(ctx, libraryID, deviceID, clusterIDs)
	if err != nil {
		return apitypes.Page[apitypes.RecordingListItem]{}, err
	}

	out := make([]apitypes.RecordingListItem, 0, len(seeds))
	for _, seed := range seeds {
		chosen, ok := chosenByCluster[strings.TrimSpace(seed.TrackClusterID)]
		if !ok {
			continue
		}
		out = append(out, apitypes.RecordingListItem{
			LibraryRecordingID:          strings.TrimSpace(seed.TrackClusterID),
			PreferredVariantRecordingID: chosen.Chosen.TrackVariantID,
			TrackClusterID:              strings.TrimSpace(seed.TrackClusterID),
			RecordingID:                 strings.TrimSpace(seed.TrackClusterID),
			AlbumID:                     strings.TrimSpace(chosen.Chosen.AlbumVariantID),
			Title:                       chosen.Chosen.Title,
			DurationMS:                  chosen.Chosen.DurationMS,
			Artists:                     append([]string(nil), chosen.Chosen.Artists...),
			VariantCount:                int64(chosen.VariantCount),
			HasVariants:                 chosen.VariantCount > 1,
		})
	}
	pageInfo.Returned = len(out)
	return apitypes.Page[apitypes.RecordingListItem]{Items: out, Page: pageInfo}, nil
}

func (s *CatalogService) listTrackSourceSeedsPage(
	ctx context.Context,
	libraryID string,
	req apitypes.PageRequest,
) ([]recordingSeedRow, apitypes.PageInfo, error) {
	limit, offset := normalizePageRequest(req)
	var total int64
	countQuery := `
SELECT COUNT(*) FROM (
	SELECT COALESCE(NULLIF(r.track_cluster_id, ''), r.track_variant_id) AS track_cluster_id
	FROM track_variants r
	WHERE r.library_id = ?
	GROUP BY COALESCE(NULLIF(r.track_cluster_id, ''), r.track_variant_id)
) grouped`
	if err := s.app.storage.ReadWithContext(ctx).Raw(countQuery, libraryID).Scan(&total).Error; err != nil {
		return nil, apitypes.PageInfo{}, err
	}
	query := `
SELECT grouped.track_cluster_id, grouped.sort_title
FROM (
	SELECT
		COALESCE(NULLIF(r.track_cluster_id, ''), r.track_variant_id) AS track_cluster_id,
		MIN(LOWER(r.title)) AS sort_title
	FROM track_variants r
	WHERE r.library_id = ?
	GROUP BY COALESCE(NULLIF(r.track_cluster_id, ''), r.track_variant_id)
) grouped
ORDER BY grouped.sort_title ASC, grouped.track_cluster_id ASC
LIMIT ? OFFSET ?`
	var rows []trackSourceSeedRow
	if err := s.app.storage.ReadWithContext(ctx).Raw(query, libraryID, limit, offset).Scan(&rows).Error; err != nil {
		return nil, apitypes.PageInfo{}, err
	}
	seeds := make([]recordingSeedRow, 0, len(rows))
	for _, row := range rows {
		seeds = append(seeds, recordingSeedRow{TrackClusterID: strings.TrimSpace(row.TrackClusterID)})
	}
	return seeds, newOffsetPageInfo(limit, offset, len(seeds), int(total)), nil
}

func (s *CatalogService) listTrackSourceSeedsCursor(
	ctx context.Context,
	libraryID string,
	req apitypes.CursorPageRequest,
) ([]recordingSeedRow, apitypes.CursorPageInfo, error) {
	limit := normalizeCursorPageRequest(req)
	args := []any{libraryID}
	whereClause := ""
	if sortTitle, trackClusterID, ok, err := decodeCatalogCursorPair(req.Cursor); err != nil {
		return nil, apitypes.CursorPageInfo{}, err
	} else if ok {
		whereClause = "WHERE grouped.sort_title > ? OR (grouped.sort_title = ? AND grouped.track_cluster_id > ?)"
		args = append(args, sortTitle, sortTitle, trackClusterID)
	}
	query := `
SELECT grouped.track_cluster_id, grouped.sort_title
FROM (
	SELECT
		COALESCE(NULLIF(r.track_cluster_id, ''), r.track_variant_id) AS track_cluster_id,
		MIN(LOWER(r.title)) AS sort_title
	FROM track_variants r
	WHERE r.library_id = ?
	GROUP BY COALESCE(NULLIF(r.track_cluster_id, ''), r.track_variant_id)
) grouped
` + whereClause + `
ORDER BY grouped.sort_title ASC, grouped.track_cluster_id ASC
LIMIT ?`
	args = append(args, limit+1)
	var rows []trackSourceSeedRow
	if err := s.app.storage.ReadWithContext(ctx).Raw(query, args...).Scan(&rows).Error; err != nil {
		return nil, apitypes.CursorPageInfo{}, err
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	seeds := make([]recordingSeedRow, 0, len(rows))
	for _, row := range rows {
		seeds = append(seeds, recordingSeedRow{TrackClusterID: strings.TrimSpace(row.TrackClusterID)})
	}
	nextCursor := ""
	if hasMore && len(rows) > 0 {
		last := rows[len(rows)-1]
		nextCursor = encodeCatalogCursor(last.SortTitle, last.TrackClusterID)
	}
	return seeds, apitypes.CursorPageInfo{
		Limit:      limit,
		Returned:   len(seeds),
		HasMore:    hasMore,
		NextCursor: nextCursor,
	}, nil
}

func (s *CatalogService) listPlaylistSourceSeedsPage(
	ctx context.Context,
	libraryID string,
	playlistID string,
	req apitypes.PageRequest,
) ([]playlistTrackSeedRow, apitypes.PageInfo, error) {
	limit, offset := normalizePageRequest(req)
	playlistID = strings.TrimSpace(playlistID)
	isLikedPlaylist := playlistID == likedPlaylistIDForLibrary(libraryID)
	countQuery := `
SELECT COUNT(*)
FROM playlist_items pi
JOIN playlists p ON p.library_id = pi.library_id AND p.playlist_id = pi.playlist_id
WHERE pi.library_id = ? AND pi.playlist_id = ? AND pi.deleted_at IS NULL AND p.deleted_at IS NULL`
	var total int64
	if err := s.app.storage.ReadWithContext(ctx).Raw(countQuery, libraryID, playlistID).Scan(&total).Error; err != nil {
		return nil, apitypes.PageInfo{}, err
	}
	query := `
SELECT
	pi.item_id,
	pi.track_variant_id AS library_recording_id,
	pi.added_at,
	pi.position_key
FROM playlist_items pi
JOIN playlists p ON p.library_id = pi.library_id AND p.playlist_id = pi.playlist_id
WHERE pi.library_id = ? AND pi.playlist_id = ? AND pi.deleted_at IS NULL AND p.deleted_at IS NULL
ORDER BY pi.position_key ASC, pi.item_id ASC
LIMIT ? OFFSET ?`
	if isLikedPlaylist {
		query = `
SELECT
	pi.item_id,
	pi.track_variant_id AS library_recording_id,
	pi.added_at,
	pi.position_key
FROM playlist_items pi
JOIN playlists p ON p.library_id = pi.library_id AND p.playlist_id = pi.playlist_id
WHERE pi.library_id = ? AND pi.playlist_id = ? AND pi.deleted_at IS NULL AND p.deleted_at IS NULL
ORDER BY pi.added_at DESC, pi.item_id DESC
LIMIT ? OFFSET ?`
	}
	var seeds []playlistTrackSeedRow
	if err := s.app.storage.ReadWithContext(ctx).Raw(query, libraryID, playlistID, limit, offset).Scan(&seeds).Error; err != nil {
		return nil, apitypes.PageInfo{}, err
	}
	return seeds, newOffsetPageInfo(limit, offset, len(seeds), int(total)), nil
}

func (s *CatalogService) listPlaylistSourceSeedsCursor(
	ctx context.Context,
	libraryID string,
	playlistID string,
	req apitypes.CursorPageRequest,
) ([]playlistTrackSeedRow, apitypes.CursorPageInfo, error) {
	limit := normalizeCursorPageRequest(req)
	playlistID = strings.TrimSpace(playlistID)
	isLikedPlaylist := playlistID == likedPlaylistIDForLibrary(libraryID)
	args := []any{libraryID, playlistID}
	whereClause := ""
	if isLikedPlaylist {
		if addedAt, itemID, ok, err := decodeCatalogCursorTimePair(req.Cursor); err != nil {
			return nil, apitypes.CursorPageInfo{}, err
		} else if ok {
			whereClause = " AND (pi.added_at < ? OR (pi.added_at = ? AND pi.item_id < ?))"
			args = append(args, addedAt, addedAt, itemID)
		}
	} else {
		if positionKey, itemID, ok, err := decodeCatalogCursorPair(req.Cursor); err != nil {
			return nil, apitypes.CursorPageInfo{}, err
		} else if ok {
			whereClause = " AND (pi.position_key > ? OR (pi.position_key = ? AND pi.item_id > ?))"
			args = append(args, positionKey, positionKey, itemID)
		}
	}
	query := `
SELECT
	pi.item_id,
	pi.track_variant_id AS library_recording_id,
	pi.added_at,
	pi.position_key
FROM playlist_items pi
JOIN playlists p ON p.library_id = pi.library_id AND p.playlist_id = pi.playlist_id
WHERE pi.library_id = ? AND pi.playlist_id = ? AND pi.deleted_at IS NULL AND p.deleted_at IS NULL` + whereClause + `
ORDER BY pi.position_key ASC, pi.item_id ASC
LIMIT ?`
	if isLikedPlaylist {
		query = `
SELECT
	pi.item_id,
	pi.track_variant_id AS library_recording_id,
	pi.added_at,
	pi.position_key
FROM playlist_items pi
JOIN playlists p ON p.library_id = pi.library_id AND p.playlist_id = pi.playlist_id
WHERE pi.library_id = ? AND pi.playlist_id = ? AND pi.deleted_at IS NULL AND p.deleted_at IS NULL` + whereClause + `
ORDER BY pi.added_at DESC, pi.item_id DESC
LIMIT ?`
	}
	args = append(args, limit+1)
	var seeds []playlistTrackSeedRow
	if err := s.app.storage.ReadWithContext(ctx).Raw(query, args...).Scan(&seeds).Error; err != nil {
		return nil, apitypes.CursorPageInfo{}, err
	}
	hasMore := len(seeds) > limit
	if hasMore {
		seeds = seeds[:limit]
	}
	nextCursor := ""
	if hasMore && len(seeds) > 0 {
		last := seeds[len(seeds)-1]
		if isLikedPlaylist {
			nextCursor = encodeCatalogCursor(last.AddedAt.UTC().Format(time.RFC3339Nano), last.ItemID)
		} else {
			nextCursor = encodeCatalogCursor(last.PositionKey, last.ItemID)
		}
	}
	return seeds, apitypes.CursorPageInfo{
		Limit:      limit,
		Returned:   len(seeds),
		HasMore:    hasMore,
		NextCursor: nextCursor,
	}, nil
}

func (s *CatalogService) listLikedSourceSeedsPage(
	ctx context.Context,
	libraryID string,
	req apitypes.PageRequest,
) ([]likedRecordingSeedRow, apitypes.PageInfo, error) {
	limit, offset := normalizePageRequest(req)
	likedPlaylistID := likedPlaylistIDForLibrary(libraryID)
	countQuery := `
SELECT COUNT(*)
FROM playlist_items pi
JOIN playlists p ON p.library_id = pi.library_id AND p.playlist_id = pi.playlist_id
WHERE pi.library_id = ? AND pi.playlist_id = ? AND pi.deleted_at IS NULL AND p.deleted_at IS NULL`
	var total int64
	if err := s.app.storage.ReadWithContext(ctx).Raw(countQuery, libraryID, likedPlaylistID).Scan(&total).Error; err != nil {
		return nil, apitypes.PageInfo{}, err
	}
	query := `
SELECT
	pi.item_id,
	pi.track_variant_id AS library_recording_id,
	pi.added_at
FROM playlist_items pi
JOIN playlists p ON p.library_id = pi.library_id AND p.playlist_id = pi.playlist_id
WHERE pi.library_id = ? AND pi.playlist_id = ? AND pi.deleted_at IS NULL AND p.deleted_at IS NULL
ORDER BY pi.added_at DESC, pi.item_id DESC
LIMIT ? OFFSET ?`
	var seeds []likedRecordingSeedRow
	if err := s.app.storage.ReadWithContext(ctx).Raw(query, libraryID, likedPlaylistID, limit, offset).Scan(&seeds).Error; err != nil {
		return nil, apitypes.PageInfo{}, err
	}
	return seeds, newOffsetPageInfo(limit, offset, len(seeds), int(total)), nil
}

func (s *CatalogService) listLikedSourceSeedsCursor(
	ctx context.Context,
	libraryID string,
	req apitypes.CursorPageRequest,
) ([]likedRecordingSeedRow, apitypes.CursorPageInfo, error) {
	limit := normalizeCursorPageRequest(req)
	likedPlaylistID := likedPlaylistIDForLibrary(libraryID)
	args := []any{libraryID, likedPlaylistID}
	whereClause := ""
	if addedAt, itemID, ok, err := decodeCatalogCursorTimePair(req.Cursor); err != nil {
		return nil, apitypes.CursorPageInfo{}, err
	} else if ok {
		whereClause = " AND (pi.added_at < ? OR (pi.added_at = ? AND pi.item_id < ?))"
		args = append(args, addedAt, addedAt, itemID)
	}
	query := `
SELECT
	pi.item_id,
	pi.track_variant_id AS library_recording_id,
	pi.added_at
FROM playlist_items pi
JOIN playlists p ON p.library_id = pi.library_id AND p.playlist_id = pi.playlist_id
WHERE pi.library_id = ? AND pi.playlist_id = ? AND pi.deleted_at IS NULL AND p.deleted_at IS NULL` + whereClause + `
ORDER BY pi.added_at DESC, pi.item_id DESC
LIMIT ?`
	args = append(args, limit+1)
	var seeds []likedRecordingSeedRow
	if err := s.app.storage.ReadWithContext(ctx).Raw(query, args...).Scan(&seeds).Error; err != nil {
		return nil, apitypes.CursorPageInfo{}, err
	}
	hasMore := len(seeds) > limit
	if hasMore {
		seeds = seeds[:limit]
	}
	nextCursor := ""
	if hasMore && len(seeds) > 0 {
		last := seeds[len(seeds)-1]
		nextCursor = encodeCatalogCursor(last.AddedAt.UTC().Format(time.RFC3339Nano), last.ItemID)
	}
	return seeds, apitypes.CursorPageInfo{
		Limit:      limit,
		Returned:   len(seeds),
		HasMore:    hasMore,
		NextCursor: nextCursor,
	}, nil
}

func (s *CatalogService) buildPlaylistTrackItems(
	ctx context.Context,
	libraryID string,
	deviceID string,
	seeds []playlistTrackSeedRow,
) ([]apitypes.PlaylistTrackItem, error) {
	clusterIDs := make([]string, 0, len(seeds))
	for _, seed := range seeds {
		clusterID := strings.TrimSpace(seed.LibraryRecordingID)
		if clusterID == "" {
			continue
		}
		clusterIDs = append(clusterIDs, clusterID)
	}
	chosenByCluster, err := s.choosePreferredRecordingVariantsForClusters(ctx, libraryID, deviceID, clusterIDs)
	if err != nil {
		return nil, err
	}

	out := make([]apitypes.PlaylistTrackItem, 0, len(seeds))
	for _, seed := range seeds {
		clusterID := strings.TrimSpace(seed.LibraryRecordingID)
		chosen, ok := chosenByCluster[clusterID]
		if !ok {
			continue
		}
		out = append(out, apitypes.PlaylistTrackItem{
			ItemID:             strings.TrimSpace(seed.ItemID),
			LibraryRecordingID: clusterID,
			RecordingID:        strings.TrimSpace(chosen.Chosen.TrackVariantID),
			AlbumID:            strings.TrimSpace(chosen.Chosen.AlbumVariantID),
			Title:              chosen.Chosen.Title,
			DurationMS:         chosen.Chosen.DurationMS,
			Artists:            append([]string(nil), chosen.Chosen.Artists...),
			AddedAt:            seed.AddedAt,
		})
	}
	return out, nil
}

func (s *CatalogService) buildLikedRecordingItems(
	ctx context.Context,
	libraryID string,
	deviceID string,
	seeds []likedRecordingSeedRow,
) ([]apitypes.LikedRecordingItem, error) {
	clusterIDs := make([]string, 0, len(seeds))
	for _, seed := range seeds {
		clusterID := strings.TrimSpace(seed.LibraryRecordingID)
		if clusterID == "" {
			continue
		}
		clusterIDs = append(clusterIDs, clusterID)
	}
	chosenByCluster, err := s.choosePreferredRecordingVariantsForClusters(ctx, libraryID, deviceID, clusterIDs)
	if err != nil {
		return nil, err
	}

	out := make([]apitypes.LikedRecordingItem, 0, len(seeds))
	for _, seed := range seeds {
		clusterID := strings.TrimSpace(seed.LibraryRecordingID)
		chosen, ok := chosenByCluster[clusterID]
		if !ok {
			continue
		}
		out = append(out, apitypes.LikedRecordingItem{
			LibraryRecordingID: clusterID,
			RecordingID:        strings.TrimSpace(chosen.Chosen.TrackVariantID),
			AlbumID:            strings.TrimSpace(chosen.Chosen.AlbumVariantID),
			Title:              chosen.Chosen.Title,
			DurationMS:         chosen.Chosen.DurationMS,
			Artists:            append([]string(nil), chosen.Chosen.Artists...),
			AddedAt:            seed.AddedAt,
		})
	}
	return out, nil
}

func (s *CatalogService) choosePreferredRecordingVariantsForClusters(
	ctx context.Context,
	libraryID string,
	deviceID string,
	clusterIDs []string,
) (map[string]chosenRecordingVariant, error) {
	rowsByCluster, err := s.listRecordingVariantRowsForClusters(ctx, libraryID, deviceID, clusterIDs, s.app.cfg.TranscodeProfile)
	if err != nil {
		return nil, err
	}
	preferredByCluster, err := s.preferredRecordingVariantIDsForClusters(ctx, libraryID, deviceID, clusterIDs)
	if err != nil {
		return nil, err
	}

	out := make(map[string]chosenRecordingVariant, len(rowsByCluster))
	for _, clusterID := range clusterIDs {
		trimmedClusterID := strings.TrimSpace(clusterID)
		if trimmedClusterID == "" {
			continue
		}
		variants := rowsByCluster[trimmedClusterID]
		if len(variants) == 0 {
			continue
		}
		preferredID := chooseRecordingVariantID(variants, preferredByCluster[trimmedClusterID])
		chosen := variants[0]
		for _, variant := range variants {
			if variant.TrackVariantID == preferredID {
				chosen = variant
				break
			}
		}
		out[trimmedClusterID] = chosenRecordingVariant{
			ClusterID:    trimmedClusterID,
			Chosen:       chosen,
			VariantCount: len(variants),
		}
	}
	return out, nil
}

func (s *CatalogService) listCollapsedAlbumsForSeeds(ctx context.Context, libraryID, deviceID string, seeds []albumSeedRow, req apitypes.PageRequest) (apitypes.Page[apitypes.AlbumListItem], error) {
	pagedSeeds, pageInfo := pageItems(seeds, req)
	clusterIDs := make([]string, 0, len(pagedSeeds))
	for _, seed := range pagedSeeds {
		clusterIDs = append(clusterIDs, seed.AlbumClusterID)
	}
	rowsByCluster, err := s.listAlbumVariantRowsForClusters(ctx, libraryID, deviceID, clusterIDs)
	if err != nil {
		return apitypes.Page[apitypes.AlbumListItem]{}, err
	}
	preferredByCluster, err := s.preferredAlbumVariantIDsForClusters(ctx, libraryID, deviceID, clusterIDs)
	if err != nil {
		return apitypes.Page[apitypes.AlbumListItem]{}, err
	}

	out := make([]apitypes.AlbumListItem, 0, len(pagedSeeds))
	for _, seed := range pagedSeeds {
		variants := rowsByCluster[strings.TrimSpace(seed.AlbumClusterID)]
		if len(variants) == 0 {
			continue
		}
		explicitPreferred := preferredByCluster[strings.TrimSpace(seed.AlbumClusterID)]
		chosen := variants[0]
		for _, variant := range variants[1:] {
			if compareAlbumVariants(variant, chosen, explicitPreferred) < 0 {
				chosen = variant
			}
		}
		thumb, _ := s.loadAlbumArtworkRef(ctx, libraryID, chosen.AlbumVariantID)
		out = append(out, apitypes.AlbumListItem{
			LibraryAlbumID:          chosen.AlbumClusterID,
			PreferredVariantAlbumID: chosen.AlbumVariantID,
			AlbumID:                 chosen.AlbumClusterID,
			AlbumClusterID:          chosen.AlbumClusterID,
			Title:                   chosen.Title,
			Artists:                 append([]string(nil), chosen.Artists...),
			Year:                    chosen.Year,
			TrackCount:              chosen.TrackCount,
			Thumb:                   thumb,
			VariantCount:            int64(len(variants)),
			HasVariants:             len(variants) > 1,
		})
	}
	pageInfo.Returned = len(out)
	return apitypes.Page[apitypes.AlbumListItem]{Items: out, Page: pageInfo}, nil
}

func collapsedAlbumFromVariants(variants []apitypes.AlbumVariantItem) (apitypes.AlbumListItem, error) {
	if len(variants) == 0 {
		return apitypes.AlbumListItem{}, fmt.Errorf("album variants are required")
	}
	preferredID := chooseAlbumVariantID(append([]apitypes.AlbumVariantItem(nil), variants...), "")
	chosen := variants[0]
	for _, variant := range variants {
		if variant.IsPreferred || variant.AlbumID == preferredID {
			chosen = variant
			break
		}
	}
	return apitypes.AlbumListItem{
		LibraryAlbumID:          chosen.AlbumClusterID,
		PreferredVariantAlbumID: chosen.AlbumID,
		AlbumID:                 chosen.AlbumClusterID,
		AlbumClusterID:          chosen.AlbumClusterID,
		Title:                   chosen.Title,
		Artists:                 append([]string(nil), chosen.Artists...),
		Year:                    chosen.Year,
		TrackCount:              chosen.TrackCount,
		Thumb:                   chosen.Thumb,
		VariantCount:            int64(len(variants)),
		HasVariants:             len(variants) > 1,
	}, nil
}

func encodeCatalogCursor(parts ...string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strings.Join(parts, "\x1f")))
}

func decodeCatalogCursorPair(cursor string) (string, string, bool, error) {
	cursor = strings.TrimSpace(cursor)
	if cursor == "" {
		return "", "", false, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return "", "", false, fmt.Errorf("decode cursor: %w", err)
	}
	parts := strings.Split(string(raw), "\x1f")
	if len(parts) != 2 {
		return "", "", false, fmt.Errorf("decode cursor: invalid token")
	}
	return parts[0], parts[1], true, nil
}

func decodeCatalogCursorTimePair(cursor string) (time.Time, string, bool, error) {
	left, right, ok, err := decodeCatalogCursorPair(cursor)
	if err != nil || !ok {
		return time.Time{}, "", ok, err
	}
	parsed, err := time.Parse(time.RFC3339Nano, left)
	if err != nil {
		return time.Time{}, "", false, fmt.Errorf("decode cursor time: %w", err)
	}
	return parsed, right, true, nil
}

func (s *CatalogService) trackClusterIDForVariant(ctx context.Context, libraryID, recordingID string) (string, bool, error) {
	var row TrackVariantModel
	if err := s.app.storage.ReadWithContext(ctx).Where("library_id = ? AND track_variant_id = ?", libraryID, recordingID).Take(&row).Error; err == nil {
		return strings.TrimSpace(row.TrackClusterID), true, nil
	} else if err != gorm.ErrRecordNotFound {
		return "", false, err
	}
	if err := s.app.storage.ReadWithContext(ctx).Where("library_id = ? AND track_cluster_id = ?", libraryID, recordingID).Take(&row).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return "", false, nil
		}
		return "", false, err
	}
	return strings.TrimSpace(row.TrackClusterID), true, nil
}

func (s *CatalogService) albumClusterIDForVariant(ctx context.Context, libraryID, albumID string) (string, bool, error) {
	var row AlbumVariantModel
	if err := s.app.storage.ReadWithContext(ctx).Where("library_id = ? AND album_variant_id = ?", libraryID, albumID).Take(&row).Error; err == nil {
		return strings.TrimSpace(row.AlbumClusterID), true, nil
	} else if err != gorm.ErrRecordNotFound {
		return "", false, err
	}
	if err := s.app.storage.ReadWithContext(ctx).Where("library_id = ? AND album_cluster_id = ?", libraryID, albumID).Take(&row).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return "", false, nil
		}
		return "", false, err
	}
	return strings.TrimSpace(row.AlbumClusterID), true, nil
}

func (s *CatalogService) explicitRecordingVariantID(ctx context.Context, libraryID, deviceID, recordingID string) (string, bool, error) {
	recordingID = strings.TrimSpace(recordingID)
	if recordingID == "" {
		return "", false, nil
	}
	var row TrackVariantModel
	if err := s.app.storage.ReadWithContext(ctx).Where("library_id = ? AND track_variant_id = ?", libraryID, recordingID).Take(&row).Error; err == nil {
		return recordingID, true, nil
	} else if err != gorm.ErrRecordNotFound {
		return "", false, err
	}
	variants, err := s.listRecordingVariantsRows(ctx, libraryID, deviceID, recordingID, s.app.cfg.TranscodeProfile)
	if err != nil {
		return "", false, err
	}
	if len(variants) == 0 {
		return "", false, nil
	}
	explicitPreferredID, _, err := s.preferredRecordingVariantID(ctx, libraryID, deviceID, recordingID)
	if err != nil {
		return "", false, err
	}
	chosenID := chooseRecordingVariantID(variants, explicitPreferredID)
	if strings.TrimSpace(chosenID) == "" {
		return "", false, nil
	}
	return chosenID, true, nil
}

func (s *CatalogService) explicitAlbumVariantID(ctx context.Context, libraryID, deviceID, albumID string) (string, bool, error) {
	albumID = strings.TrimSpace(albumID)
	if albumID == "" {
		return "", false, nil
	}
	var row AlbumVariantModel
	if err := s.app.storage.ReadWithContext(ctx).Where("library_id = ? AND album_variant_id = ?", libraryID, albumID).Take(&row).Error; err == nil {
		return albumID, true, nil
	} else if err != gorm.ErrRecordNotFound {
		return "", false, err
	}
	clusterID, ok, err := s.albumClusterIDForVariant(ctx, libraryID, albumID)
	if err != nil || !ok {
		return "", false, err
	}
	rowsByCluster, err := s.listAlbumVariantRowsForClusters(ctx, libraryID, deviceID, []string{clusterID})
	if err != nil {
		return "", false, err
	}
	variants := rowsByCluster[clusterID]
	if len(variants) == 0 {
		return "", false, nil
	}
	explicitPreferredID, _, err := s.preferredAlbumVariantID(ctx, libraryID, deviceID, clusterID)
	if err != nil {
		return "", false, err
	}
	chosen := variants[0]
	for _, variant := range variants[1:] {
		if compareAlbumVariants(variant, chosen, explicitPreferredID) < 0 {
			chosen = variant
		}
	}
	if strings.TrimSpace(chosen.AlbumVariantID) == "" {
		return "", false, nil
	}
	return chosen.AlbumVariantID, true, nil
}

func (s *CatalogService) preferredRecordingVariantID(ctx context.Context, libraryID, deviceID, recordingID string) (string, bool, error) {
	clusterID, ok, err := s.trackClusterIDForVariant(ctx, libraryID, recordingID)
	if err != nil || !ok {
		return "", false, err
	}
	var pref DeviceVariantPreference
	if err := s.app.storage.ReadWithContext(ctx).
		Where("library_id = ? AND device_id = ? AND scope_type = ? AND cluster_id = ?", libraryID, deviceID, "track", clusterID).
		Take(&pref).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return "", false, nil
		}
		return "", false, err
	}
	return strings.TrimSpace(pref.ChosenVariantID), true, nil
}

func (s *CatalogService) preferredAlbumVariantID(ctx context.Context, libraryID, deviceID, albumID string) (string, bool, error) {
	clusterID, ok, err := s.albumClusterIDForVariant(ctx, libraryID, albumID)
	if err != nil || !ok {
		return "", false, err
	}
	var pref DeviceVariantPreference
	if err := s.app.storage.ReadWithContext(ctx).
		Where("library_id = ? AND device_id = ? AND scope_type = ? AND cluster_id = ?", libraryID, deviceID, "album", clusterID).
		Take(&pref).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return "", false, nil
		}
		return "", false, err
	}
	return strings.TrimSpace(pref.ChosenVariantID), true, nil
}

func (s *CatalogService) preferredRecordingVariantIDsForClusters(ctx context.Context, libraryID, deviceID string, clusterIDs []string) (map[string]string, error) {
	clusterIDs = compactNonEmptyStrings(clusterIDs)
	if len(clusterIDs) == 0 {
		return map[string]string{}, nil
	}
	type row struct {
		ClusterID       string
		ChosenVariantID string
	}
	var rows []row
	if err := s.app.storage.ReadWithContext(ctx).
		Model(&DeviceVariantPreference{}).
		Select("cluster_id, chosen_variant_id").
		Where("library_id = ? AND device_id = ? AND scope_type = ? AND cluster_id IN ?", libraryID, deviceID, "track", clusterIDs).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]string, len(rows))
	for _, row := range rows {
		out[strings.TrimSpace(row.ClusterID)] = strings.TrimSpace(row.ChosenVariantID)
	}
	return out, nil
}

func (s *CatalogService) preferredAlbumVariantIDsForClusters(ctx context.Context, libraryID, deviceID string, clusterIDs []string) (map[string]string, error) {
	clusterIDs = compactNonEmptyStrings(clusterIDs)
	if len(clusterIDs) == 0 {
		return map[string]string{}, nil
	}
	type row struct {
		ClusterID       string
		ChosenVariantID string
	}
	var rows []row
	if err := s.app.storage.ReadWithContext(ctx).
		Model(&DeviceVariantPreference{}).
		Select("cluster_id, chosen_variant_id").
		Where("library_id = ? AND device_id = ? AND scope_type = ? AND cluster_id IN ?", libraryID, deviceID, "album", clusterIDs).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]string, len(rows))
	for _, row := range rows {
		out[strings.TrimSpace(row.ClusterID)] = strings.TrimSpace(row.ChosenVariantID)
	}
	return out, nil
}

func (s *CatalogService) listRecordingVariantRowsForClusters(ctx context.Context, libraryID, deviceID string, clusterIDs []string, preferredProfile string) (map[string][]recordingVariantRow, error) {
	clusterIDs = compactNonEmptyStrings(clusterIDs)
	if len(clusterIDs) == 0 {
		return map[string][]recordingVariantRow{}, nil
	}
	query := `
SELECT
	r.track_variant_id AS track_variant_id,
	r.track_cluster_id AS track_cluster_id,
	COALESCE(MAX(sf.source_file_id), '') AS source_file_id,
	r.title,
	r.duration_ms,
	COALESCE(GROUP_CONCAT(DISTINCT ar.name), '') AS artists_csv,
	COALESCE(MIN(at.album_variant_id), '') AS album_variant_id,
	COALESCE(MIN(al.title), '') AS album_title,
	COALESCE(MIN(at.track_no), 0) AS track_no,
	COALESCE(MIN(at.disc_no), 0) AS disc_no,
	COALESCE(MAX(sf.container), '') AS container,
	COALESCE(MAX(sf.codec), '') AS codec,
	COALESCE(MAX(sf.bitrate), 0) AS bitrate,
	COALESCE(MAX(sf.sample_rate), 0) AS sample_rate,
	COALESCE(MAX(sf.channels), 0) AS channels,
	COALESCE(MAX(sf.is_lossless), 0) AS is_lossless,
	COALESCE(MAX(sf.quality_rank), 0) AS quality_rank,
	COALESCE(MAX(CASE WHEN sf.device_id = ? AND sf.is_present = 1 THEN 1 ELSE 0 END), 0) AS is_present_local,
	COALESCE(MAX(CASE WHEN dac.device_id = ? AND dac.is_cached = 1 THEN 1 ELSE 0 END), 0) AS is_cached_local,
	COALESCE(MAX(CASE WHEN sf.device_id = ? AND sf.is_present = 1 THEN sf.local_path ELSE '' END), '') AS local_path
FROM track_variants r
LEFT JOIN credits c ON c.library_id = r.library_id AND c.entity_type = 'track' AND c.entity_id = r.track_variant_id
LEFT JOIN artists ar ON ar.library_id = c.library_id AND ar.artist_id = c.artist_id
LEFT JOIN album_tracks at ON at.library_id = r.library_id AND at.track_variant_id = r.track_variant_id
LEFT JOIN album_variants al ON al.library_id = at.library_id AND al.album_variant_id = at.album_variant_id
LEFT JOIN source_files sf ON sf.library_id = r.library_id AND sf.track_variant_id = r.track_variant_id
LEFT JOIN optimized_assets oa ON oa.library_id = sf.library_id AND oa.source_file_id = sf.source_file_id AND (? = '' OR oa.profile = ?)
LEFT JOIN device_asset_caches dac ON dac.library_id = oa.library_id AND dac.optimized_asset_id = oa.optimized_asset_id
WHERE r.library_id = ? AND r.track_cluster_id IN ?
GROUP BY r.track_variant_id, r.track_cluster_id, r.title, r.duration_ms`
	type row struct {
		TrackVariantID string
		TrackClusterID string
		SourceFileID   string
		Title          string
		DurationMS     int64
		ArtistsCSV     string
		AlbumVariantID string
		AlbumTitle     string
		TrackNo        int
		DiscNo         int
		Container      string
		Codec          string
		Bitrate        int
		SampleRate     int
		Channels       int
		IsLossless     bool
		QualityRank    int
		IsPresentLocal bool
		IsCachedLocal  bool
		LocalPath      string
	}
	var rows []row
	if err := s.app.storage.ReadWithContext(ctx).Raw(query, deviceID, deviceID, deviceID, preferredProfile, preferredProfile, libraryID, clusterIDs).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string][]recordingVariantRow, len(clusterIDs))
	for _, row := range rows {
		clusterID := strings.TrimSpace(row.TrackClusterID)
		out[clusterID] = append(out[clusterID], recordingVariantRow{
			TrackVariantID: row.TrackVariantID,
			TrackClusterID: clusterID,
			SourceFileID:   row.SourceFileID,
			Title:          row.Title,
			DurationMS:     row.DurationMS,
			Artists:        splitArtists(row.ArtistsCSV),
			AlbumVariantID: row.AlbumVariantID,
			AlbumTitle:     row.AlbumTitle,
			TrackNo:        row.TrackNo,
			DiscNo:         row.DiscNo,
			Container:      row.Container,
			Codec:          row.Codec,
			Bitrate:        row.Bitrate,
			SampleRate:     row.SampleRate,
			Channels:       row.Channels,
			IsLossless:     row.IsLossless,
			QualityRank:    row.QualityRank,
			IsPresentLocal: row.IsPresentLocal,
			IsCachedLocal:  row.IsCachedLocal,
			LocalPath:      row.LocalPath,
		})
	}
	return out, nil
}

func (s *CatalogService) listAlbumVariantRowsForClusters(ctx context.Context, libraryID, deviceID string, clusterIDs []string) (map[string][]albumVariantRow, error) {
	clusterIDs = compactNonEmptyStrings(clusterIDs)
	if len(clusterIDs) == 0 {
		return map[string][]albumVariantRow{}, nil
	}
	query := `
SELECT
	a.album_variant_id AS album_variant_id,
	a.album_cluster_id AS album_cluster_id,
	a.title,
	COALESCE(GROUP_CONCAT(DISTINCT ar.name), '') AS artists_csv,
	a.year,
	a.edition,
	COUNT(DISTINCT at.track_variant_id) AS track_count,
	COALESCE(MAX(sf.quality_rank), 0) AS best_quality_rank,
	COUNT(DISTINCT CASE WHEN sf.device_id = ? AND sf.is_present = 1 THEN at.track_variant_id END) AS local_track_count
FROM album_variants a
LEFT JOIN album_tracks at ON at.library_id = a.library_id AND at.album_variant_id = a.album_variant_id
LEFT JOIN credits c ON c.library_id = a.library_id AND c.entity_type = 'album' AND c.entity_id = a.album_variant_id
LEFT JOIN artists ar ON ar.library_id = c.library_id AND ar.artist_id = c.artist_id
LEFT JOIN source_files sf ON sf.library_id = at.library_id AND sf.track_variant_id = at.track_variant_id
WHERE a.library_id = ? AND a.album_cluster_id IN ?
GROUP BY a.album_variant_id, a.album_cluster_id, a.title, a.year, a.edition`
	type row struct {
		AlbumVariantID  string
		AlbumClusterID  string
		Title           string
		ArtistsCSV      string
		Year            *int
		Edition         string
		TrackCount      int64
		BestQualityRank int
		LocalTrackCount int64
	}
	var rows []row
	if err := s.app.storage.ReadWithContext(ctx).Raw(query, deviceID, libraryID, clusterIDs).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string][]albumVariantRow, len(clusterIDs))
	for _, row := range rows {
		clusterID := strings.TrimSpace(row.AlbumClusterID)
		out[clusterID] = append(out[clusterID], albumVariantRow{
			AlbumVariantID:  row.AlbumVariantID,
			AlbumClusterID:  clusterID,
			Title:           row.Title,
			Artists:         splitArtists(row.ArtistsCSV),
			Year:            row.Year,
			Edition:         row.Edition,
			TrackCount:      row.TrackCount,
			BestQualityRank: row.BestQualityRank,
			LocalTrackCount: row.LocalTrackCount,
		})
	}
	return out, nil
}

func (s *CatalogService) listRecordingVariantsRows(ctx context.Context, libraryID, deviceID, recordingID, preferredProfile string) ([]recordingVariantRow, error) {
	clusterID, ok, err := s.trackClusterIDForVariant(ctx, libraryID, recordingID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	query := `
SELECT
	r.track_variant_id AS track_variant_id,
	r.track_cluster_id AS track_cluster_id,
	COALESCE(MAX(sf.source_file_id), '') AS source_file_id,
	r.title,
	r.duration_ms,
	COALESCE(GROUP_CONCAT(DISTINCT ar.name), '') AS artists_csv,
	COALESCE(MIN(at.album_variant_id), '') AS album_variant_id,
	COALESCE(MIN(al.title), '') AS album_title,
	COALESCE(MIN(at.track_no), 0) AS track_no,
	COALESCE(MIN(at.disc_no), 0) AS disc_no,
	COALESCE(MAX(sf.container), '') AS container,
	COALESCE(MAX(sf.codec), '') AS codec,
	COALESCE(MAX(sf.bitrate), 0) AS bitrate,
	COALESCE(MAX(sf.sample_rate), 0) AS sample_rate,
	COALESCE(MAX(sf.channels), 0) AS channels,
	COALESCE(MAX(sf.is_lossless), 0) AS is_lossless,
	COALESCE(MAX(sf.quality_rank), 0) AS quality_rank,
	COALESCE(MAX(CASE WHEN sf.device_id = ? AND sf.is_present = 1 THEN 1 ELSE 0 END), 0) AS is_present_local,
	COALESCE(MAX(CASE WHEN dac.device_id = ? AND dac.is_cached = 1 THEN 1 ELSE 0 END), 0) AS is_cached_local,
	COALESCE(MAX(CASE WHEN sf.device_id = ? AND sf.is_present = 1 THEN sf.local_path ELSE '' END), '') AS local_path
FROM track_variants r
LEFT JOIN credits c ON c.library_id = r.library_id AND c.entity_type = 'track' AND c.entity_id = r.track_variant_id
LEFT JOIN artists ar ON ar.library_id = c.library_id AND ar.artist_id = c.artist_id
LEFT JOIN album_tracks at ON at.library_id = r.library_id AND at.track_variant_id = r.track_variant_id
LEFT JOIN album_variants al ON al.library_id = at.library_id AND al.album_variant_id = at.album_variant_id
LEFT JOIN source_files sf ON sf.library_id = r.library_id AND sf.track_variant_id = r.track_variant_id
LEFT JOIN optimized_assets oa ON oa.library_id = sf.library_id AND oa.source_file_id = sf.source_file_id AND (? = '' OR oa.profile = ?)
LEFT JOIN device_asset_caches dac ON dac.library_id = oa.library_id AND dac.optimized_asset_id = oa.optimized_asset_id
WHERE r.library_id = ? AND r.track_cluster_id = ?
GROUP BY r.track_variant_id, r.track_cluster_id, r.title, r.duration_ms`
	type row struct {
		TrackVariantID string
		TrackClusterID string
		SourceFileID   string
		Title          string
		DurationMS     int64
		ArtistsCSV     string
		AlbumVariantID string
		AlbumTitle     string
		TrackNo        int
		DiscNo         int
		Container      string
		Codec          string
		Bitrate        int
		SampleRate     int
		Channels       int
		IsLossless     bool
		QualityRank    int
		IsPresentLocal bool
		IsCachedLocal  bool
		LocalPath      string
	}
	var rows []row
	if err := s.app.storage.ReadWithContext(ctx).Raw(query, deviceID, deviceID, deviceID, preferredProfile, preferredProfile, libraryID, clusterID).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]recordingVariantRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, recordingVariantRow{
			TrackVariantID: row.TrackVariantID,
			TrackClusterID: row.TrackClusterID,
			SourceFileID:   row.SourceFileID,
			Title:          row.Title,
			DurationMS:     row.DurationMS,
			Artists:        splitArtists(row.ArtistsCSV),
			AlbumVariantID: row.AlbumVariantID,
			AlbumTitle:     row.AlbumTitle,
			TrackNo:        row.TrackNo,
			DiscNo:         row.DiscNo,
			Container:      row.Container,
			Codec:          row.Codec,
			Bitrate:        row.Bitrate,
			SampleRate:     row.SampleRate,
			Channels:       row.Channels,
			IsLossless:     row.IsLossless,
			QualityRank:    row.QualityRank,
			IsPresentLocal: row.IsPresentLocal,
			IsCachedLocal:  row.IsCachedLocal,
			LocalPath:      row.LocalPath,
		})
	}
	explicitPreferredID, _, err := s.preferredRecordingVariantID(ctx, libraryID, deviceID, recordingID)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(out, func(i, j int) bool { return compareRecordingVariants(out[i], out[j], explicitPreferredID) < 0 })
	return out, nil
}

func (s *CatalogService) listAlbumVariantsRows(ctx context.Context, libraryID, deviceID, albumID string) ([]albumVariantRow, error) {
	clusterID, ok, err := s.albumClusterIDForVariant(ctx, libraryID, albumID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	query := `
SELECT
	a.album_variant_id AS album_variant_id,
	a.album_cluster_id AS album_cluster_id,
	a.title,
	COALESCE(GROUP_CONCAT(DISTINCT ar.name), '') AS artists_csv,
	a.year,
	a.edition,
	COUNT(DISTINCT at.track_variant_id) AS track_count,
	COALESCE(MAX(sf.quality_rank), 0) AS best_quality_rank,
	COUNT(DISTINCT CASE WHEN sf.device_id = ? AND sf.is_present = 1 THEN at.track_variant_id END) AS local_track_count
FROM album_variants a
LEFT JOIN album_tracks at ON at.library_id = a.library_id AND at.album_variant_id = a.album_variant_id
LEFT JOIN credits c ON c.library_id = a.library_id AND c.entity_type = 'album' AND c.entity_id = a.album_variant_id
LEFT JOIN artists ar ON ar.library_id = c.library_id AND ar.artist_id = c.artist_id
LEFT JOIN source_files sf ON sf.library_id = at.library_id AND sf.track_variant_id = at.track_variant_id
WHERE a.library_id = ? AND a.album_cluster_id = ?
GROUP BY a.album_variant_id, a.album_cluster_id, a.title, a.year, a.edition`
	type row struct {
		AlbumVariantID  string
		AlbumClusterID  string
		Title           string
		ArtistsCSV      string
		Year            *int
		Edition         string
		TrackCount      int64
		BestQualityRank int
		LocalTrackCount int64
	}
	var rows []row
	if err := s.app.storage.ReadWithContext(ctx).Raw(query, deviceID, libraryID, clusterID).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]albumVariantRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, albumVariantRow{
			AlbumVariantID:  row.AlbumVariantID,
			AlbumClusterID:  row.AlbumClusterID,
			Title:           row.Title,
			Artists:         splitArtists(row.ArtistsCSV),
			Year:            row.Year,
			Edition:         row.Edition,
			TrackCount:      row.TrackCount,
			BestQualityRank: row.BestQualityRank,
			LocalTrackCount: row.LocalTrackCount,
		})
	}
	explicitPreferredID, _, err := s.preferredAlbumVariantID(ctx, libraryID, deviceID, albumID)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(out, func(i, j int) bool { return compareAlbumVariants(out[i], out[j], explicitPreferredID) < 0 })
	return out, nil
}

func chooseRecordingVariantID(variants []recordingVariantRow, explicitPreferredID string) string {
	if len(variants) == 0 {
		return ""
	}
	best := variants[0]
	for i := 1; i < len(variants); i++ {
		if compareRecordingVariants(variants[i], best, explicitPreferredID) < 0 {
			best = variants[i]
		}
	}
	return best.TrackVariantID
}

func compareRecordingVariants(left, right recordingVariantRow, explicitPreferredID string) int {
	if explicitPreferredID != "" {
		if left.TrackVariantID == explicitPreferredID && right.TrackVariantID != explicitPreferredID {
			return -1
		}
		if right.TrackVariantID == explicitPreferredID && left.TrackVariantID != explicitPreferredID {
			return 1
		}
	}
	if left.IsPresentLocal != right.IsPresentLocal {
		if left.IsPresentLocal {
			return -1
		}
		return 1
	}
	if left.IsCachedLocal != right.IsCachedLocal {
		if left.IsCachedLocal {
			return -1
		}
		return 1
	}
	if left.QualityRank != right.QualityRank {
		if left.QualityRank > right.QualityRank {
			return -1
		}
		return 1
	}
	if left.Bitrate != right.Bitrate {
		if left.Bitrate > right.Bitrate {
			return -1
		}
		return 1
	}
	if left.TrackVariantID < right.TrackVariantID {
		return -1
	}
	if left.TrackVariantID > right.TrackVariantID {
		return 1
	}
	return 0
}

func chooseAlbumVariantID(variants []apitypes.AlbumVariantItem, preferredID string) string {
	if preferredID != "" {
		for _, variant := range variants {
			if variant.AlbumID == preferredID {
				return preferredID
			}
		}
	}
	sort.SliceStable(variants, func(i, j int) bool {
		if variants[i].LocalTrackCount != variants[j].LocalTrackCount {
			return variants[i].LocalTrackCount > variants[j].LocalTrackCount
		}
		if variants[i].TrackCount != variants[j].TrackCount {
			return variants[i].TrackCount > variants[j].TrackCount
		}
		if variants[i].BestQualityRank != variants[j].BestQualityRank {
			return variants[i].BestQualityRank > variants[j].BestQualityRank
		}
		if !strings.EqualFold(variants[i].Title, variants[j].Title) {
			return strings.ToLower(variants[i].Title) < strings.ToLower(variants[j].Title)
		}
		return variants[i].AlbumID < variants[j].AlbumID
	})
	if len(variants) == 0 {
		return ""
	}
	return variants[0].AlbumID
}

func compareAlbumVariants(left, right albumVariantRow, explicitPreferredID string) int {
	if explicitPreferredID != "" {
		if left.AlbumVariantID == explicitPreferredID && right.AlbumVariantID != explicitPreferredID {
			return -1
		}
		if right.AlbumVariantID == explicitPreferredID && left.AlbumVariantID != explicitPreferredID {
			return 1
		}
	}
	if left.LocalTrackCount != right.LocalTrackCount {
		if left.LocalTrackCount > right.LocalTrackCount {
			return -1
		}
		return 1
	}
	if left.TrackCount != right.TrackCount {
		if left.TrackCount > right.TrackCount {
			return -1
		}
		return 1
	}
	if left.BestQualityRank != right.BestQualityRank {
		if left.BestQualityRank > right.BestQualityRank {
			return -1
		}
		return 1
	}
	if strings.ToLower(left.Title) < strings.ToLower(right.Title) {
		return -1
	}
	if strings.ToLower(left.Title) > strings.ToLower(right.Title) {
		return 1
	}
	if left.AlbumVariantID < right.AlbumVariantID {
		return -1
	}
	if left.AlbumVariantID > right.AlbumVariantID {
		return 1
	}
	return 0
}

func (s *CatalogService) loadAlbumArtworkRef(ctx context.Context, libraryID, albumID string) (apitypes.ArtworkRef, error) {
	ref, err := s.loadArtworkRef(ctx, libraryID, "album", albumID, defaultArtworkVariant320)
	if err != nil || strings.TrimSpace(ref.BlobID) != "" {
		return ref, err
	}
	local, localErr := s.app.requireActiveContext(ctx)
	if localErr != nil {
		return apitypes.ArtworkRef{}, localErr
	}
	variantAlbumID, ok, err := s.explicitAlbumVariantID(ctx, libraryID, local.DeviceID, albumID)
	if err != nil || !ok || strings.TrimSpace(variantAlbumID) == "" || variantAlbumID == strings.TrimSpace(albumID) {
		return apitypes.ArtworkRef{}, err
	}
	return s.loadArtworkRef(ctx, libraryID, "album", variantAlbumID, defaultArtworkVariant320)
}

func (s *CatalogService) loadPlaylistArtworkRef(ctx context.Context, libraryID, playlistID string) (apitypes.ArtworkRef, bool, error) {
	ref, err := s.loadArtworkRef(ctx, libraryID, "playlist", playlistID, defaultArtworkVariant320)
	if err != nil {
		return apitypes.ArtworkRef{}, false, err
	}
	return ref, strings.TrimSpace(ref.BlobID) != "", nil
}

func (s *CatalogService) loadArtworkRef(ctx context.Context, libraryID, scopeType, scopeID, variant string) (apitypes.ArtworkRef, error) {
	var row ArtworkVariant
	if err := s.app.storage.ReadWithContext(ctx).
		Where("library_id = ? AND scope_type = ? AND scope_id = ? AND variant = ?", libraryID, strings.TrimSpace(scopeType), strings.TrimSpace(scopeID), strings.TrimSpace(variant)).
		Take(&row).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return apitypes.ArtworkRef{}, nil
		}
		return apitypes.ArtworkRef{}, err
	}
	return artworkRefFromRow(row), nil
}

func artworkRefFromRow(row ArtworkVariant) apitypes.ArtworkRef {
	if strings.TrimSpace(row.BlobID) == "" {
		return apitypes.ArtworkRef{}
	}
	return apitypes.ArtworkRef{
		BlobID:  strings.TrimSpace(row.BlobID),
		MIME:    strings.TrimSpace(row.MIME),
		FileExt: normalizeArtworkFileExt(row.FileExt, row.MIME),
		Variant: strings.TrimSpace(row.Variant),
		Width:   row.W,
		Height:  row.H,
		Bytes:   row.Bytes,
	}
}
