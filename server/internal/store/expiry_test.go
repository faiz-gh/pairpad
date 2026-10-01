package store

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"
)

type deleteCall struct {
	cutoff time.Time
	keep   []string
	limit  int
}

type fakeDeleter struct {
	mu      sync.Mutex
	calls   []deleteCall
	results []int64 // rows deleted per call; 0 once exhausted
	err     error
}

func (f *fakeDeleter) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeDeleter) DeleteExpiredRooms(_ context.Context, cutoff time.Time, keep []string, limit int) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, deleteCall{cutoff, keep, limit})
	if f.err != nil {
		return 0, f.err
	}
	if len(f.results) == 0 {
		return 0, nil
	}
	n := f.results[0]
	f.results = f.results[1:]
	return n, nil
}

func newExpirer(d RoomDeleter, active ...string) *Expirer {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	return &Expirer{
		Deleter:  d,
		TTL:      30 * 24 * time.Hour,
		Interval: time.Hour,
		Active:   func() []string { return active },
		Log:      discard(),
		Now:      func() time.Time { return now },
	}
}

func TestSweepUsesTTLCutoffAndSkipsActiveRooms(t *testing.T) {
	d := &fakeDeleter{results: []int64{3}}
	n, err := newExpirer(d, "live01", "live02").Sweep(context.Background())
	if err != nil || n != 3 {
		t.Fatalf("Sweep = %d, %v; want 3, nil", n, err)
	}
	want := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	if len(d.calls) != 1 || !d.calls[0].cutoff.Equal(want) {
		t.Fatalf("cutoff = %v, want %v", d.calls[0].cutoff, want)
	}
	if !slices.Equal(d.calls[0].keep, []string{"live01", "live02"}) {
		t.Fatalf("keep = %v", d.calls[0].keep)
	}
}

func TestSweepDeletesInBatchesUntilDone(t *testing.T) {
	d := &fakeDeleter{results: []int64{expiryBatch, expiryBatch, 7}}
	n, err := newExpirer(d).Sweep(context.Background())
	if err != nil || n != 2*expiryBatch+7 {
		t.Fatalf("Sweep = %d, %v", n, err)
	}
	if len(d.calls) != 3 {
		t.Fatalf("calls = %d, want 3 (stop after a short batch)", len(d.calls))
	}
	for _, c := range d.calls {
		if c.limit != expiryBatch {
			t.Fatalf("limit = %d", c.limit)
		}
	}
}

func TestSweepReturnsErrors(t *testing.T) {
	d := &fakeDeleter{err: errors.New("db down")}
	if _, err := newExpirer(d).Sweep(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

func TestRunSweepsImmediatelyAndStopsWithContext(t *testing.T) {
	d := &fakeDeleter{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	e := newExpirer(d)
	go func() {
		e.Run(ctx)
		close(done)
	}()
	eventually(t, time.Second, func() bool { return d.callCount() > 0 })
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not return after cancel")
	}
}
