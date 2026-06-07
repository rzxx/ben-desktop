# ben-relayd

Standalone public relay and registry service for the Ben desktop network plane.

## What This Service Does

- Runs a public libp2p host with Circuit Relay v2 enabled.
- Exposes the registry HTTP API used for:
  - member presence announce
  - member presence lookup
  - invite owner lookup
- Persists registry state in SQLite.
- Enforces:
  - signed invite attestation validity and expiry
  - single pinned root public key per library
  - membership certificate signature and authority-chain validation
  - stale membership certificate serial rejection per device
  - signed invite and membership revocation state pushed by owners/admins
  - membership-backed Circuit Relay reservation ACLs

## Deployment Modes

- Direct HTTPS:
  - provide `-tls-cert` and `-tls-key`
- Reverse proxy TLS termination:
  - leave `-tls-cert` and `-tls-key` unset
  - terminate TLS at the proxy and forward to `-http-addr`

For closed beta, either mode is fine. If you already have standard ingress or a reverse proxy, terminating TLS there is simpler operationally.

## Important Flags

- Identity and persistence:
  - `-identity-key`
  - `-db`
- Public libp2p listen addresses:
  - `-peer-listen-addrs`
  - `-advertise-addrs`
- HTTP serving:
  - `-http-addr`
  - `-tls-cert`
  - `-tls-key`
  - `-read-header-timeout`
  - `-read-timeout`
  - `-write-timeout`
  - `-idle-timeout`
  - `-shutdown-timeout`
- HTTP abuse controls:
  - `-max-body-bytes`
  - `-rate-limit-rps`
  - `-rate-limit-burst`
  - `-rate-limit-idle-ttl`
  - `-trusted-proxies`
  - `-client-ip-header`
- Relay capacity controls:
  - `-relay-acl-disabled` for local development only
  - `-relay-reservation-ttl`
  - `-relay-max-reservations`
  - `-relay-max-circuits`
  - `-relay-max-reservations-per-peer`
  - `-relay-max-reservations-per-ip`
  - `-relay-max-reservations-per-asn`
  - `-relay-limit-duration`
  - `-relay-limit-data-bytes`

## Example

```powershell
go run . `
  -http-addr :8787 `
  -identity-key C:\ben-relayd\identity.key `
  -db C:\ben-relayd\registry.db `
  -peer-listen-addrs /ip4/0.0.0.0/tcp/4001,/ip4/0.0.0.0/udp/4001/quic-v1 `
  -tls-cert C:\ben-relayd\tls\fullchain.pem `
  -tls-key C:\ben-relayd\tls\privkey.pem
```

## Hosted Deployments And Public Addressing

If the relay is behind a platform TCP proxy or load balancer, the addresses it listens on inside the container are not necessarily the addresses other peers should dial.

Use:

- `-peer-listen-addrs` for the local bind addresses inside the container or VM
- `-advertise-addrs` for the public libp2p multiaddrs clients should learn from identify and health output

Example:

```text
-peer-listen-addrs /ip4/0.0.0.0/tcp/4001
-advertise-addrs /dns4/relay-p2p.example.com/tcp/15140
```

The relay logs both the local `listenAddrs` and the public `advertiseAddrs` at startup.

## Metrics

`relayd` exposes Prometheus metrics at `/metrics`.

Useful production signals include:

- `relayd_http_requests_total`
- `relayd_http_request_duration_seconds`
- `relayd_registry_events_total`
- `relayd_rate_limit_rejected_total`
- `relayd_presence_records`
- `relayd_member_auth_state_records`
- `relayd_relay_acl_decisions_total`
- `relayd_sqlite_operation_failures_total`

`/healthz` checks SQLite availability and returns HTTP 503 when the registry database is not usable.

## Reverse Proxies And Rate Limiting

By default, per-client HTTP rate limiting uses the TCP remote address. If the registry HTTP API is behind a trusted reverse proxy, configure both:

```text
RELAYD_TRUSTED_PROXIES=<proxy-ip-or-cidr>
RELAYD_CLIENT_IP_HEADER=X-Forwarded-For
```

Forwarded client IP headers are ignored unless the TCP remote address matches `-trusted-proxies`. Do not set `-client-ip-header` without a trusted proxy list.

## Relay ACL

Circuit Relay reservations are membership-gated by default. Desktop clients authorize their relay peer ID with `/v1/relay/authorize` before reserving. The ACL allows reservations only for currently authenticated member peer IDs, and allows relay connections only to authorized destination peers.

`-relay-acl-disabled` exists only for local development and should not be used for public deployments.

## Environment Variables

`relayd` also accepts these environment variables as defaults, with CLI flags taking precedence:

- `PORT`
  - used for `-http-addr` as `:<PORT>` when `RELAYD_HTTP_ADDR` is unset
