package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	apitypes "ben/core/api/types"
	"ben/desktop/internal/desktopcore"
)

func TestLibraryFacadeForwardsToBridge(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	summary := apitypes.LibrarySummary{LibraryID: "lib-1", Name: "Library", Role: "admin", JoinedAt: time.Now().UTC(), IsActive: true}
	member := apitypes.LibraryMemberStatus{LibraryID: "lib-1", DeviceID: "dev-1", Role: "admin"}
	scanStats := apitypes.ScanStats{Scanned: 1, Imported: 1}
	calls := make([]string, 0, 10)
	host := &coreHost{
		started: true,
		bridge: &passthroughBridgeStub{
			UnavailableCore: desktopcore.NewUnavailableCore(errors.New("unused")),
			listLibrariesFn: func(context.Context) ([]apitypes.LibrarySummary, error) {
				calls = append(calls, "list")
				return []apitypes.LibrarySummary{summary}, nil
			},
			activeLibraryFn: func(context.Context) (apitypes.LibrarySummary, bool, error) {
				calls = append(calls, "active")
				return summary, true, nil
			},
			createLibraryFn: func(_ context.Context, name string) (apitypes.LibrarySummary, error) {
				calls = append(calls, "create:"+name)
				return summary, nil
			},
			selectLibraryFn: func(_ context.Context, libraryID string) (apitypes.LibrarySummary, error) {
				calls = append(calls, "select:"+libraryID)
				return summary, nil
			},
			renameLibraryFn: func(_ context.Context, libraryID, name string) (apitypes.LibrarySummary, error) {
				calls = append(calls, "rename:"+libraryID+":"+name)
				return summary, nil
			},
			leaveLibraryFn: func(_ context.Context, libraryID string) error {
				calls = append(calls, "leave:"+libraryID)
				return nil
			},
			deleteLibraryFn: func(_ context.Context, libraryID string) error {
				calls = append(calls, "delete:"+libraryID)
				return nil
			},
			listLibraryMembersFn: func(context.Context) ([]apitypes.LibraryMemberStatus, error) {
				calls = append(calls, "members")
				return []apitypes.LibraryMemberStatus{member}, nil
			},
			updateLibraryMemberRoleFn: func(_ context.Context, deviceID, role string) error {
				calls = append(calls, "role:"+deviceID+":"+role)
				return nil
			},
			removeLibraryMemberFn: func(_ context.Context, deviceID string) error {
				calls = append(calls, "remove:"+deviceID)
				return nil
			},
			rescanNowFn: func(context.Context) (apitypes.ScanStats, error) {
				calls = append(calls, "rescan-now")
				return scanStats, nil
			},
			rescanRootFn: func(_ context.Context, root string) (apitypes.ScanStats, error) {
				calls = append(calls, "rescan-root:"+root)
				return scanStats, nil
			},
		},
	}
	facade := NewLibraryFacade(host)

	if got, err := facade.ListLibraries(ctx); err != nil || len(got) != 1 || got[0].LibraryID != summary.LibraryID {
		t.Fatalf("list libraries = %+v, err=%v", got, err)
	}
	if got, ok, err := facade.ActiveLibrary(ctx); err != nil || !ok || got.LibraryID != summary.LibraryID {
		t.Fatalf("active library = %+v, ok=%v, err=%v", got, ok, err)
	}
	if _, err := facade.CreateLibrary(ctx, "Library"); err != nil {
		t.Fatalf("create library: %v", err)
	}
	if _, err := facade.SelectLibrary(ctx, "lib-1"); err != nil {
		t.Fatalf("select library: %v", err)
	}
	if _, err := facade.RenameLibrary(ctx, "lib-1", "Renamed"); err != nil {
		t.Fatalf("rename library: %v", err)
	}
	if err := facade.LeaveLibrary(ctx, "lib-1"); err != nil {
		t.Fatalf("leave library: %v", err)
	}
	if err := facade.DeleteLibrary(ctx, "lib-1"); err != nil {
		t.Fatalf("delete library: %v", err)
	}
	if got, err := facade.ListLibraryMembers(ctx); err != nil || len(got) != 1 || got[0].DeviceID != member.DeviceID {
		t.Fatalf("list library members = %+v, err=%v", got, err)
	}
	if err := facade.UpdateLibraryMemberRole(ctx, "dev-1", "guest"); err != nil {
		t.Fatalf("update library member role: %v", err)
	}
	if err := facade.RemoveLibraryMember(ctx, "dev-1"); err != nil {
		t.Fatalf("remove library member: %v", err)
	}
	if got, err := facade.RescanNow(ctx); err != nil || got.Scanned != scanStats.Scanned {
		t.Fatalf("rescan now = %+v, err=%v", got, err)
	}
	if got, err := facade.RescanRoot(ctx, "C:/music"); err != nil || got.Imported != scanStats.Imported {
		t.Fatalf("rescan root = %+v, err=%v", got, err)
	}

	want := []string{
		"list",
		"active",
		"create:Library",
		"select:lib-1",
		"rename:lib-1:Renamed",
		"leave:lib-1",
		"delete:lib-1",
		"members",
		"role:dev-1:guest",
		"remove:dev-1",
		"rescan-now",
		"rescan-root:C:/music",
	}
	if strings.Join(calls, "|") != strings.Join(want, "|") {
		t.Fatalf("library facade calls = %v, want %v", calls, want)
	}
}

