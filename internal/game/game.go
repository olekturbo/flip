package game

import (
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"
)

// Phase represents the overall game lifecycle stage.
type Phase string

const (
	PhaseLobby    Phase = "lobby"
	PhasePlaying  Phase = "playing"
	PhaseRoundEnd Phase = "round_end"
	PhaseGameOver Phase = "game_over"
)

const (
	WinScore       = 200
	ReconnectGrace = 60 * time.Second
	TurnSkipDelay  = 30 * time.Second
	AutoNextRound  = 4 * time.Second
	MaxPlayers     = 6
)

// pendingAction holds an action card awaiting target selection by the drawer.
type pendingAction struct {
	Card      Card
	DrawerIdx int
}

// PendingActionState is the JSON-serialisable form broadcast to clients.
type PendingActionState struct {
	Card           Card     `json:"card"`
	DrawerID       string   `json:"drawerID"`
	ValidTargetIDs []string `json:"validTargetIDs"`
}

// GameState is the JSON-serialisable snapshot broadcast to clients.
type GameState struct {
	Phase              Phase               `json:"phase"`
	RoundNumber        int                 `json:"roundNumber"`
	CurrentPlayerIndex int                 `json:"currentPlayerIndex"`
	DeckSize           int                 `json:"deckSize"`
	Message            string              `json:"message"`
	Players            []PlayerView        `json:"players"`
	PendingAction      *PendingActionState `json:"pendingAction,omitempty"`
	WinnerIDs          []string            `json:"winnerIDs,omitempty"` // one or more in case of a tie
	NextRoundIn        int                 `json:"nextRoundIn,omitempty"`
	Events             []string            `json:"events"`
}

// PlayerView is the serialisable projection of a Player.
type PlayerView struct {
	ID              string       `json:"id"`
	Name            string       `json:"name"`
	Cards           []Card       `json:"cards"`
	TotalScore      int          `json:"totalScore"`
	RoundScore      int          `json:"roundScore"`
	RoundBonus      int          `json:"roundBonus,omitempty"` // +15 for Flip 7 winner
	Status          PlayerStatus `json:"status"`
	HasSecondChance bool         `json:"hasSecondChance"`
	IsHost          bool         `json:"isHost"`
	Connected       bool         `json:"connected"`
}

// Game holds the mutable state of one room's game.
// All exported methods acquire g.mu; internal helpers assume it is already held.
type Game struct {
	mu           sync.RWMutex
	ID           string
	Phase        Phase
	Players      []*Player
	Deck         []Card
	UsedCards    []Card         // discard pile; reshuffled into Deck when Deck runs out
	pending      *pendingAction // action card awaiting target selection
	CurrentIndex int
	RoundNumber  int
	DealerIndex  int // index of the current dealer; -1 before first round
	Message      string
	Winners        []*Player // one or more players (tie is possible)
	flip7WinnerID  string    // ID of the player who triggered Flip 7 this round (for display)
	roundEndedAt   time.Time
	dealingQueue   []int   // player indices still to receive initial card
	inDealing      bool    // true while initial deal is in progress
	deferredCards  []Card  // action cards queued for resolution after a Flip 3
	deferredFor    *Player // the Flip 3 target who must choose targets for deferred cards
	deferredAdvance bool   // call advanceTurn once all deferred cards are resolved
	inDeferred     bool    // true while processDeferredCards is running (suppresses advanceTurn)
	events         []string
}

// logEvent appends a message to the event history (capped at 80 entries).
func (g *Game) logEvent(format string, args ...interface{}) {
	g.events = append(g.events, fmt.Sprintf(format, args...))
	if len(g.events) > 80 {
		g.events = g.events[len(g.events)-80:]
	}
}

// processDeferredCards resolves action cards that were drawn during a Flip 3 one by one.
// Cards with a single valid target are auto-resolved; cards with multiple valid targets
// set g.pending so the player can choose, then return — the next call continues.
// When the queue is empty, advanceTurn is called if required.
func (g *Game) processDeferredCards() {
	g.inDeferred = true
	defer func() { g.inDeferred = false }()

	for len(g.deferredCards) > 0 {
		dc := g.deferredCards[0]
		g.deferredCards = g.deferredCards[1:]

		p := g.deferredFor
		if p == nil || p.Status != StatusActive || g.Phase != PhasePlaying {
			continue
		}

		targets := g.validTargetsFor(dc)
		switch len(targets) {
		case 0:
			g.logEvent("  %s (deferred) — no valid target, discarded", dc.Name)
			g.Message = fmt.Sprintf("%s (deferred from Flip 3) — no valid target, discarded.", dc.Name)
		case 1:
			g.resolveActionWithTarget(p, dc, g.playerByID(targets[0]))
		default:
			// Require player choice — pause until Target() is called.
			g.pending = &pendingAction{Card: dc, DrawerIdx: g.indexOfPlayer(p)}
			g.Message = fmt.Sprintf("%s drew %s during Flip 3 — choose a target!", p.Name, dc.Name)
			return
		}
	}

	// All deferred cards resolved.
	g.deferredCards = nil
	g.deferredFor = nil
	if g.deferredAdvance && g.Phase == PhasePlaying && !g.inDealing {
		g.deferredAdvance = false
		g.advanceTurn()
	}
}

