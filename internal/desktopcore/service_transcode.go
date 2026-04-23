package desktopcore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	apitypes "ben/desktop/api/types"

	"gorm.io/gorm"
)

const (
	audioProfileVBRStandard = "aac_lc_vbr_standard"
	audioProfileVBRHigh     = "aac_lc_vbr_high"
	audioProfileVBRVeryHigh = "aac_lc_vbr_very_high"
)

var (
	ErrProviderOnlyTranscode = errors.New("provider-only transcode authority")
	ErrUnsupportedProfile    = errors.New("unsupported audio profile")
)

type AudioProfile struct {
	ID         string
	VBRQuality string
}

type AACTranscodeBuilder interface {
	BuildAAC(ctx context.Context, sourcePath string, profile AudioProfile) ([]byte, error)
}

type ffmpegAACBuilder struct {
	ffmpegPath string
}

type transcodeFlight struct {
	done chan struct{}
	err  error
}

type TranscodeService struct {
	app     *App
	builder AACTranscodeBuilder

	mu      sync.Mutex
	flights map[string]*transcodeFlight
}

func newTranscodeService(app *App) *TranscodeService {
	if app == nil {
		return nil
	}
	builder := app.cfg.TranscodeBuilder
	if builder == nil {
		builder = &ffmpegAACBuilder{ffmpegPath: strings.TrimSpace(app.cfg.FFmpegPath)}
	}
	return &TranscodeService{
		app:     app,
		builder: builder,
		flights: make(map[string]*transcodeFlight),
	}
}

func resolveAudioProfile(profile string) (AudioProfile, error) {
	switch strings.TrimSpace(profile) {
	case "", "desktop", audioProfileVBRHigh:
		return AudioProfile{ID: audioProfileVBRHigh, VBRQuality: "1.5"}, nil
	case audioProfileVBRStandard:
		return AudioProfile{ID: audioProfileVBRStandard, VBRQuality: "1.25"}, nil
	case audioProfileVBRVeryHigh:
		return AudioProfile{ID: audioProfileVBRVeryHigh, VBRQuality: "2.0"}, nil
	default:
		return AudioProfile{}, fmt.Errorf("%w: %q", ErrUnsupportedProfile, strings.TrimSpace(profile))
	}
}

