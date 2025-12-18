package overworld

import (
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/mc112"
	mcbiome "github.com/df-mc/dragonfly/server/world/generator/advanced/overworld/internal/biome"
)

// This is a minimal port of the Java 1.12 MapGenMineshaft start logic and a simplified block layout.
// It prioritises correct seeding/spawn positions, but piece layout is not yet a full 1:1 port.

type mineshaftType uint8

const (
	mineshaftNormal mineshaftType = iota
	mineshaftMesa
)

type mineshaftComponentKind uint8

const (
	mineshaftRoom mineshaftComponentKind = iota
	mineshaftCorridor
)

type mineshaftComponent struct {
	kind     mineshaftComponentKind
	bb       structureBB
	rotation int // 0..3, used for corridors
	rails    bool
	mType    mineshaftType
}

type mineshaftStructure struct {
	start      world.ChunkPos
	mType      mineshaftType
	bb         structureBB
	components []mineshaftComponent
}

func (g *Overworld) applyMineshafts(chunkX, chunkZ int, c *chunk.Chunk) {
	// Matches MapGenBase.range (8).
	const scanRange = 8

	chunkMinX, chunkMinZ := chunkX<<4, chunkZ<<4
	chunkMaxX, chunkMaxZ := chunkMinX+15, chunkMinZ+15

	for sx := chunkX - scanRange; sx <= chunkX+scanRange; sx++ {
		for sz := chunkZ - scanRange; sz <= chunkZ+scanRange; sz++ {
			s := g.mineshaftStructure(sx, sz)
			if s == nil {
				continue
			}
			if !s.bb.intersectsXZ(chunkMinX, chunkMinZ, chunkMaxX, chunkMaxZ) {
				continue
			}
			g.placeMineshaft(chunkX, chunkZ, c, s)
		}
	}
}

func (g *Overworld) mineshaftStructure(startChunkX, startChunkZ int) *mineshaftStructure {
	pos := world.ChunkPos{int32(startChunkX), int32(startChunkZ)}
	if v, ok := g.mineshaftCache.Load(pos); ok {
		if v == nil {
			return nil
		}
		if s, ok := v.(*mineshaftStructure); ok {
			return s
		}
		return nil
	}

	// MapGenBase seed: rand.setSeed((long)chunkX*j ^ (long)chunkZ*k ^ worldSeed)
	seed := int64(startChunkX)*g.mapGenJ ^ int64(startChunkZ)*g.mapGenK ^ g.seed
	r := mc112.NewRand(seed)
	_ = r.Int32() // MapGenStructure.recursiveGenerate consumes one nextInt().

	// MapGenMineshaft.canSpawnStructureAtCoords
	const chance = 0.004
	if r.Float64() >= chance {
		g.mineshaftCache.Store(pos, (*mineshaftStructure)(nil))
		return nil
	}
	maxAbs := int32(mineshaftMaxInt(mineshaftAbsInt(startChunkX), mineshaftAbsInt(startChunkZ)))
	if r.Intn(80) >= maxAbs {
		g.mineshaftCache.Store(pos, (*mineshaftStructure)(nil))
		return nil
	}

	biomeID := g.biomeProvider.biomes(startChunkX*16+8, startChunkZ*16+8, 1, 1)[0]
	mType := mineshaftNormal
	if mcbiome.IsMesa(biomeID) {
		mType = mineshaftMesa
	}

	s := g.buildMineshaftStart(startChunkX, startChunkZ, mType, r)
	g.mineshaftCache.Store(pos, s)
	return s
}

