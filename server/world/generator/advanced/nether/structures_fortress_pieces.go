package nether

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/java26"
)

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func mirrorLeftRight(dir cube.Direction) cube.Direction {
	switch dir {
	case cube.North:
		return cube.South
	case cube.South:
		return cube.North
	default:
		return dir
	}
}

func (p *bridgePieceBase) rotatedFacing(dir cube.Direction) cube.Direction {
	switch p.facing {
	case cube.South:
		return mirrorLeftRight(dir)
	case cube.West:
		return mirrorLeftRight(dir).RotateRight()
	case cube.East:
		return dir.RotateRight()
	default:
		return dir
	}
}

func (p *bridgePieceBase) isInside(sbb structureBB, x, y, z int) bool {
	wx := p.getXWithOffset(x, z)
	wy := p.getYWithOffset(y)
	wz := p.getZWithOffset(x, z)
	return sbb.isVecInside(wx, wy, wz)
}

func (p *bridgePieceBase) setStairs(c fortressRegion, chunkX, chunkZ int, sbb structureBB, x, y, z int, facing cube.Direction, g *Nether) {
	dir := p.rotatedFacing(facing)
	p.setBlock(c, chunkX, chunkZ, sbb, x, y, z, g.netherBrickStairsRID[dir])
}

func (p *bridgePieceBase) generateChest(c fortressRegion, chunkX, chunkZ int, sbb structureBB, r *java26.Random, x, y, z int, g *Nether) bool {
	if g.chestRID == 0 {
		return false
	}
	wx := p.getXWithOffset(x, z)
	wy := p.getYWithOffset(y)
	wz := p.getZWithOffset(x, z)
	if !sbb.isVecInside(wx, wy, wz) {
		return false
	}
	if wx>>4 != chunkX || wz>>4 != chunkZ {
		return false
	}
	minY := int(c.Range().Min())
	maxY := int(c.Range().Max())
	if wy < minY || wy > maxY {
		return false
	}
	if c.Block(uint8(wx&15), int16(wy), uint8(wz&15), 0) == g.chestRID {
		return false
	}
	p.setBlock(c, chunkX, chunkZ, sbb, x, y, z, g.chestRID)
	lootSeed := r.NextLong()
	pos := cube.Pos{wx, wy, wz}
	p.recordBlockEntity(pos, map[string]any{
		"id": "minecraft:chest", "x": int32(wx), "y": int32(wy), "z": int32(wz),
		"LootTable": "minecraft:chests/nether_bridge", "LootTableSeed": lootSeed,
	})
	return true
}

func (p *bridgePieceBase) recordBlockEntity(pos cube.Pos, data map[string]any) {
	if p.placement == nil {
		panic("nether: fortress block entity has no placement sink")
	}
	for i := len(p.placement.Blocks) - 1; i >= 0; i-- {
		block := &p.placement.Blocks[i]
		if block.Pos == [3]int32{int32(pos[0]), int32(pos[1]), int32(pos[2])} {
			block.NBT = data
			return
		}
	}
	panic("nether: fortress block entity has no matching block write")
}

func randomHorizontalFacing(r *java26.Random) cube.Direction {
	switch r.NextInt(4) {
	case 0:
		return cube.North
	case 1:
		return cube.East
	case 2:
		return cube.South
	default:
		return cube.West
	}
}

func createBridgePiece(kind bridgePieceKind, pieces []bridgePiece, r *java26.Random, x, y, z int, facing cube.Direction, depth int) bridgePiece {
	switch kind {
	case bridgeStraight:
		return createBridgeStraight(pieces, r, x, y, z, facing, depth)
	case bridgeCrossing3:
		return createBridgeCrossing3(pieces, r, x, y, z, facing, depth)
	case bridgeCrossing:
		return createBridgeCrossing(pieces, r, x, y, z, facing, depth)
	case bridgeStairs:
		return createBridgeStairs(pieces, r, x, y, z, depth, facing)
	case bridgeThrone:
		return createBridgeThrone(pieces, r, x, y, z, depth, facing)
	case bridgeEntrance:
		return createBridgeEntrance(pieces, r, x, y, z, facing, depth)
	case bridgeCorridor5:
		return createBridgeCorridor5(pieces, r, x, y, z, facing, depth)
	case bridgeCrossing2:
		return createBridgeCrossing2(pieces, r, x, y, z, facing, depth)
	case bridgeCorridor2:
		return createBridgeCorridor2(pieces, r, x, y, z, facing, depth)
	case bridgeCorridor:
		return createBridgeCorridor(pieces, r, x, y, z, facing, depth)
	case bridgeCorridor3:
		return createBridgeCorridor3(pieces, r, x, y, z, facing, depth)
	case bridgeCorridor4:
		return createBridgeCorridor4(pieces, r, x, y, z, facing, depth)
	case bridgeNetherStalkRoom:
		return createBridgeNetherStalkRoom(pieces, r, x, y, z, facing, depth)
	case bridgeEnd:
		return createBridgeEnd(pieces, r, x, y, z, facing, depth)
	default:
		return nil
	}
}

type netherBridgeCorridor struct {
	bridgePieceBase
	chest bool
}

func newBridgeCorridor(componentType int, r *java26.Random, bb structureBB, facing cube.Direction) *netherBridgeCorridor {
	return &netherBridgeCorridor{
		bridgePieceBase: bridgePieceBase{bb: bb, facing: facing, componentType: componentType},
		chest:           r.NextInt(3) == 0,
	}
}

func createBridgeCorridor(pieces []bridgePiece, r *java26.Random, x, y, z int, facing cube.Direction, depth int) bridgePiece {
	bb := componentBB(x, y, z, -1, 0, 0, 5, 7, 5, facing)
	if !isAboveGround(bb) || findIntersecting(pieces, bb) != nil {
		return nil
	}
	return newBridgeCorridor(depth, r, bb, facing)
}

func (p *netherBridgeCorridor) buildComponent(start *netherBridgeStart, pieces []bridgePiece, r *java26.Random) {
	p.getNextComponentX(start, pieces, r, 0, 1, true)
}

func (p *netherBridgeCorridor) addComponentParts(c fortressRegion, chunkX, chunkZ int, sbb structureBB, r *java26.Random, g *Nether) bool {
	brick := g.netherBricksRID
	nsFence, weFence := p.netherBrickFenceRID(g, 1|2), p.netherBrickFenceRID(g, 4|8)
	air := g.airRID

	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 0, 0, 0, 4, 1, 4, brick, brick, false, air)
	p.fillWithAir(c, chunkX, chunkZ, sbb, 0, 2, 0, 4, 5, 4, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 4, 2, 0, 4, 5, 4, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 4, 3, 1, 4, 4, 1, nsFence, nsFence, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 4, 3, 3, 4, 4, 3, nsFence, nsFence, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 0, 2, 0, 0, 5, 0, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 0, 2, 4, 3, 5, 4, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 1, 3, 4, 1, 4, 4, weFence, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 3, 3, 4, 3, 4, 4, weFence, brick, false, air)

	if p.chest && p.isInside(sbb, 3, 2, 3) {
		p.chest = false
		if p.state != nil {
			p.state.ChestPlaced = true
		}
		p.generateChest(c, chunkX, chunkZ, sbb, r, 3, 2, 3, g)
	}

	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 0, 6, 0, 4, 6, 4, brick, brick, false, air)

	for i := 0; i <= 4; i++ {
		for j := 0; j <= 4; j++ {
			p.replaceAirAndLiquidDownwards(c, chunkX, chunkZ, sbb, i, -1, j, brick, g)
		}
	}

	return true
}

