package desktopcore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	apitypes "ben/desktop/api/types"
	"ben/desktop/internal/settings"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const defaultInspectSettingsAppName = "ben-desktop"

type inspectContextError struct {
	msg        string
	resolution ContextResolution
}

func (e *inspectContextError) Error() string {
	if e == nil {
		return ""
	}
	return e.msg
}

func OpenInspector(cfg InspectConfig) (*Inspector, error) {
	resolved, err := resolveInspectConfig(cfg)
	if err != nil {
		return nil, err
	}

	db, err := openReadOnlySQLite(resolved.DBPath)
	if err != nil {
		return nil, fmt.Errorf("open inspector sqlite: %w", err)
	}
	storage := NewDBService(db)

	appCfg, err := ResolveConfig(Config{
		DBPath:           resolved.DBPath,
		BlobRoot:         resolved.BlobRoot,
		FFmpegPath:       resolved.FFmpegPath,
		TranscodeProfile: resolved.PreferredProfile,
	})
	if err != nil {
		_ = storage.Close()
		return nil, err
	}

	app := &App{
		cfg:                 appCfg,
		db:                  db,
		storage:             storage,
		blobs:               NewBlobStoreService(appCfg.BlobRoot),
		activity:            newActivityStatus(),
		jobs:                NewJobsService(),
		catalogEvents:       NewCatalogEventsService(),
		pinEvents:           NewPinEventsService(),
		activitySubscribers: make(map[uint64]func(apitypes.ActivityStatus)),
		tagReader: func() TagReader {
			if appCfg.TagReader != nil {
				return appCfg.TagReader
			}
			return NewTagReader()
		}(),
	}
	app.operator = newOperatorService(app)
	app.catalog = &CatalogService{app: app}
	app.cache = &CacheService{app: app}
	app.pin = newPinService(app)
	app.playlist = &PlaylistService{app: app}
	app.playback = newPlaybackService(app)

	return &Inspector{
		cfg:          resolved,
		app:          app,
		mediaChecker: newInspectMediaChecker(strings.TrimSpace(appCfg.FFmpegPath)),
	}, nil
}

func (i *Inspector) Close() error {
	if i == nil || i.app == nil {
		return nil
	}
	return i.app.Close()
}

func (i *Inspector) ResolveContext(ctx context.Context, req ResolveInspectContextRequest) (ContextResolution, error) {
	return i.resolveContext(ctx, req)
}

func resolveInspectConfig(cfg InspectConfig) (InspectConfig, error) {
	cfg.DBPath = strings.TrimSpace(cfg.DBPath)
	cfg.BlobRoot = strings.TrimSpace(cfg.BlobRoot)
	cfg.SettingsAppName = strings.TrimSpace(cfg.SettingsAppName)
	cfg.FFmpegPath = strings.TrimSpace(cfg.FFmpegPath)
	cfg.PreferredProfile = strings.TrimSpace(cfg.PreferredProfile)

	if cfg.DBPath == "" {
		path, err := DefaultDBPath()
		if err != nil {
			return InspectConfig{}, err
		}
		cfg.DBPath = path
	}
	if cfg.BlobRoot == "" {
		path, err := DefaultBlobRoot()
		if err != nil {
			return InspectConfig{}, err
		}
		cfg.BlobRoot = path
	}
	if cfg.SettingsAppName == "" {
		cfg.SettingsAppName = defaultInspectSettingsAppName
	}
	if cfg.PreferredProfile == "" || cfg.FFmpegPath == "" {
		path, err := settings.DefaultPath(cfg.SettingsAppName)
		if err == nil {
			store, storeErr := settings.NewStore(path)
			if storeErr == nil {
				state, loadErr := store.Load()
				_ = store.Close()
				if loadErr == nil {
					if cfg.PreferredProfile == "" {
						cfg.PreferredProfile = settings.EffectiveTranscodeProfile(state.Core.TranscodeProfile)
					}
					if cfg.FFmpegPath == "" {
						cfg.FFmpegPath = strings.TrimSpace(state.Core.FFmpegPath)
					}
				}
			}
		}
	}
	if cfg.PreferredProfile == "" {
		cfg.PreferredProfile = settings.DefaultTranscodeProfile
	}
	return cfg, nil
}

