package block

import (
	"math"
	"math/rand/v2"
	"slices"
	"sync"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
)

// smelter is a struct that may be embedded by blocks that can smelt blocks and items, such as blast furnaces, furnaces,
// and smokers. It ports AbstractFurnaceBlockEntity.
type smelter struct {
	mu sync.Mutex

	viewers   map[ContainerViewer]struct{}
	inventory *inventory.Inventory

	// litTime is the ticks the burning fuel has left and litDuration the
	// ticks it burns in all.
	litTime, litDuration int
	// cookingTimer counts the ticks the input has cooked and
	// cookingTotalTime the ticks it takes, 0 until the input's recipe is
	// looked up.
	cookingTimer, cookingTotalTime int
	// recipesUsed counts the recipes cooked since a player last took from
	// the result slot.
	recipesUsed map[string]int
	// storedXP is experience kept by actors that only stored BDS's
	// StoredXPInt.
	storedXP int
}

// newSmelter initialises a new smelter with the given remaining, maximum, and cook durations and XP, and returns it.
func newSmelter() *smelter {
	s := &smelter{viewers: make(map[ContainerViewer]struct{}), recipesUsed: make(map[string]int)}
	s.inventory = inventory.New(3, func(slot int, before, after item.Stack) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if slot == 0 && (after.Empty() || before.Empty() || !before.Comparable(after)) {
			// AbstractFurnaceBlockEntity.setItem starts cooking a different
			// input from the beginning.
			s.cookingTimer, s.cookingTotalTime = 0, 0
		}
		for viewer := range s.viewers {
			viewer.ViewSlotChange(slot, after)
		}
	})
	return s
}

// InsertItem inserts an item from a hopper as the furnace's
// WorldlyContainer accepts it: a hopper above fills the input slot and one
// at the side the fuel slot.
func (s *smelter) InsertItem(h Hopper, pos cube.Pos, tx *world.Tx) bool {
	slot := 1
	if h.Facing == cube.FaceDown {
		slot = 0
	}
	for sourceSlot, sourceStack := range h.inventory.Slots() {
		if sourceStack.Empty() {
			continue
		}
		it, _ := s.inventory.Item(slot)
		if slot == 1 && item.CookingBurnTime(sourceStack.Item(), false) <= 0 {
			continue
		}
		if !it.Empty() && (!sourceStack.Comparable(it) || it.Count() >= it.MaxCount()) {
			continue
		}
		stack := sourceStack.Grow(1 - sourceStack.Count())
		if !it.Empty() {
			stack = it.Grow(1)
		}
		_ = s.inventory.SetItem(slot, stack)
		_ = h.inventory.SetItem(sourceSlot, sourceStack.Grow(-1))
		notifyComparatorUpdate(pos, tx)
		return true
	}
	return false
}

// ExtractItem lets a hopper below take from the result slot, then from the
// fuel slot if it holds a bucket, as AbstractFurnaceBlockEntity's
// SLOTS_FOR_DOWN and canTakeItemThroughFace.
func (s *smelter) ExtractItem(h Hopper, pos cube.Pos, tx *world.Tx) bool {
	for sourceSlot, sourceStack := range s.inventory.Slots() {
		if sourceStack.Empty() {
			continue
		}

		if sourceSlot == 0 {
			continue
		}

		if sourceSlot == 1 {
			if item.CookingBurnTime(sourceStack.Item(), false) > 0 {
				continue
			}
		}

		_, err := h.inventory.AddItemInSlotOrder(sourceStack.Grow(-sourceStack.Count() + 1))
		if err != nil {
			// The hopper is full.
			continue
		}

		_ = s.Inventory(tx, pos).SetItem(sourceSlot, sourceStack.Grow(-1))
		notifyComparatorUpdate(pos, tx)
		return true
	}

	return false
}

// Durations returns the remaining, maximum, and cook durations of the smelter.
func (s *smelter) Durations() (remaining time.Duration, max time.Duration, cook time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	const tick = time.Millisecond * 50
	return time.Duration(s.litTime) * tick, time.Duration(s.litDuration) * tick, time.Duration(s.cookingTimer) * tick
}