func TestNetworkFacadeForwardsToBridge(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	local := apitypes.LocalContext{LibraryID: "lib-1", DeviceID: "dev-1", Device: "desktop", Role: "admin", PeerID: "peer-1"}
	summary := apitypes.InspectSummary{Libraries: 1, Devices: 1, Memberships: 1}
	report := apitypes.LibraryOplogDiagnostics{LibraryID: "lib-1"}
	activity := apitypes.ActivityStatus{Scan: apitypes.ScanActivityStatus{Phase: "idle"}}
	network := apitypes.NetworkStatus{LibraryID: "lib-1", DeviceID: "dev-1", ServiceTag: "ben-123"}
	checkpoint := apitypes.LibraryCheckpointStatus{LibraryID: "lib-1", CheckpointID: "ckpt-1"}
	calls := make([]string, 0, 6)
	host := &coreHost{
		started: true,
		bridge: &passthroughBridgeStub{
			UnavailableCore: desktopcore.NewUnavailableCore(errors.New("unused")),
			ensureLocalContextFn: func(context.Context) (apitypes.LocalContext, error) {
				calls = append(calls, "local")
				return local, nil
			},
			inspectFn: func(context.Context) (apitypes.InspectSummary, error) {
				calls = append(calls, "inspect")
				return summary, nil
			},
			inspectLibraryOplogFn: func(_ context.Context, libraryID string) (apitypes.LibraryOplogDiagnostics, error) {
				calls = append(calls, "oplog:"+libraryID)
				return report, nil
			},
			activityStatusFn: func(context.Context) (apitypes.ActivityStatus, error) {
				calls = append(calls, "activity")
				return activity, nil
			},
			networkStatusFn: func() apitypes.NetworkStatus {
				calls = append(calls, "network")
				return network
			},
			checkpointStatusFn: func(context.Context) (apitypes.LibraryCheckpointStatus, error) {
				calls = append(calls, "checkpoint")
				return checkpoint, nil
			},
		},
	}
	facade := NewNetworkFacade(host)

	if got, err := facade.EnsureLocalContext(ctx); err != nil || got.DeviceID != local.DeviceID {
		t.Fatalf("ensure local context = %+v, err=%v", got, err)
	}
	if got, err := facade.Inspect(ctx); err != nil || got.Libraries != summary.Libraries {
		t.Fatalf("inspect = %+v, err=%v", got, err)
	}
	if got, err := facade.InspectLibraryOplog(ctx, "lib-1"); err != nil || got.LibraryID != report.LibraryID {
		t.Fatalf("inspect library oplog = %+v, err=%v", got, err)
	}
	if got, err := facade.ActivityStatus(ctx); err != nil || got.Scan.Phase != activity.Scan.Phase {
		t.Fatalf("activity status = %+v, err=%v", got, err)
	}
	if got := facade.NetworkStatus(); got.ServiceTag != network.ServiceTag {
		t.Fatalf("network status = %+v", got)
	}
	if got, err := facade.CheckpointStatus(ctx); err != nil || got.CheckpointID != checkpoint.CheckpointID {
		t.Fatalf("checkpoint status = %+v, err=%v", got, err)
	}

	want := []string{"local", "inspect", "oplog:lib-1", "activity", "network", "checkpoint"}
	if strings.Join(calls, "|") != strings.Join(want, "|") {
		t.Fatalf("network facade calls = %v, want %v", calls, want)
	}
}

