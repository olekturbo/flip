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
	Card            Card
	DrawerIdx       int
	ThiefVictim     *Player // non-nil during Thief stage 2 (card selection after player chosen)
	ShufflePartner  *Player // non-nil during Shuffle stage 2 (card pair selection after partner chosen)
}

// PendingActionState is the JSON-serialisable form broadcast to clients.
type PendingActionState struct {
	Card           Card     `json:"card"`
	DrawerID       string   `json:"drawerID"`
	ValidTargetIDs []string `json:"validTargetIDs"`
	// Thief stage 2: victim already chosen, now pick which card to steal.
	ThiefVictimID  string `json:"thiefVictimID,omitempty"`
	StealableCards []Card `json:"stealableCards,omitempty"`
	// Shuffle stage 2: partner already chosen, now pick which cards to exchange.
	ShufflePartnerID    string `json:"shufflePartnerID,omitempty"`
	ShuffleDrawerCards  []Card `json:"shuffleDrawerCards,omitempty"`
	ShufflePartnerCards []Card `json:"shufflePartnerCards,omitempty"`
}

// GameEvent is a structured notification emitted for each significant game action.
// Clients drive animations and sounds from this rather than parsing Message strings.
// Seq is a monotonic counter; clients detect a new event when seq differs from the
// last value they processed.
type GameEvent struct {
	Seq       int    `json:"seq"`
	Type      string `json:"type"`
	PlayerID  string `json:"playerID,omitempty"`
	PlayerID2 string `json:"playerID2,omitempty"` // swap partner / thief victim
	CardName  string `json:"cardName,omitempty"`  // stolen card / swap drawer's card
	CardName2 string `json:"cardName2,omitempty"` // swap partner's card
	CardValue int    `json:"cardValue,omitempty"` // bust / SC saved card value
	Score     int    `json:"score,omitempty"`     // score at voluntary stop
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
	LastEvent          *GameEvent          `json:"lastEvent,omitempty"`
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
	lastEvent      *GameEvent
	eventSeq       int
}

// emit sets the structured last-event broadcast to clients.
func (g *Game) emit(e GameEvent) {
	g.eventSeq++
	e.Seq = g.eventSeq
	g.lastEvent = &e
}

// msg logs the event AND sets the message bar to the same text.
func (g *Game) msg(format string, args ...interface{}) {
	text := fmt.Sprintf(format, args...)
	g.events = append(g.events, text)
	if len(g.events) > 300 {
		g.events = g.events[len(g.events)-300:]
	}
	g.Message = text
}

