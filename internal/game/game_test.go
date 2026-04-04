package game

import "testing"

// makeGame creates a 2-player game already in the Playing phase, bypassing the
// dealing phase. Alice (index 0) is the current player; Bob (index 1) goes next.
// The provided deck slice is set as the draw pile in order (index 0 = next drawn).
func makeGame(aliceCards, bobCards []Card, deck ...Card) (*Game, *Player, *Player) {
	alice := &Player{
		ID: "alice", SessionID: "sa", Name: "Alice",
		Cards: aliceCards, Status: StatusActive,
		Connected: true, IsHost: true,
	}
	bob := &Player{
		ID: "bob", SessionID: "sb", Name: "Bob",
		Cards: bobCards, Status: StatusActive,
		Connected: true,
	}
	g := &Game{
		ID:           "room",
		Phase:        PhasePlaying,
		RoundNumber:  1,
		DealerIndex:  1, // Bob dealt → Alice goes first
		CurrentIndex: 0,
		Deck:         append([]Card{}, deck...),
		Players:      []*Player{alice, bob},
	}
	return g, alice, bob
}

// ─── Draw ────────────────────────────────────────────────────────────────────

func TestDrawNumberCard(t *testing.T) {
	g, alice, bob := makeGame(
		[]Card{NumberCard(3)},       // Alice's hand
		[]Card{NumberCard(5)},       // Bob's hand
		NumberCard(8),               // Alice draws this
	)

	if err := g.Draw("sa"); err != nil {
		t.Fatalf("Draw() error: %v", err)
	}

	if alice.Status != StatusActive {
		t.Errorf("Alice status = %s, want active", alice.Status)
	}
	if !alice.HasNumber(8) {
		t.Error("Alice should have drawn 8")
	}
	// Turn should have passed to Bob
	if g.CurrentIndex != 1 {
		t.Errorf("CurrentIndex = %d, want 1 (Bob)", g.CurrentIndex)
	}
	_ = bob
}

func TestDrawWrongTurnReturnsError(t *testing.T) {
	g, _, _ := makeGame(nil, nil, NumberCard(1))

	if err := g.Draw("sb"); err == nil {
		t.Error("Draw() on wrong turn should return error")
	}
}

// ─── Bust ────────────────────────────────────────────────────────────────────

func TestDrawDuplicateBust(t *testing.T) {
	g, alice, _ := makeGame(
		[]Card{NumberCard(5)}, // Alice already has 5
		nil,
		NumberCard(5), // Alice draws another 5 → bust
	)

	if err := g.Draw("sa"); err != nil {
		t.Fatalf("Draw() error: %v", err)
	}

	if alice.Status != StatusBusted {
		t.Errorf("Alice status = %s, want busted", alice.Status)
	}
}

func TestBustedPlayerScoresZero(t *testing.T) {
	p := &Player{
		Cards:  []Card{NumberCard(5), NumberCard(5)},
		Status: StatusBusted,
	}
	// RoundScore still returns the sum; it's endRound that zeroes busted players.
	// Test that the endRound mechanism zeroes correctly via score accumulation.
	g := &Game{
		Phase:   PhaseRoundEnd,
		Players: []*Player{p},
	}
	// endRound zeroes busted players — verify via TotalScore after endRound
	p.Status = StatusBusted
	g.endRound(nil)
	if p.TotalScore != 0 {
		t.Errorf("busted player TotalScore = %d, want 0", p.TotalScore)
	}
}

// ─── Second Chance ───────────────────────────────────────────────────────────

func TestSecondChanceSavesBust(t *testing.T) {
	g, alice, _ := makeGame(
		[]Card{NumberCard(5), SecondChanceCard()}, // Alice has 5 + SC
		nil,
		NumberCard(5), // duplicate → SC should save her
	)
	alice.HasSecondChance = true

	if err := g.Draw("sa"); err != nil {
		t.Fatalf("Draw() error: %v", err)
	}

	if alice.Status == StatusBusted {
		t.Error("Alice should NOT be busted — Second Chance should save her")
	}
	if alice.HasSecondChance {
		t.Error("Second Chance should be consumed")
	}
	// Turn should advance after SC use
	if g.CurrentIndex != 1 {
		t.Errorf("CurrentIndex = %d, want 1 (Bob) after SC save", g.CurrentIndex)
	}
}

