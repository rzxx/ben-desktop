package desktopcore

import (
	"context"
	"database/sql"
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
	if err := s.app.storage.WithContext(ctx).Raw(query, local.LibraryID).Scan(&rows).Error; err != nil {
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
	var rows []recordingSeedRow
	if err := s.app.storage.WithContext(ctx).Raw(query, local.LibraryID).Scan(&rows).Error; err != nil {
		return apitypes.Page[apitypes.RecordingListItem]{}, err
	}
	seeds := make([]recordingSeedRow, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		groupID := strings.TrimSpace(row.TrackClusterID)
		if groupID == "" {
			groupID = row.RecordingID
		}
		if _, ok := seen[groupID]; ok {
			continue
		}
		seen[groupID] = struct{}{}
		row.TrackClusterID = groupID
		seeds = append(seeds, row)
	}
	return s.listCollapsedRecordings(ctx, local.LibraryID, local.DeviceID, seeds, req.PageRequest)
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
	if err := s.app.storage.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
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
	if err := s.app.storage.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
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
	if err := s.app.storage.WithContext(ctx).Raw(query, local.LibraryID, strings.TrimSpace(req.AlbumID)).Scan(&rows).Error; err != nil {
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
	}
	query := `
SELECT
	p.playlist_id,
	p.name,
	p.kind,
	p.created_by,
	p.updated_at,
	COUNT(DISTINCT CASE WHEN r.track_variant_id IS NOT NULL THEN pi.item_id END) AS item_count,
	MAX(CASE WHEN aw.scope_id IS NULL THEN 0 ELSE 1 END) AS has_custom_cover
FROM playlists p
LEFT JOIN playlist_items pi ON pi.library_id = p.library_id AND pi.playlist_id = p.playlist_id AND pi.deleted_at IS NULL
LEFT JOIN track_variants r ON r.library_id = pi.library_id AND r.track_variant_id = pi.track_variant_id
LEFT JOIN artwork_variants aw ON aw.library_id = p.library_id AND aw.scope_type = 'playlist' AND aw.scope_id = p.playlist_id
WHERE p.library_id = ? AND p.deleted_at IS NULL
GROUP BY p.playlist_id, p.name, p.kind, p.created_by, p.updated_at
ORDER BY CASE WHEN p.kind = ? THEN 0 ELSE 1 END ASC, LOWER(p.name) ASC, p.playlist_id ASC`
	var rows []row
	if err := s.app.storage.WithContext(ctx).Raw(query, local.LibraryID, playlistKindLiked).Scan(&rows).Error; err != nil {
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
	type row struct {
		ItemID             string
		LibraryRecordingID string
		Title              string
		DurationMS         int64
		ArtistsCSV         string
		AddedAt            time.Time
	}
	query := `
SELECT
	pi.item_id,
	pi.track_variant_id AS library_recording_id,
	MIN(r.title) AS title,
	MAX(r.duration_ms) AS duration_ms,
	COALESCE(GROUP_CONCAT(ar.name, '` + artistSeparator + `'), '') AS artists_csv,
	pi.added_at
FROM playlist_items pi
JOIN playlists p ON p.library_id = pi.library_id AND p.playlist_id = pi.playlist_id
JOIN track_variants r ON r.library_id = pi.library_id AND r.track_cluster_id = pi.track_variant_id
LEFT JOIN credits c ON c.library_id = r.library_id AND c.entity_type = 'track' AND c.entity_id = r.track_variant_id
LEFT JOIN artists ar ON ar.library_id = c.library_id AND ar.artist_id = c.artist_id
WHERE pi.library_id = ? AND pi.playlist_id = ? AND pi.deleted_at IS NULL AND p.deleted_at IS NULL
GROUP BY pi.item_id, pi.track_variant_id, pi.position_key, pi.added_at, p.kind
ORDER BY CASE WHEN p.kind = 'liked' THEN 0 ELSE 1 END ASC,
	CASE WHEN p.kind = 'liked' THEN pi.added_at END DESC,
	CASE WHEN p.kind <> 'liked' THEN pi.position_key END ASC,
	pi.item_id ASC`
	var rows []row
	if err := s.app.storage.WithContext(ctx).Raw(query, local.LibraryID, strings.TrimSpace(req.PlaylistID)).Scan(&rows).Error; err != nil {
		return apitypes.Page[apitypes.PlaylistTrackItem]{}, err
	}
	paged, pageInfo := pageItems(rows, req.PageRequest)
	out := make([]apitypes.PlaylistTrackItem, 0, len(paged))
	for _, row := range paged {
		out = append(out, apitypes.PlaylistTrackItem{
			ItemID:             row.ItemID,
			LibraryRecordingID: row.LibraryRecordingID,
			RecordingID:        row.LibraryRecordingID,
			Title:              row.Title,
			DurationMS:         row.DurationMS,
			Artists:            splitArtists(row.ArtistsCSV),
			AddedAt:            row.AddedAt,
		})
	}
	return apitypes.Page[apitypes.PlaylistTrackItem]{Items: out, Page: pageInfo}, nil
}

func (s *CatalogService) ListLikedRecordings(ctx context.Context, req apitypes.LikedRecordingListRequest) (apitypes.Page[apitypes.LikedRecordingItem], error) {
	local, err := s.app.requireActiveContext(ctx)
	if err != nil {
		return apitypes.Page[apitypes.LikedRecordingItem]{}, err
	}
	type row struct {
		LibraryRecordingID string
		Title              string
		DurationMS         int64
		ArtistsCSV         string
		AddedAt            time.Time
	}
	query := `
SELECT
	pi.track_variant_id AS library_recording_id,
	MIN(r.title) AS title,
	MAX(r.duration_ms) AS duration_ms,
	COALESCE(GROUP_CONCAT(ar.name, '` + artistSeparator + `'), '') AS artists_csv,
	pi.added_at
FROM playlist_items pi
JOIN playlists p ON p.library_id = pi.library_id AND p.playlist_id = pi.playlist_id
JOIN track_variants r ON r.library_id = pi.library_id AND r.track_cluster_id = pi.track_variant_id
LEFT JOIN credits c ON c.library_id = r.library_id AND c.entity_type = 'track' AND c.entity_id = r.track_variant_id
LEFT JOIN artists ar ON ar.library_id = c.library_id AND ar.artist_id = c.artist_id
WHERE pi.library_id = ? AND pi.playlist_id = ? AND pi.deleted_at IS NULL AND p.deleted_at IS NULL
GROUP BY pi.item_id, pi.track_variant_id, pi.added_at
ORDER BY pi.added_at DESC, pi.item_id DESC`
	var rows []row
	if err := s.app.storage.WithContext(ctx).Raw(query, local.LibraryID, likedPlaylistIDForLibrary(local.LibraryID)).Scan(&rows).Error; err != nil {
		return apitypes.Page[apitypes.LikedRecordingItem]{}, err
	}
	paged, pageInfo := pageItems(rows, req.PageRequest)
	out := make([]apitypes.LikedRecordingItem, 0, len(paged))
	for _, row := range paged {
		out = append(out, apitypes.LikedRecordingItem{
			LibraryRecordingID: row.LibraryRecordingID,
			RecordingID:        row.LibraryRecordingID,
			Title:              row.Title,
			DurationMS:         row.DurationMS,
			Artists:            splitArtists(row.ArtistsCSV),
			AddedAt:            row.AddedAt,
		})
	}
	return apitypes.Page[apitypes.LikedRecordingItem]{Items: out, Page: pageInfo}, nil
}

func (s *CatalogService) listCollapsedAlbums(ctx context.Context, libraryID, deviceID string, req apitypes.PageRequest, query string, args ...any) (apitypes.Page[apitypes.AlbumListItem], error) {
	var rows []albumSeedRow
	if err := s.app.storage.WithContext(ctx).Raw(query, args...).Scan(&rows).Error; err != nil {
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

func (s *CatalogService) listCollapsedRecordings(ctx context.Context, libraryID, deviceID string, seeds []recordingSeedRow, req apitypes.PageRequest) (apitypes.Page[apitypes.RecordingListItem], error) {
	pagedSeeds, pageInfo := pageItems(seeds, req)
	clusterIDs := make([]string, 0, len(pagedSeeds))
	for _, seed := range pagedSeeds {
		clusterIDs = append(clusterIDs, seed.TrackClusterID)
	}
	rowsByCluster, err := s.listRecordingVariantRowsForClusters(ctx, libraryID, deviceID, clusterIDs, s.app.cfg.TranscodeProfile)
	if err != nil {
		return apitypes.Page[apitypes.RecordingListItem]{}, err
	}
	preferredByCluster, err := s.preferredRecordingVariantIDsForClusters(ctx, libraryID, deviceID, clusterIDs)
	if err != nil {
		return apitypes.Page[apitypes.RecordingListItem]{}, err
	}

	out := make([]apitypes.RecordingListItem, 0, len(pagedSeeds))
	for _, seed := range pagedSeeds {
		variants := rowsByCluster[strings.TrimSpace(seed.TrackClusterID)]
		if len(variants) == 0 {
			continue
		}
		preferredID := chooseRecordingVariantID(variants, preferredByCluster[strings.TrimSpace(seed.TrackClusterID)])
		chosen := variants[0]
		for _, variant := range variants {
			if variant.TrackVariantID == preferredID {
				chosen = variant
				break
			}
		}
		out = append(out, apitypes.RecordingListItem{
			LibraryRecordingID:          strings.TrimSpace(seed.TrackClusterID),
			PreferredVariantRecordingID: chosen.TrackVariantID,
			TrackClusterID:              strings.TrimSpace(seed.TrackClusterID),
			RecordingID:                 strings.TrimSpace(seed.TrackClusterID),
			Title:                       chosen.Title,
			DurationMS:                  chosen.DurationMS,
			Artists:                     append([]string(nil), chosen.Artists...),
			VariantCount:                int64(len(variants)),
			HasVariants:                 len(variants) > 1,
		})
	}
	pageInfo.Returned = len(out)
	return apitypes.Page[apitypes.RecordingListItem]{Items: out, Page: pageInfo}, nil
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

func (s *CatalogService) trackClusterIDForVariant(ctx context.Context, libraryID, recordingID string) (string, bool, error) {
	var row TrackVariantModel
	if err := s.app.storage.WithContext(ctx).Where("library_id = ? AND track_variant_id = ?", libraryID, recordingID).Take(&row).Error; err == nil {
		return strings.TrimSpace(row.TrackClusterID), true, nil
	} else if err != gorm.ErrRecordNotFound {
		return "", false, err
	}
	if err := s.app.storage.WithContext(ctx).Where("library_id = ? AND track_cluster_id = ?", libraryID, recordingID).Take(&row).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return "", false, nil
		}
		return "", false, err
	}
	return strings.TrimSpace(row.TrackClusterID), true, nil
}

func (s *CatalogService) albumClusterIDForVariant(ctx context.Context, libraryID, albumID string) (string, bool, error) {
	var row AlbumVariantModel
	if err := s.app.storage.WithContext(ctx).Where("library_id = ? AND album_variant_id = ?", libraryID, albumID).Take(&row).Error; err == nil {
		return strings.TrimSpace(row.AlbumClusterID), true, nil
	} else if err != gorm.ErrRecordNotFound {
		return "", false, err
	}
	if err := s.app.storage.WithContext(ctx).Where("library_id = ? AND album_cluster_id = ?", libraryID, albumID).Take(&row).Error; err != nil {
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
	if err := s.app.storage.WithContext(ctx).Where("library_id = ? AND track_variant_id = ?", libraryID, recordingID).Take(&row).Error; err == nil {
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
	if err := s.app.storage.WithContext(ctx).Where("library_id = ? AND album_variant_id = ?", libraryID, albumID).Take(&row).Error; err == nil {
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
	if err := s.app.storage.WithContext(ctx).
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
	if err := s.app.storage.WithContext(ctx).
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
	if err := s.app.storage.WithContext(ctx).
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
	if err := s.app.storage.WithContext(ctx).
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
	if err := s.app.storage.WithContext(ctx).Raw(query, deviceID, deviceID, deviceID, preferredProfile, preferredProfile, libraryID, clusterIDs).Scan(&rows).Error; err != nil {
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
	if err := s.app.storage.WithContext(ctx).Raw(query, deviceID, libraryID, clusterIDs).Scan(&rows).Error; err != nil {
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
	if err := s.app.storage.WithContext(ctx).Raw(query, deviceID, deviceID, deviceID, preferredProfile, preferredProfile, libraryID, clusterID).Scan(&rows).Error; err != nil {
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
	if err := s.app.storage.WithContext(ctx).Raw(query, deviceID, libraryID, clusterID).Scan(&rows).Error; err != nil {
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

func (s *CatalogService) catalogAvailabilityHintsForClusters(ctx context.Context, libraryID, localDeviceID string, clusterIDs []string) (map[string]apitypes.CatalogTrackAvailabilityHint, error) {
	clusterIDs = compactNonEmptyStrings(clusterIDs)
	if len(clusterIDs) == 0 {
		return map[string]apitypes.CatalogTrackAvailabilityHint{}, nil
	}
	type row struct {
		TrackClusterID string
		DeviceID       string
		Role           string
		LastSeenAt     sql.NullTime
	}
	type cachedRow struct {
		TrackClusterID string
	}
	query := `
SELECT
	tv.track_cluster_id AS track_cluster_id,
	sf.device_id,
	COALESCE(m.role, '') AS role,
	d.last_seen_at
FROM track_variants tv
JOIN source_files sf ON sf.library_id = tv.library_id AND sf.track_variant_id = tv.track_variant_id AND sf.is_present = 1
LEFT JOIN memberships m ON m.library_id = sf.library_id AND m.device_id = sf.device_id
LEFT JOIN devices d ON d.device_id = sf.device_id
WHERE tv.library_id = ? AND tv.track_cluster_id IN ?
GROUP BY tv.track_cluster_id, sf.device_id, m.role, d.last_seen_at`
	var rows []row
	if err := s.app.storage.WithContext(ctx).Raw(query, libraryID, clusterIDs).Scan(&rows).Error; err != nil {
		return nil, err
	}
	cachedQuery := `
SELECT DISTINCT
	tv.track_cluster_id AS track_cluster_id
FROM track_variants tv
JOIN optimized_assets oa ON oa.library_id = tv.library_id AND oa.track_variant_id = tv.track_variant_id
JOIN device_asset_caches dac ON dac.library_id = oa.library_id AND dac.optimized_asset_id = oa.optimized_asset_id
WHERE tv.library_id = ?
	AND tv.track_cluster_id IN ?
	AND dac.device_id = ?
	AND dac.is_cached = 1`
	var cachedRows []cachedRow
	if err := s.app.storage.WithContext(ctx).Raw(cachedQuery, libraryID, clusterIDs, localDeviceID).Scan(&cachedRows).Error; err != nil {
		return nil, err
	}
	type facts struct {
		local, cached     bool
		providers, online int
	}
	factMap := make(map[string]facts, len(clusterIDs))
	for _, item := range rows {
		clusterID := strings.TrimSpace(item.TrackClusterID)
		deviceID := strings.TrimSpace(item.DeviceID)
		if clusterID == "" || deviceID == "" {
			continue
		}
		next := factMap[clusterID]
		if deviceID == localDeviceID {
			next.local = true
		} else if canProvideLocalMedia(item.Role) {
			next.providers++
			if item.LastSeenAt.Valid && item.LastSeenAt.Time.UTC().After(time.Now().UTC().Add(-availabilityOnlineWindow)) {
				next.online++
			}
		}
		factMap[clusterID] = next
	}
	for _, item := range cachedRows {
		next := factMap[strings.TrimSpace(item.TrackClusterID)]
		next.cached = true
		factMap[strings.TrimSpace(item.TrackClusterID)] = next
	}
	out := make(map[string]apitypes.CatalogTrackAvailabilityHint, len(clusterIDs))
	for _, clusterID := range clusterIDs {
		f := factMap[strings.TrimSpace(clusterID)]
		hint := apitypes.CatalogTrackAvailabilityHint{
			HasLocalSource:            f.local,
			HasCachedLocal:            f.cached,
			ProviderDeviceCount:       f.providers,
			OnlineProviderDeviceCount: f.online,
		}
		switch {
		case hint.HasLocalSource:
			hint.State = apitypes.CatalogAvailabilityLocal
		case hint.HasCachedLocal:
			hint.State = apitypes.CatalogAvailabilityCached
		case hint.OnlineProviderDeviceCount > 0:
			hint.State = apitypes.CatalogAvailabilityProviderOnline
		case hint.ProviderDeviceCount > 0:
			hint.State = apitypes.CatalogAvailabilityProviderOffline
		default:
			hint.State = apitypes.CatalogAvailabilityUnavailable
		}
		out[strings.TrimSpace(clusterID)] = hint
	}
	return out, nil
}

func (s *CatalogService) catalogAggregateHintsForAlbumVariants(ctx context.Context, libraryID, localDeviceID string, albumIDs []string) (map[string]apitypes.CatalogAggregateAvailabilityHint, error) {
	albumIDs = compactNonEmptyStrings(albumIDs)
	if len(albumIDs) == 0 {
		return map[string]apitypes.CatalogAggregateAvailabilityHint{}, nil
	}
	type row struct {
		AlbumVariantID string
		TrackClusterID string
	}
	var rows []row
	query := `
SELECT
	at.album_variant_id,
	tv.track_cluster_id
FROM album_tracks at
JOIN track_variants tv ON tv.library_id = at.library_id AND tv.track_variant_id = at.track_variant_id
WHERE at.library_id = ? AND at.album_variant_id IN ?
GROUP BY at.album_variant_id, tv.track_cluster_id`
	if err := s.app.storage.WithContext(ctx).Raw(query, libraryID, albumIDs).Scan(&rows).Error; err != nil {
		return nil, err
	}
	grouped := make(map[string][]string)
	allClusterIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		grouped[row.AlbumVariantID] = append(grouped[row.AlbumVariantID], row.TrackClusterID)
		allClusterIDs = append(allClusterIDs, row.TrackClusterID)
	}
	return s.aggregateHintsByGroup(ctx, libraryID, localDeviceID, grouped, allClusterIDs)
}

func (s *CatalogService) catalogAggregateHintsForArtists(ctx context.Context, libraryID, localDeviceID string, artistIDs []string) (map[string]apitypes.CatalogAggregateAvailabilityHint, error) {
	artistIDs = compactNonEmptyStrings(artistIDs)
	if len(artistIDs) == 0 {
		return map[string]apitypes.CatalogAggregateAvailabilityHint{}, nil
	}
	type row struct {
		ArtistID       string
		TrackClusterID string
	}
	var rows []row
	query := `
SELECT
	c.artist_id,
	tv.track_cluster_id
FROM credits c
JOIN track_variants tv ON tv.library_id = c.library_id AND tv.track_variant_id = c.entity_id
WHERE c.library_id = ? AND c.entity_type = 'track' AND c.artist_id IN ?
GROUP BY c.artist_id, tv.track_cluster_id`
	if err := s.app.storage.WithContext(ctx).Raw(query, libraryID, artistIDs).Scan(&rows).Error; err != nil {
		return nil, err
	}
	grouped := make(map[string][]string)
	allClusterIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		grouped[row.ArtistID] = append(grouped[row.ArtistID], row.TrackClusterID)
		allClusterIDs = append(allClusterIDs, row.TrackClusterID)
	}
	return s.aggregateHintsByGroup(ctx, libraryID, localDeviceID, grouped, allClusterIDs)
}

func (s *CatalogService) aggregateHintsByGroup(ctx context.Context, libraryID, localDeviceID string, grouped map[string][]string, allClusterIDs []string) (map[string]apitypes.CatalogAggregateAvailabilityHint, error) {
	hints, err := s.catalogAvailabilityHintsForClusters(ctx, libraryID, localDeviceID, allClusterIDs)
	if err != nil {
		return nil, err
	}
	out := make(map[string]apitypes.CatalogAggregateAvailabilityHint, len(grouped))
	for key, clusterIDs := range grouped {
		var agg apitypes.CatalogAggregateAvailabilityHint
		for _, clusterID := range clusterIDs {
			switch hints[strings.TrimSpace(clusterID)].State {
			case apitypes.CatalogAvailabilityLocal:
				agg.LocalTrackCount++
				agg.AvailableTrackCount++
			case apitypes.CatalogAvailabilityCached:
				agg.CachedTrackCount++
				agg.AvailableTrackCount++
			case apitypes.CatalogAvailabilityProviderOnline:
				agg.ProviderOnlineTrackCount++
				agg.AvailableTrackCount++
			case apitypes.CatalogAvailabilityProviderOffline:
				agg.ProviderOfflineTrackCount++
				agg.AvailableTrackCount++
			default:
				agg.UnavailableTrackCount++
			}
		}
		out[strings.TrimSpace(key)] = agg
	}
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
		if strings.ToLower(variants[i].Title) != strings.ToLower(variants[j].Title) {
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
	if err := s.app.storage.WithContext(ctx).
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