// Experience returns the experience the recipes the smelter cooked are worth
// at least, without the chance of a further point for each fraction.
func (s *smelter) Experience() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	xp := s.storedXP
	for id, n := range s.recipesUsed {
		if r, ok := item.CookingRecipeByID(id); ok {
			xp += int(math.Floor(float64(float32(n) * r.Experience)))
		}
	}
	return xp
}

// ResetExperience returns the experience the recipes the smelter cooked are
// worth and forgets them, as
// AbstractFurnaceBlockEntity.getRecipesToAwardAndPopExperience: each recipe
// gives its experience times the times it was cooked, rounded down, and a
// further point with the chance of the fraction left.
func (s *smelter) ResetExperience() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	xp := s.storedXP
	ids := make([]string, 0, len(s.recipesUsed))
	for id := range s.recipesUsed {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		r, ok := item.CookingRecipeByID(id)
		if !ok {
			continue
		}
		amount := float32(s.recipesUsed[id]) * r.Experience
		whole := float32(math.Floor(float64(amount)))
		xp += int(whole)
		if frac := amount - whole; frac != 0 && rand.Float32() < frac {
			xp++
		}
	}
	s.storedXP = 0
	clear(s.recipesUsed)
	return xp
}

// Inventory returns the inventory of the furnace.
func (s *smelter) Inventory(*world.Tx, cube.Pos) *inventory.Inventory {
	return s.inventory
}

// AddViewer adds a viewer to the furnace, so that it is updated whenever the inventory of the furnace is changed.
func (s *smelter) AddViewer(v ContainerViewer, _ *world.Tx, _ cube.Pos) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.viewers[v] = struct{}{}
}

// RemoveViewer removes a viewer from the furnace, so that slot updates in the inventory are no longer sent to
// it.
func (s *smelter) RemoveViewer(v ContainerViewer, _ *world.Tx, _ cube.Pos) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.viewers) == 0 {
		// No viewers.
		return
	}
	delete(s.viewers, v)
}

// setDurations sets the remaining, maximum, and cook durations of the smelter to the given values.
func (s *smelter) setDurations(remaining, max, cook time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.litTime, s.litDuration, s.cookingTimer = int(remaining.Milliseconds()/50), int(max.Milliseconds()/50), int(cook.Milliseconds()/50)
}

// smelterTotalCookTime ports AbstractFurnaceBlockEntity.getTotalCookTime:
// the recipe's cooking time divided by the furnace's speed multiplier, 2 in
// the fast smokers and blast furnaces.
func smelterTotalCookTime(r item.CookingRecipe, fast bool) int {
	if fast {
		return (r.CookingTime + 1) / 2
	}
	return r.CookingTime
}

