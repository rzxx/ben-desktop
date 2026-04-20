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

export const PLAYBACK_TRANSPORT_EVENT_NAME = "playback:transport";
export const PLAYBACK_QUEUE_EVENT_NAME = "playback:queue";

export function resolveThumbnailURL(thumb: ArtworkRef) {
  return PlaybackFacade.ResolveThumbnailURL(thumb);
}

export function resolveAlbumArtworkURL(albumId: string, variant: string) {
  return PlaybackFacade.ResolveAlbumArtworkURL(albumId, variant);
}

export function resolveRecordingArtworkURL(
  recordingId: string,
  variant: string,
) {
  return PlaybackFacade.ResolveRecordingArtworkURL(recordingId, variant);
}

export function startPreparePlaybackRecording(
  recordingId: string,
  preferredProfile = "",
  purpose = Types.PlaybackPreparationPurpose.PlaybackPreparationPlayNow,
) {
  return PlaybackFacade.StartPreparePlaybackRecording(
    recordingId,
    preferredProfile,
    purpose,
  );
}

export function startEnsureRecordingEncoding(
  recordingId: string,
  preferredProfile = "",
) {
  return PlaybackFacade.StartEnsureRecordingEncoding(
    recordingId,
    preferredProfile,
  );
}

export function startEnsureAlbumEncodings(
  albumId: string,
  preferredProfile = "",
) {
  return PlaybackFacade.StartEnsureAlbumEncodings(albumId, preferredProfile);
}

export function startEnsurePlaylistEncodings(
  playlistId: string,
  preferredProfile = "",
) {
  return PlaybackFacade.StartEnsurePlaylistEncodings(
    playlistId,
    preferredProfile,
  );
}

export function listRecordingPlaybackAvailability(
  recordingIds: string[],
  preferredProfile = "",
): Promise<RecordingPlaybackAvailability[]> {
  return PlaybackFacade.ListRecordingPlaybackAvailability(
    new Types.RecordingPlaybackAvailabilityListRequest({
      PreferredProfile: preferredProfile,
      RecordingIDs: recordingIds,
    }),
  );
}

export function listAlbumAvailabilitySummaries(
  albumIds: string[],
  preferredProfile = "",
): Promise<AlbumAvailabilitySummaryItem[]> {
  return PlaybackFacade.ListAlbumAvailabilitySummaries(
    new Types.AlbumAvailabilitySummaryListRequest({
      AlbumIDs: albumIds,
      PreferredProfile: preferredProfile,
    }),
  );
}

export function getPlaybackSnapshot(): Promise<SessionSnapshot> {
  return PlaybackService.GetPlaybackSnapshot();
}

export function getPlaybackDebugDump(): Promise<string> {
  return PlaybackService.GetPlaybackDebugDump();
}

export function getPlaybackTraceEnabled(): Promise<boolean> {
  return PlaybackService.GetPlaybackTraceEnabled();
}

export function setPlaybackTraceEnabled(enabled: boolean): Promise<void> {
  return PlaybackService.SetPlaybackTraceEnabled(enabled);
}

export function clearPlaybackDebugTrace(): Promise<void> {
  return PlaybackService.ClearPlaybackDebugTrace();
}

export function togglePlayback() {
  return PlaybackService.TogglePlayback();
}

export function nextTrack() {
  return PlaybackService.Next();
}

export function previousTrack() {
  return PlaybackService.Previous();
}

export function seekTo(positionMs: number) {
  return PlaybackService.SeekTo(positionMs);
}

export function setVolume(volume: number) {
  return PlaybackService.SetVolume(volume);
}

export function setShuffle(enabled: boolean) {
  return PlaybackService.SetShuffle(enabled);
}

export function setRepeatMode(mode: string) {
  return PlaybackService.SetRepeatMode(mode);
}

export function playAlbum(albumId: string) {
  return PlaybackService.PlayAlbum(albumId);
}

export function playAlbumTrack(albumId: string, recordingId: string) {
  return PlaybackService.PlayAlbumTrack(albumId, recordingId);
}

export function queueAlbum(albumId: string) {
  return PlaybackService.QueueAlbum(albumId);
}

export function playPlaylist(playlistId: string) {
  return PlaybackService.PlayPlaylist(playlistId);
}

export function playPlaylistTrack(playlistId: string, itemId: string) {
  return PlaybackService.PlayPlaylistTrack(playlistId, itemId);
}

export function queuePlaylist(playlistId: string) {
  return PlaybackService.QueuePlaylist(playlistId);
}

export function queuePlaylistTrack(playlistId: string, itemId: string) {
  return PlaybackService.QueuePlaylistTrack(playlistId, itemId);
}

export function playRecording(recordingId: string) {
  return PlaybackService.PlayRecording(recordingId);
}

export function queueRecording(recordingId: string) {
  return PlaybackService.QueueRecording(recordingId);
}

export function playLiked() {
  return PlaybackService.PlayLiked();
}

export function playLikedTrack(recordingId: string) {
  return PlaybackService.PlayLikedTrack(recordingId);
}

export function queueLikedTrack(recordingId: string) {
  return PlaybackService.QueueLikedTrack(recordingId);
}

export function playTracks() {
  return PlaybackService.PlayTracks();
}

export function shuffleTracks() {
  return PlaybackService.ShuffleTracks();
}

export function playTracksFrom(recordingId: string) {
  return PlaybackService.PlayTracksFrom(recordingId);
}

export function selectQueueEntry(entryId: string) {
  return PlaybackService.SelectEntry(entryId);
}

export function removeQueuedEntry(entryId: string) {
  return PlaybackService.RemoveQueuedEntry(entryId);
}

export function clearQueue() {
  return PlaybackService.ClearQueue();
}

export type { PlaybackPreparationStatus };
