// Package hub relays opaque Yjs updates between the peers of a room.
//
// Each active room has one Hub goroutine that owns the room's peer set, an
// in-memory log of every update seen so far (loaded from the database when
// the hub starts) and each peer's latest awareness (presence) frame. Neither
// updates nor awareness payloads are ever decoded.
package hub

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"github.com/coder/websocket"

	"github.com/faiz-gh/pairpad/server/internal/store"
)

// Wire message types. See docs/protocol.md.
const (
	MsgUpdate    byte = 0x00
	MsgSynced    byte = 0x01
	MsgAwareness byte = 0x02
	MsgPeerLeft  byte = 0x03
)

// Application close codes (4000–4999 are free for applications). Browsers
// expose close codes to scripts, unlike the upgrade's HTTP status.
const (
	// StatusRoomFull: the room already has MaxPeers peers. Retry later.
	StatusRoomFull websocket.StatusCode = 4008
	// StatusDocTooLarge: the room's history exceeds MaxHistoryBytes; the
	// update that crossed it was rejected.
	StatusDocTooLarge websocket.StatusCode = 4009
)

// ErrRoomFull is returned by Join when the room is at MaxPeers.
var ErrRoomFull = errors.New("hub: room full")

// MaxAwarenessSize caps awareness frames; presence state is a name, a color
// and a cursor, so anything larger is ignored rather than kept in memory.
const MaxAwarenessSize = 16 << 10

// sendBuffer is how many outbound frames a peer may lag behind before it is
// dropped as a slow consumer.
const sendBuffer = 256

// Loader reads a room's persisted updates.
type Loader interface {
	LoadUpdates(ctx context.Context, roomID string) ([][]byte, error)
}

// Persister queues updates for durable storage. Add must not block on I/O.
// Flush writes everything queued so far; a hub calls it before loading
// history so updates from a just-torn-down hub are not missed.
type Persister interface {
	Add(u store.Update)
	Flush(ctx context.Context) error
}

// Client is one connected peer. The hub writes framed messages to its send
// channel and closes the channel when the peer is removed.
type Client struct {
	send chan []byte

	// Set by the hub before it closes send; read by the write pump after.
	closeCode   websocket.StatusCode
	closeReason string
}

// NewClient returns a client with a buffered outbound queue.
func NewClient() *Client {
	return &Client{
		send:        make(chan []byte, sendBuffer),
		closeCode:   websocket.StatusGoingAway,
		closeReason: "disconnected by server",
	}
}

// Send yields framed messages destined for this peer. It is closed when the
// hub drops the peer or shuts down.
func (c *Client) Send() <-chan []byte { return c.send }

// Snapshot is what a joining peer receives before any live frame.
type Snapshot struct {
	// History holds the room's Yjs updates (payloads, without the type byte).
	History [][]byte
	// Presence holds the latest full AWARENESS frame of every other peer.
	Presence [][]byte
}

type joinReq struct {
	c     *Client
	reply chan joinReply
}

type joinReply struct {
	snap Snapshot
	err  error
}

type message struct {
	from  *Client
	frame []byte // full wire frame: message type followed by the payload
}

// refreshResult carries a reloaded history back to the hub loop. rows cover at
// least history[:upto]; entries appended after upto are kept on top.
type refreshResult struct {
	upto int
	rows [][]byte
	err  error
}

// Hub is the relay for a single room.
type Hub struct {
	id      string
	loader  Loader
	persist Persister
	log     *slog.Logger
	onStop  func(*Hub)

	// refreshMin is the history length at which the hub first reloads its
	// in-memory log from the database to pick up compacted snapshots.
	refreshMin int
	// maxPeers and maxHistoryBytes are the room limits; see Options.
	maxPeers        int
	maxHistoryBytes int

	join      chan joinReq
	leave     chan *Client
	broadcast chan message
	refreshed chan refreshResult
	quit      chan struct{}
	done      chan struct{}

	// bg tracks the refresh goroutine so the manager can wait for it.
	bg sync.WaitGroup

	// err is set before done is closed if the hub failed to start.
	err error

	// Owned by the run goroutine.
	clients      map[*Client]struct{}
	presence     map[*Client][]byte // latest AWARENESS frame per peer
	history      [][]byte
	historyBytes int // sum of len(history[i])
	refreshing   bool
	nextRefresh  int
}

