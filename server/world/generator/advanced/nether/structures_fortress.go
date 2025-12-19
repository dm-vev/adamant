package nether

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/mc112"
)

type netherBridgeStructure struct {
	start  world.ChunkPos
	bb     structureBB
	pieces []bridgePiece
}

type structureBB struct {
	minX, minY, minZ int
	maxX, maxY, maxZ int
}

func (bb structureBB) intersects(o structureBB) bool {
	return bb.maxX >= o.minX && bb.minX <= o.maxX && bb.maxZ >= o.minZ && bb.minZ <= o.maxZ && bb.maxY >= o.minY && bb.minY <= o.maxY
}

func (bb structureBB) intersectsXZ(minX, minZ, maxX, maxZ int) bool {
	return bb.maxX >= minX && bb.minX <= maxX && bb.maxZ >= minZ && bb.minZ <= maxZ
}

func (bb structureBB) isVecInside(x, y, z int) bool {
	return x >= bb.minX && x <= bb.maxX && z >= bb.minZ && z <= bb.maxZ && y >= bb.minY && y <= bb.maxY
}

func (bb *structureBB) expandTo(o structureBB) {
	if o.minX < bb.minX {
		bb.minX = o.minX
	}
	if o.minY < bb.minY {
		bb.minY = o.minY
	}
	if o.minZ < bb.minZ {
		bb.minZ = o.minZ
	}
	if o.maxX > bb.maxX {
		bb.maxX = o.maxX
	}
	if o.maxY > bb.maxY {
		bb.maxY = o.maxY
	}
	if o.maxZ > bb.maxZ {
		bb.maxZ = o.maxZ
	}
}

func (bb *structureBB) offset(x, y, z int) {
	bb.minX += x
	bb.minY += y
	bb.minZ += z
	bb.maxX += x
	bb.maxY += y
	bb.maxZ += z
}

func (bb structureBB) xSize() int { return bb.maxX - bb.minX + 1 }
func (bb structureBB) ySize() int { return bb.maxY - bb.minY + 1 }
func (bb structureBB) zSize() int { return bb.maxZ - bb.minZ + 1 }

func componentBB(structMinX, structMinY, structMinZ, xMin, yMin, zMin, xMax, yMax, zMax int, facing cube.Direction) structureBB {
	switch facing {
	case cube.North:
		return structureBB{
			minX: structMinX + xMin,
			minY: structMinY + yMin,
			minZ: structMinZ - zMax + 1 + zMin,
			maxX: structMinX + xMax - 1 + xMin,
			maxY: structMinY + yMax - 1 + yMin,
			maxZ: structMinZ + zMin,
		}
	case cube.South:
		return structureBB{
			minX: structMinX + xMin,
			minY: structMinY + yMin,
			minZ: structMinZ + zMin,
			maxX: structMinX + xMax - 1 + xMin,
			maxY: structMinY + yMax - 1 + yMin,
			maxZ: structMinZ + zMax - 1 + zMin,
		}
	case cube.West:
		return structureBB{
			minX: structMinX - zMax + 1 + zMin,
			minY: structMinY + yMin,
			minZ: structMinZ + xMin,
			maxX: structMinX + zMin,
			maxY: structMinY + yMax - 1 + yMin,
			maxZ: structMinZ + xMax - 1 + xMin,
		}
	case cube.East:
		return structureBB{
			minX: structMinX + zMin,
			minY: structMinY + yMin,
			minZ: structMinZ + xMin,
			maxX: structMinX + zMax - 1 + zMin,
			maxY: structMinY + yMax - 1 + yMin,
			maxZ: structMinZ + xMax - 1 + xMin,
		}
	default:
		return structureBB{
			minX: structMinX + xMin,
			minY: structMinY + yMin,
			minZ: structMinZ + zMin,
			maxX: structMinX + xMax - 1 + xMin,
			maxY: structMinY + yMax - 1 + yMin,
			maxZ: structMinZ + zMax - 1 + zMin,
		}
	}
}

func setBlockWorld(c *chunk.Chunk, chunkX, chunkZ, worldX, worldY, worldZ int, rid uint32) {
	if worldX>>4 != chunkX || worldZ>>4 != chunkZ {
		return
	}
	yy := int16(worldY)
	if yy < int16(c.Range().Min()) || yy > int16(c.Range().Max()) {
		return
	}
	c.SetBlock(uint8(worldX&15), yy, uint8(worldZ&15), 0, rid)
}

