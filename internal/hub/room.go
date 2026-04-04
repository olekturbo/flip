package hub

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"nhooyr.io/websocket"

	"flip7/internal/game"
)

// ClientMessage is a message sent from the browser to the server.
type ClientMessage struct {
	Action    string `json:"action"`
	Name      string `json:"name"`
	SessionID string `json:"sessionID"`
	TargetID  string `json:"targetID"`  // for "target" action
	CardValue int    `json:"cardValue"` // for "steal" action (Thief card)
}

// Client wraps a WebSocket connection with a per-connection write lock.
type Client struct {
	writeMu   sync.Mutex
	conn      *websocket.Conn
	sessionID string
	playerID  string
}

func (c *Client) writeJSON(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return c.conn.Write(ctx, websocket.MessageText, data)
}

// Room manages the WebSocket clients and game state for one room.
type Room struct {
	mu      sync.RWMutex
	id      string
	game    *game.Game
	clients map[string]*Client // sessionID → client
	hub     *Hub
}

func newRoom(id string, h *Hub) *Room {
	r := &Room{
		id:      id,
		game:    game.New(id),
		clients: make(map[string]*Client),
		hub:     h,
	}
	go r.ticker()
	return r
}

// HandleConnection manages the full lifecycle of one WebSocket connection.
func (r *Room) HandleConnection(conn *websocket.Conn, reqCtx context.Context) {
	// Use a cancellable child context so the heartbeat goroutine can shut down
	// the read loop when a ping fails (dead connection).
	ctx, cancel := context.WithCancel(reqCtx)
	defer cancel()

	// Increase the read limit to accommodate larger state messages.
	conn.SetReadLimit(1 << 20) // 1 MiB

	// ── Step 1: expect a "join" message ─────────────────────────────────────
	_, raw, err := conn.Read(ctx)
	if err != nil {
		log.Printf("ws read(join) error: %v", err)
		conn.Close(websocket.StatusBadGateway, "read error")
		return
	}
	var msg ClientMessage
	if err := json.Unmarshal(raw, &msg); err != nil || msg.Action != "join" {
		log.Printf("ws bad join message: raw=%q err=%v", raw, err)
		conn.Close(websocket.StatusPolicyViolation, "expected join action")
		return
	}

	// ── Step 2: resolve player (rejoin or new) ──────────────────────────────
	var player *game.Player
	sessionID := msg.SessionID

	if sessionID != "" {
		if p, ok := r.game.Rejoin(sessionID); ok {
			player = p
		}
	}

	if player == nil {
		sessionID = newSessionID()
		name := msg.Name
		if name == "" {
			name = "Player"
		}
		p, err := r.game.AddPlayer(sessionID, name)
		if err != nil {
			log.Printf("ws AddPlayer error room=%s name=%q: %v", r.id, name, err)
			c := &Client{conn: conn}
			_ = c.writeJSON(map[string]any{"type": "error", "message": err.Error()})
			conn.Close(websocket.StatusNormalClosure, "join failed")
			return
		}
		player = p
	}
	log.Printf("ws joined room=%s player=%q session=%s", r.id, player.Name, sessionID[:8])

	// ── Step 3: register client ─────────────────────────────────────────────
	client := &Client{conn: conn, sessionID: sessionID, playerID: player.ID}
	r.addClient(client)
	defer func() {
		// Only remove this specific client — a newer connection may have already
		// replaced it in the map (mobile reconnect race condition).
		r.removeClient(sessionID, client)
		// Only mark disconnected if no replacement client is active.
		r.mu.RLock()
		_, stillConnected := r.clients[sessionID]
		r.mu.RUnlock()
		if !stillConnected {
			r.game.Disconnect(sessionID)
			r.broadcastState() // notify others of disconnection
		}
	}()

	// ── Step 4: send private join confirmation ──────────────────────────────
	_ = client.writeJSON(map[string]any{
		"type":       "joined",
		"sessionID":  sessionID,
		"playerID":   player.ID,
		"playerName": player.Name,
		"isHost":     player.IsHost,
		"roomID":     r.id,
	})

	// ── Step 5: broadcast current state to everyone ─────────────────────────
	r.broadcastState()

	// ── Step 6: heartbeat — ping every 15 s, close on failure ───────────────
	// Browsers respond to WebSocket pings automatically with a pong frame;
	// no JS-side handling is needed.  15 s is short enough to detect mobile
	// connections killed by backgrounding within a reasonable time.
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				pingCtx, pingCancel := context.WithTimeout(ctx, 5*time.Second)
				err := conn.Ping(pingCtx)
				pingCancel()
				if err != nil {
					// Connection is dead; cancel the main context to unblock conn.Read.
					cancel()
					return
				}
			}
		}
	}()

	// ── Step 7: read loop ────────────────────────────────────────────────────
	for {
		_, raw, err := conn.Read(ctx)
		if err != nil {
			log.Printf("ws read error room=%s player=%q: %v", r.id, player.Name, err)
			break
		}
		var action ClientMessage
		if json.Unmarshal(raw, &action) != nil {
			continue
		}
		r.handleAction(sessionID, action)
	}
}

