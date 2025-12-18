package overworld

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/mc112"
	mcbiome "github.com/df-mc/dragonfly/server/world/generator/advanced/overworld/internal/biome"
)

type scatteredKind uint8

const (
	scatteredNone scatteredKind = iota
	scatteredDesertPyramid
	scatteredJunglePyramid
	scatteredSwampHut
	scatteredIgloo
)

type structureBB struct {
	minX, minY, minZ int
	maxX, maxY, maxZ int
}

func (bb structureBB) intersectsXZ(minX, minZ, maxX, maxZ int) bool {
	return bb.maxX >= minX && bb.maxZ >= minZ && bb.minX <= maxX && bb.minZ <= maxZ
}

type scatteredStructure struct {
	start world.ChunkPos
	kind  scatteredKind
	rot   int
	y     int
	bb    structureBB
}

func (g *Overworld) applyScatteredStructures(chunkX, chunkZ int, c *chunk.Chunk) {
	const (
		maxDistance = 32
		minDistance = 8
	)

	cellX, cellZ := floorDiv(chunkX, maxDistance), floorDiv(chunkZ, maxDistance)
	chunkMinX, chunkMinZ := chunkX<<4, chunkZ<<4
	chunkMaxX, chunkMaxZ := chunkMinX+15, chunkMinZ+15

	for dx := -1; dx <= 1; dx++ {
		for dz := -1; dz <= 1; dz++ {
			startX, startZ := g.scatteredStartChunk(cellX+dx, cellZ+dz, maxDistance, minDistance)
			s := g.scatteredStructure(startX, startZ)
			if s == nil || s.kind == scatteredNone {
				continue
			}
			if !s.bb.intersectsXZ(chunkMinX, chunkMinZ, chunkMaxX, chunkMaxZ) {
				continue
			}
			g.placeScatteredStructure(chunkX, chunkZ, c, s)
		}
	}
}

func (g *Overworld) scatteredStructure(startChunkX, startChunkZ int) *scatteredStructure {
	pos := world.ChunkPos{int32(startChunkX), int32(startChunkZ)}
	if v, ok := g.scatteredCache.Load(pos); ok {
		if v == nil {
			return nil
		}
		if s, ok := v.(*scatteredStructure); ok {
			return s
		}
		return nil
	}

	// Determine structure biome at the centre of the start chunk.
	biomeID := g.biomeProvider.biomes(startChunkX*16+8, startChunkZ*16+8, 1, 1)[0]

	var kind scatteredKind
	switch mcbiome.ID(biomeID) {
	case mcbiome.Desert, mcbiome.DesertHills, mcbiome.DesertM:
		kind = scatteredDesertPyramid
	case mcbiome.Jungle, mcbiome.JungleHills, mcbiome.ModifiedJungle:
		kind = scatteredJunglePyramid
	case mcbiome.Swamp, mcbiome.SwampHills:
		kind = scatteredSwampHut
	case mcbiome.SnowyTundra, mcbiome.IceSpikes, mcbiome.SnowyTaiga, mcbiome.SnowyTaigaHills, mcbiome.SnowyTaigaMountains:
		kind = scatteredIgloo
	default:
		g.scatteredCache.Store(pos, (*scatteredStructure)(nil))
		return nil
	}

	// Structure random: MapGenStructure seeding (chunk coords + world seed).
	r := mc112.NewRand(int64(startChunkX)*341873128712 + int64(startChunkZ)*132897987541 + g.seed)
	rot := int(r.Intn(4))

	startX, startZ := startChunkX*16, startChunkZ*16
	y := g.scatteredGroundY(kind, rot, startX, startZ)

	bb := scatteredBoundingBox(kind, rot, startX, y, startZ)
	s := &scatteredStructure{start: pos, kind: kind, rot: rot, y: y, bb: bb}
	g.scatteredCache.Store(pos, s)
	return s
}

