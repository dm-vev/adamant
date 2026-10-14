package block

import (
	"fmt"
	"math"
	"math/rand/v2"
	"strings"
	"sync"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/item/recipe"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Crafter is a redstone-operated nine-slot crafting container.
type Crafter struct {
	solid
	bass

	Facing cube.Face
	Top cube.Face
	Triggered bool
	Crafting bool
	CustomName string
	DisabledSlots uint16
	CraftingTicksRemaining int32

	inventory *inventory.Inventory
	viewerMu *sync.RWMutex
	viewers map[ContainerViewer]struct{}
}

// NewCrafter creates an empty crafter with all nine slots enabled.
func NewCrafter() Crafter {
	m := new(sync.RWMutex)
	v := make(map[ContainerViewer]struct{}, 1)
	return Crafter{
		Facing: cube.FaceNorth, Top: cube.FaceUp,
		inventory: inventory.New(9, func(slot int, _, after item.Stack) {
			m.RLock()
			defer m.RUnlock()
			for viewer := range v { viewer.ViewSlotChange(slot, after) }
		}),
		viewerMu: m, viewers: v,
	}
}

func (c Crafter) ContainerSize() int { return 9 }
func (c Crafter) Inventory(*world.Tx, cube.Pos) *inventory.Inventory { return c.inventory }
func (c Crafter) WithName(a ...any) world.Item { c.CustomName = strings.TrimSuffix(fmt.Sprintln(a...), "\n"); return c }
func (c Crafter) AddViewer(v ContainerViewer, _ *world.Tx, _ cube.Pos) { c.viewerMu.Lock(); defer c.viewerMu.Unlock(); c.viewers[v] = struct{}{} }
func (c Crafter) RemoveViewer(v ContainerViewer, _ *world.Tx, _ cube.Pos) { c.viewerMu.Lock(); defer c.viewerMu.Unlock(); delete(c.viewers, v) }

// SetSlotDisabled changes an empty slot's enabled state to the requested value.
func (c Crafter) SetSlotDisabled(slot int, disabled bool) (Crafter, bool) {
	if slot < 0 || slot >= 9 {
		return c, false
	}
	stack, _ := c.inventory.Item(slot)
	if !stack.Empty() {
		return c, false
	}
	wasDisabled := c.DisabledSlots&(1<<slot) != 0
	if wasDisabled == disabled {
		return c, false
	}
	if disabled {
		c.DisabledSlots |= 1 << slot
	} else {
		c.DisabledSlots &^= 1 << slot
	}
	return c, true
}

// NotifySlotState broadcasts a slot's disabled state to current viewers.
func (c Crafter) NotifySlotState(slot int) {
	disabled := c.DisabledSlots&(1<<slot) != 0
	c.viewerMu.RLock()
	defer c.viewerMu.RUnlock()
	for viewer := range c.viewers {
		if v, ok := viewer.(interface{ ViewCrafterSlotState(int, bool) }); ok {
			v.ViewCrafterSlotState(slot, disabled)
		}
	}
}

func (c Crafter) NotifyTriggered() {
	c.viewerMu.RLock()
	defer c.viewerMu.RUnlock()
	for viewer := range c.viewers {
		if v, ok := viewer.(interface{ ViewCrafterTriggered(bool) }); ok {
			v.ViewCrafterTriggered(c.Triggered)
		}
	}
}

func (c Crafter) Activate(pos cube.Pos, _ cube.Face, tx *world.Tx, u item.User, _ *item.UseContext) bool {
	if opener, ok := u.(ContainerOpener); ok { opener.OpenBlockContainer(pos, tx); return true }
	return true
}

func (c Crafter) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, c)
	if !used {
		return false
	}
	c = NewCrafter()
	// BDS faces a crafter like a dispenser. Facing up or down, its top points
	// back at the player, both ways; Java points an upward crafter's top away.
	c.Facing = placerFacing(user, pos)
	c.Top = cube.FaceUp
	if c.Facing.Axis() == cube.Y {
		c.Top = user.Rotation().Direction().Opposite().Face()
	}
	place(tx, pos, c, user, ctx)
	return placed(ctx)
}

