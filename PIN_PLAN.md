# Rebuild Pinning As A Dedicated Pin Service

## Summary
- Replace the current playback-owned offline pin flow with a first-class `PinService` that owns durable pin intent, background reconciliation, cache protection, peer-triggered fetches, and UI state.
- Treat pinning as two separate layers: `direct user intent` and `resolved materialization`. Direct intent is what the user pinned; resolved materialization is the current set of logical songs, exact recordings, artwork, and cached blobs protected by that intent.
- Use logical-first pin resolution for playlists and liked songs, keep exact-recording pinning as a separate explicit scope, and make overlap stable: shared assets are reused, while exact pins keep exact-only requirements when they are truly distinct.
- Pin intent is explicitly per-device, not global. The same library item can be pinned on one device and unpinned on another, and reconciliation/materialization is evaluated independently per `(library, device, scope_kind, scope_id, profile)`.
- Artwork handling is explicit: baseline artwork replication between devices stays in the existing sync system, while `PinService` additionally makes sure pinned items have their artwork materialized locally and protected from cache cleanup on that device.

## Key Changes
- Replace `offline_pins` and the pin-refresh logic inside `PlaybackService` with a new pin schema and service:
  - `pin_roots`: durable pin intents, one row per `(library, device, scope_kind, scope_id, profile)`.
  - `pin_members`: reconciled desired media membership for each root. Store both `library_recording_id` and resolved `variant_recording_id`, plus a `resolution_policy` of `logical_preferred` or `exact_variant`.
  - `pin_blob_refs`: durable cache/artwork protection refs derived from `pin_members`, used directly by cache cleanup and cache overview.
- Device scoping is part of the data model, not an implementation detail:
  - Pin and unpin mutate only the active device's `pin_roots`.
  - Syncing a library to another device does not copy pin intent to that device.
  - UI/API wording should call this out directly anywhere pin semantics are described.
- Supported pin root kinds are decision-complete and fixed:
  - `recording_cluster`: collapsed deduped song pin.
  - `recording_variant`: exact recording pin.
  - `album_variant`: exact album variant pin.
  - `playlist`: custom playlist pin.
  - `liked_playlist`: liked songs pin.
- Resolution policy is fixed:
  - `playlist` and `liked_playlist` expand from stored playlist `library_recording_id` values only, never from legacy exact ids.
  - `recording_cluster` resolves to the current preferred/local-best playback variant for fetch/materialization.
  - `recording_variant` resolves only to that exact variant.
  - `album_variant` resolves only tracks belonging to that exact album variant.
  - If a logical root and an exact root overlap, protection is the union of both resolved targets; no duplicate fetch occurs unless the exact root truly requires a different variant blob.
- Make local items pinnable as real intent:
  - Local-only tracks/albums/playlists still create `pin_roots`.
  - Reconciliation records members and protection refs even when no remote fetch is needed.
  - Future library changes, variant preference changes, or remote-only additions under the same root automatically reconcile without losing the pinned state.
- Cover artwork explicitly inside reconciliation:
  - Every resolved pinned member also resolves the artwork refs that belong to that pinned subject on the local device.
  - If a pinned item's artwork blob is missing locally, reconciliation backfills it and records it in `pin_blob_refs` the same way as audio blobs.
  - This does not replace normal cross-device artwork sync for library entities; it replaces the old pin-specific artwork protection path and makes `pin_blob_refs` the authoritative source for pinned artwork retention.
- Move all background work into `PinService`:
  - Debounced reconcile queue keyed by pin root.
  - Reconcile triggers on playlist mutations, liked mutations, album-track changes, recording merge/split/preference changes, sync/apply completion, active peer changes, network runtime start/stop, new asset cache entries, and pin/unpin mutations.
  - Pending members stay pending with retry metadata instead of pretending the pin completed fully.
- Reuse existing playback fetch/transcode machinery through a narrow adapter instead of duplicating it:
  - Prefer connected peers with cached optimized assets first.
  - Fall back to provider-capable peers for source/transcode fetch.
  - Persist fetched assets into normal optimized/cache tables, then rebuild `pin_blob_refs`.
  - Trigger pending reconcile when peer availability improves or sync discovers a peer that can satisfy a pending member.