func (g *Overworld) scatteredStartChunk(cellX, cellZ, maxDistance, minDistance int) (startX, startZ int) {
	seed := int64(cellX)*341873128712 + int64(cellZ)*132897987541 + g.seed + 14357617
	r := mc112.NewRand(seed)
	startX = cellX*maxDistance + int(r.Intn(int32(maxDistance-minDistance)))
	startZ = cellZ*maxDistance + int(r.Intn(int32(maxDistance-minDistance)))
	return startX, startZ
}

func scatteredSize(kind scatteredKind) (w, h, l int) {
	switch kind {
	case scatteredDesertPyramid:
		return 21, 15, 21
	case scatteredJunglePyramid:
		return 12, 10, 15
	case scatteredSwampHut:
		return 7, 6, 9
	case scatteredIgloo:
		return 7, 5, 7
	default:
		return 1, 1, 1
	}
}

func scatteredBoundingBox(kind scatteredKind, rot, startX, startY, startZ int) structureBB {
	w, h, l := scatteredSize(kind)
	if rot&1 == 1 {
		w, l = l, w
	}
	return structureBB{
		minX: startX, minY: startY, minZ: startZ,
		maxX: startX + w - 1, maxY: startY + h - 1, maxZ: startZ + l - 1,
	}
}

func (g *Overworld) scatteredGroundY(kind scatteredKind, rot, startX, startZ int) int {
	// Approximation of Java's getAverageGroundLevel: sample the preview terrain height within the structure footprint.
	w, _, l := scatteredSize(kind)
	if rot&1 == 1 {
		w, l = l, w
	}
	if w <= 0 || l <= 0 {
		return javaSeaLevel
	}

	const sampleStep = 4

	xs := make([]int, 0, w/sampleStep+2)
	for x := 0; x < w; x += sampleStep {
		xs = append(xs, x)
	}
	if last := w - 1; xs[len(xs)-1] != last {
		xs = append(xs, last)
	}

	zs := make([]int, 0, l/sampleStep+2)
	for z := 0; z < l; z += sampleStep {
		zs = append(zs, z)
	}
	if last := l - 1; zs[len(zs)-1] != last {
		zs = append(zs, last)
	}

	cache := make(map[world.ChunkPos]*chunk.Chunk, 4)
	sum := 0
	count := 0
	for _, dx := range xs {
		for _, dz := range zs {
			y := g.previewSurfaceYCached(cache, startX+dx, startZ+dz)
			sum += y
			count++
		}
	}
	if count == 0 {
		return javaSeaLevel
	}
	y := sum / count

	switch kind {
	case scatteredSwampHut:
		// Swamp huts in Java tend to sit at/above sea level.
		if y < javaSeaLevel {
			y = javaSeaLevel
		}
	}
	return y
}

func (g *Overworld) previewSurfaceYCached(cache map[world.ChunkPos]*chunk.Chunk, worldX, worldZ int) int {
	chunkX, chunkZ := worldX>>4, worldZ>>4
	pos := world.ChunkPos{int32(chunkX), int32(chunkZ)}
	c, ok := cache[pos]
	if !ok {
		c = g.previewChunk(chunkX, chunkZ)
		cache[pos] = c
	}
	return int(c.HighestBlock(uint8(worldX&15), uint8(worldZ&15)))
}

type scatteredPlacer struct {
	g      *Overworld
	chunkX int
	chunkZ int
	c      *chunk.Chunk

	startX int
	startY int
	startZ int
	rot    int

	width  int
	length int
}

func (p *scatteredPlacer) set(x, y, z int, rid uint32) {
	wx, wz := scatteredWorldXZ(p.startX, p.startZ, p.width, p.length, p.rot, x, z)
	wy := p.startY + y
	if wx>>4 != p.chunkX || wz>>4 != p.chunkZ {
		return
	}
	yy := int16(wy)
	if yy < int16(p.c.Range().Min()) || yy > int16(p.c.Range().Max()) {
		return
	}
	p.c.SetBlock(uint8(wx&15), yy, uint8(wz&15), 0, rid)
}