func newHub(id string, loader Loader, persist Persister, log *slog.Logger, opts Options, onStop func(*Hub)) *Hub {
	return &Hub{
		id:              id,
		loader:          loader,
		persist:         persist,
		log:             log.With("room", id),
		onStop:          onStop,
		refreshMin:      opts.HistoryRefreshMin,
		maxPeers:        opts.MaxPeers,
		maxHistoryBytes: opts.MaxHistoryBytes,
		join:            make(chan joinReq),
		leave:           make(chan *Client),
		broadcast:       make(chan message),
		refreshed:       make(chan refreshResult),
		quit:            make(chan struct{}),
		done:            make(chan struct{}),
		clients:         make(map[*Client]struct{}),
		presence:        make(map[*Client][]byte),
	}
}

// ID returns the room ID.
func (h *Hub) ID() string { return h.id }

// Done is closed once the hub has stopped.
func (h *Hub) Done() <-chan struct{} { return h.done }

// Leave removes c from the room. It is safe to call after the hub stopped or
// after c was already dropped.
func (h *Hub) Leave(c *Client) {
	select {
	case h.leave <- c:
	case <-h.done:
	}
}

// Broadcast relays a frame from c to every other peer. UPDATE payloads are
// also appended to the history and queued for persistence; AWARENESS frames
// are remembered as c's presence. Other types are ignored. frame must not be
// modified afterwards.
func (h *Hub) Broadcast(c *Client, frame []byte) {
	select {
	case h.broadcast <- message{from: c, frame: frame}:
	case <-h.done:
	}
}

// tryJoin registers c and returns the snapshot it must replay first. ok is
// false if the hub stopped before accepting the join; err is ErrRoomFull if
// it refused it.
func (h *Hub) tryJoin(ctx context.Context, c *Client) (snap Snapshot, ok bool, err error) {
	reply := make(chan joinReply, 1)
	select {
	case h.join <- joinReq{c: c, reply: reply}:
		r := <-reply
		return r.snap, r.err == nil, r.err
	case <-h.done:
		return Snapshot{}, false, nil
	case <-ctx.Done():
		return Snapshot{}, false, ctx.Err()
	}
}

func (h *Hub) run(parent context.Context) {
	// Cancelled when the hub stops, which aborts an in-flight refresh.
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	defer func() {
		for c := range h.clients {
			close(c.send)
		}
		h.clients = nil
		h.onStop(h)
		close(h.done)
	}()

	history, err := h.load(ctx)
	if err != nil {
		h.err = err
		h.log.Error("load history failed", "err", err)
		return
	}
	h.setHistory(history)
	h.nextRefresh = max(h.refreshMin, 2*len(history))
	h.log.Debug("hub started", "updates", len(history))

	for {
		select {
		case req := <-h.join:
			if len(h.clients) >= h.maxPeers {
				req.reply <- joinReply{err: ErrRoomFull}
				continue
			}
			h.clients[req.c] = struct{}{}
			presence := make([][]byte, 0, len(h.presence))
			for _, f := range h.presence {
				presence = append(presence, f)
			}
			// Hand out a length-capped view: later appends never touch
			// the elements the client is replaying.
			req.reply <- joinReply{snap: Snapshot{
				History:  h.history[:len(h.history):len(h.history)],
				Presence: presence,
			}}

		case c := <-h.leave:
			h.remove(c)
			if len(h.clients) == 0 {
				h.log.Debug("hub empty; stopping")
				return
			}

		case m := <-h.broadcast:
			if _, ok := h.clients[m.from]; !ok {
				continue // peer was dropped; it resends full state on reconnect
			}
			switch m.frame[0] {
			case MsgUpdate:
				update := m.frame[1:]
				if h.historyBytes+len(update) > h.maxHistoryBytes {
					// Abuse backstop: honest clients cap the document well
					// below this. The update is neither relayed nor stored,
					// and the sender is disconnected so it can't diverge
					// silently from everyone else.
					h.log.Warn("rejecting update over room history limit",
						"historyBytes", h.historyBytes, "update", len(update))
					h.removeWith(m.from, StatusDocTooLarge, "document too large")
					if len(h.clients) == 0 {
						return
					}
					continue
				}
				h.history = append(h.history, update)
				h.historyBytes += len(update)
				h.persist.Add(store.Update{RoomID: h.id, Data: update})
				h.maybeRefresh(ctx)
			case MsgAwareness:
				if len(m.frame) > MaxAwarenessSize {
					continue
				}
				h.presence[m.from] = m.frame
			default:
				continue
			}
			h.relay(m.from, m.frame)

		case r := <-h.refreshed:
			h.applyRefresh(r)

		case <-h.quit:
			return
		}
	}
}

