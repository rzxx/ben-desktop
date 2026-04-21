# Relay-First Libp2p Migration Plan

## Summary

- You do **not** need to implement Circuit Relay v2 from scratch. `go-libp2p` already provides the relay server side via `EnableRelayService(...)` / `relay.New(...)`, and the client side via `EnableRelay`, `EnableAutoRelayWithStaticRelays`, and `EnableHolePunching`.
- You **should** create a new dedicated public server component for this app’s network plane. The reason is architectural and operational, not because libp2p forces a separate package. The desktop runtime should not also be your public relay.
- Chosen direction for this app:
  - Discovery model: `relay + registry`
  - Transport scope: `QUIC + TCP`
  - First rollout relay scope: `control + metadata only`, not artwork/audio/blob payloads
- Primary upstream references to build against:
  - Circuit Relay v2 spec: https://github.com/libp2p/specs/blob/master/relay/circuit-v2.md
  - DCUtR spec: https://github.com/libp2p/specs/blob/master/relay/DCUtR.md
  - go-libp2p options: https://pkg.go.dev/github.com/libp2p/go-libp2p
- Grounded repo facts:
  - Current transport is `TCP-only`, mDNS-discovered, and manual-multiaddr dial based.
  - Current code enables relay/hole punching flags, but does not enable AutoRelay and still depends on LAN discovery.
  - Current invite flow embeds a direct `PeerAddrHint`.
  - Current stream opens use plain `host.NewStream`; that is valid for default/direct usage, but insufficient for protocols that must intentionally run over limited relayed connections because those opens must opt in with `network.WithAllowLimitedConn`.

## Important Public API / Interface Changes

- Add network config fields:
  - `RelayBootstrapAddrs []string`
  - `RegistryURL string`
  - `EnableLANDiscovery bool`
  - `RequireDirectForLargeTransfers bool`
- Add internal interface:
  - `PeerLocator` with `Announce`, `LookupMemberPeer`, `LookupInviteOwner`
- Replace invite payload `ben-invite-v1` with `ben-invite-v2`:
  - `tokenId`
  - `libraryId`
  - `ownerPeerId`
  - `registryUrl`
  - optional `relayBootstrapAddrs`
- Extend persisted join-session state with:
  - `RegistryURL`
  - `RelayBootstrapJSON`
  - `LastResolvedOwnerAddrsJSON`
- Persist last-known-good peer addresses locally for registry fallback, not only in-memory for the current runtime
- Extend network status/debug output with:
  - current relay reservation state
  - advertised relay addrs
  - last registry announce time
  - direct-vs-relayed connection type
  - direct-upgrade lifecycle state
- Add shared transport helpers:
  - `ConnectionState(peerID)` determines whether any connection exists, whether a direct connection exists, and whether only limited relayed connections exist
  - `EnsureDirectConnection(peerID)` checks for an existing direct connection, attempts `network.WithForceDirectDial` when none exists, and fails explicitly if only limited connections remain

## Architecture Decisions

- Add a new same-repo relay module: `relayd`
- `ben-relayd` responsibilities:
  - run a public libp2p host with relay service enabled
  - expose a tiny HTTP registry API
  - persist ephemeral peer advertisements with TTL
  - not host any desktop/library/playback runtime
- Initial deployment default:
  - one public `ben-relayd` instance
  - config accepts multiple relay bootstrap addrs later, but initial implementation targets one managed relay
- Client hosts:
  - remove `EnableRelayService()` from desktop and invite client hosts
  - keep `EnableRelay()`
  - add `EnableAutoRelayWithStaticRelays(...)`
  - keep `EnableHolePunching()`
  - add `EnableAutoNATv2()` if available in the current pinned libp2p version path
- Transport listeners:
  - stop forcing TCP-only transport
  - listen on wildcard TCP and QUIC
  - preferred listen set: `/ip4/0.0.0.0/tcp/0`, `/ip6/::/tcp/0`, `/ip4/0.0.0.0/udp/0/quic-v1`, `/ip6/::/udp/0/quic-v1`
- Discovery:
  - mDNS stays optional LAN optimization only
  - registry is authoritative for peer address resolution when available
  - registry is not the sole recovery path; clients keep last-known-good peer addresses locally, can still dial manual multiaddrs, and keep existing live connections even if registry access is lost
  - no DHT/rendezvous in first rollout
