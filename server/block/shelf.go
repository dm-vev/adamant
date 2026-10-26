package block

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// Shelf is a wooden shelf that holds three stacks on its front, swapped with
// the held item, or with the end of the hotbar while it is powered. It ports
// ShelfBlock and ShelfBlockEntity.
type Shelf struct {
	transparent
	sourceWaterDisplacer
	bass

	Material ShelfMaterial
	// Facing is the direction the front of the shelf faces.
	Facing  cube.Direction
	Powered bool
	// PoweredShelfType is the place of the shelf in a chain of powered
	// shelves, Java's side_chain: unconnected, right, center or left.
	PoweredShelfType uint8
	// Items are the stacks on the shelf, from left to right as seen from its
	// front.
	Items [3]item.Stack
}

const (
	shelfUnconnected uint8 = iota
	shelfRight
	shelfCenter
	shelfLeft
)

// shelfMaxChain is how many powered shelves at most form a chain.
const shelfMaxChain = 3

// Model ...
func (s Shelf) Model() world.BlockModel {
	return model.Shelf{Facing: s.Facing}
}

// UseOnBlock ...
func (s Shelf) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) (used bool) {
	pos, _, used = firstReplaceable(tx, pos, face, s)
	if !used {
		return
	}
	s.Facing = user.Rotation().Direction().Opposite()
	s.Powered = redstonePowered(pos, tx)
	s.PoweredShelfType = shelfUnconnected
	s.Items = [3]item.Stack{}

	place(tx, pos, s, user, ctx)
	if placed(ctx) {
		s.placed(pos, tx)
	}
	return placed(ctx)
}

// placed ports ShelfBlock.onPlace for a shelf newly set: a powered shelf
// joins the chains beside it, an unpowered one leaves them.
func (s Shelf) placed(pos cube.Pos, tx *world.Tx) {
	if s.Powered {
		s.joinChains(pos, tx)
		return
	}
	s.leaveChains(pos, tx)
}

// ActivateAt ports ShelfBlock.useItemOn: a click on one of the three slots
// of the front swaps the held stack with it, or, while the shelf is powered,
// swaps the end of the hotbar with the shelves of its chain.
func (s Shelf) ActivateAt(pos cube.Pos, face cube.Face, hit mgl64.Vec3, tx *world.Tx, u item.User, ctx *item.UseContext) bool {
	slot, ok := s.hitSlot(face, hit)
	if !ok {
		return false
	}
	held, _ := u.HeldItems()
	if s.Powered {
		holder, ok := u.(interface{ Inventory() *inventory.Inventory })
		if !ok {
			return false
		}
		if s.swapHotbar(pos, tx, holder.Inventory()) {
			s.playSound(pos, tx, "block.shelf.multi_swap")
		}
		ctx.HeldItemSet = true
		return true
	}
	removed := s.Items[slot]
	if removed.Empty() && held.Empty() {
		return false
	}
	s.Items[slot] = held
	tx.SetBlock(pos, s, nil)
	notifyComparatorUpdate(pos, tx)
	switch {
	case removed.Empty():
		s.playSound(pos, tx, "block.shelf.place_item")
	case held.Empty():
		s.playSound(pos, tx, "block.shelf.take_item")
	default:
		s.playSound(pos, tx, "block.shelf.single_swap")
	}
	if creative, ok := u.(interface{ GameMode() world.GameMode }); ok && creative.GameMode().CreativeInventory() && removed.Empty() {
		// Players with infinite materials keep what they place.
		return true
	}
	if removed.Empty() {
		ctx.SubtractFromCount(held.Count())
	} else {
		ctx.NewItem, ctx.ReplaceHeldItem = removed, true
	}
	return true
}

// hitSlot ports SelectableSlotContainer.getHitSlot for the one row of three
// columns of a shelf: only the front of the shelf has slots.
func (s Shelf) hitSlot(face cube.Face, hit mgl64.Vec3) (int, bool) {
	if face != s.Facing.Face() {
		return 0, false
	}
	var horizontal float64
	switch s.Facing {
	case cube.North:
		horizontal = 1 - hit[0]
	case cube.South:
		horizontal = hit[0]
	case cube.West:
		horizontal = hit[2]
	case cube.East:
		horizontal = 1 - hit[2]
	}
	return max(0, min(2, int(math.Floor(horizontal*16/(16.0/3))))), true
}

