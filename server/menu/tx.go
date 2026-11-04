package menu

import (
	"fmt"
	"log/slog"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
)

// Slot is a slot of an inventory a menu moves items in.
type Slot struct {
	Inv   *inventory.Inventory
	Index int
}

// Name is how the caller of an operation names a slot in its protocol. The
// game never reads it: it reports it back with each change it makes.
type Name uint32

// Ref is a slot with the name the caller gives it.
type Ref struct {
	Slot
	Name Name
}

// Role is what a slot is for, when a menu changes the slot of its own accord
// rather than at the caller's word: the grid a craft consumes, the result,
// the lapis an enchantment takes. A Namer names such slots.
type Role uint8

const (
	// RoleInventory is a slot of the main inventory.
	RoleInventory Role = iota + 1
	RoleCraftingGrid
	RoleCartographyInput
	RoleCartographyAdditional
	RoleResult
	RoleEnchantingInput
	RoleEnchantingLapis
	RoleAnvilInput
	RoleAnvilMaterial
	RoleGrindstoneFirst
	RoleGrindstoneSecond
	RoleSmithingInput
	RoleSmithingMaterial
	RoleSmithingTemplate
	RoleLoomBanner
	RoleLoomDye
	RoleStonecutterInput
	RoleBeaconPayment
	// RoleTradeInput is either input of a trade.
	RoleTradeInput
)

// Namer returns the name of a slot that a menu changes for the role passed.
type Namer func(s Slot, r Role) Name

// Change is a change a batch made to a slot.
type Change struct {
	Ref Ref
	// Before is what the slot held before this change; After is the stack
	// set, as it was passed: the inventory may hold it clamped to its
	// maximum count, or refuse it in a slot its validator forbids.
	Before, After item.Stack
}

// Owner is the player a batch is for, as the menu sees it. *player.Player
// implements it.
type Owner interface {
	world.Entity
	Name() string
	GameMode() world.GameMode
	ExperienceLevel() int
	SetExperienceLevel(level int)
	EnchantmentSeed() int64
	ResetEnchantmentSeed()
	Drop(s item.Stack) int
	UnlockedRecipeIDs() []string
	Message(a ...any)
	// CloseMenu closes the menu of the player, as the server closes it.
	CloseMenu(clientRequested bool)
}

// Opts are what Begin needs to start a batch.
type Opts struct {
	Tx    *world.Tx
	Owner Owner
	Inv   Inventories
	// Menu is the menu the player has open, nil if none.
	Menu   *Menu
	Viewer Viewer
	Name   Namer
	Log    *slog.Logger
	// Done is called once the batch committed or rolled back.
	Done func()
}

// Tx is a batch of operations on the slots of a player and its menu. It
// takes effect whole or not at all: an operation that fails leaves the batch
// to be rolled back. The world effects of operations, such as experience
// paid, blocks changed and entities spawned, happen as the operation runs and
// are not rolled back, as before. A Tx is used within the transaction of
// Opts.Tx, and ends with Commit or Rollback.
type Tx struct {
	o       Opts
	changes []Change
	// results are the results of the crafts of the batch not taken yet, in
	// the order they were made; a result taken is left empty in its place.
	results []item.Stack
	ended   bool
	// multiCraft is the special recipe the batch crafts, nil if none.
	multiCraft *MultiCraft
}

// Begin starts a batch. Player.BeginMenu is how adapters start one.
func Begin(o Opts) *Tx {
	return &Tx{o: o}
}

// Item returns the stack in a slot.
func (t *Tx) Item(s Slot) (item.Stack, error) {
	return s.Inv.Item(s.Index)
}

// Changes returns the changes made so far, in order. An adapter reads those
// an operation added after each operation.
func (t *Tx) Changes() []Change {
	return t.changes
}

// TakeResult puts the result of index i of the crafts of the batch in the
// result slot. A craft of one result does it itself, for index 0.
func (t *Tx) TakeResult(i int) error {
	if i < 0 || i >= len(t.results) {
		// The index comes from the client; reject out-of-range values to avoid panics.
		return fmt.Errorf("invalid pending result slot: %v", i)
	}
	res := t.results[i]
	if res.Empty() {
		return fmt.Errorf("tried duplicating created result: %v", i)
	}
	t.results[i] = item.Stack{}
	return t.set(t.named(Slot{Inv: t.o.Inv.UI, Index: UIResult}, RoleResult), res)
}

// createResults adds the results of a craft to the results of the batch.
// The client takes each of several results itself (TakeResult); a single
// result is taken at once, as index 0.
func (t *Tx) createResults(results ...item.Stack) error {
	t.results = append(t.results, results...)
	if len(results) > 1 {
		return nil
	}
	return t.TakeResult(0)
}

// named returns the slot s named by the Namer of the batch for the role r.
func (t *Tx) named(s Slot, r Role) Ref {
	return Ref{Slot: s, Name: t.o.Name(s, r)}
}

// set sets a slot to s and records the change. A slot that cannot be read,
// or that does not take s (ErrSlotRefusesItem), fails the set before
// anything changes: its inventory would leave it as it was. A stack put in
// a disabled slot of the crafter of the menu enables the slot first.
func (t *Tx) set(r Ref, s item.Stack) error {
	before, err := r.Inv.Item(r.Index)
	if err != nil {
		return err
	}
	if err := ensureAccepts(r, s); err != nil {
		return err
	}
	if m := t.o.Menu; m != nil && r.Inv == m.Inventory() && !s.Empty() {
		pos := m.Pos()
		if crafter, ok := t.o.Tx.Block(pos).(block.Crafter); ok && crafter.DisabledSlots&(1<<r.Index) != 0 {
			crafter.DisabledSlots &^= 1 << r.Index
			t.o.Tx.SetBlock(pos, crafter, nil)
			crafter.NotifySlotState(r.Index)
		}
	}
	if err := r.Inv.SetItem(r.Index, s); err != nil {
		return err
	}
	t.changes = append(t.changes, Change{Ref: r, Before: before, After: s})
	return nil
}

// Commit ends the batch. If a slot of the inventory of a block container of
// the menu changed, the comparators reading it, and those of the other half
// of a pair, are updated.
func (t *Tx) Commit() {
	defer t.end()
	m := t.o.Menu
	if m == nil {
		return
	}
	for _, c := range t.changes {
		if c.Ref.Inv != m.Inventory() {
			continue
		}
		tx, pos := t.o.Tx, m.Pos()
		opened := tx.Block(pos)
		if _, ok := opened.(block.Container); !ok {
			return
		}
		block.NotifyComparatorUpdate(pos, tx)
		if paired, ok := opened.(interface{ Paired() bool }); ok && paired.Paired() {
			for _, face := range cube.HorizontalFaces() {
				side := pos.Side(face)
				if _, ok := tx.Block(side).(interface{ Paired() bool }); ok {
					block.NotifyComparatorUpdate(side, tx)
				}
			}
		}
		return
	}
}

// Rollback ends the batch with every slot it changed restored, the last
// change first, so that each slot holds what it held before the batch.
func (t *Tx) Rollback() {
	defer t.end()
	for i := len(t.changes) - 1; i >= 0; i-- {
		c := t.changes[i]
		_ = c.Ref.Inv.SetItem(c.Ref.Index, c.Before)
	}
}

// end calls the Done function of the batch the first time it ends.
func (t *Tx) end() {
	if t.ended {
		return
	}
	t.ended = true
	if t.o.Done != nil {
		t.o.Done()
	}
}
