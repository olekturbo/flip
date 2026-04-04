package game

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/cucumber/godog"
)

// ── scenario context ──────────────────────────────────────────────────────────
// One bddCtx is created per scenario; state accumulates across steps.

type bddCtx struct {
	// Standalone player used only in scoring scenarios (no full game needed).
	sp *Player

	// 2-player game.
	g          *Game
	alice      *Player
	bob        *Player
	aliceCards []Card
	bobCards   []Card
	deckCards  []Card

	// Pre-game state applied when ensureGame() is first called.
	aliceInitScore  int
	bobInitScore    int
	aliceInitStatus PlayerStatus
	bobInitStatus   PlayerStatus
}

// ensureGame lazily creates the 2-player game the first time a When step runs.
// Given steps only store values in the context; this function applies them.
func (c *bddCtx) ensureGame() {
	if c.g != nil {
		return
	}
	alice := &Player{
		ID:         "alice",
		SessionID:  "sa",
		Name:       "Alice",
		Cards:      append([]Card{}, c.aliceCards...),
		TotalScore: c.aliceInitScore,
		Status:     StatusActive,
		Connected:  true,
		IsHost:     true,
	}
	bob := &Player{
		ID:         "bob",
		SessionID:  "sb",
		Name:       "Bob",
		Cards:      append([]Card{}, c.bobCards...),
		TotalScore: c.bobInitScore,
		Status:     StatusActive,
		Connected:  true,
	}
	if c.aliceInitStatus != "" {
		alice.Status = c.aliceInitStatus
	}
	if c.bobInitStatus != "" {
		bob.Status = c.bobInitStatus
	}
	// Auto-derive HasSecondChance from cards — mirrors what the game does in play.
	for _, card := range alice.Cards {
		if card.Type == CardTypeSecondChance {
			alice.HasSecondChance = true
			break
		}
	}
	for _, card := range bob.Cards {
		if card.Type == CardTypeSecondChance {
			bob.HasSecondChance = true
			break
		}
	}
	c.g = &Game{
		ID:           "bdd",
		Phase:        PhasePlaying,
		RoundNumber:  1,
		DealerIndex:  1, // Bob dealt last → Alice goes first
		CurrentIndex: 0,
		Deck:         append([]Card{}, c.deckCards...),
		Players:      []*Player{alice, bob},
	}
	c.alice = alice
	c.bob = bob
}

// ── card parsing ──────────────────────────────────────────────────────────────

func bddParseCard(s string) Card {
	switch s {
	case "Freeze":
		return FreezeCard()
	case "Flip 3":
		return Flip3Card()
	case "2nd Chance":
		return SecondChanceCard()
	case "×2":
		return ModifierMulCard()
	case "÷2":
		return ModifierDivCard()
	}
	if strings.HasPrefix(s, "+") {
		v, _ := strconv.Atoi(s[1:])
		return ModifierAddCard(v)
	}
	if strings.HasPrefix(s, "-") {
		v, _ := strconv.Atoi(s[1:])
		return ModifierSubCard(v)
	}
	v, _ := strconv.Atoi(s)
	return NumberCard(v)
}

func bddParseCards(list string) []Card {
	if strings.TrimSpace(list) == "" {
		return nil
	}
	var cards []Card
	for _, part := range strings.Split(list, ",") {
		if name := strings.TrimSpace(part); name != "" {
			cards = append(cards, bddParseCard(name))
		}
	}
	return cards
}

func bddCardNames(cards []Card) []string {
	names := make([]string, len(cards))
	for i, c := range cards {
		names[i] = c.Name
	}
	return names
}

// ── step definitions — scoring ────────────────────────────────────────────────

func (c *bddCtx) aPlayerHolds(list string) error {
	c.sp = &Player{Cards: bddParseCards(list)}
	return nil
}

func (c *bddCtx) theirRoundScoreIs(expected int) error {
	if c.sp == nil {
		return fmt.Errorf("no standalone player set up — use 'a player holds [...]'")
	}
	if got := c.sp.RoundScore(); got != expected {
		return fmt.Errorf("round score: got %d, want %d (cards: %v)", got, expected, bddCardNames(c.sp.Cards))
	}
	return nil
}

// ── step definitions — game setup (Given) ────────────────────────────────────

func (c *bddCtx) aliceHasCards(list string) error {
	c.aliceCards = bddParseCards(list)
	return nil
}

func (c *bddCtx) bobHasCards(list string) error {
	c.bobCards = bddParseCards(list)
	return nil
}