func (b *ffmpegAACBuilder) BuildAAC(ctx context.Context, sourcePath string, profile AudioProfile) ([]byte, error) {
	sourcePath = strings.TrimSpace(sourcePath)
	if sourcePath == "" {
		return nil, fmt.Errorf("source path is required")
	}
	if strings.TrimSpace(profile.ID) == "" {
		return nil, fmt.Errorf("audio profile is required")
	}
	ffmpegPath := strings.TrimSpace(b.ffmpegPath)
	if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}

	tempDir, err := os.MkdirTemp("", "ben-transcode-*")
	if err != nil {
		return nil, fmt.Errorf("create transcode temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	out := filepath.Join(tempDir, "out.m4a")
	args := []string{
		"-hide_banner", "-loglevel", "error", "-y",
		"-i", sourcePath,
		"-vn",
		"-c:a", "aac",
		"-q:a", profile.VBRQuality,
		"-movflags", "+faststart",
		out,
	}
	if err := runFFmpeg(ctx, ffmpegPath, args); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(out)
	if err != nil {
		return nil, fmt.Errorf("read transcode output: %w", err)
	}
	return data, nil
}

func runFFmpeg(ctx context.Context, ffmpegPath string, args []string) error {
	cmd := exec.CommandContext(ctx, ffmpegPath, args...)
	configureBackgroundProcess(cmd)
	output, err := cmd.CombinedOutput()
	if err != nil {
		var execErr *exec.Error
		if errors.As(err, &execErr) && errors.Is(execErr.Err, exec.ErrNotFound) {
			return fmt.Errorf("ffmpeg executable %q not found (install ffmpeg or pass a valid path): %w", ffmpegPath, err)
		}
		stderr := strings.TrimSpace(string(output))
		if stderr == "" {
			return fmt.Errorf("ffmpeg failed: %w", err)
		}
		return fmt.Errorf("ffmpeg failed: %w (%s)", err, stderr)
	}
	return nil
}

func (s *TranscodeService) EnsureRecordingEncoding(ctx context.Context, local apitypes.LocalContext, recordingID, preferredProfile, requesterDeviceID string) (bool, error) {
	if s == nil || s.app == nil {
		return false, fmt.Errorf("transcode service is not available")
	}
	recordingID = strings.TrimSpace(recordingID)
	if recordingID == "" {
		return false, fmt.Errorf("recording id is required")
	}
	spec, err := resolveAudioProfile(firstNonEmpty(strings.TrimSpace(preferredProfile), strings.TrimSpace(s.app.cfg.TranscodeProfile)))
	if err != nil {
		return false, err
	}
	source, found, err := s.bestLocalRecordingSource(ctx, local.LibraryID, local.DeviceID, recordingID)
	if err != nil {
		return false, err
	}
	if !found {
		return false, nil
	}
	if _, err := os.Stat(source.LocalPath); err != nil {
		return false, nil
	}
	if !canProvideLocalMedia(local.Role) {
		return false, ErrProviderOnlyTranscode
	}

	if asset, ok, err := s.findEncodingForSource(ctx, local.LibraryID, source.SourceFileID, spec.ID); err != nil {
		return false, err
	} else if ok {
		if blobPath, blobErr := s.blobPath(asset.BlobID); blobErr == nil {
			if _, statErr := os.Stat(blobPath); statErr == nil {
				if err := s.markAssetCached(ctx, local, asset.OptimizedAssetID); err != nil {
					return false, err
				}
				return false, nil
			}
		}
	}

	key := strings.TrimSpace(local.LibraryID) + "|" + strings.TrimSpace(source.SourceFileID) + "|" + spec.ID
	activityKey := key + "|local"
	flight, leader := s.beginFlight(key)
	if !leader {
		s.app.setTranscodeActivity(activityKey, apitypes.TranscodeActivityStatus{
			RecordingID:       recordingID,
			SourceFileID:      source.SourceFileID,
			SourcePath:        source.LocalPath,
			Profile:           spec.ID,
			RequestKind:       "local",
			RequesterDeviceID: strings.TrimSpace(requesterDeviceID),
			Phase:             "waiting_existing",
			StartedAt:         time.Now().UTC(),
		})
		defer s.app.clearTranscodeActivity(activityKey)
		if err := waitForTranscodeFlight(ctx, flight); err != nil {
			return false, err
		}
		if asset, ok, err := s.findEncodingForSource(ctx, local.LibraryID, source.SourceFileID, spec.ID); err != nil {
			return false, err
		} else if ok {
			if err := s.markAssetCached(ctx, local, asset.OptimizedAssetID); err != nil {
				return false, err
			}
		}
		return false, nil
	}

	var runErr error
	defer func() {
		s.finishFlight(key, flight, runErr)
	}()
	s.app.setTranscodeActivity(activityKey, apitypes.TranscodeActivityStatus{
		RecordingID:       recordingID,
		SourceFileID:      source.SourceFileID,
		SourcePath:        source.LocalPath,
		Profile:           spec.ID,
		RequestKind:       "local",
		RequesterDeviceID: strings.TrimSpace(requesterDeviceID),
		Phase:             "running",
		StartedAt:         time.Now().UTC(),
	})
	defer s.app.clearTranscodeActivity(activityKey)

	if asset, ok, err := s.findEncodingForSource(ctx, local.LibraryID, source.SourceFileID, spec.ID); err != nil {
		runErr = err
		return false, err
	} else if ok {
		if err := s.markAssetCached(ctx, local, asset.OptimizedAssetID); err != nil {
			runErr = err
			return false, err
		}
		return false, nil
	}

	encoded, err := s.builder.BuildAAC(ctx, source.LocalPath, spec)
	if err != nil {
		runErr = err
		return false, err
	}
	blobID, err := s.storeBlobBytes(encoded)
	if err != nil {
		runErr = err
		return false, err
	}

	now := time.Now().UTC()
	encodingID := stableNameID("encoding", source.SourceFileID+"|"+spec.ID)
	bitrate := measuredAverageBitrate(len(encoded), source.DurationMS)
	if err := s.app.storage.Transaction(ctx, func(tx *gorm.DB) error {
		if err := s.app.upsertOptimizedAssetTx(tx, local, OptimizedAssetModel{
			OptimizedAssetID:  encodingID,
			SourceFileID:      source.SourceFileID,
			TrackVariantID:    source.TrackVariantID,
			Profile:           spec.ID,
			BlobID:            blobID,
			MIME:              "audio/mp4",
			DurationMS:        source.DurationMS,
			Bitrate:           bitrate,
			Codec:             "aac",
			Container:         "m4a",
			CreatedByDeviceID: local.DeviceID,
			CreatedAt:         now,
			UpdatedAt:         now,
		}); err != nil {
			return err
		}
		lastVerified := now
		return s.app.upsertDeviceAssetCacheTx(tx, local, DeviceAssetCacheModel{
			DeviceID:         local.DeviceID,
			OptimizedAssetID: encodingID,
			IsCached:         true,
			LastVerifiedAt:   &lastVerified,
			UpdatedAt:        now,
		})
	}); err != nil {
		runErr = err
		return false, err
	}

	return true, nil
}

func (s *TranscodeService) bestLocalRecordingSource(ctx context.Context, libraryID, deviceID, recordingID string) (SourceFileModel, bool, error) {
	query := `
SELECT sf.*
FROM source_files sf
JOIN track_variants req ON req.library_id = sf.library_id
JOIN track_variants cand ON cand.library_id = sf.library_id AND cand.track_variant_id = sf.track_variant_id
WHERE sf.library_id = ? AND sf.device_id = ? AND sf.is_present = 1 AND req.track_variant_id = ? AND cand.track_cluster_id = req.track_cluster_id
ORDER BY CASE WHEN sf.track_variant_id = ? THEN 0 ELSE 1 END ASC, sf.last_seen_at DESC, sf.quality_rank DESC, sf.size_bytes DESC, sf.local_path ASC
LIMIT 1`
	var row SourceFileModel
	if err := s.app.storage.WithContext(ctx).Raw(query, libraryID, deviceID, recordingID, recordingID).Scan(&row).Error; err != nil {
		return SourceFileModel{}, false, err
	}
	if strings.TrimSpace(row.SourceFileID) == "" {
		return SourceFileModel{}, false, nil
	}
	return row, true, nil
}

func (s *TranscodeService) findEncodingForSource(ctx context.Context, libraryID, sourceFileID, profile string) (OptimizedAssetModel, bool, error) {
	var row OptimizedAssetModel
	err := s.app.storage.WithContext(ctx).
		Where("library_id = ? AND source_file_id = ? AND profile = ?", libraryID, sourceFileID, profile).
		Take(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return OptimizedAssetModel{}, false, nil
		}
		return OptimizedAssetModel{}, false, err
	}
	return row, true, nil
}