func (g *Overworld) buildMineshaftStart(startChunkX, startChunkZ int, mType mineshaftType, r *mc112.Rand) *mineshaftStructure {
	// Room bounding box matches StructureMineshaftPieces.Room constructor (Java 1.12):
	// new StructureBoundingBox(x, 50, z, x+7+rand.nextInt(6), 54+rand.nextInt(6), z+7+rand.nextInt(6))
	startX := startChunkX*16 + 2
	startZ := startChunkZ*16 + 2
	minY := 50
	maxX := startX + 7 + int(r.Intn(6))
	maxY := 54 + int(r.Intn(6))
	maxZ := startZ + 7 + int(r.Intn(6))

	roomBB := structureBB{minX: startX, minY: minY, minZ: startZ, maxX: maxX, maxY: maxY, maxZ: maxZ}

	components := []mineshaftComponent{{kind: mineshaftRoom, bb: roomBB, mType: mType}}

	// markAvailableHeight(world, rand, 10) with vanilla sea level 63.
	roomBB, components = mineshaftMarkAvailableHeight(roomBB, components, r)
	components[0].bb = roomBB

	// Simplified: spawn a few corridors from the room, using the same loop structure as Java.
	ySize := roomBB.maxY - roomBB.minY + 1
	j := ySize - 3 - 1
	if j <= 0 {
		j = 1
	}
	xSize := roomBB.maxX - roomBB.minX + 1
	zSize := roomBB.maxZ - roomBB.minZ + 1

	baseMinX, baseMinZ := roomBB.minX, roomBB.minZ
	depth := 0

	for k := 0; k < xSize; k += 4 {
		k += int(r.Intn(int32(xSize)))
		if k+3 > xSize {
			break
		}
		y := roomBB.minY + int(r.Intn(int32(j))) + 1
		components = g.tryAddCorridor(components, r, baseMinX, baseMinZ, depth+1, roomBB.minX+k, y, roomBB.minZ-1, 0, mType)
	}
	for k := 0; k < xSize; k += 4 {
		k += int(r.Intn(int32(xSize)))
		if k+3 > xSize {
			break
		}
		y := roomBB.minY + int(r.Intn(int32(j))) + 1
		components = g.tryAddCorridor(components, r, baseMinX, baseMinZ, depth+1, roomBB.minX+k, y, roomBB.maxZ+1, 2, mType)
	}
	for k := 0; k < zSize; k += 4 {
		k += int(r.Intn(int32(zSize)))
		if k+3 > zSize {
			break
		}
		y := roomBB.minY + int(r.Intn(int32(j))) + 1
		components = g.tryAddCorridor(components, r, baseMinX, baseMinZ, depth+1, roomBB.minX-1, y, roomBB.minZ+k, 3, mType)
	}
	for k := 0; k < zSize; k += 4 {
		k += int(r.Intn(int32(zSize)))
		if k+3 > zSize {
			break
		}
		y := roomBB.minY + int(r.Intn(int32(j))) + 1
		components = g.tryAddCorridor(components, r, baseMinX, baseMinZ, depth+1, roomBB.maxX+1, y, roomBB.minZ+k, 1, mType)
	}

	union := roomBB
	for _, comp := range components[1:] {
		union = bbUnion(union, comp.bb)
	}

	return &mineshaftStructure{
		start:      world.ChunkPos{int32(startChunkX), int32(startChunkZ)},
		mType:      mType,
		bb:         union,
		components: components,
	}
}

func mineshaftMarkAvailableHeight(roomBB structureBB, comps []mineshaftComponent, r *mc112.Rand) (structureBB, []mineshaftComponent) {
	// StructureStart.markAvailableHeight with sea level 63 and p=10.
	seaLevel := javaSeaLevel
	i := seaLevel - 10
	ySize := roomBB.maxY - roomBB.minY + 1
	j := ySize + 1
	if j < i {
		j += int(r.Intn(int32(i - j)))
	}
	k := j - roomBB.maxY
	roomBB = bbOffset(roomBB, 0, k, 0)
	for i := range comps {
		comps[i].bb = bbOffset(comps[i].bb, 0, k, 0)
	}
	return roomBB, comps
}

func (g *Overworld) tryAddCorridor(comps []mineshaftComponent, r *mc112.Rand, baseMinX, baseMinZ, depth, x, y, z, rot int, mType mineshaftType) []mineshaftComponent {
	// Similar limits to generateAndAddPiece: depth<=8 and within 80 blocks.
	if depth > 8 {
		return comps
	}
	if mineshaftAbsInt(x-baseMinX) > 80 || mineshaftAbsInt(z-baseMinZ) > 80 {
		return comps
	}

	corr := g.newCorridor(r, comps, x, y, z, rot, mType)
	if corr == nil {
		return comps
	}
	comps = append(comps, *corr)

	// Small amount of branching to keep it from being just spokes.
	if depth < 5 && r.Intn(3) == 0 {
		endX, endZ := corridorEnd(corr.bb, rot)
		nextRot := int(r.Intn(4))
		comps = g.tryAddCorridor(comps, r, baseMinX, baseMinZ, depth+1, endX, y, endZ, nextRot, mType)
	}
	return comps
}