// swapHotbar ports ShelfBlock.swapHotbar: the stacks of the shelves of the
// chain, left to right, swap with the last slots of the hotbar.
func (s Shelf) swapHotbar(pos cube.Pos, tx *world.Tx, inv *inventory.Inventory) bool {
	chain := s.connected(pos, tx)
	swapped := false
	for i, shelfPos := range chain {
		shelf, ok := tx.Block(shelfPos).(Shelf)
		if !ok {
			continue
		}
		for slot := range shelf.Items {
			invSlot := 9 - (len(chain)-i)*len(shelf.Items) + slot
			if invSlot < 0 || invSlot >= inv.Size() {
				continue
			}
			placedItem, _ := inv.Item(invSlot)
			removed := shelf.Items[slot]
			shelf.Items[slot] = placedItem
			if !placedItem.Empty() || !removed.Empty() {
				_ = inv.SetItem(invSlot, removed)
				swapped = true
			}
		}
		tx.SetBlock(shelfPos, shelf, nil)
		notifyComparatorUpdate(shelfPos, tx)
	}
	return swapped
}

// connectable ports ShelfBlock.isConnectable: only powered shelves chain.
func (s Shelf) connectable() bool {
	return s.Powered
}

// shelfNeighbour returns the shelf steps blocks to the left of a shelf facing
// facing, as seen from its front, or to the right if steps is negative, if
// it is a connectable shelf facing the same way.
func shelfNeighbour(pos cube.Pos, facing cube.Direction, steps int, tx *world.Tx) (cube.Pos, Shelf, bool) {
	dir := facing.RotateRight()
	if steps < 0 {
		dir, steps = facing.RotateLeft(), -steps
	}
	at := pos
	for range steps {
		at = at.Side(dir.Face())
	}
	shelf, ok := tx.Block(at).(Shelf)
	return at, shelf, ok && shelf.connectable() && shelf.Facing == facing
}

// shelfConnectsTowards ports SideChainPart.isConnectionTowards.
func shelfConnectsTowards(part, end uint8) bool {
	return part == shelfCenter || part == end
}

// connected ports SideChainPartBlock.getAllBlocksConnectedTo: the shelves
// chained with the one at pos, from left to right.
func (s Shelf) connected(pos cube.Pos, tx *world.Tx) []cube.Pos {
	if !s.connectable() {
		return nil
	}
	chain := []cube.Pos{pos}
	for steps := 1; steps < shelfMaxChain; steps++ {
		at, shelf, ok := shelfNeighbour(pos, s.Facing, steps, tx)
		if ok && shelfConnectsTowards(shelf.PoweredShelfType, shelfLeft) {
			chain = append([]cube.Pos{at}, chain...)
		}
		if !ok || shelf.PoweredShelfType != shelfCenter {
			break
		}
	}
	for steps := 1; steps < shelfMaxChain; steps++ {
		at, shelf, ok := shelfNeighbour(pos, s.Facing, -steps, tx)
		if ok && shelfConnectsTowards(shelf.PoweredShelfType, shelfRight) {
			chain = append(chain, at)
		}
		if !ok || shelf.PoweredShelfType != shelfCenter {
			break
		}
	}
	return chain
}

// setShelfPart ports SideChainPartBlock.setPart.
func setShelfPart(pos cube.Pos, part uint8, tx *world.Tx) {
	if shelf, ok := tx.Block(pos).(Shelf); ok && shelf.PoweredShelfType != part {
		shelf.PoweredShelfType = part
		tx.SetBlock(pos, shelf, nil)
	}
}

// shelfConnectedRight, shelfConnectedLeft, shelfDisconnectedRight and
// shelfDisconnectedLeft port the transitions of SideChainPart.
func shelfConnectedRight(part uint8) uint8 {
	if part == shelfUnconnected || part == shelfLeft {
		return shelfLeft
	}
	return shelfCenter
}

func shelfConnectedLeft(part uint8) uint8 {
	if part == shelfUnconnected || part == shelfRight {
		return shelfRight
	}
	return shelfCenter
}

func shelfDisconnectedRight(part uint8) uint8 {
	if part == shelfUnconnected || part == shelfLeft {
		return shelfUnconnected
	}
	return shelfRight
}

func shelfDisconnectedLeft(part uint8) uint8 {
	if part == shelfUnconnected || part == shelfRight {
		return shelfUnconnected
	}
	return shelfLeft
}