type netherBridgeCorridor2 struct {
	bridgePieceBase
	chest bool
}

func newBridgeCorridor2(componentType int, r *java26.Random, bb structureBB, facing cube.Direction) *netherBridgeCorridor2 {
	return &netherBridgeCorridor2{
		bridgePieceBase: bridgePieceBase{bb: bb, facing: facing, componentType: componentType},
		chest:           r.NextInt(3) == 0,
	}
}

func createBridgeCorridor2(pieces []bridgePiece, r *java26.Random, x, y, z int, facing cube.Direction, depth int) bridgePiece {
	bb := componentBB(x, y, z, -1, 0, 0, 5, 7, 5, facing)
	if !isAboveGround(bb) || findIntersecting(pieces, bb) != nil {
		return nil
	}
	return newBridgeCorridor2(depth, r, bb, facing)
}

func (p *netherBridgeCorridor2) buildComponent(start *netherBridgeStart, pieces []bridgePiece, r *java26.Random) {
	p.getNextComponentZ(start, pieces, r, 0, 1, true)
}

func (p *netherBridgeCorridor2) addComponentParts(c fortressRegion, chunkX, chunkZ int, sbb structureBB, r *java26.Random, g *Nether) bool {
	brick := g.netherBricksRID
	nsFence, weFence := p.netherBrickFenceRID(g, 1|2), p.netherBrickFenceRID(g, 4|8)
	air := g.airRID

	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 0, 0, 0, 4, 1, 4, brick, brick, false, air)
	p.fillWithAir(c, chunkX, chunkZ, sbb, 0, 2, 0, 4, 5, 4, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 0, 2, 0, 0, 5, 4, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 0, 3, 1, 0, 4, 1, nsFence, nsFence, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 0, 3, 3, 0, 4, 3, nsFence, nsFence, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 4, 2, 0, 4, 5, 0, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 1, 2, 4, 4, 5, 4, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 1, 3, 4, 1, 4, 4, weFence, weFence, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 3, 3, 4, 3, 4, 4, weFence, weFence, false, air)

	if p.chest && p.isInside(sbb, 1, 2, 3) {
		p.chest = false
		if p.state != nil {
			p.state.ChestPlaced = true
		}
		p.generateChest(c, chunkX, chunkZ, sbb, r, 1, 2, 3, g)
	}

	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 0, 6, 0, 4, 6, 4, brick, brick, false, air)

	for i := 0; i <= 4; i++ {
		for j := 0; j <= 4; j++ {
			p.replaceAirAndLiquidDownwards(c, chunkX, chunkZ, sbb, i, -1, j, brick, g)
		}
	}

	return true
}

type netherBridgeCorridor3 struct {
	bridgePieceBase
}

func newBridgeCorridor3(componentType int, bb structureBB, facing cube.Direction) *netherBridgeCorridor3 {
	return &netherBridgeCorridor3{
		bridgePieceBase: bridgePieceBase{bb: bb, facing: facing, componentType: componentType},
	}
}

func createBridgeCorridor3(pieces []bridgePiece, r *java26.Random, x, y, z int, facing cube.Direction, depth int) bridgePiece {
	bb := componentBB(x, y, z, -1, -7, 0, 5, 14, 10, facing)
	if !isAboveGround(bb) || findIntersecting(pieces, bb) != nil {
		return nil
	}
	return newBridgeCorridor3(depth, bb, facing)
}

func (p *netherBridgeCorridor3) buildComponent(start *netherBridgeStart, pieces []bridgePiece, r *java26.Random) {
	p.getNextComponentNormal(start, pieces, r, 1, 0, true)
}

func (p *netherBridgeCorridor3) addComponentParts(c fortressRegion, chunkX, chunkZ int, sbb structureBB, r *java26.Random, g *Nether) bool {
	brick := g.netherBricksRID
	fence := p.netherBrickFenceRID(g, 1|2)
	air := g.airRID

	for i := 0; i <= 9; i++ {
		j := maxInt(1, 7-i)
		k := minInt(maxInt(j+5, 14-i), 13)
		p.fillWithBlocks(c, chunkX, chunkZ, sbb, 0, 0, i, 4, j, i, brick, brick, false, air)
		p.fillWithAir(c, chunkX, chunkZ, sbb, 1, j+1, i, 3, k-1, i, air)

		if i <= 6 {
			p.setStairs(c, chunkX, chunkZ, sbb, 1, j+1, i, cube.South, g)
			p.setStairs(c, chunkX, chunkZ, sbb, 2, j+1, i, cube.South, g)
			p.setStairs(c, chunkX, chunkZ, sbb, 3, j+1, i, cube.South, g)
		}

		p.fillWithBlocks(c, chunkX, chunkZ, sbb, 0, k, i, 4, k, i, brick, brick, false, air)
		p.fillWithBlocks(c, chunkX, chunkZ, sbb, 0, j+1, i, 0, k-1, i, brick, brick, false, air)
		p.fillWithBlocks(c, chunkX, chunkZ, sbb, 4, j+1, i, 4, k-1, i, brick, brick, false, air)

		if i&1 == 0 {
			p.fillWithBlocks(c, chunkX, chunkZ, sbb, 0, j+2, i, 0, j+3, i, fence, fence, false, air)
			p.fillWithBlocks(c, chunkX, chunkZ, sbb, 4, j+2, i, 4, j+3, i, fence, fence, false, air)
		}

		for i1 := 0; i1 <= 4; i1++ {
			p.replaceAirAndLiquidDownwards(c, chunkX, chunkZ, sbb, i1, -1, i, brick, g)
		}
	}

	return true
}

type netherBridgeCorridor4 struct {
	bridgePieceBase
}

func newBridgeCorridor4(componentType int, bb structureBB, facing cube.Direction) *netherBridgeCorridor4 {
	return &netherBridgeCorridor4{
		bridgePieceBase: bridgePieceBase{bb: bb, facing: facing, componentType: componentType},
	}
}

func createBridgeCorridor4(pieces []bridgePiece, r *java26.Random, x, y, z int, facing cube.Direction, depth int) bridgePiece {
	bb := componentBB(x, y, z, -3, 0, 0, 9, 7, 9, facing)
	if !isAboveGround(bb) || findIntersecting(pieces, bb) != nil {
		return nil
	}
	return newBridgeCorridor4(depth, bb, facing)
}