- Split pin state out of playback availability for the UI:
  - Add `api/types/pin.go` with `PinSubjectRef`, `PinState`, `PinSourceRef`, `PinChangeEvent`, and a `PinSurface`.
  - `PinState` fields are fixed: `Pinned`, `Direct`, `Covered`, `Pending`, `Sources[]`.
  - Frontend stops using `RecordingPlaybackAvailability.Pinned` and `AggregateAvailabilitySummary.ScopePinned` as the source of truth.
  - UI loads pin state directly for albums, playlists, tracks, liked tracks, and exact recording variants, and shows direct-vs-covered state consistently in lists, details, and context menus.
- Remove optimistic pin hacks from the frontend:
  - Liked page no longer mutates `ScopePinned` locally.
  - Pin buttons and badges refresh from `PinChangeEvent` + `PinSurface` queries.
  - Playlist rows and album tiles display direct/covered pin state, not only availability labels.
- Make cache cleanup depend on pin refs, not on re-resolving pins through playback:
  - `CacheService` reads `pin_blob_refs` to determine protected blobs.
  - Cache overview groups protection by root scope from `pin_blob_refs`.
  - Reconciliation removes stale blob refs when tracks leave a pinned playlist/album unless another active root still covers them.

## Public API / Interface Changes
- Add a new `PinFacade` / `PinSurface` and move pin mutations there:
  - `StartPin(PinIntentRequest)`, `Unpin(PinIntentRequest)`, `ListPinStates(PinStateListRequest)`, `GetPinState(PinStateRequest)`.
  - Keep `StartPinLiked` / `UnpinLiked` as thin convenience wrappers over `liked_playlist`.
- Remove pin ownership from playback-facing models as authoritative API:
  - `RecordingPlaybackAvailability.Pinned` and aggregate playback pin fields stop driving UI logic.
  - Catalog/detail loaders join playback availability with pin state explicitly.
- Emit `PinChangeEvent` separately from catalog invalidation so UI refreshes pin badges without overloading availability refresh.

## Removals
- Remove the old playback-owned pin persistence and refresh pipeline:
  - Delete `offline_pins`.
  - Delete the pinned-scope refresh job/reconcile logic from `PlaybackService`.
  - Remove the old `StartPin*Offline` / `Unpin*Offline` ownership from playback-facing facades once callers are moved to `PinFacade`.
- Remove old authoritative pin reads from playback availability models:
  - `RecordingPlaybackAvailability.Pinned` and `AggregateAvailabilitySummary.ScopePinned` stop being the source of truth for pin UI/state decisions.
  - Any frontend optimistic pin toggles or loaders that only read those playback fields should be deleted after `PinSurface` adoption.
- Remove old cache protection code that infers pinned blobs by re-resolving playback scopes instead of reading durable pin refs.
- Remove dead code and tests that only exist for the old playback-owned pin model after equivalent `PinService` coverage is in place.

## Test Plan
- Unit and integration coverage in new `internal/desktopcore/service_pin_test.go` plus touched frontend tests:
  - Pin/unpin each root kind: `recording_cluster`, `recording_variant`, `album_variant`, `playlist`, `liked_playlist`.
  - Pinning the same item on device A must not mark it pinned on device B unless device B pins it separately.
  - Local-only items remain pinnable and survive reconciliation with no remote fetch.
  - Pinned items keep their associated artwork protected locally, and missing artwork is backfilled without changing the separate library artwork sync contract.
  - Playlist and liked pins expand from logical song ids only and do not re-download exact blobs already satisfied by a pinned/preferred local album variant.
  - Exact recording pin keeps exact-only protection even when the logical song is also pinned elsewhere.
  - Album variant pin protects only that variant, not sibling variants in the same album cluster.
  - Adding/removing playlist items and liking/unliking tracks updates `pin_members` and `pin_blob_refs` automatically.
  - Preferred recording variant change retargets logical pins while leaving exact pins untouched.
  - Pending members fetch automatically when a suitable peer appears or a provider becomes available.
  - Cache cleanup preserves only blobs with active `pin_blob_refs`.
  - Frontend list/detail pages show direct/covered/pending state correctly and no longer rely on optimistic liked-playlist toggles.
  - Regression test that synced playlist items carrying legacy exact ids are normalized before pin expansion.

## Assumptions
- No backward compatibility is required for the old pin DB model. Old pin tables/code can be removed instead of migrated.
- Default pin profile remains `desktop`.
- UI should show `direct + covered` pin state, not only a boolean.
- Playlist and liked pinning are `logical-first`.
- Local items are always allowed to be pinned as durable intent.
- General artwork sync remains a separate system; this plan only changes how pinned artwork is materialized/protected for a given device.
