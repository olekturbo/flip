---
name: Flip 7 — correct game rules
description: Complete, accurate Flip 7 rules to use when rewriting the game logic
type: project
---

# Flip 7 — Correct Rules

**Why:** The initial implementation used simplified/wrong rules. Use these for the next rewrite.
**How to apply:** Replace all game logic in `internal/game/` with the rules below.

---

## Deck (94 cards total)

### Number cards (79 total)
- Value N = N copies in the deck
- 0 → 1 card, 1 → 1 card, 2 → 2 cards, 3 → 3 cards, … 12 → 12 cards

### Action cards
- Freeze
- Flip Three
- Second Chance

### Modifier cards
- +2, +4, +6, +8, +10
- ×2

---

## Setup each round
1. Shuffle all 94 cards (NOT reshuffled mid-round; used cards set aside, new deck only when exhausted).
2. Choose a Dealer (passes left each round).
3. Dealer deals **one card face-up** to each player (including themselves).
   - If an Action card comes up during dealing → resolve it immediately, then continue dealing.
4. After everyone has one starting card, normal play begins.

---

## Turn structure
- Active player chooses each turn:
  - **Hit** — draw one card from the top of the deck
  - **Stay** — bank current points and exit the round (score is locked in)
- Number cards go in a row (the "line").
- Action/Modifier cards are placed **above** the line.
- **Only duplicate Number cards cause a bust** — Action and Modifier cards never cause a bust.

---

## Busting
- Draw a Number card whose value already appears in your line → **bust**.
- You score **0** this round and are immediately out for the rest of the round.
- Exception: **Second Chance** (see below).

---

## Action cards
When drawn, the active player **chooses any active player** (including themselves) to receive the effect.
If the drawer is the only active player, they must apply it to themselves.

### Freeze
- The chosen player **banks their current points and exits the round** (they do NOT bust; score is kept).

### Flip Three
- The chosen player must **draw 3 cards one at a time**.
- Stops early only if they bust or achieve Flip 7.
- If another Action card appears during Flip Three, resolve it **after all 3 cards are drawn**.
- All card types (including Action cards) count toward the 3 cards.

### Second Chance
- Keep this card in front of you.
- If you would bust (draw a duplicate number): **discard both the duplicate and the Second Chance card** — you survive, but your **turn ends immediately**.
- You can only hold **one** Second Chance at a time; if dealt another while holding one, give it to another active player (or discard if none available).
- All unused Second Chance cards are discarded at end of round.

---

## Modifier cards
- **+2 / +4 / +6 / +8 / +10** — add that amount to your final round score.
- **×2** — doubles the sum of your Number cards first, then +modifiers are added on top.
  - Example: number sum = 19, ×2 → 38, then +10 modifier → **48**.
- Modifiers do **not** count toward the Flip 7 bonus (7 unique Number cards only).

---

## Flip 7 bonus
- Collect **7 unique Number cards** without busting → round ends immediately for **all players**.
- The Flip 7 player scores: number total + modifiers + **15 bonus points**.
- Other active players (not busted, not frozen/stayed) **bank whatever they currently have**.

---

## End of round
- Round ends when all players have either Stayed, been Frozen, or busted — **OR** a Flip 7 is achieved.
- Used cards are set aside (not reshuffled until the deck runs out mid-round).
- Dealer role passes to the left.

---

## Scoring
1. Sum of Number cards.
2. Apply ×2 multiplier (if held).
3. Add +modifier cards.
4. Busted players score **0**.

---

## Winning
- Game ends at the **conclusion of a round** in which at least one player reaches **200 points**.
- Player with the highest total wins.
- Tied players **share the victory**.
