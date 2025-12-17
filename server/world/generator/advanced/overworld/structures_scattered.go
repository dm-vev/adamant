package overworld

import (
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
			// TODO: Place scattered structures (desert/jungle temples, witch hut, igloo) 1:1.
			_ = c
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

	// TODO: Match per-structure ground level logic precisely (average ground level + offsets).
	startX, startZ := startChunkX*16, startChunkZ*16
	y := g.previewSurfaceY(startX+8, startZ+8)

	bb := scatteredBoundingBox(kind, startX, y, startZ)
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

func scatteredBoundingBox(kind scatteredKind, startX, startY, startZ int) structureBB {
	// Placeholder sizes (block placement TODO).
	// Most scattered features are aligned to the start chunk origin in Java.
	var (
		w, h, l int
	)
	switch kind {
	case scatteredDesertPyramid:
		w, h, l = 21, 15, 21
	case scatteredJunglePyramid:
		w, h, l = 12, 10, 15
	case scatteredSwampHut:
		w, h, l = 7, 6, 9
	case scatteredIgloo:
		w, h, l = 7, 5, 7
	default:
		w, h, l = 1, 1, 1
	}
	return structureBB{
		minX: startX, minY: startY, minZ: startZ,
		maxX: startX + w - 1, maxY: startY + h - 1, maxZ: startZ + l - 1,
	}
}

func floorDiv(x, d int) int {
	if x >= 0 {
		return x / d
	}
	return -((-x + d - 1) / d)
}