func (c Crafter) BreakInfo() BreakInfo {
	return newBreakInfo(3.5, pickaxeHarvestable, pickaxeEffective, oneOf(Crafter{})).withBlastResistance(3.5).withBreakHandler(func(pos cube.Pos, tx *world.Tx, _ item.User) {
		for _, stack := range c.Inventory(tx, pos).Clear() { dropItem(tx, stack, pos.Vec3()) }
	})
}

func (c Crafter) RedstonePowerUpdate(_ cube.Pos, _ *world.Tx, power int) (world.Block, bool) {
	powered := power > 0
	if powered == c.Triggered { return c, false }
	c.Triggered = powered
	if !powered { c.Crafting = false; c.CraftingTicksRemaining = 0 }
	c.NotifyTriggered()
	return c, true
}
func (c Crafter) RedstonePowerPostUpdate(pos cube.Pos, tx *world.Tx, _, after world.Block, oldPower, newPower int) {
	if oldPower == 0 && newPower > 0 { tx.ScheduleBlockUpdate(pos, after, redstoneTicks(2)) }
}
func (c Crafter) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) { c.craft(pos, tx) }
func (c Crafter) Tick(_ int64, pos cube.Pos, tx *world.Tx) {
	if c.CraftingTicksRemaining <= 0 {
		return
	}
	c.CraftingTicksRemaining--
	if c.CraftingTicksRemaining == 0 {
		c.Crafting = false
		tx.SetBlock(pos, c, nil)
		return
	}
	tx.SetBlockEntity(pos, c)
}

func (c Crafter) craft(pos cube.Pos, tx *world.Tx) {
	input := make([]item.Stack, 9)
	for slot := range 9 { input[slot], _ = c.inventory.Item(slot) }
	var output item.Stack
	var match bool
	for _, dynamic := range recipe.DynamicRecipes() {
		if dynamic.Block() != "crafting_table" { continue }
		recipeInput := make([]recipe.Item, 9)
		for i := range input { recipeInput[i] = input[i] }
		if stacks, ok := dynamic.Match(recipeInput); ok && len(stacks) > 0 { output, match = stacks[0], true; break }
	}
	if !match {
		bestPriority := uint32(math.MaxUint32)
		for _, registered := range recipe.Recipes() {
			if registered.Block() != "crafting_table" || registered.Priority() >= bestPriority {
				continue
			}
			var candidate item.Stack
			var found bool
			switch registered.(type) {
			case recipe.Shaped:
				candidate, found = matchCrafterShaped(registered, input)
			case recipe.Shapeless, recipe.UserDataShapeless:
				candidate, found = matchCrafterShapeless(registered, input)
			}
			if found {
				output, match, bestPriority = candidate, true, registered.Priority()
			}
		}
	}
	if !match || output.Empty() {
		return
	}
	c.Crafting = true
	c.CraftingTicksRemaining = 6
	front := pos.Side(c.Facing)
	remaining := c.insertResult(tx, front, output)
	if !remaining.Empty() { c.eject(pos, tx, remaining) }
	for slot := range 9 {
		if rest := crafterRemainder(input[slot]); !rest.Empty() {
			remaining := c.insertResult(tx, front, rest)
			if !remaining.Empty() { c.eject(pos, tx, remaining) }
		}
		if !input[slot].Empty() { _ = c.inventory.SetItem(slot, input[slot].Grow(-1)) }
	}
	notifyComparatorUpdate(pos, tx)
	tx.SetBlock(pos, c, nil)
}

func matchCrafterShaped(r recipe.Recipe, input []item.Stack) (item.Stack, bool) {
	shaped, ok := r.(recipe.Shaped); if !ok { return item.Stack{}, false }
	shape, ingredients := shaped.Shape(), r.Input()
	w, h := shape[0], shape[1]
	if w < 1 || h < 1 || w > 3 || h > 3 || len(ingredients) != w*h { return item.Stack{}, false }
	for oy := range 4-h { for ox := range 4-w { for mirror := range 2 {
		good := true
		for y := range 3 { for x := range 3 {
			idx := y*3+x
			if x < ox || x >= ox+w || y < oy || y >= oy+h { if !input[idx].Empty() { good = false }; continue }
			rx := x-ox; if mirror == 1 { rx = w-1-rx }
			expected := ingredients[(y-oy)*w+rx]
			if !crafterIngredientMatches(input[idx], expected) { good = false }
		}}
		if good { out := r.Output(); if len(out)>0 { return out[0], true } }
	}}}
	return item.Stack{}, false
}

