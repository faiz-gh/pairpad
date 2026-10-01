package hub

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/faiz-gh/pairpad/server/internal/store"
)

type fakeLoader struct {
	mu      sync.Mutex
	updates map[string][][]byte
	loads   int
	err     error
}

func (f *fakeLoader) LoadUpdates(_ context.Context, roomID string) ([][]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.loads++
	if f.err != nil {
		return nil, f.err
	}
	return append([][]byte(nil), f.updates[roomID]...), nil
}

// set replaces what the "database" holds, e.g. to simulate compaction.
func (f *fakeLoader) set(roomID string, updates [][]byte, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.updates == nil {
		f.updates = make(map[string][][]byte)
	}
	f.updates[roomID] = updates
	f.err = err
}

func (f *fakeLoader) loadCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.loads
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
	return newTestManagerWith(history, Options{})
}

func newTestManagerWith(history map[string][][]byte, opts Options) (*Manager, *fakeLoader, *fakePersister) {
	l := &fakeLoader{updates: history}
	p := &fakePersister{}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewManager(l, p, log, opts), l, p
}

func frame(update string) []byte {
	return append([]byte{MsgUpdate}, update...)
}

func awareness(state string) []byte {
	return append([]byte{MsgAwareness}, state...)
}

func join(t *testing.T, m *Manager, room string) (*Hub, *Client, [][]byte) {
	t.Helper()
	h, c, snap := joinSnap(t, m, room)
	return h, c, snap.History
}

