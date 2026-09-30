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

- [ ] Anonymous rooms with short shareable IDs (e.g. `/k7x2p`)
- [ ] Real-time collaborative editing with conflict-free merging
- [ ] Live multi-cursor and presence (who's in the room)
- [ ] Syntax highlighting with language picker
- [ ] Persistent rooms that expire after inactivity
- [ ] Dark / light theme

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
| Reverse Proxy | Caddy |
| Orchestration | Docker Compose |
| Hosting | Oracle Cloud (Ampere A1, ARM64) |

## Architecture

```
Browser (Svelte 5 SPA + CodeMirror 6 + Yjs)
   │  HTTPS / WSS
Caddy (TLS, static files, reverse proxy)
   │
Go server
   ├─ REST: POST /api/rooms, GET /api/rooms/:id
   ├─ WS:   /ws/:roomId  → in-memory hub per room
   └─ Persistence worker (batched writes)
   │
PostgreSQL
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
    is_snapshot BOOLEAN NOT NULL DEFAULT false,         -- reserved for compaction
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
├── deploy/              # docker-compose.yml, Caddyfile, .env.example
├── docs/                # Architecture and protocol docs
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
```

The backend reads its config from environment variables. See [`deploy/.env.example`](deploy/.env.example). `docker compose` exposes Postgres on `127.0.0.1:5432` for local `go run`, and the Vite dev server proxies `/api` and `/ws` to `localhost:8080`.

## Deployment (Oracle Cloud)

Pairpad targets Oracle Cloud's Always Free Ampere A1 (ARM64) instances.

1. Build multi-arch images:
```bash
   docker buildx build --platform linux/arm64,linux/amd64 -t pairpad-server ./server
```
2. Open ports **80** and **443** in both the VCN security list **and** the instance's iptables (Oracle's Ubuntu images block them by default).
3. Run `docker compose up -d` from the `deploy/` directory.
4. Set up automated `pg_dump` backups to Object Storage.

## Limits & Abuse Prevention

- Rate limiting on room creation
- Maximum document size: 512 KB
- Maximum peers per room
- WebSocket origin validation and message size caps
- Inactive rooms are automatically deleted

## Roadmap

- **v0.1** — MVP (see Features above)
- **v0.2** — Read-only share links, room passwords, download as file, fork a pad
- **v0.3** — Optional GitHub OAuth, saved pads, interview mode (host can lock editing / clear pad)
- **Later** — Sandboxed code execution, chat, voice

## Contributing

Contributions are welcome! Please open an issue to discuss major changes before submitting a pull request.
