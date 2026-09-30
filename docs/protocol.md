# WebSocket protocol

Endpoint: `GET /ws/{roomId}` (upgraded to a WebSocket). The room must exist.
Unknown rooms get `404` before the upgrade, and disallowed `Origin`s get `403`.

## Frames

Every message is a **binary** frame. Byte 0 is the message type, and the rest
is the payload. Text frames and empty frames are ignored.

| Type   | Direction        | Name   | Payload                                                 |
| ------ | ---------------- | ------ | ------------------------------------------------------- |
| `0x00` | client ⇄ server  | UPDATE | One opaque Yjs update (v1 encoding, as produced by `doc.on('update')` / `Y.encodeStateAsUpdate`) |
| `0x01` | server → client  | SYNCED | Empty. Marks the end of the history replay               |

Receivers must ignore unknown types so new types can be added without
breaking old clients.

## Session

```
client                                   server
  │ ── GET /ws/{roomId} (upgrade) ───────▶ │
  │ ◀── UPDATE (stored update 1) ───────── │  history replay, in insertion order
  │ ◀── UPDATE (stored update …) ───────── │
  │ ◀── UPDATE (stored update N) ───────── │
  │ ◀── SYNCED ─────────────────────────── │
  │ ── UPDATE (local edit) ──────────────▶ │  relayed to every *other* peer, queued for persistence
  │ ◀── UPDATE (peer edit) ─────────────── │
```

1. On join, the server sends every update it holds for the room, in order,
   and then `SYNCED`. The replay is the server's complete state for the room.
2. After that, the server relays each `UPDATE` it receives to every other peer
   in the room, never back to the sender, and queues it to be written to
   Postgres.
3. The server never decodes the payload. Yjs updates are commutative and
   idempotent, so peers can apply them in any order and duplicates are harmless.

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

## Limits and close codes

| Condition                         | Behaviour                         |
| --------------------------------- | --------------------------------- |
| Frame larger than 1 MiB           | Closed with `1009` (message too big) |
| Peer falls 256 frames behind      | Dropped. Closed with `1001`, and the client reconnects |
| Server shutdown                   | Closed with `1001`                |
| No pong for a ping (every 30 s)   | Connection closed                 |

The reference client is `web/src/lib/sync/provider.svelte.ts`, and the server
side is `server/internal/hub/conn.go`.
