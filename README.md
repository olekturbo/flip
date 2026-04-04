# Flip 7 — Multiplayer Web Game

[![CI](https://github.com/olekturbo/flip/actions/workflows/ci.yml/badge.svg)](https://github.com/olekturbo/flip/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/olekturbo/flip/graph/badge.svg)](https://codecov.io/gh/olekturbo/flip)

A faithful digital adaptation of the **Flip 7** card game by [The Op](https://theop.games/pages/flip-7), playable in the browser with 2–6 players over WebSockets.

## Playing the Game

1. Open the app and enter your name.
2. Share the room URL with friends.
3. The host presses **Start** when everyone is in.
4. Take turns drawing cards or stopping — first to **200 points** wins.

Rules are available in-game via the **Rules** button.

## Rules Summary

### Deck — 100 cards
| Type | Cards |
|------|-------|
| Number cards | 0–12; card N appears N times (0 appears once) → **79 cards** |
| Action cards | 3× Freeze, 3× Flip 3, 3× Second Chance → **9 cards** |
| Positive modifier cards | +2, +4, +6, +8, +10, ×2 → **6 cards** |
| Negative modifier cards | -2, -4, -6, -8, -10, ÷2 → **6 cards** |

The deck carries over between rounds and is only reshuffled when it runs out.

### Each Round
1. The dealer (rotates each round) deals **one card face-up** to every player starting left of the dealer.
2. Play passes clockwise from the dealer's left. On your turn:
   - **Draw** — take one card from the deck; turn passes to the next player.
   - **Stay** — bank your current points and exit the round.
3. The round ends when every player has either Stayed, been Frozen, or Busted.

### Busting
Drawing a number you **already hold** = bust. You score **0** this round. Only duplicate number cards cause busts — action and modifier cards never do.

### Action Cards
When drawn you **choose any active player** to receive its effect (including yourself). If you're the only active player it targets you automatically.

| Card | Effect |
|------|--------|
| **Freeze** | Target banks their points and exits the round immediately. |
| **Flip 3** | Target draws 3 cards one at a time. The sequence stops early on a bust or Flip 7. Action cards drawn during Flip 3 resolve interactively after all draws complete. |
| **Second Chance** | Target holds this card. If they would bust, discard both the duplicate and this card — they survive but **their turn ends**. Only one per player at a time. |

### Modifier Cards
| Card | Effect |
|------|--------|
| **+2 / +4 / +6 / +8 / +10** | Added to your final round score. |
| **×2** | Doubles your number-card total; modifiers applied on top. |
| **-2 / -4 / -6 / -8 / -10** | Subtracted from your final round score. |
| **÷2** | Halves your number-card total (rounded down); modifiers applied on top. Cancels out with ×2 if both held. |

### Scoring
`score = (sum of number cards [×2 or ÷2 if held; cancel each other]) + sum of +modifiers - sum of -modifiers`  
Minimum round score is **0**. Busted players score **0**. The Flip 7 bonus (+15) is added after modifiers.

### Flip 7 Bonus 🎉
Collect **7 unique number cards** → round ends immediately for everyone.  
You score your number total + modifiers + **+15 bonus**. All other active players bank whatever they hold.

### Winning
Game ends at the **end of a round** in which one player has the **highest score at or above 200 points**. If two or more players are tied at 200+, the round ends but the game continues — rounds are played until one player pulls clearly ahead.

---

## Running Locally

```bash
go run ./cmd/server        # serves at http://localhost:8080
```

Requires Go 1.22+. No other dependencies — the frontend is vanilla JS/CSS with no build step.

## Tests

```bash
go test ./internal/game/...
```

The test suite has two layers:

**Unit / scenario tests** (`*_test.go`) — Go's standard `testing` package:

| File | What it tests |
|------|---------------|
| `deck_test.go` | Deck composition — 100 cards, correct counts per type and value |
| `player_test.go` | `RoundScore()` (table-driven), `HasNumber()`, `UniqueNumberCount()` |
| `game_test.go` | Game mechanics — draw, bust, Second Chance, Stop, Freeze, Flip 3, Flip 7, win condition, tie at 200+, dealing-phase SC, valid targets, player management |

**BDD feature tests** (`bdd_test.go` + `features/*.feature`) — [godog](https://github.com/cucumber/godog) with Gherkin:

| Feature file | What it documents |
|---|---|
| `scoring.feature` | All score combinations: plain numbers, ×2, ÷2, +/- modifiers, minimum zero |
| `bust.feature` | Bust on duplicate number; modifiers never bust |
| `second_chance.feature` | SC prevents bust, consumed on use, auto-resolves with single target |
| `freeze.feature` | Freeze targeting, auto-target, banked score |
| `flip3.feature` | 3 forced draws, stops on bust / SC save / Flip 7 |
| `flip7.feature` | 7 unique numbers ends round, +15 bonus, active players bank |
| `round_and_game.feature` | Score accumulation, win at 200+, tie continuation, bust at threshold |

The feature files serve as **living documentation** of the rules — readable without knowing Go. Game-mechanics tests bypass the dealing phase by constructing state directly (same-package access), then drive actions through the public API (`Draw`, `Stop`, `Target`).

## Docker

```bash
docker build -t flip7 .
docker run -p 8080:8080 flip7
```

Static files are embedded in the binary at compile time (`//go:embed web`).  
Set the `PORT` environment variable to override the default port 8080.

## Project Structure

```
cmd/server/          Entry point (HTTP server, PORT env var)
internal/
  api/               HTTP router (REST + WebSocket upgrade)
  hub/               Room and client lifecycle, WebSocket handling
  game/              Game logic, state machine, scoring
web/
  css/style.css      All styles (game page + mobile)
  js/app.js          Vanilla JS client (WebSocket, rendering, animations)
  index.html         Landing page
  game.html          Game page + in-game rules modal
embed.go             //go:embed directive (bakes web/ into binary)
Dockerfile           Two-stage build (golang:1.22-alpine → alpine:3.19)
```

## Tech Stack

- **Backend:** Go, [`nhooyr.io/websocket`](https://github.com/nhooyr/websocket)
- **Frontend:** Vanilla JS + CSS — no framework, no build step
- **Transport:** WebSocket with 15 s ping / 5 s timeout; full state broadcast on every change
- **Persistence:** In-memory only; rooms expire after 10 minutes empty
