package hub

import (
	"crypto/rand"
	"fmt"
	"sync"
)

// Hub manages all active game rooms.
type Hub struct {
	mu    sync.RWMutex
	rooms map[string]*Room
}

// New creates an empty Hub.
func New() *Hub {
	return &Hub{rooms: make(map[string]*Room)}
}

// GetOrCreateRoom returns the existing room with id, or creates a new one.
func (h *Hub) GetOrCreateRoom(id string) *Room {
	h.mu.Lock()
	defer h.mu.Unlock()

	if r, ok := h.rooms[id]; ok {
		return r
	}
	r := newRoom(id, h)
	h.rooms[id] = r
	return r
}

// NewRoomID creates a room with a fresh random 6-hex-char ID and returns it.
func (h *Hub) NewRoomID() string {
	h.mu.Lock()
	defer h.mu.Unlock()

	for {
		id := randomRoomID()
		if _, exists := h.rooms[id]; !exists {
			r := newRoom(id, h)
			h.rooms[id] = r
			return id
		}
	}
}

// randomRoomID returns a 6-character lowercase hex string.
func randomRoomID() string {
	var b [3]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("%02x%02x%02x", b[0], b[1], b[2])
}