// "Alice has [...] and a Second Chance" — list should NOT include 2nd Chance;
// it is appended here and the HasSecondChance flag derived in ensureGame.
func (c *bddCtx) aliceHasCardsAndSC(list string) error {
	c.aliceCards = append(bddParseCards(list), SecondChanceCard())
	return nil
}

func (c *bddCtx) bobHasCardsAndSC(list string) error {
	c.bobCards = append(bddParseCards(list), SecondChanceCard())
	return nil
}

func (c *bddCtx) deckIs(list string) error {
	c.deckCards = bddParseCards(list)
	return nil
}

// aliceTotalScoreStep doubles as Given (stores initial score) and Then (asserts).
func (c *bddCtx) aliceTotalScoreStep(expected int) error {
	if c.alice == nil {
		c.aliceInitScore = expected
		return nil
	}
	if c.alice.TotalScore != expected {
		return fmt.Errorf("Alice total score: got %d, want %d", c.alice.TotalScore, expected)
	}
	return nil
}

func (c *bddCtx) bobTotalScoreStep(expected int) error {
	if c.bob == nil {
		c.bobInitScore = expected
		return nil
	}
	if c.bob.TotalScore != expected {
		return fmt.Errorf("Bob total score: got %d, want %d", c.bob.TotalScore, expected)
	}
	return nil
}

// ── step definitions — actions (When) ────────────────────────────────────────

func (c *bddCtx) aliceDraws() error {
	c.ensureGame()
	return c.g.Draw("sa")
}

func (c *bddCtx) bobDraws() error {
	c.ensureGame()
	return c.g.Draw("sb")
}

func (c *bddCtx) aliceStops() error {
	c.ensureGame()
	return c.g.Stop("sa")
}

func (c *bddCtx) bobStops() error {
	c.ensureGame()
	return c.g.Stop("sb")
}

func (c *bddCtx) aliceTargetsBob() error {
	c.ensureGame()
	return c.g.Target("sa", "bob")
}

func (c *bddCtx) aliceTargetsSelf() error {
	c.ensureGame()
	return c.g.Target("sa", "alice")
}

func (c *bddCtx) bobTargetsAlice() error {
	c.ensureGame()
	return c.g.Target("sb", "alice")
}

func (c *bddCtx) bobTargetsSelf() error {
	c.ensureGame()
	return c.g.Target("sb", "bob")
}

// ── step definitions — status (Given + Then) ─────────────────────────────────
// These work as Given (setup, before ensureGame) and Then (assertion, after).

func (c *bddCtx) aliceStatusIs(status string) error {
	if c.alice == nil {
		c.aliceInitStatus = PlayerStatus(status)
		return nil
	}
	if string(c.alice.Status) != status {
		return fmt.Errorf("Alice status: got %q, want %q", c.alice.Status, status)
	}
	return nil
}

func (c *bddCtx) bobStatusIs(status string) error {
	if c.bob == nil {
		c.bobInitStatus = PlayerStatus(status)
		return nil
	}
	if string(c.bob.Status) != status {
		return fmt.Errorf("Bob status: got %q, want %q", c.bob.Status, status)
	}
	return nil
}

func (c *bddCtx) aliceNotBusted() error {
	c.ensureGame()
	if c.alice.Status == StatusBusted {
		return fmt.Errorf("Alice should not be busted (cards: %v)", bddCardNames(c.alice.Cards))
	}
	return nil
}

func (c *bddCtx) bobNotBusted() error {
	c.ensureGame()
	if c.bob.Status == StatusBusted {
		return fmt.Errorf("Bob should not be busted (cards: %v)", bddCardNames(c.bob.Cards))
	}
	return nil
}

// ── step definitions — Second Chance ─────────────────────────────────────────

func (c *bddCtx) aliceHasSecondChance() error {
	c.ensureGame()
	if !c.alice.HasSecondChance {
		return fmt.Errorf("Alice should have Second Chance")
	}
	return nil
}

func (c *bddCtx) aliceNoSecondChance() error {
	c.ensureGame()
	if c.alice.HasSecondChance {
		return fmt.Errorf("Alice should no longer have Second Chance")
	}
	return nil
}

func (c *bddCtx) bobHasSecondChance() error {
	c.ensureGame()
	if !c.bob.HasSecondChance {
		return fmt.Errorf("Bob should have Second Chance")
	}
	return nil
}

func (c *bddCtx) bobNoSecondChance() error {
	c.ensureGame()
	if c.bob.HasSecondChance {
		return fmt.Errorf("Bob should no longer have Second Chance")
	}
	return nil
}

// ── step definitions — turn and pending ──────────────────────────────────────

