Compare the actual game mechanics in `internal/game/` against the rules modal in `web/game.html` (the block between `<!-- Rules modal -->` and `<script src="/js/app.js">`).

For each discrepancy found, update the HTML to match the code — not the other way around. The code is the source of truth.

Check every section:
1. **Deck** — count and breakdown from `deck.go` NewDeck()
2. **Action card effects** — from `game.go` resolveActionWithTarget() and resolveActionAuto()
3. **Second Chance** — bust save logic, one-per-player rule, turn-ends-immediately rule
4. **Flip 3** — who draws, when it stops, deferred action resolution
5. **Freeze** — banks points and exits (not just skip-turn)
6. **Modifier scoring** — from `player.go` RoundScore(): multiply first, then add
7. **Flip 7 bonus** — number of unique cards needed, bonus points, from triggerFlip7() and endRound()
8. **Win condition** — WinScore constant value, tie rules
9. **Deck persistence** — deck carries between rounds, reshuffled only when empty (startRound logic)

After updating, confirm which sections were changed (or say "all sections already in sync" if nothing needed updating).

After updating `web/game.html`, also update `RULES.md` in the repository root to match. Keep RULES.md in sync with the HTML rules modal: same sections, same wording.
