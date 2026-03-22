package desktopcore

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	apitypes "ben/desktop/api/types"
	"gorm.io/gorm"
)

type catalogRebuildSnapshot struct {
	albumIDs              map[string]struct{}
	albumClusterIDs       map[string]struct{}
	albumClusterByVariant map[string]string
	albumFamilyByCluster  map[string]string
	clusterIDsByFamily    map[string][]string
	variantsByCluster     map[string][]catalogAlbumVariantSnapshot
	localAlbumIDs         map[string]struct{}
}

type catalogAlbumVariantSnapshot struct {
	AlbumVariantID  string
	AlbumClusterID  string
	Title           string
	TrackCount      int64
	BestQualityRank int
}

func (a *App) rebuildCatalogMaterialization(ctx context.Context, libraryID string, local *apitypes.LocalContext) error {
	libraryID = strings.TrimSpace(libraryID)
	if a == nil || a.storage == nil || libraryID == "" {
		return nil
	}

	var reconcileAlbumIDs []string
	if err := a.storage.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		reconcileAlbumIDs, err = a.prepareCatalogRebuildTx(tx, libraryID, local)
		return err
	}); err != nil {
		return err
	}

	a.emitCatalogChange(apitypes.CatalogChangeEvent{
		Kind:          apitypes.CatalogChangeInvalidateBase,
		InvalidateAll: true,
	})
	a.emitAvailabilityInvalidateAllForActiveLibrary(libraryID)

	if local != nil && a.artwork != nil {
		if err := a.reconcileLocalAlbumArtworkBestEffort(ctx, *local, reconcileAlbumIDs); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) prepareCatalogRebuildTx(tx *gorm.DB, libraryID string, local *apitypes.LocalContext) ([]string, error) {
	deviceID := ""
	if local != nil {
		deviceID = strings.TrimSpace(local.DeviceID)
	}

	before, err := captureCatalogRebuildSnapshotTx(tx, libraryID, deviceID)
	if err != nil {
		return nil, err
	}
	if err := rebuildCatalogMaterializationTx(tx, libraryID); err != nil {
		return nil, err
	}
	if err := pruneDanglingVariantPreferencesTx(tx, libraryID); err != nil {
		return nil, err
	}

	after, err := captureCatalogRebuildSnapshotTx(tx, libraryID, deviceID)
	if err != nil {
		return nil, err
	}
	if err := migrateAlbumOfflinePinsTx(tx, libraryID, before, after); err != nil {
		return nil, err
	}

	if local == nil {
		return nil, nil
	}

	for _, albumID := range diffStringSet(before.albumIDs, after.albumIDs) {
		if err := a.deleteArtworkScopeTx(tx, *local, "album", albumID); err != nil {
			return nil, err
		}
	}

	reconcileSet := make(map[string]struct{})
	for albumID := range before.localAlbumIDs {
		if _, ok := after.albumIDs[albumID]; ok {
			reconcileSet[albumID] = struct{}{}
		}
	}
	for albumID := range after.localAlbumIDs {
		reconcileSet[albumID] = struct{}{}
	}
	return sortedStringKeys(reconcileSet), nil
}

