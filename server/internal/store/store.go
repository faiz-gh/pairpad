// Package store is the PostgreSQL access layer. Yjs updates are stored as
// opaque bytes; nothing here decodes them.
package store

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound is returned when a room does not exist.
var ErrNotFound = errors.New("store: not found")

const (
	// roomIDAlphabet omits look-alike characters (0/o, 1/l/i) so IDs survive
	// being read aloud or retyped.
	roomIDAlphabet = "23456789abcdefghjkmnpqrstuvwxyz"
	roomIDLength   = 6
)

// Room is a collaborative pad's metadata.
type Room struct {
	ID           string    `json:"id"`
	CreatedAt    time.Time `json:"createdAt"`
	LastActiveAt time.Time `json:"lastActiveAt"`
}

// Update is one opaque Yjs update belonging to a room.
type Update struct {
	RoomID string
	Data   []byte
}

// Store wraps a pgx connection pool.
type Store struct {
	pool *pgxpool.Pool
	log  *slog.Logger
}

// New returns a Store backed by pool.
func New(pool *pgxpool.Pool, log *slog.Logger) *Store {
	return &Store{pool: pool, log: log}
}

// ValidRoomID reports whether id has the shape of a generated room ID. It lets
// handlers reject junk before touching the database.
func ValidRoomID(id string) bool {
	if len(id) != roomIDLength {
		return false
	}
	for _, r := range id {
		if !containsRune(roomIDAlphabet, r) {
			return false
		}
	}
	return true
}

func containsRune(s string, r rune) bool {
	for _, c := range s {
		if c == r {
			return true
		}
	}
	return false
}

func newRoomID() (string, error) {
	max := big.NewInt(int64(len(roomIDAlphabet)))
	b := make([]byte, roomIDLength)
	for i := range b {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		b[i] = roomIDAlphabet[n.Int64()]
	}
	return string(b), nil
}

// CreateRoom inserts a room with a fresh random ID, retrying on collision.
func (s *Store) CreateRoom(ctx context.Context) (Room, error) {
	const attempts = 5
	for range attempts {
		id, err := newRoomID()
		if err != nil {
			return Room{}, err
		}
		var r Room
		err = s.pool.QueryRow(ctx, `
			INSERT INTO rooms (id) VALUES ($1)
			ON CONFLICT (id) DO NOTHING
			RETURNING id, created_at, last_active_at`, id,
		).Scan(&r.ID, &r.CreatedAt, &r.LastActiveAt)
		if errors.Is(err, pgx.ErrNoRows) {
			continue // collision
		}
		if err != nil {
			return Room{}, fmt.Errorf("insert room: %w", err)
		}
		return r, nil
	}
	return Room{}, fmt.Errorf("could not allocate a unique room id after %d attempts", attempts)
}

// GetRoom returns the room with the given ID or ErrNotFound.
func (s *Store) GetRoom(ctx context.Context, id string) (Room, error) {
	var r Room
	err := s.pool.QueryRow(ctx,
		"SELECT id, created_at, last_active_at FROM rooms WHERE id = $1", id,
	).Scan(&r.ID, &r.CreatedAt, &r.LastActiveAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Room{}, ErrNotFound
	}
	if err != nil {
		return Room{}, fmt.Errorf("get room: %w", err)
	}
	return r, nil
}

// TouchRoom marks a room as active now and returns it, or ErrNotFound. It is
// both the existence check and the activity bump for a joining peer; being a
// single UPDATE, it can't interleave with DeleteExpiredRooms (see there).
func (s *Store) TouchRoom(ctx context.Context, id string) (Room, error) {
	var r Room
	err := s.pool.QueryRow(ctx, `
		UPDATE rooms SET last_active_at = now() WHERE id = $1
		RETURNING id, created_at, last_active_at`, id,
	).Scan(&r.ID, &r.CreatedAt, &r.LastActiveAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Room{}, ErrNotFound
	}
	if err != nil {
		return Room{}, fmt.Errorf("touch room: %w", err)
	}
	return r, nil
}

