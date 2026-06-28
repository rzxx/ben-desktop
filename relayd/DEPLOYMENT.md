# ben-relayd deployment guide

This guide is for moving `relayd` off Railway without keeping Railway-specific assumptions in the app.

## Supported hosted shape

`relayd` supports the HTTP/WebSocket ingress shape exposed by hosts such as Unkey Deploy and Render:

- Registry API: normal HTTPS requests on the platform-provided `PORT`.
- libp2p relay: WebSocket upgrade requests on the same HTTPS domain, forwarded internally to a local libp2p `/ws` listener.
- Storage: explicit `RELAYD_STORAGE_DIR`, not a Railway-named env var.

The relay needs writable storage while it is running, but durable storage is optional. Without durable storage, redeploys can change the relay peer ID and clear registry state; online desktop clients discover the replacement identity, reserve it, republish presence, and resync membership revocations.

Useful references:

- Unkey Deploy apps, Dockerfiles, runtime settings, and variables: https://www.unkey.com/docs/platform/apps/overview
- Unkey app settings for storage: https://www.unkey.com/docs/platform/apps/settings
- Render web services, Docker, `PORT`, and WebSocket support: https://render.com/docs/web-services
- Render free tier storage/spin-down limitations: https://render.com/free
- Render persistent disks: https://render.com/docs/disks

## Required runtime shape

Run exactly one `relayd` instance unless registry state is moved out of process. The relay uses SQLite for runtime membership authorization, presence, root pins, and membership revocations. This state is reconstructible from signed client state after a replacement.

Required platform capabilities:

- Docker image or Dockerfile deploy.
- A public HTTPS domain that supports WebSocket upgrades.
- A writable directory for SQLite and the identity key. A persistent volume avoids identity churn but is not required for recovery.
- Single running replica.
- A health check that calls `/healthz`.

The old Railway TCP-proxy shape still works. The portable one-port hosted shape is WebSocket:

```text
Registry URL:
https://<relay-domain>

Relay bootstrap multiaddr:
/dns4/<relay-domain>/tcp/443/wss/p2p/<relay-peer-id>
```

The relay peer ID is printed in startup logs and returned from:

```sh
curl https://<relay-domain>/healthz
```

## Unkey Deploy setup

Unkey Deploy can run `relayd` and supports WebSocket ingress. Its configured `/data` storage is ephemeral, so instance replacement changes the relay peer identity and briefly clears registry state. Current desktop clients recover through the stable registry URL.

Use this setup when brief relay unavailability during instance replacement is acceptable:

- Dockerfile/build context: `build/docker/Dockerfile.relayd`
- Public HTTP port: platform `PORT`
- Health check path: `/healthz`
- Replicas/instances: minimum `1`, maximum `1`
- Storage: smallest ephemeral `/data` disk available

Set environment variables:

```text
RELAYD_STORAGE_DIR=/data
RELAYD_WEBSOCKET_INGRESS=true
RELAYD_PEER_LISTEN_ADDRS=/ip4/127.0.0.1/tcp/0/ws
RELAYD_ADVERTISE_ADDRS=/dns4/<unkey-domain>/tcp/443/wss
```

The relay image starts as root only long enough to claim the mounted storage directory, then drops to UID/GID `65532` before opening SQLite, loading the relay identity, or serving traffic.

Do not set `RELAYD_TLS_CERT_PATH` or `RELAYD_TLS_KEY_PATH` on Unkey. TLS terminates at the platform edge, and the container receives plain HTTP/WebSocket traffic on `PORT`.

After deployment:

1. Open `https://<unkey-domain>/healthz`.
2. Confirm the JSON response includes `db.ok: true`, `peerId`, and at least one `/wss` address in `addrs`.
3. Put this in desktop settings:

```json
{
  "core": {
    "registryUrl": "https://<unkey-domain>"
  }
}
```

The desktop app discovers the current relay bootstrap address from `GET /healthz`, so `relayBootstrap` can be omitted for this hosted WebSocket shape. Static `relayBootstrap` entries still work as fallback, but a stale static peer ID should not be your primary Unkey path.

4. Redeploy once and check `/healthz` again. If `peerId` changes, the relay identity was replaced. Keep an owner/admin desktop online and verify that it republishes a circuit address without restarting.

What resets when Unkey replaces the instance:

- The relay peer ID can change.
- Presence and relay authorization state are rebuilt by online clients.
- Owners/admins automatically republish membership revocations.
- Existing and in-progress v4 invites re-resolve the owner through the registry URL.

## Render setup

On Render, a persistent disk keeps the relay identity stable and reduces recovery churn, but an ephemeral disk remains functionally recoverable through the registry URL.

Create a Docker web service:

- Dockerfile path: `build/docker/Dockerfile.relayd`
- Health check path: `/healthz`
- Instance count: `1`
- Persistent disk mount path: `/data`

Set environment variables:

```text
RELAYD_STORAGE_DIR=/data
RELAYD_WEBSOCKET_INGRESS=true
RELAYD_PEER_LISTEN_ADDRS=/ip4/127.0.0.1/tcp/0/ws
RELAYD_ADVERTISE_ADDRS=/dns4/<render-service>.onrender.com/tcp/443/wss
```

Desktop settings:

```json
{
  "core": {
    "registryUrl": "https://<render-service>.onrender.com",
    "relayBootstrap": [
      "/dns4/<render-service>.onrender.com/tcp/443/wss/p2p/<relay-peer-id>"
    ]
  }
}
```

## Railway compatibility

Existing Railway deployments can keep using the native TCP proxy setup:

```text
RELAYD_STORAGE_DIR=/data
RELAYD_PEER_LISTEN_ADDRS=/ip4/0.0.0.0/tcp/4001
RELAYD_ADVERTISE_ADDRS=/dns4/<railway-tcp-proxy-host>/tcp/<railway-tcp-proxy-port>
```

`RAILWAY_VOLUME_MOUNT_PATH` is still supported as a legacy fallback, but new deployments should use `RELAYD_STORAGE_DIR`.

## Migration checklist

1. Deploy the new relay. Add persistent storage if a stable relay peer identity is operationally useful.
2. Confirm `/healthz` returns `db.ok: true`.
3. Copy the new relay peer ID.
4. Update every desktop install's `core.registryUrl`. For hosted WebSocket relays that expose `/healthz.addrs`, `core.relayBootstrap` can be omitted and discovered at runtime.
5. Existing network runtimes reconcile the new registry and relay configuration automatically.
6. Verify that an existing invite still reaches an online owner after the cutover.
7. If persistent storage is configured, confirm `/healthz.peerId` remains stable across redeploys.
