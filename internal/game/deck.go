package game

import "math/rand"

// NewDeck returns a freshly shuffled 94-card Flip 7 deck:
//
//	Number cards: 0 → 1 copy, N → N copies (N = 1..12)  →  79 cards
//	3× Freeze, 3× Flip Three, 3× Second Chance           →   9 cards
//	+2, +4, +6, +8, +10, ×2                              →   6 cards
//
// Total: 94 cards.
func NewDeck() []Card {
	deck := make([]Card, 0, 94)

	// Number cards
	for i := 0; i <= 12; i++ {
		count := i
		if i == 0 {
			count = 1
		}
		for j := 0; j < count; j++ {
			deck = append(deck, NumberCard(i))
		}
	}

	// Action cards (3 each)
	for i := 0; i < 3; i++ {
		deck = append(deck, FreezeCard())
		deck = append(deck, Flip3Card())
		deck = append(deck, SecondChanceCard())
	}

	// Modifier cards (one each)
	for _, v := range []int{2, 4, 6, 8, 10} {
		deck = append(deck, ModifierAddCard(v))
	}
	deck = append(deck, ModifierMulCard())

	rand.Shuffle(len(deck), func(i, j int) {
		deck[i], deck[j] = deck[j], deck[i]
	})
	return deck
}
