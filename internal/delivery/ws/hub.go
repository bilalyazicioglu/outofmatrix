// Package ws implements the WebSocket delivery layer: a hub that fans
// real-time media-processing events out to each user's connected clients.
package ws

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"outofmatrix/internal/usecase"
)

// Hub tracks connected clients grouped by user and broadcasts MediaEvents to
// the owning user's connections. It implements usecase.Notifier.
type Hub struct {
	log *slog.Logger

	mu      sync.RWMutex
	clients map[uuid.UUID]map[*Client]struct{}
}

var _ usecase.Notifier = (*Hub)(nil)

func NewHub(log *slog.Logger) *Hub {
	if log == nil {
		log = slog.Default()
	}
	return &Hub{
		log:     log,
		clients: make(map[uuid.UUID]map[*Client]struct{}),
	}
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// Connections are authenticated with a JWT (header or ?token=) before the
	// upgrade, so cross-origin pages without a token gain nothing here.
	CheckOrigin: func(*http.Request) bool { return true },
}

// ServeWS upgrades an authenticated HTTP request to a WebSocket connection
// and registers it with the hub. The caller must have validated the user.
func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request, userID uuid.UUID) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		// Upgrade already wrote an HTTP error response.
		h.log.Warn("websocket upgrade failed", "error", err, "remote", r.RemoteAddr)
		return
	}

	client := &Client{
		hub:    h,
		conn:   conn,
		userID: userID,
		send:   make(chan []byte, 64),
	}
	h.register(client)
	h.log.Info("websocket connected", "user_id", userID, "remote", r.RemoteAddr)

	go client.writePump()
	go client.readPump()
}

// NotifyMedia implements usecase.Notifier: it fans the event out to every
// connection the owning user has open. Slow consumers are skipped rather
// than allowed to stall the media pipeline.
func (h *Hub) NotifyMedia(userID uuid.UUID, evt usecase.MediaEvent) {
	payload, err := json.Marshal(evt)
	if err != nil {
		h.log.Error("marshal media event", "error", err)
		return
	}

	h.mu.RLock()
	defer h.mu.RUnlock()
	for client := range h.clients[userID] {
		select {
		case client.send <- payload:
		default:
			h.log.Warn("dropping event for slow websocket client", "user_id", userID)
		}
	}
}

func (h *Hub) register(c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	set, ok := h.clients[c.userID]
	if !ok {
		set = make(map[*Client]struct{})
		h.clients[c.userID] = set
	}
	set[c] = struct{}{}
}

func (h *Hub) unregister(c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if set, ok := h.clients[c.userID]; ok {
		if _, present := set[c]; present {
			delete(set, c)
			close(c.send)
			if len(set) == 0 {
				delete(h.clients, c.userID)
			}
		}
	}
}
