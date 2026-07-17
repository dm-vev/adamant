package block

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"sync"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Dropper is a redstone component that moves or ejects one random stored item when powered.
type Dropper struct {
	solid
	bass

	// Facing is the direction the dropper dispenses towards.
	Facing cube.Face
	// Triggered is whether the dropper has a dispense tick pending.
	Triggered bool
	// CustomName is displayed when the dropper is opened.
	CustomName string

	inventory *inventory.Inventory
	viewerMu  *sync.RWMutex
	viewers   map[ContainerViewer]struct{}
}

// NewDropper creates an initialised dropper with a nine-slot inventory.
func NewDropper() Dropper {
	m := new(sync.RWMutex)
	v := make(map[ContainerViewer]struct{}, 1)
	return Dropper{
		inventory: inventory.New(9, func(slot int, _, after item.Stack) {
			m.RLock()
			defer m.RUnlock()
			for viewer := range v {
				viewer.ViewSlotChange(slot, after)
			}
		}),
		viewerMu: m,
		viewers:  v,
	}
}

// Inventory returns the dropper's nine-slot inventory.
func (d Dropper) Inventory(*world.Tx, cube.Pos) *inventory.Inventory { return d.inventory }

// WithName returns the dropper with the custom name passed.
func (d Dropper) WithName(a ...any) world.Item {
	d.CustomName = strings.TrimSuffix(fmt.Sprintln(a...), "\n")
	return d
}

// AddViewer adds a viewer to the dropper.
func (d Dropper) AddViewer(v ContainerViewer, _ *world.Tx, _ cube.Pos) {
	d.viewerMu.Lock()
	defer d.viewerMu.Unlock()
	d.viewers[v] = struct{}{}
}

// RemoveViewer removes a viewer from the dropper.
func (d Dropper) RemoveViewer(v ContainerViewer, _ *world.Tx, _ cube.Pos) {
	d.viewerMu.Lock()
	defer d.viewerMu.Unlock()
	delete(d.viewers, v)
}

// Activate opens the dropper's inventory.
func (Dropper) Activate(pos cube.Pos, _ cube.Face, tx *world.Tx, u item.User, _ *item.UseContext) bool {
	if opener, ok := u.(ContainerOpener); ok {
		opener.OpenBlockContainer(pos, tx)
		return true
	}
	return false
}

// UseOnBlock places the dropper facing away from the clicked face.
func (d Dropper) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, d)
	if !used {
		return false
	}
	d = NewDropper()
	d.Facing = face.Opposite()
	place(tx, pos, d, user, ctx)
	return placed(ctx)
}

// BreakInfo returns the dropper's breaking properties and drops its contents when broken.
func (d Dropper) BreakInfo() BreakInfo {
	return newBreakInfo(3.5, pickaxeHarvestable, pickaxeEffective, oneOf(Dropper{})).withBlastResistance(17.5).withBreakHandler(func(pos cube.Pos, tx *world.Tx, _ item.User) {
		for _, stack := range d.Inventory(tx, pos).Clear() {
			dropItem(tx, stack, pos.Vec3())
		}
	})
}

// RedstoneUpdate schedules dispensing on a rising redstone edge and clears the state on a falling edge.
func (d Dropper) RedstoneUpdate(pos cube.Pos, tx *world.Tx) {
	powered := redstonePowered(pos, tx)
	if powered == d.Triggered {
		return
	}
	d.Triggered = powered
	tx.SetBlock(pos, d, &world.SetOpts{DisableBlockUpdates: true})
	if powered {
		tx.ScheduleBlockUpdate(pos, d, redstoneTicks(4))
	}
}

// ScheduledTick dispenses one item if the dropper is still triggered.
func (d Dropper) ScheduledTick(pos cube.Pos, tx *world.Tx, r *rand.Rand) {
	if !d.Triggered {
		return
	}
	d.Triggered = false
	tx.SetBlock(pos, d, &world.SetOpts{DisableBlockUpdates: true})

	slots := d.inventory.Slots()
	nonEmpty := make([]int, 0, len(slots))
	for slot, stack := range slots {
		if !stack.Empty() {
			nonEmpty = append(nonEmpty, slot)
		}
	}
	if len(nonEmpty) == 0 {
		return
	}
	slot := nonEmpty[r.IntN(len(nonEmpty))]
	stack := slots[slot]
	one := stack.Grow(1 - stack.Count())
	frontPos := pos.Side(d.Facing)
	if container, ok := tx.Block(frontPos).(Container); ok {
		if _, err := container.Inventory(tx, frontPos).AddItem(one); err == nil {
			_ = d.inventory.SetItem(slot, stack.Grow(-1))
			notifyComparatorUpdate(pos, tx)
			notifyComparatorUpdate(frontPos, tx)
			return
		}
	}
	direction := frontPos.Vec3Centre().Sub(pos.Vec3Centre())
	opts := world.EntitySpawnOpts{Position: pos.Vec3Centre().Add(direction.Mul(0.7)), Velocity: direction.Mul(0.2)}
	tx.AddEntity(tx.World().EntityRegistry().Config().Item(opts, one))
	_ = d.inventory.SetItem(slot, stack.Grow(-1))
	notifyComparatorUpdate(pos, tx)
}

// EncodeItem ...
func (Dropper) EncodeItem() (name string, meta int16) { return "minecraft:dropper", 0 }

// EncodeBlock ...
func (d Dropper) EncodeBlock() (string, map[string]any) {
	return "minecraft:dropper", map[string]any{"facing_direction": int32(d.Facing), "triggered_bit": boolByte(d.Triggered)}
}

// EncodeNBT ...
func (d Dropper) EncodeNBT() map[string]any {
	if d.inventory == nil {
		facing, triggered, customName := d.Facing, d.Triggered, d.CustomName
		d = NewDropper()
		d.Facing, d.Triggered, d.CustomName = facing, triggered, customName
	}
	m := map[string]any{"id": "Dropper", "Items": nbtconv.InvToNBT(d.inventory)}
	if d.CustomName != "" {
		m["CustomName"] = d.CustomName
	}
	return m
}

// DecodeNBT ...
func (d Dropper) DecodeNBT(data map[string]any) any {
	facing, triggered := d.Facing, d.Triggered
	d = NewDropper()
	d.Facing, d.Triggered = facing, triggered
	d.CustomName = nbtconv.String(data, "CustomName")
	nbtconv.InvFromNBT(d.inventory, nbtconv.Slice(data, "Items"))
	return d
}

// allDroppers returns every dropper block state.
func allDroppers() (droppers []world.Block) {
	for _, face := range cube.Faces() {
		droppers = append(droppers, Dropper{Facing: face}, Dropper{Facing: face, Triggered: true})
	}
	return
}