type bridgePiece interface {
	pieceBase() *bridgePieceBase
	buildComponent(start *netherBridgeStart, pieces []bridgePiece, r *mc112.Rand)
	addComponentParts(c *chunk.Chunk, chunkX, chunkZ int, sbb structureBB, r *mc112.Rand, g *Nether) bool
}

type bridgePieceBase struct {
	bb            structureBB
	facing        cube.Direction
	componentType int
}

func (p *bridgePieceBase) pieceBase() *bridgePieceBase { return p }

func (p *bridgePieceBase) getXWithOffset(x, z int) int {
	switch p.facing {
	case cube.North, cube.South:
		return p.bb.minX + x
	case cube.West:
		return p.bb.maxX - z
	case cube.East:
		return p.bb.minX + z
	default:
		return x
	}
}

func (p *bridgePieceBase) getYWithOffset(y int) int {
	return y + p.bb.minY
}

func (p *bridgePieceBase) getZWithOffset(x, z int) int {
	switch p.facing {
	case cube.North:
		return p.bb.maxZ - z
	case cube.South:
		return p.bb.minZ + z
	case cube.West, cube.East:
		return p.bb.minZ + x
	default:
		return z
	}
}

func (p *bridgePieceBase) setBlock(c *chunk.Chunk, chunkX, chunkZ int, sbb structureBB, x, y, z int, rid uint32) {
	wx := p.getXWithOffset(x, z)
	wy := p.getYWithOffset(y)
	wz := p.getZWithOffset(x, z)
	if sbb.isVecInside(wx, wy, wz) {
		setBlockWorld(c, chunkX, chunkZ, wx, wy, wz, rid)
	}
}

func (p *bridgePieceBase) blockRIDAt(c *chunk.Chunk, chunkX, chunkZ int, sbb structureBB, x, y, z int, airRID uint32) uint32 {
	wx := p.getXWithOffset(x, z)
	wy := p.getYWithOffset(y)
	wz := p.getZWithOffset(x, z)
	if !sbb.isVecInside(wx, wy, wz) {
		return airRID
	}
	if wx>>4 != chunkX || wz>>4 != chunkZ {
		return airRID
	}
	return c.Block(uint8(wx&15), int16(wy), uint8(wz&15), 0)
}

func (p *bridgePieceBase) fillWithAir(c *chunk.Chunk, chunkX, chunkZ int, sbb structureBB, minX, minY, minZ, maxX, maxY, maxZ int, airRID uint32) {
	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			for z := minZ; z <= maxZ; z++ {
				p.setBlock(c, chunkX, chunkZ, sbb, x, y, z, airRID)
			}
		}
	}
}

func (p *bridgePieceBase) fillWithBlocks(c *chunk.Chunk, chunkX, chunkZ int, sbb structureBB, minX, minY, minZ, maxX, maxY, maxZ int, boundaryRID, insideRID uint32, existingOnly bool, airRID uint32) {
	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			for z := minZ; z <= maxZ; z++ {
				if !existingOnly || p.blockRIDAt(c, chunkX, chunkZ, sbb, x, y, z, airRID) != airRID {
					if y != minY && y != maxY && x != minX && x != maxX && z != minZ && z != maxZ {
						p.setBlock(c, chunkX, chunkZ, sbb, x, y, z, insideRID)
					} else {
						p.setBlock(c, chunkX, chunkZ, sbb, x, y, z, boundaryRID)
					}
				}
			}
		}
	}
}

func (p *bridgePieceBase) replaceAirAndLiquidDownwards(c *chunk.Chunk, chunkX, chunkZ int, sbb structureBB, x, y, z int, rid uint32, g *Nether) {
	wx := p.getXWithOffset(x, z)
	wy := p.getYWithOffset(y)
	wz := p.getZWithOffset(x, z)
	if !sbb.isVecInside(wx, wy, wz) {
		return
	}
	if wx>>4 != chunkX || wz>>4 != chunkZ {
		return
	}
	for wy > 1 {
		cur := c.Block(uint8(wx&15), int16(wy), uint8(wz&15), 0)
		if cur != g.airRID && cur != g.lavaStillRID && cur != g.lavaFlowRID {
			break
		}
		c.SetBlock(uint8(wx&15), int16(wy), uint8(wz&15), 0, rid)
		wy--
	}
}

