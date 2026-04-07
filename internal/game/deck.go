package game

import "math/rand"

// NewDeck returns a freshly shuffled 106-card Flip 7 deck:
//
//	Number cards: 0 → 1 copy, N → N copies (N = 1..12)              →  79 cards
//	3× Freeze, 3× Flip Three, 3× Second Chance, 3× Thief, 3× Swap    → 15 cards
//	+2, +4, +6, +8, +10, ×2                                          →   6 cards
//	-2, -4, -6, -8, -10, ÷2                                          →   6 cards
//
// Total: 106 cards.
func NewDeck() []Card {
	deck := make([]Card, 0, 106)

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
		deck = append(deck, ThiefCard())
		deck = append(deck, ShuffleCard())
	}

	// Positive modifier cards (one each)
	for _, v := range []int{2, 4, 6, 8, 10} {
		deck = append(deck, ModifierAddCard(v))
	}
	deck = append(deck, ModifierMulCard())

	// Negative modifier cards (one each)
	for _, v := range []int{2, 4, 6, 8, 10} {
		deck = append(deck, ModifierSubCard(v))
	}
	deck = append(deck, ModifierDivCard())

	rand.Shuffle(len(deck), func(i, j int) {
		deck[i], deck[j] = deck[j], deck[i]
	})
	return deck
}
