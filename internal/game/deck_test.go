package game

import "testing"

func TestDeckComposition(t *testing.T) {
	deck := NewDeck()

	if len(deck) != 100 {
		t.Fatalf("expected 100 cards, got %d", len(deck))
	}

	counts := map[CardType]int{}
	numCounts := map[int]int{}
	for _, c := range deck {
		counts[c.Type]++
		if c.Type == CardTypeNumber {
			numCounts[c.Value]++
		}
	}

	// Number cards: 0 appears once, N appears N times for N=1..12
	wantTotal := 0
	for n := 0; n <= 12; n++ {
		want := n
		if n == 0 {
			want = 1
		}
		wantTotal += want
		if numCounts[n] != want {
			t.Errorf("number %d: want %d copies, got %d", n, want, numCounts[n])
		}
	}
	if counts[CardTypeNumber] != wantTotal {
		t.Errorf("total number cards: want %d, got %d", wantTotal, counts[CardTypeNumber])
	}

	// Action cards: 3 each
	for _, ct := range []CardType{CardTypeFreeze, CardTypeFlip3, CardTypeSecondChance} {
		if counts[ct] != 3 {
			t.Errorf("%s: want 3, got %d", ct, counts[ct])
		}
	}

	// Positive modifiers: +2, +4, +6, +8, +10, ×2 = 6 cards
	if counts[CardTypeModifierAdd] != 5 {
		t.Errorf("modifier_add: want 5, got %d", counts[CardTypeModifierAdd])
	}
	if counts[CardTypeModifierMul] != 1 {
		t.Errorf("modifier_mul: want 1, got %d", counts[CardTypeModifierMul])
	}

	// Negative modifiers: -2, -4, -6, -8, -10, ÷2 = 6 cards
	if counts[CardTypeModifierSub] != 5 {
		t.Errorf("modifier_sub: want 5, got %d", counts[CardTypeModifierSub])
	}
	if counts[CardTypeModifierDiv] != 1 {
		t.Errorf("modifier_div: want 1, got %d", counts[CardTypeModifierDiv])
	}
}

func TestModifierSubCardValues(t *testing.T) {
	deck := NewDeck()
	for _, c := range deck {
		if c.Type == CardTypeModifierSub && c.Value >= 0 {
			t.Errorf("modifier_sub card %q has non-negative value %d", c.Name, c.Value)
		}
	}
}
