package game

import "time"

// PlayerStatus describes a player's state within the current round.
type PlayerStatus string

const (
	StatusActive   PlayerStatus = "active"   // still drawing this round
	StatusStopped  PlayerStatus = "stopped"  // voluntarily banked points and exited
	StatusBusted   PlayerStatus = "busted"   // drew a duplicate number — scores 0
	StatusFrozen   PlayerStatus = "frozen"   // frozen by another player — banks current points and exits
	StatusInactive PlayerStatus = "inactive" // disconnected past the grace window
)

// Player holds all per-player state for the game.
type Player struct {
	ID              string       `json:"id"`
	SessionID       string       `json:"-"` // never sent over the wire
	Name            string       `json:"name"`
	Cards           []Card       `json:"cards"`
	TotalScore      int          `json:"totalScore"`
	Status          PlayerStatus `json:"status"`
	HasSecondChance bool         `json:"hasSecondChance"`
	IsHost          bool         `json:"isHost"`

	// Connection tracking (not serialised).
	Connected      bool      `json:"-"`
	DisconnectedAt time.Time `json:"-"`
}

// RoundScore computes the round score from the player's cards:
//  1. Sum of all number card values.
//  2. Apply ×2 multiplier if held.
//  3. Add +modifier values.
func (p *Player) RoundScore() int {
	numSum := 0
	addSum := 0
	hasMul := false
	for _, c := range p.Cards {
		switch c.Type {
		case CardTypeNumber:
			numSum += c.Value
		case CardTypeModifierAdd:
			addSum += c.Value
		case CardTypeModifierMul:
			hasMul = true
		}
	}
	if hasMul {
		return numSum*2 + addSum
	}
	return numSum + addSum
}

// HasNumber returns true if the player already holds a number card with value v.
func (p *Player) HasNumber(v int) bool {
	for _, c := range p.Cards {
		if c.Type == CardTypeNumber && c.Value == v {
			return true
		}
	}
	return false
}

// UniqueNumberCount returns how many distinct number-card values the player holds.
func (p *Player) UniqueNumberCount() int {
	seen := make(map[int]bool)
	for _, c := range p.Cards {
		if c.Type == CardTypeNumber {
			seen[c.Value] = true
		}
	}
	return len(seen)
}

// ResetForRound clears round-level state while preserving TotalScore and IsHost.
// All players become Active so reconnected players can join the next round.
func (p *Player) ResetForRound() {
	p.Cards = make([]Card, 0)
	p.HasSecondChance = false
	p.Status = StatusActive
}