func (g *Overworld) newCorridor(r *mc112.Rand, comps []mineshaftComponent, x, y, z, rot int, mType mineshaftType) *mineshaftComponent {
	// Rough equivalent of Corridor.findCorridorSize: 2..4 sections, each 5 blocks.
	sections := int(r.Intn(3)) + 2
	length := sections * 5

	bb := structureBB{minX: x, minY: y, minZ: z, maxX: x, maxY: y + 2, maxZ: z}
	switch rot & 3 {
	case 0: // north
		bb.minX = x
		bb.minZ = z - (length - 1)
		bb.maxX = x + 2
		bb.maxZ = z
	case 2: // south
		bb.minX = x
		bb.minZ = z
		bb.maxX = x + 2
		bb.maxZ = z + (length - 1)
	case 3: // west
		bb.minX = x - (length - 1)
		bb.minZ = z
		bb.maxX = x
		bb.maxZ = z + 2
	case 1: // east
		bb.minX = x
		bb.minZ = z
		bb.maxX = x + (length - 1)
		bb.maxZ = z + 2
	}
	if bb.minY < 1 || bb.maxY > 255 {
		return nil
	}
	for _, c := range comps {
		if bbIntersects(bb, c.bb) {
			return nil
		}
	}

	return &mineshaftComponent{
		kind:     mineshaftCorridor,
		bb:       bb,
		rotation: rot & 3,
		rails:    r.Intn(3) == 0,
		mType:    mType,
	}
}

func (g *Overworld) placeMineshaft(chunkX, chunkZ int, c *chunk.Chunk, s *mineshaftStructure) {
	for _, comp := range s.components {
		switch comp.kind {
		case mineshaftRoom:
			g.placeMineshaftRoom(chunkX, chunkZ, c, comp)
		case mineshaftCorridor:
			g.placeMineshaftCorridor(chunkX, chunkZ, c, comp)
		}
	}
}

func (g *Overworld) placeMineshaftRoom(chunkX, chunkZ int, c *chunk.Chunk, comp mineshaftComponent) {
	// Simplified: carve a room and add a dirt floor.
	floorRID := g.dirtRID
	for x := comp.bb.minX; x <= comp.bb.maxX; x++ {
		for z := comp.bb.minZ; z <= comp.bb.maxZ; z++ {
			g.setRIDIfInChunk(c, chunkX, chunkZ, x, comp.bb.minY, z, floorRID)
			for y := comp.bb.minY + 1; y <= comp.bb.maxY; y++ {
				g.setRIDIfInChunk(c, chunkX, chunkZ, x, y, z, g.airRID)
			}
		}
	}
}