func matchCrafterShapeless(r recipe.Recipe, input []item.Stack) (item.Stack, bool) {
	expected := r.Input()
	if len(expected) > len(input) {
		return item.Stack{}, false
	}
	occupied := [9]bool{}
	for slot := range 9 {
		if input[slot].Empty() {
			continue
		}
		found := false
		for i, ingredient := range expected {
			if occupied[i] || !crafterIngredientMatches(input[slot], ingredient) {
				continue
			}
			if input[slot].Count() < ingredient.Count() {
				continue
			}
			occupied[i], found = true, true
			break
		}
		if !found {
			return item.Stack{}, false
		}
	}
	for _, used := range occupied[:len(expected)] {
		if !used {
			return item.Stack{}, false
		}
	}
	out := r.Output()
	if len(out) == 0 {
		return item.Stack{}, false
	}
	return out[0], true
}

func crafterIngredientMatches(stack item.Stack, expected recipe.Item) bool {
	switch ingredient := expected.(type) {
	case item.Stack:
		if stack.Empty() || ingredient.Empty() {
			return stack.Empty() && ingredient.Empty()
		}
		name, meta := stack.Item().EncodeItem(); wantName, wantMeta := ingredient.Item().EncodeItem()
		if name != wantName { return false }
		if _, variants := ingredient.Value("variants"); variants { return true }
		return meta == wantMeta
	case recipe.ItemTag:
		if stack.Empty() { return false }
		name, _ := stack.Item().EncodeItem(); return ingredient.Contains(name)
	default: return false
	}
}

func crafterRemainder(stack item.Stack) item.Stack {
	if stack.Empty() {
		return item.Stack{}
	}
	switch it := stack.Item().(type) {
	case item.HoneyBottle:
		return item.NewStack(item.GlassBottle{}, 1)
	case item.MushroomStew, item.RabbitStew, item.BeetrootSoup, item.SuspiciousStew:
		return item.NewStack(item.Bowl{}, 1)
	case item.Bucket:
		if !it.Empty() {
			return item.NewStack(item.Bucket{}, 1)
		}
	}
	return item.Stack{}
}

func (c Crafter) insertResult(tx *world.Tx, destPos cube.Pos, stack item.Stack) item.Stack {
	if target, ok := tx.Block(destPos).(Container); ok {
		inv := target.Inventory(tx, destPos)
		added, _ := inv.AddItem(stack)
		if added > 0 { notifyComparatorUpdate(destPos, tx); stack = stack.Grow(-added) }
	}
	return stack
}
func (c Crafter) eject(pos cube.Pos, tx *world.Tx, stack item.Stack) {
	dir := pos.Side(c.Facing).Vec3Centre().Sub(pos.Vec3Centre())
	opts := world.EntitySpawnOpts{Position: pos.Vec3Centre().Add(dir.Mul(.7)), Velocity: dir.Mul(.2)}
	tx.AddEntity(tx.World().EntityRegistry().Config().Item(opts, stack))
}

// InsertItem receives one item from a hopper into an enabled, least-filled matching crafter slot.
func (c Crafter) InsertItem(h Hopper, pos cube.Pos, tx *world.Tx) bool {
	for sourceSlot := range h.inventory.Size() {
		source, _ := h.inventory.Item(sourceSlot)
		if source.Empty() {
			continue
		}
		best, bestCount := -1, math.MaxInt
		for slot := range 9 {
			if c.DisabledSlots&(1<<slot) != 0 {
				continue
			}
			current, _ := c.inventory.Item(slot)
			if !current.Empty() && !current.Comparable(source) {
				continue
			}
			if current.Count() >= current.MaxCount() {
				continue
			}
			if current.Count() < bestCount {
				best, bestCount = slot, current.Count()
			}
		}
		if best < 0 {
			continue
		}
		current, _ := c.inventory.Item(best)
		if current.Empty() {
			current = source.Grow(1 - source.Count())
		} else {
			current = current.Grow(1)
		}
		_ = c.inventory.SetItem(best, current)
		_ = h.inventory.SetItem(sourceSlot, source.Grow(-1))
		notifyComparatorUpdate(pos, tx)
		return true
	}
	return false
}

func (c Crafter) ComparatorOutput(*world.Tx, cube.Pos) uint8 {
	count := 0
	for slot := range 9 {
		stack, _ := c.inventory.Item(slot)
		if !stack.Empty() || c.DisabledSlots&(1<<slot) != 0 {
			count++
		}
	}
	return uint8(count)
}