// New creates an empty game for the given room ID.
func New(id string) *Game {
	return &Game{
		ID:          id,
		Phase:       PhaseLobby,
		DealerIndex: -1,
	}
}

// ─── Player management ────────────────────────────────────────────────────────

func (g *Game) AddPlayer(sessionID, name string) (*Player, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.Phase != PhaseLobby {
		return nil, fmt.Errorf("game is already in progress")
	}
	if len(g.Players) >= MaxPlayers {
		return nil, fmt.Errorf("room is full (%d players max)", MaxPlayers)
	}
	for _, p := range g.Players {
		if p.SessionID == sessionID {
			p.Connected = true
			return p, nil
		}
	}
	p := &Player{
		ID:        newUUID(),
		SessionID: sessionID,
		Name:      name,
		Cards:     make([]Card, 0),
		Status:    StatusActive,
		Connected: true,
		IsHost:    len(g.Players) == 0,
	}
	g.Players = append(g.Players, p)
	return p, nil
}

func (g *Game) Rejoin(sessionID string) (*Player, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()

	p := g.findBySession(sessionID)
	if p == nil {
		return nil, false
	}
	if !p.Connected && time.Since(p.DisconnectedAt) > ReconnectGrace {
		p.Status = StatusInactive
	}
	p.Connected = true
	return p, true
}

func (g *Game) Disconnect(sessionID string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if p := g.findBySession(sessionID); p != nil {
		p.Connected = false
		p.DisconnectedAt = time.Now()
	}
}

// TickInactive marks stale disconnected players inactive, skips them when current,
// and auto-resolves a pending action if the drawer has been gone too long.
func (g *Game) TickInactive() bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.Phase != PhasePlaying {
		return false
	}

	changed := false
	cp := g.currentPlayer()

	for _, p := range g.Players {
		if p.Connected || p.Status == StatusStopped || p.Status == StatusBusted ||
			p.Status == StatusFrozen || p.Status == StatusInactive {
			continue
		}
		threshold := ReconnectGrace
		if p == cp {
			threshold = TurnSkipDelay
		}
		if time.Since(p.DisconnectedAt) > threshold {
			p.Status = StatusInactive
			changed = true
		}
	}

	// Auto-resolve a pending action if the drawer disconnected.
	if g.pending != nil {
		drawer := g.Players[g.pending.DrawerIdx]
		if !drawer.Connected && time.Since(drawer.DisconnectedAt) > TurnSkipDelay {
			card := g.pending.Card
			g.pending = nil
			targets := g.validTargetsFor(card)
			if len(targets) > 0 {
				g.resolveActionWithTarget(drawer, card, g.playerByID(targets[0]))
			}
			changed = true
		}
	}

	cp = g.currentPlayer()
	if cp != nil && cp.Status == StatusInactive && g.pending == nil {
		g.logEvent("%s is inactive — turn skipped", cp.Name)
		g.Message = fmt.Sprintf("%s is inactive — turn skipped.", cp.Name)
		g.advanceTurn()
		changed = true
	}

	return changed
}

func (g *Game) TickNextRound() bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.Phase != PhaseRoundEnd {
		return false
	}
	if time.Since(g.roundEndedAt) < AutoNextRound {
		return false
	}
	g.startRound()
	return true
}

// ─── Game flow ────────────────────────────────────────────────────────────────

func (g *Game) Start(sessionID string) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.Phase != PhaseLobby {
		return fmt.Errorf("game is already in progress")
	}
	host := g.findBySession(sessionID)
	if host == nil || !host.IsHost {
		return fmt.Errorf("only the host can start the game")
	}
	if len(g.Players) < 2 {
		return fmt.Errorf("need at least 2 players to start")
	}
	g.startRound()
	return nil
}