func (r *Room) handleAction(sessionID string, msg ClientMessage) {
	var actionErr error
	switch msg.Action {
	case "start":
		actionErr = r.game.Start(sessionID)
	case "draw":
		actionErr = r.game.Draw(sessionID)
	case "stop":
		actionErr = r.game.Stop(sessionID)
	case "target":
		actionErr = r.game.Target(sessionID, msg.TargetID)
	case "steal":
		actionErr = r.game.Steal(sessionID, msg.CardValue)
	case "restart":
		actionErr = r.game.Restart(sessionID)
	default:
		return
	}

	if actionErr != nil {
		r.sendTo(sessionID, map[string]any{"type": "error", "message": actionErr.Error()})
		return
	}
	r.broadcastState()
}

// broadcastState sends the full game state to every connected client.
func (r *Room) broadcastState() {
	state := r.game.State()
	msg := map[string]any{"type": "state", "game": state}
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, c := range r.clients {
		c.writeMu.Lock()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = c.conn.Write(ctx, websocket.MessageText, data)
		cancel()
		c.writeMu.Unlock()
	}
}

func (r *Room) sendTo(sessionID string, v any) {
	r.mu.RLock()
	c, ok := r.clients[sessionID]
	r.mu.RUnlock()
	if ok {
		_ = c.writeJSON(v)
	}
}

func (r *Room) addClient(c *Client) {
	r.mu.Lock()
	r.clients[c.sessionID] = c
	r.mu.Unlock()
}

// removeClient removes the client from the map only if it is still the
// registered client for that session.  This prevents a reconnecting player's
// new connection from being evicted when the old goroutine's deferred cleanup
// finally runs.
func (r *Room) removeClient(sessionID string, c *Client) {
	r.mu.Lock()
	if r.clients[sessionID] == c {
		delete(r.clients, sessionID)
	}
	r.mu.Unlock()
}

// Phase returns the current game phase as a string (for the status API).
func (r *Room) Phase() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return string(r.game.Phase)
}

func (r *Room) isEmpty() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.clients) == 0
}

// ticker runs background housekeeping every 2 seconds.
func (r *Room) ticker() {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	emptyAt := time.Time{}

	for range t.C {
		changed1 := r.game.TickInactive()
		changed2 := r.game.TickNextRound()
		if changed1 || changed2 {
			r.broadcastState()
		}

		// Stop the ticker once the room has been empty for 10 minutes.
		if r.isEmpty() {
			if emptyAt.IsZero() {
				emptyAt = time.Now()
			} else if time.Since(emptyAt) > 10*time.Minute {
				return
			}
		} else {
			emptyAt = time.Time{}
		}
	}
}

// newSessionID returns a random session identifier.
func newSessionID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