func (p *netherBridgeCorridor4) buildComponent(start *netherBridgeStart, pieces []bridgePiece, r *java26.Random) {
	i := 1
	if p.facing == cube.West || p.facing == cube.North {
		i = 5
	}
	p.getNextComponentX(start, pieces, r, 0, i, r.NextInt(8) > 0)
	p.getNextComponentZ(start, pieces, r, 0, i, r.NextInt(8) > 0)
}

func (p *netherBridgeCorridor4) addComponentParts(c fortressRegion, chunkX, chunkZ int, sbb structureBB, r *java26.Random, g *Nether) bool {
	brick := g.netherBricksRID
	weFence, nsFence := p.netherBrickFenceRID(g, 4|8), p.netherBrickFenceRID(g, 1|2)
	southEastFence, southWestFence := p.netherBrickFenceRID(g, 2|8), p.netherBrickFenceRID(g, 2|4)
	air := g.airRID

	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 0, 0, 0, 8, 1, 8, brick, brick, false, air)
	p.fillWithAir(c, chunkX, chunkZ, sbb, 0, 2, 0, 8, 5, 8, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 0, 6, 0, 8, 6, 5, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 0, 2, 0, 2, 5, 0, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 6, 2, 0, 8, 5, 0, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 1, 3, 0, 1, 4, 0, weFence, weFence, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 7, 3, 0, 7, 4, 0, weFence, weFence, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 0, 2, 4, 8, 2, 8, brick, brick, false, air)
	p.fillWithAir(c, chunkX, chunkZ, sbb, 1, 1, 4, 2, 2, 4, air)
	p.fillWithAir(c, chunkX, chunkZ, sbb, 6, 1, 4, 7, 2, 4, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 1, 3, 8, 7, 3, 8, weFence, weFence, false, air)
	p.setBlock(c, chunkX, chunkZ, sbb, 0, 3, 8, southEastFence)
	p.setBlock(c, chunkX, chunkZ, sbb, 8, 3, 8, southWestFence)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 0, 3, 6, 0, 3, 7, nsFence, nsFence, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 8, 3, 6, 8, 3, 7, nsFence, nsFence, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 0, 3, 4, 0, 5, 5, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 8, 3, 4, 8, 5, 5, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 1, 3, 5, 2, 5, 5, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 6, 3, 5, 7, 5, 5, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 1, 4, 5, 1, 5, 5, weFence, weFence, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 7, 4, 5, 7, 5, 5, weFence, weFence, false, air)

	for i := 0; i <= 5; i++ {
		for j := 0; j <= 8; j++ {
			p.replaceAirAndLiquidDownwards(c, chunkX, chunkZ, sbb, j, -1, i, brick, g)
		}
	}

	return true
}

type netherBridgeCorridor5 struct {
	bridgePieceBase
}

func newBridgeCorridor5(componentType int, bb structureBB, facing cube.Direction) *netherBridgeCorridor5 {
	return &netherBridgeCorridor5{
		bridgePieceBase: bridgePieceBase{bb: bb, facing: facing, componentType: componentType},
	}
}

func createBridgeCorridor5(pieces []bridgePiece, r *java26.Random, x, y, z int, facing cube.Direction, depth int) bridgePiece {
	bb := componentBB(x, y, z, -1, 0, 0, 5, 7, 5, facing)
	if !isAboveGround(bb) || findIntersecting(pieces, bb) != nil {
		return nil
	}
	return newBridgeCorridor5(depth, bb, facing)
}

func (p *netherBridgeCorridor5) buildComponent(start *netherBridgeStart, pieces []bridgePiece, r *java26.Random) {
	p.getNextComponentNormal(start, pieces, r, 1, 0, true)
}

func (p *netherBridgeCorridor5) addComponentParts(c fortressRegion, chunkX, chunkZ int, sbb structureBB, r *java26.Random, g *Nether) bool {
	brick := g.netherBricksRID
	fence := p.netherBrickFenceRID(g, 1|2)
	air := g.airRID

	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 0, 0, 0, 4, 1, 4, brick, brick, false, air)
	p.fillWithAir(c, chunkX, chunkZ, sbb, 0, 2, 0, 4, 5, 4, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 0, 2, 0, 0, 5, 4, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 4, 2, 0, 4, 5, 4, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 0, 3, 1, 0, 4, 1, fence, fence, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 0, 3, 3, 0, 4, 3, fence, fence, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 4, 3, 1, 4, 4, 1, fence, fence, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 4, 3, 3, 4, 4, 3, fence, fence, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 0, 6, 0, 4, 6, 4, brick, brick, false, air)

	for i := 0; i <= 4; i++ {
		for j := 0; j <= 4; j++ {
			p.replaceAirAndLiquidDownwards(c, chunkX, chunkZ, sbb, i, -1, j, brick, g)
		}
	}

	return true
}

type netherBridgeCrossing struct {
	bridgePieceBase
}

func newBridgeCrossing(componentType int, bb structureBB, facing cube.Direction) *netherBridgeCrossing {
	return &netherBridgeCrossing{
		bridgePieceBase: bridgePieceBase{bb: bb, facing: facing, componentType: componentType},
	}
}

func createBridgeCrossing(pieces []bridgePiece, r *java26.Random, x, y, z int, facing cube.Direction, depth int) bridgePiece {
	bb := componentBB(x, y, z, -2, 0, 0, 7, 9, 7, facing)
	if !isAboveGround(bb) || findIntersecting(pieces, bb) != nil {
		return nil
	}
	return newBridgeCrossing(depth, bb, facing)
}

func (p *netherBridgeCrossing) buildComponent(start *netherBridgeStart, pieces []bridgePiece, r *java26.Random) {
	p.getNextComponentNormal(start, pieces, r, 2, 0, false)
	p.getNextComponentX(start, pieces, r, 0, 2, false)
	p.getNextComponentZ(start, pieces, r, 0, 2, false)
}