func TestJobsFacadeForwardsToBridge(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	job := desktopcore.JobSnapshot{
		JobID:     "job-1",
		Kind:      "scan",
		LibraryID: "lib-1",
		Phase:     desktopcore.JobPhaseRunning,
	}
	calls := make([]string, 0, 2)
	host := &coreHost{
		started: true,
		bridge: &passthroughBridgeStub{
			UnavailableCore: desktopcore.NewUnavailableCore(errors.New("unused")),
			listJobsFn: func(_ context.Context, libraryID string) ([]desktopcore.JobSnapshot, error) {
				calls = append(calls, "list:"+libraryID)
				return []desktopcore.JobSnapshot{job}, nil
			},
			getJobFn: func(_ context.Context, jobID string) (desktopcore.JobSnapshot, bool, error) {
				calls = append(calls, "get:"+jobID)
				return job, true, nil
			},
		},
	}
	facade := NewJobsFacade(host)

	if got, err := facade.ListJobs(ctx, "lib-1"); err != nil || len(got) != 1 || got[0].JobID != job.JobID {
		t.Fatalf("list jobs = %+v, err=%v", got, err)
	}
	if got, ok, err := facade.GetJob(ctx, "job-1"); err != nil || !ok || got.JobID != job.JobID {
		t.Fatalf("get job = %+v, ok=%v, err=%v", got, ok, err)
	}

	want := []string{"list:lib-1", "get:job-1"}
	if strings.Join(calls, "|") != strings.Join(want, "|") {
		t.Fatalf("jobs facade calls = %v, want %v", calls, want)
	}
}