func (g *Game) Draw(sessionID string) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.Phase != PhasePlaying {
		return fmt.Errorf("game is not in progress")
	}
	if g.pending != nil {
		return fmt.Errorf("choose a target for the action card first")
	}
	cp := g.currentPlayer()
	if cp == nil || cp.SessionID != sessionID {
		return fmt.Errorf("it is not your turn")
	}
	if cp.Status != StatusActive {
		return fmt.Errorf("you cannot draw right now (status: %s)", cp.Status)
	}
	g.drawOne(cp, false)
	return nil
}

func (g *Game) Stop(sessionID string) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.Phase != PhasePlaying {
		return fmt.Errorf("game is not in progress")
	}
	if g.pending != nil {
		return fmt.Errorf("choose a target for the action card first")
	}
	cp := g.currentPlayer()
	if cp == nil || cp.SessionID != sessionID {
		return fmt.Errorf("it is not your turn")
	}
	if cp.Status != StatusActive {
		return fmt.Errorf("you cannot stop right now")
	}
	cp.Status = StatusStopped
	g.logEvent("%s stopped — %d pts", cp.Name, cp.RoundScore())
	g.Message = fmt.Sprintf("%s stopped with %d round points.", cp.Name, cp.RoundScore())
	g.advanceTurn()
	return nil
}

// Target resolves a pending action card by applying it to the chosen target.
func (g *Game) Target(sessionID, targetID string) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.Phase != PhasePlaying {
		return fmt.Errorf("game is not in progress")
	}
	if g.pending == nil {
		return fmt.Errorf("no action card awaiting a target")
	}
	drawer := g.Players[g.pending.DrawerIdx]
	if drawer.SessionID != sessionID {
		return fmt.Errorf("it is not your action to resolve")
	}

	target := g.playerByID(targetID)
	if target == nil {
		return fmt.Errorf("target player not found")
	}

	// Validate target is in the valid set for this card.
	valid := false
	for _, id := range g.validTargetsFor(g.pending.Card) {
		if id == targetID {
			valid = true
			break
		}
	}
	if !valid {
		return fmt.Errorf("invalid target for this action")
	}

	card := g.pending.Card
	g.pending = nil
	g.resolveActionWithTarget(drawer, card, target)
	if g.deferredFor != nil {
		// Still processing deferred cards from a Flip 3 — continue the queue.
		g.processDeferredCards()
	} else if g.inDealing {
		g.continueDealing()
	}
	return nil
}

func (g *Game) Restart(sessionID string) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.Phase != PhaseGameOver {
		return fmt.Errorf("can only restart after game over")
	}
	host := g.findBySession(sessionID)
	if host == nil || !host.IsHost {
		return fmt.Errorf("only the host can restart")
	}
	g.Phase = PhaseLobby
	g.RoundNumber = 0
	g.DealerIndex = -1
	g.Winners = nil
	g.Deck = nil
	g.UsedCards = nil
	g.pending = nil
	g.flip7WinnerID = ""
	g.events = nil
	g.deferredCards = nil
	g.deferredFor = nil
	g.deferredAdvance = false
	g.Message = "Game reset — waiting for the host to start."
	for _, p := range g.Players {
		p.TotalScore = 0
		p.Cards = make([]Card, 0)
		p.Status = StatusActive
		p.HasSecondChance = false
	}
	return nil
}

func (g *Game) State() GameState {
	g.mu.RLock()
	defer g.mu.RUnlock()

	views := make([]PlayerView, len(g.Players))
	for i, p := range g.Players {
		rs := p.RoundScore()
		if p.Status == StatusBusted || p.Status == StatusInactive {
			rs = 0
		}
		bonus := 0
		if g.flip7WinnerID != "" && p.ID == g.flip7WinnerID {
			bonus = 15
		}
		views[i] = PlayerView{
			ID:              p.ID,
			Name:            p.Name,
			Cards:           p.Cards,
			TotalScore:      p.TotalScore,
			RoundScore:      rs,
			RoundBonus:      bonus,
			Status:          p.Status,
			HasSecondChance: p.HasSecondChance,
			IsHost:          p.IsHost,
			Connected:       p.Connected,
		}
	}

	var winnerIDs []string
	for _, w := range g.Winners {
		winnerIDs = append(winnerIDs, w.ID)
	}

	nextRoundIn := 0
	if g.Phase == PhaseRoundEnd && !g.roundEndedAt.IsZero() {
		if remaining := AutoNextRound - time.Since(g.roundEndedAt); remaining > 0 {
			nextRoundIn = int(remaining.Seconds()) + 1
		}
	}

	var pendingState *PendingActionState
	if g.pending != nil {
		drawer := g.Players[g.pending.DrawerIdx]
		pendingState = &PendingActionState{
			Card:           g.pending.Card,
			DrawerID:       drawer.ID,
			ValidTargetIDs: g.validTargetsFor(g.pending.Card),
		}
	}

	events := make([]string, len(g.events))
	copy(events, g.events)

	return GameState{
		Phase:              g.Phase,
		RoundNumber:        g.RoundNumber,
		CurrentPlayerIndex: g.CurrentIndex,
		DeckSize:           len(g.Deck),
		Message:            g.Message,
		Players:            views,
		PendingAction:      pendingState,
		WinnerIDs:          winnerIDs,
		NextRoundIn:        nextRoundIn,
		Events:             events,
	}
}