func (g *Overworld) placeMineshaftCorridor(chunkX, chunkZ int, c *chunk.Chunk, comp mineshaftComponent) {
	planksRID, fenceRID := g.oakPlanksRID, g.oakFenceRID
	if comp.mType == mineshaftMesa {
		planksRID, fenceRID = g.darkOakPlanksRID, g.darkOakFenceRID
	}

	// Carve air.
	for x := comp.bb.minX; x <= comp.bb.maxX; x++ {
		for z := comp.bb.minZ; z <= comp.bb.maxZ; z++ {
			for y := comp.bb.minY; y <= comp.bb.maxY; y++ {
				g.setRIDIfInChunk(c, chunkX, chunkZ, x, y, z, g.airRID)
			}
		}
	}

	// Supports every 5 blocks.
	length := corridorLength(comp.bb, comp.rotation)
	for t := 0; t < length; t++ {
		if t%5 != 0 {
			continue
		}
		sx, sz := corridorSliceOrigin(comp.bb, comp.rotation, t)
		if comp.rotation&1 == 0 {
			// corridor runs north/south: width on X (sx..sx+2).
			g.setRIDIfInChunk(c, chunkX, chunkZ, sx+0, comp.bb.minY+0, sz, fenceRID)
			g.setRIDIfInChunk(c, chunkX, chunkZ, sx+0, comp.bb.minY+1, sz, fenceRID)
			g.setRIDIfInChunk(c, chunkX, chunkZ, sx+2, comp.bb.minY+0, sz, fenceRID)
			g.setRIDIfInChunk(c, chunkX, chunkZ, sx+2, comp.bb.minY+1, sz, fenceRID)
			g.setRIDIfInChunk(c, chunkX, chunkZ, sx+0, comp.bb.minY+2, sz, planksRID)
			g.setRIDIfInChunk(c, chunkX, chunkZ, sx+1, comp.bb.minY+2, sz, planksRID)
			g.setRIDIfInChunk(c, chunkX, chunkZ, sx+2, comp.bb.minY+2, sz, planksRID)
		} else {
			// corridor runs east/west: width on Z (sz..sz+2).
			g.setRIDIfInChunk(c, chunkX, chunkZ, sx, comp.bb.minY+0, sz+0, fenceRID)
			g.setRIDIfInChunk(c, chunkX, chunkZ, sx, comp.bb.minY+1, sz+0, fenceRID)
			g.setRIDIfInChunk(c, chunkX, chunkZ, sx, comp.bb.minY+0, sz+2, fenceRID)
			g.setRIDIfInChunk(c, chunkX, chunkZ, sx, comp.bb.minY+1, sz+2, fenceRID)
			g.setRIDIfInChunk(c, chunkX, chunkZ, sx, comp.bb.minY+2, sz+0, planksRID)
			g.setRIDIfInChunk(c, chunkX, chunkZ, sx, comp.bb.minY+2, sz+1, planksRID)
			g.setRIDIfInChunk(c, chunkX, chunkZ, sx, comp.bb.minY+2, sz+2, planksRID)
		}
	}

	// Best-effort rails through the centre.
	if comp.rails && g.railRID != 0 {
		for t := 0; t < length; t++ {
			wx, wz := corridorCentre(comp.bb, comp.rotation, t)
			g.setRIDIfInChunk(c, chunkX, chunkZ, wx, comp.bb.minY, wz, g.railRID)
		}
	}
}

func corridorLength(bb structureBB, rot int) int {
	if rot&1 == 0 {
		return bb.maxZ - bb.minZ + 1
	}
	return bb.maxX - bb.minX + 1
}

func corridorEnd(bb structureBB, rot int) (x, z int) {
	switch rot & 3 {
	case 0:
		return bb.minX, bb.minZ
	case 2:
		return bb.minX, bb.maxZ
	case 3:
		return bb.minX, bb.minZ
	case 1:
		return bb.maxX, bb.minZ
	default:
		return bb.minX, bb.minZ
	}
}

func corridorSliceOrigin(bb structureBB, rot, t int) (x, z int) {
	switch rot & 3 {
	case 0: // north: t increases south->north
		return bb.minX, bb.maxZ - t
	case 2: // south
		return bb.minX, bb.minZ + t
	case 3: // west
		return bb.maxX - t, bb.minZ
	case 1: // east
		return bb.minX + t, bb.minZ
	default:
		return bb.minX, bb.minZ
	}
}

func corridorCentre(bb structureBB, rot, t int) (x, z int) {
	sx, sz := corridorSliceOrigin(bb, rot, t)
	if rot&1 == 0 {
		return sx + 1, sz
	}
	return sx, sz + 1
}

func bbIntersects(a, b structureBB) bool {
	return a.maxX >= b.minX && a.minX <= b.maxX &&
		a.maxY >= b.minY && a.minY <= b.maxY &&
		a.maxZ >= b.minZ && a.minZ <= b.maxZ
}

func bbUnion(a, b structureBB) structureBB {
	return structureBB{
		minX: mineshaftMinInt(a.minX, b.minX),
		minY: mineshaftMinInt(a.minY, b.minY),
		minZ: mineshaftMinInt(a.minZ, b.minZ),
		maxX: mineshaftMaxInt(a.maxX, b.maxX),
		maxY: mineshaftMaxInt(a.maxY, b.maxY),
		maxZ: mineshaftMaxInt(a.maxZ, b.maxZ),
	}
}

func bbOffset(bb structureBB, dx, dy, dz int) structureBB {
	return structureBB{
		minX: bb.minX + dx, minY: bb.minY + dy, minZ: bb.minZ + dz,
		maxX: bb.maxX + dx, maxY: bb.maxY + dy, maxZ: bb.maxZ + dz,
	}
}

func mineshaftAbsInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func mineshaftMinInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func mineshaftMaxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