// logEvent appends to the event history without changing the message bar.
// Use only for structural log entries (e.g. round separators).
func (g *Game) logEvent(format string, args ...interface{}) {
	g.events = append(g.events, fmt.Sprintf(format, args...))
	if len(g.events) > 300 {
		g.events = g.events[len(g.events)-300:]
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

		// Thief always requires two-stage interaction — skip the auto-resolve path.
		if dc.Type == CardTypeThief {
			thiefTargets := g.validThiefTargets(p)
			if len(thiefTargets) == 0 {
				g.msg("Thief (deferred from Flip 3) — no valid target, discarded.")
				g.emit(GameEvent{Type: "thief_discarded", PlayerID: p.ID})
				g.UsedCards = append(g.UsedCards, dc)
			} else {
				g.pending = &pendingAction{Card: dc, DrawerIdx: g.indexOfPlayer(p)}
				g.msg("%s drew Thief during Flip 3 — choose a player to steal from!", p.Name)
				return
			}
			continue
		}

		// Shuffle always requires two-stage interaction — skip the auto-resolve path.
		if dc.Type == CardTypeShuffle {
			targets := g.validShuffleTargets(p)
			if len(targets) == 0 {
				g.msg("Swap (deferred from Flip 3) — no valid swap target, discarded.")
				g.emit(GameEvent{Type: "swap_discarded", PlayerID: p.ID})
				g.UsedCards = append(g.UsedCards, dc)
			} else {
				g.pending = &pendingAction{Card: dc, DrawerIdx: g.indexOfPlayer(p)}
				g.msg("%s drew Swap during Flip 3 — choose a player to swap with!", p.Name)
				return
			}
			continue
		}

		targets := g.validTargetsFor(dc)
		switch len(targets) {
		case 0:
			g.msg("%s (deferred from Flip 3) — no valid target, discarded.", dc.Name)
		case 1:
			g.resolveActionWithTarget(p, dc, g.playerByID(targets[0]))
		default:
			// Require player choice — pause until Target() is called.
			g.pending = &pendingAction{Card: dc, DrawerIdx: g.indexOfPlayer(p)}
			g.msg("%s drew %s during Flip 3 — choose a target!", p.Name, dc.Name)
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
			victim := g.pending.ThiefVictim
			shufflePartner := g.pending.ShufflePartner
			g.pending = nil

			if card.Type == CardTypeThief {
				if victim == nil {
					thiefTargets := g.validThiefTargets(drawer)
					if len(thiefTargets) > 0 {
						victim = g.playerByID(thiefTargets[0])
					}
				}
				if victim != nil {
					stealable := g.stealableCardsFor(drawer, victim)
					if len(stealable) > 0 {
						g.applyThiefSteal(drawer, victim, stealable[0], card)
						if g.Phase == PhasePlaying {
							g.advanceTurn()
						}
					}
				}
			} else if card.Type == CardTypeShuffle {
				if shufflePartner == nil {
					targets := g.validShuffleTargets(drawer)
					if len(targets) > 0 {
						shufflePartner = g.playerByID(targets[0])
					}
				}
				if shufflePartner != nil {
					drawerNums := numberCardsOf(drawer)
					partnerNums := numberCardsOf(shufflePartner)
					if len(drawerNums) > 0 && len(partnerNums) > 0 {
						g.applyShuffleSwap(drawer, shufflePartner, drawerNums[0], partnerNums[0], card)
						if g.Phase == PhasePlaying {
							g.advanceTurn()
						}
					}
				}
			} else {
				targets := g.validTargetsFor(card)
				if len(targets) > 0 {
					g.resolveActionWithTarget(drawer, card, g.playerByID(targets[0]))
				}
			}
			changed = true
		}
	}

	cp = g.currentPlayer()
	if cp != nil && cp.Status == StatusInactive && g.pending == nil {
		g.msg("%s is inactive — turn skipped.", cp.Name)
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
	g.msg("%s stopped — %d pts.", cp.Name, cp.RoundScore())
	g.emit(GameEvent{Type: "stop", PlayerID: cp.ID, Score: cp.RoundScore()})
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
	var validIDs []string
	switch g.pending.Card.Type {
	case CardTypeThief:
		validIDs = g.validThiefTargets(drawer)
	case CardTypeShuffle:
		validIDs = g.validShuffleTargets(drawer)
	default:
		validIDs = g.validTargetsFor(g.pending.Card)
	}
	valid := false
	for _, id := range validIDs {
		if id == targetID {
			valid = true
			break
		}
	}
	if !valid {
		return fmt.Errorf("invalid target for this action")
	}

	card := g.pending.Card

	// Thief stage 1 → 2: victim chosen; await card selection via Steal().
	if card.Type == CardTypeThief && g.pending.ThiefVictim == nil {
		g.pending.ThiefVictim = target
		g.msg("%s chose to steal from %s — pick a card!", drawer.Name, target.Name)
		return nil
	}

	// Shuffle stage 1 → 2: partner chosen; await card pair selection via ShuffleSwap().
	if card.Type == CardTypeShuffle && g.pending.ShufflePartner == nil {
		g.pending.ShufflePartner = target
		g.msg("%s chose to swap with %s — pick cards to exchange!", drawer.Name, target.Name)
		return nil
	}

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

// Steal resolves the card-selection stage (stage 2) of a pending Thief action.
// cardValue is the Value of the number card to steal from the already-chosen victim.
func (g *Game) Steal(sessionID string, cardValue int) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.Phase != PhasePlaying {
		return fmt.Errorf("game is not in progress")
	}
	if g.pending == nil || g.pending.Card.Type != CardTypeThief {
		return fmt.Errorf("no Thief action awaiting card selection")
	}
	if g.pending.ThiefVictim == nil {
		return fmt.Errorf("choose a player to steal from first")
	}
	drawer := g.Players[g.pending.DrawerIdx]
	if drawer.SessionID != sessionID {
		return fmt.Errorf("it is not your action to resolve")
	}

	victim := g.pending.ThiefVictim
	stealable := g.stealableCardsFor(drawer, victim)
	var chosen Card
	found := false
	for _, c := range stealable {
		if c.Value == cardValue {
			chosen = c
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("card %d is not available to steal from %s", cardValue, victim.Name)
	}

	thiefCard := g.pending.Card
	g.pending = nil
	g.applyThiefSteal(drawer, victim, chosen, thiefCard)

	if g.Phase == PhasePlaying {
		if g.deferredFor != nil {
			g.processDeferredCards()
		} else if g.inDealing {
			g.continueDealing()
		} else if !g.inDeferred {
			g.advanceTurn()
		}
	}
	return nil
}

// ShuffleSwap resolves the card-pair selection stage (stage 2) of a pending Shuffle action.
// drawerCardValue is the drawer's number card value to give; partnerCardValue is the partner's.
func (g *Game) ShuffleSwap(sessionID string, drawerCardValue, partnerCardValue int) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.Phase != PhasePlaying {
		return fmt.Errorf("game is not in progress")
	}
	if g.pending == nil || g.pending.Card.Type != CardTypeShuffle {
		return fmt.Errorf("no Swap action awaiting card selection")
	}
	if g.pending.ShufflePartner == nil {
		return fmt.Errorf("choose a player to swap with first")
	}
	drawer := g.Players[g.pending.DrawerIdx]
	if drawer.SessionID != sessionID {
		return fmt.Errorf("it is not your action to resolve")
	}

	partner := g.pending.ShufflePartner

	// Validate drawer's chosen card
	var drawerCard Card
	drawerFound := false
	for _, c := range drawer.Cards {
		if c.Type == CardTypeNumber && c.Value == drawerCardValue {
			drawerCard = c
			drawerFound = true
			break
		}
	}
	if !drawerFound {
		return fmt.Errorf("you do not hold number card %d", drawerCardValue)
	}

	// Validate partner's chosen card
	var partnerCard Card
	partnerFound := false
	for _, c := range partner.Cards {
		if c.Type == CardTypeNumber && c.Value == partnerCardValue {
			partnerCard = c
			partnerFound = true
			break
		}
	}
	if !partnerFound {
		return fmt.Errorf("%s does not hold number card %d", partner.Name, partnerCardValue)
	}

	shuffleCard := g.pending.Card
	g.pending = nil
	g.applyShuffleSwap(drawer, partner, drawerCard, partnerCard, shuffleCard)

	if g.Phase == PhasePlaying {
		if g.deferredFor != nil {
			g.processDeferredCards()
		} else if g.inDealing {
			g.continueDealing()
		} else if !g.inDeferred {
			g.advanceTurn()
		}
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
	g.msg("Game reset — waiting for the host to start.")
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
		switch {
		case g.pending.Card.Type == CardTypeThief && g.pending.ThiefVictim != nil:
			// Stage 2: victim chosen — show stealable cards for selection.
			pendingState = &PendingActionState{
				Card:           g.pending.Card,
				DrawerID:       drawer.ID,
				ThiefVictimID:  g.pending.ThiefVictim.ID,
				StealableCards: g.stealableCardsFor(drawer, g.pending.ThiefVictim),
			}
		case g.pending.Card.Type == CardTypeThief:
			// Stage 1: choose which player to steal from.
			pendingState = &PendingActionState{
				Card:           g.pending.Card,
				DrawerID:       drawer.ID,
				ValidTargetIDs: g.validThiefTargets(drawer),
			}
		case g.pending.Card.Type == CardTypeShuffle && g.pending.ShufflePartner != nil:
			// Stage 2: partner chosen — show number cards for swap selection.
			pendingState = &PendingActionState{
				Card:                g.pending.Card,
				DrawerID:            drawer.ID,
				ShufflePartnerID:    g.pending.ShufflePartner.ID,
				ShuffleDrawerCards:  numberCardsOf(drawer),
				ShufflePartnerCards: numberCardsOf(g.pending.ShufflePartner),
			}
		case g.pending.Card.Type == CardTypeShuffle:
			// Stage 1: choose partner to swap with.
			pendingState = &PendingActionState{
				Card:           g.pending.Card,
				DrawerID:       drawer.ID,
				ValidTargetIDs: g.validShuffleTargets(drawer),
			}
		default:
			pendingState = &PendingActionState{
				Card:           g.pending.Card,
				DrawerID:       drawer.ID,
				ValidTargetIDs: g.validTargetsFor(g.pending.Card),
			}
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
		LastEvent:          g.lastEvent,
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
	g.msg("Round %d — dealing initial cards...", g.RoundNumber)
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
		g.msg("No active players — round skipped.")
		return
	}
	g.msg("Round %d — %s's turn.", g.RoundNumber, g.currentPlayer().Name)
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
			if p.HasSecondChance {
				g.consumeSecondChance(p)
				g.msg("%s was dealt %d — Second Chance saved the bust!", p.Name, card.Value)
				g.emit(GameEvent{Type: "second_chance", PlayerID: p.ID, CardValue: card.Value})
			} else {
				p.Cards = append(p.Cards, card)
				p.Status = StatusBusted
				g.msg("%s was dealt %d — BUSTED!", p.Name, card.Value)
				g.emit(GameEvent{Type: "bust", PlayerID: p.ID, CardValue: card.Value})
			}
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
			g.msg("%s was dealt %s — no valid target, discarded.", p.Name, card.Name)
		case 1:
			g.resolveActionWithTarget(p, card, g.playerByID(targets[0]))
		default:
			g.pending = &pendingAction{Card: card, DrawerIdx: g.indexOfPlayer(p)}
			g.msg("%s was dealt %s — choose a target!", p.Name, card.Name)
		}

	case CardTypeThief:
		targets := g.validThiefTargets(p)
		if len(targets) == 0 {
			g.msg("%s was dealt Thief — no valid target, discarded.", p.Name)
			g.emit(GameEvent{Type: "thief_discarded", PlayerID: p.ID})
			g.UsedCards = append(g.UsedCards, card)
		} else {
			g.pending = &pendingAction{Card: card, DrawerIdx: g.indexOfPlayer(p)}
			g.msg("%s was dealt Thief — choose a player to steal from!", p.Name)
		}

	case CardTypeShuffle:
		targets := g.validShuffleTargets(p)
		if len(targets) == 0 {
			g.msg("%s was dealt Swap — no valid swap target, discarded.", p.Name)
			g.emit(GameEvent{Type: "swap_discarded", PlayerID: p.ID})
			g.UsedCards = append(g.UsedCards, card)
		} else {
			g.pending = &pendingAction{Card: card, DrawerIdx: g.indexOfPlayer(p)}
			g.msg("%s was dealt Swap — choose a player to swap with!", p.Name)
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
		g.msg("All cards drawn — %s is forced to stop.", p.Name)
		g.emit(GameEvent{Type: "stop_forced", PlayerID: p.ID})
		g.advanceTurn()
		return nil
	}

	card := g.pop()

	switch card.Type {
	case CardTypeNumber:
		if p.HasNumber(card.Value) {
			if p.HasSecondChance {
				g.consumeSecondChance(p)
				g.msg("%s drew %d — Second Chance saved the bust! Turn ends.", p.Name, card.Value)
				g.emit(GameEvent{Type: "second_chance", PlayerID: p.ID, CardValue: card.Value})
				g.advanceTurn()
			} else {
				p.Cards = append(p.Cards, card)
				p.Status = StatusBusted
				g.msg("%s drew %d — BUSTED!", p.Name, card.Value)
				g.emit(GameEvent{Type: "bust", PlayerID: p.ID, CardValue: card.Value})
				g.advanceTurn()
			}
		} else {
			p.Cards = append(p.Cards, card)
			g.msg("%s drew %d.", p.Name, card.Value)
			if p.UniqueNumberCount() == 7 {
				g.triggerFlip7(p)
			} else {
				g.advanceTurn() // one card per turn — pass to next player
			}
		}

	case CardTypeModifierAdd, CardTypeModifierMul, CardTypeModifierSub, CardTypeModifierDiv:
		p.Cards = append(p.Cards, card)
		g.msg("%s drew %s.", p.Name, card.Name)
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
			g.msg("%s drew %s — no valid target, discarded.", p.Name, card.Name)
			g.advanceTurn()
		case 1:
			// Only one possible target: auto-resolve immediately.
			g.resolveActionWithTarget(p, card, g.playerByID(targets[0]))
		default:
			g.pending = &pendingAction{Card: card, DrawerIdx: g.CurrentIndex}
			g.msg("%s drew %s — choose a target!", p.Name, card.Name)
		}

	case CardTypeThief:
		if inFlip3 {
			return []Card{card} // defer; don't resolve yet
		}
		targets := g.validThiefTargets(p)
		if len(targets) == 0 {
			g.msg("%s drew Thief — no valid target, discarded.", p.Name)
			g.emit(GameEvent{Type: "thief_discarded", PlayerID: p.ID})
			g.UsedCards = append(g.UsedCards, card)
			g.advanceTurn()
		} else {
			// Always pending: two-stage choice (player, then card).
			g.pending = &pendingAction{Card: card, DrawerIdx: g.CurrentIndex}
			g.msg("%s drew Thief — choose a player to steal from!", p.Name)
		}

	case CardTypeShuffle:
		if inFlip3 {
			return []Card{card} // defer; don't resolve yet
		}
		targets := g.validShuffleTargets(p)
		if len(targets) == 0 {
			g.msg("%s drew Swap — no valid swap target, discarded.", p.Name)
			g.emit(GameEvent{Type: "swap_discarded", PlayerID: p.ID})
			g.UsedCards = append(g.UsedCards, card)
			g.advanceTurn()
		} else {
			// Always pending: two-stage choice (partner, then card pair).
			g.pending = &pendingAction{Card: card, DrawerIdx: g.CurrentIndex}
			g.msg("%s drew Swap — choose a player to swap with!", p.Name)
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
			g.msg("%s froze themselves — banks %d pts!", drawer.Name, drawer.RoundScore())
		} else {
			g.msg("%s used Freeze on %s — %s banks %d pts and exits!",
				drawer.Name, target.Name, target.Name, target.RoundScore())
		}
		g.emit(GameEvent{Type: "freeze", PlayerID: target.ID})
		if !g.inDealing && !g.inDeferred {
			g.advanceTurn()
		}

	case CardTypeFlip3:
		target.Cards = append(target.Cards, card)
		if target == drawer {
			g.msg("%s drew Flip 3 — drawing 3 cards!", drawer.Name)
		} else {
			g.msg("%s used Flip 3 on %s — %s draws 3 cards!", drawer.Name, target.Name, target.Name)
		}
		g.emit(GameEvent{Type: "flip3", PlayerID: target.ID})
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
			g.msg("%s drew Second Chance — one bust blocked!", drawer.Name)
		} else {
			g.msg("%s gave Second Chance to %s!", drawer.Name, target.Name)
		}
		if !g.inDealing && !g.inDeferred {
			g.advanceTurn()
		}

	case CardTypeThief:
		// Auto-steal: used when Thief is resolved via resolveActionWithTarget
		// (e.g. auto-resolution path). Take the first stealable card.
		stealable := g.stealableCardsFor(drawer, target)
		if len(stealable) > 0 {
			g.applyThiefSteal(drawer, target, stealable[0], card)
		} else {
			g.UsedCards = append(g.UsedCards, card)
			g.msg("%s used Thief on %s — nothing to steal, discarded.", drawer.Name, target.Name)
			g.emit(GameEvent{Type: "thief_discarded", PlayerID: drawer.ID})
		}
		if !g.inDealing && !g.inDeferred && g.Phase == PhasePlaying {
			g.advanceTurn()
		}

	case CardTypeShuffle:
		// Auto-swap: take the first number card from each player.
		drawerNums := numberCardsOf(drawer)
		targetNums := numberCardsOf(target)
		if len(drawerNums) > 0 && len(targetNums) > 0 {
			g.applyShuffleSwap(drawer, target, drawerNums[0], targetNums[0], card)
		} else {
			g.UsedCards = append(g.UsedCards, card)
			g.msg("%s used Swap with %s — no cards to swap, discarded.", drawer.Name, target.Name)
			g.emit(GameEvent{Type: "swap_discarded", PlayerID: drawer.ID})
		}
		if !g.inDealing && !g.inDeferred && g.Phase == PhasePlaying {
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
		g.emit(GameEvent{Type: "stop_forced", PlayerID: p.ID})
		return nil, true
	}

	card := g.pop()

	switch card.Type {
	case CardTypeNumber:
		if p.HasNumber(card.Value) {
			if p.HasSecondChance {
				g.consumeSecondChance(p)
				g.msg("%s drew %d during Flip 3 — Second Chance saved the bust! Draws continue.", p.Name, card.Value)
				g.emit(GameEvent{Type: "second_chance", PlayerID: p.ID, CardValue: card.Value})
				return nil, false // SC saves the bust; remaining Flip 3 draws continue
			}
			p.Cards = append(p.Cards, card)
			p.Status = StatusBusted
			g.msg("%s drew %d during Flip 3 — BUSTED!", p.Name, card.Value)
			g.emit(GameEvent{Type: "bust", PlayerID: p.ID, CardValue: card.Value})
			return nil, true // bust stops the Flip 3 sequence
		}
		p.Cards = append(p.Cards, card)
		g.msg("%s drew %d (Flip 3).", p.Name, card.Value)
		if p.UniqueNumberCount() == 7 {
			g.triggerFlip7(p)
			return nil, true
		}

	case CardTypeModifierAdd, CardTypeModifierMul, CardTypeModifierSub, CardTypeModifierDiv:
		p.Cards = append(p.Cards, card)
		g.msg("%s drew %s (Flip 3).", p.Name, card.Name)

	case CardTypeFreeze, CardTypeFlip3, CardTypeSecondChance, CardTypeThief, CardTypeShuffle:
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
			g.msg("Freeze (deferred) — %s banks %d pts and exits!",
				g.Players[nextIdx].Name, g.Players[nextIdx].RoundScore())
			g.emit(GameEvent{Type: "freeze", PlayerID: g.Players[nextIdx].ID})
		} else {
			target.Cards = append(target.Cards, card)
			g.msg("Freeze (deferred) — no active player to freeze.")
		}

	case CardTypeFlip3:
		target.Cards = append(target.Cards, card)
		g.msg("Flip 3 (deferred) — %s draws 3 more cards!", target.Name)
		g.emit(GameEvent{Type: "flip3", PlayerID: target.ID})
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
			g.msg("%s gets Second Chance (deferred from Flip 3)!", target.Name)
		} else {
			g.msg("Second Chance (deferred) discarded — %s already holds one.", target.Name)
		}

	case CardTypeThief:
		// Auto-steal: pick the first stealable card from the first valid opponent.
		for _, opp := range g.Players {
			if opp == target || opp.Status != StatusActive {
				continue
			}
			stealable := g.stealableCardsFor(target, opp)
			if len(stealable) > 0 {
				g.applyThiefSteal(target, opp, stealable[0], card)
				return
			}
		}
		g.UsedCards = append(g.UsedCards, card)
		g.msg("Thief (deferred) — no valid target to steal from, discarded.")
		g.emit(GameEvent{Type: "thief_discarded", PlayerID: target.ID})

	case CardTypeShuffle:
		// Auto-swap: pick the first valid opponent and exchange first number cards.
		for _, opp := range g.Players {
			if opp == target || opp.Status != StatusActive {
				continue
			}
			drawerNums := numberCardsOf(target)
			partnerNums := numberCardsOf(opp)
			if len(drawerNums) > 0 && len(partnerNums) > 0 {
				g.applyShuffleSwap(target, opp, drawerNums[0], partnerNums[0], card)
				return
			}
		}
		g.UsedCards = append(g.UsedCards, card)
		g.msg("Swap (deferred) — no valid swap target, discarded.")
		g.emit(GameEvent{Type: "swap_discarded", PlayerID: target.ID})
	}
}