// ─── Internal helpers ─────────────────────────────────────────────────────────

func (g *Game) startRound() {
	g.Phase = PhasePlaying
	g.RoundNumber++
	g.roundEndedAt = time.Time{}
	g.pending = nil
	g.flip7WinnerID = ""

	// Return all cards from players' hands to the discard pile before reset.
	for _, p := range g.Players {
		g.UsedCards = append(g.UsedCards, p.Cards...)
	}
	for _, p := range g.Players {
		p.ResetForRound()
	}

	// Use existing deck; only create a fresh one when both piles are empty.
	if len(g.Deck) == 0 && len(g.UsedCards) == 0 {
		g.Deck = NewDeck()
	} else if len(g.Deck) == 0 {
		g.refillDeck()
	}

	// Advance dealer (rotates through all player slots).
	if len(g.Players) > 0 {
		g.DealerIndex = (g.DealerIndex + 1) % len(g.Players)
	}

	g.CurrentIndex = -1
	g.inDealing = true

	n := len(g.Players)
	g.dealingQueue = make([]int, 0, n)
	for i := 0; i < n; i++ {
		idx := (g.DealerIndex + 1 + i) % n
		if g.Players[idx].Status != StatusInactive {
			g.dealingQueue = append(g.dealingQueue, idx)
		}
	}

	g.logEvent("── Round %d ──", g.RoundNumber)
	g.Message = fmt.Sprintf("Round %d — dealing initial cards...", g.RoundNumber)
	g.continueDealing()
}

func (g *Game) continueDealing() {
	for len(g.dealingQueue) > 0 && g.Phase == PhasePlaying {
		idx := g.dealingQueue[0]
		g.dealingQueue = g.dealingQueue[1:]
		p := g.Players[idx]
		if p.Status != StatusActive {
			continue
		}
		g.dealCardTo(p)
		if g.pending != nil {
			return // waiting for player input
		}
	}
	g.dealingQueue = nil
	g.inDealing = false
	if g.Phase == PhasePlaying {
		g.finishDealing()
	}
}

func (g *Game) finishDealing() {
	g.CurrentIndex = g.nextActiveFrom(g.DealerIndex)
	if g.CurrentIndex < 0 {
		g.Phase = PhaseRoundEnd
		g.roundEndedAt = time.Now()
		g.Message = "No active players — round skipped."
		return
	}
	g.Message = fmt.Sprintf("Round %d — %s's turn.", g.RoundNumber, g.currentPlayer().Name)
}

// dealCardTo deals one starting card to p during the dealing phase.
func (g *Game) dealCardTo(p *Player) {
	if (len(g.Deck) == 0 && len(g.UsedCards) == 0) || p.Status != StatusActive {
		return
	}
	card := g.pop()

	switch card.Type {
	case CardTypeNumber:
		if p.HasNumber(card.Value) {
			p.Cards = append(p.Cards, card)
			p.Status = StatusBusted
			g.logEvent("%s dealt %d — BUSTED (duplicate)", p.Name, card.Value)
		} else {
			p.Cards = append(p.Cards, card)
			g.logEvent("%s dealt %d", p.Name, card.Value)
		}
	case CardTypeModifierAdd, CardTypeModifierMul, CardTypeModifierSub, CardTypeModifierDiv:
		p.Cards = append(p.Cards, card)
		g.logEvent("%s dealt %s", p.Name, card.Name)
	case CardTypeFreeze, CardTypeFlip3, CardTypeSecondChance:
		targets := g.validTargetsFor(card)
		switch len(targets) {
		case 0:
			g.logEvent("%s dealt %s — no valid target, discarded", p.Name, card.Name)
			g.Message = fmt.Sprintf("%s was dealt %s — no valid target, discarded.", p.Name, card.Name)
		case 1:
			g.resolveActionWithTarget(p, card, g.playerByID(targets[0]))
		default:
			g.logEvent("%s dealt %s — choosing target", p.Name, card.Name)
			g.pending = &pendingAction{Card: card, DrawerIdx: g.indexOfPlayer(p)}
			g.Message = fmt.Sprintf("%s was dealt %s — choose a target!", p.Name, card.Name)
		}
	}
}

