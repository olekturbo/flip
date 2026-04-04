package game

import "fmt"

// CardType identifies the kind of card.
type CardType string

const (
	CardTypeNumber       CardType = "number"
	CardTypeFreeze       CardType = "freeze"
	CardTypeFlip3        CardType = "flip3"
	CardTypeSecondChance CardType = "second_chance"
	CardTypeModifierAdd  CardType = "modifier_add" // +2, +4, +6, +8, +10
	CardTypeModifierMul  CardType = "modifier_mul" // ×2
	CardTypeModifierSub  CardType = "modifier_sub" // -2, -4, -6, -8, -10
	CardTypeModifierDiv  CardType = "modifier_div" // ÷2
	CardTypeThief        CardType = "thief"
)

// Card is a single card in the deck.
type Card struct {
	Type  CardType `json:"type"`
	Value int      `json:"value"` // number cards: 0–12; modifier_add: 2/4/6/8/10; modifier_sub: -2/-4/-6/-8/-10
	Name  string   `json:"name"`
}

func NumberCard(v int) Card      { return Card{Type: CardTypeNumber, Value: v, Name: fmt.Sprintf("%d", v)} }
func FreezeCard() Card           { return Card{Type: CardTypeFreeze, Name: "Freeze"} }
func Flip3Card() Card            { return Card{Type: CardTypeFlip3, Name: "Flip 3"} }
func SecondChanceCard() Card     { return Card{Type: CardTypeSecondChance, Name: "2nd Chance"} }
func ModifierAddCard(v int) Card { return Card{Type: CardTypeModifierAdd, Value: v, Name: fmt.Sprintf("+%d", v)} }
func ModifierMulCard() Card      { return Card{Type: CardTypeModifierMul, Name: "×2"} }
func ModifierSubCard(v int) Card { return Card{Type: CardTypeModifierSub, Value: -v, Name: fmt.Sprintf("-%d", v)} }
func ModifierDivCard() Card      { return Card{Type: CardTypeModifierDiv, Name: "÷2"} }
func ThiefCard() Card            { return Card{Type: CardTypeThief, Name: "Thief"} }
