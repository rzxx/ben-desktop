package desktopcore

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func tagsSnapshotJSON(tags Tags) (string, error) {
	raw, err := json.Marshal(map[string]any{
		"title":        tags.Title,
		"album":        tags.Album,
		"album_artist": tags.AlbumArtist,
		"artists":      tags.Artists,
		"track":        tags.TrackNo,
		"disc":         tags.DiscNo,
		"year":         tags.Year,
		"duration_ms":  tags.DurationMS,
		"container":    tags.Container,
		"codec":        tags.Codec,
		"bitrate":      tags.Bitrate,
		"sample_rate":  tags.SampleRate,
		"channels":     tags.Channels,
		"is_lossless":  tags.IsLossless,
		"quality_rank": tags.QualityRank,
	})
	if err != nil {
		return "", fmt.Errorf("marshal tags: %w", err)
	}
	return string(raw), nil
}

func tagsFromSnapshotJSON(raw string) (Tags, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "{}" {
		return Tags{}, nil
	}

	var payload struct {
		Title       string   `json:"title"`
		Album       string   `json:"album"`
		AlbumArtist string   `json:"album_artist"`
		Artists     []string `json:"artists"`
		TrackNo     int      `json:"track"`
		DiscNo      int      `json:"disc"`
		Year        int      `json:"year"`
		DurationMS  int64    `json:"duration_ms"`
		Container   string   `json:"container"`
		Codec       string   `json:"codec"`
		Bitrate     int      `json:"bitrate"`
		SampleRate  int      `json:"sample_rate"`
		Channels    int      `json:"channels"`
		IsLossless  bool     `json:"is_lossless"`
		QualityRank int      `json:"quality_rank"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return Tags{}, fmt.Errorf("decode tags snapshot: %w", err)
	}
	return Tags{
		Title:       payload.Title,
		Album:       payload.Album,
		AlbumArtist: payload.AlbumArtist,
		Artists:     append([]string(nil), payload.Artists...),
		TrackNo:     payload.TrackNo,
		DiscNo:      payload.DiscNo,
		Year:        payload.Year,
		DurationMS:  payload.DurationMS,
		Container:   payload.Container,
		Codec:       payload.Codec,
		Bitrate:     payload.Bitrate,
		SampleRate:  payload.SampleRate,
		Channels:    payload.Channels,
		IsLossless:  payload.IsLossless,
		QualityRank: payload.QualityRank,
	}, nil
}

func sourceFileOplogPayloadFromRow(row SourceFileModel) (sourceFileOplogPayload, error) {
	tags, err := tagsFromSnapshotJSON(row.TagsJSON)
	if err != nil {
		return sourceFileOplogPayload{}, err
	}
	return sourceFileOplogPayload{
		DeviceID:        strings.TrimSpace(row.DeviceID),
		SourceFileID:    strings.TrimSpace(row.SourceFileID),
		LibraryID:       strings.TrimSpace(row.LibraryID),
		EditionScopeKey: strings.TrimSpace(row.EditionScopeKey),
		MTimeNS:         row.MTimeNS,
		SizeBytes:       row.SizeBytes,
		HashAlgo:        strings.TrimSpace(row.HashAlgo),
		HashHex:         strings.TrimSpace(row.HashHex),
		Tags:            tags,
		IsPresent:       row.IsPresent,
	}, nil
}

func conflictingPathSourceFilesTx(tx *gorm.DB, libraryID, deviceID, pathKey, sourceFingerprint string) ([]SourceFileModel, error) {
	var rows []SourceFileModel
	if err := tx.
		Where("library_id = ? AND device_id = ? AND path_key = ? AND source_fingerprint <> ?", libraryID, deviceID, pathKey, sourceFingerprint).
		Order("source_file_id ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func upsertIngestTx(tx *gorm.DB, in ingestRecord, mutatedAt time.Time, isPresent bool) error {
	if mutatedAt.IsZero() {
		mutatedAt = time.Now().UTC()
	}

	recordingKey, albumKey, groupKey := normalizedRecordKeys(in.Tags)
	editionScopeKey := strings.TrimSpace(in.EditionScopeKey)
	if editionScopeKey == "" {
		editionScopeKey = normalizeCatalogKey(strings.Join([]string{
			firstNonEmpty(in.Tags.AlbumArtist, firstArtist(in.Tags.Artists)),
			in.Tags.Album,
		}, "|"))
	}
	trackVariantID := explicitTrackVariantID(recordingKey, editionScopeKey, in.Tags.DiscNo, in.Tags.TrackNo)
	trackClusterID := stableNameID("track_cluster", recordingKey)
	albumVariantID := explicitAlbumVariantID(albumKey, editionScopeKey)
	albumClusterID := stableNameID("library_album", groupKey)

	tagsJSON, err := tagsSnapshotJSON(in.Tags)
	if err != nil {
		return err
	}

	album := AlbumVariantModel{
		LibraryID:      in.LibraryID,
		AlbumVariantID: albumVariantID,
		AlbumClusterID: albumClusterID,
		Title:          in.Tags.Album,
		KeyNorm:        albumKey,
		CreatedAt:      mutatedAt,
		UpdatedAt:      mutatedAt,
	}
	if in.Tags.Year > 0 {
		year := in.Tags.Year
		album.Year = &year
	}
	if err := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "library_id"}, {Name: "album_variant_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"album_cluster_id", "title", "year", "updated_at"}),
	}).Create(&album).Error; err != nil {
		return err
	}

	recording := TrackVariantModel{
		LibraryID:      in.LibraryID,
		TrackVariantID: trackVariantID,
		TrackClusterID: trackClusterID,
		KeyNorm:        recordingKey,
		Title:          in.Tags.Title,
		DurationMS:     in.Tags.DurationMS,
		CreatedAt:      mutatedAt,
		UpdatedAt:      mutatedAt,
	}
	if err := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "library_id"}, {Name: "track_variant_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"track_cluster_id", "title", "duration_ms", "updated_at"}),
	}).Create(&recording).Error; err != nil {
		return err
	}

	if err := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "library_id"}, {Name: "album_variant_id"}, {Name: "track_variant_id"}, {Name: "disc_no"}, {Name: "track_no"}},
		DoNothing: true,
	}).Create(&AlbumTrack{
		LibraryID:      in.LibraryID,
		AlbumVariantID: albumVariantID,
		TrackVariantID: trackVariantID,
		DiscNo:         maxTrackNumber(in.Tags.DiscNo),
		TrackNo:        maxTrackNumber(in.Tags.TrackNo),
	}).Error; err != nil {
		return err
	}

	if err := upsertArtistsAndCredits(tx, in.LibraryID, trackVariantID, albumVariantID, in.Tags.Artists, firstNonEmpty(in.Tags.AlbumArtist, firstArtist(in.Tags.Artists))); err != nil {
		return err
	}

	localPath := strings.TrimSpace(in.Path)
	if localPath != "" {
		localPath = filepath.Clean(localPath)
	}
	pathKey := ""
	if localPath != "" {
		pathKey = localPathKey(localPath)
	}
	if pathKey == "" {
		pathKey = opaqueSourcePathKey(in.SourceFileID)
	}
	sourceFingerprint := strings.TrimSpace(in.HashAlgo) + ":" + strings.TrimSpace(in.HashHex)
	if localPath != "" {
		if err := tx.
			Where("library_id = ? AND device_id = ? AND path_key = ? AND source_fingerprint <> ?", in.LibraryID, in.DeviceID, pathKey, sourceFingerprint).
			Delete(&SourceFileModel{}).Error; err != nil {
			return err
		}
		if err := upsertLocalSourcePathTx(tx, in.LibraryID, in.DeviceID, in.SourceFileID, localPath, mutatedAt); err != nil {
			return err
		}
	} else if storedPath, storedPathKey, err := resolveStoredSourcePathTx(tx, in.LibraryID, in.DeviceID, in.SourceFileID); err != nil {
		return err
	} else if storedPath != "" {
		localPath = storedPath
		pathKey = storedPathKey
	}

	content := SourceFileModel{
		LibraryID:         in.LibraryID,
		DeviceID:          in.DeviceID,
		SourceFileID:      in.SourceFileID,
		TrackVariantID:    trackVariantID,
		LocalPath:         localPath,
		PathKey:           pathKey,
		SourceFingerprint: sourceFingerprint,
		EditionScopeKey:   editionScopeKey,
		HashAlgo:          in.HashAlgo,
		HashHex:           in.HashHex,
		MTimeNS:           in.MTimeNS,
		SizeBytes:         in.SizeBytes,
		Container:         in.Tags.Container,
		Codec:             in.Tags.Codec,
		Bitrate:           in.Tags.Bitrate,
		SampleRate:        in.Tags.SampleRate,
		Channels:          in.Tags.Channels,
		IsLossless:        in.Tags.IsLossless,
		QualityRank:       in.Tags.QualityRank,
		DurationMS:        in.Tags.DurationMS,
		TagsJSON:          tagsJSON,
		LastSeenAt:        mutatedAt,
		IsPresent:         isPresent,
		CreatedAt:         mutatedAt,
		UpdatedAt:         mutatedAt,
	}
	values := sourceFileUpsertValues(content)
	return tx.Model(&SourceFileModel{}).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "library_id"}, {Name: "device_id"}, {Name: "path_key"}},
		DoUpdates: clause.Assignments(sourceFileConflictAssignments(content)),
	}).Create(values).Error
}

func sourceFileUpsertValues(content SourceFileModel) map[string]any {
	return map[string]any{
		"library_id":         content.LibraryID,
		"device_id":          content.DeviceID,
		"source_file_id":     content.SourceFileID,
		"track_variant_id":   content.TrackVariantID,
		"local_path":         content.LocalPath,
		"path_key":           content.PathKey,
		"source_fingerprint": content.SourceFingerprint,
		"edition_scope_key":  content.EditionScopeKey,
		"hash_algo":          content.HashAlgo,
		"hash_hex":           content.HashHex,
		"m_time_ns":          content.MTimeNS,
		"size_bytes":         content.SizeBytes,
		"container":          content.Container,
		"codec":              content.Codec,
		"bitrate":            content.Bitrate,
		"sample_rate":        content.SampleRate,
		"channels":           content.Channels,
		"is_lossless":        content.IsLossless,
		"quality_rank":       content.QualityRank,
		"duration_ms":        content.DurationMS,
		"tags_json":          content.TagsJSON,
		"last_seen_at":       content.LastSeenAt,
		"is_present":         content.IsPresent,
		"created_at":         content.CreatedAt,
		"updated_at":         content.UpdatedAt,
	}
}

func sourceFileConflictAssignments(content SourceFileModel) map[string]any {
	return map[string]any{
		"source_file_id":     content.SourceFileID,
		"track_variant_id":   content.TrackVariantID,
		"local_path":         content.LocalPath,
		"path_key":           content.PathKey,
		"source_fingerprint": content.SourceFingerprint,
		"edition_scope_key":  content.EditionScopeKey,
		"hash_algo":          content.HashAlgo,
		"hash_hex":           content.HashHex,
		"m_time_ns":          content.MTimeNS,
		"size_bytes":         content.SizeBytes,
		"container":          content.Container,
		"codec":              content.Codec,
		"bitrate":            content.Bitrate,
		"sample_rate":        content.SampleRate,
		"channels":           content.Channels,
		"is_lossless":        content.IsLossless,
		"quality_rank":       content.QualityRank,
		"duration_ms":        content.DurationMS,
		"tags_json":          content.TagsJSON,
		"last_seen_at":       content.LastSeenAt,
		"is_present":         content.IsPresent,
		"updated_at":         content.UpdatedAt,
	}
}