// pop removes and returns the top card from the deck, refilling from used pile if needed.
func (g *Game) pop() Card {
	if len(g.Deck) == 0 {
		g.refillDeck()
	}
	card := g.Deck[0]
	g.Deck = g.Deck[1:]
	return card
}

// refillDeck shuffles the discard pile into a fresh draw pile.
func (g *Game) refillDeck() {
	g.Deck = g.UsedCards
	g.UsedCards = make([]Card, 0, 94)
	rand.Shuffle(len(g.Deck), func(i, j int) {
		g.Deck[i], g.Deck[j] = g.Deck[j], g.Deck[i]
	})
}

// drawOne draws one card for player p during normal play.
// inFlip3=true: action cards are returned deferred instead of triggering pending.
func (g *Game) drawOne(p *Player, inFlip3 bool) []Card {
	if len(g.Deck) == 0 && len(g.UsedCards) == 0 {
		p.Status = StatusStopped
		g.Message = fmt.Sprintf("All cards drawn — %s is forced to stop.", p.Name)
		g.advanceTurn()
		return nil
	}

	card := g.pop()

	switch card.Type {
	case CardTypeNumber:
		if p.HasNumber(card.Value) {
			if p.HasSecondChance {
				g.consumeSecondChance(p)
				g.logEvent("%s survived bust with 2nd Chance (drew %d)", p.Name, card.Value)
				g.Message = fmt.Sprintf("%s drew %d (duplicate!) — Second Chance used! Turn ends.", p.Name, card.Value)
				g.advanceTurn()
			} else {
				p.Cards = append(p.Cards, card)
				p.Status = StatusBusted
				g.logEvent("%s BUSTED — duplicate %d", p.Name, card.Value)
				g.Message = fmt.Sprintf("%s drew %d — BUSTED! All round points lost.", p.Name, card.Value)
				g.advanceTurn()
			}
		} else {
			p.Cards = append(p.Cards, card)
			g.logEvent("%s drew %d", p.Name, card.Value)
			g.Message = fmt.Sprintf("%s drew %d.", p.Name, card.Value)
			if p.UniqueNumberCount() == 7 {
				g.triggerFlip7(p)
			} else {
				g.advanceTurn() // one card per turn — pass to next player
			}
		}

	case CardTypeModifierAdd, CardTypeModifierMul, CardTypeModifierSub, CardTypeModifierDiv:
		p.Cards = append(p.Cards, card)
		g.logEvent("%s drew %s", p.Name, card.Name)
		g.Message = fmt.Sprintf("%s drew %s.", p.Name, card.Name)
		g.advanceTurn() // one card per turn — pass to next player

	case CardTypeFreeze, CardTypeFlip3, CardTypeSecondChance:
		if inFlip3 {
			return []Card{card} // defer; don't resolve yet
		}
		// Set pending — wait for the drawer to choose a target.
		targets := g.validTargetsFor(card)
		switch len(targets) {
		case 0:
			// No valid target (e.g. all active players already hold Second Chance).
			g.logEvent("%s drew %s — no valid target, discarded", p.Name, card.Name)
			g.Message = fmt.Sprintf("%s drew %s — no valid target, discarded.", p.Name, card.Name)
			g.advanceTurn()
		case 1:
			// Only one possible target: auto-resolve immediately.
			g.resolveActionWithTarget(p, card, g.playerByID(targets[0]))
		default:
			g.pending = &pendingAction{Card: card, DrawerIdx: g.CurrentIndex}
			g.Message = fmt.Sprintf("%s drew %s — choose a target!", p.Name, card.Name)
		}
	}

	return nil
}

