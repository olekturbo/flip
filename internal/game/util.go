package game

import (
	"crypto/rand"
	"fmt"
)

// removeCard removes the first card of the given type and value from *cards.
func removeCard(cards *[]Card, t CardType, value int) {
	for i, c := range *cards {
		if c.Type == t && c.Value == value {
			*cards = append((*cards)[:i], (*cards)[i+1:]...)
			return
		}
	}
}

// newUUID returns a random UUID v4-style identifier.
func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