// joinChains ports SideChainPartBlock.updateSelfAndNeighborsOnPoweringUp: a
// shelf newly powered joins the chain to its left, then that to its right,
// as long as the chain stays within three shelves.
func (s Shelf) joinChains(pos cube.Pos, tx *world.Tx) {
	if !s.connectable() || s.PoweredShelfType != shelfUnconnected {
		return
	}
	leftPos, left, leftOK := shelfNeighbour(pos, s.Facing, 1, tx)
	rightPos, right, rightOK := shelfNeighbour(pos, s.Facing, -1, tx)
	leftChain, rightChain := 0, 0
	if leftOK {
		leftChain = len(left.connected(leftPos, tx))
	}
	if rightOK {
		rightChain = len(right.connected(rightPos, tx))
	}
	part, length := shelfUnconnected, 1
	if leftChain > 0 && length+leftChain <= shelfMaxChain {
		part = shelfConnectedLeft(part)
		setShelfPart(leftPos, shelfConnectedRight(left.PoweredShelfType), tx)
		length += leftChain
	}
	if rightChain > 0 && length+rightChain <= shelfMaxChain {
		part = shelfConnectedRight(part)
		setShelfPart(rightPos, shelfConnectedLeft(right.PoweredShelfType), tx)
	}
	setShelfPart(pos, part, tx)
}

// leaveChains ports SideChainPartBlock.updateNeighborsAfterPoweringDown: the
// shelves beside one no longer powered let go of it.
func (s Shelf) leaveChains(pos cube.Pos, tx *world.Tx) {
	if leftPos, left, ok := shelfNeighbour(pos, s.Facing, 1, tx); ok {
		setShelfPart(leftPos, shelfDisconnectedRight(left.PoweredShelfType), tx)
	}
	if rightPos, right, ok := shelfNeighbour(pos, s.Facing, -1, tx); ok {
		setShelfPart(rightPos, shelfDisconnectedLeft(right.PoweredShelfType), tx)
	}
}

// NeighbourUpdateTick ports ShelfBlock.neighborChanged: the shelf follows the
// redstone signal it receives, and leaves its chain when the signal goes.
func (s Shelf) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	powered := redstonePowered(pos, tx)
	if powered == s.Powered {
		return
	}
	s.Powered = powered
	if !powered {
		s.PoweredShelfType = shelfUnconnected
	}
	tx.SetBlock(pos, s, nil)
	if powered {
		s.playSound(pos, tx, "block.shelf.activate")
	} else {
		s.playSound(pos, tx, "block.shelf.deactivate")
	}
	s.placed(pos, tx)
}

func (Shelf) playSound(pos cube.Pos, tx *world.Tx, name string) {
	tx.PlaySound(pos.Vec3Centre(), sound.Custom{Name: name, Volume: 1, Pitch: 1})
}

// ComparatorOutputTowards ports ShelfBlock.getAnalogOutputSignal: only a
// comparator behind the shelf reads it, one bit for each occupied slot.
func (s Shelf) ComparatorOutputTowards(_ *world.Tx, _ cube.Pos, towards cube.Face) uint8 {
	if towards != s.Facing.Opposite().Face() {
		return 0
	}
	var out uint8
	for slot, stack := range s.Items {
		if !stack.Empty() {
			out |= 1 << slot
		}
	}
	return out
}

// shelfMaxStack is the most items a shelf slot takes from a hopper, as a
// ListBackedContainer.
func shelfMaxStack(stack item.Stack) int {
	return min(stack.MaxCount(), 99)
}

// InsertItem ports a hopper pushing into the shelf as a container: one item
// goes into the first slot that is empty or holds more of it with room left.
func (s Shelf) InsertItem(h Hopper, pos cube.Pos, tx *world.Tx) bool {
	for sourceSlot, stack := range h.inventory.Slots() {
		if stack.Empty() {
			continue
		}
		for slot, has := range s.Items {
			switch {
			case has.Empty():
				s.Items[slot] = stack.Grow(1 - stack.Count())
			case has.Comparable(stack) && has.Count() < shelfMaxStack(has):
				s.Items[slot] = has.Grow(1)
			default:
				continue
			}
			_ = h.inventory.SetItem(sourceSlot, stack.Grow(-1))
			tx.SetBlock(pos, s, nil)
			notifyComparatorUpdate(pos, tx)
			return true
		}
		return false
	}
	return false
}

// ExtractItem ports a hopper pulling one item from the first occupied slot
// of the shelf.
func (s Shelf) ExtractItem(h Hopper, pos cube.Pos, tx *world.Tx) bool {
	for slot, stack := range s.Items {
		if stack.Empty() {
			continue
		}
		if _, err := h.inventory.AddItem(stack.Grow(1 - stack.Count())); err != nil {
			continue
		}
		s.Items[slot] = stack.Grow(-1)
		tx.SetBlock(pos, s, nil)
		notifyComparatorUpdate(pos, tx)
		return true
	}
	return false
}

