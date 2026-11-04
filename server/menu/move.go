package menu

import (
	"fmt"
	"math"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/event"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world/sound"
)

// Move moves count items from one slot to another: all or part of a stack
// into an empty slot or onto a stack of the same item, or into or out of a
// bundle. It calls the HandleTake handler of the source's inventory and the
// HandlePlace handler of the destination's, and fails if either cancels. It
// refuses a destination that is the source (ErrSameSlot) or that does not
// take the items (ErrSlotRefusesItem), to take a stack cursed with binding
// out of an armour slot (outside creative), and an item locked in the
// inventory or in its slot out of it. Taking the result of a furnace, blast
// furnace or smoker drops the experience it collected at the player's feet.
func (t *Tx) Move(from, to Ref, count int) error {
	if err := t.refused(); err != nil {
		return err
	}
	if t.sameSlot(from.Slot, to.Slot) {
		// The items would be taken out of the slot and added to what it
		// held before.
		return fmt.Errorf("move %v items: %w", count, ErrSameSlot)
	}
	i, _ := t.Item(from.Slot)
	if err := t.ensureBindingAllowsRemoval(i, from.Slot, to.Slot); err != nil {
		return err
	}
	if i.Count() < count {
		return fmt.Errorf("client tried subtracting %v from item count, but there are only %v", count, i.Count())
	}
	dest, _ := t.Item(to.Slot)
	if err := t.ensureUnlockedForInventoryMove(i, from.Slot, to.Slot); err != nil {
		return err
	}
	if destinationBundle, ok := dest.Item().(item.Bundle); ok {
		return t.moveIntoBundle(from, to, count, i, dest, destinationBundle)
	}
	if sourceBundle, ok := i.Item().(item.Bundle); ok && !dest.Empty() {
		return t.moveOutOfBundle(from, to, count, i, dest, sourceBundle)
	}
	if !i.Comparable(dest) {
		return fmt.Errorf("client tried transferring %v to %v, but the stacks are incomparable", i, dest)
	}
	if i.Count() < count {
		return fmt.Errorf("client tried subtracting %v from item count, but there are only %v", count, i.Count())
	}
	if (dest.Count()+count > dest.MaxCount()) && !dest.Empty() {
		return fmt.Errorf("client tried adding %v to item count %v, but max is %v", count, dest.Count(), dest.MaxCount())
	}
	if dest.Empty() {
		dest = i.Grow(-math.MaxInt32)
	}
	if err := ensureAccepts(to, dest.Grow(count)); err != nil {
		return err
	}

	ctx := event.C(inventory.Holder(t.o.Owner))
	_ = call(ctx, from.Index, i.Grow(count-i.Count()), from.Inv.Handler().HandleTake)
	err := call(ctx, to.Index, i.Grow(count-i.Count()), to.Inv.Handler().HandlePlace)
	if err != nil {
		return err
	}

	if err := t.set(from, i.Grow(-count)); err != nil {
		return err
	}
	if err := t.set(to, dest.Grow(count)); err != nil {
		return err
	}
	t.collectRewards(from.Slot)
	return nil
}

// moveIntoBundle moves count items of incoming, the stack of from, into
// bundle, the stack bundleStack of to.
func (t *Tx) moveIntoBundle(from, to Ref, count int, incoming, bundleStack item.Stack, bundle item.Bundle) error {
	updated, remaining := bundle.Insert(bundleStack, incoming, count)
	if remaining.Count() == incoming.Count() {
		return fmt.Errorf("item does not fit in bundle")
	}
	moved := incoming.Grow(-remaining.Count())
	ctx := event.C(inventory.Holder(t.o.Owner))
	_ = call(ctx, from.Index, moved, from.Inv.Handler().HandleTake)
	if err := call(ctx, to.Index, moved, to.Inv.Handler().HandlePlace); err != nil {
		return err
	}
	if err := t.set(from, remaining); err != nil {
		return err
	}
	if err := t.set(to, updated); err != nil {
		return err
	}
	t.o.Tx.PlaySound(t.o.Owner.Position(), sound.Custom{Name: "item.bundle.insert", Volume: 0.8, Pitch: 1})
	t.collectRewards(from.Slot)
	return nil
}

