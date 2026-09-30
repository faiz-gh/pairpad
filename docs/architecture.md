# Architecture

```
Browser (SvelteKit SPA · CodeMirror 6 · Yjs)
   │  HTTP(S) / WS(S)
Caddy — serves web/build, proxies /api/* and /ws/* to server:8080
   │
Go server
   ├─ internal/api    REST + WebSocket upgrade (origin check, 1 MiB frame cap)
   ├─ internal/hub    one goroutine per active room: relay + in-memory update log
   └─ internal/store  Postgres access, migrations, persistence batcher
   │
PostgreSQL — rooms, room_updates
```

## Sync

All merging happens in the browser. Each pad is a `Y.Doc` with a single
`Y.Text` named `content`, bound to CodeMirror by `y-codemirror.next`. The
server is a relay that treats Yjs updates as **opaque bytes**. It never decodes
them, so it needs no Yjs implementation and there is no CRDT logic in Go. The
wire format is in [protocol.md](protocol.md).

We didn't use y-websocket's protocol because its handshake (SyncStep1 → SyncStep2)
needs the server to compute a diff from a server-side `Y.Doc`. Our protocol
replaces that with "replay the full log, then `SYNCED`". The client works out
what the server is missing on its own (see the reconnect rule).

## Hub lifecycle

- `hub.Manager` starts a room's `Hub` on the first join and forgets it when the
  hub stops. A hub stops when its last peer leaves, or when the server shuts down.
- On start, a hub **flushes the batcher, then loads** the room's updates from
  Postgres into an in-memory log. The flush guarantees that updates from a
  hub that just stopped are visible to the new one.
- The hub goroutine owns the peer set and the log. Joins receive a snapshot of
  the log (replayed by the peer's write pump), followed by `SYNCED`, followed
  by live frames. Registration and snapshot happen atomically in the hub loop,
  so no update can fall between the replay and the live stream.
- Broadcast does a non-blocking send into each peer's 256-frame queue. A peer
  whose queue is full is dropped rather than slowing the room down.
- Join/teardown race: `Manager.Join` retries if the hub it found stopped
  before accepting the join. The hub removes itself from the map before it
  signals done.

**Single-instance assumption:** the in-memory log and per-room hub assume one
server process owns each room. Scaling out needs sticky routing by room ID, or
a shared pub/sub plus reading history from Postgres.

## Persistence

- `store.Batcher` buffers `(room_id, update)` pairs under a mutex. `Add` never
  does I/O, so the relay path never waits on Postgres.
- A background loop writes the buffer every **500 ms**, or as soon as **50**
  updates are queued, whichever is first. Each batch is written in one
  transaction with `COPY` and also bumps `rooms.last_active_at`.
- A failed batch is put back at the front of the buffer and retried on the
  next tick. Size-triggered retries are suppressed while writes are failing.
- **Graceful shutdown** (SIGTERM/SIGINT): stop accepting HTTP → close all hubs
  (peers get `1001` and reconnect elsewhere or later) → final flush, retried
  until `SHUTDOWN_TIMEOUT`. Compose's `stop_grace_period` (15 s) is longer than
  that timeout (10 s).

## Schema

See `server/migrations/`. Migrations are embedded in the binary and applied on
startup, under a Postgres advisory lock, and tracked in `schema_migrations`.

- `rooms(id, created_at, last_active_at)`: `id` is 6 characters from
  `23456789abcdefghjkmnpqrstuvwxyz`.
- `room_updates(id bigserial, room_id, data bytea, is_snapshot, created_at)`
  with an index on `(room_id, id)`. `is_snapshot` is reserved for compaction,
  which isn't implemented yet.

## Frontend

A SvelteKit static SPA (`adapter-static`, `ssr = false`, fallback `index.html`).
Caddy serves it with `try_files {path} /index.html`. `/` creates a room and
redirects, and `/[roomId]` opens the editor. Svelte 5 runes only.
