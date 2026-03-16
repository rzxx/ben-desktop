import * as PlaybackFacade from "../../../bindings/ben/desktop/playbackfacade";
import {
  Types,
  type ArtworkRef,
  type PlaybackPreparationStatus,
} from "./models";

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

export type { PlaybackPreparationStatus };
