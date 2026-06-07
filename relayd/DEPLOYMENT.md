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
- Unkey changelog for WebSocket servers, `PORT`, read-only root filesystem, and `/data` storage: https://www.unkey.com/changelog
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

Create an Unkey Deploy project/app for this repository and configure:

- Dockerfile/build context: `build/docker/Dockerfile.relayd`
- Public HTTP port: platform `PORT`
- Health check path: `/healthz`
- Replicas/instances: minimum `1`, maximum `1`
- Storage/volume: mount writable storage at `/data`

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
2. Copy `peerId` from the JSON response.
3. Put this in desktop settings:

```json
{
  "core": {
    "registryUrl": "https://<unkey-domain>",
    "relayBootstrap": [
      "/dns4/<unkey-domain>/tcp/443/wss/p2p/<relay-peer-id>"
    ]
  }
}
```

4. Restart the desktop app or restart the network runtime so it reloads settings.
5. Create a fresh invite and verify that `/healthz` still reports the same `peerId` after a redeploy.

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

1. Deploy the new relay with persistent storage.
2. Confirm `/healthz` returns `db.ok: true`.
3. Copy the new relay peer ID.
4. Update every desktop install's `core.registryUrl` and `core.relayBootstrap`.
5. Restart network runtimes.
6. Create new invites after the config swap. Invite lookup now prefers the configured registry URL over the URL embedded in an invite, but fresh invites avoid stale cached owner addresses.
7. Create fresh invites after the cutover. Old invites or in-progress join sessions may still contain stale relay addresses.
8. After redeploying the new relay, check that `/healthz.peerId` did not change. If it changed, storage is not persistent or `RELAYD_STORAGE_DIR` is wrong.
