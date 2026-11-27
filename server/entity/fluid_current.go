package entity

import (
	"math"

	"github.com/dm-vev/adamant/server/block"
	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/world"
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

const (
	liquidKindNone  uint8 = 0
	liquidKindWater uint8 = 1
	liquidKindLava  uint8 = 2
)

func liquidKindOf(typ string) uint8 {
	switch typ {
	case "water":
		return liquidKindWater
	case "lava":
		return liquidKindLava
	default:
		return liquidKindNone
	}
}

// measureFluids ports EntityFluidInteraction.update for an entity with the
// bounding box box. The currents are only summed if current is set, as for
// entities that Entity.isPushedByFluid.
func measureFluids(tx blockReader, box cube.BBox, current bool) (c fluidContact) {
	entityY := box.Min()[1]
	shrunk := box.Grow(-.001)
	minX, minY, minZ := int(math.Floor(shrunk.Min()[0])), int(math.Floor(shrunk.Min()[1])), int(math.Floor(shrunk.Min()[2]))
	maxX, maxY, maxZ := int(math.Ceil(shrunk.Max()[0]))-1, int(math.Ceil(shrunk.Max()[1]))-1, int(math.Ceil(shrunk.Max()[2]))-1
	if !tx.MayContainLiquid(cube.Pos{minX, minY, minZ}, cube.Pos{maxX, maxY, maxZ}) {
		return c
	}
	return measureFluidsRange(tx, entityY, shrunk.Min()[1], minX, minY, minZ, maxX, maxY, maxZ, current)
}

func measureFluidsRange(tx blockReader, entityY, shrunkMinY float64, minX, minY, minZ, maxX, maxY, maxZ int, current bool) (c fluidContact) {
	reg := tx.World().BlockRegistry()
	for x := minX; x <= maxX; x++ {
		for y := minY; y <= maxY; y++ {
			for z := minZ; z <= maxZ; z++ {
				pos := cube.Pos{x, y, z}
				rid, ok := tx.LiquidRID(pos)
				if !ok {
					continue
				}
				kind, depth, falling := reg.LiquidInfo(rid)
				var height *float64
				var cur *fluidCurrent
				switch kind {
				case liquidKindWater:
					height, cur = &c.waterHeight, &c.water
				case liquidKindLava:
					height, cur = &c.lavaHeight, &c.lava
				default:
					continue
				}
				top := float64(y) + liquidHeightInfoWithRID(tx, reg, pos, rid, kind, depth, falling)
				if top < shrunkMinY {
					continue
				}
				*height = math.Max(top-entityY, *height)
				if current {
					flow := fluidFlowInfoWithRID(tx, reg, pos, rid, kind, depth, falling)
					cur.height = math.Max(*height, cur.height)
					if flow != (mgl64.Vec3{}) {
						if cur.height < .4 {
							flow = flow.Mul(cur.height)
						}
						cur.sum = cur.sum.Add(flow)
					}
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
	impulse := normalizeVec3(c.sum).Mul(scale)
	if math.Abs(vel[0]) < .003 && math.Abs(vel[2]) < .003 && impulse.Len() < .0045000000000000005 {
		impulse = normalizeVec3(impulse).Mul(.0045000000000000005)
	}
	return vel.Add(impulse)
}

// liquidHeightInfo ports FlowingFluid.getHeight using precomputed liquid metadata.
func liquidHeightInfo(tx blockReader, reg world.BlockRegistry, pos cube.Pos, kind, depth uint8, falling bool) float64 {
	return liquidHeightInfoWithRID(tx, reg, pos, 0, kind, depth, falling)
}

func liquidHeightInfoWithRID(tx blockReader, reg world.BlockRegistry, pos cube.Pos, rid uint32, kind, depth uint8, falling bool) float64 {
	if arid, ok := tx.LiquidRID(cube.Pos{pos[0], pos[1] + 1, pos[2]}); ok {
		if rid != 0 && arid == rid {
			return 1
		}
		if akind, _, _ := reg.LiquidInfo(arid); akind == kind {
			return 1
		}
	}
	if falling {
		return 8.0 / 9
	}
	return float64(depth) / 9
}

var horizontalFlowDirs = [4]struct {
	face   cube.Face
	dx, dz int
}{
	{cube.FaceNorth, 0, -1},
	{cube.FaceEast, 1, 0},
	{cube.FaceSouth, 0, 1},
	{cube.FaceWest, -1, 0},
}

// fluidFlow ports FlowingFluid.getFlow: the direction the liquid l at pos
// flows in, pulled down where it falls along a wall.
func fluidFlow(tx blockReader, pos cube.Pos, l world.Liquid) mgl64.Vec3 {
	if kind := liquidKindOf(l.LiquidType()); kind != liquidKindNone {
		return fluidFlowInfo(tx, tx.World().BlockRegistry(), pos, kind, uint8(max(0, min(8, l.LiquidDepth()))), l.LiquidFalling())
	}
	return fluidFlowFallback(tx, pos, l)
}

func fluidFlowInfo(tx blockReader, reg world.BlockRegistry, pos cube.Pos, kind, depth uint8, falling bool) mgl64.Vec3 {
	return fluidFlowInfoWithRID(tx, reg, pos, 0, kind, depth, falling)
}

func fluidFlowInfoWithRID(tx blockReader, reg world.BlockRegistry, pos cube.Pos, rid uint32, kind, depth uint8, falling bool) mgl64.Vec3 {
	stillSource := depth == 8 && !falling
	own := liquidOwnHeightInfo(depth, falling)
	var flowX, flowZ float64
	for _, dir := range horizontalFlowDirs {
		side := cube.Pos{pos[0] + dir.dx, pos[1], pos[2] + dir.dz}
		var height, distance float32
		if nrid, ok := tx.LiquidRID(side); ok {
			if stillSource && rid != 0 && nrid == rid {
				continue
			}
			nkind, ndepth, nfalling := reg.LiquidInfo(nrid)
			if nkind != kind {
				continue
			}
			if stillSource && ndepth == 8 && !nfalling {
				continue
			}
			height = liquidOwnHeightInfo(ndepth, nfalling)
		}
		if height == 0 {
			if brid, ok := tx.LiquidRID(cube.Pos{side[0], side[1] - 1, side[2]}); ok {
				bkind, bdepth, bfalling := reg.LiquidInfo(brid)
				if bkind == kind {
					if bh := liquidOwnHeightInfo(bdepth, bfalling); bh > 0 && !blockInTag(tx.Block(side), "minecraft:blocks_fluid_flow") {
						distance = own - (bh - .8888889)
					}
				}
			}
		} else if height > 0 {
			distance = own - height
		}
		if distance != 0 {
			flowX += float64(dir.dx) * float64(distance)
			flowZ += float64(dir.dz) * float64(distance)
		}
	}
	if !falling && flowX == 0 && flowZ == 0 {
		return mgl64.Vec3{}
	}
	flow := mgl64.Vec3{flowX, 0, flowZ}
	if falling {
		for _, dir := range horizontalFlowDirs {
			side := cube.Pos{pos[0] + dir.dx, pos[1], pos[2] + dir.dz}
			if liquidSolidFaceInfo(tx, reg, side, dir.face, kind) || liquidSolidFaceInfo(tx, reg, cube.Pos{side[0], side[1] + 1, side[2]}, dir.face, kind) {
				flow = normalizeVec3(flow).Add(mgl64.Vec3{0, -6, 0})
				break
			}
		}
	}
	return normalizeVec3(flow)
}

func fluidFlowFallback(tx blockReader, pos cube.Pos, l world.Liquid) mgl64.Vec3 {
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
				flow = normalizeVec3(flow).Add(mgl64.Vec3{0, -6, 0})
				break
			}
		}
	}
	return normalizeVec3(flow)
}

// liquidOwnHeightInfo ports FlowingFluid.getOwnHeight for precomputed liquid metadata.
func liquidOwnHeightInfo(depth uint8, falling bool) float32 {
	if falling {
		return float32(8) / 9
	}
	return float32(depth) / 9
}

// liquidOwnHeight ports FlowingFluid.getOwnHeight: amount/9 of a block, with
// a falling liquid at its full amount of 8.
func liquidOwnHeight(l world.Liquid) float32 {
	if l.LiquidFalling() {
		return float32(8) / 9
	}
	return float32(l.LiquidDepth()) / 9
}

// liquidSolidFaceInfo ports FlowingFluid.isSolidFace for a horizontal face using liquid RIDs.
func liquidSolidFaceInfo(tx blockReader, reg world.BlockRegistry, pos cube.Pos, face cube.Face, kind uint8) bool {
	if orid, ok := tx.LiquidRID(pos); ok {
		if okind, _, _ := reg.LiquidInfo(orid); okind == kind {
			return false
		}
	}
	switch b := tx.Block(pos).(type) {
	case block.Ice, block.FrostedIce:
		return false
	default:
		return b.Model().FaceSolid(pos, face, tx)
	}
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