func (c *bddCtx) itIsAliceTurn() error {
	c.ensureGame()
	if c.g.CurrentIndex != 0 {
		return fmt.Errorf("expected Alice's turn (index 0), got %d", c.g.CurrentIndex)
	}
	return nil
}

func (c *bddCtx) itIsBobTurn() error {
	c.ensureGame()
	if c.g.CurrentIndex != 1 {
		return fmt.Errorf("expected Bob's turn (index 1), got %d", c.g.CurrentIndex)
	}
	return nil
}

func (c *bddCtx) targetChoicePending() error {
	c.ensureGame()
	if c.g.pending == nil {
		return fmt.Errorf("expected a pending target choice, but none exists")
	}
	return nil
}

func (c *bddCtx) noTargetChoicePending() error {
	c.ensureGame()
	if c.g.pending != nil {
		return fmt.Errorf("expected no pending target choice, but %s is waiting", c.g.pending.Card.Name)
	}
	return nil
}

// ── step definitions — round / game phase ────────────────────────────────────

func (c *bddCtx) roundHasEnded() error {
	c.ensureGame()
	if c.g.Phase != PhaseRoundEnd && c.g.Phase != PhaseGameOver {
		return fmt.Errorf("expected round_end or game_over, phase is %q", c.g.Phase)
	}
	return nil
}

func (c *bddCtx) gameIsOver() error {
	c.ensureGame()
	if c.g.Phase != PhaseGameOver {
		return fmt.Errorf("expected game_over, phase is %q", c.g.Phase)
	}
	return nil
}

func (c *bddCtx) gameIsNotOver() error {
	c.ensureGame()
	if c.g.Phase == PhaseGameOver {
		return fmt.Errorf("game should not be over yet")
	}
	return nil
}

func (c *bddCtx) aliceIsWinner() error {
	c.ensureGame()
	for _, w := range c.g.Winners {
		if w == c.alice {
			return nil
		}
	}
	names := make([]string, len(c.g.Winners))
	for i, w := range c.g.Winners {
		names[i] = w.Name
	}
	return fmt.Errorf("Alice is not the winner; winners: %v", names)
}

// ── step definitions — score assertions ──────────────────────────────────────

func (c *bddCtx) aliceRoundScoreIs(expected int) error {
	c.ensureGame()
	if got := c.alice.RoundScore(); got != expected {
		return fmt.Errorf("Alice round score: got %d, want %d", got, expected)
	}
	return nil
}

func (c *bddCtx) bobRoundScoreIs(expected int) error {
	c.ensureGame()
	if got := c.bob.RoundScore(); got != expected {
		return fmt.Errorf("Bob round score: got %d, want %d", got, expected)
	}
	return nil
}

// ── step definitions — card-in-hand ──────────────────────────────────────────

func (c *bddCtx) aliceHasCardInHand(name string) error {
	c.ensureGame()
	for _, card := range c.alice.Cards {
		if card.Name == name {
			return nil
		}
	}
	return fmt.Errorf("Alice does not have %q in hand; holds: %v", name, bddCardNames(c.alice.Cards))
}

func (c *bddCtx) aliceNotHaveCardInHand(name string) error {
	c.ensureGame()
	for _, card := range c.alice.Cards {
		if card.Name == name {
			return fmt.Errorf("Alice should not have %q in hand", name)
		}
	}
	return nil
}

func (c *bddCtx) bobHasCardInHand(name string) error {
	c.ensureGame()
	for _, card := range c.bob.Cards {
		if card.Name == name {
			return nil
		}
	}
	return fmt.Errorf("Bob does not have %q in hand; holds: %v", name, bddCardNames(c.bob.Cards))
}

func (c *bddCtx) bobNotHaveCardInHand(name string) error {
	c.ensureGame()
	for _, card := range c.bob.Cards {
		if card.Name == name {
			return fmt.Errorf("Bob should not have %q in hand", name)
		}
	}
	return nil
}

// ── step definitions — Flip 7 ────────────────────────────────────────────────

func (c *bddCtx) aliceTriggeredFlip7() error {
	c.ensureGame()
	if c.g.flip7WinnerID != c.alice.ID {
		return fmt.Errorf("expected Alice to have triggered Flip 7, flip7WinnerID=%q", c.g.flip7WinnerID)
	}
	return nil
}

func (c *bddCtx) bobTriggeredFlip7() error {
	c.ensureGame()
	if c.g.flip7WinnerID != c.bob.ID {
		return fmt.Errorf("expected Bob to have triggered Flip 7, flip7WinnerID=%q", c.g.flip7WinnerID)
	}
	return nil
}