func (c Crafter) EncodeItem() (string, int16) { return "minecraft:crafter", 0 }
func (c Crafter) EncodeBlock() (string, map[string]any) {
	return "minecraft:crafter", map[string]any{"orientation": crafterOrientation(c.Facing, c.Top), "triggered_bit": boolByte(c.Triggered), "crafting": c.Crafting}
}
func (c Crafter) Hash() (uint64, uint64) {
	return hashCrafter, uint64(c.Facing) | uint64(c.Top)<<3 | uint64(boolByte(c.Triggered))<<6 | uint64(boolByte(c.Crafting))<<7
}
func crafterOrientation(front, top cube.Face) string {
	if front == cube.FaceDown || front == cube.FaceUp {
		switch top {
		case cube.FaceEast:
			if front == cube.FaceDown { return "down_east" }
			return "up_east"
		case cube.FaceNorth:
			if front == cube.FaceDown { return "down_north" }
			return "up_north"
		case cube.FaceSouth:
			if front == cube.FaceDown { return "down_south" }
			return "up_south"
		case cube.FaceWest:
			if front == cube.FaceDown { return "down_west" }
			return "up_west"
		}
	}
	if top == cube.FaceUp {
		switch front {
		case cube.FaceWest: return "west_up"
		case cube.FaceEast: return "east_up"
		case cube.FaceNorth: return "north_up"
		case cube.FaceSouth: return "south_up"
		}
	}
	return "north_up"
}
// EncodeNBT encodes the Crafter block actor as BDS does, its disabled slots
// as a bit mask. Triggering and crafting are block states; the remaining
// crafting ticks are kept so a crafter saved mid-craft finishes.
func (c Crafter) EncodeNBT() map[string]any {
	if c.inventory == nil {
		facing, top, triggered, crafting, disabled, name := c.Facing, c.Top, c.Triggered, c.Crafting, c.DisabledSlots, c.CustomName
		c = NewCrafter()
		c.Facing, c.Top, c.Triggered, c.Crafting, c.DisabledSlots, c.CustomName = facing, top, triggered, crafting, disabled, name
	}
	m := map[string]any{
		"id": "Crafter", "Items": nbtconv.InvToNBT(c.inventory), "disabled_slots": int16(c.DisabledSlots),
		"adamant:java": map[string]any{"crafting_ticks_remaining": c.CraftingTicksRemaining},
	}
	if c.CustomName != "" {
		m["CustomName"] = c.CustomName
	}
	return m
}

// DecodeNBT decodes a Crafter block actor, keeping the block states it was
// decoded with. Crafters saved before disabled slots became a bit mask list
// their slot numbers.
func (c Crafter) DecodeNBT(data map[string]any) any {
	facing, top, triggered, crafting := c.Facing, c.Top, c.Triggered, c.Crafting
	c = NewCrafter()
	c.Facing, c.Top, c.Triggered, c.Crafting = facing, top, triggered, crafting
	c.CraftingTicksRemaining = nbtconv.Int32(javaActorState(data), "crafting_ticks_remaining")
	c.CustomName = nbtconv.String(data, "CustomName")
	nbtconv.InvFromNBT(c.inventory, nbtconv.Slice(data, "Items"), data)
	if _, ok := data["disabled_slots"].(int16); ok {
		c.DisabledSlots = uint16(nbtconv.Int16(data, "disabled_slots")) & 0x1ff
		return c
	}
	for _, raw := range nbtconv.Slice(data, "disabled_slots") {
		if slot, ok := raw.(int32); ok && slot >= 0 && slot < 9 {
			c.DisabledSlots |= 1 << uint(slot)
		}
	}
	return c
}

func allCrafters() (states []world.Block) {
	for _, facing := range cube.Faces() {
		for _, top := range cube.Faces() {
			if facing == cube.FaceUp || facing == cube.FaceDown {
				if top == cube.FaceUp || top == cube.FaceDown { continue }
			} else if top != cube.FaceUp {
				continue
			}
			for triggered := range 2 {
				for crafting := range 2 {
					states = append(states, Crafter{Facing: facing, Top: top, Triggered: triggered == 1, Crafting: crafting == 1})
				}
			}
		}
	}
	return
}