func rebuildCatalogMaterializationTx(tx *gorm.DB, libraryID string) error {
	libraryID = strings.TrimSpace(libraryID)
	if libraryID == "" {
		return nil
	}

	var rows []SourceFileModel
	if err := tx.
		Where("library_id = ? AND is_present = ?", libraryID, true).
		Order("quality_rank DESC").
		Order("last_seen_at DESC").
		Order("updated_at DESC").
		Order("device_id ASC").
		Order("source_file_id ASC").
		Find(&rows).Error; err != nil {
		return err
	}

	albums := make(map[string]AlbumVariantModel)
	albumGroupKeys := make(map[string]string)
	tracks := make(map[string]TrackVariantModel)
	albumTracks := make(map[string]AlbumTrack)
	artists := make(map[string]Artist)
	credits := make(map[string]Credit)

	for _, row := range rows {
		tags, err := tagsFromSnapshotJSON(row.TagsJSON)
		if err != nil {
			return err
		}

		recordingKey, albumKey, groupKey := normalizedRecordKeys(tags)
		if recordingKey == "" || albumKey == "" {
			continue
		}

		editionScopeKey := strings.TrimSpace(row.EditionScopeKey)
		if editionScopeKey == "" {
			editionScopeKey = normalizeCatalogKey(strings.Join([]string{
				firstNonEmpty(tags.AlbumArtist, firstArtist(tags.Artists)),
				tags.Album,
			}, "|"))
		}
		trackVariantID := explicitTrackVariantID(recordingKey, editionScopeKey, tags.DiscNo, tags.TrackNo)
		trackClusterID := stableNameID("track_cluster", recordingKey)
		albumVariantID := explicitAlbumVariantID(albumKey, editionScopeKey)
		mutatedAt := latestNonZeroTime(row.UpdatedAt, row.LastSeenAt, row.CreatedAt)

		track, ok := tracks[trackVariantID]
		if !ok {
			track = TrackVariantModel{
				LibraryID:      libraryID,
				TrackVariantID: trackVariantID,
				TrackClusterID: trackClusterID,
				KeyNorm:        recordingKey,
				Title:          strings.TrimSpace(tags.Title),
				DurationMS:     tags.DurationMS,
				CreatedAt:      mutatedAt,
				UpdatedAt:      mutatedAt,
			}
		} else if track.UpdatedAt.Before(mutatedAt) {
			track.UpdatedAt = mutatedAt
		}
		tracks[trackVariantID] = track

		album, ok := albums[albumVariantID]
		if !ok {
			album = AlbumVariantModel{
				LibraryID:      libraryID,
				AlbumVariantID: albumVariantID,
				AlbumClusterID: "",
				Title:          strings.TrimSpace(tags.Album),
				KeyNorm:        albumKey,
				CreatedAt:      mutatedAt,
				UpdatedAt:      mutatedAt,
			}
			if tags.Year > 0 {
				year := tags.Year
				album.Year = &year
			}
		} else if album.UpdatedAt.Before(mutatedAt) {
			album.UpdatedAt = mutatedAt
		}
		albums[albumVariantID] = album
		albumGroupKeys[albumVariantID] = groupKey

		albumTrack := AlbumTrack{
			LibraryID:      libraryID,
			AlbumVariantID: albumVariantID,
			TrackVariantID: trackVariantID,
			DiscNo:         maxTrackNumber(tags.DiscNo),
			TrackNo:        maxTrackNumber(tags.TrackNo),
		}
		albumTracks[albumTrackKey(albumTrack)] = albumTrack

		collectArtistsAndCredits(libraryID, trackVariantID, albumVariantID, tags, artists, credits)
	}

	assignStrictAlbumClusterIDs(albums, albumTracks, tracks, albumGroupKeys)

	for _, model := range []any{
		&Credit{},
		&Artist{},
		&AlbumTrack{},
		&AlbumVariantModel{},
		&TrackVariantModel{},
	} {
		if err := tx.Where("library_id = ?", libraryID).Delete(model).Error; err != nil {
			return err
		}
	}

	if err := createArtistsTx(tx, artists); err != nil {
		return err
	}
	if err := createCreditsTx(tx, credits); err != nil {
		return err
	}
	if err := createAlbumVariantsTx(tx, albums); err != nil {
		return err
	}
	if err := createTrackVariantsTx(tx, tracks); err != nil {
		return err
	}
	return createAlbumTracksTx(tx, albumTracks)
}