// ── step registration ─────────────────────────────────────────────────────────

func (c *bddCtx) register(sc *godog.ScenarioContext) {
	// Scoring (standalone player, no game)
	sc.Step(`^a player holds \[([^\]]*)\]$`, c.aPlayerHolds)
	sc.Step(`^their round score is (\d+)$`, c.theirRoundScoreIs)

	// Game setup
	sc.Step(`^Alice has \[([^\]]*)\]$`, c.aliceHasCards)
	sc.Step(`^Bob has \[([^\]]*)\]$`, c.bobHasCards)
	sc.Step(`^Alice has \[([^\]]*)\] and a Second Chance$`, c.aliceHasCardsAndSC)
	sc.Step(`^Bob has \[([^\]]*)\] and a Second Chance$`, c.bobHasCardsAndSC)
	sc.Step(`^the deck is \[([^\]]*)\]$`, c.deckIs)

	// These double as Given (pre-game setup) and Then (assertion)
	sc.Step(`^Alice's total score is (\d+)$`, c.aliceTotalScoreStep)
	sc.Step(`^Bob's total score is (\d+)$`, c.bobTotalScoreStep)
	sc.Step(`^Alice is (busted|frozen|stopped|active)$`, c.aliceStatusIs)
	sc.Step(`^Bob is (busted|frozen|stopped|active)$`, c.bobStatusIs)

	// Actions
	sc.Step(`^Alice draws$`, c.aliceDraws)
	sc.Step(`^Bob draws$`, c.bobDraws)
	sc.Step(`^Alice stops$`, c.aliceStops)
	sc.Step(`^Bob stops$`, c.bobStops)
	sc.Step(`^Alice targets Bob$`, c.aliceTargetsBob)
	sc.Step(`^Alice targets herself$`, c.aliceTargetsSelf)
	sc.Step(`^Bob targets Alice$`, c.bobTargetsAlice)
	sc.Step(`^Bob targets himself$`, c.bobTargetsSelf)

	// Status assertions (not busted — separate from the combined Given/Then step)
	sc.Step(`^Alice is not busted$`, c.aliceNotBusted)
	sc.Step(`^Bob is not busted$`, c.bobNotBusted)

	// Second Chance assertions
	sc.Step(`^Alice has Second Chance$`, c.aliceHasSecondChance)
	sc.Step(`^Alice no longer has Second Chance$`, c.aliceNoSecondChance)
	sc.Step(`^Bob has Second Chance$`, c.bobHasSecondChance)
	sc.Step(`^Bob no longer has Second Chance$`, c.bobNoSecondChance)

	// Turn and pending
	sc.Step(`^it is Alice's turn$`, c.itIsAliceTurn)
	sc.Step(`^it is Bob's turn$`, c.itIsBobTurn)
	sc.Step(`^a target choice is pending$`, c.targetChoicePending)
	sc.Step(`^no target choice is pending$`, c.noTargetChoicePending)

	// Round and game phase
	sc.Step(`^the round has ended$`, c.roundHasEnded)
	sc.Step(`^the game is over$`, c.gameIsOver)
	sc.Step(`^the game is not over$`, c.gameIsNotOver)
	sc.Step(`^Alice is the winner$`, c.aliceIsWinner)

	// Scores
	sc.Step(`^Alice's round score is (\d+)$`, c.aliceRoundScoreIs)
	sc.Step(`^Bob's round score is (\d+)$`, c.bobRoundScoreIs)

	// Card-in-hand (uses (.+) to capture multi-word names like "2nd Chance")
	sc.Step(`^Alice has (.+) in her hand$`, c.aliceHasCardInHand)
	sc.Step(`^Alice does not have (.+) in her hand$`, c.aliceNotHaveCardInHand)
	sc.Step(`^Bob has (.+) in his hand$`, c.bobHasCardInHand)
	sc.Step(`^Bob does not have (.+) in his hand$`, c.bobNotHaveCardInHand)

	// Flip 7
	sc.Step(`^Alice triggered Flip 7$`, c.aliceTriggeredFlip7)
	sc.Step(`^Bob triggered Flip 7$`, c.bobTriggeredFlip7)
}

// ── test runner ───────────────────────────────────────────────────────────────

func TestBDD(t *testing.T) {
	suite := godog.TestSuite{
		ScenarioInitializer: func(sc *godog.ScenarioContext) {
			ctx := &bddCtx{}
			ctx.register(sc)
		},
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"features"},
			TestingT: t,
		},
	}
	if suite.Run() != 0 {
		t.Fatal("BDD feature tests failed")
	}
}