func TestSecondChanceConsumedFromHand(t *testing.T) {
	g, alice, _ := makeGame(
		[]Card{NumberCard(3), SecondChanceCard()},
		nil,
		NumberCard(3),
	)
	alice.HasSecondChance = true

	_ = g.Draw("sa")

	for _, c := range alice.Cards {
		if c.Type == CardTypeSecondChance {
			t.Error("Second Chance card should be removed from Alice's hand after use")
		}
	}
}

// ─── Stop ────────────────────────────────────────────────────────────────────

func TestStop(t *testing.T) {
	g, alice, _ := makeGame([]Card{NumberCard(7)}, nil)

	if err := g.Stop("sa"); err != nil {
		t.Fatalf("Stop() error: %v", err)
	}

	if alice.Status != StatusStopped {
		t.Errorf("Alice status = %s, want stopped", alice.Status)
	}
	if g.CurrentIndex != 1 {
		t.Errorf("CurrentIndex = %d, want 1 (Bob) after Alice stops", g.CurrentIndex)
	}
}

func TestStopWhenNotYourTurnReturnsError(t *testing.T) {
	g, _, _ := makeGame(nil, nil)

	if err := g.Stop("sb"); err == nil {
		t.Error("Stop() on wrong turn should return error")
	}
}

// ─── Round end — all players inactive ────────────────────────────────────────

func TestRoundEndsWhenNoActivePlayers(t *testing.T) {
	g, alice, bob := makeGame(
		[]Card{NumberCard(4)},
		[]Card{NumberCard(6)},
	)

	// Both stop — last stop triggers endRound
	_ = g.Stop("sa") // Alice stops → Bob's turn
	_ = g.Stop("sb") // Bob stops → no active players → round ends

	if g.Phase != PhaseRoundEnd {
		t.Errorf("Phase = %s, want round_end", g.Phase)
	}
	if alice.TotalScore != 4 {
		t.Errorf("Alice TotalScore = %d, want 4", alice.TotalScore)
	}
	if bob.TotalScore != 6 {
		t.Errorf("Bob TotalScore = %d, want 6", bob.TotalScore)
	}
}

// ─── Freeze ──────────────────────────────────────────────────────────────────

func TestFreezeAutoTargetSelf(t *testing.T) {
	// When there is only one active player (Alice), Freeze auto-targets her.
	g, alice, bob := makeGame(
		[]Card{NumberCard(5)},
		[]Card{NumberCard(3)},
		FreezeCard(),
	)
	bob.Status = StatusStopped // only Alice active

	_ = g.Draw("sa") // draws Freeze — single valid target (Alice herself) → auto-resolve

	if g.pending != nil {
		t.Error("expected no pending action (should auto-resolve with single target)")
	}
	if alice.Status != StatusFrozen {
		t.Errorf("Alice status = %s, want frozen", alice.Status)
	}
}

func TestFreezePendingWhenMultipleTargets(t *testing.T) {
	g, _, _ := makeGame(
		[]Card{NumberCard(5)},
		[]Card{NumberCard(3)},
		FreezeCard(),
	)

	_ = g.Draw("sa") // draws Freeze — both players active → pending

	if g.pending == nil {
		t.Error("expected pending action when multiple valid targets for Freeze")
	}
	if g.pending.Card.Type != CardTypeFreeze {
		t.Errorf("pending card type = %s, want freeze", g.pending.Card.Type)
	}
}

func TestFreezeTargetViaTarget(t *testing.T) {
	g, _, bob := makeGame(
		[]Card{NumberCard(5)},
		[]Card{NumberCard(9)},
		FreezeCard(),
	)

	_ = g.Draw("sa")                  // Alice draws Freeze → pending
	_ = g.Target("sa", "bob")         // Alice targets Bob

	if bob.Status != StatusFrozen {
		t.Errorf("Bob status = %s, want frozen", bob.Status)
	}
	if g.pending != nil {
		t.Error("pending should be cleared after Target()")
	}
}

// ─── Second Chance (action card) ─────────────────────────────────────────────

