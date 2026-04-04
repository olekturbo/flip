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
	AutoNextRound  = 5 * time.Second
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
}

// PlayerView is the serialisable projection of a Player.
type PlayerView struct {
	ID              string       `json:"id"`
	Name            string       `json:"name"`
	Cards           []Card       `json:"cards"`
	TotalScore      int          `json:"totalScore"`
	RoundScore      int          `json:"roundScore"`
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
	Winners      []*Player // one or more players (tie is possible)
	roundEndedAt time.Time
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
		views[i] = PlayerView{
			ID:              p.ID,
			Name:            p.Name,
			Cards:           p.Cards,
			TotalScore:      p.TotalScore,
			RoundScore:      rs,
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
	}
}

// ─── Internal helpers ─────────────────────────────────────────────────────────

func (g *Game) startRound() {
	g.Phase = PhasePlaying
	g.RoundNumber++
	g.roundEndedAt = time.Time{}
	g.pending = nil

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

	// Deal one starting card to each player, starting left of dealer.
	n := len(g.Players)
	for i := 0; i < n; i++ {
		idx := (g.DealerIndex + 1 + i) % n
		p := g.Players[idx]
		if p.Status == StatusInactive {
			continue
		}
		g.dealCardTo(p)
	}

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
		} else {
			p.Cards = append(p.Cards, card)
		}
	case CardTypeModifierAdd, CardTypeModifierMul:
		p.Cards = append(p.Cards, card)
	case CardTypeSecondChance:
		p.Cards = append(p.Cards, card)
		p.HasSecondChance = true
	case CardTypeFreeze:
		p.Cards = append(p.Cards, card)
		p.Status = StatusFrozen
	case CardTypeFlip3:
		p.Cards = append(p.Cards, card)
		for i := 0; i < 3 && len(g.Deck)+len(g.UsedCards) > 0 && p.Status == StatusActive; i++ {
			g.dealCardTo(p)
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
				g.Message = fmt.Sprintf("%s drew %d (duplicate!) — Second Chance used! Turn ends.", p.Name, card.Value)
				g.advanceTurn()
			} else {
				p.Cards = append(p.Cards, card)
				p.Status = StatusBusted
				g.Message = fmt.Sprintf("%s drew %d — BUSTED! All round points lost.", p.Name, card.Value)
				g.advanceTurn()
			}
		} else {
			p.Cards = append(p.Cards, card)
			g.Message = fmt.Sprintf("%s drew %d.", p.Name, card.Value)
			if p.UniqueNumberCount() == 7 {
				g.triggerFlip7(p)
			} else {
				g.advanceTurn() // one card per turn — pass to next player
			}
		}

	case CardTypeModifierAdd, CardTypeModifierMul:
		p.Cards = append(p.Cards, card)
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
		drawer.Cards = append(drawer.Cards, card)
		target.Status = StatusFrozen
		if target == drawer {
			g.Message = fmt.Sprintf("%s froze themselves — banks %d pts!", drawer.Name, drawer.RoundScore())
		} else {
			g.Message = fmt.Sprintf("%s used Freeze on %s — %s banks %d pts and exits!",
				drawer.Name, target.Name, target.Name, target.RoundScore())
		}
		g.advanceTurn()

	case CardTypeFlip3:
		drawer.Cards = append(drawer.Cards, card)
		if target == drawer {
			g.Message = fmt.Sprintf("%s drew Flip 3 — drawing 3 cards!", drawer.Name)
		} else {
			g.Message = fmt.Sprintf("%s used Flip 3 on %s — %s draws 3 cards!", drawer.Name, target.Name, target.Name)
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
		// Resolve any action cards deferred during the Flip 3 draw.
		for _, dc := range deferred {
			if target.Status == StatusActive && g.Phase == PhasePlaying {
				g.resolveActionAuto(target, dc)
			}
		}
		// After Flip 3 resolves, the drawer's turn ends — pass to next player.
		if g.Phase == PhasePlaying {
			g.advanceTurn()
		}

	case CardTypeSecondChance:
		target.Cards = append(target.Cards, card)
		target.HasSecondChance = true
		if target == drawer {
			g.Message = fmt.Sprintf("%s drew Second Chance — one bust blocked!", drawer.Name)
		} else {
			g.Message = fmt.Sprintf("%s gave Second Chance to %s!", drawer.Name, target.Name)
		}
		g.advanceTurn()
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
				g.Message = fmt.Sprintf("%s drew %d during Flip 3 (duplicate!) — Second Chance used! Draw ends.", p.Name, card.Value)
				return nil, true
			}
			p.Cards = append(p.Cards, card)
			p.Status = StatusBusted
			g.Message = fmt.Sprintf("%s drew %d during Flip 3 — BUSTED!", p.Name, card.Value)
			return nil, true
		}
		p.Cards = append(p.Cards, card)
		g.Message = fmt.Sprintf("%s drew %d (Flip 3).", p.Name, card.Value)
		if p.UniqueNumberCount() == 7 {
			g.triggerFlip7(p)
			return nil, true
		}

	case CardTypeModifierAdd, CardTypeModifierMul:
		p.Cards = append(p.Cards, card)
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
		target.Cards = append(target.Cards, card)
		// Auto-target: first active player other than target.
		targetIdx := g.indexOfPlayer(target)
		nextIdx := g.nextActiveFromExcluding(targetIdx, target)
		if nextIdx < 0 {
			g.Message = fmt.Sprintf("Freeze (deferred) — no other active player to freeze.")
		} else {
			g.Players[nextIdx].Status = StatusFrozen
			g.Message = fmt.Sprintf("Freeze (deferred) — %s banks %d pts and exits!",
				g.Players[nextIdx].Name, g.Players[nextIdx].RoundScore())
		}

	case CardTypeFlip3:
		target.Cards = append(target.Cards, card)
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

	// Find highest score at or above WinScore; collect all tied players.
	topScore := 0
	for _, p := range g.Players {
		if p.TotalScore >= WinScore && p.TotalScore > topScore {
			topScore = p.TotalScore
		}
	}
	var winners []*Player
	if topScore > 0 {
		for _, p := range g.Players {
			if p.TotalScore == topScore {
				winners = append(winners, p)
			}
		}
	}

	if len(winners) > 0 {
		g.Phase = PhaseGameOver
		g.Winners = winners
		names := make([]string, len(winners))
		for i, w := range winners {
			names[i] = fmt.Sprintf("%s (%d pts)", w.Name, w.TotalScore)
		}
		if len(winners) == 1 {
			g.Message = fmt.Sprintf("GAME OVER — %s wins! (%s)",
				winners[0].Name, strings.Join(parts, ", "))
		} else {
			g.Message = fmt.Sprintf("GAME OVER — Tie! %s (%s)",
				strings.Join(names, " & "), strings.Join(parts, ", "))
		}
	} else if flip7Winner != nil {
		g.Message = fmt.Sprintf("FLIP 7 by %s (+15 bonus)! %s — next round soon.",
			flip7Winner.Name, strings.Join(parts, ", "))
	} else {
		g.Message = fmt.Sprintf("Round %d over: %s — next round starting soon.",
			g.RoundNumber, strings.Join(parts, ", "))
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
