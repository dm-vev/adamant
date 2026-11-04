package menu

import (
	"errors"
	"fmt"
)

// The errors below are reasons an operation fails for that a protocol tells
// its client apart, as Bedrock tells them by the status of the response
// that refuses a request. Operations wrap them; errors.Is finds them.
var (
	// ErrSameSlot is the error of Move and Swap for a source and a
	// destination that are one slot.
	ErrSameSlot = errors.New("source and destination are the same slot")
	// ErrSlotRefusesItem is the error of an operation that would put a
	// stack in a slot that does not take it (inventory.Inventory.Accepts):
	// the off hand of a player takes only some items, an armour slot only
	// armour of its kind.
	ErrSlotRefusesItem = errors.New("the slot does not take the item")
)

// MissingInputError is the error of a craft for which a slot it takes an
// input from is empty: the slot of Role.
type MissingInputError struct {
	Role Role
}

// Error ...
func (e MissingInputError) Error() string {
	return fmt.Sprintf("the input slot of role %d is empty", e.Role)
}
