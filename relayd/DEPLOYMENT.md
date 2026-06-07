# ben-relayd deployment guide

This guide is for moving `relayd` off Railway without keeping Railway-specific assumptions in the app.

## Supported hosted shape

`relayd` supports the HTTP/WebSocket ingress shape exposed by hosts such as Unkey Deploy and Render:

- Registry API: normal HTTPS requests on the platform-provided `PORT`.
- libp2p relay: WebSocket upgrade requests on the same HTTPS domain, forwarded internally to a local libp2p `/ws` listener.
- Storage: explicit `RELAYD_STORAGE_DIR`, not a Railway-named env var.

Any host you choose needs persistent writable storage for the relay identity key and SQLite registry. Without that, redeploys or restarts can change the relay peer ID and erase registry state.

Useful references:

- Unkey Deploy apps, Dockerfiles, runtime settings, and variables: https://www.unkey.com/docs/platform/apps/overview
- Unkey app settings for storage: https://www.unkey.com/docs/platform/apps/settings
- Render web services, Docker, `PORT`, and WebSocket support: https://render.com/docs/web-services
- Render free tier storage/spin-down limitations: https://render.com/free
- Render persistent disks: https://render.com/docs/disks

## Required runtime shape

Run exactly one `relayd` instance unless the registry database is moved out of process. The relay stores membership auth state, presence, root pins, revocations, and the relay's persistent libp2p identity in SQLite.

Required platform capabilities:

- Docker image or Dockerfile deploy.
- A public HTTPS domain that supports WebSocket upgrades.
- A writable persistent directory for SQLite and the identity key.
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

Unkey Deploy can run `relayd` and supports WebSocket ingress, but its configured `/data` storage is ephemeral. The Unkey docs describe `/data` as an ephemeral disk volume that is created when an instance starts and destroyed when it stops. That means Unkey is not a durable production home for this relay unless Unkey adds persistent volumes or the relay database/identity are moved to external persistent storage.

Use this setup only for a temporary/free relay or deployment smoke test:

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

4. Restart the desktop app or restart the network runtime so it reloads settings.
5. Redeploy once and check `/healthz` again. If `peerId` changes, the relay identity was lost. With current Unkey ephemeral storage, this is expected after instance replacement. Restarting the desktop network runtime lets it discover the new relay address from `registryUrl`.

What resets when Unkey replaces the instance:

- The relay peer ID can change.
- Presence and relay authorization state are rebuilt by online clients.
- Owners/admins should let revocation sync run again.
- Create fresh invites after a reset; old or in-progress invite joins can still carry stale peer/relay details.

## Render setup

If you want to deploy on Render, use a service configuration with persistent storage. A free Render web service is not a good home for this relay because it loses local SQLite/key files on spin-down/restart/redeploy.

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

1. Deploy the new relay with persistent storage. Do not count Unkey ephemeral `/data` as persistent storage.
2. Confirm `/healthz` returns `db.ok: true`.
3. Copy the new relay peer ID.
4. Update every desktop install's `core.registryUrl`. For hosted WebSocket relays that expose `/healthz.addrs`, `core.relayBootstrap` can be omitted and discovered at runtime.
5. Restart network runtimes.
6. Create new invites after the config swap. Invite lookup now prefers the configured registry URL over the URL embedded in an invite, but fresh invites avoid stale cached owner addresses.
7. Create fresh invites after the cutover. Old invites or in-progress join sessions may still contain stale relay addresses.
8. After redeploying the new relay, check that `/healthz.peerId` did not change. If it changed, storage is not persistent or `RELAYD_STORAGE_DIR` is wrong.
