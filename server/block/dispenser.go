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

func (d Dispenser) Inventory(*world.Tx, cube.Pos) *inventory.Inventory { return d.inventory }

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
	d.Facing = face.Opposite()
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
		tx.ScheduleBlockUpdate(pos, after, redstoneTicks(2))
	}
}

func (d Dispenser) ScheduledTick(pos cube.Pos, tx *world.Tx, r *rand.Rand) {
	if !d.Triggered {
		return
	}

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
	direction := pos.Side(d.Facing).Vec3Centre().Sub(pos.Vec3Centre())
	opts := world.EntitySpawnOpts{Position: pos.Vec3Centre().Add(direction.Mul(0.7)), Velocity: direction.Mul(1.1)}

	switch held := stack.Item().(type) {
	case item.Egg:
		tx.AddEntity(tx.World().EntityRegistry().Config().Egg(opts, nil))
	case item.Snowball:
		tx.AddEntity(tx.World().EntityRegistry().Config().Snowball(opts, nil))
	case item.Boat:
		front := pos.Side(d.Facing)
		frontLiquid, frontWater := tx.Liquid(front)
		belowLiquid, waterBelow := tx.Liquid(front.Side(cube.FaceDown))
		frontWater = frontWater && frontLiquid.LiquidType() == "water"
		waterBelow = waterBelow && belowLiquid.LiquidType() == "water"
		_, frontAir := tx.Block(front).(Air)
		if frontWater || frontAir && waterBelow {
			direction := front.Vec3Centre().Sub(pos.Vec3Centre())
			opts.Position = front.Vec3Middle().Add(mgl64.Vec3{0, 0.375, 0})
			opts.Rotation = cube.Rotation{math.Atan2(direction[2], direction[0])*180/math.Pi + 90, 0}
			opts.Velocity = direction.Mul(0.1)
			tx.AddEntity(tx.World().EntityRegistry().Config().Boat(opts, held.Variant.Int()))
		} else {
			d.eject(tx, opts, stack.Grow(1-stack.Count()))
		}
	case item.ChestBoat:
		front := pos.Side(d.Facing)
		frontLiquid, frontWater := tx.Liquid(front)
		belowLiquid, waterBelow := tx.Liquid(front.Side(cube.FaceDown))
		frontWater = frontWater && frontLiquid.LiquidType() == "water"
		waterBelow = waterBelow && belowLiquid.LiquidType() == "water"
		_, frontAir := tx.Block(front).(Air)
		if frontWater || frontAir && waterBelow {
			direction := front.Vec3Centre().Sub(pos.Vec3Centre())
			opts.Position = front.Vec3Middle().Add(mgl64.Vec3{0, 0.375, 0})
			opts.Rotation = cube.Rotation{math.Atan2(direction[2], direction[0])*180/math.Pi + 90, 0}
			opts.Velocity = direction.Mul(0.1)
			tx.AddEntity(tx.World().EntityRegistry().Config().ChestBoat(opts, held.Variant.Int()))
		} else {
			d.eject(tx, opts, stack.Grow(1-stack.Count()))
		}
	case item.Minecart, item.MinecartChest, item.MinecartHopper, item.MinecartTNT:
		front := pos.Side(d.Facing)
		if IsRail(tx.Block(front)) {
			opts = world.EntitySpawnOpts{Position: front.Vec3Middle().Add(mgl64.Vec3{0, 0.0625, 0})}
			direction, _, _ := RailInfo(tx.Block(front))
			if direction.Ascending() {
				opts.Position[1] += 0.5
			}
			conf := tx.World().EntityRegistry().Config()
			switch held.(type) {
			case item.Minecart:
				tx.AddEntity(conf.Minecart(opts))
			case item.MinecartChest:
				tx.AddEntity(conf.MinecartChest(opts))
			case item.MinecartHopper:
				tx.AddEntity(conf.MinecartHopper(opts))
			case item.MinecartTNT:
				tx.AddEntity(conf.MinecartTNT(opts))
			}
		} else {
			d.eject(tx, opts, stack.Grow(1-stack.Count()))
		}
	case item.FireCharge:
		front := pos.Side(d.Facing)
		_, wasFire := tx.Block(front).(Fire)
		Fire{}.Start(tx, front)
		_, isFire := tx.Block(front).(Fire)
		if wasFire || !isFire {
			d.eject(tx, opts, stack.Grow(1-stack.Count()))
		}
	default:
		d.eject(tx, opts, stack.Grow(1-stack.Count()))
	}
	_ = d.inventory.SetItem(slot, stack.Grow(-1))
	notifyComparatorUpdate(pos, tx)
}

func (Dispenser) eject(tx *world.Tx, opts world.EntitySpawnOpts, stack item.Stack) {
	opts.Velocity = opts.Velocity.Mul(0.2 / 1.1)
	tx.AddEntity(tx.World().EntityRegistry().Config().Item(opts, stack))
}

func (Dispenser) EncodeItem() (string, int16) { return "minecraft:dispenser", 0 }

func (d Dispenser) EncodeBlock() (string, map[string]any) {
	return "minecraft:dispenser", map[string]any{"facing_direction": int32(d.Facing), "triggered_bit": boolByte(d.Triggered)}
}

func (d Dispenser) EncodeNBT() map[string]any {
	if d.inventory == nil {
		facing, triggered, customName := d.Facing, d.Triggered, d.CustomName
		d = NewDispenser()
		d.Facing, d.Triggered, d.CustomName = facing, triggered, customName
	}
	m := map[string]any{"id": "Dispenser", "Items": nbtconv.InvToNBT(d.inventory)}
	if d.CustomName != "" {
		m["CustomName"] = d.CustomName
	}
	return m
}

func (d Dispenser) DecodeNBT(data map[string]any) any {
	facing, triggered := d.Facing, d.Triggered
	d = NewDispenser()
	d.Facing, d.Triggered = facing, triggered
	d.CustomName = nbtconv.String(data, "CustomName")
	nbtconv.InvFromNBT(d.inventory, nbtconv.Slice(data, "Items"), data)
	return d
}

func allDispensers() (dispensers []world.Block) {
	for _, face := range cube.Faces() {
		dispensers = append(dispensers, Dispenser{Facing: face}, Dispenser{Facing: face, Triggered: true})
	}
	return
}
