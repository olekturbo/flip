# Flip 7 — Project Instructions

## Rules sync rule (MANDATORY)

Whenever you modify **any game mechanic** — scoring, card effects, deck composition, action card behaviour, Flip 7 bonus, bust logic, dealing phase, win condition, or any other rule — you **must** also update the rules modal in `web/game.html`.

The rules modal starts at the `<!-- Rules modal -->` comment and ends before `<script src="/js/app.js">`.

Keep the HTML description **exactly in sync** with the actual Go logic in `internal/game/`. Do not paraphrase or summarise differently from how the code works.

Specific sections to watch:
| Game change | HTML section to update |
|---|---|
| Deck composition (`deck.go`) | "Deck — 94 cards" |
| Scoring formula (`player.go RoundScore`) | "Modifier Cards", "Scoring" |
| Action card effects (`game.go resolveActionWithTarget`) | "Action Cards" |
| Flip 7 bonus (`game.go triggerFlip7 / endRound`) | "Flip 7 Bonus" |
| Win score constant (`WinScore`) | "Winning" |
| Bust / Second Chance logic | "Busting", "Action Cards → 2nd Chance" |

## Tech stack reminder
- Backend: Go, `nhooyr.io/websocket`, package layout `internal/game`, `internal/hub`, `internal/api`
- Frontend: vanilla JS + CSS, no build step, files in `web/`
- Run: `go run ./cmd/server` from project root
