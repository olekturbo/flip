# Flip 7 — Project Instructions

## Rules sync rule (MANDATORY)

Whenever you modify **any game mechanic** — scoring, card effects, deck composition, action card behaviour, Flip 7 bonus, bust logic, dealing phase, win condition, or any other rule — you **must** update **all** of the following:

1. **`web/game.html`** rules modal (between `<!-- Rules modal -->` and `<script src="/js/app.js">`).
2. **`web/rules.html`** standalone rules page — keep in sync with `game.html`.
3. **`RULES.md`** in the repository root — keep it word-for-word in sync with the HTML modal.
4. **`README.md`** contains only a one-paragraph summary + link to `RULES.md` — **do not** duplicate full rule details there; only update the blurb if the core premise changes (e.g. win score).
5. **`internal/game/features/*.feature`** — the relevant BDD scenario(s) must reflect the changed mechanic. See also the Tests sync rule below.

Keep every description **exactly in sync** with the actual Go logic in `internal/game/`. Do not paraphrase or summarise differently from how the code works.

Specific sections to watch:
| Game change | What to update |
|---|---|
| Deck composition (`deck.go`) | "Deck — 106 cards" in game.html/rules.html/RULES.md |
| Scoring formula (`player.go RoundScore`) | "Modifier Cards" + "Scoring" in game.html/rules.html/RULES.md |
| Freeze card effect | "Action Cards → Freeze" in game.html/rules.html/RULES.md |
| Flip 3 card effect | "Action Cards → Flip 3" in game.html/rules.html/RULES.md + `flip3.feature` |
| Second Chance card effect | "Action Cards → 2nd Chance" + "Busting" in game.html/rules.html/RULES.md + `second_chance.feature` |
| Thief card effect | "Action Cards → Thief" in game.html/rules.html/RULES.md + `thief.feature` |
| Swap card effect | "Action Cards → Swap" in game.html/rules.html/RULES.md + `shuffle.feature` |
| Positive modifier cards (+2…+10, ×2) | "Modifier Cards" in game.html/rules.html/RULES.md |
| Negative modifier cards (-2…-10, ÷2) | "Modifier Cards" in game.html/rules.html/RULES.md |
| Flip 7 bonus (`triggerFlip7 / endRound`) | "Flip 7 Bonus" in game.html/rules.html/RULES.md + `flip7.feature` |
| Win score constant (`WinScore`) | "Winning" in game.html/rules.html/RULES.md + README blurb + `round_and_game.feature` |
| Bust logic | "Busting" in game.html/rules.html/RULES.md + `bust.feature` |

## Card visibility rule (MANDATORY)

Every action card that resolves successfully **must remain visible in a player's hand** on the board — as a styled card element, not just a banner or ghost animation. This applies to all existing and future card types:

| Card | Stays in whose hand |
|---|---|
| Freeze | Target's hand (blue marker — frozen status) |
| Flip 3 | Target's hand (orange marker) |
| Second Chance | Target's hand until consumed (green marker; ghost animation on use) |
| Thief (successful steal) | Drawer's hand (purple marker) |
| Swap (successful swap) | Drawer's hand (teal marker) |

When adding a new action card: ensure `applyXxx` adds the card to the relevant player's `Cards` slice (not `UsedCards`) so it renders automatically via `card-{type}` CSS. Only discard to `UsedCards` when the card has **no effect** (no valid target etc.) — in that case a ghost animation should be shown instead.

## Tests sync rule (MANDATORY)

Whenever you modify **any game mechanic** in `internal/game/` — or add a new one — you **must** also update the tests in `internal/game/*_test.go`.

- **New card type or deck change** → update `deck_test.go` (card counts, values)
- **Scoring formula change** → add/update cases in `TestRoundScore` in `player_test.go`
- **New or changed action card, bust logic, Flip 3/7, win condition, dealing phase** → add/update scenario tests in `game_test.go` AND add/update BDD scenarios in `features/*.feature`

The BDD feature files in `internal/game/features/` are **living documentation** — they must stay in sync with actual game behaviour:

| Feature file | What it documents |
|---|---|
| `scoring.feature` | All score combinations: plain numbers, ×2, ÷2, +/- modifiers, minimum zero |
| `bust.feature` | Bust on duplicate number; modifiers never bust |
| `second_chance.feature` | SC prevents bust, consumed on use, auto-resolves with single target, Flip 3 draws continue |
| `freeze.feature` | Freeze targeting, auto-target, banked score |
| `flip3.feature` | 3 forced draws, stops on bust or Flip 7 only, deferred action resolution |
| `flip7.feature` | 7 unique numbers ends round, +15 bonus, active players bank |
| `thief.feature` | Two-stage steal: choose player then card; discarded when nothing to steal; Flip 7 on stolen 7th |
| `shuffle.feature` | Two-stage swap: choose partner then card pair; discarded when no valid swap target; Flip 7 on swapped 7th |
| `round_and_game.feature` | Score accumulation, win at 200+, tie continuation, bust at threshold |

When adding a new mechanic, add a readable Gherkin scenario. When changing a mechanic, update the scenario to match.

Run `go test ./internal/game/...` and confirm all tests pass before committing. Never leave a mechanic change without test coverage.

## Authoritative rules (source: The Op official rulebook)

These are the official rules this implementation is designed to match. Deviations are noted.

### Deck — 106 cards
- **Number cards (79):** values 0–12; card N appears N times (0 appears once).
- **Action cards (15):** 3× Freeze, 3× Flip 3, 3× Second Chance, 3× Thief, 3× Swap.
- **Positive modifier cards (6):** +2, +4, +6, +8, +10, ×2.
- **Negative modifier cards (6):** -2, -4, -6, -8, -10, ÷2.
- Deck carries over between rounds; reshuffled only when empty.

### Turn
Draw one card OR stay. Action cards require choosing a target (any active player including self; auto-targets self when only one active player remains).

### Bust
Drawing a duplicate number = bust, score 0. Only number cards cause busts.

### Second Chance
- When used to prevent a bust: discard both SC and duplicate; **turn ends** (play passes to next player). During Flip 3, the remaining Flip 3 draws **continue** (only a real bust or Flip 7 stops the sequence early).
- Only one SC per player at a time.

### Flip 3
- Target draws exactly 3 cards one at a time.
- Sequence stops early on **bust** or **Flip 7**; a Second Chance save does NOT stop it — remaining draws continue.
- Action cards drawn during Flip 3 are deferred and resolved interactively after all draws complete.

### Thief
- Two-stage interaction: first choose a target player, then choose which of their number cards to steal.
- The stolen number card moves from the target's hand to the drawer's hand.
- Thief can only target players who hold at least one number card the drawer does not already hold.
- If no valid target exists (or nothing to steal), Thief is discarded with no effect.
- Stealing the 7th unique number triggers Flip 7 immediately.

### Swap
- Two-stage interaction: first choose any active opponent who holds at least one number card (drawer must also hold at least one); then choose which of the drawer's number cards and which of the partner's number cards to swap.
- The two chosen number cards trade hands.
- If no valid partner exists, Swap is discarded with no effect.
- Swapping the 7th unique number triggers Flip 7 immediately (drawer checked first).

### Negative modifiers
- **-2 / -4 / -6 / -8 / -10** — subtracted from the final round score.
- **÷2** — halves the number-card total (rounded down) before modifiers are applied. Cancels out with ×2 if both held (net effect: ×1).
- Minimum round score is 0 (negative results are floored at zero).

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