func openReadOnlySQLite(path string) (*gorm.DB, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("db path is required")
	}
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	dsn := fmt.Sprintf("file:%s?mode=ro", filepath.ToSlash(absPath))
	db, err := gorm.Open(sqlite.Dialector{DriverName: "sqlite", DSN: dsn}, &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, err
	}
	if err := db.Exec("PRAGMA query_only=ON;").Error; err != nil {
		return nil, fmt.Errorf("set query_only on: %w", err)
	}
	if err := db.Exec("PRAGMA busy_timeout=5000;").Error; err != nil {
		return nil, fmt.Errorf("set busy timeout: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(1)
	return db, nil
}

func (i *Inspector) resolveContext(ctx context.Context, req ResolveInspectContextRequest) (ContextResolution, error) {
	resolution := ContextResolution{
		Selected: InspectContext{
			PreferredProfile: i.resolvePreferredProfile(req.PreferredProfile),
			NetworkRunning:   false,
		},
	}
	if req.NetworkRunning != nil {
		resolution.Selected.NetworkRunning = *req.NetworkRunning
	}

	requestedLibraryID := strings.TrimSpace(req.LibraryID)
	requestedDeviceID := strings.TrimSpace(req.DeviceID)

	libraries, err := i.availableLibraries(ctx, requestedDeviceID)
	if err != nil {
		return resolution, err
	}
	devices, err := i.availableDevices(ctx, requestedLibraryID)
	if err != nil {
		return resolution, err
	}
	resolution.AvailableLibraries = libraries
	resolution.AvailableDevices = devices

	libraryID := requestedLibraryID
	deviceID := requestedDeviceID
	source := "explicit"

	if libraryID != "" {
		ok, err := i.libraryExists(ctx, libraryID)
		if err != nil {
			return resolution, err
		}
		if !ok {
			return resolution, fmt.Errorf("library %s not found", libraryID)
		}
	} else {
		if len(libraries) != 1 {
			resolution.Ambiguous = len(libraries) > 1
			return resolution, &inspectContextError{
				msg:        "library_id is required because multiple libraries are available",
				resolution: resolution,
			}
		}
		libraryID = libraries[0].LibraryID
		source = "inferred_library"
	}

	if requestedLibraryID == "" {
		devices, err = i.availableDevices(ctx, libraryID)
		if err != nil {
			return resolution, err
		}
		resolution.AvailableDevices = devices
	}

	if deviceID != "" {
		ok, err := i.deviceExists(ctx, deviceID)
		if err != nil {
			return resolution, err
		}
		if !ok {
			return resolution, fmt.Errorf("device %s not found", deviceID)
		}
	} else {
		if len(devices) != 1 {
			resolution.Ambiguous = len(devices) > 1
			return resolution, &inspectContextError{
				msg:        "device_id is required because multiple devices are available",
				resolution: resolution,
			}
		}
		deviceID = devices[0].DeviceID
		if source == "explicit" {
			source = "inferred_device"
		} else {
			source = "inferred_library_device"
		}
	}

	resolution.Selected.LibraryID = libraryID
	resolution.Selected.DeviceID = deviceID
	resolution.InferenceSource = source
	return resolution, nil
}

func (i *Inspector) resolvePreferredProfile(requested string) string {
	requested = strings.TrimSpace(requested)
	if requested != "" {
		return settings.EffectiveTranscodeProfile(requested)
	}
	if i == nil {
		return settings.DefaultTranscodeProfile
	}
	return settings.EffectiveTranscodeProfile(i.cfg.PreferredProfile)
}

func (i *Inspector) availableLibraries(ctx context.Context, deviceID string) ([]InspectLibraryCandidate, error) {
	type row struct {
		LibraryID string
		Name      string
	}
	var rows []row
	query := i.app.storage.WithContext(ctx).Table("libraries").Select("libraries.library_id, libraries.name")
	deviceID = strings.TrimSpace(deviceID)
	if deviceID != "" {
		query = query.Joins("JOIN memberships ON memberships.library_id = libraries.library_id").Where("memberships.device_id = ?", deviceID)
	}
	if err := query.Order("libraries.library_id ASC").Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]InspectLibraryCandidate, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		libraryID := strings.TrimSpace(row.LibraryID)
		if libraryID == "" {
			continue
		}
		if _, ok := seen[libraryID]; ok {
			continue
		}
		seen[libraryID] = struct{}{}
		out = append(out, InspectLibraryCandidate{
			LibraryID: libraryID,
			Name:      strings.TrimSpace(row.Name),
		})
	}
	return out, nil
}