func TestSecondChanceGivenToTarget(t *testing.T) {
	g, _, bob := makeGame(
		[]Card{NumberCard(2)},
		[]Card{NumberCard(4)},
		SecondChanceCard(),
	)

	_ = g.Draw("sa") // Alice draws 2nd Chance → pending

	if g.pending == nil {
		t.Fatal("expected pending action for Second Chance")
	}

	_ = g.Target("sa", "bob") // Alice gives it to Bob

	if !bob.HasSecondChance {
		t.Error("Bob should have Second Chance after being targeted")
	}
}

func TestSecondChanceCannotStackOnSamePlayer(t *testing.T) {
	g, _, bob := makeGame(
		[]Card{NumberCard(2)},
		[]Card{NumberCard(4), SecondChanceCard()},
		SecondChanceCard(),
	)
	bob.HasSecondChance = true

	_ = g.Draw("sa") // Alice draws 2nd Chance

	// Bob already has SC → he should not be a valid target
	if g.pending != nil {
		for _, id := range g.pending.Card.Type.validTargetCheck(g) {
			if id == "bob" {
				t.Error("Bob should not be a valid SC target — he already holds one")
			}
		}
	}
}

// validTargetCheck is a test helper to call validTargetsFor without touching unexported pending.
func (ct CardType) validTargetCheck(g *Game) []string {
	return g.validTargetsFor(Card{Type: ct})
}

// ─── Flip 3 ──────────────────────────────────────────────────────────────────

func TestFlip3DrawsThreeCards(t *testing.T) {
	g, _, bob := makeGame(
		[]Card{NumberCard(1)},
		[]Card{NumberCard(2)},
		Flip3Card(),
		// Bob will draw these 3 cards during Flip 3
		NumberCard(4), NumberCard(6), NumberCard(8),
	)

	_ = g.Draw("sa") // Alice draws Flip 3 → both valid → pending
	_ = g.Target("sa", "bob") // Alice targets Bob

	// Bob should have drawn 4, 6, 8 in addition to his starting 2
	if !bob.HasNumber(4) || !bob.HasNumber(6) || !bob.HasNumber(8) {
		t.Errorf("Bob's cards after Flip 3: %v — expected 4, 6, 8", bob.Cards)
	}
}

func TestFlip3BustsTargetOnDuplicate(t *testing.T) {
	g, _, bob := makeGame(
		[]Card{NumberCard(1)},
		[]Card{NumberCard(5)},          // Bob has 5
		Flip3Card(),
		NumberCard(3),                   // first Flip 3 draw — ok
		NumberCard(5),                   // second Flip 3 draw — duplicate → bust
		NumberCard(9),                   // should NOT be drawn (Flip 3 stops on bust)
	)

	_ = g.Draw("sa")
	_ = g.Target("sa", "bob")

	if bob.Status != StatusBusted {
		t.Errorf("Bob status = %s, want busted after Flip 3 duplicate", bob.Status)
	}
	if bob.HasNumber(9) {
		t.Error("card 9 should not be drawn — Flip 3 stops on bust")
	}
}

func TestFlip3SecondChanceSavesTarget(t *testing.T) {
	g, _, bob := makeGame(
		[]Card{NumberCard(1)},
		[]Card{NumberCard(5), SecondChanceCard()}, // Bob has 5 + SC
		Flip3Card(),
		NumberCard(3),
		NumberCard(5), // duplicate during Flip 3 — SC should save Bob
		NumberCard(9), // should NOT be drawn (SC stops remaining Flip 3 draws)
	)
	bob.HasSecondChance = true

	_ = g.Draw("sa")
	_ = g.Target("sa", "bob")

	if bob.Status == StatusBusted {
		t.Error("Bob should NOT be busted — Second Chance should save him during Flip 3")
	}
	if bob.HasSecondChance {
		t.Error("Bob's Second Chance should be consumed")
	}
	if bob.HasNumber(9) {
		t.Error("card 9 should not be drawn — Flip 3 stops after SC use")
	}
}

// ─── Flip 7 ──────────────────────────────────────────────────────────────────

