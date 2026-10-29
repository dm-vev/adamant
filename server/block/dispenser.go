package block

import (
	"fmt"
	"log/slog"
	"math"
	"math/rand/v2"
	"strings"
	"sync"
	"time"

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
		slog.Error("dispenser loot table", "table", d.lootTable, "pos", pos, "error", err)
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
			d.eject(tx, pos, stack.Grow(1-stack.Count()))
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
			d.eject(tx, pos, stack.Grow(1-stack.Count()))
		}
	case item.Minecart, item.MinecartChest, item.MinecartHopper, item.MinecartTNT, item.MinecartCommandBlock:
		front := pos.Side(d.Facing)
		railPos := front
		if _, air := tx.Block(front).(Air); air {
			railPos = front.Side(cube.FaceDown)
		}
		if IsRail(tx.Block(railPos)) {
			// Native MinecartItem::dispense requests the rail-cell centre,
			// offset 1/8 along the horizontal facing, for every rail shape.
			var centre mgl64.Vec3
			for axis := range 3 {
				centre[axis] = float64(float32(railPos[axis]) + .5)
				if axis != 1 {
					centre[axis] = float64(float32(centre[axis]) + float32(direction[axis])*.125)
				}
			}
			opts = world.EntitySpawnOpts{Position: centre}
			conf := tx.World().EntityRegistry().Config()
			var cart *world.EntityHandle
			switch held.(type) {
			case item.Minecart:
				cart = conf.Minecart(opts)
			case item.MinecartChest:
				cart = conf.MinecartChest(opts)
			case item.MinecartHopper:
				cart = conf.MinecartHopper(opts)
			case item.MinecartTNT:
				cart = conf.MinecartTNT(opts)
			case item.MinecartCommandBlock:
				cart = conf.MinecartCommandBlock(opts)
			}
			if origin, ok := cart.Type().(interface{ NetworkOffset() float64 }); ok {
				centre[1] = float64(float32(centre[1]) - float32(origin.NetworkOffset()))
				cart.SetPosition(centre)
			}
			spawned := tx.AddEntity(cart)
			// Native chunk actor passes include a newly dispensed cart in the
			// current or a later chunk, but defer a cart entering an earlier
			// chunk until the next game tick. Coordinates are signed here.
			if owner, ok := tx.EntityChunk(spawned); ok && owner[0] >= int32(pos[0]>>4) && owner[1] >= int32(pos[2]>>4) {
				tx.QueueEntityTick(spawned)
			}
		} else {
			d.eject(tx, pos, stack.Grow(1-stack.Count()))
		}
	case item.FireCharge:
		tx.AddEntity(tx.World().EntityRegistry().Config().SmallFireball(opts, nil))
	case item.WindCharge:
		tx.AddEntity(tx.World().EntityRegistry().Config().WindCharge(opts, nil))
	case item.Arrow:
		tx.AddEntity(tx.World().EntityRegistry().Config().Arrow(opts, world.ArrowSpawnConfig{
			Damage: 2, Tip: held.Tip, ObtainArrowOnPickup: true,
		}))
	case item.BottleOfEnchanting:
		tx.AddEntity(tx.World().EntityRegistry().Config().BottleOfEnchanting(opts, nil))
	case item.SplashPotion:
		tx.AddEntity(tx.World().EntityRegistry().Config().SplashPotion(opts, held.Type, nil))
	case item.LingeringPotion:
		tx.AddEntity(tx.World().EntityRegistry().Config().LingeringPotion(opts, held.Type, nil))
	case item.Firework:
		tx.AddEntity(tx.World().EntityRegistry().Config().Firework(opts, held, nil, 1, 0, false))
	case item.ArmourStand:
		front := pos.Side(d.Facing)
		opts.Position = front.Vec3Middle()
		opts.Rotation = cube.Rotation{math.Atan2(direction[2], direction[0])*180/math.Pi + 90, 0}
		create := tx.World().EntityRegistry().Config().ArmourStand
		if create == nil {
			d.eject(tx, pos, stack.Grow(1-stack.Count()))
		} else {
			tx.AddEntity(create(opts))
		}
	case TNT:
		create := tx.World().EntityRegistry().Config().TNT
		if create == nil {
			d.eject(tx, pos, stack.Grow(1-stack.Count()))
		} else {
			tx.AddEntity(create(world.EntitySpawnOpts{Position: pos.Side(d.Facing).Vec3Middle()}, 4*time.Second, nil))
		}
	case item.SpawnEgg, item.Harness:
		behavior := held.(interface {
			UseOnEntity(world.Entity, *world.Tx, item.User, *item.UseContext) bool
		})
		front := pos.Side(d.Facing)
		box := cube.Box(float64(front.X()), float64(front.Y()), float64(front.Z()), float64(front.X()+1), float64(front.Y()+1), float64(front.Z()+1))
		for entity := range tx.EntitiesWithin(box) {
			ctx := &item.UseContext{}
			if behavior.UseOnEntity(entity, tx, nil, ctx) {
				left := stack.Damage(ctx.Damage).Grow(-ctx.CountSub)
				if !ctx.NewItem.Empty() {
					if left.Empty() {
						left = ctx.NewItem
					} else {
						d.eject(tx, pos, ctx.NewItem)
					}
				}
				_ = d.inventory.SetItem(slot, left)
				notifyComparatorUpdate(pos, tx)
				return
			}
		}
		d.eject(tx, pos, stack.Grow(1-stack.Count()))
	case item.Bucket, item.MobBucket, item.FishBucket, item.BoneMeal, item.FlintAndSteel, item.GlassBottle:
		behavior := held.(interface {
			UseOnBlock(cube.Pos, cube.Face, mgl64.Vec3, *world.Tx, item.User, *item.UseContext) bool
		})
		ctx := &item.UseContext{}
		if behavior.UseOnBlock(pos.Side(d.Facing), d.Facing, pos.Side(d.Facing).Vec3Centre(), tx, nil, ctx) {
			left := stack.Damage(ctx.Damage).Grow(-ctx.CountSub)
			if !ctx.NewItem.Empty() {
				if left.Empty() {
					left = ctx.NewItem
				} else {
					d.eject(tx, pos, ctx.NewItem)
				}
			}
			_ = d.inventory.SetItem(slot, left)
			notifyComparatorUpdate(pos, tx)
			return
		}
		d.eject(tx, pos, stack.Grow(1-stack.Count()))
	default:
		d.eject(tx, pos, stack.Grow(1-stack.Count()))
	}
	_ = d.inventory.SetItem(slot, stack.Grow(-1))
	notifyComparatorUpdate(pos, tx)
}

func (d Dispenser) eject(tx *world.Tx, pos cube.Pos, stack item.Stack) {
	ejectContainerItem(pos, d.Facing, tx, stack)
	playItemEjectionEffects(pos, d.Facing, tx)
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
