package menu

import (
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// SetSlot sets a slot to s as an operation does, with the change in the
// journal, for the tests of the journal of a batch.
func (t *Tx) SetSlot(r Ref, s item.Stack) error { return t.set(r, s) }

// AddResults adds the results of a craft to the batch as a craft does, for
// the tests of the results of a batch: a single result is put in the
// result slot at once.
func (t *Tx) AddResults(results ...item.Stack) error { return t.createResults(results...) }

// OfferedByTable reports if an enchanting table may offer e for it, for the
// tests of the enchantments a table offers.
func OfferedByTable(e item.EnchantmentType, it world.Item) bool { return offeredByTable(e, it) }