func (p *netherBridgeCrossing) addComponentParts(c fortressRegion, chunkX, chunkZ int, sbb structureBB, r *java26.Random, g *Nether) bool {
	brick := g.netherBricksRID
	weFence, nsFence := p.netherBrickFenceRID(g, 4|8), p.netherBrickFenceRID(g, 1|2)
	air := g.airRID

	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 0, 0, 0, 6, 1, 6, brick, brick, false, air)
	p.fillWithAir(c, chunkX, chunkZ, sbb, 0, 2, 0, 6, 7, 6, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 0, 2, 0, 1, 6, 0, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 0, 2, 6, 1, 6, 6, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 5, 2, 0, 6, 6, 0, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 5, 2, 6, 6, 6, 6, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 0, 2, 0, 0, 6, 1, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 0, 2, 5, 0, 6, 6, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 6, 2, 0, 6, 6, 1, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 6, 2, 5, 6, 6, 6, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 2, 6, 0, 4, 6, 0, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 2, 5, 0, 4, 5, 0, weFence, weFence, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 2, 6, 6, 4, 6, 6, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 2, 5, 6, 4, 5, 6, weFence, weFence, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 0, 6, 2, 0, 6, 4, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 0, 5, 2, 0, 5, 4, nsFence, nsFence, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 6, 6, 2, 6, 6, 4, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 6, 5, 2, 6, 5, 4, nsFence, nsFence, false, air)

	for i := 0; i <= 6; i++ {
		for j := 0; j <= 6; j++ {
			p.replaceAirAndLiquidDownwards(c, chunkX, chunkZ, sbb, i, -1, j, brick, g)
		}
	}

	return true
}

type netherBridgeCrossing2 struct {
	bridgePieceBase
}

func newBridgeCrossing2(componentType int, bb structureBB, facing cube.Direction) *netherBridgeCrossing2 {
	return &netherBridgeCrossing2{
		bridgePieceBase: bridgePieceBase{bb: bb, facing: facing, componentType: componentType},
	}
}

func createBridgeCrossing2(pieces []bridgePiece, r *java26.Random, x, y, z int, facing cube.Direction, depth int) bridgePiece {
	bb := componentBB(x, y, z, -1, 0, 0, 5, 7, 5, facing)
	if !isAboveGround(bb) || findIntersecting(pieces, bb) != nil {
		return nil
	}
	return newBridgeCrossing2(depth, bb, facing)
}

func (p *netherBridgeCrossing2) buildComponent(start *netherBridgeStart, pieces []bridgePiece, r *java26.Random) {
	p.getNextComponentNormal(start, pieces, r, 1, 0, true)
	p.getNextComponentX(start, pieces, r, 0, 1, true)
	p.getNextComponentZ(start, pieces, r, 0, 1, true)
}

func (p *netherBridgeCrossing2) addComponentParts(c fortressRegion, chunkX, chunkZ int, sbb structureBB, r *java26.Random, g *Nether) bool {
	brick := g.netherBricksRID
	air := g.airRID

	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 0, 0, 0, 4, 1, 4, brick, brick, false, air)
	p.fillWithAir(c, chunkX, chunkZ, sbb, 0, 2, 0, 4, 5, 4, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 0, 2, 0, 0, 5, 0, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 4, 2, 0, 4, 5, 0, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 0, 2, 4, 0, 5, 4, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 4, 2, 4, 4, 5, 4, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 0, 6, 0, 4, 6, 4, brick, brick, false, air)

	for i := 0; i <= 4; i++ {
		for j := 0; j <= 4; j++ {
			p.replaceAirAndLiquidDownwards(c, chunkX, chunkZ, sbb, i, -1, j, brick, g)
		}
	}

	return true
}

type netherBridgeCrossing3 struct {
	bridgePieceBase
}

func newBridgeCrossing3(componentType int, bb structureBB, facing cube.Direction) *netherBridgeCrossing3 {
	return &netherBridgeCrossing3{
		bridgePieceBase: bridgePieceBase{bb: bb, facing: facing, componentType: componentType},
	}
}

func newBridgeCrossing3Start(r *java26.Random, x, z int, _ *Nether) *netherBridgeCrossing3 {
	facing := randomHorizontalFacing(r)
	bb := structureBB{
		minX: x,
		minY: 64,
		minZ: z,
		maxX: x + 19 - 1,
		maxY: 73,
		maxZ: z + 19 - 1,
	}
	return newBridgeCrossing3(0, bb, facing)
}

func createBridgeCrossing3(pieces []bridgePiece, r *java26.Random, x, y, z int, facing cube.Direction, depth int) bridgePiece {
	bb := componentBB(x, y, z, -8, -3, 0, 19, 10, 19, facing)
	if !isAboveGround(bb) || findIntersecting(pieces, bb) != nil {
		return nil
	}
	return newBridgeCrossing3(depth, bb, facing)
}

func (p *netherBridgeCrossing3) buildComponent(start *netherBridgeStart, pieces []bridgePiece, r *java26.Random) {
	p.getNextComponentNormal(start, pieces, r, 8, 3, false)
	p.getNextComponentX(start, pieces, r, 3, 8, false)
	p.getNextComponentZ(start, pieces, r, 3, 8, false)
}

func (p *netherBridgeCrossing3) addComponentParts(c fortressRegion, chunkX, chunkZ int, sbb structureBB, r *java26.Random, g *Nether) bool {
	brick := g.netherBricksRID
	air := g.airRID

	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 7, 3, 0, 11, 4, 18, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 0, 3, 7, 18, 4, 11, brick, brick, false, air)
	p.fillWithAir(c, chunkX, chunkZ, sbb, 8, 5, 0, 10, 7, 18, air)
	p.fillWithAir(c, chunkX, chunkZ, sbb, 0, 5, 8, 18, 7, 10, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 7, 5, 0, 7, 5, 7, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 7, 5, 11, 7, 5, 18, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 11, 5, 0, 11, 5, 7, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 11, 5, 11, 11, 5, 18, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 0, 5, 7, 7, 5, 7, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 11, 5, 7, 18, 5, 7, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 0, 5, 11, 7, 5, 11, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 11, 5, 11, 18, 5, 11, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 7, 2, 0, 11, 2, 5, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 7, 2, 13, 11, 2, 18, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 7, 0, 0, 11, 1, 3, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 7, 0, 15, 11, 1, 18, brick, brick, false, air)

	for i := 7; i <= 11; i++ {
		for j := 0; j <= 2; j++ {
			p.replaceAirAndLiquidDownwards(c, chunkX, chunkZ, sbb, i, -1, j, brick, g)
			p.replaceAirAndLiquidDownwards(c, chunkX, chunkZ, sbb, i, -1, 18-j, brick, g)
		}
	}

	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 0, 2, 7, 5, 2, 11, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 13, 2, 7, 18, 2, 11, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 0, 0, 7, 3, 1, 11, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 15, 0, 7, 18, 1, 11, brick, brick, false, air)

	for k := 0; k <= 2; k++ {
		for l := 7; l <= 11; l++ {
			p.replaceAirAndLiquidDownwards(c, chunkX, chunkZ, sbb, k, -1, l, brick, g)
			p.replaceAirAndLiquidDownwards(c, chunkX, chunkZ, sbb, 18-k, -1, l, brick, g)
		}
	}

	return true
}

type netherBridgeEnd struct {
	bridgePieceBase
	fillSeed int32
}

func newBridgeEnd(componentType int, r *java26.Random, bb structureBB, facing cube.Direction) *netherBridgeEnd {
	return &netherBridgeEnd{
		bridgePieceBase: bridgePieceBase{bb: bb, facing: facing, componentType: componentType},
		fillSeed:        r.NextInt32(),
	}
}

