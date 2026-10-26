package block

import (
	"sync"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/item/recipe"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
)

// brewer is a struct that may be embedded by blocks that can brew potions, such as brewing stands. It ports
// BrewingStandBlockEntity on Bedrock's slots: 0 holds the ingredient, 1 to 3 the potions and 4 the fuel.
type brewer struct {
	mu sync.Mutex

	viewers   map[ContainerViewer]struct{}
	inventory *inventory.Inventory

	// brewTime counts down the ticks left of the brew.
	brewTime int
	// fuelAmount is the brews the burning blaze powder has left and
	// fuelTotal the brews it gave.
	fuelAmount int32
	fuelTotal  int32
	// ingredient is the item the brew started with; changing it stops the
	// brew.
	ingredient world.Item
}

const (
	// brewingTicks is how long a brew takes, BrewingStandBlockEntity's 400
	// ticks at the default speed.
	brewingTicks = 400
	// brewingFuelUses is the brews a blaze powder gives,
	// cooking/brewing_default_uses.
	brewingFuelUses = 20
)

// newBrewer creates a new initialised brewer. The inventory is properly initialised.
func newBrewer() *brewer {
	b := &brewer{viewers: make(map[ContainerViewer]struct{})}
	b.inventory = inventory.New(5, func(slot int, _, item item.Stack) {
		b.mu.Lock()
		defer b.mu.Unlock()
		for viewer := range b.viewers {
			viewer.ViewSlotChange(slot, item)
		}
	})
	return b
}

// brewingPotionInput reports if a stack goes in a potion slot, as
// PotionIngredient.isPotionInput: a potion, splash or lingering potion or a
// glass bottle.
func brewingPotionInput(s item.Stack) bool {
	switch s.Item().(type) {
	case item.Potion, item.SplashPotion, item.LingeringPotion, item.GlassBottle:
		return true
	}
	return false
}

// brewingCanPlace ports BrewingStandBlockEntity.canPlaceItem for a slot.
func (b *brewer) brewingCanPlace(slot int, s item.Stack) bool {
	switch slot {
	case 4:
		_, ok := s.Item().(item.BlazePowder)
		return ok
	case 0:
		return recipe.ValidBrewingReagent(s.Item())
	}
	held, _ := b.inventory.Item(slot)
	return brewingPotionInput(s) && held.Empty()
}

// InsertItem inserts an item from a hopper as the brewing stand's
// WorldlyContainer accepts it: a hopper above fills the ingredient slot, one
// at the side the first empty potion slot or the fuel slot.
func (b *brewer) InsertItem(h Hopper, pos cube.Pos, tx *world.Tx) bool {
	slots := []int{1, 2, 3, 4}
	if h.Facing == cube.FaceDown {
		slots = []int{0}
	}
	for sourceSlot, sourceStack := range h.inventory.Slots() {
		if sourceStack.Empty() {
			continue
		}
		for _, slot := range slots {
			if !b.brewingCanPlace(slot, sourceStack) {
				continue
			}
			it, _ := b.inventory.Item(slot)
			if !it.Empty() && (!sourceStack.Comparable(it) || it.Count() >= it.MaxCount()) {
				continue
			}
			stack := sourceStack.Grow(1 - sourceStack.Count())
			if !it.Empty() {
				stack = it.Grow(1)
			}
			_ = b.inventory.SetItem(slot, stack)
			_ = h.inventory.SetItem(sourceSlot, sourceStack.Grow(-1))
			notifyComparatorUpdate(pos, tx)
			return true
		}
	}
	return false
}

// ExtractItem lets a hopper below take the potions, then a glass bottle from
// the ingredient slot, as SLOTS_FOR_DOWN and canTakeItemThroughFace.
func (b *brewer) ExtractItem(h Hopper, pos cube.Pos, tx *world.Tx) bool {
	for _, slot := range []int{1, 2, 3, 0} {
		stack, _ := b.inventory.Item(slot)
		if stack.Empty() {
			continue
		}
		if _, bottle := stack.Item().(item.GlassBottle); slot == 0 && !bottle {
			continue
		}
		if _, err := h.inventory.AddItem(stack.Grow(1 - stack.Count())); err != nil {
			continue
		}
		_ = b.inventory.SetItem(slot, stack.Grow(-1))
		notifyComparatorUpdate(pos, tx)
		return true
	}
	return false
}

// Duration returns the remaining duration of the brewing process.
func (b *brewer) Duration() time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()
	return time.Duration(b.brewTime) * time.Millisecond * 50
}

// Fuel returns the fuel and maximum fuel of the brewer.
func (b *brewer) Fuel() (fuel, maxFuel int32) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.fuelAmount, b.fuelTotal
}

// Inventory returns the inventory of the brewer.
func (b *brewer) Inventory(*world.Tx, cube.Pos) *inventory.Inventory {
	return b.inventory
}

// AddViewer adds a viewer to the brewer, so that it is updated whenever the inventory of the brewer is changed.
func (b *brewer) AddViewer(v ContainerViewer, _ *world.Tx, _ cube.Pos) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.viewers[v] = struct{}{}
}

