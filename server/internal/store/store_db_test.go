package store

// Integration tests against a real Postgres. Skipped unless TEST_DATABASE_URL
// is set, e.g. the compose database:
//
//	TEST_DATABASE_URL=postgres://pairpad:change-me@localhost:5432/pairpad go test ./internal/store/

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/faiz-gh/pairpad/server/migrations"
)

func testStore(t *testing.T) (*Store, *pgxpool.Pool) {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := Migrate(ctx, pool, migrations.FS, discard()); err != nil {
		t.Fatal(err)
	}
	return New(pool, discard()), pool
}

// newRoom creates a room and schedules its deletion.
func newRoom(t *testing.T, s *Store) Room {
	t.Helper()
	r, err := s.CreateRoom(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		s.pool.Exec(context.Background(), "DELETE FROM rooms WHERE id = $1", r.ID) //nolint:errcheck
	})
	return r
}

func age(t *testing.T, pool *pgxpool.Pool, id string, by time.Duration) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		"UPDATE rooms SET last_active_at = now() - $2::interval WHERE id = $1", id, by.String(),
	); err != nil {
		t.Fatal(err)
	}
}

func exists(t *testing.T, s *Store, id string) bool {
	t.Helper()
	_, err := s.GetRoom(context.Background(), id)
	if err != nil && !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	return err == nil
}

func TestDBRoomLifecycleAndReplayOrder(t *testing.T) {
	s, pool := testStore(t)
	ctx := context.Background()
	r := newRoom(t, s)
	if !ValidRoomID(r.ID) {
		t.Fatalf("invalid id %q", r.ID)
	}

	if err := s.InsertUpdates(ctx, []Update{{r.ID, []byte("u1")}, {r.ID, []byte("u2")}}); err != nil {
		t.Fatal(err)
	}
	// A snapshot row written later must still replay first.
	if _, err := pool.Exec(ctx,
		"INSERT INTO room_updates (room_id, data, is_snapshot) VALUES ($1, 'S', true)", r.ID); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadUpdates(ctx, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || string(got[0]) != "S" || string(got[1]) != "u1" || string(got[2]) != "u2" {
		t.Fatalf("replay = %q, want [S u1 u2]", got)
	}

	if _, err := s.TouchRoom(ctx, "zzzzzz"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("TouchRoom(missing) err = %v", err)
	}
}

func TestDBDeleteExpiredRooms(t *testing.T) {
	s, pool := testStore(t)
	ctx := context.Background()
	stale := newRoom(t, s)
	busy := newRoom(t, s) // stale, but has connected peers
	fresh := newRoom(t, s)
	age(t, pool, stale.ID, 40*24*time.Hour)
	age(t, pool, busy.ID, 40*24*time.Hour)
	if err := s.InsertUpdates(ctx, []Update{{stale.ID, []byte("x")}}); err != nil {
		t.Fatal(err)
	}
	age(t, pool, stale.ID, 40*24*time.Hour) // the insert bumped it

	cutoff := time.Now().Add(-30 * 24 * time.Hour)
	if _, err := s.DeleteExpiredRooms(ctx, cutoff, []string{busy.ID}, 1000); err != nil {
		t.Fatal(err)
	}
	if exists(t, s, stale.ID) {
		t.Error("stale room survived")
	}
	if !exists(t, s, busy.ID) {
		t.Error("room with active peers was deleted")
	}
	if !exists(t, s, fresh.ID) {
		t.Error("fresh room was deleted")
	}
	var n int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM room_updates WHERE room_id = $1", stale.ID).Scan(&n); err != nil || n != 0 {
		t.Fatalf("updates of deleted room: %d, %v (want cascade to 0)", n, err)
	}
}

func TestDBTouchSavesRoomFromExpiry(t *testing.T) {
	s, pool := testStore(t)
	ctx := context.Background()
	r := newRoom(t, s)
	age(t, pool, r.ID, 40*24*time.Hour)

	if _, err := s.TouchRoom(ctx, r.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DeleteExpiredRooms(ctx, time.Now().Add(-30*24*time.Hour), nil, 1000); err != nil {
		t.Fatal(err)
	}
	if !exists(t, s, r.ID) {
		t.Fatal("touched room was expired")
	}
}

func TestDBConcurrentTouchWinsOverDelete(t *testing.T) {
	s, pool := testStore(t)
	ctx := context.Background()
	r := newRoom(t, s)
	age(t, pool, r.ID, 40*24*time.Hour)

	// Hold a transaction that touches the room, start the DELETE (it blocks
	// on the row lock), then commit: the DELETE must re-check and skip it.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, "UPDATE rooms SET last_active_at = now() WHERE id = $1", r.ID); err != nil {
		t.Fatal(err)
	}
	deleted := make(chan int64, 1)
	go func() {
		n, err := s.DeleteExpiredRooms(ctx, time.Now().Add(-30*24*time.Hour), nil, 1000)
		if err != nil {
			t.Error(err)
		}
		deleted <- n
	}()
	time.Sleep(200 * time.Millisecond) // let the DELETE reach the lock
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	<-deleted
	if !exists(t, s, r.ID) {
		t.Fatal("room touched concurrently was deleted")
	}
}

func TestDBInsertDropsUpdatesForDeletedRooms(t *testing.T) {
	s, pool := testStore(t)
	ctx := context.Background()
	live := newRoom(t, s)
	gone := newRoom(t, s)
	if _, err := pool.Exec(ctx, "DELETE FROM rooms WHERE id = $1", gone.ID); err != nil {
		t.Fatal(err)
	}

	err := s.InsertUpdates(ctx, []Update{{live.ID, []byte("a")}, {gone.ID, []byte("b")}, {live.ID, []byte("c")}})
	if err != nil {
		t.Fatalf("InsertUpdates must not fail for a deleted room: %v", err)
	}
	got, err := s.LoadUpdates(ctx, live.ID)
	if err != nil || len(got) != 2 {
		t.Fatalf("live room updates = %q, %v", got, err)
	}
	if err := s.InsertUpdates(ctx, []Update{{gone.ID, []byte("only")}}); err != nil {
		t.Fatalf("batch of only deleted rooms: %v", err)
	}
}
