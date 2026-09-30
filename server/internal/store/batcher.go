package store

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Writer persists a batch of updates. *Store implements it.
type Writer interface {
	InsertUpdates(ctx context.Context, updates []Update) error
}

// writeTimeout bounds a single batch write so a hung database can't wedge the
// flusher forever.
const writeTimeout = 10 * time.Second

// Batcher buffers updates in memory and writes them to a Writer every
// interval, or as soon as maxBatch updates are buffered, whichever comes
// first. Add never touches the database, so callers on the relay path are
// never blocked by a slow write.
type Batcher struct {
	w        Writer
	interval time.Duration
	maxBatch int
	log      *slog.Logger

	mu  sync.Mutex
	buf []Update

	// flushMu serialises flushes so Flush callers observe every write that
	// was in flight when they started, including a failed one being requeued.
	flushMu sync.Mutex

	kick      chan struct{}
	stop      chan struct{}
	done      chan struct{}
	closeOnce sync.Once
}

// NewBatcher starts a Batcher's flush loop. Call Close to stop it.
func NewBatcher(w Writer, interval time.Duration, maxBatch int, log *slog.Logger) *Batcher {
	b := &Batcher{
		w:        w,
		interval: interval,
		maxBatch: maxBatch,
		log:      log,
		kick:     make(chan struct{}, 1),
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
	go b.run()
	return b
}

// Add queues an update for persistence. It never blocks on I/O.
func (b *Batcher) Add(u Update) {
	b.mu.Lock()
	b.buf = append(b.buf, u)
	full := len(b.buf) >= b.maxBatch
	b.mu.Unlock()

	if full {
		select {
		case b.kick <- struct{}{}:
		default: // a flush is already requested
		}
	}
}

// Pending reports how many updates are buffered and not yet written.
func (b *Batcher) Pending() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.buf)
}

// Flush synchronously writes everything buffered so far.
func (b *Batcher) Flush(ctx context.Context) error {
	return b.flush(ctx)
}

func (b *Batcher) run() {
	defer close(b.done)
	ticker := time.NewTicker(b.interval)
	defer ticker.Stop()

	// After a failed write, size-triggered kicks are ignored until the next
	// tick so a database outage doesn't turn every Add into a retry.
	failing := false
	for {
		select {
		case <-ticker.C:
			failing = b.flush(context.Background()) != nil
		case <-b.kick:
			if !failing {
				failing = b.flush(context.Background()) != nil
			}
		case <-b.stop:
			return
		}
	}
}

func (b *Batcher) flush(ctx context.Context) error {
	b.flushMu.Lock()
	defer b.flushMu.Unlock()

	b.mu.Lock()
	batch := b.buf
	b.buf = nil
	b.mu.Unlock()
	if len(batch) == 0 {
		return nil
	}

	wctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	if err := b.w.InsertUpdates(wctx, batch); err != nil {
		// Put the batch back in front of anything added meanwhile so
		// ordering is preserved on retry.
		b.mu.Lock()
		b.buf = append(batch, b.buf...)
		b.mu.Unlock()
		b.log.Error("persist updates failed; will retry", "count", len(batch), "err", err)
		return err
	}
	return nil
}

// Close stops the flush loop and writes any remaining updates, retrying until
// they are persisted or ctx expires.
func (b *Batcher) Close(ctx context.Context) error {
	b.closeOnce.Do(func() { close(b.stop) })
	<-b.done

	for {
		err := b.flush(ctx)
		if err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			b.log.Error("dropping unpersisted updates on shutdown", "count", b.Pending())
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}
