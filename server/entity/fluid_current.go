package entity

import (
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// fluidCurrent ports EntityFluidInteraction.CurrentAccumulator: the flows of
// the blocks of a fluid an entity is in.
type fluidCurrent struct {
	height float64
	sum    mgl64.Vec3
	count  int
}

// fluidContact is what EntityFluidInteraction.update measures of the water
// and lava an entity is in.
type fluidContact struct {
	waterHeight, lavaHeight float64
	water, lava             fluidCurrent
}

// measureFluids ports EntityFluidInteraction.update for an entity with the
// bounding box box. The currents are only summed if current is set, as for
// entities that Entity.isPushedByFluid.
func measureFluids(tx blockReader, box cube.BBox, current bool) (c fluidContact) {
	entityY := box.Min()[1]
	box = box.Grow(-.001)
	minX, minY, minZ := int(math.Floor(box.Min()[0])), int(math.Floor(box.Min()[1])), int(math.Floor(box.Min()[2]))
	maxX, maxY, maxZ := int(math.Ceil(box.Max()[0]))-1, int(math.Ceil(box.Max()[1]))-1, int(math.Ceil(box.Max()[2]))-1
	if !tx.MayContainLiquid(cube.Pos{minX, minY, minZ}, cube.Pos{maxX, maxY, maxZ}) {
		return c
	}
	for x := minX; x <= maxX; x++ {
		for y := minY; y <= maxY; y++ {
			for z := minZ; z <= maxZ; z++ {
				pos := cube.Pos{x, y, z}
				l, ok := tx.Liquid(pos)
				if !ok {
					continue
				}
				top := float64(y) + liquidHeight(tx, pos, l)
				if top < box.Min()[1] {
					continue
				}
				var height *float64
				var cur *fluidCurrent
				switch l.LiquidType() {
				case "water":
					height, cur = &c.waterHeight, &c.water
				case "lava":
					height, cur = &c.lavaHeight, &c.lava
				default:
					continue
				}
				*height = math.Max(top-entityY, *height)
				if current {
					flow := fluidFlow(tx, pos, l)
					cur.height = math.Max(*height, cur.height)
					if cur.height < .4 {
						flow = flow.Mul(cur.height)
					}
					cur.sum = cur.sum.Add(flow)
					cur.count++
				}
			}
		}
	}
	return c
}

// push ports Entity.updateFluidInteraction pushing an entity other than a
// player along the currents it is in.
func (c fluidContact) push(tx *world.Tx, vel mgl64.Vec3) mgl64.Vec3 {
	if c.waterHeight > 0 {
		vel = c.water.apply(vel, .014)
	}
	if c.lavaHeight > 0 {
		// The FAST_LAVA attribute of the Nether.
		scale := .0023333333333333335
		if tx.World().Dimension().WaterEvaporates() {
			scale = .007
		}
		vel = c.lava.apply(vel, scale)
	}
	return vel
}

// apply ports CurrentAccumulator.applyTo for an entity other than a player.
func (c fluidCurrent) apply(vel mgl64.Vec3, scale float64) mgl64.Vec3 {
	if c.count == 0 || c.sum.LenSqr() < float64(float32(1e-5)) {
		return vel
	}
	impulse := javaNormalize(c.sum).Mul(scale)
	if math.Abs(vel[0]) < .003 && math.Abs(vel[2]) < .003 && impulse.Len() < .0045000000000000005 {
		impulse = javaNormalize(impulse).Mul(.0045000000000000005)
	}
	return vel.Add(impulse)
}

// fluidFlow ports FlowingFluid.getFlow: the direction the liquid l at pos
// flows in, pulled down where it falls along a wall.
func fluidFlow(tx blockReader, pos cube.Pos, l world.Liquid) mgl64.Vec3 {
	own := liquidOwnHeight(l)
	var flowX, flowZ float64
	for _, face := range cube.HorizontalFaces() {
		side := pos.Side(face)
		neighbour, ok := tx.Liquid(side)
		if ok && neighbour.LiquidType() != l.LiquidType() {
			continue
		}
		var height, distance float32
		if ok {
			height = liquidOwnHeight(neighbour)
		}
		if height == 0 {
			if !blockInTag(tx.Block(side), "minecraft:blocks_fluid_flow") {
				below, ok := tx.Liquid(side.Side(cube.FaceDown))
				if !ok || below.LiquidType() == l.LiquidType() {
					if ok {
						height = liquidOwnHeight(below)
					}
					if height > 0 {
						distance = own - (height - .8888889)
					}
				}
			}
		} else if height > 0 {
			distance = own - height
		}
		if distance != 0 {
			step := cube.Pos{}.Side(face)
			flowX += float64(step[0]) * float64(distance)
			flowZ += float64(step[2]) * float64(distance)
		}
	}
	flow := mgl64.Vec3{flowX, 0, flowZ}
	if l.LiquidFalling() {
		for _, face := range cube.HorizontalFaces() {
			side := pos.Side(face)
			if liquidSolidFace(tx, side, face, l) || liquidSolidFace(tx, side.Side(cube.FaceUp), face, l) {
				flow = javaNormalize(flow).Add(mgl64.Vec3{0, -6, 0})
				break
			}
		}
	}
	return javaNormalize(flow)
}

// liquidOwnHeight ports FlowingFluid.getOwnHeight: amount/9 of a block, with
// a falling liquid at its full amount of 8.
func liquidOwnHeight(l world.Liquid) float32 {
	if l.LiquidFalling() {
		return float32(8) / 9
	}
	return float32(l.LiquidDepth()) / 9
}

// liquidSolidFace ports FlowingFluid.isSolidFace for a horizontal face.
func liquidSolidFace(tx blockReader, pos cube.Pos, face cube.Face, l world.Liquid) bool {
	if other, ok := tx.Liquid(pos); ok && other.LiquidType() == l.LiquidType() {
		return false
	}
	switch b := tx.Block(pos).(type) {
	case block.Ice, block.FrostedIce:
		return false
	default:
		return b.Model().FaceSolid(pos, face, tx)
	}
}

// pushedByFluid ports the WaterAnimal, AgeableWaterCreature and Turtle
// overrides of Entity.isPushedByFluid.
func (*oceanWildlifeBehaviour) pushedByFluid(*Mob) bool { return false }

// pushedByFluid ports the AbstractFish and Axolotl overrides of
// Entity.isPushedByFluid.
func (*AquaticMobBehaviour) pushedByFluid(*Mob) bool { return false }

// pushedByFluid ports Frog.isPushedByFluid.
func (*frogBehaviour) pushedByFluid(*Mob) bool { return false }

// pushedByFluid ports Drowned.isPushedByFluid and AbstractNautilus's
// override; guardians are pushed as every monster is.
func (b *oceanBehaviour) pushedByFluid(*Mob) bool {
	switch b.kind {
	case oceanDrowned:
		return !b.swimming
	case oceanNautilus, oceanZombieNautilus:
		return false
	}
	return true
}
