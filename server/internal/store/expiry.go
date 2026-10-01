package store

import (
	"context"
	"log/slog"
	"time"
)

// RoomDeleter deletes rooms idle since before cutoff. *Store implements it.
type RoomDeleter interface {
	DeleteExpiredRooms(ctx context.Context, cutoff time.Time, keep []string, limit int) (int64, error)
}

// expiryBatch bounds each DELETE so a large backlog doesn't hold locks or
// cascade millions of update rows in one statement.
const expiryBatch = 500

// Expirer periodically deletes rooms with no activity for TTL. Rooms that
// currently have connected peers (reported by Active) are never deleted, even
// if nobody has edited them for longer than TTL.
type Expirer struct {
	Deleter  RoomDeleter
	TTL      time.Duration
	Interval time.Duration
	Active   func() []string
	Log      *slog.Logger
	Now      func() time.Time // defaults to time.Now
}

// Run sweeps once immediately and then every Interval until ctx is done.
func (e *Expirer) Run(ctx context.Context) {
	ticker := time.NewTicker(e.Interval)
	defer ticker.Stop()
	for {
		if n, err := e.Sweep(ctx); err != nil {
			if ctx.Err() == nil {
				e.Log.Error("room expiry sweep failed", "err", err)
			}
		} else if n > 0 {
			e.Log.Info("expired inactive rooms", "rooms", n, "ttl", e.TTL.String())
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Sweep deletes every expired room in batches and returns the total.
func (e *Expirer) Sweep(ctx context.Context) (int64, error) {
	now := time.Now
	if e.Now != nil {
		now = e.Now
	}
	cutoff := now().Add(-e.TTL)
	var total int64
	for {
		n, err := e.Deleter.DeleteExpiredRooms(ctx, cutoff, e.Active(), expiryBatch)
		total += n
		if err != nil || n < expiryBatch {
			return total, err
		}
	}
}