func TestCatalogFacadeForwardsToBridge(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	album := apitypes.AlbumListItem{AlbumID: "album-1", Title: "Album"}
	artist := apitypes.ArtistListItem{ArtistID: "artist-1", Name: "Artist"}
	recording := apitypes.RecordingListItem{RecordingID: "rec-1", Title: "Track"}
	playlist := apitypes.PlaylistListItem{PlaylistID: "pl-1", Name: "Playlist"}
	liked := apitypes.LikedRecordingItem{RecordingID: "rec-1", Title: "Track"}
	item := apitypes.PlaylistItemRecord{PlaylistID: "pl-1", ItemID: "item-1", RecordingID: "rec-1"}
	pageInfo := apitypes.PageInfo{Returned: 1, Total: 1}
	calls := make([]string, 0, 18)
	host := &coreHost{
		started: true,
		bridge: &passthroughBridgeStub{
			UnavailableCore: desktopcore.NewUnavailableCore(errors.New("unused")),
			listArtistsFn: func(_ context.Context, req apitypes.ArtistListRequest) (apitypes.Page[apitypes.ArtistListItem], error) {
				calls = append(calls, "list-artists")
				return apitypes.Page[apitypes.ArtistListItem]{Items: []apitypes.ArtistListItem{artist}, Page: pageInfo}, nil
			},
			getArtistFn: func(_ context.Context, artistID string) (apitypes.ArtistListItem, error) {
				calls = append(calls, "get-artist:"+artistID)
				return artist, nil
			},
			listArtistAlbumsFn: func(_ context.Context, req apitypes.ArtistAlbumListRequest) (apitypes.Page[apitypes.AlbumListItem], error) {
				calls = append(calls, "artist-albums:"+req.ArtistID)
				return apitypes.Page[apitypes.AlbumListItem]{Items: []apitypes.AlbumListItem{album}, Page: pageInfo}, nil
			},
			listAlbumsFn: func(_ context.Context, req apitypes.AlbumListRequest) (apitypes.Page[apitypes.AlbumListItem], error) {
				calls = append(calls, "list-albums")
				return apitypes.Page[apitypes.AlbumListItem]{Items: []apitypes.AlbumListItem{album}, Page: pageInfo}, nil
			},
			getAlbumFn: func(_ context.Context, albumID string) (apitypes.AlbumListItem, error) {
				calls = append(calls, "get-album:"+albumID)
				return album, nil
			},
			listRecordingsFn: func(_ context.Context, req apitypes.RecordingListRequest) (apitypes.Page[apitypes.RecordingListItem], error) {
				calls = append(calls, "list-recordings")
				return apitypes.Page[apitypes.RecordingListItem]{Items: []apitypes.RecordingListItem{recording}, Page: pageInfo}, nil
			},
			getRecordingFn: func(_ context.Context, recordingID string) (apitypes.RecordingListItem, error) {
				calls = append(calls, "get-recording:"+recordingID)
				return recording, nil
			},
			listRecordingVariantsFn: func(_ context.Context, req apitypes.RecordingVariantListRequest) (apitypes.Page[apitypes.RecordingVariantItem], error) {
				calls = append(calls, "recording-variants:"+req.RecordingID)
				return apitypes.Page[apitypes.RecordingVariantItem]{Page: pageInfo}, nil
			},
			listAlbumVariantsFn: func(_ context.Context, req apitypes.AlbumVariantListRequest) (apitypes.Page[apitypes.AlbumVariantItem], error) {
				calls = append(calls, "album-variants:"+req.AlbumID)
				return apitypes.Page[apitypes.AlbumVariantItem]{Page: pageInfo}, nil
			},
			setPreferredRecordingFn: func(_ context.Context, recordingID, variantRecordingID string) error {
				calls = append(calls, "set-recording-pref:"+recordingID+":"+variantRecordingID)
				return nil
			},
			setPreferredAlbumFn: func(_ context.Context, albumID, variantAlbumID string) error {
				calls = append(calls, "set-album-pref:"+albumID+":"+variantAlbumID)
				return nil
			},
			listAlbumTracksFn: func(_ context.Context, req apitypes.AlbumTrackListRequest) (apitypes.Page[apitypes.AlbumTrackItem], error) {
				calls = append(calls, "album-tracks:"+req.AlbumID)
				return apitypes.Page[apitypes.AlbumTrackItem]{Page: pageInfo}, nil
			},
			listPlaylistsFn: func(_ context.Context, req apitypes.PlaylistListRequest) (apitypes.Page[apitypes.PlaylistListItem], error) {
				calls = append(calls, "list-playlists")
				return apitypes.Page[apitypes.PlaylistListItem]{Items: []apitypes.PlaylistListItem{playlist}, Page: pageInfo}, nil
			},
			getPlaylistSummaryFn: func(_ context.Context, playlistID string) (apitypes.PlaylistListItem, error) {
				calls = append(calls, "get-playlist:"+playlistID)
				return playlist, nil
			},
			listPlaylistTracksFn: func(_ context.Context, req apitypes.PlaylistTrackListRequest) (apitypes.Page[apitypes.PlaylistTrackItem], error) {
				calls = append(calls, "playlist-tracks:"+req.PlaylistID)
				return apitypes.Page[apitypes.PlaylistTrackItem]{Page: pageInfo}, nil
			},
			listLikedRecordingsFn: func(_ context.Context, req apitypes.LikedRecordingListRequest) (apitypes.Page[apitypes.LikedRecordingItem], error) {
				calls = append(calls, "liked")
				return apitypes.Page[apitypes.LikedRecordingItem]{Items: []apitypes.LikedRecordingItem{liked}, Page: pageInfo}, nil
			},
			createPlaylistFn: func(_ context.Context, name, kind string) (apitypes.PlaylistRecord, error) {
				calls = append(calls, "create-playlist:"+name+":"+kind)
				return apitypes.PlaylistRecord{PlaylistID: "pl-1", Name: name, Kind: apitypes.PlaylistKind(kind)}, nil
			},
			renamePlaylistFn: func(_ context.Context, playlistID, name string) (apitypes.PlaylistRecord, error) {
				calls = append(calls, "rename-playlist:"+playlistID+":"+name)
				return apitypes.PlaylistRecord{PlaylistID: playlistID, Name: name}, nil
			},
			deletePlaylistFn: func(_ context.Context, playlistID string) error {
				calls = append(calls, "delete-playlist:"+playlistID)
				return nil
			},
			addPlaylistItemFn: func(_ context.Context, req apitypes.PlaylistAddItemRequest) (apitypes.PlaylistItemRecord, error) {
				calls = append(calls, "add-playlist-item:"+req.PlaylistID+":"+req.RecordingID)
				return item, nil
			},
			movePlaylistItemFn: func(_ context.Context, req apitypes.PlaylistMoveItemRequest) (apitypes.PlaylistItemRecord, error) {
				calls = append(calls, "move-playlist-item:"+req.PlaylistID+":"+req.ItemID)
				return item, nil
			},
			removePlaylistItemFn: func(_ context.Context, playlistID, itemID string) error {
				calls = append(calls, "remove-playlist-item:"+playlistID+":"+itemID)
				return nil
			},
			likeRecordingFn: func(_ context.Context, recordingID string) error {
				calls = append(calls, "like:"+recordingID)
				return nil
			},
			unlikeRecordingFn: func(_ context.Context, recordingID string) error {
				calls = append(calls, "unlike:"+recordingID)
				return nil
			},
			isRecordingLikedFn: func(_ context.Context, recordingID string) (bool, error) {
				calls = append(calls, "is-liked:"+recordingID)
				return true, nil
			},
		},
	}
	facade := NewCatalogFacade(host)

	if _, err := facade.ListArtists(ctx, apitypes.ArtistListRequest{}); err != nil {
		t.Fatalf("list artists: %v", err)
	}
	if _, err := facade.GetArtist(ctx, "artist-1"); err != nil {
		t.Fatalf("get artist: %v", err)
	}
	if _, err := facade.ListArtistAlbums(ctx, apitypes.ArtistAlbumListRequest{ArtistID: "artist-1"}); err != nil {
		t.Fatalf("list artist albums: %v", err)
	}
	if _, err := facade.ListAlbums(ctx, apitypes.AlbumListRequest{}); err != nil {
		t.Fatalf("list albums: %v", err)
	}
	if _, err := facade.GetAlbum(ctx, "album-1"); err != nil {
		t.Fatalf("get album: %v", err)
	}
	if _, err := facade.ListRecordings(ctx, apitypes.RecordingListRequest{}); err != nil {
		t.Fatalf("list recordings: %v", err)
	}
	if _, err := facade.GetRecording(ctx, "rec-1"); err != nil {
		t.Fatalf("get recording: %v", err)
	}
	if _, err := facade.ListRecordingVariants(ctx, apitypes.RecordingVariantListRequest{RecordingID: "rec-1"}); err != nil {
		t.Fatalf("list recording variants: %v", err)
	}
	if _, err := facade.ListAlbumVariants(ctx, apitypes.AlbumVariantListRequest{AlbumID: "album-1"}); err != nil {
		t.Fatalf("list album variants: %v", err)
	}
	if err := facade.SetPreferredRecordingVariant(ctx, "rec-1", "rec-variant"); err != nil {
		t.Fatalf("set preferred recording variant: %v", err)
	}
	if err := facade.SetPreferredAlbumVariant(ctx, "album-1", "album-variant"); err != nil {
		t.Fatalf("set preferred album variant: %v", err)
	}
	if _, err := facade.ListAlbumTracks(ctx, apitypes.AlbumTrackListRequest{AlbumID: "album-1"}); err != nil {
		t.Fatalf("list album tracks: %v", err)
	}
	if _, err := facade.ListPlaylists(ctx, apitypes.PlaylistListRequest{}); err != nil {
		t.Fatalf("list playlists: %v", err)
	}
	if _, err := facade.GetPlaylistSummary(ctx, "pl-1"); err != nil {
		t.Fatalf("get playlist summary: %v", err)
	}
	if _, err := facade.ListPlaylistTracks(ctx, apitypes.PlaylistTrackListRequest{PlaylistID: "pl-1"}); err != nil {
		t.Fatalf("list playlist tracks: %v", err)
	}
	if _, err := facade.ListLikedRecordings(ctx, apitypes.LikedRecordingListRequest{}); err != nil {
		t.Fatalf("list liked recordings: %v", err)
	}
	if _, err := facade.CreatePlaylist(ctx, "Playlist", "normal"); err != nil {
		t.Fatalf("create playlist: %v", err)
	}
	if _, err := facade.RenamePlaylist(ctx, "pl-1", "Renamed"); err != nil {
		t.Fatalf("rename playlist: %v", err)
	}
	if err := facade.DeletePlaylist(ctx, "pl-1"); err != nil {
		t.Fatalf("delete playlist: %v", err)
	}
	if _, err := facade.AddPlaylistItem(ctx, apitypes.PlaylistAddItemRequest{PlaylistID: "pl-1", RecordingID: "rec-1"}); err != nil {
		t.Fatalf("add playlist item: %v", err)
	}
	if _, err := facade.MovePlaylistItem(ctx, apitypes.PlaylistMoveItemRequest{PlaylistID: "pl-1", ItemID: "item-1"}); err != nil {
		t.Fatalf("move playlist item: %v", err)
	}
	if err := facade.RemovePlaylistItem(ctx, "pl-1", "item-1"); err != nil {
		t.Fatalf("remove playlist item: %v", err)
	}
	if err := facade.LikeRecording(ctx, "rec-1"); err != nil {
		t.Fatalf("like recording: %v", err)
	}
	if err := facade.UnlikeRecording(ctx, "rec-1"); err != nil {
		t.Fatalf("unlike recording: %v", err)
	}
	if liked, err := facade.IsRecordingLiked(ctx, "rec-1"); err != nil || !liked {
		t.Fatalf("is recording liked = %v, err=%v", liked, err)
	}
}

