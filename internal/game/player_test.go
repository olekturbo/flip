package game

import "testing"

func TestRoundScore(t *testing.T) {
	tests := []struct {
		name  string
		cards []Card
		want  int
	}{
		{
			name:  "empty hand",
			cards: nil,
			want:  0,
		},
		{
			name:  "single number",
			cards: []Card{NumberCard(7)},
			want:  7,
		},
		{
			name:  "multiple numbers",
			cards: []Card{NumberCard(3), NumberCard(5), NumberCard(2)},
			want:  10,
		},
		{
			name:  "times two multiplier",
			cards: []Card{NumberCard(4), NumberCard(6), ModifierMulCard()},
			want:  20, // (4+6) * 2
		},
		{
			name:  "divide by two modifier",
			cards: []Card{NumberCard(5), NumberCard(7), ModifierDivCard()},
			want:  6, // (5+7) / 2 = 6 (integer division)
		},
		{
			name:  "divide by two rounds down",
			cards: []Card{NumberCard(3), NumberCard(4), ModifierDivCard()},
			want:  3, // (3+4) / 2 = 3
		},
		{
			name:  "times two and divide two cancel",
			cards: []Card{NumberCard(6), ModifierMulCard(), ModifierDivCard()},
			want:  6, // modifiers cancel
		},
		{
			name:  "positive modifier adds to score",
			cards: []Card{NumberCard(5), ModifierAddCard(10)},
			want:  15,
		},
		{
			name:  "negative modifier subtracts from score",
			cards: []Card{NumberCard(8), ModifierSubCard(4)},
			want:  4, // 8 + (-4)
		},
		{
			name:  "score cannot go below zero",
			cards: []Card{NumberCard(1), ModifierSubCard(10)},
			want:  0, // 1 - 10 = -9 → clamped to 0
		},
		{
			name:  "negative modifier on zero numbers",
			cards: []Card{NumberCard(0), ModifierSubCard(2)},
			want:  0, // 0 - 2 = -2 → clamped to 0
		},
		{
			name:  "multiplier applies before modifiers",
			cards: []Card{NumberCard(3), NumberCard(4), ModifierMulCard(), ModifierAddCard(10)},
			want:  24, // (3+4)*2 + 10
		},
		{
			name:  "full mix",
			cards: []Card{NumberCard(5), NumberCard(3), ModifierMulCard(), ModifierSubCard(4), ModifierAddCard(2)},
			want:  14, // (5+3)*2 + (-4) + 2 = 16 - 4 + 2 = 14
		},
		{
			name:  "second chance card in hand does not affect score",
			cards: []Card{NumberCard(6), SecondChanceCard()},
			want:  6,
		},
		{
			name:  "freeze card in hand does not affect score",
			cards: []Card{NumberCard(9), FreezeCard()},
			want:  9,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Player{Cards: tt.cards}
			got := p.RoundScore()
			if got != tt.want {
				t.Errorf("RoundScore() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestHasNumber(t *testing.T) {
	p := &Player{Cards: []Card{NumberCard(3), NumberCard(7), ModifierAddCard(4)}}
	if !p.HasNumber(3) {
		t.Error("HasNumber(3) = false, want true")
	}
	if !p.HasNumber(7) {
		t.Error("HasNumber(7) = false, want true")
	}
	if p.HasNumber(5) {
		t.Error("HasNumber(5) = true, want false")
	}
	// Modifier card with value 4 is not a number card
	if p.HasNumber(4) {
		t.Error("HasNumber(4) = true for modifier card, want false")
	}
}

func TestUniqueNumberCount(t *testing.T) {
	p := &Player{Cards: []Card{NumberCard(1), NumberCard(3), NumberCard(5), ModifierMulCard()}}
	if p.UniqueNumberCount() != 3 {
		t.Errorf("UniqueNumberCount() = %d, want 3", p.UniqueNumberCount())
	}
}