func (i *Inspector) availableDevices(ctx context.Context, libraryID string) ([]InspectDeviceCandidate, error) {
	type row struct {
		DeviceID   string
		Name       string
		PeerID     string
		Role       string
		LastSeenAt *time.Time
	}
	var rows []row
	query := i.app.storage.WithContext(ctx).
		Table("devices").
		Select("devices.device_id, devices.name, COALESCE(devices.peer_id, '') AS peer_id, COALESCE(memberships.role, '') AS role, devices.last_seen_at").
		Joins("LEFT JOIN memberships ON memberships.device_id = devices.device_id")
	libraryID = strings.TrimSpace(libraryID)
	if libraryID != "" {
		query = query.Where("memberships.library_id = ?", libraryID)
	}
	if err := query.Order("devices.device_id ASC").Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]InspectDeviceCandidate, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		deviceID := strings.TrimSpace(row.DeviceID)
		if deviceID == "" {
			continue
		}
		if _, ok := seen[deviceID]; ok {
			continue
		}
		seen[deviceID] = struct{}{}
		out = append(out, InspectDeviceCandidate{
			DeviceID:   deviceID,
			Name:       strings.TrimSpace(row.Name),
			PeerID:     strings.TrimSpace(row.PeerID),
			Role:       strings.TrimSpace(row.Role),
			LastSeenAt: row.LastSeenAt,
		})
	}
	return out, nil
}

func (i *Inspector) libraryExists(ctx context.Context, libraryID string) (bool, error) {
	var count int64
	if err := i.app.storage.WithContext(ctx).Model(&Library{}).Where("library_id = ?", strings.TrimSpace(libraryID)).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (i *Inspector) deviceExists(ctx context.Context, deviceID string) (bool, error) {
	var count int64
	if err := i.app.storage.WithContext(ctx).Model(&Device{}).Where("device_id = ?", strings.TrimSpace(deviceID)).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (i *Inspector) resolveLocalContext(ctx context.Context, req ResolveInspectContextRequest) (ContextResolution, apitypes.LocalContext, error) {
	resolution, err := i.resolveContext(ctx, req)
	if err != nil {
		return resolution, apitypes.LocalContext{}, err
	}

	local := apitypes.LocalContext{
		LibraryID: resolution.Selected.LibraryID,
		DeviceID:  resolution.Selected.DeviceID,
	}
	var row struct {
		Name   string
		PeerID string
		Role   string
	}
	err = i.app.storage.WithContext(ctx).
		Table("devices").
		Select("devices.name, COALESCE(devices.peer_id, '') AS peer_id, COALESCE(memberships.role, '') AS role").
		Joins("LEFT JOIN memberships ON memberships.device_id = devices.device_id AND memberships.library_id = ?", local.LibraryID).
		Where("devices.device_id = ?", local.DeviceID).
		Take(&row).Error
	switch {
	case err == nil:
		local.Device = strings.TrimSpace(row.Name)
		local.PeerID = strings.TrimSpace(row.PeerID)
		local.Role = strings.TrimSpace(row.Role)
	case errors.Is(err, gorm.ErrRecordNotFound):
	default:
		return resolution, apitypes.LocalContext{}, err
	}
	return resolution, local, nil
}

func inspectorRequest(payload any) any {
	data, err := json.Marshal(payload)
	if err != nil {
		return payload
	}
	var out any
	if err := json.Unmarshal(data, &out); err != nil {
		return payload
	}
	return out
}

func traceShell(payload any) map[string]any {
	return map[string]any{
		"request": payload,
	}
}

func sortAnomalies(items []InspectAnomaly) []InspectAnomaly {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Severity != items[j].Severity {
			return items[i].Severity < items[j].Severity
		}
		return items[i].Code < items[j].Code
	})
	return items
}

func dumpTrackVariants(rows []TrackVariantModel) []map[string]any {
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, map[string]any{
			"library_id":       strings.TrimSpace(row.LibraryID),
			"track_variant_id": strings.TrimSpace(row.TrackVariantID),
			"track_cluster_id": strings.TrimSpace(row.TrackClusterID),
			"key_norm":         strings.TrimSpace(row.KeyNorm),
			"title":            strings.TrimSpace(row.Title),
			"duration_ms":      row.DurationMS,
		})
	}
	return out
}

