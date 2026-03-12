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
		DeviceID:     strings.TrimSpace(row.DeviceID),
		SourceFileID: strings.TrimSpace(row.SourceFileID),
		LibraryID:    strings.TrimSpace(row.LibraryID),
		LocalPath:    filepath.Clean(strings.TrimSpace(row.LocalPath)),
		MTimeNS:      row.MTimeNS,
		SizeBytes:    row.SizeBytes,
		HashAlgo:     strings.TrimSpace(row.HashAlgo),
		HashHex:      strings.TrimSpace(row.HashHex),
		Tags:         tags,
		IsPresent:    row.IsPresent,
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
	trackVariantID := stableNameID("recording", recordingKey)
	trackClusterID := stableNameID("track_cluster", recordingKey)
	albumVariantID := stableNameID("album", albumKey)
	albumClusterID := stableNameID("album_cluster", groupKey)

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
		Columns:   []clause.Column{{Name: "library_id"}, {Name: "key_norm"}},
		DoUpdates: clause.AssignmentColumns([]string{"album_variant_id", "album_cluster_id", "title", "year", "updated_at"}),
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
		Columns:   []clause.Column{{Name: "library_id"}, {Name: "key_norm"}},
		DoUpdates: clause.AssignmentColumns([]string{"track_variant_id", "track_cluster_id", "title", "duration_ms", "updated_at"}),
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

	pathKey := localPathKey(in.Path)
	sourceFingerprint := strings.TrimSpace(in.HashAlgo) + ":" + strings.TrimSpace(in.HashHex)
	if err := tx.
		Where("library_id = ? AND device_id = ? AND path_key = ? AND source_fingerprint <> ?", in.LibraryID, in.DeviceID, pathKey, sourceFingerprint).
		Delete(&SourceFileModel{}).Error; err != nil {
		return err
	}

	content := SourceFileModel{
		LibraryID:         in.LibraryID,
		DeviceID:          in.DeviceID,
		SourceFileID:      in.SourceFileID,
		TrackVariantID:    trackVariantID,
		LocalPath:         filepath.Clean(in.Path),
		PathKey:           pathKey,
		SourceFingerprint: sourceFingerprint,
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
	return tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "library_id"}, {Name: "device_id"}, {Name: "source_fingerprint"}},
		DoUpdates: clause.AssignmentColumns([]string{"source_file_id", "track_variant_id", "local_path", "path_key", "hash_algo", "hash_hex", "m_time_ns", "size_bytes", "container", "codec", "bitrate", "sample_rate", "channels", "is_lossless", "quality_rank", "duration_ms", "tags_json", "last_seen_at", "is_present", "updated_at"}),
	}).Create(&content).Error
}