func joinSnap(t *testing.T, m *Manager, room string) (*Hub, *Client, Snapshot) {
	t.Helper()
	c := NewClient()
	h, snap, err := m.Join(context.Background(), room, c)
	if err != nil {
		t.Fatalf("join: %v", err)
	}
	return h, c, snap
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
	if loads := l.loadCount(); loads != 2 {
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

func historyOf(t *testing.T, m *Manager, room string) []string {
	t.Helper()
	h, probe, history := join(t, m, room)
	defer h.Leave(probe)
	out := make([]string, len(history))
	for i, u := range history {
		out[i] = string(u)
	}
	return out
}

func TestHistoryRefreshPicksUpCompactedLog(t *testing.T) {
	m, l, _ := newTestManagerWith(nil, Options{HistoryRefreshMin: 4})
	defer m.Close()

	h, alice, _ := join(t, m, "room01")
	for _, u := range []string{"u1", "u2", "u3"} {
		h.Broadcast(alice, frame(u))
	}
	// The compactor merges u1..u4 while the room is live.
	l.set("room01", [][]byte{[]byte("S")}, nil)
	h.Broadcast(alice, frame("u4")) // reaches HistoryRefreshMin: triggers reload

	waitFor(t, func() bool { return historyOf(t, m, "room01")[0] == "S" })

	h.Broadcast(alice, frame("u5"))
	got := historyOf(t, m, "room01")
	if len(got) != 2 || got[0] != "S" || got[1] != "u5" {
		t.Fatalf("history = %q, want [S u5]", got)
	}
}

func TestHistoryRefreshFailureKeepsLog(t *testing.T) {
	m, l, _ := newTestManagerWith(nil, Options{HistoryRefreshMin: 2})
	defer m.Close()

	h, alice, _ := join(t, m, "room01")
	l.set("room01", nil, errors.New("db down"))
	h.Broadcast(alice, frame("u1"))
	h.Broadcast(alice, frame("u2")) // triggers a reload that fails

	waitFor(t, func() bool { return l.loadCount() >= 2 })
	h.Broadcast(alice, frame("u3")) // processed after the failed result or alongside it

	got := historyOf(t, m, "room01")
	if len(got) != 3 || got[0] != "u1" || got[2] != "u3" {
		t.Fatalf("history = %q, want [u1 u2 u3]", got)
	}
}

func TestAwarenessRelayedButNotPersisted(t *testing.T) {
	m, _, p := newTestManager(nil)
	defer m.Close()

	h, alice, _ := join(t, m, "room01")
	_, bob, _ := join(t, m, "room01")

	h.Broadcast(alice, awareness("alice@1"))
	if got := recv(t, bob); !bytes.Equal(got, awareness("alice@1")) {
		t.Fatalf("bob got %q", got)
	}
	assertNothing(t, alice)

	if n := len(p.snapshot()); n != 0 {
		t.Fatalf("persisted %d updates; awareness must not be persisted", n)
	}
	if got := historyOf(t, m, "room01"); len(got) != 0 {
		t.Fatalf("history = %q; awareness must not enter the history", got)
	}
}

func TestJoinerReceivesLatestPresenceOfOthers(t *testing.T) {
	m, _, _ := newTestManager(nil)
	defer m.Close()

	h, alice, _ := join(t, m, "room01")
	_, bob, _ := join(t, m, "room01")
	h.Broadcast(alice, awareness("alice@1"))
	h.Broadcast(alice, awareness("alice@2")) // only the latest is kept
	h.Broadcast(bob, awareness("bob@1"))

	_, _, snap := joinSnap(t, m, "room01")
	got := make([]string, len(snap.Presence))
	for i, f := range snap.Presence {
		got[i] = string(f[1:])
	}
	slices.Sort(got)
	if !slices.Equal(got, []string{"alice@2", "bob@1"}) {
		t.Fatalf("presence = %q, want [alice@2 bob@1]", got)
	}
}

func TestLeaveBroadcastsPeerLeftWithLastPresence(t *testing.T) {
	m, _, _ := newTestManager(nil)
	defer m.Close()

	h, alice, _ := join(t, m, "room01")
	_, bob, _ := join(t, m, "room01")
	h.Broadcast(bob, awareness("bob@3"))
	recv(t, alice) // bob's awareness

	h.Leave(bob)
	want := append([]byte{MsgPeerLeft}, "bob@3"...)
	if got := recv(t, alice); !bytes.Equal(got, want) {
		t.Fatalf("alice got %q, want %q", got, want)
	}

	_, _, snap := joinSnap(t, m, "room01")
	if len(snap.Presence) != 0 {
		t.Fatalf("departed peer's presence still replayed: %q", snap.Presence)
	}
}

func TestLeaveWithoutPresenceSendsNothing(t *testing.T) {
	m, _, _ := newTestManager(nil)
	defer m.Close()

	h, alice, _ := join(t, m, "room01")
	_, bob, _ := join(t, m, "room01")
	h.Leave(bob)
	assertNothing(t, alice)
}

func TestOversizedAwarenessIsIgnored(t *testing.T) {
	m, _, _ := newTestManager(nil)
	defer m.Close()

	h, alice, _ := join(t, m, "room01")
	_, bob, _ := join(t, m, "room01")
	h.Broadcast(alice, awareness(string(make([]byte, MaxAwarenessSize))))
	assertNothing(t, bob)
}

func TestRoomFullRejectsJoinUntilSomeoneLeaves(t *testing.T) {
	m, _, _ := newTestManagerWith(nil, Options{MaxPeers: 2})
	defer m.Close()

	h, alice, _ := join(t, m, "room01")
	join(t, m, "room01")
	if _, _, err := m.Join(context.Background(), "room01", NewClient()); !errors.Is(err, ErrRoomFull) {
		t.Fatalf("third join err = %v, want ErrRoomFull", err)
	}
	h.Leave(alice)
	if _, _, err := m.Join(context.Background(), "room01", NewClient()); err != nil {
		t.Fatalf("join after a peer left: %v", err)
	}
}

func TestUpdateOverHistoryLimitIsRejectedAndSenderDropped(t *testing.T) {
	m, _, p := newTestManagerWith(
		map[string][][]byte{"room01": {make([]byte, 60)}},
		Options{MaxHistoryBytes: 100},
	)
	defer m.Close()

	h, alice, _ := join(t, m, "room01")
	_, bob, _ := join(t, m, "room01")

	h.Broadcast(alice, frame(string(make([]byte, 30)))) // 90 bytes: fine
	recv(t, bob)
	h.Broadcast(alice, frame(string(make([]byte, 20)))) // 110 bytes: over

	// Alice is disconnected with the dedicated close code...
	for range alice.Send() {
	}
	if alice.closeCode != StatusDocTooLarge {
		t.Fatalf("alice close code = %d, want %d", alice.closeCode, StatusDocTooLarge)
	}
	// ...and the oversized update went nowhere.
	assertNothing(t, bob)
	if n := len(p.snapshot()); n != 1 {
		t.Fatalf("persisted %d updates, want 1 (the accepted one)", n)
	}
	if got := historyOf(t, m, "room01"); len(got) != 2 {
		t.Fatalf("history has %d entries, want 2", len(got))
	}
}

func TestDroppedPeersGetGoingAway(t *testing.T) {
	m, _, _ := newTestManager(nil)
	_, alice, _ := join(t, m, "room01")
	m.Close()
	for range alice.Send() {
	}
	if alice.closeCode != 1001 {
		t.Fatalf("close code on shutdown = %d, want 1001", alice.closeCode)
	}
}
