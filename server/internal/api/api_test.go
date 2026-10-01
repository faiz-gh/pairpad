package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/faiz-gh/pairpad/server/internal/ratelimit"
	"github.com/faiz-gh/pairpad/server/internal/store"
)

type fakeRooms struct{ created int }

func (f *fakeRooms) CreateRoom(context.Context) (store.Room, error) {
	f.created++
	return store.Room{ID: "abc234", CreatedAt: time.Now()}, nil
}
func (f *fakeRooms) GetRoom(context.Context, string) (store.Room, error) {
	return store.Room{}, store.ErrNotFound
}
func (f *fakeRooms) TouchRoom(context.Context, string) (store.Room, error) {
	return store.Room{}, store.ErrNotFound
}

func createFrom(h http.Handler, remote, xff string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "/api/rooms", nil)
	r.RemoteAddr = remote
	if xff != "" {
		r.Header.Set("X-Forwarded-For", xff)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestCreateRoomIsRateLimitedPerClient(t *testing.T) {
	rooms := &fakeRooms{}
	h := New(rooms, nil, Config{
		TrustedProxies: DefaultTrustedProxies,
		CreateLimiter:  ratelimit.New(60, 2),
	}, slog.New(slog.NewTextHandler(io.Discard, nil))).Routes()

	const caddy = "172.18.0.3:40000"
	for i := range 2 {
		if w := createFrom(h, caddy, "198.51.100.7"); w.Code != http.StatusCreated {
			t.Fatalf("request %d: status %d", i, w.Code)
		}
	}

	w := createFrom(h, caddy, "198.51.100.7")
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("3rd request: status %d, want 429", w.Code)
	}
	if got := w.Header().Get("Retry-After"); got != "60" {
		t.Fatalf("Retry-After = %q, want 60", got)
	}
	var body struct {
		RetryAfter int `json:"retryAfter"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil || body.RetryAfter != 60 {
		t.Fatalf("body retryAfter = %d, %v", body.RetryAfter, err)
	}

	// A spoofed left-hand X-Forwarded-For entry doesn't buy a fresh bucket...
	if w := createFrom(h, caddy, "203.0.113.1, 198.51.100.7"); w.Code != http.StatusTooManyRequests {
		t.Fatalf("spoofed XFF: status %d, want 429", w.Code)
	}
	// ...but a different real client is unaffected.
	if w := createFrom(h, caddy, "198.51.100.8"); w.Code != http.StatusCreated {
		t.Fatalf("other client: status %d", w.Code)
	}
	if rooms.created != 3 {
		t.Fatalf("rooms created = %d, want 3", rooms.created)
	}
}

func TestRoutesWithStaticDir(t *testing.T) {
	h := New(&fakeRooms{}, nil, Config{StaticDir: staticDir(t)},
		slog.New(slog.NewTextHandler(io.Discard, nil))).Routes()

	cases := []struct {
		target   string
		wantCode int
		wantBody string
	}{
		{"/", 200, "<html>app</html>"},
		{"/k7x2p9", 200, "<html>app</html>"},
		{"/healthz", 200, `"ok"`},
		{"/api/rooms/zzzzzz", 404, "room not found"},
		{"/api/nope", 404, `"not found"`},
		{"/ws/", 404, `"not found"`},
	}
	for _, tc := range cases {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", tc.target, nil))
		if w.Code != tc.wantCode || !strings.Contains(w.Body.String(), tc.wantBody) {
			t.Errorf("GET %s: %d %q, want %d containing %q", tc.target, w.Code, w.Body.String(), tc.wantCode, tc.wantBody)
		}
	}
}

func TestRoutesWithoutStaticDirServeNoApp(t *testing.T) {
	h := New(&fakeRooms{}, nil, Config{}, slog.New(slog.NewTextHandler(io.Discard, nil))).Routes()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/k7x2p9", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("GET /k7x2p9 without STATIC_DIR: %d, want 404", w.Code)
	}
}
