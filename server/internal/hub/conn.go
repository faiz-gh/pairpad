package hub

import (
	"context"
	"errors"
	"time"

	"github.com/coder/websocket"
)

const (
	// MaxMessageSize caps inbound frames; larger frames close the socket
	// with status 1009 (message too big).
	MaxMessageSize = 1 << 20

	writeTimeout = 10 * time.Second
	pingInterval = 30 * time.Second
)

// Serve runs a peer for roomID over conn until either side disconnects. It
// replays history, sends SYNCED, then relays live updates both ways.
func (m *Manager) Serve(ctx context.Context, conn *websocket.Conn, roomID string) {
	defer conn.CloseNow() //nolint:errcheck

	c := NewClient()
	h, history, err := m.Join(ctx, roomID, c)
	if err != nil {
		if errors.Is(err, ErrClosed) {
			conn.Close(websocket.StatusGoingAway, "server shutting down") //nolint:errcheck
		} else {
			m.log.Error("join failed", "room", roomID, "err", err)
			conn.Close(websocket.StatusInternalError, "join failed") //nolint:errcheck
		}
		return
	}
	defer h.Leave(c)

	conn.SetReadLimit(MaxMessageSize)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		defer cancel()
		if err := writePump(ctx, conn, c, history); err != nil && ctx.Err() == nil {
			m.log.Debug("write pump ended", "room", roomID, "err", err)
		}
	}()

	for {
		typ, msg, err := conn.Read(ctx)
		if err != nil {
			return
		}
		if typ != websocket.MessageBinary || len(msg) < 2 {
			continue
		}
		switch msg[0] {
		case MsgUpdate:
			h.Broadcast(c, msg)
		default:
			// Unknown types are ignored for forward compatibility.
		}
	}
}

func writePump(ctx context.Context, conn *websocket.Conn, c *Client, history [][]byte) error {
	write := func(frame []byte) error {
		wctx, cancel := context.WithTimeout(ctx, writeTimeout)
		defer cancel()
		return conn.Write(wctx, websocket.MessageBinary, frame)
	}

	buf := make([]byte, 0, 1024)
	for _, u := range history {
		buf = append(append(buf[:0], MsgUpdate), u...)
		if err := write(buf); err != nil {
			return err
		}
	}
	if err := write([]byte{MsgSynced}); err != nil {
		return err
	}

	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()
	for {
		select {
		case frame, ok := <-c.send:
			if !ok {
				return conn.Close(websocket.StatusGoingAway, "disconnected by server")
			}
			if err := write(frame); err != nil {
				return err
			}
		case <-ticker.C:
			pctx, cancel := context.WithTimeout(ctx, writeTimeout)
			err := conn.Ping(pctx)
			cancel()
			if err != nil {
				return err
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