// BreakInfo ...
func (s Shelf) BreakInfo() BreakInfo {
	return newBreakInfo(2, alwaysHarvestable, axeEffective, oneOf(Shelf{Material: s.Material})).withBlastResistance(3).withBreakHandler(func(pos cube.Pos, tx *world.Tx, _ item.User) {
		for _, stack := range s.Items {
			if !stack.Empty() {
				dropItem(tx, stack, pos.Vec3Centre())
			}
		}
		s.leaveChains(pos, tx)
	})
}

// FlammabilityInfo ...
func (s Shelf) FlammabilityInfo() FlammabilityInfo {
	if !s.Material.Flammable() {
		return newFlammabilityInfo(0, 0, false)
	}
	return newFlammabilityInfo(30, 20, true)
}

// EncodeItem ...
func (s Shelf) EncodeItem() (string, int16) {
	return "minecraft:" + s.Material.String() + "_shelf", 0
}

// EncodeBlock ...
func (s Shelf) EncodeBlock() (string, map[string]any) {
	return "minecraft:" + s.Material.String() + "_shelf", map[string]any{
		"minecraft:cardinal_direction": s.Facing.String(),
		"powered_bit":                  boolByte(s.Powered),
		"powered_shelf_type":           int32(s.PoweredShelfType),
	}
}

// EncodeNBT stores the stacks of the shelf in its Shelf block actor.
func (s Shelf) EncodeNBT() map[string]any {
	items := make([]map[string]any, 0, len(s.Items))
	for slot, stack := range s.Items {
		if stack.Empty() {
			continue
		}
		data := item.WriteNBT(stack, true)
		data["Slot"] = byte(slot)
		items = append(items, data)
	}
	return map[string]any{"id": "Shelf", "Items": items}
}

// DecodeNBT ...
func (s Shelf) DecodeNBT(data map[string]any) any {
	s.Items = [3]item.Stack{}
	registry := world.BlockRegistryFromNBT(data)
	for _, entry := range nbtconv.Slice(data, "Items") {
		m, _ := entry.(map[string]any)
		slot := int(nbtconv.Uint8(m, "Slot"))
		if stack := item.ReadNBT(m, nil, registry); !stack.Empty() && slot < len(s.Items) {
			s.Items[slot] = stack
		}
	}
	return s
}

// allShelves ...
func allShelves() (shelves []world.Block) {
	for _, material := range ShelfMaterials() {
		for _, facing := range cube.Directions() {
			for poweredType := uint8(0); poweredType < 4; poweredType++ {
				shelves = append(shelves,
					Shelf{Material: material, Facing: facing, PoweredShelfType: poweredType},
					Shelf{Material: material, Facing: facing, Powered: true, PoweredShelfType: poweredType},
				)
			}
		}
	}
	return
}

// ShelfMaterial represents a shelf material.
type ShelfMaterial uint8

const (
	shelfOak ShelfMaterial = iota
	shelfSpruce
	shelfBirch
	shelfJungle
	shelfAcacia
	shelfDarkOak
	shelfCrimson
	shelfWarped
	shelfMangrove
	shelfCherry
	shelfPaleOak
	shelfBamboo
	shelfPoplar
)

// Uint8 returns the material as a uint8.
func (m ShelfMaterial) Uint8() uint8 {
	return uint8(m)
}

// String ...
func (m ShelfMaterial) String() string {
	switch m {
	case shelfOak:
		return "oak"
	case shelfSpruce:
		return "spruce"
	case shelfBirch:
		return "birch"
	case shelfJungle:
		return "jungle"
	case shelfAcacia:
		return "acacia"
	case shelfDarkOak:
		return "dark_oak"
	case shelfCrimson:
		return "crimson"
	case shelfWarped:
		return "warped"
	case shelfMangrove:
		return "mangrove"
	case shelfCherry:
		return "cherry"
	case shelfPaleOak:
		return "pale_oak"
	case shelfBamboo:
		return "bamboo"
	case shelfPoplar:
		return "poplar"
	}
	panic("unknown shelf material")
}

// Flammable reports if the shelf material burns.
func (m ShelfMaterial) Flammable() bool {
	return m != shelfCrimson && m != shelfWarped
}

// ShelfMaterials returns all shelf materials.
func ShelfMaterials() []ShelfMaterial {
	return []ShelfMaterial{
		shelfOak, shelfSpruce, shelfBirch, shelfJungle, shelfAcacia, shelfDarkOak,
		shelfCrimson, shelfWarped, shelfMangrove, shelfCherry, shelfPaleOak, shelfBamboo, shelfPoplar,
	}
}