// load flushes pending writes, so the database holds everything this process
// has relayed, then reads the room's stored updates (snapshots first).
func (h *Hub) load(ctx context.Context) ([][]byte, error) {
	if err := h.persist.Flush(ctx); err != nil {
		return nil, err
	}
	return h.loader.LoadUpdates(ctx, h.id)
}

// maybeRefresh reloads the history from the database in the background once
// it has grown past nextRefresh. The compactor may have merged the stored
// rows into a snapshot by then, so the reload lets long-lived rooms shed their
// accumulated updates. The threshold doubles each time, so a room that isn't
// being compacted pays amortised O(1) per update.
func (h *Hub) maybeRefresh(ctx context.Context) {
	if h.refreshing || len(h.history) < h.nextRefresh {
		return
	}
	h.refreshing = true
	upto := len(h.history) // every entry below upto has been handed to persist
	h.bg.Add(1)
	go func() {
		defer h.bg.Done()
		rows, err := h.load(ctx)
		select {
		case h.refreshed <- refreshResult{upto: upto, rows: rows, err: err}:
		case <-ctx.Done():
		}
	}()
}

func (h *Hub) applyRefresh(r refreshResult) {
	h.refreshing = false
	if r.err != nil {
		h.log.Warn("history refresh failed", "err", r.err)
		h.nextRefresh = len(h.history) + h.refreshMin
		return
	}
	// rows hold at least history[:upto]; later entries may appear twice if
	// they were flushed meanwhile, which is harmless as Yjs updates are
	// idempotent. Build a new slice: peers may still be replaying the old one.
	before := len(h.history)
	fresh := make([][]byte, 0, len(r.rows)+before-r.upto)
	fresh = append(fresh, r.rows...)
	fresh = append(fresh, h.history[r.upto:]...)
	h.setHistory(fresh)
	h.nextRefresh = max(h.refreshMin, 2*len(fresh))
	h.log.Debug("history refreshed", "before", before, "after", len(fresh))
}

// relay sends frame to every peer except from (nil sends to everyone),
// dropping peers whose queue is full rather than blocking the room.
func (h *Hub) relay(from *Client, frame []byte) {
	var slow []*Client
	for c := range h.clients {
		if c == from {
			continue
		}
		select {
		case c.send <- frame:
		default:
			slow = append(slow, c)
		}
	}
	for _, c := range slow {
		h.log.Warn("dropping slow peer")
		h.remove(c)
	}
}

// remove disconnects c. If c had announced presence, the remaining peers get
// a PEER_LEFT frame carrying c's last awareness payload, from which they read
// the awareness client IDs to clear.
func (h *Hub) setHistory(history [][]byte) {
	h.history = history
	h.historyBytes = 0
	for _, u := range history {
		h.historyBytes += len(u)
	}
}

func (h *Hub) remove(c *Client) {
	h.removeWith(c, websocket.StatusGoingAway, "disconnected by server")
}

// removeWith is remove with the close code and reason c's socket gets.
func (h *Hub) removeWith(c *Client, code websocket.StatusCode, reason string) {
	if _, ok := h.clients[c]; !ok {
		return
	}
	delete(h.clients, c)
	c.closeCode, c.closeReason = code, reason
	close(c.send)
	if last, ok := h.presence[c]; ok {
		delete(h.presence, c)
		left := make([]byte, len(last))
		copy(left, last)
		left[0] = MsgPeerLeft
		h.relay(nil, left)
	}
}