// validThiefTargets returns IDs of active opponents from whom drawer can steal
// at least one number card they don't already hold.
func (g *Game) validThiefTargets(drawer *Player) []string {
	var ids []string
	for _, p := range g.Players {
		if p == drawer || p.Status != StatusActive {
			continue
		}
		if len(g.stealableCardsFor(drawer, p)) > 0 {
			ids = append(ids, p.ID)
		}
	}
	return ids
}

// stealableCardsFor returns the number cards victim holds that drawer does not.
func (g *Game) stealableCardsFor(drawer, victim *Player) []Card {
	var cards []Card
	for _, c := range victim.Cards {
		if c.Type == CardTypeNumber && !drawer.HasNumber(c.Value) {
			cards = append(cards, c)
		}
	}
	return cards
}

// applyThiefSteal moves stolenCard from victim's hand to thief's hand, discards
// the Thief card, and triggers Flip 7 if thief now holds 7 unique numbers.
func (g *Game) applyThiefSteal(thief, victim *Player, stolenCard, thiefCard Card) {
	for i, c := range victim.Cards {
		if c.Type == CardTypeNumber && c.Value == stolenCard.Value {
			victim.Cards = append(victim.Cards[:i], victim.Cards[i+1:]...)
			break
		}
	}
	thief.Cards = append(thief.Cards, stolenCard)
	thief.Cards = append(thief.Cards, thiefCard)
	g.msg("%s used Thief — stole %s from %s!", thief.Name, stolenCard.Name, victim.Name)
	g.emit(GameEvent{Type: "thief_steal", PlayerID: thief.ID, PlayerID2: victim.ID, CardName: stolenCard.Name})
	if thief.UniqueNumberCount() == 7 {
		g.triggerFlip7(thief)
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
	g.msg("★ %s — FLIP 7! (+15 bonus)", p.Name)
	g.emit(GameEvent{Type: "flip7", PlayerID: p.ID})
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
		g.msg("GAME OVER — %s wins with %d pts! (%s)",
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
			g.msg("FLIP 7 by %s (+15 bonus)! %s  %s — next round soon.",
				flip7Winner.Name, strings.Join(parts, ", "), tieMsg)
		} else {
			g.msg("Round %d: %s  %s — next round soon.",
				g.RoundNumber, strings.Join(parts, ", "), tieMsg)
		}

	default:
		// Nobody at 200+ yet — normal round end.
		if flip7Winner != nil {
			g.msg("FLIP 7 by %s (+15 bonus)! %s — next round soon.",
				flip7Winner.Name, strings.Join(parts, ", "))
		} else {
			g.msg("Round %d over: %s — next round starting soon.",
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

// ─── Shuffle helpers ──────────────────────────────────────────────────────────

// numberCardsOf returns a copy of all number cards in p's hand.
func numberCardsOf(p *Player) []Card {
	var cards []Card
	for _, c := range p.Cards {
		if c.Type == CardTypeNumber {
			cards = append(cards, c)
		}
	}
	return cards
}

// validShuffleTargets returns IDs of active players (excluding drawer) who have
// at least one number card, when the drawer also has at least one number card.
func (g *Game) validShuffleTargets(drawer *Player) []string {
	if len(numberCardsOf(drawer)) == 0 {
		return nil
	}
	var ids []string
	for _, p := range g.Players {
		if p == drawer || p.Status != StatusActive {
			continue
		}
		if len(numberCardsOf(p)) > 0 {
			ids = append(ids, p.ID)
		}
	}
	return ids
}

// applyShuffleSwap exchanges drawerCard and partnerCard between drawer and partner,
// keeps the Shuffle card in drawer's hand as a visible marker, and triggers
// Flip 7 if either player now holds 7 unique number cards.
func (g *Game) applyShuffleSwap(drawer, partner *Player, drawerCard, partnerCard, shuffleCard Card) {
	// Remove drawer's card from drawer's hand.
	for i, c := range drawer.Cards {
		if c.Type == CardTypeNumber && c.Value == drawerCard.Value {
			drawer.Cards = append(drawer.Cards[:i], drawer.Cards[i+1:]...)
			break
		}
	}
	// Remove partner's card from partner's hand.
	for i, c := range partner.Cards {
		if c.Type == CardTypeNumber && c.Value == partnerCard.Value {
			partner.Cards = append(partner.Cards[:i], partner.Cards[i+1:]...)
			break
		}
	}
	// Cross-give the cards.
	drawer.Cards = append(drawer.Cards, partnerCard)
	partner.Cards = append(partner.Cards, drawerCard)
	// Shuffle card stays in drawer's hand as a visible marker.
	drawer.Cards = append(drawer.Cards, shuffleCard)

	g.msg("%s used Swap — swapped %s with %s's %s!", drawer.Name, drawerCard.Name, partner.Name, partnerCard.Name)
	g.emit(GameEvent{Type: "swap_success", PlayerID: drawer.ID, PlayerID2: partner.ID, CardName: drawerCard.Name, CardName2: partnerCard.Name})

	// Check Flip 7 for drawer first (they initiated the action), then partner.
	if drawer.UniqueNumberCount() == 7 {
		g.triggerFlip7(drawer)
	} else if partner.UniqueNumberCount() == 7 {
		g.triggerFlip7(partner)
	}
}