// resolveActionWithTarget applies an action card effect; drawer chose target explicitly.
func (g *Game) resolveActionWithTarget(drawer *Player, card Card, target *Player) {
	switch card.Type {

	case CardTypeFreeze:
		target.Cards = append(target.Cards, card)
		target.Status = StatusFrozen
		if target == drawer {
			g.logEvent("%s froze themselves — %d pts banked", drawer.Name, drawer.RoundScore())
			g.Message = fmt.Sprintf("%s froze themselves — banks %d pts!", drawer.Name, drawer.RoundScore())
		} else {
			g.logEvent("%s froze %s — %s banks %d pts", drawer.Name, target.Name, target.Name, target.RoundScore())
			g.Message = fmt.Sprintf("%s used Freeze on %s — %s banks %d pts and exits!",
				drawer.Name, target.Name, target.Name, target.RoundScore())
		}
		if !g.inDealing && !g.inDeferred {
			g.advanceTurn()
		}

	case CardTypeFlip3:
		target.Cards = append(target.Cards, card)
		if target == drawer {
			g.logEvent("%s Flip 3 — drawing 3 cards", drawer.Name)
			g.Message = fmt.Sprintf("%s drew Flip 3 — drawing 3 cards!", drawer.Name)
		} else {
			g.logEvent("%s → Flip 3 → %s draws 3 cards", drawer.Name, target.Name)
			g.Message = fmt.Sprintf("%s used Flip 3 on %s — %s draws 3 cards!", drawer.Name, target.Name, target.Name)
		}
		if g.inDealing {
			for i, qIdx := range g.dealingQueue {
				if g.Players[qIdx] == target {
					g.dealingQueue = append(g.dealingQueue[:i], g.dealingQueue[i+1:]...)
					break
				}
			}
		}
		deferred := []Card{}
		for i := 0; i < 3; i++ {
			if target.Status != StatusActive || g.Phase != PhasePlaying {
				break
			}
			d, ended := g.drawOneFlip3(target)
			deferred = append(deferred, d...)
			if ended {
				break
			}
		}
		// Resolve action cards drawn during the Flip 3 interactively when possible.
		if len(deferred) > 0 && target.Status == StatusActive && g.Phase == PhasePlaying {
			g.deferredCards = deferred
			g.deferredFor = target
			g.deferredAdvance = !g.inDealing
			g.processDeferredCards()
			// processDeferredCards calls advanceTurn when all cards are resolved.
		} else if g.Phase == PhasePlaying && !g.inDealing && !g.inDeferred {
			g.advanceTurn()
		}

	case CardTypeSecondChance:
		target.Cards = append(target.Cards, card)
		target.HasSecondChance = true
		if target == drawer {
			g.logEvent("%s drew 2nd Chance", drawer.Name)
			g.Message = fmt.Sprintf("%s drew Second Chance — one bust blocked!", drawer.Name)
		} else {
			g.logEvent("%s gave 2nd Chance to %s", drawer.Name, target.Name)
			g.Message = fmt.Sprintf("%s gave Second Chance to %s!", drawer.Name, target.Name)
		}
		if !g.inDealing && !g.inDeferred {
			g.advanceTurn()
		}
	}
}

// drawOneFlip3 draws one card for p as part of the 3 forced draws in Flip Three.
// Action cards are returned deferred. Bust/SC-save set status but do NOT advance turn
// (the Flip Three loop and resolveActionWithTarget handle turn advancement).
// Returns (deferred action cards, early-stop flag).
func (g *Game) drawOneFlip3(p *Player) ([]Card, bool) {
	if len(g.Deck) == 0 && len(g.UsedCards) == 0 {
		p.Status = StatusStopped
		g.Message += fmt.Sprintf(" (no cards left — %s stops)", p.Name)
		return nil, true
	}

	card := g.pop()

	switch card.Type {
	case CardTypeNumber:
		if p.HasNumber(card.Value) {
			if p.HasSecondChance {
				g.consumeSecondChance(p)
				g.logEvent("  %s survived Flip 3 bust with 2nd Chance (drew %d)", p.Name, card.Value)
				g.Message = fmt.Sprintf("%s drew %d during Flip 3 (duplicate!) — Second Chance used! Draw ends.", p.Name, card.Value)
				return nil, true // SC stops the Flip 3 sequence; turn will end normally
			}
			p.Cards = append(p.Cards, card)
			p.Status = StatusBusted
			g.logEvent("  %s BUSTED in Flip 3 — duplicate %d", p.Name, card.Value)
			g.Message = fmt.Sprintf("%s drew %d during Flip 3 — BUSTED!", p.Name, card.Value)
			return nil, true // bust stops the Flip 3 sequence
		}
		p.Cards = append(p.Cards, card)
		g.logEvent("  %s drew %d (Flip 3)", p.Name, card.Value)
		g.Message = fmt.Sprintf("%s drew %d (Flip 3).", p.Name, card.Value)
		if p.UniqueNumberCount() == 7 {
			g.triggerFlip7(p)
			return nil, true
		}

	case CardTypeModifierAdd, CardTypeModifierMul, CardTypeModifierSub, CardTypeModifierDiv:
		p.Cards = append(p.Cards, card)
		g.logEvent("  %s drew %s (Flip 3)", p.Name, card.Name)
		g.Message = fmt.Sprintf("%s drew %s (Flip 3).", p.Name, card.Name)

	case CardTypeFreeze, CardTypeFlip3, CardTypeSecondChance:
		return []Card{card}, false // defer; resolve after all 3 cards drawn
	}

	return nil, false
}

