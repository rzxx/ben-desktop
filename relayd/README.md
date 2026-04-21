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
- Relay capacity controls:
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

## Environment Variables

`relayd` also accepts these environment variables as defaults, with CLI flags taking precedence:

- `PORT`
  - used for `-http-addr` as `:<PORT>` when `RELAYD_HTTP_ADDR` is unset
- `RELAYD_HTTP_ADDR`
- `RELAYD_DB_PATH`
- `RELAYD_IDENTITY_KEY_PATH`
- `RELAYD_PEER_LISTEN_ADDRS`
- `RELAYD_ADVERTISE_ADDRS`
- `RELAYD_TLS_CERT_PATH`
- `RELAYD_TLS_KEY_PATH`

This makes hosted deployments easier because the binary can run with no custom start command if the platform injects `PORT`.

## Railway Deployment

Recommended Railway setup:

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
```

Notes:

- Railway documents public HTTP/HTTPS and public TCP proxying. This README therefore recommends a TCP-only relay configuration on Railway.
- If you use a custom hostname for the TCP proxy, keep the Railway-assigned proxy port in the advertised multiaddr.
- The volume is required so the relay peer identity and SQLite registry survive redeploys.

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

- Immediate membership revocation:
  - `relayd` still does not ingest owner-pushed membership revocation state, so the currently highest valid certificate for a device remains accepted until expiry or supersession.
- Immediate invite revocation:
  - `relayd` still does not ingest an owner-pushed invite revocation feed; invite lookup is bounded by signed attestation expiry instead.
- Operational hardening beyond the process itself:
  - metrics, dashboards, alerting, and backup/restore for the SQLite DB are still deployment tasks, not built into the binary.