func pruneDanglingVariantPreferencesTx(tx *gorm.DB, libraryID string) error {
	type prefRow struct {
		LibraryID       string
		DeviceID        string
		ScopeType       string
		ClusterID       string
		ChosenVariantID string
	}

	var prefs []prefRow
	if err := tx.Model(&DeviceVariantPreference{}).
		Select("library_id, device_id, scope_type, cluster_id, chosen_variant_id").
		Where("library_id = ?", strings.TrimSpace(libraryID)).
		Scan(&prefs).Error; err != nil {
		return err
	}

	albumClusters, albumVariants, err := loadVariantPreferenceTargetsTx(tx, libraryID, "album")
	if err != nil {
		return err
	}
	trackClusters, trackVariants, err := loadVariantPreferenceTargetsTx(tx, libraryID, "track")
	if err != nil {
		return err
	}

	for _, pref := range prefs {
		scope := strings.TrimSpace(pref.ScopeType)
		clusterID := strings.TrimSpace(pref.ClusterID)
		chosenVariantID := strings.TrimSpace(pref.ChosenVariantID)
		var clusters map[string]struct{}
		var variants map[string]struct{}
		switch scope {
		case "album":
			clusters = albumClusters
			variants = albumVariants
		case "track":
			clusters = trackClusters
			variants = trackVariants
		default:
			continue
		}
		if _, ok := clusters[clusterID]; ok {
			if _, ok := variants[chosenVariantID]; ok {
				continue
			}
		}
		if err := tx.Where(
			"library_id = ? AND device_id = ? AND scope_type = ? AND cluster_id = ?",
			pref.LibraryID,
			pref.DeviceID,
			scope,
			clusterID,
		).Delete(&DeviceVariantPreference{}).Error; err != nil {
			return err
		}
	}
	return nil
}

func migrateAlbumOfflinePinsTx(tx *gorm.DB, libraryID string, before, after catalogRebuildSnapshot) error {
	type pinRow struct {
		LibraryID string
		DeviceID  string
		Scope     string
		ScopeID   string
		Profile   string
		CreatedAt time.Time
		UpdatedAt time.Time
	}

	var pins []pinRow
	if err := tx.Model(&OfflinePin{}).
		Select("library_id, device_id, scope, scope_id, profile, created_at, updated_at").
		Where("library_id = ? AND scope = ?", strings.TrimSpace(libraryID), "album").
		Scan(&pins).Error; err != nil {
		return err
	}

	for _, pin := range pins {
		albumID := strings.TrimSpace(pin.ScopeID)
		if albumID == "" {
			continue
		}
		if _, ok := after.albumClusterIDs[albumID]; ok {
			continue
		}

		clusterID := albumID
		if _, ok := before.albumClusterIDs[clusterID]; !ok {
			clusterID = strings.TrimSpace(before.albumClusterByVariant[albumID])
		}
		if clusterID == "" {
			if err := deleteAlbumOfflinePinTx(tx, pin.LibraryID, pin.DeviceID, albumID); err != nil {
				return err
			}
			continue
		}

		if _, ok := after.albumClusterIDs[clusterID]; !ok {
			familyKey := strings.TrimSpace(before.albumFamilyByCluster[clusterID])
			candidateClusters := after.clusterIDsByFamily[familyKey]
			destinationID := chooseMigratedAlbumPinClusterID(candidateClusters, after.variantsByCluster)
			if destinationID == "" {
				if err := deleteAlbumOfflinePinTx(tx, pin.LibraryID, pin.DeviceID, albumID); err != nil {
					return err
				}
				continue
			}
			clusterID = destinationID
		}
		destinationID := clusterID
		if destinationID == albumID {
			continue
		}

		var existing OfflinePin
		err := tx.Where(
			"library_id = ? AND device_id = ? AND scope = ? AND scope_id = ?",
			pin.LibraryID,
			pin.DeviceID,
			"album",
			destinationID,
		).Take(&existing).Error
		if err != nil && err != gorm.ErrRecordNotFound {
			return err
		}
		if err == nil {
			if err := deleteAlbumOfflinePinTx(tx, pin.LibraryID, pin.DeviceID, albumID); err != nil {
				return err
			}
			continue
		}

		if err := tx.Create(&OfflinePin{
			LibraryID: pin.LibraryID,
			DeviceID:  pin.DeviceID,
			Scope:     "album",
			ScopeID:   destinationID,
			Profile:   strings.TrimSpace(pin.Profile),
			CreatedAt: pin.CreatedAt,
			UpdatedAt: pin.UpdatedAt,
		}).Error; err != nil {
			return err
		}
		if err := deleteAlbumOfflinePinTx(tx, pin.LibraryID, pin.DeviceID, albumID); err != nil {
			return err
		}
	}
	return nil
}