func findIntersecting(pieces []bridgePiece, bb structureBB) bridgePiece {
	for _, p := range pieces {
		if p.pieceBase().bb.intersects(bb) {
			return p
		}
	}
	return nil
}

func isAboveGround(bb structureBB) bool {
	return bb.minY > 10
}

// --- Fortress structure generation ---

func (g *Nether) applyNetherBridge(chunkX, chunkZ int, c *chunk.Chunk, r *mc112.Rand, simulate bool) {
	const scanRange = 8

	chunkMinX, chunkMinZ := chunkX<<4, chunkZ<<4
	chunkMaxX, chunkMaxZ := chunkMinX+15, chunkMinZ+15
	chunkBB := structureBB{minX: chunkMinX, minY: 0, minZ: chunkMinZ, maxX: chunkMaxX, maxY: 127, maxZ: chunkMaxZ}

	for sx := chunkX - scanRange; sx <= chunkX+scanRange; sx++ {
		for sz := chunkZ - scanRange; sz <= chunkZ+scanRange; sz++ {
			s := g.netherBridgeStructure(sx, sz)
			if s == nil {
				continue
			}
			if !s.bb.intersectsXZ(chunkMinX, chunkMinZ, chunkMaxX, chunkMaxZ) {
				continue
			}
			for _, p := range s.pieces {
				if !p.pieceBase().bb.intersects(chunkBB) {
					continue
				}
				if !simulate {
					p.addComponentParts(c, chunkX, chunkZ, chunkBB, r, g)
					continue
				}
				switch piece := p.(type) {
				case *netherBridgeCorridor:
					chest := piece.chest
					piece.addComponentParts(c, chunkX, chunkZ, chunkBB, r, g)
					piece.chest = chest
				case *netherBridgeCorridor2:
					chest := piece.chest
					piece.addComponentParts(c, chunkX, chunkZ, chunkBB, r, g)
					piece.chest = chest
				case *netherBridgeThrone:
					hasSpawner := piece.hasSpawner
					piece.addComponentParts(c, chunkX, chunkZ, chunkBB, r, g)
					piece.hasSpawner = hasSpawner
				default:
					p.addComponentParts(c, chunkX, chunkZ, chunkBB, r, g)
				}
			}
		}
	}
}

func (g *Nether) netherBridgeStructure(startChunkX, startChunkZ int) *netherBridgeStructure {
	pos := world.ChunkPos{int32(startChunkX), int32(startChunkZ)}
	if v, ok := g.bridgeCache.Load(pos); ok {
		if v == nil {
			return nil
		}
		if s, ok := v.(*netherBridgeStructure); ok {
			return s
		}
		return nil
	}

	if !g.canSpawnNetherBridgeAtCoords(startChunkX, startChunkZ) {
		g.bridgeCache.Store(pos, (*netherBridgeStructure)(nil))
		return nil
	}

	seed := int64(startChunkX)*g.mapGenJ ^ int64(startChunkZ)*g.mapGenK ^ g.seed
	r := mc112.NewRand(seed)
	_ = r.Int32()

	start := newNetherBridgeStart(r, startChunkX, startChunkZ, g)
	s := &netherBridgeStructure{
		start:  pos,
		bb:     start.bb,
		pieces: start.pieces,
	}
	g.bridgeCache.Store(pos, s)
	return s
}

func (g *Nether) canSpawnNetherBridgeAtCoords(chunkX, chunkZ int) bool {
	i := chunkX >> 4
	j := chunkZ >> 4
	r := mc112.NewRand(int64(i^(j<<4)) ^ g.seed)
	_ = r.Int32()
	if r.Intn(3) != 0 {
		return false
	}
	if chunkX != (i<<4)+4+int(r.Intn(8)) {
		return false
	}
	return chunkZ == (j<<4)+4+int(r.Intn(8))
}

// --- Piece selection ---

type bridgePieceKind uint8

const (
	bridgeStraight bridgePieceKind = iota
	bridgeCrossing3
	bridgeCrossing
	bridgeStairs
	bridgeThrone
	bridgeEntrance
	bridgeCorridor5
	bridgeCrossing2
	bridgeCorridor2
	bridgeCorridor
	bridgeCorridor3
	bridgeCorridor4
	bridgeNetherStalkRoom
	bridgeEnd
)