func TestInviteFacadeForwardsToBridge(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	invite := apitypes.InviteCodeResult{LibraryID: "lib-1", InviteCode: "code-1", Role: "member"}
	session := apitypes.JoinSession{SessionID: "session-1", RequestID: "req-1", Status: "pending", LibraryID: "lib-1", Pending: true}
	result := apitypes.JoinLibraryResult{LibraryID: "lib-1", DeviceID: "dev-join"}
	request := apitypes.InviteJoinRequestRecord{RequestID: "req-1", LibraryID: "lib-1", Status: "pending"}
	issued := apitypes.IssuedInviteRecord{InviteID: "invite-1", LibraryID: "lib-1", Status: "active"}
	calls := make([]string, 0, 10)
	host := &coreHost{
		started: true,
		bridge: &passthroughBridgeStub{
			UnavailableCore: desktopcore.NewUnavailableCore(errors.New("unused")),
			createInviteCodeFn: func(_ context.Context, req apitypes.InviteCodeRequest) (apitypes.InviteCodeResult, error) {
				calls = append(calls, "create:"+req.Role)
				return invite, nil
			},
			listIssuedInvitesFn: func(_ context.Context, status string) ([]apitypes.IssuedInviteRecord, error) {
				calls = append(calls, "issued:"+status)
				return []apitypes.IssuedInviteRecord{issued}, nil
			},
			revokeIssuedInviteFn: func(_ context.Context, inviteID, reason string) error {
				calls = append(calls, "revoke:"+inviteID+":"+reason)
				return nil
			},
			startJoinFromInviteFn: func(_ context.Context, req apitypes.JoinFromInviteInput) (apitypes.JoinSession, error) {
				calls = append(calls, "start:"+req.InviteCode)
				return session, nil
			},
			getJoinSessionFn: func(_ context.Context, sessionID string) (apitypes.JoinSession, error) {
				calls = append(calls, "get:"+sessionID)
				return session, nil
			},
			finalizeJoinSessionFn: func(_ context.Context, sessionID string) (apitypes.JoinLibraryResult, error) {
				calls = append(calls, "finalize:"+sessionID)
				return result, nil
			},
			cancelJoinSessionFn: func(_ context.Context, sessionID string) error {
				calls = append(calls, "cancel:"+sessionID)
				return nil
			},
			listJoinRequestsFn: func(_ context.Context, status string) ([]apitypes.InviteJoinRequestRecord, error) {
				calls = append(calls, "requests:"+status)
				return []apitypes.InviteJoinRequestRecord{request}, nil
			},
			approveJoinRequestFn: func(_ context.Context, requestID, role string) error {
				calls = append(calls, "approve:"+requestID+":"+role)
				return nil
			},
			rejectJoinRequestFn: func(_ context.Context, requestID, reason string) error {
				calls = append(calls, "reject:"+requestID+":"+reason)
				return nil
			},
		},
	}
	facade := NewInviteFacade(host)

	if _, err := facade.CreateInviteCode(ctx, apitypes.InviteCodeRequest{Role: "member"}); err != nil {
		t.Fatalf("create invite code: %v", err)
	}
	if got, err := facade.ListIssuedInvites(ctx, "active"); err != nil || len(got) != 1 || got[0].InviteID != issued.InviteID {
		t.Fatalf("list issued invites = %+v, err=%v", got, err)
	}
	if err := facade.RevokeIssuedInvite(ctx, "invite-1", "manual"); err != nil {
		t.Fatalf("revoke issued invite: %v", err)
	}
	if _, err := facade.StartJoinFromInvite(ctx, apitypes.JoinFromInviteInput{InviteCode: "code-1"}); err != nil {
		t.Fatalf("start join from invite: %v", err)
	}
	if _, err := facade.GetJoinSession(ctx, "session-1"); err != nil {
		t.Fatalf("get join session: %v", err)
	}
	if _, err := facade.FinalizeJoinSession(ctx, "session-1"); err != nil {
		t.Fatalf("finalize join session: %v", err)
	}
	if err := facade.CancelJoinSession(ctx, "session-1"); err != nil {
		t.Fatalf("cancel join session: %v", err)
	}
	if got, err := facade.ListJoinRequests(ctx, "pending"); err != nil || len(got) != 1 || got[0].RequestID != request.RequestID {
		t.Fatalf("list join requests = %+v, err=%v", got, err)
	}
	if err := facade.ApproveJoinRequest(ctx, "req-1", "guest"); err != nil {
		t.Fatalf("approve join request: %v", err)
	}
	if err := facade.RejectJoinRequest(ctx, "req-1", "no"); err != nil {
		t.Fatalf("reject join request: %v", err)
	}

	want := []string{
		"create:member",
		"issued:active",
		"revoke:invite-1:manual",
		"start:code-1",
		"get:session-1",
		"finalize:session-1",
		"cancel:session-1",
		"requests:pending",
		"approve:req-1:guest",
		"reject:req-1:no",
	}
	if strings.Join(calls, "|") != strings.Join(want, "|") {
		t.Fatalf("invite facade calls = %v, want %v", calls, want)
	}
}