func createBridgeEnd(pieces []bridgePiece, r *java26.Random, x, y, z int, facing cube.Direction, depth int) bridgePiece {
	bb := componentBB(x, y, z, -1, -3, 0, 5, 10, 8, facing)
	if !isAboveGround(bb) || findIntersecting(pieces, bb) != nil {
		return nil
	}
	return newBridgeEnd(depth, r, bb, facing)
}

func (p *netherBridgeEnd) buildComponent(start *netherBridgeStart, pieces []bridgePiece, r *java26.Random) {
}

func (p *netherBridgeEnd) addComponentParts(c fortressRegion, chunkX, chunkZ int, sbb structureBB, r *java26.Random, g *Nether) bool {
	brick := g.netherBricksRID
	rand := java26.NewLegacyRandom(int64(p.fillSeed))

	for i := 0; i <= 4; i++ {
		for j := 3; j <= 4; j++ {
			k := int(rand.NextInt(8))
			p.fillWithBlocks(c, chunkX, chunkZ, sbb, i, j, 0, i, j, k, brick, brick, false, g.airRID)
		}
	}

	l := int(rand.NextInt(8))
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 0, 5, 0, 0, 5, l, brick, brick, false, g.airRID)
	l = int(rand.NextInt(8))
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 4, 5, 0, 4, 5, l, brick, brick, false, g.airRID)

	for i1 := 0; i1 <= 4; i1++ {
		k1 := int(rand.NextInt(5))
		p.fillWithBlocks(c, chunkX, chunkZ, sbb, i1, 2, 0, i1, 2, k1, brick, brick, false, g.airRID)
	}

	for j1 := 0; j1 <= 4; j1++ {
		for l1 := 0; l1 <= 1; l1++ {
			i2 := int(rand.NextInt(3))
			p.fillWithBlocks(c, chunkX, chunkZ, sbb, j1, l1, 0, j1, l1, i2, brick, brick, false, g.airRID)
		}
	}

	return true
}

type netherBridgeEntrance struct {
	bridgePieceBase
}

func newBridgeEntrance(componentType int, bb structureBB, facing cube.Direction) *netherBridgeEntrance {
	return &netherBridgeEntrance{
		bridgePieceBase: bridgePieceBase{bb: bb, facing: facing, componentType: componentType},
	}
}

func createBridgeEntrance(pieces []bridgePiece, r *java26.Random, x, y, z int, facing cube.Direction, depth int) bridgePiece {
	bb := componentBB(x, y, z, -5, -3, 0, 13, 14, 13, facing)
	if !isAboveGround(bb) || findIntersecting(pieces, bb) != nil {
		return nil
	}
	return newBridgeEntrance(depth, bb, facing)
}

func (p *netherBridgeEntrance) buildComponent(start *netherBridgeStart, pieces []bridgePiece, r *java26.Random) {
	p.getNextComponentNormal(start, pieces, r, 5, 3, true)
}

func (p *netherBridgeEntrance) addComponentParts(c fortressRegion, chunkX, chunkZ int, sbb structureBB, r *java26.Random, g *Nether) bool {
	brick := g.netherBricksRID
	defaultFence := p.netherBrickFenceRID(g, 0)
	weFence, nsFence := p.netherBrickFenceRID(g, 4|8), p.netherBrickFenceRID(g, 1|2)
	northEastFence, southEastFence := p.netherBrickFenceRID(g, 1|8), p.netherBrickFenceRID(g, 2|8)
	southWestFence, northWestFence := p.netherBrickFenceRID(g, 2|4), p.netherBrickFenceRID(g, 1|4)
	northSouthWest, northSouthEast := p.netherBrickFenceRID(g, 1|2|4), p.netherBrickFenceRID(g, 1|2|8)
	air := g.airRID

	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 0, 3, 0, 12, 4, 12, brick, brick, false, air)
	p.fillWithAir(c, chunkX, chunkZ, sbb, 0, 5, 0, 12, 13, 12, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 0, 5, 0, 1, 12, 12, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 11, 5, 0, 12, 12, 12, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 2, 5, 11, 4, 12, 12, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 8, 5, 11, 10, 12, 12, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 5, 9, 11, 7, 12, 12, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 2, 5, 0, 4, 12, 1, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 8, 5, 0, 10, 12, 1, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 5, 9, 0, 7, 12, 1, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 2, 11, 2, 10, 12, 10, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 5, 8, 0, 7, 8, 0, defaultFence, defaultFence, false, air)

	for i := 1; i <= 11; i += 2 {
		p.fillWithBlocks(c, chunkX, chunkZ, sbb, i, 10, 0, i, 11, 0, weFence, weFence, false, air)
		p.fillWithBlocks(c, chunkX, chunkZ, sbb, i, 10, 12, i, 11, 12, weFence, weFence, false, air)
		p.fillWithBlocks(c, chunkX, chunkZ, sbb, 0, 10, i, 0, 11, i, nsFence, nsFence, false, air)
		p.fillWithBlocks(c, chunkX, chunkZ, sbb, 12, 10, i, 12, 11, i, nsFence, nsFence, false, air)
		p.setBlock(c, chunkX, chunkZ, sbb, i, 13, 0, brick)
		p.setBlock(c, chunkX, chunkZ, sbb, i, 13, 12, brick)
		p.setBlock(c, chunkX, chunkZ, sbb, 0, 13, i, brick)
		p.setBlock(c, chunkX, chunkZ, sbb, 12, 13, i, brick)
		if i != 11 {
			p.setBlock(c, chunkX, chunkZ, sbb, i+1, 13, 0, weFence)
			p.setBlock(c, chunkX, chunkZ, sbb, i+1, 13, 12, weFence)
			p.setBlock(c, chunkX, chunkZ, sbb, 0, 13, i+1, nsFence)
			p.setBlock(c, chunkX, chunkZ, sbb, 12, 13, i+1, nsFence)
		}
	}

	p.setBlock(c, chunkX, chunkZ, sbb, 0, 13, 0, northEastFence)
	p.setBlock(c, chunkX, chunkZ, sbb, 0, 13, 12, southEastFence)
	p.setBlock(c, chunkX, chunkZ, sbb, 12, 13, 12, southWestFence)
	p.setBlock(c, chunkX, chunkZ, sbb, 12, 13, 0, northWestFence)

	for k := 3; k <= 9; k += 2 {
		p.fillWithBlocks(c, chunkX, chunkZ, sbb, 1, 7, k, 1, 8, k, northSouthWest, northSouthWest, false, air)
		p.fillWithBlocks(c, chunkX, chunkZ, sbb, 11, 7, k, 11, 8, k, northSouthEast, northSouthEast, false, air)
	}

	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 4, 2, 0, 8, 2, 12, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 0, 2, 4, 12, 2, 8, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 4, 0, 0, 8, 1, 3, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 4, 0, 9, 8, 1, 12, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 0, 0, 4, 3, 1, 8, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 9, 0, 4, 12, 1, 8, brick, brick, false, air)

	for l := 4; l <= 8; l++ {
		for j := 0; j <= 2; j++ {
			p.replaceAirAndLiquidDownwards(c, chunkX, chunkZ, sbb, l, -1, j, brick, g)
			p.replaceAirAndLiquidDownwards(c, chunkX, chunkZ, sbb, l, -1, 12-j, brick, g)
		}
	}

	for i1 := 0; i1 <= 2; i1++ {
		for j1 := 4; j1 <= 8; j1++ {
			p.replaceAirAndLiquidDownwards(c, chunkX, chunkZ, sbb, i1, -1, j1, brick, g)
			p.replaceAirAndLiquidDownwards(c, chunkX, chunkZ, sbb, 12-i1, -1, j1, brick, g)
		}
	}

	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 5, 5, 5, 7, 5, 7, brick, brick, false, air)
	p.fillWithAir(c, chunkX, chunkZ, sbb, 6, 1, 6, 6, 4, 6, air)
	p.setBlock(c, chunkX, chunkZ, sbb, 6, 0, 6, brick)
	p.setBlock(c, chunkX, chunkZ, sbb, 6, 5, 6, g.lavaFlowRID)

	return true
}

