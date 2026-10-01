# Pairpad

> A faster alternative to Codeshare. Open a link, start typing, code together in real time.

No signup. No modal. Just a pad.

## Why Pairpad?

Pairpad is built around a few measurable performance goals:

| Goal | Target |
|------|--------|
| Time to first keystroke | < 1 second |
| Keystroke latency between peers | < 100 ms |
| Initial bundle size (gzipped) | < 200 KB |
| Joining a pad | Paste link → you're in |

## Features

### MVP (v0.1)

- [x] Anonymous rooms with short shareable IDs (e.g. `/k7x2p9`)
- [x] Real-time collaborative editing with conflict-free merging
- [x] Live multi-cursor and presence (who's in the room)
- [x] Syntax highlighting with language picker
- [x] Persistent rooms that expire after inactivity
- [x] Dark / light theme

### Out of scope for MVP

Accounts, code execution, chat, video/audio, and multi-file projects.

## Tech Stack

| Layer | Technology |
|-------|------------|
| Frontend | Svelte 5 |
| UI Library | shadcn-svelte |
| Editor | CodeMirror 6 |
| Sync | Yjs (CRDT) + y-codemirror.next |
| Backend | Go |
| Realtime | WebSockets |
| Database | PostgreSQL |
| Reverse Proxy | Traefik via Dokploy (production), Caddy (local all-in-one stack) |
| Orchestration | Docker Compose, deployed with Dokploy |
| Hosting | Oracle Cloud (Ampere A1, ARM64), Tailscale for private access |

## Architecture

```
Browser (Svelte 5 SPA + CodeMirror 6 + Yjs)
   │  HTTP(S) / WS(S)
Traefik (Dokploy): routes the public IP and tailnet hostnames
   │
Go server
   ├─ Web app: the built SPA (precompressed, SPA fallback)
   ├─ REST: POST /api/rooms, GET /api/rooms/:id
   ├─ WS:   /ws/:roomId  → in-memory hub per room
   └─ Persistence worker (batched writes)
   │
PostgreSQL
   │
Compactor (Node + Yjs) — merges each room's update log into a snapshot
```

**How sync works:** Clients merge edits using Yjs (a CRDT), so concurrent typing never conflicts. The Go server acts as a relay: it broadcasts binary updates to peers in the same room and persists them to PostgreSQL in batches, keeping the database off the hot path of every keystroke. When a room's update log grows too large, it is compacted into a single snapshot.

See [`docs/architecture.md`](docs/architecture.md) and [`docs/protocol.md`](docs/protocol.md) for details.

### Database schema

```sql
CREATE TABLE rooms (
    id             TEXT PRIMARY KEY,                    -- short random ID, e.g. k7x2p9
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_active_at TIMESTAMPTZ NOT NULL DEFAULT now()   -- bumped on every persisted batch
);

CREATE TABLE room_updates (
    id          BIGSERIAL PRIMARY KEY,                  -- defines replay order
    room_id     TEXT NOT NULL REFERENCES rooms (id) ON DELETE CASCADE,
    data        BYTEA NOT NULL,                         -- opaque Yjs update
    is_snapshot BOOLEAN NOT NULL DEFAULT false,         -- written by the compactor
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX room_updates_room_id_id_idx ON room_updates (room_id, id);
```

Migrations live in [`server/migrations/`](server/migrations) and are applied automatically on startup.

## Project Structure

```
pairpad/
├── web/                 # Svelte 5 frontend
├── server/              # Go backend
│   ├── cmd/             # Entrypoints
│   ├── migrations/      # Plain SQL migrations (embedded, applied on startup)
│   └── internal/
│       ├── api/         # REST handlers
│       ├── hub/         # WebSocket room hubs
│       └── store/       # PostgreSQL access
├── deploy/              # local all-in-one stack: docker-compose.yml, Caddyfile, .env.example
│   └── dokploy/         # production: compose (app + compactor), app.Dockerfile, setup guide
├── docs/                # Architecture and protocol docs
├── compactor/           # Node service that compacts update logs into Yjs snapshots
└── .github/workflows/   # CI: lint, test, build & push images
```

## Getting Started

### Prerequisites

- Docker & Docker Compose
- Go 1.22+ (for local backend development)
- Node.js 20+ and pnpm (for local frontend development)

### Run with Docker Compose

```bash
git clone https://github.com/faiz-gh/pairpad.git
cd pairpad/deploy
cp .env.example .env
docker compose up -d
```

Then open http://localhost.

### Local Development

```bash
# Backend
cd server
go run ./cmd/server

# Frontend
cd web
pnpm install
pnpm dev

# Compactor (optional locally; merges update logs into snapshots)
cd compactor
pnpm install
DATABASE_URL=postgres://pairpad:change-me@localhost:5432/pairpad pnpm start
```

The backend reads its config from environment variables. See [`deploy/.env.example`](deploy/.env.example). `docker compose` exposes Postgres on `127.0.0.1:5432` for local `go run`, and the Vite dev server proxies `/api` and `/ws` to `localhost:8080`.

## Deployment (Dokploy on Oracle Cloud)

Pairpad targets an Oracle Cloud Always Free Ampere A1 (ARM64) instance running [Dokploy](https://dokploy.com), with Tailscale for private access:

- **Postgres** is a separate Dokploy database service, so app deploys never restart it.
- **[`deploy/dokploy/docker-compose.yml`](deploy/dokploy/docker-compose.yml)** runs the app (Go server + web UI in one image) and the compactor.
- **Dokploy's Traefik** serves the app on the public IP and on the server's MagicDNS names.

Step-by-step setup: **[`deploy/dokploy/README.md`](deploy/dokploy/README.md)**.

CI publishes multi-arch images (`linux/amd64` + `linux/arm64`) to GHCR on every push to `master`: `ghcr.io/faiz-gh/pairpad-app` and `-compactor` for Dokploy, plus `-server` and `-web` for the local stack.

## Continuous Integration

[`.github/workflows/ci.yml`](.github/workflows/ci.yml) runs on every push and pull request:

| Job | Checks |
|-----|--------|
| Server (Go) | `gofmt`, `go vet`, `go test -race` including Postgres integration tests, build |
| Web (SvelteKit) | `pnpm check`, `lint`, `test`, `build`, and the **bundle budget** (initial JS < 200 KB gzipped) |
| Compactor (Node) | `tsc`, tests including Postgres integration tests |
| Images | Multi-arch builds of `app`, `compactor`, `server` and `web` once the jobs above pass; pushed to GHCR only from `master` |

## Limits & Abuse Prevention

- Rate limiting on room creation (per IP: burst of 10, then 60 per hour)
- Maximum document size: 512 KB of text
- Maximum peers per room (25)
- WebSocket origin validation and message size caps (1 MiB frames)
- Inactive rooms are automatically deleted (after 30 days without edits or visits)

All limits are configurable; see [`deploy/.env.example`](deploy/.env.example) and [`docs/architecture.md`](docs/architecture.md#limits).

## Roadmap

- **v0.1** — MVP (see Features above)
- **v0.2** — Read-only share links, room passwords, download as file, fork a pad
- **v0.3** — Optional GitHub OAuth, saved pads, interview mode (host can lock editing / clear pad)
- **Later** — Sandboxed code execution, chat, voice

## Contributing

Contributions are welcome! Please open an issue to discuss major changes before submitting a pull request.