type bridgePieceWeight struct {
	kind          bridgePieceKind
	weight        int
	placeCount    int
	maxPlaceCount int
	allowInRow    bool
}

func (w *bridgePieceWeight) doPlace() bool {
	return w.maxPlaceCount == 0 || w.placeCount < w.maxPlaceCount
}

func (w *bridgePieceWeight) isValid() bool {
	return w.maxPlaceCount == 0 || w.placeCount < w.maxPlaceCount
}

var bridgePrimaryWeights = []bridgePieceWeight{
	{kind: bridgeStraight, weight: 30, maxPlaceCount: 0, allowInRow: true},
	{kind: bridgeCrossing3, weight: 10, maxPlaceCount: 4},
	{kind: bridgeCrossing, weight: 10, maxPlaceCount: 4},
	{kind: bridgeStairs, weight: 10, maxPlaceCount: 3},
	{kind: bridgeThrone, weight: 5, maxPlaceCount: 2},
	{kind: bridgeEntrance, weight: 5, maxPlaceCount: 1},
}

var bridgeSecondaryWeights = []bridgePieceWeight{
	{kind: bridgeCorridor5, weight: 25, maxPlaceCount: 0, allowInRow: true},
	{kind: bridgeCrossing2, weight: 15, maxPlaceCount: 5},
	{kind: bridgeCorridor2, weight: 5, maxPlaceCount: 10},
	{kind: bridgeCorridor, weight: 5, maxPlaceCount: 10},
	{kind: bridgeCorridor3, weight: 10, maxPlaceCount: 3, allowInRow: true},
	{kind: bridgeCorridor4, weight: 7, maxPlaceCount: 2},
	{kind: bridgeNetherStalkRoom, weight: 5, maxPlaceCount: 2},
}

type netherBridgeStart struct {
	base             *netherBridgeCrossing3
	primaryWeights   []bridgePieceWeight
	secondaryWeights []bridgePieceWeight
	lastPlaced       *bridgePieceWeight
	pendingChildren  []bridgePiece
	pieces           []bridgePiece
	bb               structureBB
}

func newNetherBridgeStart(r *mc112.Rand, chunkX, chunkZ int, g *Nether) *netherBridgeStart {
	startPiece := newBridgeCrossing3Start(r, (chunkX<<4)+2, (chunkZ<<4)+2, g)
	s := &netherBridgeStart{
		base:             startPiece,
		primaryWeights:   make([]bridgePieceWeight, len(bridgePrimaryWeights)),
		secondaryWeights: make([]bridgePieceWeight, len(bridgeSecondaryWeights)),
		pieces:           []bridgePiece{startPiece},
	}
	copy(s.primaryWeights, bridgePrimaryWeights)
	copy(s.secondaryWeights, bridgeSecondaryWeights)

	startPiece.buildComponent(s, s.pieces, r)
	for len(s.pendingChildren) > 0 {
		i := int(r.Intn(int32(len(s.pendingChildren))))
		p := s.pendingChildren[i]
		s.pendingChildren = append(s.pendingChildren[:i], s.pendingChildren[i+1:]...)
		p.buildComponent(s, s.pieces, r)
	}

	s.updateBoundingBox()
	s.setRandomHeight(r, 48, 70)
	return s
}

func (s *netherBridgeStart) updateBoundingBox() {
	if len(s.pieces) == 0 {
		return
	}
	bb := s.pieces[0].pieceBase().bb
	for _, p := range s.pieces[1:] {
		bb.expandTo(p.pieceBase().bb)
	}
	s.bb = bb
}

func (s *netherBridgeStart) setRandomHeight(r *mc112.Rand, minY, maxY int) {
	i := maxY - minY + 1 - s.bb.ySize()
	var j int
	if i > 1 {
		j = minY + int(r.Intn(int32(i)))
	} else {
		j = minY
	}
	k := j - s.bb.minY
	s.bb.offset(0, k, 0)
	for _, p := range s.pieces {
		p.pieceBase().bb.offset(0, k, 0)
	}
}

func (p *bridgePieceBase) getTotalWeight(weights []bridgePieceWeight) int {
	total := 0
	ok := false
	for _, w := range weights {
		if w.maxPlaceCount > 0 && w.placeCount < w.maxPlaceCount {
			ok = true
		}
		total += w.weight
	}
	if !ok {
		return -1
	}
	return total
}