func chooseMigratedAlbumPinVariantIDTx(tx *gorm.DB, libraryID, deviceID, clusterID string, candidates []catalogAlbumVariantSnapshot) (string, bool, error) {
	if len(candidates) == 0 {
		return "", false, nil
	}

	localTrackCounts, err := loadAlbumLocalTrackCountsTx(tx, libraryID, deviceID, candidates)
	if err != nil {
		return "", false, err
	}

	candidateIDs := make(map[string]struct{}, len(candidates))
	items := make([]apitypes.AlbumVariantItem, 0, len(candidates))
	for _, candidate := range candidates {
		albumID := strings.TrimSpace(candidate.AlbumVariantID)
		if albumID == "" {
			continue
		}
		candidateIDs[albumID] = struct{}{}
		items = append(items, apitypes.AlbumVariantItem{
			AlbumID:         albumID,
			Title:           strings.TrimSpace(candidate.Title),
			TrackCount:      candidate.TrackCount,
			BestQualityRank: candidate.BestQualityRank,
			LocalTrackCount: localTrackCounts[albumID],
		})
	}
	if len(items) == 0 {
		return "", false, nil
	}

	var pref DeviceVariantPreference
	err = tx.Where(
		"library_id = ? AND device_id = ? AND scope_type = ? AND cluster_id = ?",
		strings.TrimSpace(libraryID),
		strings.TrimSpace(deviceID),
		"album",
		strings.TrimSpace(clusterID),
	).Take(&pref).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		return "", false, err
	}
	if err == nil {
		preferredID := strings.TrimSpace(pref.ChosenVariantID)
		if _, ok := candidateIDs[preferredID]; ok {
			return preferredID, true, nil
		}
	}

	chosenID := chooseAlbumVariantID(items, "")
	if strings.TrimSpace(chosenID) == "" {
		return "", false, nil
	}
	return chosenID, true, nil
}

func loadAlbumLocalTrackCountsTx(tx *gorm.DB, libraryID, deviceID string, candidates []catalogAlbumVariantSnapshot) (map[string]int64, error) {
	counts := make(map[string]int64, len(candidates))
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" || len(candidates) == 0 {
		return counts, nil
	}

	albumIDs := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		albumID := strings.TrimSpace(candidate.AlbumVariantID)
		if albumID == "" {
			continue
		}
		albumIDs = append(albumIDs, albumID)
	}
	if len(albumIDs) == 0 {
		return counts, nil
	}

	type row struct {
		AlbumVariantID  string
		LocalTrackCount int64
	}
	var rows []row
	if err := tx.Table("album_tracks AS at").
		Select("at.album_variant_id AS album_variant_id, COUNT(DISTINCT at.track_variant_id) AS local_track_count").
		Joins("JOIN source_files sf ON sf.library_id = at.library_id AND sf.track_variant_id = at.track_variant_id").
		Where("at.library_id = ? AND sf.device_id = ? AND sf.is_present = ? AND at.album_variant_id IN ?", strings.TrimSpace(libraryID), deviceID, true, albumIDs).
		Group("at.album_variant_id").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		albumID := strings.TrimSpace(row.AlbumVariantID)
		if albumID == "" {
			continue
		}
		counts[albumID] = row.LocalTrackCount
	}
	return counts, nil
}

func deleteAlbumOfflinePinTx(tx *gorm.DB, libraryID, deviceID, albumID string) error {
	return tx.Where(
		"library_id = ? AND device_id = ? AND scope = ? AND scope_id = ?",
		strings.TrimSpace(libraryID),
		strings.TrimSpace(deviceID),
		"album",
		strings.TrimSpace(albumID),
	).Delete(&OfflinePin{}).Error
}