type netherBridgeNetherStalkRoom struct {
	bridgePieceBase
}

func newBridgeNetherStalkRoom(componentType int, bb structureBB, facing cube.Direction) *netherBridgeNetherStalkRoom {
	return &netherBridgeNetherStalkRoom{
		bridgePieceBase: bridgePieceBase{bb: bb, facing: facing, componentType: componentType},
	}
}

func createBridgeNetherStalkRoom(pieces []bridgePiece, r *java26.Random, x, y, z int, facing cube.Direction, depth int) bridgePiece {
	bb := componentBB(x, y, z, -5, -3, 0, 13, 14, 13, facing)
	if !isAboveGround(bb) || findIntersecting(pieces, bb) != nil {
		return nil
	}
	return newBridgeNetherStalkRoom(depth, bb, facing)
}

func (p *netherBridgeNetherStalkRoom) buildComponent(start *netherBridgeStart, pieces []bridgePiece, r *java26.Random) {
	p.getNextComponentNormal(start, pieces, r, 5, 3, true)
	p.getNextComponentNormal(start, pieces, r, 5, 11, true)
}

func (p *netherBridgeNetherStalkRoom) addComponentParts(c fortressRegion, chunkX, chunkZ int, sbb structureBB, r *java26.Random, g *Nether) bool {
	brick := g.netherBricksRID
	weFence, nsFence := p.netherBrickFenceRID(g, 4|8), p.netherBrickFenceRID(g, 1|2)
	northEastFence, southEastFence := p.netherBrickFenceRID(g, 1|8), p.netherBrickFenceRID(g, 2|8)
	southWestFence, northWestFence := p.netherBrickFenceRID(g, 2|4), p.netherBrickFenceRID(g, 1|4)
	northSouthWest, northSouthEast := p.netherBrickFenceRID(g, 1|2|4), p.netherBrickFenceRID(g, 1|2|8)
	air := g.airRID

	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 0, 3, 0, 12, 4, 12, brick, brick, false, air)
	p.fillWithAir(c, chunkX, chunkZ, sbb, 0, 5, 0, 12, 13, 12, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 0, 5, 0, 1, 12, 12, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 11, 5, 0, 12, 12, 12, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 2, 5, 11, 4, 12, 12, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 8, 5, 11, 10, 12, 12, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 5, 9, 11, 7, 12, 12, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 2, 5, 0, 4, 12, 1, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 8, 5, 0, 10, 12, 1, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 5, 9, 0, 7, 12, 1, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 2, 11, 2, 10, 12, 10, brick, brick, false, air)

	for i := 1; i <= 11; i += 2 {
		p.fillWithBlocks(c, chunkX, chunkZ, sbb, i, 10, 0, i, 11, 0, weFence, weFence, false, air)
		p.fillWithBlocks(c, chunkX, chunkZ, sbb, i, 10, 12, i, 11, 12, weFence, weFence, false, air)
		p.fillWithBlocks(c, chunkX, chunkZ, sbb, 0, 10, i, 0, 11, i, nsFence, nsFence, false, air)
		p.fillWithBlocks(c, chunkX, chunkZ, sbb, 12, 10, i, 12, 11, i, nsFence, nsFence, false, air)
		p.setBlock(c, chunkX, chunkZ, sbb, i, 13, 0, brick)
		p.setBlock(c, chunkX, chunkZ, sbb, i, 13, 12, brick)
		p.setBlock(c, chunkX, chunkZ, sbb, 0, 13, i, brick)
		p.setBlock(c, chunkX, chunkZ, sbb, 12, 13, i, brick)
		if i != 11 {
			p.setBlock(c, chunkX, chunkZ, sbb, i+1, 13, 0, weFence)
			p.setBlock(c, chunkX, chunkZ, sbb, i+1, 13, 12, weFence)
			p.setBlock(c, chunkX, chunkZ, sbb, 0, 13, i+1, nsFence)
			p.setBlock(c, chunkX, chunkZ, sbb, 12, 13, i+1, nsFence)
		}
	}

	p.setBlock(c, chunkX, chunkZ, sbb, 0, 13, 0, northEastFence)
	p.setBlock(c, chunkX, chunkZ, sbb, 0, 13, 12, southEastFence)
	p.setBlock(c, chunkX, chunkZ, sbb, 12, 13, 12, southWestFence)
	p.setBlock(c, chunkX, chunkZ, sbb, 12, 13, 0, northWestFence)

	for j1 := 3; j1 <= 9; j1 += 2 {
		p.fillWithBlocks(c, chunkX, chunkZ, sbb, 1, 7, j1, 1, 8, j1, northSouthWest, northSouthWest, false, air)
		p.fillWithBlocks(c, chunkX, chunkZ, sbb, 11, 7, j1, 11, 8, j1, northSouthEast, northSouthEast, false, air)
	}

	for j := 0; j <= 6; j++ {
		k := j + 4

		for l := 5; l <= 7; l++ {
			p.setStairs(c, chunkX, chunkZ, sbb, l, 5+j, k, cube.North, g)
		}

		if k >= 5 && k <= 8 {
			p.fillWithBlocks(c, chunkX, chunkZ, sbb, 5, 5, k, 7, j+4, k, brick, brick, false, air)
		} else if k >= 9 && k <= 10 {
			p.fillWithBlocks(c, chunkX, chunkZ, sbb, 5, 8, k, 7, j+4, k, brick, brick, false, air)
		}

		if j >= 1 {
			p.fillWithAir(c, chunkX, chunkZ, sbb, 5, 6+j, k, 7, 9+j, k, air)
		}
	}

	for k1 := 5; k1 <= 7; k1++ {
		p.setStairs(c, chunkX, chunkZ, sbb, k1, 12, 11, cube.North, g)
	}

	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 5, 6, 7, 5, 7, 7, northSouthEast, northSouthEast, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 7, 6, 7, 7, 7, 7, northSouthWest, northSouthWest, false, air)
	p.fillWithAir(c, chunkX, chunkZ, sbb, 5, 13, 12, 7, 13, 12, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 2, 5, 2, 3, 5, 3, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 2, 5, 9, 3, 5, 10, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 2, 5, 4, 2, 5, 8, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 9, 5, 2, 10, 5, 3, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 9, 5, 9, 10, 5, 10, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 10, 5, 4, 10, 5, 8, brick, brick, false, air)
	p.setStairs(c, chunkX, chunkZ, sbb, 4, 5, 2, cube.West, g)
	p.setStairs(c, chunkX, chunkZ, sbb, 4, 5, 3, cube.West, g)
	p.setStairs(c, chunkX, chunkZ, sbb, 4, 5, 9, cube.West, g)
	p.setStairs(c, chunkX, chunkZ, sbb, 4, 5, 10, cube.West, g)
	p.setStairs(c, chunkX, chunkZ, sbb, 8, 5, 2, cube.East, g)
	p.setStairs(c, chunkX, chunkZ, sbb, 8, 5, 3, cube.East, g)
	p.setStairs(c, chunkX, chunkZ, sbb, 8, 5, 9, cube.East, g)
	p.setStairs(c, chunkX, chunkZ, sbb, 8, 5, 10, cube.East, g)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 3, 4, 4, 4, 4, 8, g.soulSandRID, g.soulSandRID, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 8, 4, 4, 9, 4, 8, g.soulSandRID, g.soulSandRID, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 3, 5, 4, 4, 5, 8, g.netherWartRID, g.netherWartRID, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 8, 5, 4, 9, 5, 8, g.netherWartRID, g.netherWartRID, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 4, 2, 0, 8, 2, 12, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 0, 2, 4, 12, 2, 8, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 4, 0, 0, 8, 1, 3, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 4, 0, 9, 8, 1, 12, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 0, 0, 4, 3, 1, 8, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 9, 0, 4, 12, 1, 8, brick, brick, false, air)

	for l1 := 4; l1 <= 8; l1++ {
		for i1 := 0; i1 <= 2; i1++ {
			p.replaceAirAndLiquidDownwards(c, chunkX, chunkZ, sbb, l1, -1, i1, brick, g)
			p.replaceAirAndLiquidDownwards(c, chunkX, chunkZ, sbb, l1, -1, 12-i1, brick, g)
		}
	}

	for i2 := 0; i2 <= 2; i2++ {
		for j2 := 4; j2 <= 8; j2++ {
			p.replaceAirAndLiquidDownwards(c, chunkX, chunkZ, sbb, i2, -1, j2, brick, g)
			p.replaceAirAndLiquidDownwards(c, chunkX, chunkZ, sbb, 12-i2, -1, j2, brick, g)
		}
	}

	return true
}

