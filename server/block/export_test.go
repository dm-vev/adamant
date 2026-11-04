package block

import (
	"math"
	"math/rand/v2"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// scheduledTickBySwitch is ScheduledTick as it was before the dispense
// behaviours, kept until the comparison with them is in.
func (d Dispenser) scheduledTickBySwitch(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
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
	case item.SpawnEgg:
		if !held.Dispense(tx, pos, d.Facing) {
			return
		}
	case item.Harness:
		front := pos.Side(d.Facing)
		box := cube.Box(float64(front.X()), float64(front.Y()), float64(front.Z()), float64(front.X()+1), float64(front.Y()+1), float64(front.Z()+1))
		for entity := range tx.EntitiesWithin(box) {
			ctx := &item.UseContext{}
			if held.UseOnEntity(entity, tx, nil, ctx) {
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

// ScheduledTickBySwitch runs the dispense of a dispenser as it was before the
// dispense behaviours.
func ScheduledTickBySwitch(d Dispenser, pos cube.Pos, tx *world.Tx, r *rand.Rand) {
	d.scheduledTickBySwitch(pos, tx, r)
}
