package block

import (
	"fmt"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"strings"
	"sync"
)

// Hopper is a low-capacity storage block that can be used to collect item entities directly above it, as well as to
// transfer items into and out of other containers.
type Hopper struct {
	transparent
	sourceWaterDisplacer

	// Facing is the direction the hopper is facing.
	Facing cube.Face
	// Powered is whether the hopper is powered or not. If the hopper is powered it will be locked and will stop
	// moving items into or out of itself.
	Powered bool
	// CustomName is the custom name of the hopper. This name is displayed when the hopper is opened, and may include
	// colour codes.
	CustomName string

	// TransferCooldown counts the native two-game-tick transfer steps. A
	// successful transfer sets four steps, giving an eight-game-tick interval.
	TransferCooldown int64
	// CollectCooldown is the duration in ticks until the hopper can collect items again.
	CollectCooldown int64

	inventory *inventory.Inventory
	viewerMu  *sync.RWMutex
	viewers   map[ContainerViewer]struct{}
	runtime   *hopperRuntime
}

// hopperRuntime belongs to the actor, so copies of its block data share the
// reentrancy guard. This clock is transient and is not part of saved NBT.
type hopperRuntime struct{ lastTransferTick int64 }

// NewHopper creates a new initialised hopper. The inventory is properly initialised.
func NewHopper() Hopper {
	m := new(sync.RWMutex)
	v := make(map[ContainerViewer]struct{}, 1)
	return Hopper{
		inventory: inventory.New(5, func(slot int, _, item item.Stack) {
			m.RLock()
			defer m.RUnlock()
			for viewer := range v {
				viewer.ViewSlotChange(slot, item)
			}
		}),
		viewerMu: m,
		viewers:  v,
		runtime:  &hopperRuntime{lastTransferTick: -1},
	}
}

func (Hopper) ContainerSize() int { return 5 }

// Model ...
func (Hopper) Model() world.BlockModel {
	return model.Hopper{}
}

// SideClosed ...
func (Hopper) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool {
	return false
}

// CanRedstoneWireStepDown ...
func (Hopper) CanRedstoneWireStepDown(cube.Pos, cube.Pos, *world.Tx) bool {
	return false
}

// BreakInfo ...
func (h Hopper) BreakInfo() BreakInfo {
	return newBreakInfo(3, pickaxeHarvestable, pickaxeEffective, oneOf(Hopper{})).withBlastResistance(4.8).withBreakHandler(func(pos cube.Pos, tx *world.Tx, u item.User) {
		for _, i := range h.Inventory(tx, pos).Clear() {
			dropItem(tx, i, pos.Vec3())
		}
	})
}

func (h Hopper) RedstonePowerUpdate(_ cube.Pos, _ *world.Tx, power int) (world.Block, bool) {
	powered := power > 0
	if powered == h.Powered {
		return h, false
	}
	h.Powered = powered
	return h, true
}

// Inventory returns the inventory of the hopper.
func (h Hopper) Inventory(*world.Tx, cube.Pos) *inventory.Inventory {
	return h.inventory
}

// WithName returns the hopper after applying a specific name to the block.
func (h Hopper) WithName(a ...any) world.Item {
	h.CustomName = strings.TrimSuffix(fmt.Sprintln(a...), "\n")
	return h
}

// AddViewer adds a viewer to the hopper, so that it is updated whenever the inventory of the hopper is changed.
func (h Hopper) AddViewer(v ContainerViewer, _ *world.Tx, _ cube.Pos) {
	h.viewerMu.Lock()
	defer h.viewerMu.Unlock()
	h.viewers[v] = struct{}{}
}

// RemoveViewer removes a viewer from the hopper, so that slot updates in the inventory are no longer sent to it.
func (h Hopper) RemoveViewer(v ContainerViewer, _ *world.Tx, _ cube.Pos) {
	h.viewerMu.Lock()
	defer h.viewerMu.Unlock()
	delete(h.viewers, v)
}

// Activate ...
func (Hopper) Activate(pos cube.Pos, _ cube.Face, tx *world.Tx, u item.User, _ *item.UseContext) bool {
	if opener, ok := u.(ContainerOpener); ok {
		opener.OpenBlockContainer(pos, tx)
		return true
	}
	return false
}