// RemoveViewer removes a viewer from the brewer, so that slot updates in the inventory are no longer sent to
// it.
func (b *brewer) RemoveViewer(v ContainerViewer, _ *world.Tx, _ cube.Pos) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.viewers, v)
}

// setDuration sets the brew duration of the brewer to the given duration. A
// brew loaded with its ingredient continues with that ingredient.
func (b *brewer) setDuration(duration time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.brewTime = int(duration.Milliseconds() / 50)
	if ingredient, _ := b.inventory.Item(0); b.brewTime > 0 && !ingredient.Empty() {
		b.ingredient = ingredient.Item()
	}
}

// setFuel sets the fuel of the brewer to the given fuel and maximum fuel.
func (b *brewer) setFuel(fuel, maxFuel int32) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.fuelAmount, b.fuelTotal = fuel, maxFuel
}

// brewingResults returns what the ingredient brews each potion slot into,
// and whether any of them brews, as BrewingStandBlockEntity.isBrewable.
func brewingResults(block string, potions [3]item.Stack, ingredient item.Stack) (out [3]item.Stack, brewable bool) {
	out = potions
	if ingredient.Empty() || !recipe.ValidBrewingReagent(ingredient.Item()) {
		return out, false
	}
	for i, p := range potions {
		if p.Empty() {
			continue
		}
		if result, ok := recipe.Perform(block, p.Item(), ingredient.Item()); ok {
			out[i], brewable = result[0], true
		}
	}
	return out, brewable
}

func sameItem(a, b world.Item) bool {
	if a == nil || b == nil {
		return a == b
	}
	nameA, metaA := a.EncodeItem()
	nameB, metaB := b.EncodeItem()
	return nameA == nameB && metaA == metaB
}

// tickBrewing ports BrewingStandBlockEntity.serverTick: blaze powder is
// burned for 20 brews once the fuel is out, a brew takes one of them as it
// starts and 400 ticks to finish, and it stops if nothing can be brewed any
// more or the ingredient is changed. It returns whether the inventory
// changed.
func (b *brewer) tickBrewing(block string, pos cube.Pos, tx *world.Tx) bool {
	b.mu.Lock()
	prevBrew, prevFuel, prevTotal := b.brewTime, b.fuelAmount, b.fuelTotal
	ingredient, _ := b.inventory.Item(0)
	fuel, _ := b.inventory.Item(4)
	var potions [3]item.Stack
	for i := range potions {
		potions[i], _ = b.inventory.Item(i + 1)
	}

	changed := false
	var sets []func()
	if _, ok := fuel.Item().(item.BlazePowder); ok && b.fuelAmount <= 0 {
		b.fuelAmount, b.fuelTotal = brewingFuelUses, brewingFuelUses
		remaining := fuel.Grow(-1)
		sets = append(sets, func() { _ = b.inventory.SetItem(4, remaining) })
		changed = true
	}

	results, brewable := brewingResults(block, potions, ingredient)
	brewed := false
	switch {
	case b.brewTime > 0:
		b.brewTime--
		if b.brewTime == 0 && brewable {
			brewed, changed = true, true
			for i, r := range results {
				sets = append(sets, func() { _ = b.inventory.SetItem(i+1, r) })
			}
			remaining := ingredient.Grow(-1)
			if remainder, ok := brewingRemainder(ingredient.Item()); ok {
				if remaining.Empty() {
					remaining = remainder
				} else {
					sets = append(sets, func() { dropItem(tx, remainder, pos.Vec3()) })
				}
			}
			sets = append(sets, func() { _ = b.inventory.SetItem(0, remaining) })
		} else if !brewable || !sameItem(ingredient.Item(), b.ingredient) {
			b.brewTime = 0
		}
	case brewable && b.fuelAmount > 0:
		b.fuelAmount--
		b.brewTime = brewingTicks
		b.ingredient = ingredient.Item()
	}
	brew, fuelAmount, fuelTotal := b.brewTime, b.fuelAmount, b.fuelTotal
	viewers := make([]ContainerViewer, 0, len(b.viewers))
	for v := range b.viewers {
		viewers = append(viewers, v)
	}
	b.mu.Unlock()

	for _, set := range sets {
		set()
	}
	if brewed {
		tx.PlaySound(pos.Vec3Centre(), sound.PotionBrewed{})
	}
	const tick = time.Millisecond * 50
	for _, v := range viewers {
		v.ViewBrewingUpdate(time.Duration(prevBrew)*tick, time.Duration(brew)*tick, prevFuel, fuelAmount, prevTotal, fuelTotal)
	}
	return changed
}

// brewingRemainder returns what an ingredient leaves once brewed, its
// crafting remainder: a glass bottle for dragon's breath.
func brewingRemainder(it world.Item) (item.Stack, bool) {
	if _, ok := it.(item.DragonBreath); ok {
		return item.NewStack(item.GlassBottle{}, 1), true
	}
	return item.Stack{}, false
}
