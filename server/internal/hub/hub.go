// Package hub relays opaque Yjs updates between the peers of a room.
//
// Each active room has one Hub goroutine that owns the room's peer set and an
// in-memory log of every update seen so far (loaded from the database when
// the hub starts). Updates are never decoded.
package hub

import (
	"context"
	"log/slog"

	"github.com/faiz-gh/pairpad/server/internal/store"
)

// Wire message types. See docs/protocol.md.
const (
	MsgUpdate byte = 0x00
	MsgSynced byte = 0x01
)

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
}

// NewClient returns a client with a buffered outbound queue.
func NewClient() *Client {
	return &Client{send: make(chan []byte, sendBuffer)}
}

// Send yields framed messages destined for this peer. It is closed when the
// hub drops the peer or shuts down.
func (c *Client) Send() <-chan []byte { return c.send }

type joinReq struct {
	c     *Client
	reply chan [][]byte
}

type message struct {
	from  *Client
	frame []byte // full wire frame: MsgUpdate followed by the update
}

// Hub is the relay for a single room.
type Hub struct {
	id      string
	loader  Loader
	persist Persister
	log     *slog.Logger
	onStop  func(*Hub)

	join      chan joinReq
	leave     chan *Client
	broadcast chan message
	quit      chan struct{}
	done      chan struct{}

	// err is set before done is closed if the hub failed to start.
	err error

	// Owned by the run goroutine.
	clients map[*Client]struct{}
	history [][]byte
}

func newHub(id string, loader Loader, persist Persister, log *slog.Logger, onStop func(*Hub)) *Hub {
	return &Hub{
		id:        id,
		loader:    loader,
		persist:   persist,
		log:       log.With("room", id),
		onStop:    onStop,
		join:      make(chan joinReq),
		leave:     make(chan *Client),
		broadcast: make(chan message),
		quit:      make(chan struct{}),
		done:      make(chan struct{}),
		clients:   make(map[*Client]struct{}),
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

// Broadcast relays an UPDATE frame from c to every other peer and queues its
// payload for persistence. frame must start with MsgUpdate and must not be
// modified afterwards.
func (h *Hub) Broadcast(c *Client, frame []byte) {
	select {
	case h.broadcast <- message{from: c, frame: frame}:
	case <-h.done:
	}
}

// tryJoin registers c and returns the history it must replay first. ok is
// false if the hub stopped before accepting the join.
func (h *Hub) tryJoin(ctx context.Context, c *Client) (history [][]byte, ok bool, err error) {
	reply := make(chan [][]byte, 1)
	select {
	case h.join <- joinReq{c: c, reply: reply}:
		return <-reply, true, nil
	case <-h.done:
		return nil, false, nil
	case <-ctx.Done():
		return nil, false, ctx.Err()
	}
}

func (h *Hub) run(ctx context.Context) {
	defer func() {
		for c := range h.clients {
			close(c.send)
		}
		h.clients = nil
		h.onStop(h)
		close(h.done)
	}()

	if err := h.persist.Flush(ctx); err != nil {
		h.err = err
		h.log.Error("flush before load failed", "err", err)
		return
	}
	history, err := h.loader.LoadUpdates(ctx, h.id)
	if err != nil {
		h.err = err
		h.log.Error("load history failed", "err", err)
		return
	}
	h.history = history
	h.log.Debug("hub started", "updates", len(history))

	for {
		select {
		case req := <-h.join:
			h.clients[req.c] = struct{}{}
			// Hand out a length-capped view: later appends never touch
			// the elements the client is replaying.
			req.reply <- h.history[:len(h.history):len(h.history)]

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
			update := m.frame[1:]
			h.history = append(h.history, update)
			h.persist.Add(store.Update{RoomID: h.id, Data: update})
			for c := range h.clients {
				if c == m.from {
					continue
				}
				select {
				case c.send <- m.frame:
				default:
					h.log.Warn("dropping slow peer")
					h.remove(c)
				}
			}

		case <-h.quit:
			return
		}
	}
}

func (h *Hub) remove(c *Client) {
	if _, ok := h.clients[c]; ok {
		delete(h.clients, c)
		close(c.send)
	}
}