// UseOnBlock ...
func (h Hopper) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, h)
	if !used {
		return false
	}

	//noinspection GoAssignmentToReceiver
	h = NewHopper()
	h.Facing = cube.FaceDown
	if h.Facing != face {
		h.Facing = face.Opposite()
	}

	place(tx, pos, h, user, ctx)
	return placed(ctx)
}

// Tick ...
func (h Hopper) Tick(tick int64, pos cube.Pos, tx *world.Tx) {
	if h.CollectCooldown > 0 {
		h.CollectCooldown--
		tx.SetBlockEntity(pos, h)
	}
	// HopperBlockActor::_tick (client 0x4acb450) ignores an unchanged
	// Level::getCurrentTick value. Native heap traces show that clock advancing
	// on even game ticks, including while an idle hopper waits for an item.
	// Keep the separate item-actor collection timer in its existing GT units.
	if tick&1 != 0 {
		return
	}
	h.tickTransfer(tick, pos, tx, 50)
}

func (h Hopper) tickTransfer(tick int64, pos cube.Pos, tx *world.Tx, remaining int) {
	if h.runtime == nil {
		h.runtime = &hopperRuntime{lastTransferTick: -1}
		tx.SetBlockEntity(pos, h)
	}
	if h.runtime.lastTransferTick >= tick {
		return
	}
	h.runtime.lastTransferTick = tick
	before := h.TransferCooldown
	if before > 1 {
		h.TransferCooldown--
	} else {
		h.TransferCooldown = 0
	}
	if h.TransferCooldown != before {
		tx.SetBlockEntity(pos, h)
	}
	if h.TransferCooldown > 0 {
		return
	}
	if remaining > 0 {
		h.tickNeighbours(tick, pos, tx, remaining-1)
	}
	// A neighbour can transfer into this actor while it is already on the
	// recursion stack. Preserve that live data; native tests the cooldown only
	// on entry, so it still completes the transfer attempt below.
	live, ok := tx.Block(pos).(Hopper)
	if !ok || live.inventory != h.inventory {
		return
	}
	h = live

	if !h.Powered {
		inserted := h.insertItem(pos, tx)
		extracted := h.extractItem(pos, tx)
		if inserted || extracted {
			h.TransferCooldown = 4
			tx.SetBlock(pos, h, nil)
			return
		}
	}
}

func (h Hopper) tickNeighbours(tick int64, pos cube.Pos, tx *world.Tx, remaining int) {
	// The native candidates are below this hopper, below its destination, and
	// below the paired half when the destination is a double chest. Null
	// candidates take part in the same two-draw forward shuffle.
	type neighbour struct {
		pos    cube.Pos
		hopper Hopper
		valid  bool
	}
	var neighbours [3]neighbour
	lookup := func(i int, p cube.Pos) {
		if b, loaded := tx.BlockLoaded(p); loaded {
			if hopper, ok := b.(Hopper); ok {
				neighbours[i] = neighbour{pos: p, hopper: hopper, valid: true}
			}
		}
	}
	destination := pos.Side(h.Facing)
	lookup(0, pos.Side(cube.FaceDown))
	lookup(1, destination.Side(cube.FaceDown))
	if b, loaded := tx.BlockLoaded(destination); loaded {
		if chest, ok := b.(chestBlock); ok {
			if paired, ok := chest.chestData().PairedWith(destination); ok {
				lookup(2, paired.Side(cube.FaceDown))
			}
		}
	}
	if tx.RandomUint32()&1 == 0 {
		neighbours[0], neighbours[1] = neighbours[1], neighbours[0]
	}
	index := tx.RandomUint32() % 3
	neighbours[index], neighbours[2] = neighbours[2], neighbours[index]
	for _, neighbour := range neighbours {
		if !neighbour.valid {
			continue
		}
		if b, loaded := tx.BlockLoaded(neighbour.pos); loaded {
			if live, ok := b.(Hopper); ok && live.inventory == neighbour.hopper.inventory {
				live.tickTransfer(tick, neighbour.pos, tx, remaining)
			}
		}
	}
}

// HopperInsertable represents a block that can have its contents inserted into by a hopper.
type HopperInsertable interface {
	// InsertItem handles the insert logic for that block.
	InsertItem(h Hopper, pos cube.Pos, tx *world.Tx) bool
}

