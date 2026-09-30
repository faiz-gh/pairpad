package hub

import (
	"context"
	"errors"
	"log/slog"
	"sync"
)

// ErrClosed is returned by Join after the manager has been closed.
var ErrClosed = errors.New("hub: manager closed")

// Manager owns the set of running hubs, creating one on a room's first join
// and forgetting it once it stops (when its last peer leaves).
type Manager struct {
	loader  Loader
	persist Persister
	log     *slog.Logger

	// ctx bounds hub startup work (flush + history load); cancelled on Close.
	ctx    context.Context
	cancel context.CancelFunc

	mu     sync.Mutex
	hubs   map[string]*Hub
	closed bool
	wg     sync.WaitGroup
}

// NewManager returns an empty manager.
func NewManager(loader Loader, persist Persister, log *slog.Logger) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{
		loader:  loader,
		persist: persist,
		log:     log,
		ctx:     ctx,
		cancel:  cancel,
		hubs:    make(map[string]*Hub),
	}
}

// Join adds c to roomID's hub, starting the hub if needed, and returns the
// hub plus the history c must replay before any live frames. The caller must
// have verified that the room exists.
func (m *Manager) Join(ctx context.Context, roomID string, c *Client) (*Hub, [][]byte, error) {
	for {
		h, err := m.getOrStart(roomID)
		if err != nil {
			return nil, nil, err
		}
		history, ok, err := h.tryJoin(ctx, c)
		if err != nil {
			return nil, nil, err
		}
		if ok {
			return h, history, nil
		}
		if h.err != nil {
			return nil, nil, h.err
		}
		// The hub emptied and stopped between lookup and join; it has
		// already removed itself from the map, so retrying starts a new one.
	}
}

func (m *Manager) getOrStart(roomID string) (*Hub, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, ErrClosed
	}
	if h, ok := m.hubs[roomID]; ok {
		return h, nil
	}
	h := newHub(roomID, m.loader, m.persist, m.log, m.forget)
	m.hubs[roomID] = h
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		h.run(m.ctx)
	}()
	return h, nil
}

// forget is called by a hub's goroutine just before it signals done.
func (m *Manager) forget(h *Hub) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.hubs[h.id] == h {
		delete(m.hubs, h.id)
	}
}

// Active reports how many hubs are running.
func (m *Manager) Active() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.hubs)
}

// Close stops every hub, disconnecting all peers, and waits for them to exit.
// Updates already relayed have been handed to the Persister by then.
func (m *Manager) Close() {
	m.mu.Lock()
	m.closed = true
	hubs := make([]*Hub, 0, len(m.hubs))
	for _, h := range m.hubs {
		hubs = append(hubs, h)
	}
	m.mu.Unlock()

	m.cancel()
	for _, h := range hubs {
		close(h.quit)
	}
	m.wg.Wait()
}