func TestFlip7EndsRoundWithBonus(t *testing.T) {
	// Alice has 6 unique numbers; drawing the 7th triggers Flip 7.
	g, alice, bob := makeGame(
		[]Card{NumberCard(1), NumberCard(2), NumberCard(3), NumberCard(4), NumberCard(5), NumberCard(6)},
		[]Card{NumberCard(8)},
		NumberCard(7), // Alice draws 7 → Flip 7!
	)

	_ = g.Draw("sa")

	if g.Phase != PhaseRoundEnd {
		t.Errorf("Phase = %s, want round_end after Flip 7", g.Phase)
	}
	// Flip 7 winner: score = (1+2+3+4+5+6+7) + 15 bonus = 28 + 15 = 43
	if alice.TotalScore != 43 {
		t.Errorf("Alice TotalScore = %d, want 43 (28 + 15 Flip 7 bonus)", alice.TotalScore)
	}
	// Bob should bank his current score
	if bob.TotalScore != 8 {
		t.Errorf("Bob TotalScore = %d, want 8 (banked on Flip 7)", bob.TotalScore)
	}
	if g.flip7WinnerID != alice.ID {
		t.Error("flip7WinnerID should be Alice's ID")
	}
}

func TestFlip7BobStatusStopped(t *testing.T) {
	g, _, bob := makeGame(
		[]Card{NumberCard(1), NumberCard(2), NumberCard(3), NumberCard(4), NumberCard(5), NumberCard(6)},
		[]Card{NumberCard(8)},
		NumberCard(7),
	)

	_ = g.Draw("sa")

	if bob.Status != StatusStopped {
		t.Errorf("Bob status = %s, want stopped after opponent's Flip 7", bob.Status)
	}
}

// ─── Win condition ───────────────────────────────────────────────────────────

func TestGameOverWhenPlayerReaches200(t *testing.T) {
	g, alice, _ := makeGame(
		[]Card{NumberCard(10)}, // Alice's hand: 10
		[]Card{NumberCard(5)},
	)
	alice.TotalScore = 190 // Alice needs +10 to win

	_ = g.Stop("sa") // banks 10 → total = 200
	_ = g.Stop("sb")

	if g.Phase != PhaseGameOver {
		t.Errorf("Phase = %s, want game_over when a player reaches 200", g.Phase)
	}
	if len(g.Winners) != 1 || g.Winners[0] != alice {
		t.Error("Alice should be the winner")
	}
}

func TestTieAt200ContinuesGame(t *testing.T) {
	g, alice, bob := makeGame(
		[]Card{NumberCard(10)},
		[]Card{NumberCard(10)},
	)
	alice.TotalScore = 190
	bob.TotalScore = 190

	_ = g.Stop("sa") // Alice banks 10 → 200
	_ = g.Stop("sb") // Bob banks 10 → 200

	// Both at 200 — tie, game should continue (phase = round_end, not game_over)
	if g.Phase == PhaseGameOver {
		t.Error("game should NOT be over when multiple players are tied at 200+")
	}
	if g.Phase != PhaseRoundEnd {
		t.Errorf("Phase = %s, want round_end for tie continuation", g.Phase)
	}
}

func TestStrictlyHighestWins(t *testing.T) {
	g, alice, bob := makeGame(
		[]Card{NumberCard(12)}, // Alice banks 12 → 212
		[]Card{NumberCard(5)},  // Bob banks 5 → 205
	)
	alice.TotalScore = 200
	bob.TotalScore = 200

	_ = g.Stop("sa")
	_ = g.Stop("sb")

	if g.Phase != PhaseGameOver {
		t.Errorf("Phase = %s, want game_over — Alice is strictly highest", g.Phase)
	}
	if len(g.Winners) != 1 || g.Winners[0] != alice {
		t.Error("Alice should win with strictly higher score")
	}
}

// ─── Dealing phase: Second Chance ────────────────────────────────────────────

func TestDealingPhaseSecondChanceSavesBust(t *testing.T) {
	// Simulate the dealing-phase SC check directly.
	g := &Game{
		ID:          "room",
		Phase:       PhasePlaying,
		RoundNumber: 1,
		inDealing:   true,
	}
	alice := &Player{
		ID: "alice", SessionID: "sa", Name: "Alice",
		Cards:           []Card{NumberCard(7), SecondChanceCard()},
		Status:          StatusActive,
		HasSecondChance: true,
	}
	g.Players = []*Player{alice}
	// Put a duplicate 7 at the top of the deck
	g.Deck = []Card{NumberCard(7)}

	g.dealCardTo(alice)

	if alice.Status == StatusBusted {
		t.Error("Alice should NOT be busted — Second Chance should prevent dealing bust")
	}
	if alice.HasSecondChance {
		t.Error("Second Chance should be consumed after dealing bust save")
	}
}