func dumpAlbumVariants(rows []AlbumVariantModel) []map[string]any {
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, map[string]any{
			"library_id":       strings.TrimSpace(row.LibraryID),
			"album_variant_id": strings.TrimSpace(row.AlbumVariantID),
			"album_cluster_id": strings.TrimSpace(row.AlbumClusterID),
			"title":            strings.TrimSpace(row.Title),
			"year":             row.Year,
			"edition":          strings.TrimSpace(row.Edition),
			"key_norm":         strings.TrimSpace(row.KeyNorm),
		})
	}
	return out
}

func dumpAlbumTracks(rows []AlbumTrack) []map[string]any {
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, map[string]any{
			"library_id":       strings.TrimSpace(row.LibraryID),
			"album_variant_id": strings.TrimSpace(row.AlbumVariantID),
			"track_variant_id": strings.TrimSpace(row.TrackVariantID),
			"disc_no":          row.DiscNo,
			"track_no":         row.TrackNo,
			"title_override":   row.TitleOverride,
		})
	}
	return out
}

func dumpPreferences(rows []DeviceVariantPreference) []map[string]any {
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, map[string]any{
			"library_id":        strings.TrimSpace(row.LibraryID),
			"device_id":         strings.TrimSpace(row.DeviceID),
			"scope_type":        strings.TrimSpace(row.ScopeType),
			"cluster_id":        strings.TrimSpace(row.ClusterID),
			"chosen_variant_id": strings.TrimSpace(row.ChosenVariantID),
		})
	}
	return out
}

func dumpSourceFiles(rows []SourceFileModel) []map[string]any {
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, map[string]any{
			"library_id":         strings.TrimSpace(row.LibraryID),
			"device_id":          strings.TrimSpace(row.DeviceID),
			"source_file_id":     strings.TrimSpace(row.SourceFileID),
			"track_variant_id":   strings.TrimSpace(row.TrackVariantID),
			"source_fingerprint": strings.TrimSpace(row.SourceFingerprint),
			"container":          strings.TrimSpace(row.Container),
			"codec":              strings.TrimSpace(row.Codec),
			"bitrate":            row.Bitrate,
			"sample_rate":        row.SampleRate,
			"channels":           row.Channels,
			"is_lossless":        row.IsLossless,
			"quality_rank":       row.QualityRank,
			"duration_ms":        row.DurationMS,
			"is_present":         row.IsPresent,
			"local_path":         strings.TrimSpace(row.LocalPath),
			"edition_scope_key":  strings.TrimSpace(row.EditionScopeKey),
			"hash_algo":          strings.TrimSpace(row.HashAlgo),
			"hash_hex":           strings.TrimSpace(row.HashHex),
			"size_bytes":         row.SizeBytes,
		})
	}
	return out
}

func dumpOptimizedAssets(rows []OptimizedAssetModel) []map[string]any {
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, map[string]any{
			"library_id":           strings.TrimSpace(row.LibraryID),
			"optimized_asset_id":   strings.TrimSpace(row.OptimizedAssetID),
			"source_file_id":       strings.TrimSpace(row.SourceFileID),
			"track_variant_id":     strings.TrimSpace(row.TrackVariantID),
			"profile":              strings.TrimSpace(row.Profile),
			"blob_id":              strings.TrimSpace(row.BlobID),
			"mime":                 strings.TrimSpace(row.MIME),
			"duration_ms":          row.DurationMS,
			"bitrate":              row.Bitrate,
			"codec":                strings.TrimSpace(row.Codec),
			"container":            strings.TrimSpace(row.Container),
			"created_by_device_id": strings.TrimSpace(row.CreatedByDeviceID),
		})
	}
	return out
}

func dumpDeviceAssetCaches(rows []DeviceAssetCacheModel) []map[string]any {
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, map[string]any{
			"library_id":         strings.TrimSpace(row.LibraryID),
			"device_id":          strings.TrimSpace(row.DeviceID),
			"optimized_asset_id": strings.TrimSpace(row.OptimizedAssetID),
			"is_cached":          row.IsCached,
			"last_verified_at":   row.LastVerifiedAt,
		})
	}
	return out
}

