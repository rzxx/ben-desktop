import * as PlaybackFacade from "../../../bindings/ben/desktop/playbackfacade";
import * as PlaybackService from "../../../bindings/ben/desktop/playbackservice";
import {
  Types,
  type AlbumAvailabilitySummaryItem,
  type ArtworkRef,
  type PlaybackPreparationStatus,
  type RecordingPlaybackAvailability,
  type SessionSnapshot,
} from "./models";
import { traceWailsCall } from "@/lib/observability/trace";

export const PLAYBACK_TRANSPORT_EVENT_NAME = "playback:transport";
export const PLAYBACK_QUEUE_EVENT_NAME = "playback:queue";

export function resolveThumbnailURL(thumb: ArtworkRef) {
  return traceWailsCall("playback", "resolve_thumbnail_url", {}, () =>
    PlaybackFacade.ResolveThumbnailURL(thumb),
  );
}

export function resolveAlbumArtworkURL(albumId: string, variant: string) {
  return traceWailsCall(
    "playback",
    "resolve_album_artwork_url",
    { albumId, variant },
    () => PlaybackFacade.ResolveAlbumArtworkURL(albumId, variant),
  );
}

export function resolveRecordingArtworkURL(
  recordingId: string,
  variant: string,
) {
  return traceWailsCall(
    "playback",
    "resolve_recording_artwork_url",
    { recordingId, variant },
    () => PlaybackFacade.ResolveRecordingArtworkURL(recordingId, variant),
  );
}

export function startPreparePlaybackRecording(
  recordingId: string,
  preferredProfile = "",
  purpose = Types.PlaybackPreparationPurpose.PlaybackPreparationPlayNow,
) {
  return traceWailsCall(
    "playback",
    "start_prepare_playback_recording",
    { recordingId, preferredProfile, purpose },
    () =>
      PlaybackFacade.StartPreparePlaybackRecording(
        recordingId,
        preferredProfile,
        purpose,
      ),
  );
}

export function startEnsureRecordingEncoding(
  recordingId: string,
  preferredProfile = "",
) {
  return traceWailsCall(
    "playback",
    "start_ensure_recording_encoding",
    { recordingId, preferredProfile },
    () =>
      PlaybackFacade.StartEnsureRecordingEncoding(
        recordingId,
        preferredProfile,
      ),
  );
}

export function startEnsureAlbumEncodings(
  albumId: string,
  preferredProfile = "",
) {
  return traceWailsCall(
    "playback",
    "start_ensure_album_encodings",
    { albumId, preferredProfile },
    () => PlaybackFacade.StartEnsureAlbumEncodings(albumId, preferredProfile),
  );
}

export function startEnsurePlaylistEncodings(
  playlistId: string,
  preferredProfile = "",
) {
  return traceWailsCall(
    "playback",
    "start_ensure_playlist_encodings",
    { playlistId, preferredProfile },
    () =>
      PlaybackFacade.StartEnsurePlaylistEncodings(playlistId, preferredProfile),
  );
}

export function listRecordingPlaybackAvailability(
  recordingIds: string[],
  preferredProfile = "",
): Promise<RecordingPlaybackAvailability[]> {
  return traceWailsCall(
    "playback",
    "list_recording_playback_availability",
    { count: recordingIds.length, preferredProfile },
    () =>
      PlaybackFacade.ListRecordingPlaybackAvailability(
        new Types.RecordingPlaybackAvailabilityListRequest({
          PreferredProfile: preferredProfile,
          RecordingIDs: recordingIds,
        }),
      ),
  );
}

export function listAlbumAvailabilitySummaries(
  albumIds: string[],
  preferredProfile = "",
): Promise<AlbumAvailabilitySummaryItem[]> {
  return traceWailsCall(
    "playback",
    "list_album_availability_summaries",
    { count: albumIds.length, preferredProfile },
    () =>
      PlaybackFacade.ListAlbumAvailabilitySummaries(
        new Types.AlbumAvailabilitySummaryListRequest({
          AlbumIDs: albumIds,
          PreferredProfile: preferredProfile,
        }),
      ),
  );
}

export function getPlaybackSnapshot(): Promise<SessionSnapshot> {
  return traceWailsCall("playback", "get_playback_snapshot", undefined, () =>
    PlaybackService.GetPlaybackSnapshot(),
  );
}

export function togglePlayback() {
  return traceWailsCall("playback", "toggle_playback", undefined, () =>
    PlaybackService.TogglePlayback(),
  );
}

export function nextTrack() {
  return traceWailsCall("playback", "next", undefined, () =>
    PlaybackService.Next(),
  );
}