- Failure-domain note:
  - relay + registry co-location is acceptable for the initial rollout, but it creates a shared failure domain that must be called out explicitly in docs and ops planning
- Relay policy:
  - relay is strictly for invite flow, membership/control traffic, and small sync metadata
  - relay is not for checkpoints, artwork, playback assets, or other large blobs
  - plain `host.NewStream` is valid by default, but protocols that may run over relayed connections must explicitly opt in with `network.WithAllowLimitedConn`
  - large-transfer protocols require a direct connection
  - before opening a direct-required stream, attempt a direct-upgrade path with `EnsureDirectConnection(peerID)`
  - if only a limited relayed connection remains after that upgrade attempt, return explicit `direct_connection_required`

## Implementation Phases

### Phase 1: Introduce Dedicated Relay Service

- Create `relayd`
- Start a public host with `ForceReachabilityPublic()` in this service only
- Enable relay service with explicit resource limits instead of defaults
- Configure the relay host with an explicit libp2p resource manager profile sized for a control-plane relay, not a bulk-transfer relay
- Initial relay limits must stay conservative:
  - `ReservationTTL = 1h`
  - `MaxReservations = 128` initially; do not raise past `256` in Phase 1 without observed need
  - `MaxCircuits = 8` initially; do not raise past `16` in Phase 1 without observed need
  - `Limit.Duration = 90s` initially; keep Phase 1 in the `60-120s` range
  - `Limit.Data = 256 KiB` per direction initially; keep Phase 1 in the `256 KiB-1 MiB` range
  - `MaxReservationsPerPeer = 1`
  - `MaxReservationsPerIP = 8` initially
  - `MaxReservationsPerASN = 32` initially
- Larger relay limits, including sustained multi-MiB relayed transfers, are explicitly deferred until observed production need
- Relay is not intended for sustained data transfer in the initial rollout
- Add health endpoint and structured logs for:
  - reservations opened/refreshed/expired
  - rejected reservations due to total/per-peer/per-IP/per-ASN limits
  - per-IP or per-ASN saturation events
  - relayed connections opened/closed
  - data-limit or duration-limit terminations

### Phase 2: Add Registry Service

- Put a small HTTP API inside `ben-relayd`
- Registry endpoints:
  - `POST /v1/presence/announce`
  - `GET /v1/presence/member`
  - `GET /v1/invites/owner`
- Presence record shape:
  - `libraryId`
  - `deviceId`
  - `peerId`
  - `addrs`
  - `expiresAt`
  - `updatedAt`
- Registry auth:
  - pre-join owner lookup uses the invite token
  - post-join announce/lookup uses membership-backed app auth derived from the existing membership cert flow
  - relayd does not receive a separate owner-pushed invite-revocation or membership-revocation feed in this rollout
  - instead it closes the open-registry gap by enforcing signed invite validity/expiry, pinning each library to a single root public key, and rejecting stale membership certificate serials per device
- Presence TTL default: `90s`
- Client announce interval default: `30s`
- Registry persists last-good records in SQLite for initial rollout
- Registry behavior and fallback rules:
  - prefer fresh registry responses whenever the registry is healthy
  - persist last-known-good peer addresses locally on clients for stale/outage fallback
  - allow manual multiaddr dialing as a supported fallback path
  - allow existing live connections to continue without registry access

### Phase 3: Refactor Client Transport

- Replace the host builder in both `transport_libp2p.go` and `invite_transport.go` with one shared host-construction path
- That shared builder must:
  - enable QUIC + TCP
  - enable relay client support
  - enable AutoRelay with static relays
  - enable hole punching
  - never enable relay service on desktop/join hosts
- Add shared connection classification helpers and use them consistently for protocol gating, retry logic, and UI/debug output:
  - whether any connection exists
  - whether a direct connection exists
  - whether only limited relayed connections exist
  - whether the peer was reached directly after an earlier relayed bootstrap
- Change stream creation policy:
  - plain `host.NewStream` remains valid for default/direct-only behavior
  - invite/join/status/membership/small sync metadata opens use `network.WithAllowLimitedConn`
  - checkpoint/artwork/playback/blob opens require a direct connection
  - before opening a direct-required stream, call `EnsureDirectConnection(peerID)`
  - `EnsureDirectConnection(peerID)` first checks for an existing direct connection, then attempts `network.WithForceDirectDial`, and fails explicitly if only limited connections remain