func dumpPinMembers(rows []PinMember) []map[string]any {
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, map[string]any{
			"library_id":           strings.TrimSpace(row.LibraryID),
			"device_id":            strings.TrimSpace(row.DeviceID),
			"scope":                strings.TrimSpace(row.Scope),
			"scope_id":             strings.TrimSpace(row.ScopeID),
			"profile":              strings.TrimSpace(row.Profile),
			"variant_recording_id": strings.TrimSpace(row.VariantRecordingID),
			"library_recording_id": strings.TrimSpace(row.LibraryRecordingID),
			"resolution_policy":    strings.TrimSpace(row.ResolutionPolicy),
			"pending":              row.Pending,
			"last_error":           strings.TrimSpace(row.LastError),
		})
	}
	return out
}

func dumpPinBlobRefs(rows []PinBlobRef) []map[string]any {
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, map[string]any{
			"library_id":         strings.TrimSpace(row.LibraryID),
			"device_id":          strings.TrimSpace(row.DeviceID),
			"scope":              strings.TrimSpace(row.Scope),
			"scope_id":           strings.TrimSpace(row.ScopeID),
			"profile":            strings.TrimSpace(row.Profile),
			"blob_id":            strings.TrimSpace(row.BlobID),
			"ref_kind":           strings.TrimSpace(row.RefKind),
			"subject_id":         strings.TrimSpace(row.SubjectID),
			"recording_id":       strings.TrimSpace(row.RecordingID),
			"artwork_scope_type": strings.TrimSpace(row.ArtworkScopeType),
			"artwork_scope_id":   strings.TrimSpace(row.ArtworkScopeID),
			"artwork_variant":    strings.TrimSpace(row.ArtworkVariant),
		})
	}
	return out
}

func dumpDevices(rows []Device) []map[string]any {
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		activeLibraryID := ""
		if row.ActiveLibraryID != nil {
			activeLibraryID = strings.TrimSpace(*row.ActiveLibraryID)
		}
		out = append(out, map[string]any{
			"device_id":         strings.TrimSpace(row.DeviceID),
			"name":              strings.TrimSpace(row.Name),
			"peer_id":           strings.TrimSpace(row.PeerID),
			"active_library_id": activeLibraryID,
			"last_seen_at":      row.LastSeenAt,
		})
	}
	return out
}

func dumpMemberships(rows []Membership) []map[string]any {
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, map[string]any{
			"library_id": strings.TrimSpace(row.LibraryID),
			"device_id":  strings.TrimSpace(row.DeviceID),
			"role":       strings.TrimSpace(row.Role),
		})
	}
	return out
}

func dumpPlaylists(rows []Playlist) []map[string]any {
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, map[string]any{
			"library_id":  strings.TrimSpace(row.LibraryID),
			"playlist_id": strings.TrimSpace(row.PlaylistID),
			"name":        strings.TrimSpace(row.Name),
			"kind":        strings.TrimSpace(row.Kind),
			"created_by":  strings.TrimSpace(row.CreatedBy),
			"deleted_at":  row.DeletedAt,
		})
	}
	return out
}

func dumpPlaylistItems(rows []PlaylistItem) []map[string]any {
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, map[string]any{
			"library_id":       strings.TrimSpace(row.LibraryID),
			"playlist_id":      strings.TrimSpace(row.PlaylistID),
			"item_id":          strings.TrimSpace(row.ItemID),
			"track_variant_id": strings.TrimSpace(row.TrackVariantID),
			"position_key":     strings.TrimSpace(row.PositionKey),
			"added_at":         row.AddedAt,
			"deleted_at":       row.DeletedAt,
		})
	}
	return out
}

func dumpArtworkVariants(rows []ArtworkVariant) []map[string]any {
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, map[string]any{
			"library_id":        strings.TrimSpace(row.LibraryID),
			"scope_type":        strings.TrimSpace(row.ScopeType),
			"scope_id":          strings.TrimSpace(row.ScopeID),
			"variant":           strings.TrimSpace(row.Variant),
			"blob_id":           strings.TrimSpace(row.BlobID),
			"mime":              strings.TrimSpace(row.MIME),
			"file_ext":          strings.TrimSpace(row.FileExt),
			"bytes":             row.Bytes,
			"chosen_source":     strings.TrimSpace(row.ChosenSource),
			"chosen_source_ref": strings.TrimSpace(row.ChosenSourceRef),
		})
	}
	return out
}