type netherBridgeStairs struct {
	bridgePieceBase
}

func newBridgeStairs(componentType int, bb structureBB, facing cube.Direction) *netherBridgeStairs {
	return &netherBridgeStairs{
		bridgePieceBase: bridgePieceBase{bb: bb, facing: facing, componentType: componentType},
	}
}

func createBridgeStairs(pieces []bridgePiece, r *java26.Random, x, y, z int, depth int, facing cube.Direction) bridgePiece {
	bb := componentBB(x, y, z, -2, 0, 0, 7, 11, 7, facing)
	if !isAboveGround(bb) || findIntersecting(pieces, bb) != nil {
		return nil
	}
	return newBridgeStairs(depth, bb, facing)
}

func (p *netherBridgeStairs) buildComponent(start *netherBridgeStart, pieces []bridgePiece, r *java26.Random) {
	p.getNextComponentZ(start, pieces, r, 6, 2, false)
}

func (p *netherBridgeStairs) addComponentParts(c fortressRegion, chunkX, chunkZ int, sbb structureBB, r *java26.Random, g *Nether) bool {
	brick := g.netherBricksRID
	weFence, nsFence := p.netherBrickFenceRID(g, 4|8), p.netherBrickFenceRID(g, 1|2)
	air := g.airRID

	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 0, 0, 0, 6, 1, 6, brick, brick, false, air)
	p.fillWithAir(c, chunkX, chunkZ, sbb, 0, 2, 0, 6, 10, 6, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 0, 2, 0, 1, 8, 0, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 5, 2, 0, 6, 8, 0, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 0, 2, 1, 0, 8, 6, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 6, 2, 1, 6, 8, 6, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 1, 2, 6, 5, 8, 6, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 0, 3, 2, 0, 5, 4, nsFence, nsFence, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 6, 3, 2, 6, 5, 2, nsFence, nsFence, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 6, 3, 4, 6, 5, 4, nsFence, nsFence, false, air)
	p.setBlock(c, chunkX, chunkZ, sbb, 5, 2, 5, brick)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 4, 2, 5, 4, 3, 5, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 3, 2, 5, 3, 4, 5, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 2, 2, 5, 2, 5, 5, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 1, 2, 5, 1, 6, 5, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 1, 7, 1, 5, 7, 4, brick, brick, false, air)
	p.fillWithAir(c, chunkX, chunkZ, sbb, 6, 8, 2, 6, 8, 4, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 2, 6, 0, 4, 8, 0, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 2, 5, 0, 4, 5, 0, weFence, weFence, false, air)

	for i := 0; i <= 6; i++ {
		for j := 0; j <= 6; j++ {
			p.replaceAirAndLiquidDownwards(c, chunkX, chunkZ, sbb, i, -1, j, brick, g)
		}
	}

	return true
}

type netherBridgeStraight struct {
	bridgePieceBase
}

func newBridgeStraight(componentType int, bb structureBB, facing cube.Direction) *netherBridgeStraight {
	return &netherBridgeStraight{
		bridgePieceBase: bridgePieceBase{bb: bb, facing: facing, componentType: componentType},
	}
}

func createBridgeStraight(pieces []bridgePiece, r *java26.Random, x, y, z int, facing cube.Direction, depth int) bridgePiece {
	bb := componentBB(x, y, z, -1, -3, 0, 5, 10, 19, facing)
	if !isAboveGround(bb) || findIntersecting(pieces, bb) != nil {
		return nil
	}
	return newBridgeStraight(depth, bb, facing)
}

func (p *netherBridgeStraight) buildComponent(start *netherBridgeStart, pieces []bridgePiece, r *java26.Random) {
	p.getNextComponentNormal(start, pieces, r, 1, 3, false)
}