- `RELAYD_HTTP_ADDR`
- `RELAYD_DB_PATH`
- `RELAYD_IDENTITY_KEY_PATH`
- `RELAYD_STORAGE_DIR`
  - generic default directory for the DB and identity key when the explicit relayd storage variables are unset
- `RAILWAY_VOLUME_MOUNT_PATH`
  - legacy Railway fallback for the DB and identity key when the explicit relayd storage variables and `RELAYD_STORAGE_DIR` are unset
- `UNKEY_EPHEMERAL_DISK_PATH`
  - Unkey fallback for the DB and identity key when explicit storage variables, `RELAYD_STORAGE_DIR`, and the Railway fallback are unset
- `RELAYD_PEER_LISTEN_ADDRS`
- `RELAYD_ADVERTISE_ADDRS`
- `RELAYD_TLS_CERT_PATH`
- `RELAYD_TLS_KEY_PATH`
- `RELAYD_TRUSTED_PROXIES`
- `RELAYD_CLIENT_IP_HEADER`
- `RELAYD_WEBSOCKET_INGRESS`
  - set to `true` when HTTP WebSocket upgrade requests should be forwarded to a local libp2p `/ws` listener

This makes hosted deployments easier because the binary can run with no custom start command if the platform injects `PORT`.

## Hosted WebSocket Deployment

On hosts that expose one public HTTPS/WebSocket port, use the portable WebSocket ingress shape:

```text
RELAYD_STORAGE_DIR=/data
RELAYD_WEBSOCKET_INGRESS=true
RELAYD_PEER_LISTEN_ADDRS=/ip4/127.0.0.1/tcp/0/ws
RELAYD_ADVERTISE_ADDRS=/dns4/<relay-domain>/tcp/443/wss
```

Then configure the desktop app with:

```json
{
  "core": {
    "registryUrl": "https://<relay-domain>",
    "relayBootstrap": [
      "/dns4/<relay-domain>/tcp/443/wss/p2p/<relay-peer-id>"
    ]
  }
}
```

See [DEPLOYMENT.md](./DEPLOYMENT.md) for Unkey setup, Render setup, Railway compatibility, and the migration checklist.

## Railway Deployment

Railway setup:

1. Create one service for `relayd`.
2. Set `RAILWAY_DOCKERFILE_PATH=build/docker/Dockerfile.relayd`.
3. Attach one volume mounted at `/data`.
4. Generate a public Railway domain for HTTP.
5. Enable a TCP proxy for internal port `4001`.
6. Keep the service at one replica.
7. Set these environment variables:

```text
RELAYD_DB_PATH=/data/registry.db
RELAYD_IDENTITY_KEY_PATH=/data/identity.key
RELAYD_PEER_LISTEN_ADDRS=/ip4/0.0.0.0/tcp/4001
RELAYD_ADVERTISE_ADDRS=/dns4/<your-tcp-proxy-hostname>/tcp/<railway-proxy-port>
RAILWAY_RUN_UID=0
```

If `RELAYD_DB_PATH` and `RELAYD_IDENTITY_KEY_PATH` are not set, `relayd` automatically stores `ben-relayd.db` and `ben-relayd.identity.key` under Railway's injected `RAILWAY_VOLUME_MOUNT_PATH`.

Notes:

- Railway documents public HTTP/HTTPS and public TCP proxying. This README therefore recommends a TCP-only relay configuration on Railway.
- If you use a custom hostname for the TCP proxy, keep the Railway-assigned proxy port in the advertised multiaddr.
- The volume is required so the relay peer identity and SQLite registry survive redeploys.
- Railway mounts volumes as `root`. Set `RAILWAY_RUN_UID=0` so relayd can claim the storage directory at startup; on Linux it immediately drops to UID/GID 65532 before opening SQLite, loading the relay identity, or serving traffic.

## App Configuration

The desktop app still needs both the registry URL and the relay bootstrap address in its local settings:

```json
{
  "core": {
    "registryUrl": "https://<your-relay-http-domain>",
    "relayBootstrap": [
      "/dns4/<your-tcp-proxy-hostname>/tcp/<railway-proxy-port>/p2p/<relay-peer-id>"
    ]
  }
}
```

The relay peer ID is printed in `relayd` startup logs and returned from `/healthz`.

## Closed-Beta Status

This service is now suitable for a normal closed beta rollout if you provide:

- a stable DNS name
- a stable persisted `-identity-key`
- fixed public libp2p ports through `-peer-listen-addrs`
- TLS, either directly or via reverse proxy
- normal service supervision and log collection

## Remaining Open Items

- Operational hardening beyond the process itself:
  - dashboards, alerting, and backup/restore for the SQLite DB are deployment tasks.
- Multi-replica operation:
  - keep one writer replica unless the registry database is moved to external storage with explicit coordination.
