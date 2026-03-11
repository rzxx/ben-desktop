package corebridge

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	apiruntime "ben/core/api/runtime"
	apitypes "ben/core/api/types"
)

type Config struct {
	Core apitypes.Config
}

type RuntimeBridge struct {
	runtime *apiruntime.Runtime
}

type UnavailableBridge struct {
	err error
}

func Open(ctx context.Context, cfg Config) (*RuntimeBridge, error) {
	runtime, err := apiruntime.Open(ctx, cfg.Core)
	if err != nil {
		return nil, err
	}
	return &RuntimeBridge{runtime: runtime}, nil
}

func OpenFromEnv(ctx context.Context) (*RuntimeBridge, error) {
	return Open(ctx, configFromEnv())
}

func configFromEnv() Config {
	dbPath := strings.TrimSpace(os.Getenv("BEN_CORE_DB_PATH"))
	blobRoot := strings.TrimSpace(os.Getenv("BEN_CORE_BLOB_ROOT"))
	if blobRoot == "" && dbPath != "" {
		blobRoot = filepath.Join(filepath.Dir(dbPath), "blobs")
	}
	identityKeyPath := strings.TrimSpace(os.Getenv("BEN_CORE_IDENTITY_KEY_PATH"))
	if identityKeyPath == "" && dbPath != "" {
		identityKeyPath = filepath.Join(filepath.Dir(dbPath), "identity.key")
	}
	autoStart := true

	return Config{
		Core: apitypes.Config{
			DBPath:           dbPath,
			BlobRoot:         blobRoot,
			IdentityKeyPath:  identityKeyPath,
			FFmpegPath:       strings.TrimSpace(os.Getenv("BEN_CORE_FFMPEG_PATH")),
			TranscodeProfile: strings.TrimSpace(os.Getenv("BEN_CORE_TRANSCODE_PROFILE")),
			Runtime: apitypes.RuntimeConfig{
				AutoStart: &autoStart,
			},
		},
	}
}

func NewUnavailableBridge(err error) *UnavailableBridge {
	if err == nil {
		err = fmt.Errorf("core runtime is not configured")
	}
	return &UnavailableBridge{err: err}
}

func (b *RuntimeBridge) Close() error {
	if b == nil || b.runtime == nil {
		return nil
	}
	return b.runtime.Close()
}

func (b *RuntimeBridge) ListRecordings(ctx context.Context, req apitypes.RecordingListRequest) (apitypes.Page[apitypes.RecordingListItem], error) {
	return b.runtime.ListRecordings(ctx, req)
}

func (b *RuntimeBridge) GetRecording(ctx context.Context, recordingID string) (apitypes.RecordingListItem, error) {
	return b.runtime.GetRecording(ctx, recordingID)
}

func (b *RuntimeBridge) ListAlbumTracks(ctx context.Context, req apitypes.AlbumTrackListRequest) (apitypes.Page[apitypes.AlbumTrackItem], error) {
	return b.runtime.ListAlbumTracks(ctx, req)
}

func (b *RuntimeBridge) ListPlaylistTracks(ctx context.Context, req apitypes.PlaylistTrackListRequest) (apitypes.Page[apitypes.PlaylistTrackItem], error) {
	return b.runtime.ListPlaylistTracks(ctx, req)
}

func (b *RuntimeBridge) ListLikedRecordings(ctx context.Context, req apitypes.LikedRecordingListRequest) (apitypes.Page[apitypes.LikedRecordingItem], error) {
	return b.runtime.ListLikedRecordings(ctx, req)
}

func (b *RuntimeBridge) ResolvePlaybackRecording(ctx context.Context, recordingID, preferredProfile string) (apitypes.PlaybackResolveResult, error) {
	return b.runtime.ResolvePlaybackRecording(ctx, recordingID, preferredProfile)
}

