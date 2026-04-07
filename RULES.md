# Flip 7 — Rules

A faithful digital adaptation of the **Flip 7** card game by [The Op](https://theop.games/pages/flip-7).

---

## Deck — 106 cards

| Type | Cards |
|------|-------|
| Number cards | 0–12; card N appears N times (0 appears once) → **79 cards** |
| Action cards | 3× Freeze, 3× Flip 3, 3× Second Chance, 3× Thief, 3× Swap → **15 cards** |
| Positive modifier cards | +2, +4, +6, +8, +10, ×2 → **6 cards** |
| Negative modifier cards | -2, -4, -6, -8, -10, ÷2 → **6 cards** |

The deck carries over between rounds and is only reshuffled when it runs out.

---

## Each Round

1. The dealer (rotates each round) deals **one card face-up** to every player starting left of the dealer.
   - Action cards drawn during dealing are resolved immediately, then dealing continues.
2. Play passes clockwise from the dealer's left. On your turn:
   - **Draw** — take one card from the deck; turn passes to the next player.
   - **Stay** — bank your current points and exit the round.
3. The round ends when every player has either Stayed, been Frozen, or Busted — or a Flip 7 is achieved.

---

## Busting

Drawing a number you **already hold** = bust. You score **0** this round and are out for the rest of the round. Only duplicate number cards cause busts — action and modifier cards never do.

---

## Action Cards

When drawn you choose a target to receive the effect. Most cards target any active player (including yourself). If you're the only active player it targets you automatically.

### Freeze
Target banks their current points and exits the round immediately (they do not bust).

### Flip 3
Target draws exactly 3 cards one at a time. The sequence stops early only on a **bust** or **Flip 7**. A Second Chance save does **not** stop the sequence — remaining draws continue. Action cards drawn during Flip 3 are deferred and resolved interactively after all draws complete.

### Second Chance
Target holds this card. If they would bust, discard both the duplicate and this card — they survive but **their turn ends** (play passes to the next player). During Flip 3, remaining forced draws still continue after a save. Only one Second Chance per player at a time.

### Thief
Two-stage interaction: first choose a target player, then choose which of their number cards to steal. The chosen card moves from the target's hand to yours. You can only target players who hold at least one number card you don't already hold. If no valid target exists, Thief is discarded with no effect. Stealing the 7th unique number triggers Flip 7 immediately.

### Swap
Two-stage interaction: first choose any active opponent who holds at least one number card (you must also hold at least one). Then choose one of your number cards and one of theirs to swap — the two cards trade hands. If the swap gives you 7 unique number cards, Flip 7 triggers immediately. Discarded with no effect if no valid swap target exists.

---

## Modifier Cards

| Card | Effect |
|------|--------|
| **+2 / +4 / +6 / +8 / +10** | Added to your final round score. |
| **×2** | Doubles your number-card total; modifiers applied on top. |
| **-2 / -4 / -6 / -8 / -10** | Subtracted from your final round score. |
| **÷2** | Halves your number-card total (rounded down); modifiers applied on top. Cancels out with ×2 if both held (net effect: ×1). |

---

## Scoring

`score = (sum of number cards [×2 or ÷2 if held; cancel each other]) + sum of +modifiers − sum of −modifiers`

Minimum round score is **0**. Busted players score **0**. The Flip 7 bonus (+15) is added on top of this formula.

---

## Flip 7 Bonus

Collect **7 unique number cards** → the round ends immediately for everyone. You score your number total + modifiers + **+15 bonus**. All other active players bank whatever they currently hold.

---

## Winning

The game ends at the **end of a round** in which one player has the **strictly highest** score at or above **200 points**. If two or more players are tied at 200+, the round ends but the game continues — rounds are played until one player pulls clearly ahead.