func (p *netherBridgeStraight) addComponentParts(c fortressRegion, chunkX, chunkZ int, sbb structureBB, r *java26.Random, g *Nether) bool {
	brick := g.netherBricksRID
	nseFence, nswFence := p.netherBrickFenceRID(g, 1|2|8), p.netherBrickFenceRID(g, 1|2|4)
	air := g.airRID

	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 0, 3, 0, 4, 4, 18, brick, brick, false, air)
	p.fillWithAir(c, chunkX, chunkZ, sbb, 1, 5, 0, 3, 7, 18, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 0, 5, 0, 0, 5, 18, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 4, 5, 0, 4, 5, 18, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 0, 2, 0, 4, 2, 5, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 0, 2, 13, 4, 2, 18, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 0, 0, 0, 4, 1, 3, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 0, 0, 15, 4, 1, 18, brick, brick, false, air)

	for i := 0; i <= 4; i++ {
		for j := 0; j <= 2; j++ {
			p.replaceAirAndLiquidDownwards(c, chunkX, chunkZ, sbb, i, -1, j, brick, g)
			p.replaceAirAndLiquidDownwards(c, chunkX, chunkZ, sbb, i, -1, 18-j, brick, g)
		}
	}

	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 0, 1, 1, 0, 4, 1, nseFence, nseFence, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 0, 3, 4, 0, 4, 4, nseFence, nseFence, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 0, 3, 14, 0, 4, 14, nseFence, nseFence, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 0, 1, 17, 0, 4, 17, nseFence, nseFence, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 4, 1, 1, 4, 4, 1, nswFence, nswFence, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 4, 3, 4, 4, 4, 4, nswFence, nswFence, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 4, 3, 14, 4, 4, 14, nswFence, nswFence, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 4, 1, 17, 4, 4, 17, nswFence, nswFence, false, air)

	return true
}

type netherBridgeThrone struct {
	bridgePieceBase
	hasSpawner bool
}

func newBridgeThrone(componentType int, bb structureBB, facing cube.Direction) *netherBridgeThrone {
	return &netherBridgeThrone{
		bridgePieceBase: bridgePieceBase{bb: bb, facing: facing, componentType: componentType},
	}
}

func createBridgeThrone(pieces []bridgePiece, r *java26.Random, x, y, z int, depth int, facing cube.Direction) bridgePiece {
	bb := componentBB(x, y, z, -2, 0, 0, 7, 8, 9, facing)
	if !isAboveGround(bb) || findIntersecting(pieces, bb) != nil {
		return nil
	}
	return newBridgeThrone(depth, bb, facing)
}

func (p *netherBridgeThrone) buildComponent(start *netherBridgeStart, pieces []bridgePiece, r *java26.Random) {
}

func (p *netherBridgeThrone) addComponentParts(c fortressRegion, chunkX, chunkZ int, sbb structureBB, r *java26.Random, g *Nether) bool {
	brick := g.netherBricksRID
	westFence, eastFence := p.netherBrickFenceRID(g, 4), p.netherBrickFenceRID(g, 8)
	northEastFence, northWestFence := p.netherBrickFenceRID(g, 1|8), p.netherBrickFenceRID(g, 1|4)
	southEastFence, southWestFence := p.netherBrickFenceRID(g, 2|8), p.netherBrickFenceRID(g, 2|4)
	weFence, nsFence := p.netherBrickFenceRID(g, 4|8), p.netherBrickFenceRID(g, 1|2)
	air := g.airRID

	p.fillWithAir(c, chunkX, chunkZ, sbb, 0, 2, 0, 6, 7, 7, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 1, 0, 0, 5, 1, 7, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 1, 2, 1, 5, 2, 7, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 1, 3, 2, 5, 3, 7, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 1, 4, 3, 5, 4, 7, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 1, 2, 0, 1, 4, 2, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 5, 2, 0, 5, 4, 2, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 1, 5, 2, 1, 5, 3, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 5, 5, 2, 5, 5, 3, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 0, 5, 3, 0, 5, 8, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 6, 5, 3, 6, 5, 8, brick, brick, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 1, 5, 8, 5, 5, 8, brick, brick, false, air)
	p.setBlock(c, chunkX, chunkZ, sbb, 1, 6, 3, westFence)
	p.setBlock(c, chunkX, chunkZ, sbb, 5, 6, 3, eastFence)
	p.setBlock(c, chunkX, chunkZ, sbb, 0, 6, 3, northEastFence)
	p.setBlock(c, chunkX, chunkZ, sbb, 6, 6, 3, northWestFence)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 0, 6, 4, 0, 6, 7, nsFence, nsFence, false, air)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 6, 6, 4, 6, 6, 7, nsFence, nsFence, false, air)
	p.setBlock(c, chunkX, chunkZ, sbb, 0, 6, 8, southEastFence)
	p.setBlock(c, chunkX, chunkZ, sbb, 6, 6, 8, southWestFence)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 1, 6, 8, 5, 6, 8, weFence, weFence, false, air)
	p.setBlock(c, chunkX, chunkZ, sbb, 1, 7, 8, eastFence)
	p.fillWithBlocks(c, chunkX, chunkZ, sbb, 2, 7, 8, 4, 7, 8, weFence, weFence, false, air)
	p.setBlock(c, chunkX, chunkZ, sbb, 5, 7, 8, westFence)
	p.setBlock(c, chunkX, chunkZ, sbb, 2, 8, 8, eastFence)
	p.setBlock(c, chunkX, chunkZ, sbb, 3, 8, 8, weFence)
	p.setBlock(c, chunkX, chunkZ, sbb, 4, 8, 8, westFence)

	if !p.hasSpawner && p.isInside(sbb, 3, 5, 5) {
		p.hasSpawner = true
		if p.state != nil {
			p.state.SpawnerPlaced = true
		}
		if g.spawnerRID != 0 {
			wx, wy, wz := p.getXWithOffset(3, 5), p.getYWithOffset(5), p.getZWithOffset(3, 5)
			p.setBlock(c, chunkX, chunkZ, sbb, 3, 5, 5, g.spawnerRID)
			pos := cube.Pos{wx, wy, wz}
			p.recordBlockEntity(pos, map[string]any{
				"id": "minecraft:mob_spawner", "x": int32(wx), "y": int32(wy), "z": int32(wz),
				"SpawnData": map[string]any{"entity": map[string]any{"id": "minecraft:blaze"}},
				"Delay":     int16(20), "MinSpawnDelay": int16(200), "MaxSpawnDelay": int16(800),
				"SpawnCount": int16(4), "MaxNearbyEntities": int16(6), "RequiredPlayerRange": int16(16), "SpawnRange": int16(4),
			})
		}
	}

	for i := 0; i <= 6; i++ {
		for j := 0; j <= 6; j++ {
			p.replaceAirAndLiquidDownwards(c, chunkX, chunkZ, sbb, i, -1, j, brick, g)
		}
	}

	return true
}