func (b *RuntimeBridge) InspectPlaybackRecording(ctx context.Context, recordingID, preferredProfile string) (apitypes.PlaybackPreparationStatus, error) {
	return b.runtime.InspectPlaybackRecording(ctx, recordingID, preferredProfile)
}

func (b *RuntimeBridge) PreparePlaybackRecording(ctx context.Context, recordingID, preferredProfile string, purpose apitypes.PlaybackPreparationPurpose) (apitypes.PlaybackPreparationStatus, error) {
	return b.runtime.PreparePlaybackRecording(ctx, recordingID, preferredProfile, purpose)
}

func (b *RuntimeBridge) GetPlaybackPreparation(ctx context.Context, recordingID, preferredProfile string) (apitypes.PlaybackPreparationStatus, error) {
	return b.runtime.GetPlaybackPreparation(ctx, recordingID, preferredProfile)
}

func (b *RuntimeBridge) ResolveRecordingArtwork(ctx context.Context, recordingID, variant string) (apitypes.RecordingArtworkResult, error) {
	return b.runtime.ResolveRecordingArtwork(ctx, recordingID, variant)
}

func (b *RuntimeBridge) GetRecordingAvailability(ctx context.Context, recordingID, preferredProfile string) (apitypes.RecordingPlaybackAvailability, error) {
	return b.runtime.GetRecordingAvailability(ctx, recordingID, preferredProfile)
}

func (b *UnavailableBridge) Close() error {
	return nil
}

func (b *UnavailableBridge) ListRecordings(context.Context, apitypes.RecordingListRequest) (apitypes.Page[apitypes.RecordingListItem], error) {
	return apitypes.Page[apitypes.RecordingListItem]{}, b.err
}

func (b *UnavailableBridge) GetRecording(context.Context, string) (apitypes.RecordingListItem, error) {
	return apitypes.RecordingListItem{}, b.err
}

func (b *UnavailableBridge) ListAlbumTracks(context.Context, apitypes.AlbumTrackListRequest) (apitypes.Page[apitypes.AlbumTrackItem], error) {
	return apitypes.Page[apitypes.AlbumTrackItem]{}, b.err
}

func (b *UnavailableBridge) ListPlaylistTracks(context.Context, apitypes.PlaylistTrackListRequest) (apitypes.Page[apitypes.PlaylistTrackItem], error) {
	return apitypes.Page[apitypes.PlaylistTrackItem]{}, b.err
}

func (b *UnavailableBridge) ListLikedRecordings(context.Context, apitypes.LikedRecordingListRequest) (apitypes.Page[apitypes.LikedRecordingItem], error) {
	return apitypes.Page[apitypes.LikedRecordingItem]{}, b.err
}

func (b *UnavailableBridge) ResolvePlaybackRecording(context.Context, string, string) (apitypes.PlaybackResolveResult, error) {
	return apitypes.PlaybackResolveResult{}, b.err
}

func (b *UnavailableBridge) InspectPlaybackRecording(context.Context, string, string) (apitypes.PlaybackPreparationStatus, error) {
	return apitypes.PlaybackPreparationStatus{}, b.err
}

func (b *UnavailableBridge) PreparePlaybackRecording(context.Context, string, string, apitypes.PlaybackPreparationPurpose) (apitypes.PlaybackPreparationStatus, error) {
	return apitypes.PlaybackPreparationStatus{}, b.err
}

func (b *UnavailableBridge) GetPlaybackPreparation(context.Context, string, string) (apitypes.PlaybackPreparationStatus, error) {
	return apitypes.PlaybackPreparationStatus{}, b.err
}

func (b *UnavailableBridge) ResolveRecordingArtwork(context.Context, string, string) (apitypes.RecordingArtworkResult, error) {
	return apitypes.RecordingArtworkResult{}, b.err
}

func (b *UnavailableBridge) GetRecordingAvailability(context.Context, string, string) (apitypes.RecordingPlaybackAvailability, error) {
	return apitypes.RecordingPlaybackAvailability{}, b.err
}
