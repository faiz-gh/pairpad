// Package store is the PostgreSQL access layer. Yjs updates are stored as
// opaque bytes; nothing here decodes them.
package store

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
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
}

// New returns a Store backed by pool.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
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

// LoadUpdates returns every stored update for a room in insertion order.
func (s *Store) LoadUpdates(ctx context.Context, roomID string) ([][]byte, error) {
	rows, err := s.pool.Query(ctx,
		"SELECT data FROM room_updates WHERE room_id = $1 ORDER BY id", roomID)
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
			"UPDATE rooms SET last_active_at = now() WHERE id = ANY($1)", roomIDs,
		); err != nil {
			return fmt.Errorf("touch rooms: %w", err)
		}
		return nil
	})
}
