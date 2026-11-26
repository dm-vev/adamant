package entity

import (
	"math"

	"github.com/dm-vev/adamant/server/block"
	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// CheckEntityInsiders applies inside-block effects from available terrain without loading or waiting for chunks.
func CheckEntityInsiders(tx *world.Tx, box cube.BBox, e world.Entity) {
	checkEntityInsidersFiltered(tx, box, e, nil)
}

func checkEntityInsidersFiltered(tx *world.Tx, box cube.BBox, e world.Entity, include func(world.Block) bool) {
	low, high := cube.PosFromVec3(box.Min()), cube.PosFromVec3(box.Max())
	reg := tx.World().BlockRegistry()

	for y := low[1]; y <= high[1]; y++ {
		for x := low[0]; x <= high[0]; x++ {
			for z := low[2]; z <= high[2]; z++ {
				blockPos := cube.Pos{x, y, z}
				rid, loaded := tx.BlockRID(blockPos)
				if !loaded {
					continue
				}
				flags := reg.BlockFlags(rid)
				if flags.HasEntityInsider() {
					if b, ok := tx.BlockLoaded(blockPos); ok {
						if collide, ok := b.(block.EntityInsider); ok && (include == nil || include(b)) {
							collide.EntityInside(blockPos, tx, e)
							if flags.IsLiquid() {
								continue
							}
						}
					}
				}
				if flags.IsLiquid() {
					continue
				}
				if lrid, ok := tx.LiquidRID(blockPos); ok && reg.BlockFlags(lrid).HasEntityInsider() {
					if l, ok := tx.Liquid(blockPos); ok {
						if collide, ok := l.(block.EntityInsider); ok && (include == nil || include(l)) {
							collide.EntityInside(blockPos, tx, e)
						}
					}
				}
			}
		}
	}
}

// checkEntityInsiders checks if the entity is colliding with any EntityInsider blocks.
func checkEntityInsiders(tx *world.Tx, e world.Entity) {
	box := e.H().Type().BBox(e).Translate(e.Position()).Grow(-0.0001)
	if buttonProjectile(e) {
		checkEntityInsidersFiltered(tx, box, e, func(b world.Block) bool {
			_, button := b.(block.Button)
			return !button
		})
		return
	}
	CheckEntityInsiders(tx, box, e)
}

// Native arrow/trident button contacts precede projectile movement. A newly
// arriving arrow is already stopped at the button in one native snapshot,
// but presses it on the following entity tick. Other block effects retain
// their existing contact phase.
func checkButtonContactsBeforeMovement(tx *world.Tx, e world.Entity) {
	if !buttonProjectile(e) {
		return
	}
	box := e.H().Type().BBox(e).Translate(e.Position()).Grow(-0.0001)
	checkEntityInsidersFiltered(tx, box, e, func(b world.Block) bool {
		_, button := b.(block.Button)
		return button
	})
}

func buttonProjectile(e world.Entity) bool {
	switch e.H().Type().EncodeEntity() {
	case "minecraft:arrow", "minecraft:thrown_trident":
		return true
	}
	return false
}

// stepOnBlock ports the stepOn half of Entity.applyEffectsFromBlocks: an
// entity on the ground acts on the block it stands on, as magma burns it
// and redstone ore lights up.
func stepOnBlock(tx *world.Tx, e world.Entity) {
	reg := tx.World().BlockRegistry()
	pos, rid, ok := onPosLegacy(tx, reg, e)
	if !ok || !reg.BlockFlags(rid).HasEntityStepper() {
		return
	}
	if b, loaded := tx.BlockLoaded(pos); loaded {
		if s, ok := b.(block.EntityStepper); ok {
			s.EntityStepOn(pos, tx, e)
		}
	}
}

// onPosLegacy ports Entity.getOnPosLegacy: the block 0.2 below the feet of
// an entity, or of the block that supports it if it stands on the edge of
// one.
func onPosLegacy(tx *world.Tx, reg world.BlockRegistry, e world.Entity) (cube.Pos, uint32, bool) {
	p := e.Position()
	y := int(math.Floor(p[1] - .2))
	airRID := reg.AirRuntimeID()
	centre := cube.Pos{int(math.Floor(p[0])), y, int(math.Floor(p[2]))}
	if rid, loaded := tx.BlockRID(centre); !loaded {
		return centre, 0, false
	} else if rid != airRID {
		return centre, rid, true
	}
	// Entity.checkSupportingBlock: the nearest block under the box that
	// supports the entity.
	box := e.H().Type().BBox(e).Translate(p)
	best, bestRID, bestDist, found := centre, uint32(0), math.MaxFloat64, false
	for x := int(math.Floor(box.Min()[0])); x <= int(math.Floor(box.Max()[0])); x++ {
		for z := int(math.Floor(box.Min()[2])); z <= int(math.Floor(box.Max()[2])); z++ {
			pos := cube.Pos{x, y, z}
			rid, loaded := tx.BlockRID(pos)
			if !loaded || rid == airRID {
				continue
			}
			if d := pos.Vec3Centre().Sub(mgl64.Vec3{p[0], pos.Vec3Centre()[1], p[2]}).LenSqr(); d < bestDist {
				best, bestRID, bestDist, found = pos, rid, d, true
			}
		}
	}
	return best, bestRID, found
}
