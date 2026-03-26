# Desktop Mobile-Interop + Playlist Cover Plan

## Summary
- Add a versioned mobile/member sync surface on desktop, instead of changing the existing desktop-desktop sync contract in place.
- Keep current desktop TCP+mDNS sync working, but add WebSocket + Noise + Yamux listeners and advertise/dialable WS multiaddrs for mobile.
- Move playlist custom covers off the album-artwork variant pipeline and onto a dedicated canonical single-image flow that mobile and desktop both read/write.

## Public Contracts
- Introduce a versioned mobile/member protocol namespace for the mobile-facing streams. Use separate protocol IDs and DTOs for at least `sync` and `checkpoint`, and expose the same mobile-facing transport path for `playback`, `artwork`, `membership-refresh`, and invite start/status/cancel.
- Define the mobile/member sync entities to match the mobile apply model exactly: `library`, `library_member`, `artist`, `album`, `recording`, `album_availability`, `recording_playback_availability`, `playlist`, `playlist_track`, and `liked_recording`.
- Do not reuse the existing raw signed desktop oplog payloads for mobile sync. The current signatures cover the raw payload shape, so mobile-compatible payloads must be served from a separate versioned member projection/log surface.
- Change playlist-cover contracts from multi-variant artwork records to one canonical image reference. The synced/mobile `playlist` payload should carry cover metadata directly: blob id, mime, file ext, width, height, bytes, and `hasCustomCover`.
- Update desktop API/types so playlist-cover read/write surfaces return a single canonical cover ref instead of `Variants[]`. Keep `ArtworkVariant` for album art only.

## Implementation Changes
- Transport/runtime:
  - Extend the desktop libp2p host to listen on WS multiaddrs in addition to current TCP listeners, with Noise + Yamux enabled on both.
  - Make `NetworkStatus.ListenAddrs` and invite peer-address hints prefer reachable WS addresses for mobile.
  - Update invite client transport to dial explicit WS multiaddrs directly; mDNS stays optional for desktop peers and is not required for mobile join/sync.
- Member/mobile sync pipeline:
  - Add a parallel member projection/oplog/checkpoint path on desktop. Keep existing raw desktop oplog/checkpoint untouched.
  - Emit member-projection ops whenever catalog, availability, playlist, likes, membership, cache, or cover state changes. Derived rows such as album/recording availability must produce member ops when their materialized state changes.
  - Publish/fetch/install member checkpoints from the member projection state, and hook background maintenance so an owner desktop can serve member checkpoints as backlog grows.
  - Reuse the existing auth model at the transport/request level, but treat the member sync/checkpoint payloads as their own versioned contract rather than raw desktop oplog rows.
- Playlist cover pipeline:
  - Add dedicated canonical playlist-cover storage on desktop, separate from `artwork_variant` fan-out generation.
  - On upload, accept any supported image source file type, enforce the configured file-size limit plus mime/ext/dimension validation, run it through `ffmpeg`, and persist one optimized canonical JPEG blob/cover record with `variant = "canonical"` semantics.
  - Stop writing new playlist covers through the three-variant artwork fan-out builder, but keep playlist-cover normalization/optimization on the `ffmpeg` path that emits a single JPEG output.
  - Make playlist list/detail loading, artwork/blob fetch, and member-sync projection resolve playlist covers from the canonical cover store.
  - Add a migration that collapses existing playlist artwork variants into one canonical cover, preferring the highest-quality existing blob, normalizing it to the canonical JPEG form when needed, and stops exposing playlist-scope `ArtworkVariant` as the source of truth.

## Test Plan
- Transport interop:
  - JS/mobile libp2p peer can connect to desktop over WS + Noise + Yamux and complete sync, checkpoint fetch, playback asset fetch, artwork fetch, membership refresh, and invite flows.
  - Desktop-desktop TCP sync remains green.
- Sync/checkpoint:
  - Join from invite using a WS peer address.
  - Initial catch-up from desktop to mobile-compatible member projection.
  - Backlog cutover to member checkpoint, checkpoint fetch, and checkpoint install.
  - Incremental sync for playlist create/rename/delete, playlist item add/move/delete, likes/unlikes, membership refresh, and availability changes.
- Playlist covers:
  - Set/change/clear playlist cover accepts supported image file types, enforces file-size limits, produces one canonical JPEG cover state, syncs to mobile contract payloads, and fetches correctly over artwork/blob transport.
  - Existing databases with playlist artwork variants migrate without losing covers.
- Regression:
  - Existing desktop operator/admin checkpoint flows still work.
  - Album artwork pipeline stays unchanged.
  - Invite codes continue to work for desktop peers and now include a mobile-dialable WS hint.

## Assumptions
- Existing desktop-desktop sync protocols stay in place; mobile compatibility is additive and versioned.
- WS transport is added alongside current TCP transport, not as a replacement.
- The mobile-compatible sync surface is projection-based and matches the current mobile entity/apply model rather than the desktop raw oplog schema.
- Playlist custom covers use one canonical JPEG blob, still generated/optimized through `ffmpeg`, with no three-variant fan-out.
