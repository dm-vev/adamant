package block

import (
	"iter"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/potion"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/go-gl/mathgl/mgl64"
)

// entityReach is how far from a block the position of an entity can be while
// its bounding box still reaches into the block: half the width of the widest
// entity, the ender dragon.
const entityReach = 8

// entitiesInBlock yields the entities whose bounding box intersects the block at
// pos, as Level.getEntitiesOfClass(..., new AABB(pos), ...) does for the
// behaviours of a dispenser. It is not the entities whose position is inside
// the block: a mob standing on the floor of the block has its position on its
// bottom face, which is outside of the box of the block.
func entitiesInBlock(tx *world.Tx, pos cube.Pos) iter.Seq[world.Entity] {
	box := cube.Box(float64(pos.X()), float64(pos.Y()), float64(pos.Z()), float64(pos.X()+1), float64(pos.Y()+1), float64(pos.Z()+1))
	return func(yield func(world.Entity) bool) {
		for e := range tx.EntitiesWithin(box.Grow(entityReach)) {
			if !e.H().Type().BBox(e).Translate(e.Position()).IntersectsWith(box) {
				continue
			}
			if !yield(e) {
				return
			}
		}
	}
}

// ignitable is a block that flint and steel lights: a campfire, a candle, a
// cake with a candle and TNT.
type ignitable interface {
	Ignite(pos cube.Pos, tx *world.Tx, igniter world.Entity) bool
}

// dispenseFlintAndSteel ports FlintAndSteelDispenseItemBehavior: fire goes
// into the air in front where it survives, a campfire, candle or cake there
// is lit and TNT is primed; otherwise the dispenser clicks and keeps the item.
// A success wears the item down by one.
func dispenseFlintAndSteel(src dispenseSource) dispensed {
	tx, front := src.tx, src.front()
	switch b := tx.Block(front).(type) {
	case Air:
		if !tx.PlaceFire(front) {
			return src.failed()
		}
		tx.EmitGameEvent(world.GameEventBlockPlace, front.Vec3Centre(), world.GameEventContext{})
	case ignitable:
		if !b.Ignite(front, tx, nil) {
			return src.failed()
		}
	default:
		return src.failed()
	}
	return dispensed{left: src.stack.Damage(1), effects: dispenseEffects{click: clickDone}}
}

// waterPlantGrower is a block, water, that bone meal grows seagrass and corals
// in.
type waterPlantGrower interface {
	GrowWaterPlants(pos cube.Pos, face cube.Face, tx *world.Tx) bool
}

// dispenseBoneMeal ports the behaviour of Items.BONE_MEAL: BoneMealItem
// .growCrop, or .growWaterPlant for the water in front. It keeps the item and
// clicks if neither grows anything.
func dispenseBoneMeal(src dispenseSource) dispensed {
	tx, front := src.tx, src.front()
	grown, area := false, false
	if bm, ok := tx.Block(front).(item.BoneMealAffected); ok {
		if result := bm.BoneMeal(front, tx); result != item.BoneMealResultNone {
			grown, area = true, result == item.BoneMealResultArea
		}
	}
	if !grown {
		// growWaterPlant without a clicked face: no fan on the wall.
		if w, ok := tx.Block(front).(waterPlantGrower); ok && w.GrowWaterPlants(front, cube.FaceUp, tx) {
			grown = true
		}
	}
	if !grown {
		return src.failed()
	}
	// Level event 1505: the growth particles at the target.
	tx.AddParticle(front.Vec3(), particle.BoneMeal{Area: area})
	return src.done()
}

// dispenseGlassBottle ports the behaviour of Items.GLASS_BOTTLE: a beehive or
// bee nest with honey gives a honey bottle, and water in front a water bottle,
// which take the place of the bottle or go into the dispenser; the bottle is
// ejected otherwise.
func dispenseGlassBottle(src dispenseSource) dispensed {
	tx, front := src.tx, src.front()
	switch tx.Block(front).(type) {
	case Beehive, BeeNest:
		ctx := &item.UseContext{}
		if (item.GlassBottle{}).UseOnBlock(front, cube.FaceUp, mgl64.Vec3{}, tx, nil, ctx) {
			tx.EmitGameEvent(world.GameEventFluidPickup, src.pos.Vec3Centre(), world.GameEventContext{})
			return src.withRemainder(ctx.NewItem)
		}
	}
	if l, ok := tx.Liquid(front); ok && l.LiquidType() == "water" {
		tx.EmitGameEvent(world.GameEventFluidPickup, src.pos.Vec3Centre(), world.GameEventContext{})
		return src.withRemainder(item.NewStack(item.Potion{Type: potion.Water()}, 1))
	}
	return dispenseDefault(src)
}
