package desktopcore

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	apitypes "ben/desktop/api/types"
)

func TestMemberSyncBuildResponseProjectsMobileEntities(t *testing.T) {
	ctx := context.Background()
	app := openPlaylistTestApp(t)

	library, err := app.CreateLibrary(ctx, "member-sync")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.EnsureLocalContext(ctx)
	if err != nil {
		t.Fatalf("ensure local context: %v", err)
	}
	now := time.Now().UTC()
	if err := app.db.WithContext(ctx).
		Model(&Device{}).
		Where("device_id = ?", local.DeviceID).
		Update("last_seen_at", now).Error; err != nil {
		t.Fatalf("touch local device presence: %v", err)
	}

	remoteDeviceID := seedRemoteLibraryMember(t, app, library.LibraryID, "device-mobile", now)
	if err := app.db.WithContext(ctx).Model(&Device{}).
		Where("device_id = ?", remoteDeviceID).
		Update("peer_id", "peer-mobile").Error; err != nil {
		t.Fatalf("set remote peer id: %v", err)
	}

	seedSourceOnlyRecording(t, app, library.LibraryID, local.DeviceID, playbackSeedInput{
		RecordingID:    "rec-mobile",
		TrackClusterID: "rec-mobile",
		AlbumID:        "album-mobile",
		AlbumClusterID: "album-mobile",
		SourceFileID:   "src-mobile",
		QualityRank:    100,
	})
	if err := app.db.WithContext(ctx).Create(&Artist{
		LibraryID: library.LibraryID,
		ArtistID:  "artist-mobile",
		Name:      "Artist Mobile",
		NameSort:  "Artist Mobile",
	}).Error; err != nil {
		t.Fatalf("seed artist: %v", err)
	}
	if err := app.db.WithContext(ctx).Create([]Credit{
		{LibraryID: library.LibraryID, EntityType: "album", EntityID: "album-mobile", ArtistID: "artist-mobile", Role: "primary", Ord: 1},
		{LibraryID: library.LibraryID, EntityType: "track", EntityID: "rec-mobile", ArtistID: "artist-mobile", Role: "primary", Ord: 1},
	}).Error; err != nil {
		t.Fatalf("seed credits: %v", err)
	}

	playlist, err := app.CreatePlaylist(ctx, "Queue", string(apitypes.PlaylistKindNormal))
	if err != nil {
		t.Fatalf("create playlist: %v", err)
	}
	if _, err := app.AddPlaylistItem(ctx, apitypes.PlaylistAddItemRequest{
		PlaylistID:  playlist.PlaylistID,
		RecordingID: "rec-mobile",
	}); err != nil {
		t.Fatalf("add playlist item: %v", err)
	}
	if err := app.LikeRecording(ctx, "rec-mobile"); err != nil {
		t.Fatalf("like recording: %v", err)
	}
	if err := app.db.WithContext(ctx).Create(&PlaylistCover{
		LibraryID:    library.LibraryID,
		PlaylistID:   playlist.PlaylistID,
		BlobID:       "b3:playlist-cover",
		MIME:         "image/jpeg",
		FileExt:      ".jpg",
		W:            1200,
		H:            1200,
		Bytes:        4096,
		ChosenSource: "uploaded_file",
		UpdatedAt:    now,
	}).Error; err != nil {
		t.Fatalf("seed playlist cover: %v", err)
	}

	resp, err := app.memberSync.buildSyncResponse(ctx, MemberSyncRequest{
		LibraryID: library.LibraryID,
		DeviceID:  remoteDeviceID,
		PeerID:    "peer-mobile",
		MaxOps:    1000,
	})
	if err != nil {
		t.Fatalf("build member sync response: %v", err)
	}
	if resp.NeedCheckpoint {
		t.Fatalf("unexpected checkpoint response: %+v", resp.Checkpoint)
	}
	if resp.LatestVersion == 0 {
		t.Fatalf("expected non-zero latest version")
	}

	payloadByType := make(map[string]map[string]any)
	countByType := make(map[string]int)
	for _, op := range resp.Ops {
		countByType[op.EntityType]++
		var payload map[string]any
		if err := json.Unmarshal(op.PayloadJSON, &payload); err != nil {
			t.Fatalf("decode payload for %s/%s: %v", op.EntityType, op.EntityID, err)
		}
		if _, ok := payloadByType[op.EntityType]; !ok {
			payloadByType[op.EntityType] = payload
		}
	}

	for _, entityType := range []string{
		"library",
		"library_member",
		"artist",
		"album",
		"recording",
		"album_availability",
		"recording_playback_availability",
		"playlist",
		"playlist_track",
		"liked_recording",
	} {
		if countByType[entityType] == 0 {
			t.Fatalf("expected entity type %q in projection, counts=%+v", entityType, countByType)
		}
	}

	playlistPayload := payloadByType["playlist"]
	thumb, _ := playlistPayload["thumb"].(map[string]any)
	if thumb["blob_id"] != "b3:playlist-cover" {
		t.Fatalf("playlist thumb blob_id = %#v, want b3:playlist-cover", thumb["blob_id"])
	}

	recordingPayload := payloadByType["recording"]
	if recordingPayload["availability_hint"] == "" {
		t.Fatalf("expected recording availability hint in payload: %+v", recordingPayload)
	}
}

