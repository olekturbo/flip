package api

import (
	"context"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"

	"nhooyr.io/websocket"

	"flip7/internal/hub"
)

// NewRouter builds and returns the HTTP mux for the server.
// webFS is an fs.FS rooted at the directory containing index.html, game.html, etc.
func NewRouter(h *hub.Hub, webFS fs.FS) http.Handler {
	mux := http.NewServeMux()

	// REST: create a new room and return its ID.
	mux.HandleFunc("POST /api/rooms", func(w http.ResponseWriter, r *http.Request) {
		roomID := h.NewRoomID()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"roomID": roomID})
	})

	// WebSocket upgrade for an existing (or newly created) room.
	mux.HandleFunc("/ws/{roomID}", func(w http.ResponseWriter, r *http.Request) {
		roomID := r.PathValue("roomID")
		if roomID == "" {
			http.Error(w, "missing room ID", http.StatusBadRequest)
			return
		}

		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			OriginPatterns:  []string{"*"},
			CompressionMode: websocket.CompressionDisabled, // Safari drops immediately with permessage-deflate
		})
		if err != nil {
			log.Printf("ws accept error room=%s: %v", roomID, err)
			return
		}

		log.Printf("ws connected room=%s remote=%s", roomID, r.RemoteAddr)

		// Use context.Background() — NOT r.Context().
		// nhooyr.io/websocket sets TCP deadlines from the context; using the HTTP
		// request context can cause the deadline to fire when the HTTP layer
		// internally cancels the context, dropping the connection immediately.
		// The connection's own heartbeat goroutine (in HandleConnection) handles
		// dead-connection detection instead.
		room := h.GetOrCreateRoom(roomID)
		room.HandleConnection(conn, context.Background())

		log.Printf("ws disconnected room=%s remote=%s", roomID, r.RemoteAddr)
	})

	// Serve game.html for any /game/{roomID} path.
	mux.HandleFunc("GET /game/{roomID}", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, webFS, "game.html")
	})

	// Serve static assets (index.html, css/, js/).
	mux.Handle("/", http.FileServerFS(webFS))

	return mux
}