export function previousTrack() {
  return traceWailsCall("playback", "previous", undefined, () =>
    PlaybackService.Previous(),
  );
}

export function seekTo(positionMs: number) {
  return traceWailsCall("playback", "seek_to", { positionMs }, () =>
    PlaybackService.SeekTo(positionMs),
  );
}

export function setVolume(volume: number) {
  return traceWailsCall("playback", "set_volume", { volume }, () =>
    PlaybackService.SetVolume(volume),
  );
}

export function setShuffle(enabled: boolean) {
  return traceWailsCall("playback", "set_shuffle", { enabled }, () =>
    PlaybackService.SetShuffle(enabled),
  );
}

export function setRepeatMode(mode: string) {
  return traceWailsCall("playback", "set_repeat_mode", { mode }, () =>
    PlaybackService.SetRepeatMode(mode),
  );
}

export function playAlbum(albumId: string) {
  return traceWailsCall("playback", "play_album", { albumId }, () =>
    PlaybackService.PlayAlbum(albumId),
  );
}

export function playAlbumTrack(albumId: string, recordingId: string) {
  return traceWailsCall(
    "playback",
    "play_album_track",
    { albumId, recordingId },
    () => PlaybackService.PlayAlbumTrack(albumId, recordingId),
  );
}

export function playPlaylist(playlistId: string) {
  return traceWailsCall("playback", "play_playlist", { playlistId }, () =>
    PlaybackService.PlayPlaylist(playlistId),
  );
}

export function playPlaylistTrack(playlistId: string, itemId: string) {
  return traceWailsCall(
    "playback",
    "play_playlist_track",
    { playlistId, itemId },
    () => PlaybackService.PlayPlaylistTrack(playlistId, itemId),
  );
}

export function queuePlaylistTrack(playlistId: string, itemId: string) {
  return traceWailsCall(
    "playback",
    "queue_playlist_track",
    { playlistId, itemId },
    () => PlaybackService.QueuePlaylistTrack(playlistId, itemId),
  );
}

export function playRecording(recordingId: string) {
  return traceWailsCall("playback", "play_recording", { recordingId }, () =>
    PlaybackService.PlayRecording(recordingId),
  );
}

export function queueRecording(recordingId: string) {
  return traceWailsCall("playback", "queue_recording", { recordingId }, () =>
    PlaybackService.QueueRecording(recordingId),
  );
}

export function playLiked() {
  return traceWailsCall("playback", "play_liked", undefined, () =>
    PlaybackService.PlayLiked(),
  );
}

export function playLikedTrack(recordingId: string) {
  return traceWailsCall("playback", "play_liked_track", { recordingId }, () =>
    PlaybackService.PlayLikedTrack(recordingId),
  );
}

export function playOffline() {
  return traceWailsCall("playback", "play_offline", undefined, () =>
    PlaybackService.PlayOffline(),
  );
}

export function playOfflineTrack(recordingId: string) {
  return traceWailsCall("playback", "play_offline_track", { recordingId }, () =>
    PlaybackService.PlayOfflineTrack(recordingId),
  );
}

export function queueLikedTrack(recordingId: string) {
  return traceWailsCall("playback", "queue_liked_track", { recordingId }, () =>
    PlaybackService.QueueLikedTrack(recordingId),
  );
}

export function queueOfflineTrack(recordingId: string) {
  return traceWailsCall(
    "playback",
    "queue_offline_track",
    { recordingId },
    () => PlaybackService.QueueOfflineTrack(recordingId),
  );
}

export function playTracks() {
  return traceWailsCall("playback", "play_tracks", undefined, () =>
    PlaybackService.PlayTracks(),
  );
}

export function shuffleTracks() {
  return traceWailsCall("playback", "shuffle_tracks", undefined, () =>
    PlaybackService.ShuffleTracks(),
  );
}

export function playTracksFrom(recordingId: string) {
  return traceWailsCall("playback", "play_tracks_from", { recordingId }, () =>
    PlaybackService.PlayTracksFrom(recordingId),
  );
}

export function selectQueueEntry(entryId: string) {
  return traceWailsCall("playback", "select_entry", { entryId }, () =>
    PlaybackService.SelectEntry(entryId),
  );
}

export function removeQueuedEntry(entryId: string) {
  return traceWailsCall("playback", "remove_queued_entry", { entryId }, () =>
    PlaybackService.RemoveQueuedEntry(entryId),
  );
}

export function clearQueue() {
  return traceWailsCall("playback", "clear_queue", undefined, () =>
    PlaybackService.ClearQueue(),
  );
}

export type { PlaybackPreparationStatus };
