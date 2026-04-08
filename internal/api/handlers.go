package api

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"strings"

	"nhooyr.io/websocket"

	"flip7/internal/hub"
)

// assetVersion computes an 8-hex-char hash of all embedded JS and CSS files.
// It changes whenever any asset changes, so versioned URLs bust browser caches.
func assetVersion(webFS fs.FS) string {
	hash := sha256.New()
	for _, path := range []string{"css/style.css", "js/sounds.js", "js/app.js"} {
		if data, err := fs.ReadFile(webFS, path); err == nil {
			hash.Write(data)
		}
	}
	return fmt.Sprintf("%x", hash.Sum(nil))[:8]
}

// NewRouter builds and returns the HTTP mux for the server.
// webFS is an fs.FS rooted at the directory containing index.html, game.html, etc.
func NewRouter(h *hub.Hub, webFS fs.FS) http.Handler {
	// Build a version string that changes whenever JS/CSS changes.
	ver := assetVersion(webFS)
	log.Printf("asset version: %s", ver)

	// Replacer that stamps ?v=VER onto every asset URL inside HTML files.
	replacer := strings.NewReplacer(
		`href="/css/style.css"`, fmt.Sprintf(`href="/css/style.css?v=%s"`, ver),
		`src="/js/sounds.js"`, fmt.Sprintf(`src="/js/sounds.js?v=%s"`, ver),
		`src="/js/app.js"`, fmt.Sprintf(`src="/js/app.js?v=%s"`, ver),
	)

	// serveHTML reads an HTML file, injects versioned asset URLs, and responds
	// with Cache-Control: no-cache so browsers always revalidate the HTML itself.
	serveHTML := func(w http.ResponseWriter, r *http.Request, name string) {
		data, err := fs.ReadFile(webFS, name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		content := replacer.Replace(string(data))
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		fmt.Fprint(w, content)
	}

	mux := http.NewServeMux()

	// Health check — used by the self-ping keepalive and external monitors.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("healthz ok (from %s)", r.RemoteAddr)
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, "ok")
	})

	// REST: create a new room and return its ID.
	mux.HandleFunc("POST /api/rooms", func(w http.ResponseWriter, r *http.Request) {
		roomID := h.NewRoomID()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"roomID": roomID})
	})

	// REST: check if a room exists and return its phase.
	mux.HandleFunc("GET /api/rooms/{roomID}", func(w http.ResponseWriter, r *http.Request) {
		roomID := r.PathValue("roomID")
		room := h.GetRoom(roomID)
		if room == nil {
			http.Error(w, "room not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"roomID": roomID, "phase": room.Phase()})
	})

	// WebSocket upgrade for an existing (or newly created) room.
	mux.HandleFunc("GET /ws/{roomID}", func(w http.ResponseWriter, r *http.Request) {
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

	// HTML pages — served with injected versioned asset URLs and no-cache.
	mux.HandleFunc("GET /game/{roomID}", func(w http.ResponseWriter, r *http.Request) {
		serveHTML(w, r, "game.html")
	})
	mux.HandleFunc("GET /rules.html", func(w http.ResponseWriter, r *http.Request) {
		serveHTML(w, r, "rules.html")
	})
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			serveHTML(w, r, "index.html")
			return
		}
		// JS and CSS: long-lived immutable cache (versioned URLs guarantee busting).
		if strings.HasPrefix(r.URL.Path, "/js/") || strings.HasPrefix(r.URL.Path, "/css/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		http.FileServerFS(webFS).ServeHTTP(w, r)
	})

	return mux
}
