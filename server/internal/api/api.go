// Package api exposes the REST and WebSocket endpoints.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"net/netip"
	"strconv"

	"github.com/coder/websocket"

	"github.com/faiz-gh/pairpad/server/internal/hub"
	"github.com/faiz-gh/pairpad/server/internal/ratelimit"
	"github.com/faiz-gh/pairpad/server/internal/store"
)

// Rooms is the subset of the store the handlers need.
type Rooms interface {
	CreateRoom(ctx context.Context) (store.Room, error)
	GetRoom(ctx context.Context, id string) (store.Room, error)
	TouchRoom(ctx context.Context, id string) (store.Room, error)
}

// Config holds the handler settings.
type Config struct {
	// AllowedOrigins are WebSocket Origin host patterns (see
	// websocket.AcceptOptions.OriginPatterns); same-host is always allowed.
	AllowedOrigins []string
	// TrustedProxies may set X-Forwarded-For (see clientIP).
	TrustedProxies []netip.Prefix
	// CreateLimiter rate-limits POST /api/rooms per client IP; nil disables.
	CreateLimiter *ratelimit.Limiter
	// StaticDir, if set, is the built web app, served for every path not
	// under /api/ or /ws/ (see Static). Empty when a separate proxy (Caddy in
	// local development) serves the frontend.
	StaticDir string
}

// Server wires HTTP routes to the store and hub manager.
type Server struct {
	rooms Rooms
	hubs  *hub.Manager
	cfg   Config
	log   *slog.Logger
}

// New returns a Server.
func New(rooms Rooms, hubs *hub.Manager, cfg Config, log *slog.Logger) *Server {
	return &Server{rooms: rooms, hubs: hubs, cfg: cfg, log: log}
}

// Routes returns the HTTP handler.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("POST /api/rooms", s.createRoom)
	mux.HandleFunc("GET /api/rooms/{id}", s.getRoom)
	mux.HandleFunc("GET /ws/{roomId}", s.websocket)
	if s.cfg.StaticDir != "" {
		mux.Handle("GET /", Static(s.cfg.StaticDir))
		// Unknown API paths stay JSON 404s instead of falling through to
		// the app's index.html.
		notFound := func(w http.ResponseWriter, _ *http.Request) {
			writeError(w, http.StatusNotFound, "not found")
		}
		mux.HandleFunc("GET /api/", notFound)
		mux.HandleFunc("GET /ws/", notFound)
	}
	return mux
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) createRoom(w http.ResponseWriter, r *http.Request) {
	if s.cfg.CreateLimiter != nil {
		ip := clientIP(r, s.cfg.TrustedProxies)
		if ok, wait := s.cfg.CreateLimiter.Allow(ip); !ok {
			secs := int(math.Ceil(wait.Seconds()))
			s.log.Info("room creation rate limited", "ip", ip, "retryAfter", secs)
			w.Header().Set("Retry-After", strconv.Itoa(secs))
			writeJSON(w, http.StatusTooManyRequests, map[string]any{
				"error":      "too many new pads; try again later",
				"retryAfter": secs,
			})
			return
		}
	}
	room, err := s.rooms.CreateRoom(r.Context())
	if err != nil {
		s.log.Error("create room", "err", err)
		writeError(w, http.StatusInternalServerError, "could not create room")
		return
	}
	writeJSON(w, http.StatusCreated, room)
}

func (s *Server) getRoom(w http.ResponseWriter, r *http.Request) {
	room, ok := s.lookup(w, r, r.PathValue("id"), s.rooms.GetRoom)
	if ok {
		writeJSON(w, http.StatusOK, room)
	}
}

func (s *Server) websocket(w http.ResponseWriter, r *http.Request) {
	// Joining counts as activity, so an open-but-idle pad isn't expired.
	room, ok := s.lookup(w, r, r.PathValue("roomId"), s.rooms.TouchRoom)
	if !ok {
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: s.cfg.AllowedOrigins,
	})
	if err != nil {
		// Accept has already written the HTTP error (e.g. 403 bad origin).
		s.log.Debug("websocket accept", "err", err)
		return
	}
	s.hubs.Serve(r.Context(), conn, room.ID)
}

// lookup fetches a room with get, writing a 404/500 response and returning
// false if it can't.
func (s *Server) lookup(
	w http.ResponseWriter, r *http.Request, id string,
	get func(context.Context, string) (store.Room, error),
) (store.Room, bool) {
	if !store.ValidRoomID(id) {
		writeError(w, http.StatusNotFound, "room not found")
		return store.Room{}, false
	}
	room, err := get(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "room not found")
		return store.Room{}, false
	}
	if err != nil {
		s.log.Error("get room", "room", id, "err", err)
		writeError(w, http.StatusInternalServerError, "could not load room")
		return store.Room{}, false
	}
	return room, true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
