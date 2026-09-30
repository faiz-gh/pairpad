// Package api exposes the REST and WebSocket endpoints.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/coder/websocket"

	"github.com/faiz-gh/pairpad/server/internal/hub"
	"github.com/faiz-gh/pairpad/server/internal/store"
)

// Rooms is the subset of the store the handlers need.
type Rooms interface {
	CreateRoom(ctx context.Context) (store.Room, error)
	GetRoom(ctx context.Context, id string) (store.Room, error)
}

// Server wires HTTP routes to the store and hub manager.
type Server struct {
	rooms          Rooms
	hubs           *hub.Manager
	allowedOrigins []string
	log            *slog.Logger
}

// New returns a Server. allowedOrigins are host patterns (see
// websocket.AcceptOptions.OriginPatterns); same-host origins are always allowed.
func New(rooms Rooms, hubs *hub.Manager, allowedOrigins []string, log *slog.Logger) *Server {
	return &Server{rooms: rooms, hubs: hubs, allowedOrigins: allowedOrigins, log: log}
}

// Routes returns the HTTP handler.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("POST /api/rooms", s.createRoom)
	mux.HandleFunc("GET /api/rooms/{id}", s.getRoom)
	mux.HandleFunc("GET /ws/{roomId}", s.websocket)
	return mux
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) createRoom(w http.ResponseWriter, r *http.Request) {
	room, err := s.rooms.CreateRoom(r.Context())
	if err != nil {
		s.log.Error("create room", "err", err)
		writeError(w, http.StatusInternalServerError, "could not create room")
		return
	}
	writeJSON(w, http.StatusCreated, room)
}

func (s *Server) getRoom(w http.ResponseWriter, r *http.Request) {
	room, ok := s.lookup(w, r, r.PathValue("id"))
	if ok {
		writeJSON(w, http.StatusOK, room)
	}
}

func (s *Server) websocket(w http.ResponseWriter, r *http.Request) {
	room, ok := s.lookup(w, r, r.PathValue("roomId"))
	if !ok {
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: s.allowedOrigins,
	})
	if err != nil {
		// Accept has already written the HTTP error (e.g. 403 bad origin).
		s.log.Debug("websocket accept", "err", err)
		return
	}
	s.hubs.Serve(r.Context(), conn, room.ID)
}

// lookup fetches a room, writing a 404/500 response and returning false if
// it can't.
func (s *Server) lookup(w http.ResponseWriter, r *http.Request, id string) (store.Room, bool) {
	if !store.ValidRoomID(id) {
		writeError(w, http.StatusNotFound, "room not found")
		return store.Room{}, false
	}
	room, err := s.rooms.GetRoom(r.Context(), id)
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
