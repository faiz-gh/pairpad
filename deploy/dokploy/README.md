# Deploying Pairpad on Dokploy

```
 Internet ──http://PUBLIC_IP──┐
                              ▼
 Tailnet ──http://vps/──▶ Traefik (Dokploy) ──▶ app      Go server + web UI   ─┐
          (MagicDNS)                             compactor merges history     ─┼─▶ Postgres
                                                                               │   (separate Dokploy
                         dokploy-network ──────────────────────────────────────┘    database service)
```

- **app**: one container running the Go server, which also serves the built web UI
  (`deploy/dokploy/app.Dockerfile`, build context = repo root).
- **compactor**: merges each room's update log into a snapshot.
- **Postgres**: a separate Dokploy database service, so app deploys never restart it.
  The app applies the schema migrations itself on startup.
- **Traefik**: Dokploy's reverse proxy. The routing rules are labels in
  `docker-compose.yml`, so they're versioned with the code rather than configured
  in the Domains tab.

## 1. Prepare the server

1. Install Dokploy, and install Tailscale with MagicDNS enabled.
2. Note the server's addresses:
   - `PUBLIC_IP`: the instance's public IPv4.
   - `TAILNET_NAME`: its MagicDNS short name (e.g. `pairpad-vps`).
   - `TAILNET_FQDN`: the full name (e.g. `pairpad-vps.tail1234.ts.net`).
     `tailscale status --self` or the Tailscale admin console shows both.
3. In the Oracle VCN security list, and in the instance's iptables (Oracle's
   Ubuntu images block it by default), open **only TCP 80** to the internet (443
   too once you have a domain). Everything else, such as the Dokploy dashboard
   on 3000, SSH, or a database port, stays reachable only over the tailnet.

## 2. Create the database

In Dokploy: **Create service → Database → PostgreSQL** (version 17).

- Choose a database name, user and a strong password, then **Deploy**.
- Copy the **Internal Connection URL** from the database's page. It uses the
  service's internal hostname on `dokploy-network`. That's the app's
  `DATABASE_URL`.
- Optional: set an **External Port** to use `psql` from your laptop. With the
  VCN closed for that port, only tailnet devices can reach it:
  `psql postgres://USER:PASS@pairpad-vps:PORT/DB`.
- **Backups:** enable Dokploy's scheduled database backups to S3-compatible
  storage (Oracle Object Storage works).

## 3. Create the app

**Create service → Compose**:

| Setting | Value |
| --- | --- |
| Provider | Git (this repository), branch `master` |
| Compose path | `deploy/dokploy/docker-compose.yml` |
| Compose type | **Docker Compose** (not Stack: room hubs live in memory, so exactly one app container) |
| Isolated deployment | **Off**. The services join `dokploy-network` themselves to reach Traefik and Postgres |
| Domains tab | Leave empty. Routing is in the compose labels |

**Environment** tab (Dokploy writes these to the `.env` file the compose file reads):

```dotenv
DATABASE_URL=<Internal Connection URL from step 2>
PUBLIC_IP=203.0.113.5
TAILNET_NAME=pairpad-vps
TAILNET_FQDN=pairpad-vps.tail1234.ts.net
```

Every other setting has a production default. Override any of them here if
needed; they're all listed in [`../.env.example`](../.env.example), e.g.
`MAX_PEERS`, `ROOM_TTL` or `ROOM_CREATE_PER_HOUR`.

**Deploy**. The first build takes a few minutes on an Ampere instance. Then check:

- `http://PUBLIC_IP/` from anywhere: it creates a pad and redirects to it.
- `http://pairpad-vps/` from any tailnet device.
- `http://PUBLIC_IP/healthz` returns `{"status":"ok"}`.
- Logs: the app prints `applied migration` (first run) and `listening`; the
  compactor prints `compactor started`.

## Updating

Push to `master` and redeploy (or enable Dokploy's auto-deploy webhook). On
redeploy the app flushes pending edits before stopping (`stop_grace_period`),
open tabs show "Offline — reconnecting" for a few seconds, then resync
without losing anything.

**Prefer prebuilt images?** CI publishes `ghcr.io/faiz-gh/pairpad-app` and
`ghcr.io/faiz-gh/pairpad-compactor` (amd64 + arm64) on every push to `master`.
Replace each service's `build:` block with
`image: ghcr.io/faiz-gh/pairpad-app:latest` (and `-compactor`). New GHCR
packages are private, so either make them public or add registry credentials
in Dokploy.

## HTTPS

Without a domain, Pairpad is served over plain HTTP. That's fine on the
tailnet, which WireGuard encrypts, but pad contents cross the public internet
unencrypted. Pairpad works over HTTP: "Copy link" falls back to a legacy copy
method, since the modern clipboard API needs HTTPS. To add HTTPS later:

1. Point a domain's `A` record at `PUBLIC_IP`. A free option is
   `203-0-113-5.sslip.io`, which always resolves to that IP. It works with
   Let's Encrypt, but shares rate limits with every other sslip.io user.
2. Open TCP 443, add `DOMAIN=…` to the environment, and uncomment the
   `pairpad-tls` labels in `docker-compose.yml`. They use Dokploy's
   `letsencrypt` resolver.

## Troubleshooting

| Symptom | Cause |
| --- | --- |
| Traefik returns `404 page not found` | The `Host` you used isn't in the router rule. Check `PUBLIC_IP` / `TAILNET_*`, or that the app container is on `dokploy-network` |
| App exits with `DATABASE_URL is required` / `connect to database` | The variable is missing, or the database service isn't deployed or isn't on `dokploy-network` |
| Tabs stuck on "Connecting…" | Something between Traefik and the browser is stripping WebSocket upgrades (Traefik passes them through by default) |
| "Pad full — waiting" | `MAX_PEERS` people are already in that pad |
