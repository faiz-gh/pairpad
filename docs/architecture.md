# Architecture

```
Browser (SvelteKit SPA · CodeMirror 6 · Yjs)
   │  HTTP(S) / WS(S)
Production: Traefik (Dokploy) → Go server, which also serves the web app (STATIC_DIR)
Local stack: Caddy serves web/build and proxies /api/* and /ws/* to server:8080
   │
Go server
   ├─ internal/api    REST + WebSocket upgrade (origin check, 1 MiB frame cap)
   ├─ internal/hub    one goroutine per active room: relay + in-memory update log
   └─ internal/store  Postgres access, migrations, persistence batcher
   │
PostgreSQL — rooms, room_updates
   │
Compactor (Node + yjs) — merges each room's update log into a snapshot
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
- **History refresh:** once a live hub's in-memory log reaches
  `HISTORY_REFRESH_MIN` entries (default 1000), a background goroutine flushes
  and reloads the room from Postgres, and the hub swaps its log for
  `reloaded rows + entries appended since`. If the compactor has run, this
  collapses the log to a snapshot plus a short tail. The threshold then becomes
  twice the new length, so a room that isn't being compacted pays amortised
  O(1) per update. Entries flushed during the reload can appear twice, which is
  harmless because Yjs updates are idempotent.

## Presence

Cursors and "who's here" use the y-protocols **awareness** CRDT, carried over
the same socket (`AWARENESS` / `PEER_LEFT`, see [protocol.md](protocol.md)).
The hub stores each peer's latest awareness frame as opaque bytes next to its
peer set. It replays those frames to joiners after `SYNCED`, and when a peer
leaves it echoes the peer's last frame to the others as `PEER_LEFT` so they
can clear that cursor at once. Awareness never touches Postgres or the update
history.

In the browser, `PairpadProvider` owns an `Awareness` and exposes a reactive
`peers` list for the toolbar avatars. `yCollab(ytext, awareness)` draws remote
carets, name labels and selections. Each tab gets a random anonymous identity
("Mellow Panda" plus a color), kept in `sessionStorage` so a reload keeps it
while two tabs remain two people.

## Languages

The pad's language lives in the shared doc as `meta.language` (a `Y.Map`
entry, via `RoomLanguage`). A change in one tab reaches every peer like any
other edit, and it's persisted and compacted with the document. The server
and compactor don't know it exists. Concurrent changes converge through
Y.Map's last-writer-wins, and unknown values fall back to plain text.

`src/lib/editor/languages.ts` lists 21 languages. Each one's grammar, plus
syntax highlighting, bracket matching and indent-on-input, is loaded with a
dynamic `import()` into a CodeMirror compartment, so every language is its
own chunk (Python is about 30 KB gzipped) and none of it is in the initial
bundle. The editor is editable as soon as the room syncs, and highlighting
appears when the chunk arrives. Lezer grammars come from the official
`@codemirror/lang-*` packages. C#, Kotlin, Ruby, Shell and Swift use
`@codemirror/legacy-modes`. The picker is disabled until the first `SYNCED`,
because a change made earlier would never reach the server.

## Themes

Light, dark and "system" (follows the OS live), toggled from the toolbar and
kept per browser in `localStorage` (`pairpad:theme`), never shared with the
room. The resolved mode is a single `dark` class on `<html>`, which is what
shadcn's tokens key off. An inline script in `app.html` sets it before first
paint, so there's no light flash. The editor chrome (background, gutter,
active line, selection, bracket match) and the syntax colors are all CSS
variables in `routes/layout.css`, so switching themes never reconfigures
CodeMirror. The highlight style (`src/lib/editor/highlight.ts`) is loaded
lazily with the first language, so plain-text pads never download it.

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

## Room expiry

A room is **active** when it receives edits (every persisted batch bumps
`rooms.last_active_at`) or when someone joins. The WebSocket handler's
existence check is `TouchRoom`, a single `UPDATE … RETURNING`. Every
`EXPIRY_INTERVAL` (1 h, and once at startup) `store.Expirer` deletes rooms
idle longer than `ROOM_TTL` (default 30 days), 500 at a time, oldest first.
Their updates go with them via `ON DELETE CASCADE`. Rooms with a running hub
(connected peers) are passed as a keep-list and are never deleted, even if
nobody has typed for longer than the TTL.

Races, and why they're safe:

- **Join vs. sweep:** the DELETE repeats `last_active_at < cutoff` in its
  outer `WHERE`. If a join's touch commits first, Postgres re-evaluates the
  row after waiting for its lock and skips it. If the DELETE wins, the touch
  finds no row and the join gets 404. (Covered by a DB test.)
- **Pending writes for a deleted room:** `InsertUpdates` locks the batch's
  rooms `FOR KEY SHARE` and drops, with a warning, updates for rooms that no
  longer exist. A foreign-key failure would otherwise make the batcher retry
  the same batch forever and stall persistence for every room.
- **A tab open on a pad that disappears** (expired while the tab was offline
  or asleep): the upgrade gets 404, which browsers can't see, so the provider
  asks `GET /api/rooms/:id` whenever a connection fails before opening. If
  the room is gone it stops retrying, and the page shows an "expired" alert
  with the local text kept read-only for copying.

## Limits

| Limit | Where | Default | Behaviour |
| --- | --- | --- | --- |
| Room creation rate | `POST /api/rooms`, per client IP | burst 10, refill 60/h | `429` + `Retry-After`; the UI says how long to wait |
| Peers per room | hub join | 25 | WebSocket closed with `4008`; the client waits and retries |
| Document size | editor (CodeMirror transaction filter) | 512 KB of text | Local edits that would grow past it are rejected with a toolbar notice. Deleting always works, and remote changes are never filtered, so peers can't diverge |
| Room history size | hub (server backstop) | 8 MiB | Offending update dropped, sender closed with `4009` |
| Frame size | WebSocket read limit | 1 MiB | Closed with `1009` |
| Awareness frame | hub | 16 KiB | Ignored |

The client IP for rate limiting is `X-Forwarded-For`, believed only when the
direct peer is a trusted proxy (`TRUSTED_PROXIES`, default private and
loopback ranges, which covers Caddy on the compose network). The first
untrusted hop from the right wins, so a client can't reset its own bucket by
sending a forged header. The limiter (`internal/ratelimit`) is an in-memory
token bucket per IP that prunes idle keys, which fits the single-instance
design.

The server can't enforce "512 KB" itself because it never decodes Yjs. The
editor is the real enforcement, and the history-bytes cap stops a modified
client from growing a room's memory without bound.

## Compaction

Every keystroke becomes one `room_updates` row, so without compaction a pad's
load time grows with its whole edit history. The **compactor**
(`compactor/`, Node 24 + `yjs`) is the only component that decodes Yjs. It
runs as its own compose service, so the Go relay stays opaque.

Every `COMPACT_INTERVAL` (30 s) it picks up to `COMPACT_ROOMS_PER_PASS` rooms
with at least `COMPACT_MIN_UPDATES` (500) rows, largest first, and for each one
runs a single transaction:

1. `pg_try_advisory_xact_lock` per room (skip if another compactor holds it).
2. Read all of the room's rows (ids + data).
3. Apply them to a fresh `Y.Doc` (gc on, so deleted content is dropped) and
   encode its state as one update. Check that the snapshot reproduces itself
   byte for byte. A malformed row throws, and the transaction rolls back with
   the log untouched.
4. Insert the snapshot with `is_snapshot = true`, then delete **exactly the ids
   read in step 2**.

Rows the Go server inserts during the transaction are never deleted. They sit
next to the snapshot until the next pass, and since Yjs merges updates in any
order the result is the same. Replay order is `is_snapshot DESC, id`, so the
snapshot comes first. The compactor needs no coordination with live hubs:
their in-memory logs stay valid, and they pick up the snapshot on their next
history refresh.

In a real run, 600 keystroke rows (11.7 KB) became a single 621-byte snapshot.

## Schema

See `server/migrations/`. Migrations are embedded in the binary and applied on
startup, under a Postgres advisory lock, and tracked in `schema_migrations`.

- `rooms(id, created_at, last_active_at)`: `id` is 6 characters from
  `23456789abcdefghjkmnpqrstuvwxyz`.
- `room_updates(id bigserial, room_id, data bytea, is_snapshot, created_at)`
  with an index on `(room_id, id)`. `is_snapshot` marks rows written by the
  compactor.

## Frontend

A SvelteKit static SPA (`adapter-static`, `ssr = false`, fallback `index.html`,
`precompress` writing `.br`/`.gz` next to each asset). `/` creates a room and
redirects, and `/[roomId]` opens the editor. Svelte 5 runes only.

In production the Go server serves the build when `STATIC_DIR` is set
(`internal/api/static.go`):
- it sends the precompressed variant the client accepts,
- `_app/immutable/*` is cached for a year and everything else revalidates,
- extension-less unknown paths get `index.html`, and missing assets get 404.
Unknown `/api/*` and `/ws/*` paths stay JSON 404s.

The local all-in-one stack serves the same files with Caddy (`file_server
precompressed`).

## Deployment

See [`deploy/dokploy/README.md`](../deploy/dokploy/README.md). On Dokploy:

- **Two compose services.** `app` (`deploy/dokploy/app.Dockerfile`: web build +
  Go server in one 25 MB distroless image) and `compactor`.
- **Postgres** is a separate Dokploy database service, reached over
  `dokploy-network`.
- **Traefik routing** is in compose labels: one HTTP router for the public IP
  and the two MagicDNS names, plus commented HTTPS labels for when there's a
  domain.
- **Rate limiting** sees the real client address: Traefik is on a private
  network and therefore trusted for `X-Forwarded-For`, while tailnet
  addresses (100.64.0.0/10) are not.
- **No secure context** without a domain, so "Copy link" falls back from the
  async Clipboard API to `execCommand('copy')`.
- **One app replica only.** Room hubs live in memory, so Compose mode, not
  Stack.
