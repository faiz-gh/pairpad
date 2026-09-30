package hub

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/faiz-gh/pairpad/server/internal/store"
)

type fakeLoader struct {
	mu      sync.Mutex
	updates map[string][][]byte
	loads   int
}

func (f *fakeLoader) LoadUpdates(_ context.Context, roomID string) ([][]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.loads++
	return append([][]byte(nil), f.updates[roomID]...), nil
}

type fakePersister struct {
	mu    sync.Mutex
	added []store.Update
}

func (f *fakePersister) Add(u store.Update) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.added = append(f.added, u)
}

func (f *fakePersister) Flush(context.Context) error { return nil }

func (f *fakePersister) snapshot() []store.Update {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]store.Update(nil), f.added...)
}

func newTestManager(history map[string][][]byte) (*Manager, *fakeLoader, *fakePersister) {
	l := &fakeLoader{updates: history}
	p := &fakePersister{}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewManager(l, p, log), l, p
}

func frame(update string) []byte {
	return append([]byte{MsgUpdate}, update...)
}

func join(t *testing.T, m *Manager, room string) (*Hub, *Client, [][]byte) {
	t.Helper()
	c := NewClient()
	h, history, err := m.Join(context.Background(), room, c)
	if err != nil {
		t.Fatalf("join: %v", err)
	}
	return h, c, history
}

func recv(t *testing.T, c *Client) []byte {
	t.Helper()
	select {
	case f, ok := <-c.Send():
		if !ok {
			t.Fatal("send channel closed")
		}
		return f
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for frame")
		return nil
	}
}

func assertNothing(t *testing.T, c *Client) {
	t.Helper()
	select {
	case f := <-c.Send():
		t.Fatalf("unexpected frame %q", f)
	case <-time.After(50 * time.Millisecond):
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestJoinReceivesStoredHistory(t *testing.T) {
	m, _, _ := newTestManager(map[string][][]byte{"room01": {[]byte("a"), []byte("b")}})
	defer m.Close()

	_, _, history := join(t, m, "room01")
	if len(history) != 2 || string(history[0]) != "a" || string(history[1]) != "b" {
		t.Fatalf("history = %q, want [a b]", history)
	}
}

func TestJoinReceivesLiveUpdatesInHistory(t *testing.T) {
	m, _, _ := newTestManager(map[string][][]byte{"room01": {[]byte("a")}})
	defer m.Close()

	h, alice, _ := join(t, m, "room01")
	h.Broadcast(alice, frame("b"))

	_, _, history := join(t, m, "room01")
	if len(history) != 2 || string(history[1]) != "b" {
		t.Fatalf("late joiner history = %q, want [a b]", history)
	}
}

func TestBroadcastExcludesSender(t *testing.T) {
	m, _, p := newTestManager(nil)
	defer m.Close()

	h, alice, _ := join(t, m, "room01")
	_, bob, _ := join(t, m, "room01")
	_, carol, _ := join(t, m, "room01")

	h.Broadcast(alice, frame("hello"))

	for _, c := range []*Client{bob, carol} {
		if got := recv(t, c); !bytes.Equal(got, frame("hello")) {
			t.Fatalf("got %q, want %q", got, frame("hello"))
		}
	}
	assertNothing(t, alice)

	got := p.snapshot()
	if len(got) != 1 || got[0].RoomID != "room01" || string(got[0].Data) != "hello" {
		t.Fatalf("persisted %+v, want one update 'hello' for room01", got)
	}
}

func TestBroadcastIsScopedToRoom(t *testing.T) {
	m, _, _ := newTestManager(nil)
	defer m.Close()

	h, alice, _ := join(t, m, "room01")
	_, other, _ := join(t, m, "room02")

	h.Broadcast(alice, frame("x"))
	assertNothing(t, other)
}

func TestLeaveStopsDeliveryAndClosesClient(t *testing.T) {
	m, _, _ := newTestManager(nil)
	defer m.Close()

	h, alice, _ := join(t, m, "room01")
	_, bob, _ := join(t, m, "room01")

	h.Leave(bob)
	if _, ok := <-bob.Send(); ok {
		t.Fatal("expected bob's channel to be closed after leave")
	}
	h.Broadcast(alice, frame("x")) // must not panic sending to bob
	if m.Active() != 1 {
		t.Fatalf("active hubs = %d, want 1 while alice remains", m.Active())
	}
}

func TestHubTornDownWhenEmptyAndRestartedOnJoin(t *testing.T) {
	m, l, _ := newTestManager(map[string][][]byte{"room01": {[]byte("a")}})
	defer m.Close()

	h, alice, _ := join(t, m, "room01")
	h.Leave(alice)

	select {
	case <-h.Done():
	case <-time.After(time.Second):
		t.Fatal("hub did not stop after last peer left")
	}
	waitFor(t, func() bool { return m.Active() == 0 })

	h2, _, history := join(t, m, "room01")
	if h2 == h {
		t.Fatal("expected a fresh hub after teardown")
	}
	if len(history) != 1 {
		t.Fatalf("history after restart = %q", history)
	}
	l.mu.Lock()
	loads := l.loads
	l.mu.Unlock()
	if loads != 2 {
		t.Fatalf("loads = %d, want 2 (one per hub start)", loads)
	}
}

func TestSlowPeerIsDropped(t *testing.T) {
	m, _, _ := newTestManager(nil)
	defer m.Close()

	h, alice, _ := join(t, m, "room01")
	_, bob, _ := join(t, m, "room01")

	// sendBuffer frames fill bob's queue and the next one overflows it.
	// Broadcast returns once the hub has *received* a frame, so one extra
	// broadcast acts as a barrier: the overflow has been processed before
	// we start draining.
	for range sendBuffer + 2 {
		h.Broadcast(alice, frame("x"))
	}
	n := 0
	for range bob.Send() {
		n++
	}
	if n != sendBuffer {
		t.Fatalf("bob received %d frames before being dropped, want %d", n, sendBuffer)
	}
}

func TestCloseDisconnectsPeersAndRejectsJoins(t *testing.T) {
	m, _, _ := newTestManager(nil)
	_, alice, _ := join(t, m, "room01")

	m.Close()
	if _, ok := <-alice.Send(); ok {
		t.Fatal("expected channel closed on manager close")
	}
	if _, _, err := m.Join(context.Background(), "room01", NewClient()); err != ErrClosed {
		t.Fatalf("join after close: err = %v, want ErrClosed", err)
	}
}
