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
	"github.com/df-mc/dragonfly/server/loot"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Dispenser is a redstone component that uses or ejects one random stored item when powered.
type Dispenser struct {
	solid
	bass

	Facing     cube.Face
	Triggered  bool
	CustomName string

	inventory *inventory.Inventory
	viewerMu  *sync.RWMutex
	viewers   map[ContainerViewer]struct{}
	// lootTable is the loot table the dispenser is filled from when it is
	// first used, such as the arrows of a jungle temple trap.
	lootTable string
	lootSeed  int64
}

// NewDispenser creates an initialised dispenser with a nine-slot inventory.
func NewDispenser() Dispenser {
	m := new(sync.RWMutex)
	v := make(map[ContainerViewer]struct{}, 1)
	return Dispenser{
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

// ContainerSize returns the nine slots exposed by the native dispenser.
func (Dispenser) ContainerSize() int { return 9 }

func (d Dispenser) Inventory(tx *world.Tx, pos cube.Pos) *inventory.Inventory {
	return d.generateLoot(tx, pos).inventory
}

// generateLoot fills the dispenser from its loot table once, as Java's
// RandomizableContainer.unpackLootTable does when the container is first
// used.
func (d Dispenser) generateLoot(tx *world.Tx, pos cube.Pos) Dispenser {
	if d.lootTable == "" {
		return d
	}
	if err := fillContainerLoot(tx, pos, d.inventory, d.lootTable, d.lootSeed, "minecraft:dispenser"); err != nil {
		tx.World().Log().Error("dispenser loot table", "table", d.lootTable, "pos", pos, "error", err)
	}
	d.lootTable, d.lootSeed = "", 0
	if _, ok := tx.Block(pos).(Dispenser); ok {
		tx.SetBlockEntity(pos, d)
	}
	return d
}

func (d Dispenser) WithName(a ...any) world.Item {
	d.CustomName = strings.TrimSuffix(fmt.Sprintln(a...), "\n")
	return d
}

func (d Dispenser) AddViewer(v ContainerViewer, _ *world.Tx, _ cube.Pos) {
	d.viewerMu.Lock()
	defer d.viewerMu.Unlock()
	d.viewers[v] = struct{}{}
}

func (d Dispenser) RemoveViewer(v ContainerViewer, _ *world.Tx, _ cube.Pos) {
	d.viewerMu.Lock()
	defer d.viewerMu.Unlock()
	delete(d.viewers, v)
}

func (Dispenser) Activate(pos cube.Pos, _ cube.Face, tx *world.Tx, u item.User, _ *item.UseContext) bool {
	if opener, ok := u.(ContainerOpener); ok {
		opener.OpenBlockContainer(pos, tx)
		return true
	}
	return false
}

func (d Dispenser) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, d)
	if !used {
		return false
	}
	d = NewDispenser()
	d.Facing = placerFacing(user, pos)
	place(tx, pos, d, user, ctx)
	return placed(ctx)
}

func (d Dispenser) BreakInfo() BreakInfo {
	return newBreakInfo(3.5, pickaxeHarvestable, pickaxeEffective, oneOf(Dispenser{})).withBlastResistance(3.5).withBreakHandler(func(pos cube.Pos, tx *world.Tx, _ item.User) {
		for _, stack := range d.Inventory(tx, pos).Clear() {
			dropItem(tx, stack, pos.Vec3())
		}
	})
}

func (d Dispenser) RedstonePowerUpdate(_ cube.Pos, _ *world.Tx, power int) (world.Block, bool) {
	powered := power > 0
	if powered == d.Triggered {
		return d, false
	}
	d.Triggered = powered
	return d, true

}

func (d Dispenser) RedstonePowerPostUpdate(pos cube.Pos, tx *world.Tx, _, after world.Block, oldPower, newPower int) {
	if oldPower == 0 && newPower > 0 {
		tx.ScheduleBlockUpdate(pos, after, gameTicks(4))
	}
}

// ScheduledTick dispenses one item of a random slot, as Java's
// DispenserBlock.dispenseFrom, by the behaviour of its item.
func (d Dispenser) ScheduledTick(pos cube.Pos, tx *world.Tx, r *rand.Rand) {
	// Java schedules the tick on the rising edge and does not cancel it when
	// power falls again before the scheduled tick runs.
	d = d.generateLoot(tx, pos)
	slot := randomContainerSlot(d.inventory, tx.RandomUint32)
	if slot < 0 {
		playContainerSound(pos.Vec3(), tx, "block.click.fail")
		return
	}
	stack, _ := d.inventory.Item(slot)
	src := dispenseSource{d: d, pos: pos, tx: tx, stack: stack}
	res := dispenseBehaviourOf(stack.Item())(src)
	src.store(slot, res)
	src.play(res.effects)
}

func (Dispenser) EncodeItem() (string, int16) { return "minecraft:dispenser", 0 }

func (d Dispenser) EncodeBlock() (string, map[string]any) {
	return "minecraft:dispenser", map[string]any{"facing_direction": int32(d.Facing), "triggered_bit": boolByte(d.Triggered)}
}

func (d Dispenser) EncodeNBT() map[string]any {
	if d.inventory == nil {
		facing, triggered, customName, lootTable, lootSeed := d.Facing, d.Triggered, d.CustomName, d.lootTable, d.lootSeed
		d = NewDispenser()
		d.Facing, d.Triggered, d.CustomName, d.lootTable, d.lootSeed = facing, triggered, customName, lootTable, lootSeed
	}
	m := map[string]any{"id": "Dispenser", "Items": nbtconv.InvToNBT(d.inventory)}
	if d.CustomName != "" {
		m["CustomName"] = d.CustomName
	}
	if d.lootTable != "" {
		m["LootTable"], m["LootTableSeed"] = loot.BedrockTablePath(d.lootTable), d.lootSeed
	}
	return m
}

func (d Dispenser) DecodeNBT(data map[string]any) any {
	facing, triggered := d.Facing, d.Triggered
	d = NewDispenser()
	d.Facing, d.Triggered = facing, triggered
	d.CustomName = nbtconv.String(data, "CustomName")
	d.lootTable = loot.JavaTableKey(nbtconv.String(data, "LootTable"))
	d.lootSeed, _ = nbtconv.Integer(data, "LootTableSeed")
	nbtconv.InvFromNBT(d.inventory, nbtconv.Slice(data, "Items"), data)
	return d
}

func allDispensers() (dispensers []world.Block) {
	for _, face := range cube.Faces() {
		dispensers = append(dispensers, Dispenser{Facing: face}, Dispenser{Facing: face, Triggered: true})
	}
	return
}

// ContainerSize returns the nine slots of a dispenser.
