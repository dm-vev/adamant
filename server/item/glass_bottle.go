package item

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item/potion"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// GlassBottle is an item that can hold various liquids.
type GlassBottle struct{}

// bottleFiller is implemented by blocks that can fill bottles by clicking on them.
type bottleFiller interface {
	// FillBottle fills a GlassBottle by interacting with a block. Blocks that implement this interface return both the
	// block that should be placed in the world after filling the bottle, and the item that was produced as a result of
	// the filling.
	// If the bool returned is false, nothing will happen when using a GlassBottle on the block.
	FillBottle() (world.Block, Stack, bool)
}

// honeyHarvester is implemented by blocks that react to shears or a glass
// bottle taking their honey, as beehives anger their bees.
type honeyHarvester interface {
	HoneyHarvested(pos cube.Pos, tx *world.Tx, user User)
}

// UseOnBlock ...
func (g GlassBottle) UseOnBlock(pos cube.Pos, _ cube.Face, _ mgl64.Vec3, tx *world.Tx, user User, ctx *UseContext) bool {
	bl := tx.Block(pos)
	if b, ok := bl.(bottleFiller); ok {
		var res world.Block
		if res, ctx.NewItem, ok = b.FillBottle(); ok {
			ctx.SubtractFromCount(1)
			// Some blocks (think a cauldron) change when using a bottle on
			// it. Blocks holding slices, like beehives, are not comparable,
			// so their states are compared.
			if world.BlockRuntimeID(res) != world.BlockRuntimeID(bl) {
				tx.SetBlock(pos, res, nil)
			}
			if h, ok := bl.(honeyHarvester); ok {
				h.HoneyHarvested(pos, tx, user)
			}
			return true
		}
	}
	return g.fillWithWater(tx, user, pos, ctx)
}

// fillWithWater fills the bottle from the water source the user looks at,
// as BottleItem.use does past any dragon's breath.
func (g GlassBottle) fillWithWater(tx *world.Tx, user User, pos cube.Pos, ctx *UseContext) bool {
	if user != nil {
		var ok bool
		if _, pos, _, ok = povHit(tx, user, povSourceFluid); !ok {
			return false
		}
	}
	// A dispenser fills the bottle from a water source in front of it.
	if l, ok := tx.Liquid(pos); !ok || l.LiquidType() != "water" || l.LiquidDepth() != 8 || l.LiquidFalling() {
		return false
	}
	tx.PlaySound(pos.Vec3Centre(), sound.BottleFill{})
	tx.EmitGameEvent(world.GameEventFluidPickup, pos.Vec3Centre(), world.GameEventContext{Source: user})
	ctx.SubtractFromCount(1)
	ctx.NewItem = NewStack(Potion{Type: potion.Water()}, 1)
	return true
}

// Use fills the bottle with the dragon's breath of a cloud of it within two
// blocks of the user, as BottleItem.use does.
func (g GlassBottle) Use(tx *world.Tx, user User, ctx *UseContext) bool {
	box := user.H().Type().BBox(user).Translate(user.Position()).Grow(2)
	for e := range tx.EntitiesWithin(box) {
		cloud, ok := e.(interface{ BottleDragonBreath(*world.Tx) bool })
		if !ok || !box.IntersectsWith(e.H().Type().BBox(e).Translate(e.Position())) || !cloud.BottleDragonBreath(tx) {
			continue
		}
		tx.PlaySound(user.Position(), sound.Custom{Name: "item.bottle.fill_dragonbreath", Volume: 1, Pitch: 1})
		ctx.SubtractFromCount(1)
		ctx.NewItem = NewStack(DragonBreath{}, 1)
		return true
	}
	return g.fillWithWater(tx, user, cube.Pos{}, ctx)
}

// EncodeItem ...
func (g GlassBottle) EncodeItem() (name string, meta int16) {
	return "minecraft:glass_bottle", 0
}