func TestMemberCheckpointFetchResponseReturnsPublishedProjection(t *testing.T) {
	ctx := context.Background()
	app := openPlaylistTestApp(t)

	library, err := app.CreateLibrary(ctx, "member-checkpoint")
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	local, err := app.EnsureLocalContext(ctx)
	if err != nil {
		t.Fatalf("ensure local context: %v", err)
	}
	now := time.Now().UTC()
	if err := app.db.WithContext(ctx).
		Model(&Device{}).
		Where("device_id = ?", local.DeviceID).
		Update("last_seen_at", now).Error; err != nil {
		t.Fatalf("touch local device presence: %v", err)
	}

	targetDeviceID := seedRemoteLibraryMember(t, app, library.LibraryID, "device-member-checkpoint", now)
	seedPlaylistRecording(t, app, library.LibraryID, "rec-checkpoint", "Track")

	playlist, err := app.CreatePlaylist(ctx, "Queue", string(apitypes.PlaylistKindNormal))
	if err != nil {
		t.Fatalf("create playlist: %v", err)
	}
	if _, err := app.AddPlaylistItem(ctx, apitypes.PlaylistAddItemRequest{
		PlaylistID:  playlist.PlaylistID,
		RecordingID: "rec-checkpoint",
	}); err != nil {
		t.Fatalf("add playlist item: %v", err)
	}

	record, err := app.memberSync.publishCheckpointForTargetDevice(ctx, library.LibraryID, targetDeviceID)
	if err != nil {
		t.Fatalf("publish member checkpoint: %v", err)
	}
	if record.Manifest.CheckpointID == "" || record.Manifest.EntryCount == 0 {
		t.Fatalf("unexpected member checkpoint manifest: %+v", record.Manifest)
	}

	fetchResp, err := app.memberSync.buildCheckpointFetchResponse(ctx, MemberCheckpointFetchRequest{
		LibraryID:    library.LibraryID,
		CheckpointID: record.Manifest.CheckpointID,
		Auth: transportPeerAuth{
			Cert: membershipCertEnvelope{
				DeviceID: targetDeviceID,
				PeerID:   "peer-mobile",
			},
		},
	})
	if err != nil {
		t.Fatalf("build member checkpoint fetch response: %v", err)
	}
	if fetchResp.Record.Manifest.CheckpointID != record.Manifest.CheckpointID {
		t.Fatalf("fetched checkpoint id = %q, want %q", fetchResp.Record.Manifest.CheckpointID, record.Manifest.CheckpointID)
	}
	if len(fetchResp.Record.Chunks) == 0 {
		t.Fatalf("expected checkpoint chunks")
	}
}