// resolveActionAuto applies an action card with automatic targeting (used for
// action cards deferred during Flip Three — no player input required).
func (g *Game) resolveActionAuto(target *Player, card Card) {
	switch card.Type {

	case CardTypeFreeze:
		// Auto-target: next active player (including the Flip3 victim as a last resort).
		targetIdx := g.indexOfPlayer(target)
		nextIdx := g.nextActiveFromExcluding(targetIdx, target)
		if nextIdx < 0 {
			// No other active player — target freezes themselves.
			nextIdx = targetIdx
		}
		if g.Players[nextIdx].Status == StatusActive {
			g.Players[nextIdx].Cards = append(g.Players[nextIdx].Cards, card)
			g.Players[nextIdx].Status = StatusFrozen
			g.logEvent("  Freeze (auto) → %s banks %d pts", g.Players[nextIdx].Name, g.Players[nextIdx].RoundScore())
			g.Message = fmt.Sprintf("Freeze (deferred) — %s banks %d pts and exits!",
				g.Players[nextIdx].Name, g.Players[nextIdx].RoundScore())
		} else {
			target.Cards = append(target.Cards, card)
			g.Message = "Freeze (deferred) — no active player to freeze."
		}

	case CardTypeFlip3:
		target.Cards = append(target.Cards, card)
		g.logEvent("  Flip 3 (auto) → %s draws 3 more cards", target.Name)
		g.Message = fmt.Sprintf("Flip 3 (deferred) — %s draws 3 more cards!", target.Name)
		deferred := []Card{}
		for i := 0; i < 3; i++ {
			if target.Status != StatusActive || g.Phase != PhasePlaying {
				break
			}
			d, ended := g.drawOneFlip3(target)
			deferred = append(deferred, d...)
			if ended {
				break
			}
		}
		for _, dc := range deferred {
			if target.Status == StatusActive && g.Phase == PhasePlaying {
				g.resolveActionAuto(target, dc)
			}
		}

	case CardTypeSecondChance:
		if !target.HasSecondChance {
			target.Cards = append(target.Cards, card)
			target.HasSecondChance = true
			g.Message = fmt.Sprintf("%s gets Second Chance (deferred from Flip 3)!", target.Name)
		} else {
			g.Message = fmt.Sprintf("Second Chance (deferred) discarded — %s already holds one.", target.Name)
		}
	}
}

// validTargetsFor returns the IDs of players who are valid targets for card.
func (g *Game) validTargetsFor(card Card) []string {
	ids := make([]string, 0, len(g.Players))
	for _, p := range g.Players {
		if p.Status != StatusActive {
			continue
		}
		if card.Type == CardTypeSecondChance && p.HasSecondChance {
			continue // can't give a second SC to someone who already has one
		}
		ids = append(ids, p.ID)
	}
	return ids
}

// consumeSecondChance removes the SC card from p's hand and clears the flag.
func (g *Game) consumeSecondChance(p *Player) {
	p.HasSecondChance = false
	for i, c := range p.Cards {
		if c.Type == CardTypeSecondChance {
			p.Cards = append(p.Cards[:i], p.Cards[i+1:]...)
			return
		}
	}
}

// triggerFlip7 ends the round when p collects 7 unique number cards.
func (g *Game) triggerFlip7(p *Player) {
	g.logEvent("★ %s — FLIP 7! (+15 bonus)", p.Name)
	for _, other := range g.Players {
		if other != p && other.Status == StatusActive {
			other.Status = StatusStopped
		}
	}
	g.endRound(p)
}

// advanceTurn moves to the next active player, or ends the round.
func (g *Game) advanceTurn() {
	next := g.nextActiveFrom(g.CurrentIndex)
	if next < 0 {
		g.endRound(nil)
		return
	}
	g.CurrentIndex = next
	g.Message += fmt.Sprintf(" → %s's turn.", g.Players[next].Name)
}