func chooseMigratedAlbumPinClusterID(clusterIDs []string, variantsByCluster map[string][]catalogAlbumVariantSnapshot) string {
	bestClusterID := ""
	bestTrackCount := int64(-1)
	bestQualityRank := -1
	bestTitle := ""
	for _, clusterID := range clusterIDs {
		clusterID = strings.TrimSpace(clusterID)
		if clusterID == "" {
			continue
		}
		var trackCount int64
		bestClusterQuality := -1
		bestClusterTitle := ""
		for _, candidate := range variantsByCluster[clusterID] {
			if candidate.TrackCount > trackCount {
				trackCount = candidate.TrackCount
			}
			if candidate.BestQualityRank > bestClusterQuality {
				bestClusterQuality = candidate.BestQualityRank
			}
			title := strings.TrimSpace(candidate.Title)
			if bestClusterTitle == "" || strings.ToLower(title) < strings.ToLower(bestClusterTitle) {
				bestClusterTitle = title
			}
		}
		switch {
		case bestClusterID == "":
			bestClusterID = clusterID
			bestTrackCount = trackCount
			bestQualityRank = bestClusterQuality
			bestTitle = bestClusterTitle
		case trackCount > bestTrackCount:
			bestClusterID = clusterID
			bestTrackCount = trackCount
			bestQualityRank = bestClusterQuality
			bestTitle = bestClusterTitle
		case trackCount == bestTrackCount && bestClusterQuality > bestQualityRank:
			bestClusterID = clusterID
			bestQualityRank = bestClusterQuality
			bestTitle = bestClusterTitle
		case trackCount == bestTrackCount && bestClusterQuality == bestQualityRank && strings.ToLower(bestClusterTitle) < strings.ToLower(bestTitle):
			bestClusterID = clusterID
			bestTitle = bestClusterTitle
		case trackCount == bestTrackCount && bestClusterQuality == bestQualityRank && strings.EqualFold(bestClusterTitle, bestTitle) && clusterID < bestClusterID:
			bestClusterID = clusterID
		}
	}
	return bestClusterID
}

func stringSliceContains(values []string, target string) bool {
	target = strings.TrimSpace(target)
	for _, value := range values {
		if strings.TrimSpace(value) == target {
			return true
		}
	}
	return false
}

func loadVariantPreferenceTargetsTx(tx *gorm.DB, libraryID, scopeType string) (map[string]struct{}, map[string]struct{}, error) {
	switch strings.TrimSpace(scopeType) {
	case "album":
		type row struct {
			VariantID string
			ClusterID string
		}
		var rows []row
		if err := tx.Model(&AlbumVariantModel{}).
			Select("album_variant_id AS variant_id, album_cluster_id AS cluster_id").
			Where("library_id = ?", strings.TrimSpace(libraryID)).
			Scan(&rows).Error; err != nil {
			return nil, nil, err
		}
		clusters := make(map[string]struct{}, len(rows))
		variants := make(map[string]struct{}, len(rows))
		for _, row := range rows {
			clusterID := strings.TrimSpace(row.ClusterID)
			variantID := strings.TrimSpace(row.VariantID)
			if clusterID != "" {
				clusters[clusterID] = struct{}{}
			}
			if variantID != "" {
				variants[variantID] = struct{}{}
			}
		}
		return clusters, variants, nil
	case "track":
		type row struct {
			VariantID string
			ClusterID string
		}
		var rows []row
		if err := tx.Model(&TrackVariantModel{}).
			Select("track_variant_id AS variant_id, track_cluster_id AS cluster_id").
			Where("library_id = ?", strings.TrimSpace(libraryID)).
			Scan(&rows).Error; err != nil {
			return nil, nil, err
		}
		clusters := make(map[string]struct{}, len(rows))
		variants := make(map[string]struct{}, len(rows))
		for _, row := range rows {
			clusterID := strings.TrimSpace(row.ClusterID)
			variantID := strings.TrimSpace(row.VariantID)
			if clusterID != "" {
				clusters[clusterID] = struct{}{}
			}
			if variantID != "" {
				variants[variantID] = struct{}{}
			}
		}
		return clusters, variants, nil
	default:
		return map[string]struct{}{}, map[string]struct{}{}, nil
	}
}