- Expected upgrade lifecycle:
  - initial connectivity may be relayed
  - libp2p DCUtR should be allowed to upgrade automatically
  - direct-required paths may additionally trigger an explicit `network.WithForceDirectDial` attempt

### Phase 4: Redesign Invite Flow

- `CreateInviteCode` stops embedding a direct owner multiaddr as the primary bootstrap
- Invite v2 carries only minimal bootstrap data: `tokenId + libraryId + ownerPeerId + registryUrl + optional relayBootstrapAddrs`
- Invite v2 should not embed derived topology data or other non-essential configuration
- Dynamic bootstrap expansion happens through registry responses after token validation
- Join flow becomes:
  - joiner starts an ephemeral pre-join host
  - joiner obtains relay reservation through static relay bootstrap
  - joiner resolves owner through registry using invite token
  - joiner opens invite-start over a relayed connection if necessary
  - owner approval/finalize continue over the same control path
- Keep backward compatibility:
  - continue accepting `ben-invite-v1` during migration
  - issue only `ben-invite-v2` after rollout flag flips

### Phase 5: Steady-State Peer Discovery and Sync

- On active transport start, client announces current peer addrs to registry, including AutoRelay-rewritten relay addrs after a reservation is active
- On peer catchup scheduling, if peer is not already connected:
  - resolve by peer ID through registry
  - dial returned direct + relay addrs
  - let libp2p prefer direct and use relay/DCUtR when needed
  - if the peer is still reachable only through a limited relayed path and the next protocol requires direct transport, trigger `EnsureDirectConnection(peerID)` before opening the stream
- Cache last-known-good addrs locally and in memory; registry stays authoritative when available, but cached/manual fallback remains valid during outages or stale reads
- Keep mDNS peer discovery as best-effort only when `EnableLANDiscovery = true`

### Phase 6: Protocol Gating for Large Transfers

- Audit protocols:
  - allowed on relayed limited conn: invite start/status/cancel, membership refresh, small sync control, peer presence/control, and other small sync metadata
  - direct-required: checkpoint fetch, playback asset fetch, artwork blob fetch, any large blob/content stream
- Add explicit errors and UI/debug trace states for:
  - `direct_connection_required`
  - `relayed_connection_only`
  - `dcutr_upgrade_in_progress`
  - `direct_upgrade_failed`
  - `direct_upgrade_succeeded`
  - `registry_lookup_failed`

## Test Cases and Scenarios

- Unit tests:
  - invite v2 encode/decode
  - registry auth validation
  - relay-scope gating logic
  - host-builder options for desktop vs relay server
  - connection classification helper behavior for direct vs limited relayed peers
- Integration tests with in-process relay:
  - join over relay with no LAN and no direct public addr
  - autorelay address rewriting after reservation is acquired
  - direct QUIC upgrade after relayed contact
  - direct connection replaces relayed stream usage after upgrade when available
  - fallback control traffic over relay when direct upgrade fails
  - large-transfer protocols rejected on limited relay path
- Failure tests:
  - stale registry entry
  - relay restart
  - registry restart
  - relay + registry restart interaction while clients rely on cached last-known-good addresses
  - invalid invite token
  - owner offline
  - QUIC blocked, TCP fallback still works
- Regression tests against current behavior:
  - LAN-only mDNS still works when enabled
  - manual connect by multiaddr still works
  - existing library sync semantics do not change once a direct connection exists
- Manual acceptance:
  - two NATed peers on different networks can join via invite
  - peers reconnect after restart without manual multiaddr exchange
  - network debug surface clearly shows relay reservation, relayed dial, DCUtR attempt, and final direct/relayed state

## Assumptions and Defaults

- Same repo, new server command, not a separate repo
- One managed relay+registry instance for first production rollout
- `relay + registry` is the discovery model
- `QUIC + TCP` is the transport baseline
- Relay fallback is `control + metadata only`
- mDNS remains optional optimization, not dependency
- Existing `ben-invite-v1` is supported during migration; `ben-invite-v2` becomes the default issuer format