// endRound tallies scores. flip7Winner gets +15 bonus; pass nil if no Flip 7.
func (g *Game) endRound(flip7Winner *Player) {
	g.Phase = PhaseRoundEnd
	g.roundEndedAt = time.Now()
	g.pending = nil
	if flip7Winner != nil {
		g.flip7WinnerID = flip7Winner.ID
	} else {
		g.flip7WinnerID = ""
	}

	parts := make([]string, 0, len(g.Players))
	for _, p := range g.Players {
		if p.Status == StatusBusted || p.Status == StatusInactive {
			parts = append(parts, fmt.Sprintf("%s busted (0)", p.Name))
		} else {
			rs := p.RoundScore()
			if p == flip7Winner {
				rs += 15
			}
			p.TotalScore += rs
			parts = append(parts, fmt.Sprintf("%s +%d→%d", p.Name, rs, p.TotalScore))
		}
	}

	// Find the highest total score at or above WinScore.
	topScore := 0
	for _, p := range g.Players {
		if p.TotalScore >= WinScore && p.TotalScore > topScore {
			topScore = p.TotalScore
		}
	}

	// Collect every player sitting at that top score.
	var leaders []*Player
	if topScore > 0 {
		for _, p := range g.Players {
			if p.TotalScore == topScore {
				leaders = append(leaders, p)
			}
		}
	}

	switch {
	case len(leaders) == 1:
		// One player is clearly ahead at 200+ — they win.
		g.Phase = PhaseGameOver
		g.Winners = leaders
		g.Message = fmt.Sprintf("GAME OVER — %s wins with %d pts! (%s)",
			leaders[0].Name, leaders[0].TotalScore, strings.Join(parts, ", "))

	case len(leaders) > 1:
		// Multiple players tied at 200+ — keep playing until one pulls ahead.
		names := make([]string, len(leaders))
		for i, l := range leaders {
			names[i] = l.Name
		}
		tieMsg := fmt.Sprintf("%s are tied at %d pts — playing on to break the tie!",
			strings.Join(names, " & "), topScore)
		if flip7Winner != nil {
			g.Message = fmt.Sprintf("FLIP 7 by %s (+15 bonus)! %s  %s — next round soon.",
				flip7Winner.Name, strings.Join(parts, ", "), tieMsg)
		} else {
			g.Message = fmt.Sprintf("Round %d: %s  %s — next round soon.",
				g.RoundNumber, strings.Join(parts, ", "), tieMsg)
		}

	default:
		// Nobody at 200+ yet — normal round end.
		if flip7Winner != nil {
			g.Message = fmt.Sprintf("FLIP 7 by %s (+15 bonus)! %s — next round soon.",
				flip7Winner.Name, strings.Join(parts, ", "))
		} else {
			g.Message = fmt.Sprintf("Round %d over: %s — next round starting soon.",
				g.RoundNumber, strings.Join(parts, ", "))
		}
	}
}

func (g *Game) currentPlayer() *Player {
	if g.CurrentIndex < 0 || g.CurrentIndex >= len(g.Players) {
		return nil
	}
	return g.Players[g.CurrentIndex]
}

func (g *Game) findBySession(id string) *Player {
	for _, p := range g.Players {
		if p.SessionID == id {
			return p
		}
	}
	return nil
}

func (g *Game) playerByID(id string) *Player {
	for _, p := range g.Players {
		if p.ID == id {
			return p
		}
	}
	return nil
}

func (g *Game) indexOfPlayer(target *Player) int {
	for i, p := range g.Players {
		if p == target {
			return i
		}
	}
	return -1
}

// nextActiveFrom returns the index of the first Active player strictly after `from`.
func (g *Game) nextActiveFrom(from int) int {
	n := len(g.Players)
	if n == 0 {
		return -1
	}
	for i := 1; i <= n; i++ {
		idx := (from + i + n) % n
		if g.Players[idx].Status == StatusActive {
			return idx
		}
	}
	return -1
}

// nextActiveFromExcluding returns the next Active player after `from`, skipping `exclude`.
func (g *Game) nextActiveFromExcluding(from int, exclude *Player) int {
	n := len(g.Players)
	if n == 0 {
		return -1
	}
	for i := 1; i <= n; i++ {
		idx := (from + i + n) % n
		p := g.Players[idx]
		if p.Status == StatusActive && p != exclude {
			return idx
		}
	}
	return -1
}
