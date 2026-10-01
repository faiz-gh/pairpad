# WebSocket protocol

Endpoint: `GET /ws/{roomId}` (upgraded to a WebSocket). The room must exist.
Unknown or expired rooms get `404` before the upgrade, and disallowed
`Origin`s get `403`. A successful join counts as room activity for expiry.

## Frames

Every message is a **binary** frame. Byte 0 is the message type, and the rest
is the payload. Text frames and empty frames are ignored.

| Type   | Direction        | Name   | Payload                                                 |
| ------ | ---------------- | ------ | ------------------------------------------------------- |
| `0x00` | client ⇄ server  | UPDATE | One opaque Yjs update (v1 encoding, as produced by `doc.on('update')` / `Y.encodeStateAsUpdate`) |
| `0x01` | server → client  | SYNCED | Empty. Marks the end of the history replay               |
| `0x02` | client ⇄ server  | AWARENESS | One y-protocols awareness update (`encodeAwarenessUpdate`) holding the sender's own presence state |
| `0x03` | server → client  | PEER_LEFT | The departed peer's last AWARENESS payload, sent verbatim |

Receivers must ignore unknown types so new types can be added without
breaking old clients.

## Session

```
client                                   server
  │ ── GET /ws/{roomId} (upgrade) ───────▶ │
  │ ◀── UPDATE (stored update 1) ───────── │  history replay (snapshot first, if any)
  │ ◀── UPDATE (stored update …) ───────── │
  │ ◀── UPDATE (stored update N) ───────── │
  │ ◀── SYNCED ─────────────────────────── │
  │ ◀── AWARENESS (each other peer) ────── │  latest presence of everyone already here
  │ ── AWARENESS (own state) ────────────▶ │  relayed, remembered as this peer's presence
  │ ── UPDATE (local edit) ──────────────▶ │  relayed to every *other* peer, queued for persistence
  │ ◀── UPDATE / AWARENESS (peers) ─────── │
  │ ◀── PEER_LEFT (peer disconnected) ──── │
```

1. On join, the server sends every update it holds for the room, and then
   `SYNCED`. The replay is the server's complete state for the room. It may
   start with a compacted snapshot and may contain duplicates. Clients must
   not rely on replay order or on one frame per keystroke.
2. After that, the server relays each `UPDATE` it receives to every other peer
   in the room, never back to the sender, and queues it to be written to
   Postgres.
3. The server never decodes the payload. Yjs updates are commutative and
   idempotent, so peers can apply them in any order and duplicates are harmless.

### Presence

- `AWARENESS` frames are relayed to the other peers and **never persisted**.
  The server keeps only the latest frame per connection and replays those to
  a joiner right after `SYNCED`. Frames over 16 KiB are ignored.
- When a peer disconnects (clean close, crash, or dropped as a slow
  consumer), the server sends the others `PEER_LEFT` carrying that peer's
  last awareness payload. Receivers read the client IDs from it and call
  `removeAwarenessStates`, so cursors vanish immediately instead of after
  y-protocols' 30 s timeout. The server still treats the payload as opaque.
  An awareness update is `varUint(count)`, then per entry `varUint(clientID)`,
  `varUint(clock)`, `varString(stateJSON)`.
- Presence state is `{ user: { name, color, colorLight } }`, plus the
  `cursor` field that y-codemirror.next maintains.

## Client rules

- Apply received updates with a transaction origin that marks them as remote,
  so they aren't sent back.
- Send local updates only after `SYNCED` on the current socket. The editor is
  read-only until the first `SYNCED`.
- **Reconnect:** after `SYNCED` on a reconnect, send
  `Y.encodeStateAsUpdate(doc, serverStateVector)`, where `serverStateVector`
  is `Y.encodeStateVectorFromUpdate(Y.mergeUpdates(replayedUpdates))`. That
  sends only the edits the server is missing, whether they were made offline
  or lost in flight when the socket dropped.
- Reconnect with exponential backoff: 0.5 s, doubling, capped at 10 s.
- Custom close codes use the 4000–4999 application range because browsers
  expose close codes to scripts but not the upgrade's HTTP status.
- Enforce the 512 KB document limit **locally** (reject local edits that grow
  the text past 524,288 characters, and never filter remote changes). The
  server can't measure a Yjs document, so `4009` is only a backstop against
  misbehaving clients.
- If a connection fails **before opening**, check `GET /api/rooms/{roomId}`
  first. A 404 means the room expired or was deleted: stop reconnecting and
  treat the local document as read-only. (Browsers don't expose the upgrade's
  HTTP status to `WebSocket`.)
- Publish only your **own** awareness state (encode `[doc.clientID]` only).
  The server echoes your last frame as `PEER_LEFT`, so listing other clients
  would make everyone drop them.
- After each `SYNCED`, re-set your local awareness state before sending it.
  This bumps its clock, and peers that removed you on `PEER_LEFT` reject a
  state whose clock hasn't advanced.
- When the socket closes, drop all remote awareness states. They're stale
  until the next `SYNCED` replays the current ones.

## Limits and close codes

| Condition                         | Behaviour                         |
| --------------------------------- | --------------------------------- |
| Frame larger than 1 MiB           | Closed with `1009` (message too big) |
| Peer falls 256 frames behind      | Dropped. Closed with `1001`, and the client reconnects. Others get `PEER_LEFT` |
| Room already has `MAX_PEERS` peers (default 25) | Upgrade accepted, then closed with **`4008` room full**. The client shows "pad is full" and retries every 5 s |
| An `UPDATE` would push the room's in-memory history past `MAX_HISTORY_BYTES` (default 8 MiB) | Update dropped (not relayed or stored). Sender closed with **`4009` document too large** |
| Server shutdown                   | Closed with `1001`                |
| No pong for a ping (every 30 s)   | Connection closed                 |

The reference client is `web/src/lib/sync/provider.svelte.ts`, and the server
side is `server/internal/hub/conn.go`.