// moveOutOfBundle moves the first count items of bundle, the stack
// bundleStack of from, onto destination, the stack of to.
func (t *Tx) moveOutOfBundle(from, to Ref, count int, bundleStack, destination item.Stack, bundle item.Bundle) error {
	updated, extracted, ok := bundle.RemoveFirstCount(bundleStack, count)
	if !ok {
		return fmt.Errorf("bundle is empty")
	}
	if !destination.Empty() && !destination.Comparable(extracted) {
		return fmt.Errorf("bundle content is incomparable with destination stack")
	}
	if !destination.Empty() && destination.Count()+extracted.Count() > destination.MaxCount() {
		return fmt.Errorf("bundle content exceeds destination stack limit")
	}
	placed := extracted
	if !destination.Empty() {
		placed = destination.Grow(extracted.Count())
	}
	if err := ensureAccepts(to, placed); err != nil {
		return err
	}
	ctx := event.C(inventory.Holder(t.o.Owner))
	_ = call(ctx, from.Index, extracted, from.Inv.Handler().HandleTake)
	if err := call(ctx, to.Index, extracted, to.Inv.Handler().HandlePlace); err != nil {
		return err
	}
	if err := t.set(from, updated); err != nil {
		return err
	}
	if err := t.set(to, placed); err != nil {
		return err
	}
	t.o.Tx.PlaySound(t.o.Owner.Position(), sound.Custom{Name: "item.bundle.remove_one", Volume: 0.8, Pitch: 1})
	t.collectRewards(from.Slot)
	return nil
}

// Swap swaps the stacks of two slots, with the same checks as Move: either
// slot must take the stack of the other, and the handlers of both
// inventories are called for the take and the place of both stacks, and
// fail it if any cancels.
func (t *Tx) Swap(a, b Ref) error {
	if err := t.refused(); err != nil {
		return err
	}
	if t.sameSlot(a.Slot, b.Slot) {
		return fmt.Errorf("swap: %w", ErrSameSlot)
	}
	i, _ := t.Item(a.Slot)
	dest, _ := t.Item(b.Slot)
	if err := t.ensureBindingAllowsRemoval(i, a.Slot, b.Slot); err != nil {
		return err
	}
	if err := t.ensureBindingAllowsRemoval(dest, b.Slot, a.Slot); err != nil {
		return err
	}
	if err := t.ensureUnlockedForInventoryMove(i, a.Slot, b.Slot); err != nil {
		return err
	}
	if err := t.ensureUnlockedForInventoryMove(dest, b.Slot, a.Slot); err != nil {
		return err
	}
	if err := ensureAccepts(a, dest); err != nil {
		return err
	}
	if err := ensureAccepts(b, i); err != nil {
		return err
	}

	ctx := event.C(inventory.Holder(t.o.Owner))
	_ = call(ctx, a.Index, i, a.Inv.Handler().HandleTake)
	_ = call(ctx, a.Index, dest, a.Inv.Handler().HandlePlace)
	_ = call(ctx, b.Index, dest, b.Inv.Handler().HandleTake)
	err := call(ctx, b.Index, i, b.Inv.Handler().HandlePlace)
	if err != nil {
		return err
	}

	if err := t.set(a, dest); err != nil {
		return err
	}
	if err := t.set(b, i); err != nil {
		return err
	}
	t.collectRewards(a.Slot)
	t.collectRewards(b.Slot)
	return nil
}

// ensureAccepts refuses to put s in the slot of r if its inventory does not
// take it there: the inventory would leave the slot as it was while the
// operation went on, and the items moved there would vanish.
func ensureAccepts(r Ref, s item.Stack) error {
	if !r.Inv.Accepts(r.Index, s) {
		return fmt.Errorf("slot %d does not take %v: %w", r.Index, s, ErrSlotRefusesItem)
	}
	return nil
}

// smelter is a block that smelts and collects the experience of what it
// smelted: a furnace, blast furnace or smoker.
type smelter interface {
	// ResetExperience resets the collected experience of the smelter, and returns the amount of experience that was reset.
	ResetExperience() int
}

// collectRewards drops the experience the smelter of the menu collected at
// the player's feet if s, the slot items were taken from, is its result
// slot, the last of its inventory.
func (t *Tx) collectRewards(s Slot) {
	if m := t.o.Menu; m != nil && s.Inv == m.Inventory() && s.Index == s.Inv.Size()-1 {
		if f, ok := t.o.Tx.Block(m.Pos()).(smelter); ok {
			// AbstractFurnaceBlockEntity.awardUsedRecipesAndPopExperience
			// spawns the orbs at the player's feet.
			for _, o := range entity.NewExperienceOrbs(t.o.Owner.Position(), f.ResetExperience()) {
				t.o.Tx.AddEntity(o)
			}
		}
	}
}

// call uses an event.Context, slot and item.Stack to call the event handler function passed. An error is returned if
// the event.Context was cancelled either before or after the call.
func call(ctx *inventory.Context, slot int, it item.Stack, f func(ctx *inventory.Context, slot int, it item.Stack)) error {
	if ctx.Cancelled() {
		return fmt.Errorf("action was cancelled")
	}
	f(ctx, slot, it)
	if ctx.Cancelled() {
		return fmt.Errorf("action was cancelled")
	}
	return nil
}