// ─── Valid targets ────────────────────────────────────────────────────────────

func TestValidTargetsExcludeInactivePlayers(t *testing.T) {
	g, _, bob := makeGame(nil, nil)
	bob.Status = StatusStopped

	targets := g.validTargetsFor(FreezeCard())

	for _, id := range targets {
		if id == "bob" {
			t.Error("stopped Bob should not be a valid Freeze target")
		}
	}
}

func TestValidTargetsSecondChanceExcludesHolders(t *testing.T) {
	g, _, bob := makeGame(nil, nil)
	bob.HasSecondChance = true

	targets := g.validTargetsFor(SecondChanceCard())

	for _, id := range targets {
		if id == "bob" {
			t.Error("Bob already has Second Chance and should not be a valid target")
		}
	}
}

// ─── Player management ───────────────────────────────────────────────────────

func TestAddPlayerIncreasesCount(t *testing.T) {
	g := New("room")
	if _, err := g.AddPlayer("s1", "Alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := g.AddPlayer("s2", "Bob"); err != nil {
		t.Fatal(err)
	}
	if len(g.Players) != 2 {
		t.Errorf("Players count = %d, want 2", len(g.Players))
	}
}

func TestFirstPlayerIsHost(t *testing.T) {
	g := New("room")
	p, _ := g.AddPlayer("s1", "Alice")
	if !p.IsHost {
		t.Error("first player should be host")
	}
}

func TestCannotAddPlayerToStartedGame(t *testing.T) {
	g := New("room")
	g.AddPlayer("s1", "Alice")
	g.AddPlayer("s2", "Bob")
	g.Start("s1")

	_, err := g.AddPlayer("s3", "Carol")
	if err == nil {
		t.Error("AddPlayer should fail once game has started")
	}
}

func TestRoomFullRejectsPlayer(t *testing.T) {
	g := New("room")
	for i := 0; i < MaxPlayers; i++ {
		g.AddPlayer(newUUID(), "p")
	}
	_, err := g.AddPlayer("extra", "Extra")
	if err == nil {
		t.Errorf("should reject player beyond MaxPlayers (%d)", MaxPlayers)
	}
}

func TestStartRequiresAtLeastTwoPlayers(t *testing.T) {
	g := New("room")
	g.AddPlayer("s1", "Alice")
	if err := g.Start("s1"); err == nil {
		t.Error("Start() with 1 player should fail")
	}
}

func TestReconnectingSameSessionReusesPlayer(t *testing.T) {
	g := New("room")
	p1, _ := g.AddPlayer("s1", "Alice")
	g.AddPlayer("s2", "Bob")

	// Simulate reconnect with same session
	p2, ok := g.Rejoin("s1")
	if !ok {
		t.Fatal("Rejoin should succeed for known session")
	}
	if p1 != p2 {
		t.Error("Rejoin should return the same player pointer")
	}
}

// ─── Thief ───────────────────────────────────────────────────────────────────