func (s *TranscodeService) markAssetCached(ctx context.Context, local apitypes.LocalContext, optimizedAssetID string) error {
	now := time.Now().UTC()
	return s.app.storage.Transaction(ctx, func(tx *gorm.DB) error {
		lastVerified := now
		return s.app.upsertDeviceAssetCacheTx(tx, local, DeviceAssetCacheModel{
			DeviceID:         local.DeviceID,
			OptimizedAssetID: strings.TrimSpace(optimizedAssetID),
			IsCached:         true,
			LastVerifiedAt:   &lastVerified,
			UpdatedAt:        now,
		})
	})
}

func (s *TranscodeService) storeBlobBytes(data []byte) (string, error) {
	return s.app.blobs.StoreBytes(data)
}

func (s *TranscodeService) readVerifiedBlob(blobID string) ([]byte, error) {
	return s.app.blobs.ReadVerified(blobID)
}

func (s *TranscodeService) blobPath(blobID string) (string, error) {
	return s.app.blobs.Path(blobID)
}

func measuredAverageBitrate(bytes int, durationMS int64) int {
	if bytes <= 0 || durationMS <= 0 {
		return 0
	}
	return int((int64(bytes) * 8 * 1000) / durationMS)
}

func (s *TranscodeService) beginFlight(key string) (*transcodeFlight, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if flight, ok := s.flights[key]; ok {
		return flight, false
	}
	flight := &transcodeFlight{done: make(chan struct{})}
	s.flights[key] = flight
	return flight, true
}

func (s *TranscodeService) finishFlight(key string, flight *transcodeFlight, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if current, ok := s.flights[key]; ok && current == flight {
		delete(s.flights, key)
	}
	flight.err = err
	close(flight.done)
}

func waitForTranscodeFlight(ctx context.Context, flight *transcodeFlight) error {
	if flight == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-flight.done:
		return flight.err
	}
}