func (p *scatteredPlacer) fill(x1, y1, z1, x2, y2, z2 int, rid uint32) {
	if x2 < x1 {
		x1, x2 = x2, x1
	}
	if y2 < y1 {
		y1, y2 = y2, y1
	}
	if z2 < z1 {
		z1, z2 = z2, z1
	}
	for y := y1; y <= y2; y++ {
		for x := x1; x <= x2; x++ {
			for z := z1; z <= z2; z++ {
				p.set(x, y, z, rid)
			}
		}
	}
}

func scatteredWorldXZ(startX, startZ, w, l, rot, x, z int) (worldX, worldZ int) {
	switch rot & 3 {
	case 1: // clockwise 90
		return startX + z, startZ + (w - 1 - x)
	case 2: // 180
		return startX + (w - 1 - x), startZ + (l - 1 - z)
	case 3: // counterclockwise 90
		return startX + (l - 1 - z), startZ + x
	default: // 0
		return startX + x, startZ + z
	}
}

func (g *Overworld) placeScatteredStructure(chunkX, chunkZ int, c *chunk.Chunk, s *scatteredStructure) {
	if s == nil || s.kind == scatteredNone {
		return
	}
	startChunkX, startChunkZ := int(s.start[0]), int(s.start[1])
	startX, startZ := startChunkX*16, startChunkZ*16
	w, _, l := scatteredSize(s.kind)
	p := &scatteredPlacer{
		g:      g,
		chunkX: chunkX,
		chunkZ: chunkZ,
		c:      c,
		startX: startX,
		startY: s.y,
		startZ: startZ,
		rot:    s.rot,
		width:  w,
		length: l,
	}

	switch s.kind {
	case scatteredDesertPyramid:
		g.placeDesertPyramid(p)
	case scatteredJunglePyramid:
		g.placeJunglePyramid(p)
	case scatteredSwampHut:
		g.placeSwampHut(p)
	case scatteredIgloo:
		g.placeIgloo(p)
	}
}

func (g *Overworld) placeDesertPyramid(p *scatteredPlacer) {
	sandstone := g.sandstoneRID
	air := g.airRID

	orangeTerracotta := world.BlockRuntimeID(block.StainedTerracotta{Colour: item.ColourOrange()})
	blueTerracotta := world.BlockRuntimeID(block.StainedTerracotta{Colour: item.ColourBlue()})
	tnt := world.BlockRuntimeID(block.TNT{})
	chestRID := world.BlockRuntimeID(block.Chest{Facing: cube.North})

	// Foundation.
	p.fill(0, -3, 0, 20, 0, 20, sandstone)
	// Central foundation for a small hidden chamber.
	p.fill(7, -8, 7, 13, -4, 13, sandstone)

	// Outer walls.
	for y := 1; y <= 10; y++ {
		for x := 0; x <= 20; x++ {
			p.set(x, y, 0, sandstone)
			p.set(x, y, 20, sandstone)
		}
		for z := 0; z <= 20; z++ {
			p.set(0, y, z, sandstone)
			p.set(20, y, z, sandstone)
		}
	}

	// Interior.
	p.fill(1, 1, 1, 19, 9, 19, air)

	// Stepped roof.
	p.fill(0, 11, 0, 20, 11, 20, sandstone)
	p.fill(1, 12, 1, 19, 12, 19, sandstone)
	p.fill(2, 13, 2, 18, 13, 18, sandstone)
	p.fill(3, 14, 3, 17, 14, 17, sandstone)

	// Entrance (local north side).
	p.fill(9, 1, 0, 11, 3, 1, air)

	// Main room floor pattern.
	p.fill(9, 0, 9, 11, 0, 11, orangeTerracotta)
	p.set(10, 0, 10, blueTerracotta)

	// Hidden chamber (very simplified).
	p.fill(8, -7, 8, 12, -5, 12, air)
	p.set(10, -7, 10, tnt)
	p.set(8, -7, 8, chestRID)
	p.set(12, -7, 8, chestRID)
	p.set(8, -7, 12, chestRID)
	p.set(12, -7, 12, chestRID)
}