func captureCatalogRebuildSnapshotTx(tx *gorm.DB, libraryID, deviceID string) (catalogRebuildSnapshot, error) {
	type albumRow struct {
		AlbumVariantID  string
		AlbumClusterID  string
		Title           string
		ArtistsCSV      string
		TrackCount      int64
		BestQualityRank int
	}
	var albumRows []albumRow
	query := `
SELECT
	a.album_variant_id AS album_variant_id,
	a.album_cluster_id AS album_cluster_id,
	a.title,
	COALESCE(GROUP_CONCAT(DISTINCT ar.name), '') AS artists_csv,
	COUNT(DISTINCT at.track_variant_id) AS track_count,
	COALESCE(MAX(sf.quality_rank), 0) AS best_quality_rank
FROM album_variants a
LEFT JOIN album_tracks at ON at.library_id = a.library_id AND at.album_variant_id = a.album_variant_id
LEFT JOIN credits c ON c.library_id = a.library_id AND c.entity_type = 'album' AND c.entity_id = a.album_variant_id
LEFT JOIN artists ar ON ar.library_id = c.library_id AND ar.artist_id = c.artist_id
LEFT JOIN source_files sf ON sf.library_id = at.library_id AND sf.track_variant_id = at.track_variant_id AND sf.is_present = 1
WHERE a.library_id = ?
GROUP BY a.album_variant_id, a.album_cluster_id, a.title
ORDER BY a.album_variant_id ASC`
	if err := tx.Raw(query, strings.TrimSpace(libraryID)).Scan(&albumRows).Error; err != nil {
		return catalogRebuildSnapshot{}, err
	}

	snapshot := catalogRebuildSnapshot{
		albumIDs:              make(map[string]struct{}, len(albumRows)),
		albumClusterIDs:       make(map[string]struct{}, len(albumRows)),
		albumClusterByVariant: make(map[string]string, len(albumRows)),
		albumFamilyByCluster:  make(map[string]string, len(albumRows)),
		clusterIDsByFamily:    make(map[string][]string, len(albumRows)),
		variantsByCluster:     make(map[string][]catalogAlbumVariantSnapshot, len(albumRows)),
		localAlbumIDs:         map[string]struct{}{},
	}
	for _, row := range albumRows {
		albumID := strings.TrimSpace(row.AlbumVariantID)
		clusterID := strings.TrimSpace(row.AlbumClusterID)
		familyKey := normalizeCatalogKey(strings.TrimSpace(row.Title))
		if albumID != "" {
			snapshot.albumIDs[albumID] = struct{}{}
			snapshot.albumClusterByVariant[albumID] = clusterID
			if clusterID != "" {
				snapshot.albumClusterIDs[clusterID] = struct{}{}
				snapshot.albumFamilyByCluster[clusterID] = familyKey
				if !stringSliceContains(snapshot.clusterIDsByFamily[familyKey], clusterID) {
					snapshot.clusterIDsByFamily[familyKey] = append(snapshot.clusterIDsByFamily[familyKey], clusterID)
				}
			}
			snapshot.variantsByCluster[clusterID] = append(snapshot.variantsByCluster[clusterID], catalogAlbumVariantSnapshot{
				AlbumVariantID:  albumID,
				AlbumClusterID:  clusterID,
				Title:           strings.TrimSpace(row.Title),
				TrackCount:      row.TrackCount,
				BestQualityRank: row.BestQualityRank,
			})
		}
	}

	if strings.TrimSpace(deviceID) == "" {
		return snapshot, nil
	}

	type localRow struct {
		AlbumVariantID string
	}
	var localRows []localRow
	localQuery := `
SELECT DISTINCT
	at.album_variant_id AS album_variant_id
FROM album_tracks at
JOIN source_files sf ON sf.library_id = at.library_id AND sf.track_variant_id = at.track_variant_id
WHERE at.library_id = ? AND sf.device_id = ? AND sf.is_present = 1
ORDER BY at.album_variant_id ASC`
	if err := tx.Raw(localQuery, strings.TrimSpace(libraryID), strings.TrimSpace(deviceID)).Scan(&localRows).Error; err != nil {
		return catalogRebuildSnapshot{}, err
	}
	for _, row := range localRows {
		albumID := strings.TrimSpace(row.AlbumVariantID)
		if albumID != "" {
			snapshot.localAlbumIDs[albumID] = struct{}{}
		}
	}
	return snapshot, nil
}