// insertItem inserts an item into a block that can receive contents from the hopper.
func (h Hopper) insertItem(pos cube.Pos, tx *world.Tx) bool {
	destPos := pos.Side(h.Facing)
	dest, loaded := tx.BlockLoaded(destPos)
	if !loaded {
		return false
	}

	if e, ok := dest.(HopperInsertable); ok {
		return e.InsertItem(h, destPos, tx)
	}

	if container, ok := dest.(Container); ok {
		inv := container.Inventory(tx, destPos)
		for sourceSlot := range h.inventory.Size() {
			sourceStack, _ := h.inventory.Item(sourceSlot)
			if sourceStack.Empty() {
				continue
			}

			_, err := inv.AddItem(sourceStack.Grow(-sourceStack.Count() + 1))
			if err != nil {
				// The destination is full.
				return false
			}

			_ = h.inventory.SetItem(sourceSlot, sourceStack.Grow(-1))
			notifyComparatorUpdate(destPos, tx)

			if hopper, ok := dest.(Hopper); ok {
				items := inv.Items()
				// Hopper::_tryMoveInItem only starts the receiver's timer when
				// it has just become nonempty. An actor still waiting for this
				// tick's pass gets an extra step to compensate its decrement.
				if len(items) == 1 && items[0].Count() == 1 {
					hopper.TransferCooldown = 4
					if hopper.runtime == nil || hopper.runtime.lastTransferTick < tx.CurrentTick()&^1 {
						hopper.TransferCooldown++
					}
					tx.SetBlockEntity(destPos, hopper)
				}
			}

			return true
		}
	}
	return false
}

// HopperExtractable represents a block that can have its contents extracted by a hopper.
type HopperExtractable interface {
	// ExtractItem handles the extract logic for that block.
	ExtractItem(h Hopper, pos cube.Pos, tx *world.Tx) bool
}

// extractItem extracts an item from a container into the hopper.
func (h Hopper) extractItem(pos cube.Pos, tx *world.Tx) bool {
	originPos := pos.Side(cube.FaceUp)
	origin, loaded := tx.BlockLoaded(originPos)
	if !loaded {
		return false
	}

	if e, ok := origin.(HopperExtractable); ok {
		return e.ExtractItem(h, originPos, tx)
	}

	if containerOrigin, ok := origin.(Container); ok {
		inv := containerOrigin.Inventory(tx, originPos)
		for slot := range inv.Size() {
			stack, _ := inv.Item(slot)
			if stack.Empty() {
				continue
			}

			_, err := h.inventory.AddItem(stack.Grow(-stack.Count() + 1))
			if err != nil {
				// The hopper is full.
				continue
			}

			_ = inv.SetItem(slot, stack.Grow(-1))
			notifyComparatorUpdate(originPos, tx)

			return true
		}
	}
	return false
}

// EncodeItem ...
func (Hopper) EncodeItem() (name string, meta int16) {
	return "minecraft:hopper", 0
}

// EncodeBlock ...
func (h Hopper) EncodeBlock() (string, map[string]any) {
	return "minecraft:hopper", map[string]any{
		"facing_direction": int32(h.Facing),
		"toggle_bit":       h.Powered,
	}
}

// EncodeNBT ...
func (h Hopper) EncodeNBT() map[string]any {
	if h.inventory == nil {
		facing, powered, customName := h.Facing, h.Powered, h.CustomName
		//noinspection GoAssignmentToReceiver
		h = NewHopper()
		h.Facing, h.Powered, h.CustomName = facing, powered, customName
	}
	m := map[string]any{
		"Items":            nbtconv.InvToNBT(h.inventory),
		"TransferCooldown": int32(h.TransferCooldown),
		"id":               "Hopper",
	}
	if h.CustomName != "" {
		m["CustomName"] = h.CustomName
	}
	return m
}

// DecodeNBT ...
func (h Hopper) DecodeNBT(data map[string]any) any {
	facing, powered := h.Facing, h.Powered
	//noinspection GoAssignmentToReceiver
	h = NewHopper()
	h.Facing = facing
	h.Powered = powered
	h.CustomName = nbtconv.String(data, "CustomName")
	h.TransferCooldown = int64(nbtconv.Int32(data, "TransferCooldown"))
	nbtconv.InvFromNBT(h.inventory, nbtconv.Slice(data, "Items"), data)
	return h
}

// allHoppers ...
func allHoppers() (hoppers []world.Block) {
	for _, f := range cube.Faces() {
		hoppers = append(hoppers, Hopper{Facing: f})
		hoppers = append(hoppers, Hopper{Facing: f, Powered: true})
	}
	return hoppers
}