// TestThiefStealsCard: drawing Thief creates pending stage-1, Target selects
// victim (stage-2), Steal moves the chosen card from victim to thief.
func TestThiefStealsCard(t *testing.T) {
	g, alice, bob := makeGame(
		[]Card{NumberCard(3)},            // Alice's hand
		[]Card{NumberCard(5), NumberCard(7)}, // Bob's hand
		ThiefCard(),                      // Alice draws this
	)

	// Alice draws the Thief card.
	if err := g.Draw("sa"); err != nil {
		t.Fatalf("Draw() error: %v", err)
	}

	// Should be pending stage 1 (player selection).
	if g.pending == nil || g.pending.Card.Type != CardTypeThief {
		t.Fatal("expected Thief pending action after draw")
	}
	if g.pending.ThiefVictim != nil {
		t.Error("ThiefVictim should be nil at stage 1")
	}

	// Alice selects Bob as victim.
	if err := g.Target("sa", "bob"); err != nil {
		t.Fatalf("Target() error: %v", err)
	}
	if g.pending == nil || g.pending.ThiefVictim != bob {
		t.Fatal("expected ThiefVictim = bob after Target()")
	}

	// Alice steals Bob's 5.
	if err := g.Steal("sa", 5); err != nil {
		t.Fatalf("Steal() error: %v", err)
	}

	if !alice.HasNumber(5) {
		t.Error("Alice should have stolen 5 from Bob")
	}
	if bob.HasNumber(5) {
		t.Error("Bob should no longer have 5")
	}
	if g.pending != nil {
		t.Error("pending should be nil after steal resolves")
	}
	// Turn should have advanced to Bob.
	if g.CurrentIndex != 1 {
		t.Errorf("CurrentIndex = %d, want 1 (Bob)", g.CurrentIndex)
	}
}

// TestThiefCannotStealDuplicateNumber: a card Alice already holds is not offered.
func TestThiefCannotStealDuplicateNumber(t *testing.T) {
	g, _, _ := makeGame(
		[]Card{NumberCard(5)},   // Alice already has 5
		[]Card{NumberCard(5)},   // Bob also has 5
		ThiefCard(),
	)

	if err := g.Draw("sa"); err != nil {
		t.Fatalf("Draw() error: %v", err)
	}

	// Bob has 5, but Alice already has 5 — no stealable cards → Thief discarded automatically.
	if g.pending != nil {
		t.Errorf("expected Thief to be discarded (no valid steal), got pending %+v", g.pending)
	}
}

// TestThiefNoOpponentNumbers: Thief is discarded when the only opponent has no number cards.
func TestThiefNoOpponentNumbers(t *testing.T) {
	g, _, _ := makeGame(
		[]Card{NumberCard(3)}, // Alice
		[]Card{},              // Bob has no number cards
		ThiefCard(),
	)

	if err := g.Draw("sa"); err != nil {
		t.Fatalf("Draw() error: %v", err)
	}

	if g.pending != nil {
		t.Errorf("expected Thief discarded (opponent has no stealable cards), got pending")
	}
}

// TestThiefInvalidCardValueReturnsError: Steal() with a value not in stealable list fails.
func TestThiefInvalidCardValueReturnsError(t *testing.T) {
	g, _, _ := makeGame(
		[]Card{NumberCard(3)},
		[]Card{NumberCard(7)},
		ThiefCard(),
	)

	_ = g.Draw("sa")
	_ = g.Target("sa", "bob")

	if err := g.Steal("sa", 9); err == nil {
		t.Error("Steal() with invalid card value should return error")
	}
}

// TestThiefTriggersFlip7: stealing the 7th unique number card triggers Flip 7.
func TestThiefTriggersFlip7(t *testing.T) {
	g, alice, bob := makeGame(
		[]Card{NumberCard(1), NumberCard(2), NumberCard(3), NumberCard(4), NumberCard(5), NumberCard(6)},
		[]Card{NumberCard(7)},
		ThiefCard(),
	)

	_ = g.Draw("sa")
	_ = g.Target("sa", "bob")
	if err := g.Steal("sa", 7); err != nil {
		t.Fatalf("Steal() error: %v", err)
	}

	if alice.UniqueNumberCount() != 7 {
		t.Errorf("Alice should have 7 unique numbers, has %d", alice.UniqueNumberCount())
	}
	// Flip 7 ends the round.
	if g.Phase != PhaseRoundEnd {
		t.Errorf("Phase = %s, want round_end after Flip 7 via Thief", g.Phase)
	}
	_ = bob
}

// TestThiefStealBeforeTargetReturnsError: calling Steal() before Target() fails.
func TestThiefStealBeforeTargetReturnsError(t *testing.T) {
	g, _, _ := makeGame(
		[]Card{NumberCard(3)},
		[]Card{NumberCard(7)},
		ThiefCard(),
	)

	_ = g.Draw("sa")
	// pending is at stage 1; no ThiefVictim yet.
	if err := g.Steal("sa", 7); err == nil {
		t.Error("Steal() before Target() should return error")
	}
}