func (a *App) reconcileLocalAlbumArtworkBestEffort(ctx context.Context, local apitypes.LocalContext, albumIDs []string) error {
	if a == nil || a.artwork == nil || len(albumIDs) == 0 {
		return nil
	}

	a.setArtworkActivity(apitypes.ArtworkActivityStatus{
		Phase:          "running",
		AlbumsTotal:    len(albumIDs),
		CurrentAlbumID: albumIDs[0],
		Workers:        1,
		WorkersActive:  1,
	})

	errorCount := 0
	for idx, albumID := range albumIDs {
		if err := a.artwork.reconcileAlbumArtwork(ctx, local, albumID); err != nil {
			if errors.Is(err, context.Canceled) {
				return err
			}
			errorCount++
			a.logf("desktopcore: reconcile album artwork after catalog rebuild failed for album %s: %v", albumID, err)
		}

		nextAlbumID := ""
		workersActive := 0
		if idx+1 < len(albumIDs) {
			nextAlbumID = albumIDs[idx+1]
			workersActive = 1
		}
		a.updateArtworkActivity(func(status *apitypes.ArtworkActivityStatus) {
			status.Phase = "running"
			status.AlbumsTotal = len(albumIDs)
			status.AlbumsDone = idx + 1
			status.CurrentAlbumID = nextAlbumID
			status.Workers = 1
			status.WorkersActive = workersActive
			status.Errors = errorCount
		})
	}
	if errorCount > 0 {
		a.setArtworkActivity(apitypes.ArtworkActivityStatus{
			Phase:       "failed",
			AlbumsTotal: len(albumIDs),
			AlbumsDone:  len(albumIDs),
			Workers:     1,
			Errors:      errorCount,
		})
		return nil
	}
	a.setArtworkActivity(apitypes.ArtworkActivityStatus{
		Phase:       "completed",
		AlbumsTotal: len(albumIDs),
		AlbumsDone:  len(albumIDs),
		Workers:     1,
	})
	return nil
}

func collectArtistsAndCredits(
	libraryID, trackVariantID, albumVariantID string,
	tags Tags,
	artists map[string]Artist,
	credits map[string]Credit,
) {
	seenArtists := make(map[string]struct{}, len(tags.Artists)+1)
	for idx, artistName := range compactNonEmptyStrings(tags.Artists) {
		artistKey := normalizeCatalogKey(artistName)
		if artistKey == "" {
			continue
		}
		artistID := stableNameID("artist", artistKey)
		if _, ok := seenArtists[artistID]; ok {
			continue
		}
		seenArtists[artistID] = struct{}{}
		if _, ok := artists[artistID]; !ok {
			artists[artistID] = Artist{
				LibraryID: libraryID,
				ArtistID:  artistID,
				Name:      artistName,
				NameSort:  strings.ToLower(strings.TrimSpace(artistName)),
			}
		}
		credit := Credit{
			LibraryID:  libraryID,
			EntityType: "track",
			EntityID:   trackVariantID,
			ArtistID:   artistID,
			Role:       "primary",
			Ord:        idx + 1,
		}
		credits[creditKey(credit)] = credit
	}

	albumArtist := firstNonEmpty(tags.AlbumArtist, firstArtist(tags.Artists))
	artistKey := normalizeCatalogKey(albumArtist)
	if artistKey == "" {
		return
	}
	artistID := stableNameID("artist", artistKey)
	if _, ok := artists[artistID]; !ok {
		artists[artistID] = Artist{
			LibraryID: libraryID,
			ArtistID:  artistID,
			Name:      albumArtist,
			NameSort:  strings.ToLower(strings.TrimSpace(albumArtist)),
		}
	}
	credit := Credit{
		LibraryID:  libraryID,
		EntityType: "album",
		EntityID:   albumVariantID,
		ArtistID:   artistID,
		Role:       "primary",
		Ord:        1,
	}
	credits[creditKey(credit)] = credit
}

func createArtistsTx(tx *gorm.DB, artists map[string]Artist) error {
	if len(artists) == 0 {
		return nil
	}
	rows := make([]Artist, 0, len(artists))
	for _, key := range sortedStringKeys(artists) {
		rows = append(rows, artists[key])
	}
	return tx.CreateInBatches(rows, 200).Error
}

func createCreditsTx(tx *gorm.DB, credits map[string]Credit) error {
	if len(credits) == 0 {
		return nil
	}
	rows := make([]Credit, 0, len(credits))
	for _, key := range sortedStringKeys(credits) {
		rows = append(rows, credits[key])
	}
	return tx.CreateInBatches(rows, 400).Error
}

