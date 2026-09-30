package store

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"
)

type fakeWriter struct {
	mu      sync.Mutex
	batches [][]Update
	fail    int // number of upcoming calls that should fail
	block   chan struct{}
}

func (f *fakeWriter) InsertUpdates(_ context.Context, u []Update) error {
	if f.block != nil {
		<-f.block
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail > 0 {
		f.fail--
		return errors.New("db down")
	}
	f.batches = append(f.batches, append([]Update(nil), u...))
	return nil
}

func (f *fakeWriter) written() []Update {
	f.mu.Lock()
	defer f.mu.Unlock()
	var all []Update
	for _, b := range f.batches {
		all = append(all, b...)
	}
	return all
}

func (f *fakeWriter) batchCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.batches)
}

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func upd(i int) Update { return Update{RoomID: "room01", Data: []byte(fmt.Sprint(i))} }

func eventually(t *testing.T, within time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(within)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met in time")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestBatcherFlushesWhenBatchIsFull(t *testing.T) {
	w := &fakeWriter{}
	b := NewBatcher(w, time.Hour, 50, discard())
	defer b.Close(context.Background())

	for i := range 49 {
		b.Add(upd(i))
	}
	time.Sleep(50 * time.Millisecond)
	if n := len(w.written()); n != 0 {
		t.Fatalf("flushed %d updates before batch was full", n)
	}

	b.Add(upd(49))
	eventually(t, time.Second, func() bool { return len(w.written()) == 50 })
	if w.batchCount() != 1 {
		t.Fatalf("batches = %d, want 1", w.batchCount())
	}
}

func TestBatcherFlushesOnInterval(t *testing.T) {
	w := &fakeWriter{}
	b := NewBatcher(w, 30*time.Millisecond, 50, discard())
	defer b.Close(context.Background())

	b.Add(upd(1))
	b.Add(upd(2))
	eventually(t, time.Second, func() bool { return len(w.written()) == 2 })
}

func TestBatcherAddDoesNotBlockOnSlowWrite(t *testing.T) {
	w := &fakeWriter{block: make(chan struct{})}
	b := NewBatcher(w, 10*time.Millisecond, 1, discard())

	b.Add(upd(0)) // triggers a write that blocks
	time.Sleep(20 * time.Millisecond)

	done := make(chan struct{})
	go func() {
		for i := 1; i <= 100; i++ {
			b.Add(upd(i))
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Add blocked while a write was in flight")
	}

	close(w.block)
	if err := b.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := len(w.written()); n != 101 {
		t.Fatalf("written = %d, want 101", n)
	}
}

func TestBatcherRetriesFailedBatchInOrder(t *testing.T) {
	w := &fakeWriter{fail: 1}
	b := NewBatcher(w, 20*time.Millisecond, 50, discard())
	defer b.Close(context.Background())

	b.Add(upd(1))
	eventually(t, time.Second, func() bool {
		w.mu.Lock()
		defer w.mu.Unlock()
		return w.fail == 0
	})
	b.Add(upd(2))

	eventually(t, time.Second, func() bool { return len(w.written()) == 2 })
	got := w.written()
	if string(got[0].Data) != "1" || string(got[1].Data) != "2" {
		t.Fatalf("order = %q,%q, want 1,2", got[0].Data, got[1].Data)
	}
}

func TestBatcherCloseFlushesRemainder(t *testing.T) {
	w := &fakeWriter{}
	b := NewBatcher(w, time.Hour, 50, discard())

	for i := range 7 {
		b.Add(upd(i))
	}
	if err := b.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := len(w.written()); n != 7 {
		t.Fatalf("written = %d, want 7", n)
	}
	if b.Pending() != 0 {
		t.Fatalf("pending = %d after close", b.Pending())
	}
}

func TestBatcherCloseGivesUpWhenContextExpires(t *testing.T) {
	w := &fakeWriter{fail: 1 << 30}
	b := NewBatcher(w, time.Hour, 50, discard())
	b.Add(upd(1))

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := b.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("close err = %v, want deadline exceeded", err)
	}
}

func TestFlushWritesSynchronously(t *testing.T) {
	w := &fakeWriter{}
	b := NewBatcher(w, time.Hour, 50, discard())
	defer b.Close(context.Background())

	b.Add(upd(1))
	if err := b.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(w.written()) != 1 {
		t.Fatal("Flush returned before the update was written")
	}
}

func TestValidRoomID(t *testing.T) {
	id, err := newRoomID()
	if err != nil {
		t.Fatal(err)
	}
	if !ValidRoomID(id) {
		t.Fatalf("generated id %q is not valid", id)
	}
	for _, bad := range []string{"", "abc", "abcdefg", "ABCDEF", "abc10o", "../../x"} {
		if ValidRoomID(bad) {
			t.Errorf("ValidRoomID(%q) = true", bad)
		}
	}
}