// DeleteExpiredRooms deletes up to limit rooms whose last activity is before
// cutoff, skipping the IDs in keep (rooms with connected peers). Their
// updates go with them (ON DELETE CASCADE). It returns how many were deleted.
//
// The age condition is repeated on the outer DELETE on purpose: if a join or
// a write bumps last_active_at concurrently, Postgres re-evaluates the outer
// WHERE against the new row version and skips the room.
func (s *Store) DeleteExpiredRooms(ctx context.Context, cutoff time.Time, keep []string, limit int) (int64, error) {
	if keep == nil {
		keep = []string{}
	}
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM rooms
		WHERE last_active_at < $1
		  AND id IN (
			SELECT id FROM rooms
			WHERE last_active_at < $1 AND NOT (id = ANY($2))
			ORDER BY last_active_at
			LIMIT $3
		  )`, cutoff, keep, limit)
	if err != nil {
		return 0, fmt.Errorf("delete expired rooms: %w", err)
	}
	return tag.RowsAffected(), nil
}

// LoadUpdates returns every stored update for a room: snapshots first, then
// the remaining updates in insertion order. Yjs merges updates in any order,
// but replaying the snapshot first avoids buffering updates that depend on it.
func (s *Store) LoadUpdates(ctx context.Context, roomID string) ([][]byte, error) {
	rows, err := s.pool.Query(ctx,
		"SELECT data FROM room_updates WHERE room_id = $1 ORDER BY is_snapshot DESC, id", roomID)
	if err != nil {
		return nil, fmt.Errorf("load updates: %w", err)
	}
	updates, err := pgx.CollectRows(rows, pgx.RowTo[[]byte])
	if err != nil {
		return nil, fmt.Errorf("load updates: %w", err)
	}
	return updates, nil
}

// InsertUpdates writes a batch of updates in one transaction and bumps the
// affected rooms' last_active_at. Order within the batch is preserved.
//
// Updates for rooms that no longer exist (expired or deleted meanwhile) are
// dropped and logged rather than failing the batch. A foreign-key error
// would otherwise make the batcher retry the same batch forever and stall
// persistence for every room. The surviving rooms are locked FOR KEY SHARE
// so they can't be deleted before the transaction commits.
func (s *Store) InsertUpdates(ctx context.Context, updates []Update) error {
	if len(updates) == 0 {
		return nil
	}
	seen := make(map[string]struct{})
	roomIDs := make([]string, 0, 1)
	for _, u := range updates {
		if _, ok := seen[u.RoomID]; !ok {
			seen[u.RoomID] = struct{}{}
			roomIDs = append(roomIDs, u.RoomID)
		}
	}

	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx,
			"SELECT id FROM rooms WHERE id = ANY($1) FOR KEY SHARE", roomIDs)
		if err != nil {
			return fmt.Errorf("lock rooms: %w", err)
		}
		live, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return fmt.Errorf("lock rooms: %w", err)
		}
		if len(live) < len(roomIDs) {
			alive := make(map[string]struct{}, len(live))
			for _, id := range live {
				alive[id] = struct{}{}
			}
			kept := updates[:0:0]
			for _, u := range updates {
				if _, ok := alive[u.RoomID]; ok {
					kept = append(kept, u)
				}
			}
			s.log.Warn("dropping updates for deleted rooms",
				"updates", len(updates)-len(kept), "rooms", len(roomIDs)-len(live))
			updates = kept
			if len(updates) == 0 {
				return nil
			}
		}

		if _, err := tx.CopyFrom(ctx,
			pgx.Identifier{"room_updates"},
			[]string{"room_id", "data"},
			pgx.CopyFromSlice(len(updates), func(i int) ([]any, error) {
				return []any{updates[i].RoomID, updates[i].Data}, nil
			}),
		); err != nil {
			return fmt.Errorf("copy updates: %w", err)
		}
		if _, err := tx.Exec(ctx,
			"UPDATE rooms SET last_active_at = now() WHERE id = ANY($1)", live,
		); err != nil {
			return fmt.Errorf("touch rooms: %w", err)
		}
		return nil
	})
}
