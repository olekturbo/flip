# Flip 7 — Project Instructions

## Rules sync rule (MANDATORY)

Whenever you modify **any game mechanic** — scoring, card effects, deck composition, action card behaviour, Flip 7 bonus, bust logic, dealing phase, win condition, or any other rule — you **must** also update the rules modal in `web/game.html`.

The rules modal starts at the `<!-- Rules modal -->` comment and ends before `<script src="/js/app.js">`.

Keep the HTML description **exactly in sync** with the actual Go logic in `internal/game/`. Do not paraphrase or summarise differently from how the code works.

Specific sections to watch:
| Game change | HTML section to update |
|---|---|
| Deck composition (`deck.go`) | "Deck — 100 cards" |
| Scoring formula (`player.go RoundScore`) | "Modifier Cards", "Scoring" |
| Action card effects (`game.go resolveActionWithTarget`) | "Action Cards" |
| Flip 7 bonus (`game.go triggerFlip7 / endRound`) | "Flip 7 Bonus" |
| Win score constant (`WinScore`) | "Winning" |
| Bust / Second Chance logic | "Busting", "Action Cards → 2nd Chance" |

Always update `README.md` as well when rules or architecture change.

## Authoritative rules (source: The Op official rulebook)

These are the official rules this implementation is designed to match. Deviations are noted.

### Deck — 100 cards
- **Number cards (79):** values 0–12; card N appears N times (0 appears once).
- **Action cards (9):** 3× Freeze, 3× Flip 3, 3× Second Chance.
- **Positive modifier cards (6):** +2, +4, +6, +8, +10, ×2.
- **Negative modifier cards (6):** -2, -4, -6, -8, -10, ÷2.
- Deck carries over between rounds; reshuffled only when empty.

### Turn
Draw one card OR stay. Action cards require choosing a target (any active player including self; auto-targets self when only one active player remains).

### Bust
Drawing a duplicate number = bust, score 0. Only number cards cause busts.

### Second Chance
- When used to prevent a bust: discard both SC and duplicate; **turn ends** (play passes to next player). During Flip 3, the remaining Flip 3 draws are also cancelled.
- Only one SC per player at a time.

### Flip 3
- Target draws exactly 3 cards one at a time.
- Sequence stops early on **bust** or **Flip 7**; otherwise all 3 are drawn.
- Action cards drawn during Flip 3 are deferred and resolved interactively after all draws complete.

### Scoring
`(sum of number cards [×2 or ÷2 if held; cancel each other]) + sum of +modifiers - sum of -modifiers`
Minimum round score is 0.

### Flip 7 bonus
7 unique number cards → round ends immediately; player scores +15 on top of their normal score; other active players bank their current hand.

### Win condition
Game ends when one player has the **strictly highest** score at or above 200 at the end of a round. If multiple players are tied at 200+, the game continues — rounds are played until the tie is broken.

## Tech stack
- **Backend:** Go, `nhooyr.io/websocket`, packages `internal/game`, `internal/hub`, `internal/api`
- **Frontend:** vanilla JS + CSS, no build step, files in `web/`
- **Run locally:** `go run ./cmd/server` (serves on :8080)
- **Docker:** `docker build -t flip7 . && docker run -p 8080:8080 flip7`
- Static files are embedded at compile time via `embed.go` (`//go:embed web`)