func TestCacheAndPlaybackFacadesForwardToBridge(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	overview := apitypes.CacheOverview{UsedBytes: 128, EntryCount: 1}
	cachePage := apitypes.Page[apitypes.CacheEntryItem]{
		Items: []apitypes.CacheEntryItem{{BlobID: "b3:" + strings.Repeat("a", 64)}},
		Page:  apitypes.PageInfo{Returned: 1, Total: 1},
	}
	cleanup := apitypes.CacheCleanupResult{DeletedBytes: 64}
	status := apitypes.PlaybackPreparationStatus{RecordingID: "rec-1", PreferredProfile: "default"}
	resolve := apitypes.PlaybackResolveResult{RecordingID: "rec-1", PlayableURI: "file:///track.m4a"}
	availability := apitypes.RecordingPlaybackAvailability{RecordingID: "rec-1", PreferredProfile: "default"}
	recordingOverview := apitypes.RecordingAvailabilityOverview{RecordingID: "rec-1", PreferredProfile: "default"}
	albumOverview := apitypes.AlbumAvailabilityOverview{AlbumID: "album-1", PreferredProfile: "default"}
	blobRoot := t.TempDir()
	hashHex := strings.Repeat("b", 64)
	blobPath := filepath.Join(blobRoot, "b3", hashHex[:2], hashHex[2:4], hashHex)
	if err := os.MkdirAll(filepath.Dir(blobPath), 0o755); err != nil {
		t.Fatalf("mkdir blob path: %v", err)
	}
	if err := os.WriteFile(blobPath, []byte("thumb"), 0o644); err != nil {
		t.Fatalf("write blob: %v", err)
	}

	host := &coreHost{
		started:  true,
		blobRoot: blobRoot,
		bridge: &passthroughBridgeStub{
			UnavailableCore:    desktopcore.NewUnavailableCore(errors.New("unused")),
			getCacheOverviewFn: func(context.Context) (apitypes.CacheOverview, error) { return overview, nil },
			listCacheEntriesFn: func(_ context.Context, req apitypes.CacheEntryListRequest) (apitypes.Page[apitypes.CacheEntryItem], error) {
				return cachePage, nil
			},
			cleanupCacheFn: func(_ context.Context, req apitypes.CacheCleanupRequest) (apitypes.CacheCleanupResult, error) {
				return cleanup, nil
			},
			inspectPlaybackRecordingFn: func(_ context.Context, recordingID, preferredProfile string) (apitypes.PlaybackPreparationStatus, error) {
				return status, nil
			},
			preparePlaybackRecordingFn: func(_ context.Context, recordingID, preferredProfile string, purpose apitypes.PlaybackPreparationPurpose) (apitypes.PlaybackPreparationStatus, error) {
				return status, nil
			},
			getPlaybackPreparationFn: func(_ context.Context, recordingID, preferredProfile string) (apitypes.PlaybackPreparationStatus, error) {
				return status, nil
			},
			resolvePlaybackRecordingFn: func(_ context.Context, recordingID, preferredProfile string) (apitypes.PlaybackResolveResult, error) {
				return resolve, nil
			},
			listRecordingAvailabilityFn: func(_ context.Context, recordingID, preferredProfile string) ([]apitypes.RecordingAvailabilityItem, error) {
				return []apitypes.RecordingAvailabilityItem{{DeviceID: "dev-1"}}, nil
			},
			getRecordingAvailabilityFn: func(_ context.Context, recordingID, preferredProfile string) (apitypes.RecordingPlaybackAvailability, error) {
				return availability, nil
			},
			recordingAvailabilityOVFn: func(_ context.Context, recordingID, preferredProfile string) (apitypes.RecordingAvailabilityOverview, error) {
				return recordingOverview, nil
			},
			albumAvailabilityOVFn: func(_ context.Context, albumID, preferredProfile string) (apitypes.AlbumAvailabilityOverview, error) {
				return albumOverview, nil
			},
			resolveArtworkRefFn: func(_ context.Context, artwork apitypes.ArtworkRef) (apitypes.ArtworkResolveResult, error) {
				return apitypes.ArtworkResolveResult{
					Artwork:   apitypes.ArtworkRef{BlobID: artwork.BlobID, MIME: "image/webp", FileExt: ".webp"},
					LocalPath: blobPath,
					Available: true,
				}, nil
			},
			resolveRecordingArtworkFn: func(_ context.Context, recordingID, variant string) (apitypes.RecordingArtworkResult, error) {
				return apitypes.RecordingArtworkResult{
					RecordingID: recordingID,
					Artwork:     apitypes.ArtworkRef{BlobID: "b3:" + hashHex, MIME: "image/webp", FileExt: ".webp"},
				}, nil
			},
		},
	}

	cacheFacade := NewCacheFacade(host)
	if got, err := cacheFacade.GetCacheOverview(ctx); err != nil || got.UsedBytes != overview.UsedBytes {
		t.Fatalf("get cache overview = %+v, err=%v", got, err)
	}
	if got, err := cacheFacade.ListCacheEntries(ctx, apitypes.CacheEntryListRequest{}); err != nil || len(got.Items) != 1 {
		t.Fatalf("list cache entries = %+v, err=%v", got, err)
	}
	if got, err := cacheFacade.CleanupCache(ctx, apitypes.CacheCleanupRequest{}); err != nil || got.DeletedBytes != cleanup.DeletedBytes {
		t.Fatalf("cleanup cache = %+v, err=%v", got, err)
	}

	playbackFacade := NewPlaybackFacade(host)
	if _, err := playbackFacade.InspectPlaybackRecording(ctx, "rec-1", "default"); err != nil {
		t.Fatalf("inspect playback recording: %v", err)
	}
	if _, err := playbackFacade.PreparePlaybackRecording(ctx, "rec-1", "default", apitypes.PlaybackPreparationPlayNow); err != nil {
		t.Fatalf("prepare playback recording: %v", err)
	}
	if _, err := playbackFacade.GetPlaybackPreparation(ctx, "rec-1", "default"); err != nil {
		t.Fatalf("get playback preparation: %v", err)
	}
	if _, err := playbackFacade.ResolvePlaybackRecording(ctx, "rec-1", "default"); err != nil {
		t.Fatalf("resolve playback recording: %v", err)
	}
	if got, err := playbackFacade.ResolveBlobURL("b3:" + hashHex); err != nil || !strings.HasPrefix(got, "file:") {
		t.Fatalf("resolve blob url = %q, err=%v", got, err)
	}
	if got, err := playbackFacade.ResolveThumbnailURL(apitypes.ArtworkRef{BlobID: "b3:" + hashHex, MIME: "image/webp", FileExt: ".webp"}); err != nil || !strings.HasPrefix(got, "file:") {
		t.Fatalf("resolve thumbnail url = %q, err=%v", got, err)
	}
	if got, err := playbackFacade.ResolveRecordingArtworkURL(ctx, "rec-1", "320_webp"); err != nil || !strings.HasPrefix(got, "file:") {
		t.Fatalf("resolve recording artwork url = %q, err=%v", got, err)
	}
	if _, err := playbackFacade.ListRecordingAvailability(ctx, "rec-1", "default"); err != nil {
		t.Fatalf("list recording availability: %v", err)
	}
	if _, err := playbackFacade.GetRecordingAvailability(ctx, "rec-1", "default"); err != nil {
		t.Fatalf("get recording availability: %v", err)
	}
	if _, err := playbackFacade.GetRecordingAvailabilityOverview(ctx, "rec-1", "default"); err != nil {
		t.Fatalf("get recording availability overview: %v", err)
	}
	if _, err := playbackFacade.GetAlbumAvailabilityOverview(ctx, "album-1", "default"); err != nil {
		t.Fatalf("get album availability overview: %v", err)
	}
}