func createAlbumVariantsTx(tx *gorm.DB, albums map[string]AlbumVariantModel) error {
	if len(albums) == 0 {
		return nil
	}
	rows := make([]AlbumVariantModel, 0, len(albums))
	for _, key := range sortedStringKeys(albums) {
		rows = append(rows, albums[key])
	}
	return tx.CreateInBatches(rows, 200).Error
}

func createTrackVariantsTx(tx *gorm.DB, tracks map[string]TrackVariantModel) error {
	if len(tracks) == 0 {
		return nil
	}
	rows := make([]TrackVariantModel, 0, len(tracks))
	for _, key := range sortedStringKeys(tracks) {
		rows = append(rows, tracks[key])
	}
	return tx.CreateInBatches(rows, 400).Error
}

func createAlbumTracksTx(tx *gorm.DB, albumTracks map[string]AlbumTrack) error {
	if len(albumTracks) == 0 {
		return nil
	}
	rows := make([]AlbumTrack, 0, len(albumTracks))
	for _, key := range sortedStringKeys(albumTracks) {
		rows = append(rows, albumTracks[key])
	}
	return tx.CreateInBatches(rows, 400).Error
}

func assignStrictAlbumClusterIDs(albums map[string]AlbumVariantModel, albumTracks map[string]AlbumTrack, tracks map[string]TrackVariantModel, albumGroupKeys map[string]string) {
	if len(albums) == 0 {
		return
	}

	tracksByAlbum := make(map[string][]AlbumTrack, len(albums))
	for _, row := range albumTracks {
		albumID := strings.TrimSpace(row.AlbumVariantID)
		if albumID == "" {
			continue
		}
		tracksByAlbum[albumID] = append(tracksByAlbum[albumID], row)
	}

	for albumID, album := range albums {
		ordered := append([]AlbumTrack(nil), tracksByAlbum[albumID]...)
		sort.SliceStable(ordered, func(i, j int) bool {
			if ordered[i].DiscNo != ordered[j].DiscNo {
				return ordered[i].DiscNo < ordered[j].DiscNo
			}
			if ordered[i].TrackNo != ordered[j].TrackNo {
				return ordered[i].TrackNo < ordered[j].TrackNo
			}
			return ordered[i].TrackVariantID < ordered[j].TrackVariantID
		})

		signatureParts := make([]string, 0, len(ordered)+1)
		signatureParts = append(signatureParts, strings.TrimSpace(albumGroupKeys[albumID]))
		for _, track := range ordered {
			clusterID := strings.TrimSpace(tracks[track.TrackVariantID].TrackClusterID)
			signatureParts = append(signatureParts, strings.Join([]string{
				intKey(track.DiscNo),
				intKey(track.TrackNo),
				clusterID,
			}, ":"))
		}
		album.AlbumClusterID = stableNameID("library_album", strings.Join(signatureParts, "|"))
		albums[albumID] = album
	}
}

func albumTrackKey(row AlbumTrack) string {
	return fmt.Sprintf(
		"%s:%s:%s:%d:%d",
		strings.TrimSpace(row.LibraryID),
		strings.TrimSpace(row.AlbumVariantID),
		strings.TrimSpace(row.TrackVariantID),
		row.DiscNo,
		row.TrackNo,
	)
}

func creditKey(row Credit) string {
	return fmt.Sprintf(
		"%s:%s:%s:%s:%s:%d",
		strings.TrimSpace(row.LibraryID),
		strings.TrimSpace(row.EntityType),
		strings.TrimSpace(row.EntityID),
		strings.TrimSpace(row.ArtistID),
		strings.TrimSpace(row.Role),
		row.Ord,
	)
}

func diffStringSet(left, right map[string]struct{}) []string {
	out := make([]string, 0, len(left))
	for key := range left {
		if _, ok := right[key]; ok {
			continue
		}
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func latestNonZeroTime(values ...time.Time) time.Time {
	var chosen time.Time
	for _, value := range values {
		if value.IsZero() {
			continue
		}
		if chosen.IsZero() || chosen.Before(value) {
			chosen = value.UTC()
		}
	}
	return chosen
}

func sortedStringKeys[T any](items map[string]T) []string {
	keys := make([]string, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