// tickSmelting ports AbstractFurnaceBlockEntity.serverTick for a furnace
// cooking recipes of the kind passed. It returns whether the furnace is lit
// and whether its inventory changed.
func (s *smelter) tickSmelting(kind item.CookingKind, fast bool) (bool, bool) {
	s.mu.Lock()
	prevCook, prevLit, prevDuration := s.cookingTimer, s.litTime, s.litDuration
	input, _ := s.inventory.Item(0)
	fuel, _ := s.inventory.Item(1)
	result, _ := s.inventory.Item(2)

	changed := false
	isLit := false
	if s.litTime > 0 {
		s.litTime--
		isLit = s.litTime > 0
	}
	var sets []func()
	switch {
	case isLit || !fuel.Empty() && !input.Empty():
		if input.Empty() {
			s.cookingTimer = 0
			break
		}
		recipe, ok := item.CookingRecipeFor(kind, input.Item())
		if !ok {
			break
		}
		if s.cookingTotalTime == 0 {
			s.cookingTotalTime = smelterTotalCookTime(recipe, fast)
		}
		if !smelterCanBurn(result, recipe.Product) {
			s.cookingTimer = 0
			break
		}
		if !isLit {
			burn := item.CookingBurnTime(fuel.Item(), fast)
			s.litTime, s.litDuration = burn, burn
			if burn > 0 {
				remaining := fuel.Grow(-1)
				if residue, ok := item.CookingFuelRemainder(fuel.Item()); ok {
					// The remainder replaces fuel used up and is dropped
					// otherwise.
					if remaining.Empty() {
						remaining = residue
					}
				}
				sets = append(sets, func() { _ = s.inventory.SetItem(1, remaining) })
				fuel = remaining
				isLit, changed = true, true
			}
		}
		if !isLit {
			s.cookingTimer = 0
			break
		}
		if s.cookingTimer++; s.cookingTimer >= s.cookingTotalTime {
			s.cookingTimer = 0
			s.cookingTotalTime = smelterTotalCookTime(recipe, fast)
			if result.Empty() {
				result = recipe.Product
			} else {
				result = result.Grow(recipe.Product.Count())
			}
			sets = append(sets, func() { _ = s.inventory.SetItem(2, result) })
			if sponge, ok := input.Item().(Sponge); ok && sponge.Wet {
				if b, ok := fuel.Item().(item.Bucket); ok && b.Empty() {
					sets = append(sets, func() {
						_ = s.inventory.SetItem(1, item.NewStack(item.Bucket{Content: item.LiquidBucketContent(Water{Depth: 8, Still: true})}, 1))
					})
				}
			}
			remaining := input.Grow(-1)
			sets = append(sets, func() { _ = s.inventory.SetItem(0, remaining) })
			s.recipesUsed[recipe.ID]++
			changed = true
		}
	case s.cookingTimer > 0:
		if s.cookingTotalTime == 0 {
			s.cookingTotalTime = 200
			if recipe, ok := item.CookingRecipeFor(kind, input.Item()); ok {
				s.cookingTotalTime = smelterTotalCookTime(recipe, fast)
			}
		}
		s.cookingTimer = max(min(s.cookingTimer-2, s.cookingTotalTime), 0)
	}
	if !isLit {
		s.litDuration = 0
	}
	cook, lit, duration := s.cookingTimer, s.litTime, s.litDuration
	viewers := make([]ContainerViewer, 0, len(s.viewers))
	for v := range s.viewers {
		viewers = append(viewers, v)
	}
	s.mu.Unlock()

	for _, set := range sets {
		set()
	}
	if prevCook != cook || prevLit != lit || prevDuration != duration {
		const tick = time.Millisecond * 50
		requirement := 200
		if fast {
			requirement = 100
		}
		prevBurn, burn := smelterBurnDurationTicks(prevLit, prevDuration, requirement), smelterBurnDurationTicks(lit, duration, requirement)
		prevMax, maxBurn := 0, 0
		if prevDuration > 0 {
			prevMax = requirement
		}
		if duration > 0 {
			maxBurn = requirement
		}
		// Viewers are sent the fuel left scaled to the cooking time, as the
		// client draws the flame against it.
		for _, v := range viewers {
			v.ViewFurnaceUpdate(time.Duration(prevCook)*tick, time.Duration(cook)*tick, time.Duration(prevBurn)*tick, time.Duration(burn)*tick, time.Duration(prevMax)*tick, time.Duration(maxBurn)*tick)
		}
	}
	return isLit, changed
}

// smelterCanBurn ports AbstractFurnaceBlockEntity.canBurn: the result slot
// is empty or holds the same item with room for another product.
func smelterCanBurn(result, product item.Stack) bool {
	if product.Empty() {
		return false
	}
	if result.Empty() {
		return true
	}
	if !result.Comparable(product) {
		return false
	}
	return result.Count()+product.Count() <= min(64, product.MaxCount())
}

// smelterStoredXP reads the experience a furnace, blast furnace or smoker
// stores. BDS writes it as the int StoredXPInt; worlds saved before this
// server did too wrote a short, under either spelling.
func smelterStoredXP(data map[string]any) int {
	if xp, ok := data["StoredXPInt"].(int32); ok {
		return int(xp)
	}
	if xp, ok := data["StoredXPInt"].(int16); ok {
		return int(xp)
	}
	xp, _ := data["StoredXpInt"].(int16)
	return int(xp)
}

// broken drops the contents of a removed smelter and the experience of the
// recipes it cooked, as AbstractFurnaceBlockEntity.preRemoveSideEffects.
func (s *smelter) broken(pos cube.Pos, tx *world.Tx) {
	for _, i := range s.inventory.Clear() {
		dropItem(tx, i, pos.Vec3())
	}
	for _, orb := range tx.World().EntityRegistry().Config().ExperienceOrbs(pos.Vec3Centre(), s.ResetExperience()) {
		tx.AddEntity(orb)
	}
}