func (g *Overworld) placeJunglePyramid(p *scatteredPlacer) {
	air := g.airRID

	cobble := world.BlockRuntimeID(block.Cobblestone{})
	mossy := world.BlockRuntimeID(block.Cobblestone{Mossy: true})

	// Foundation.
	p.fill(0, -2, 0, 11, 0, 14, cobble)

	// Walls + roof.
	for y := 1; y <= 6; y++ {
		for x := 0; x <= 11; x++ {
			rid := cobble
			if ((x*13 + y*7) % 5) == 0 {
				rid = mossy
			}
			p.set(x, y, 0, rid)
			p.set(x, y, 14, rid)
		}
		for z := 0; z <= 14; z++ {
			rid := cobble
			if ((z*11 + y*9) % 5) == 0 {
				rid = mossy
			}
			p.set(0, y, z, rid)
			p.set(11, y, z, rid)
		}
	}
	p.fill(0, 7, 0, 11, 7, 14, cobble)

	// Interior.
	p.fill(1, 1, 1, 10, 6, 13, air)

	// Entrance (local north side).
	p.fill(5, 1, 0, 6, 3, 1, air)
}

func (g *Overworld) placeSwampHut(p *scatteredPlacer) {
	air := g.airRID

	planks := world.BlockRuntimeID(block.Planks{Wood: block.SpruceWood()})
	log := world.BlockRuntimeID(block.Log{Wood: block.SpruceWood(), Axis: cube.Y})

	// Stilts.
	for _, corner := range [][2]int{{0, 0}, {0, 8}, {6, 0}, {6, 8}} {
		x, z := corner[0], corner[1]
		p.fill(x, -4, z, x, 0, z, log)
	}

	// Floor.
	p.fill(0, 1, 0, 6, 1, 8, planks)

	// Walls.
	for y := 2; y <= 4; y++ {
		for x := 0; x <= 6; x++ {
			p.set(x, y, 0, planks)
			p.set(x, y, 8, planks)
		}
		for z := 0; z <= 8; z++ {
			p.set(0, y, z, planks)
			p.set(6, y, z, planks)
		}
	}

	// Interior.
	p.fill(1, 2, 1, 5, 4, 7, air)

	// Entrance.
	p.fill(3, 2, 0, 3, 3, 0, air)

	// Roof.
	p.fill(0, 5, 0, 6, 5, 8, planks)
}

func (g *Overworld) placeIgloo(p *scatteredPlacer) {
	air := g.airRID

	snow := world.BlockRuntimeID(block.Snow{})
	packedIce := world.BlockRuntimeID(block.PackedIce{})

	// Foundation + floor.
	p.fill(0, -1, 0, 6, 0, 6, snow)

	// Walls.
	for y := 1; y <= 3; y++ {
		for x := 0; x <= 6; x++ {
			p.set(x, y, 0, snow)
			p.set(x, y, 6, snow)
		}
		for z := 0; z <= 6; z++ {
			p.set(0, y, z, snow)
			p.set(6, y, z, snow)
		}
	}

	// Interior.
	p.fill(1, 1, 1, 5, 3, 5, air)

	// Roof (simple cap).
	p.fill(1, 4, 1, 5, 4, 5, snow)

	// Entrance.
	p.fill(3, 1, 0, 3, 2, 0, air)

	// Window.
	p.set(3, 2, 6, packedIce)
}

func floorDiv(x, d int) int {
	if x >= 0 {
		return x / d
	}
	return -((-x + d - 1) / d)
}
