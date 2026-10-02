# Pairpad

> A fast, conflict-free collaborative code pad. No signup, no modals. Powered by a Go backend, Svelte 5 frontend, and Yjs CRDTs for real-time synchronization over WebSockets.

**🚀 Try it live at [pairpad.site](https://pairpad.site)**

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
| Reverse Proxy | Traefik (via Dokploy) |
| Orchestration | Docker Compose, deployed with Dokploy |
| Hosting | Oracle Cloud (Ampere A1, ARM64), Tailscale for private access |

## Architecture

```
Browser (Svelte 5 SPA + CodeMirror 6 + Yjs)
   │  HTTP(S) / WS(S)
Traefik (Dokploy): routes the public IP and custom domains
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
├── deploy/              # Dokploy deployment configuration (docker-compose.yml, Dockerfile, env)
├── docs/                # Architecture and protocol docs
└── compactor/           # Node service that compacts update logs into Yjs snapshots
```

## Getting Started

### Prerequisites

- Go 1.22+ (for backend)
- Node.js 20+ and pnpm (for frontend)
- PostgreSQL (running locally)

### Local Development

1. **Database:** Ensure you have a local PostgreSQL instance running. 

2. **Backend:**
```bash
cd server
DATABASE_URL=postgres://user:pass@localhost:5432/pairpad?sslmode=disable go run ./cmd/server
```

3. **Frontend:**
```bash
cd web
pnpm install
pnpm dev
```
The Vite dev server will proxy `/api` and `/ws` to the Go backend running on `localhost:8080`.

4. **Compactor (optional locally):** Merges update logs into snapshots.
```bash
cd compactor
pnpm install
DATABASE_URL=postgres://user:pass@localhost:5432/pairpad pnpm start
```

For a list of all backend configuration variables, see [`deploy/.env.example`](deploy/.env.example).

## Deployment

Pairpad is designed to be deployed using [Dokploy](https://dokploy.com) (e.g. on an Oracle Cloud Always Free Ampere A1 instance). 

- **PostgreSQL** is managed as a separate Dokploy database service so app deploys never restart it.
- **[`deploy/docker-compose.yml`](deploy/docker-compose.yml)** defines the app (Go server + web UI in one container) and the compactor service.
- **Dokploy's Traefik** handles routing and automatically provisions Let's Encrypt certificates for your domains.

For a step-by-step production setup guide, see: **[`deploy/README.md`](deploy/README.md)**.

## Limits & Abuse Prevention

- Rate limiting on room creation (per IP: burst of 10, then 60 per hour)
- Maximum document size: 512 KB of text
- Maximum peers per room (25)
- WebSocket origin validation and message size caps (1 MiB frames)
- Inactive rooms are automatically deleted (after 30 days without edits or visits)

All limits are configurable via environment variables. See [`deploy/.env.production.example`](deploy/.env.production.example) and [`docs/architecture.md`](docs/architecture.md#limits).

## Roadmap

- **v0.1** — MVP (see Features above)
- **v0.2** — Read-only share links, room passwords, download as file, fork a pad
- **v0.3** — Optional GitHub OAuth, saved pads, interview mode (host can lock editing / clear pad)
- **Later** — Sandboxed code execution, chat, voice

## Contributing

Contributions are highly welcome! Whether it's a bug fix, new feature, or documentation improvement, we'd love your help.

If you have a major change in mind, please open an issue to discuss it before submitting a pull request. Feel free to jump in and submit a PR if you spot something that needs fixing!