func (p *bridgePieceBase) generatePiece(start *netherBridgeStart, weights *[]bridgePieceWeight, pieces []bridgePiece, r *mc112.Rand, x, y, z int, facing cube.Direction, depth int) bridgePiece {
	total := p.getTotalWeight(*weights)
	if total <= 0 || depth > 30 {
		return nil
	}

	for attempt := 0; attempt < 5; attempt++ {
		k := int(r.Intn(int32(total)))
		for i := range *weights {
			w := &(*weights)[i]
			k -= w.weight
			if k >= 0 {
				continue
			}
			if !w.doPlace() || (start.lastPlaced == w && !w.allowInRow) {
				break
			}
			piece := createBridgePiece(w.kind, pieces, r, x, y, z, facing, depth)
			if piece != nil {
				w.placeCount++
				start.lastPlaced = w
				if !w.isValid() {
					*weights = append((*weights)[:i], (*weights)[i+1:]...)
				}
				return piece
			}
		}
	}

	return createBridgePiece(bridgeEnd, pieces, r, x, y, z, facing, depth)
}

func (p *bridgePieceBase) generateAndAddPiece(start *netherBridgeStart, pieces []bridgePiece, r *mc112.Rand, x, y, z int, facing cube.Direction, depth int, useSecondary bool) bridgePiece {
	if math.Abs(float64(x-start.bb.minX)) > 112 || math.Abs(float64(z-start.bb.minZ)) > 112 {
		return createBridgePiece(bridgeEnd, pieces, r, x, y, z, facing, depth)
	}

	weights := &start.primaryWeights
	if useSecondary {
		weights = &start.secondaryWeights
	}

	piece := p.generatePiece(start, weights, start.pieces, r, x, y, z, facing, depth+1)
	if piece != nil {
		start.pieces = append(start.pieces, piece)
		start.pendingChildren = append(start.pendingChildren, piece)
	}
	return piece
}

func (p *bridgePieceBase) getNextComponentNormal(start *netherBridgeStart, pieces []bridgePiece, r *mc112.Rand, xOffset, yOffset int, useSecondary bool) bridgePiece {
	switch p.facing {
	case cube.North:
		return p.generateAndAddPiece(start, pieces, r, p.bb.minX+xOffset, p.bb.minY+yOffset, p.bb.minZ-1, p.facing, p.componentType, useSecondary)
	case cube.South:
		return p.generateAndAddPiece(start, pieces, r, p.bb.minX+xOffset, p.bb.minY+yOffset, p.bb.maxZ+1, p.facing, p.componentType, useSecondary)
	case cube.West:
		return p.generateAndAddPiece(start, pieces, r, p.bb.minX-1, p.bb.minY+yOffset, p.bb.minZ+xOffset, p.facing, p.componentType, useSecondary)
	case cube.East:
		return p.generateAndAddPiece(start, pieces, r, p.bb.maxX+1, p.bb.minY+yOffset, p.bb.minZ+xOffset, p.facing, p.componentType, useSecondary)
	default:
		return nil
	}
}

func (p *bridgePieceBase) getNextComponentX(start *netherBridgeStart, pieces []bridgePiece, r *mc112.Rand, xOffset, yOffset int, useSecondary bool) bridgePiece {
	switch p.facing {
	case cube.North, cube.South:
		return p.generateAndAddPiece(start, pieces, r, p.bb.minX-1, p.bb.minY+xOffset, p.bb.minZ+yOffset, cube.West, p.componentType, useSecondary)
	case cube.West, cube.East:
		return p.generateAndAddPiece(start, pieces, r, p.bb.minX+yOffset, p.bb.minY+xOffset, p.bb.minZ-1, cube.North, p.componentType, useSecondary)
	default:
		return nil
	}
}

func (p *bridgePieceBase) getNextComponentZ(start *netherBridgeStart, pieces []bridgePiece, r *mc112.Rand, xOffset, yOffset int, useSecondary bool) bridgePiece {
	switch p.facing {
	case cube.North, cube.South:
		return p.generateAndAddPiece(start, pieces, r, p.bb.maxX+1, p.bb.minY+xOffset, p.bb.minZ+yOffset, cube.East, p.componentType, useSecondary)
	case cube.West, cube.East:
		return p.generateAndAddPiece(start, pieces, r, p.bb.minX+yOffset, p.bb.minY+xOffset, p.bb.maxZ+1, cube.South, p.componentType, useSecondary)
	default:
		return nil
	}
}
