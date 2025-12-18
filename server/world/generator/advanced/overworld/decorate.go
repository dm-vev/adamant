package overworld

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/mc112"
	mcbiome "github.com/df-mc/dragonfly/server/world/generator/advanced/overworld/internal/biome"
)

type decoratorSettings struct {
	treesPerChunk    int
	extraTreeChance  float64
	flowersPerChunk  int
	grassPerChunk    int
	deadBushPerChunk int
	reedsPerChunk    int
	cactiPerChunk    int

	sunflowers bool
}

func decoratorSettingsForBiome(biomeID int) decoratorSettings {
	switch mcbiome.ID(biomeID) {
	case mcbiome.Plains:
		return decoratorSettings{treesPerChunk: 0, extraTreeChance: 0.05, flowersPerChunk: 4, grassPerChunk: 10}
	case mcbiome.SunflowerPlains:
		return decoratorSettings{treesPerChunk: 0, extraTreeChance: 0.05, flowersPerChunk: 4, grassPerChunk: 10, sunflowers: true}

	case mcbiome.Forest:
		return decoratorSettings{treesPerChunk: 10, extraTreeChance: 0.0, flowersPerChunk: 2, grassPerChunk: 2}
	case mcbiome.FlowerForest:
		return decoratorSettings{treesPerChunk: 6, extraTreeChance: 0.0, flowersPerChunk: 100, grassPerChunk: 1}
	case mcbiome.BirchForest, mcbiome.BirchForestHills, mcbiome.TallBirchForest, mcbiome.TallBirchHills:
		return decoratorSettings{treesPerChunk: 10, extraTreeChance: 0.0, flowersPerChunk: 2, grassPerChunk: 2}
	case mcbiome.DarkForest, mcbiome.DarkForestHills:
		return decoratorSettings{treesPerChunk: 20, extraTreeChance: 0.0, flowersPerChunk: 1, grassPerChunk: 2}

	case mcbiome.Taiga, mcbiome.TaigaHills, mcbiome.TaigaMountains,
		mcbiome.SnowyTaiga, mcbiome.SnowyTaigaHills, mcbiome.SnowyTaigaMountains:
		return decoratorSettings{treesPerChunk: 10, extraTreeChance: 0.0, flowersPerChunk: 1, grassPerChunk: 1}
	case mcbiome.GiantTreeTaiga, mcbiome.GiantTreeTaigaHills, mcbiome.GiantSpruceTaiga, mcbiome.GiantSpruceTaigaHills:
		return decoratorSettings{treesPerChunk: 10, extraTreeChance: 0.0, flowersPerChunk: 1, grassPerChunk: 7, deadBushPerChunk: 1}

	case mcbiome.Jungle, mcbiome.JungleHills, mcbiome.ModifiedJungle:
		return decoratorSettings{treesPerChunk: 50, extraTreeChance: 0.0, flowersPerChunk: 4, grassPerChunk: 25}
	case mcbiome.JungleEdge, mcbiome.ModifiedJungleEdge:
		return decoratorSettings{treesPerChunk: 2, extraTreeChance: 0.0, flowersPerChunk: 4, grassPerChunk: 25}

	case mcbiome.Savanna, mcbiome.SavannaPlateau, mcbiome.ShatteredSavanna, mcbiome.ShatteredSavannaPlateau:
		return decoratorSettings{treesPerChunk: 1, extraTreeChance: 0.1, flowersPerChunk: 4, grassPerChunk: 20}

	case mcbiome.Swamp, mcbiome.SwampHills:
		return decoratorSettings{treesPerChunk: 2, extraTreeChance: 0.0, flowersPerChunk: 1, grassPerChunk: 5, deadBushPerChunk: 1, reedsPerChunk: 10}

	case mcbiome.Desert, mcbiome.DesertHills, mcbiome.DesertM:
		return decoratorSettings{treesPerChunk: 0, extraTreeChance: 0.0, flowersPerChunk: 0, grassPerChunk: 0, deadBushPerChunk: 2, reedsPerChunk: 50, cactiPerChunk: 10}

	default:
		// Vanilla BiomeDecorator defaults.
		return decoratorSettings{treesPerChunk: 0, extraTreeChance: 0.1, flowersPerChunk: 2, grassPerChunk: 1}
	}
}

func (g *Overworld) decorate(chunkX, chunkZ int, c *chunk.Chunk) {
	// Vanilla uses offsets (rand.nextInt(16)+8), which makes decoration spill into neighbouring chunks. To avoid
	// clipped trees/foliage when we can only write to a single chunk here, we simulate decoration for the 2x2
	// area (this chunk + west/north/northwest), and only apply blocks that land inside the current chunk.
	chunkMinX, chunkMinZ := chunkX<<4, chunkZ<<4
	chunkMaxX, chunkMaxZ := chunkMinX+15, chunkMinZ+15

	preview := make(map[world.ChunkPos]*chunk.Chunk, 9)

	for dx := 0; dx >= -1; dx-- {
		for dz := 0; dz >= -1; dz-- {
			originChunkX, originChunkZ := chunkX+dx, chunkZ+dz
			r := g.chunkPopulationRand(originChunkX, originChunkZ)
			biomeID := g.biomeProvider.biomes(originChunkX*16+16, originChunkZ*16+16, 1, 1)[0]
			s := decoratorSettingsForBiome(biomeID)
			g.decorateOrigin(c, preview, chunkX, chunkZ, chunkMinX, chunkMinZ, chunkMaxX, chunkMaxZ, originChunkX, originChunkZ, biomeID, r, s)
		}
	}
}

func (g *Overworld) decorateOrigin(
	c *chunk.Chunk,
	preview map[world.ChunkPos]*chunk.Chunk,
	chunkX, chunkZ int,
	chunkMinX, chunkMinZ, chunkMaxX, chunkMaxZ int,
	originChunkX, originChunkZ int,
	biomeID int,
	r *mc112.Rand,
	s decoratorSettings,
) {
	originX, originZ := originChunkX<<4, originChunkZ<<4

	treeCount := s.treesPerChunk
	if treeCount < 0 {
		treeCount = 0
	}
	if s.extraTreeChance > 0 && r.Float64() < s.extraTreeChance {
		treeCount++
	}

	const treeRadius = 4
	for i := 0; i < treeCount; i++ {
		wx := originX + int(r.Intn(16)) + 8
		wz := originZ + int(r.Intn(16)) + 8
		if wx < chunkMinX-treeRadius || wx > chunkMaxX+treeRadius || wz < chunkMinZ-treeRadius || wz > chunkMaxZ+treeRadius {
			continue
		}
		baseY := g.surfaceYAt(c, preview, chunkX, chunkZ, wx, wz)
		if baseY <= 0 || baseY >= 255 {
			continue
		}
		startY := baseY + 1
		kind := g.pickTreeKind(biomeID, r)
		switch kind {
		case treeOak:
			if r.Intn(10) == 0 {
				g.genBigOak(c, preview, chunkX, chunkZ, wx, startY, wz, r)
			} else {
				g.genSimpleTree(c, preview, chunkX, chunkZ, wx, startY, wz, r, g.oakLogRID, g.oakLeavesRID)
			}
		case treeBirch:
			g.genSimpleTree(c, preview, chunkX, chunkZ, wx, startY, wz, r, g.birchLogRID, g.birchLeavesRID)
		case treeSpruce:
			g.genSpruce(c, preview, chunkX, chunkZ, wx, startY, wz, r)
		case treeJungle:
			g.genJungle(c, preview, chunkX, chunkZ, wx, startY, wz, r)
		case treeAcacia:
			g.genAcacia(c, preview, chunkX, chunkZ, wx, startY, wz, r)
		case treeDarkOak:
			g.genDarkOak(c, preview, chunkX, chunkZ, wx, startY, wz, r)
		}
	}

	for i := 0; i < s.flowersPerChunk; i++ {
		wx := originX + int(r.Intn(16)) + 8
		wz := originZ + int(r.Intn(16)) + 8
		if wx < chunkMinX || wx > chunkMaxX || wz < chunkMinZ || wz > chunkMaxZ {
			continue
		}
		topY := g.surfaceYAt(c, preview, chunkX, chunkZ, wx, wz)
		limit := topY + 32
		if limit <= 0 {
			continue
		}
		wy := int(r.Intn(int32(limit)))
		g.placeFlower(c, preview, chunkX, chunkZ, wx, wy, wz, biomeID, r)
	}

	for i := 0; i < s.grassPerChunk; i++ {
		wx := originX + int(r.Intn(16)) + 8
		wz := originZ + int(r.Intn(16)) + 8
		if wx < chunkMinX || wx > chunkMaxX || wz < chunkMinZ || wz > chunkMaxZ {
			continue
		}
		topY := g.surfaceYAt(c, preview, chunkX, chunkZ, wx, wz)
		limit := topY * 2
		if limit <= 0 {
			continue
		}
		wy := int(r.Intn(int32(limit)))
		g.placeGrass(c, preview, chunkX, chunkZ, wx, wy, wz, r)
	}

	for i := 0; i < s.deadBushPerChunk; i++ {
		wx := originX + int(r.Intn(16)) + 8
		wz := originZ + int(r.Intn(16)) + 8
		if wx < chunkMinX || wx > chunkMaxX || wz < chunkMinZ || wz > chunkMaxZ {
			continue
		}
		topY := g.surfaceYAt(c, preview, chunkX, chunkZ, wx, wz)
		limit := topY * 2
		if limit <= 0 {
			continue
		}
		wy := int(r.Intn(int32(limit)))
		g.placeDeadBush(c, preview, chunkX, chunkZ, wx, wy, wz)
	}

	reedsTries := s.reedsPerChunk
	if reedsTries > 0 {
		reedsTries += 10
	}
	for i := 0; i < reedsTries; i++ {
		wx := originX + int(r.Intn(16)) + 8
		wz := originZ + int(r.Intn(16)) + 8
		if wx < chunkMinX || wx > chunkMaxX || wz < chunkMinZ || wz > chunkMaxZ {
			continue
		}
		topY := g.surfaceYAt(c, preview, chunkX, chunkZ, wx, wz)
		limit := topY * 2
		if limit <= 0 {
			continue
		}
		wy := int(r.Intn(int32(limit)))
		g.placeSugarCane(c, preview, chunkX, chunkZ, wx, wy, wz, r)
	}

	for i := 0; i < s.cactiPerChunk; i++ {
		wx := originX + int(r.Intn(16)) + 8
		wz := originZ + int(r.Intn(16)) + 8
		if wx < chunkMinX || wx > chunkMaxX || wz < chunkMinZ || wz > chunkMaxZ {
			continue
		}
		topY := g.surfaceYAt(c, preview, chunkX, chunkZ, wx, wz)
		limit := topY * 2
		if limit <= 0 {
			continue
		}
		wy := int(r.Intn(int32(limit)))
		g.placeCactus(c, preview, chunkX, chunkZ, wx, wy, wz, r)
	}

	if s.sunflowers {
		for i := 0; i < 10; i++ {
			wx := originX + int(r.Intn(16)) + 8
			wz := originZ + int(r.Intn(16)) + 8
			if wx < chunkMinX || wx > chunkMaxX || wz < chunkMinZ || wz > chunkMaxZ {
				continue
			}
			topY := g.surfaceYAt(c, preview, chunkX, chunkZ, wx, wz)
			limit := topY + 32
			if limit <= 0 {
				continue
			}
			wy := int(r.Intn(int32(limit)))
			g.placeSunflower(c, preview, chunkX, chunkZ, wx, wy, wz)
		}
	}
}

func (g *Overworld) previewChunkCached(preview map[world.ChunkPos]*chunk.Chunk, chunkX, chunkZ int) *chunk.Chunk {
	pos := world.ChunkPos{int32(chunkX), int32(chunkZ)}
	if c, ok := preview[pos]; ok {
		return c
	}
	c := g.previewChunk(chunkX, chunkZ)
	preview[pos] = c
	return c
}

func (g *Overworld) surfaceYAt(c *chunk.Chunk, preview map[world.ChunkPos]*chunk.Chunk, chunkX, chunkZ int, worldX, worldZ int) int {
	if worldX>>4 == chunkX && worldZ>>4 == chunkZ {
		return int(c.HighestBlock(uint8(worldX&15), uint8(worldZ&15)))
	}
	pc := g.previewChunkCached(preview, worldX>>4, worldZ>>4)
	return int(pc.HighestBlock(uint8(worldX&15), uint8(worldZ&15)))
}

func (g *Overworld) blockRIDAt(c *chunk.Chunk, preview map[world.ChunkPos]*chunk.Chunk, chunkX, chunkZ int, worldX, worldY, worldZ int) uint32 {
	if worldY < 0 || worldY > 255 {
		return g.airRID
	}
	if worldX>>4 == chunkX && worldZ>>4 == chunkZ {
		return c.Block(uint8(worldX&15), int16(worldY), uint8(worldZ&15), 0)
	}
	pc := g.previewChunkCached(preview, worldX>>4, worldZ>>4)
	return pc.Block(uint8(worldX&15), int16(worldY), uint8(worldZ&15), 0)
}

func (g *Overworld) setRIDIfInChunk(c *chunk.Chunk, chunkX, chunkZ int, worldX, worldY, worldZ int, rid uint32) {
	if worldY < 0 || worldY > 255 {
		return
	}
	if worldX>>4 != chunkX || worldZ>>4 != chunkZ {
		return
	}
	yy := int16(worldY)
	if yy < int16(c.Range().Min()) || yy > int16(c.Range().Max()) {
		return
	}
	c.SetBlock(uint8(worldX&15), yy, uint8(worldZ&15), 0, rid)
}

func (g *Overworld) isSoil(rid uint32) bool {
	switch rid {
	case g.grassRID, g.dirtRID, g.podzolRID, g.myceliumRID:
		return true
	default:
		return false
	}
}

func (g *Overworld) isLeaves(rid uint32) bool {
	switch rid {
	case g.oakLeavesRID, g.spruceLeavesRID, g.birchLeavesRID, g.jungleLeavesRID, g.acaciaLeavesRID, g.darkOakLeavesRID:
		return true
	default:
		return false
	}
}

func (g *Overworld) canReplaceWithLeaves(rid uint32) bool {
	return rid == g.airRID || g.isLeaves(rid)
}

type treeKind uint8

const (
	treeOak treeKind = iota
	treeBirch
	treeSpruce
	treeJungle
	treeAcacia
	treeDarkOak
)

func (g *Overworld) pickTreeKind(biomeID int, r *mc112.Rand) treeKind {
	switch mcbiome.ID(biomeID) {
	case mcbiome.BirchForest, mcbiome.BirchForestHills, mcbiome.TallBirchForest, mcbiome.TallBirchHills:
		return treeBirch
	case mcbiome.Taiga, mcbiome.TaigaHills, mcbiome.TaigaMountains,
		mcbiome.SnowyTaiga, mcbiome.SnowyTaigaHills, mcbiome.SnowyTaigaMountains,
		mcbiome.GiantTreeTaiga, mcbiome.GiantTreeTaigaHills, mcbiome.GiantSpruceTaiga, mcbiome.GiantSpruceTaigaHills:
		return treeSpruce
	case mcbiome.Jungle, mcbiome.JungleHills, mcbiome.JungleEdge, mcbiome.ModifiedJungle, mcbiome.ModifiedJungleEdge:
		return treeJungle
	case mcbiome.Savanna, mcbiome.SavannaPlateau, mcbiome.ShatteredSavanna, mcbiome.ShatteredSavannaPlateau:
		return treeAcacia
	case mcbiome.DarkForest, mcbiome.DarkForestHills:
		return treeDarkOak
	case mcbiome.Forest, mcbiome.FlowerForest:
		if r.Intn(5) == 0 {
			return treeBirch
		}
		return treeOak
	default:
		return treeOak
	}
}

func (g *Overworld) genSimpleTree(c *chunk.Chunk, preview map[world.ChunkPos]*chunk.Chunk, chunkX, chunkZ int, x, y, z int, r *mc112.Rand, logRID, leavesRID uint32) {
	height := 4 + int(r.Intn(3))
	soil := g.blockRIDAt(c, preview, chunkX, chunkZ, x, y-1, z)
	if !g.isSoil(soil) {
		return
	}
	if y+height+1 > 255 {
		return
	}

	g.setRIDIfInChunk(c, chunkX, chunkZ, x, y-1, z, g.dirtRID)

	top := y + height
	for yy := y; yy < y+height; yy++ {
		g.setRIDIfInChunk(c, chunkX, chunkZ, x, yy, z, logRID)
	}

	for yy := top - 3; yy <= top; yy++ {
		layer := yy - top
		radius := 2 - layer/2
		for dx := -radius; dx <= radius; dx++ {
			for dz := -radius; dz <= radius; dz++ {
				if dx == 0 && dz == 0 && layer == 0 {
					continue
				}
				wx, wz := x+dx, z+dz
				if wx>>4 != chunkX || wz>>4 != chunkZ {
					continue
				}
				rid := c.Block(uint8(wx&15), int16(yy), uint8(wz&15), 0)
				if g.canReplaceWithLeaves(rid) {
					g.setRIDIfInChunk(c, chunkX, chunkZ, wx, yy, wz, leavesRID)
				}
			}
		}
	}
}

func (g *Overworld) genBigOak(c *chunk.Chunk, preview map[world.ChunkPos]*chunk.Chunk, chunkX, chunkZ int, x, y, z int, r *mc112.Rand) {
	height := 7 + int(r.Intn(5))
	soil := g.blockRIDAt(c, preview, chunkX, chunkZ, x, y-1, z)
	if !g.isSoil(soil) {
		return
	}
	if y+height+2 > 255 {
		return
	}

	g.setRIDIfInChunk(c, chunkX, chunkZ, x, y-1, z, g.dirtRID)
	for yy := y; yy < y+height; yy++ {
		g.setRIDIfInChunk(c, chunkX, chunkZ, x, yy, z, g.oakLogRID)
	}

	top := y + height
	for yy := top - 4; yy <= top; yy++ {
		layer := yy - top
		radius := 3 - layer/2
		if radius < 1 {
			radius = 1
		}
		for dx := -radius; dx <= radius; dx++ {
			for dz := -radius; dz <= radius; dz++ {
				if dx*dx+dz*dz > radius*radius+1 {
					continue
				}
				wx, wz := x+dx, z+dz
				if wx>>4 != chunkX || wz>>4 != chunkZ {
					continue
				}
				rid := c.Block(uint8(wx&15), int16(yy), uint8(wz&15), 0)
				if g.canReplaceWithLeaves(rid) {
					g.setRIDIfInChunk(c, chunkX, chunkZ, wx, yy, wz, g.oakLeavesRID)
				}
			}
		}
	}
}

func (g *Overworld) genSpruce(c *chunk.Chunk, preview map[world.ChunkPos]*chunk.Chunk, chunkX, chunkZ int, x, y, z int, r *mc112.Rand) {
	height := 6 + int(r.Intn(5))
	soil := g.blockRIDAt(c, preview, chunkX, chunkZ, x, y-1, z)
	if !g.isSoil(soil) {
		return
	}
	if y+height+2 > 255 {
		return
	}
	g.setRIDIfInChunk(c, chunkX, chunkZ, x, y-1, z, g.dirtRID)

	for yy := y; yy < y+height; yy++ {
		g.setRIDIfInChunk(c, chunkX, chunkZ, x, yy, z, g.spruceLogRID)
	}

	top := y + height
	radius := 0
	for yy := top; yy >= y+2; yy-- {
		if yy < top-2 {
			radius++
			if radius > 3 {
				radius = 3
			}
		}
		for dx := -radius; dx <= radius; dx++ {
			for dz := -radius; dz <= radius; dz++ {
				wx, wz := x+dx, z+dz
				if dx*dx+dz*dz > radius*radius+1 {
					continue
				}
				if wx>>4 != chunkX || wz>>4 != chunkZ {
					continue
				}
				rid := c.Block(uint8(wx&15), int16(yy), uint8(wz&15), 0)
				if g.canReplaceWithLeaves(rid) {
					g.setRIDIfInChunk(c, chunkX, chunkZ, wx, yy, wz, g.spruceLeavesRID)
				}
			}
		}
	}
}

func (g *Overworld) genJungle(c *chunk.Chunk, preview map[world.ChunkPos]*chunk.Chunk, chunkX, chunkZ int, x, y, z int, r *mc112.Rand) {
	height := 8 + int(r.Intn(7))
	soil := g.blockRIDAt(c, preview, chunkX, chunkZ, x, y-1, z)
	if !g.isSoil(soil) {
		return
	}
	if y+height+2 > 255 {
		return
	}
	g.setRIDIfInChunk(c, chunkX, chunkZ, x, y-1, z, g.dirtRID)

	for yy := y; yy < y+height; yy++ {
		g.setRIDIfInChunk(c, chunkX, chunkZ, x, yy, z, g.jungleLogRID)
	}

	top := y + height
	for yy := top - 2; yy <= top; yy++ {
		radius := 2
		if yy == top {
			radius = 1
		}
		for dx := -radius; dx <= radius; dx++ {
			for dz := -radius; dz <= radius; dz++ {
				wx, wz := x+dx, z+dz
				if wx>>4 != chunkX || wz>>4 != chunkZ {
					continue
				}
				rid := c.Block(uint8(wx&15), int16(yy), uint8(wz&15), 0)
				if g.canReplaceWithLeaves(rid) {
					g.setRIDIfInChunk(c, chunkX, chunkZ, wx, yy, wz, g.jungleLeavesRID)
				}
			}
		}
	}
}

func (g *Overworld) genAcacia(c *chunk.Chunk, preview map[world.ChunkPos]*chunk.Chunk, chunkX, chunkZ int, x, y, z int, r *mc112.Rand) {
	height := 5 + int(r.Intn(3))
	soil := g.blockRIDAt(c, preview, chunkX, chunkZ, x, y-1, z)
	if !g.isSoil(soil) {
		return
	}
	if y+height+4 > 255 {
		return
	}
	g.setRIDIfInChunk(c, chunkX, chunkZ, x, y-1, z, g.dirtRID)

	for yy := y; yy < y+height; yy++ {
		g.setRIDIfInChunk(c, chunkX, chunkZ, x, yy, z, g.acaciaLogRID)
	}

	top := y + height
	for dx := -2; dx <= 2; dx++ {
		for dz := -2; dz <= 2; dz++ {
			if dx*dx+dz*dz > 7 {
				continue
			}
			wx, wz := x+dx, z+dz
			if wx>>4 != chunkX || wz>>4 != chunkZ {
				continue
			}
			rid := c.Block(uint8(wx&15), int16(top), uint8(wz&15), 0)
			if g.canReplaceWithLeaves(rid) {
				g.setRIDIfInChunk(c, chunkX, chunkZ, wx, top, wz, g.acaciaLeavesRID)
			}
		}
	}
}

func (g *Overworld) genDarkOak(c *chunk.Chunk, preview map[world.ChunkPos]*chunk.Chunk, chunkX, chunkZ int, x, y, z int, r *mc112.Rand) {
	height := 6 + int(r.Intn(3))
	soil := g.blockRIDAt(c, preview, chunkX, chunkZ, x, y-1, z)
	if !g.isSoil(soil) {
		return
	}
	if y+height+4 > 255 {
		return
	}
	g.setRIDIfInChunk(c, chunkX, chunkZ, x, y-1, z, g.dirtRID)

	// 2x2 trunk.
	for yy := y; yy < y+height; yy++ {
		for dx := 0; dx <= 1; dx++ {
			for dz := 0; dz <= 1; dz++ {
				g.setRIDIfInChunk(c, chunkX, chunkZ, x+dx, yy, z+dz, g.darkOakLogRID)
			}
		}
	}

	top := y + height
	for yy := top - 2; yy <= top; yy++ {
		radius := 3
		if yy == top {
			radius = 2
		}
		for dx := -radius; dx <= radius; dx++ {
			for dz := -radius; dz <= radius; dz++ {
				if dx*dx+dz*dz > radius*radius+2 {
					continue
				}
				wx, wz := x+dx, z+dz
				if wx>>4 != chunkX || wz>>4 != chunkZ {
					continue
				}
				rid := c.Block(uint8(wx&15), int16(yy), uint8(wz&15), 0)
				if g.canReplaceWithLeaves(rid) {
					g.setRIDIfInChunk(c, chunkX, chunkZ, wx, yy, wz, g.darkOakLeavesRID)
				}
			}
		}
	}
}

func (g *Overworld) placeGrass(c *chunk.Chunk, preview map[world.ChunkPos]*chunk.Chunk, chunkX, chunkZ int, x, y, z int, r *mc112.Rand) {
	if y < 1 || y > 255 {
		return
	}
	if g.blockRIDAt(c, preview, chunkX, chunkZ, x, y, z) != g.airRID {
		return
	}
	below := g.blockRIDAt(c, preview, chunkX, chunkZ, x, y-1, z)
	if below != g.grassRID {
		return
	}
	if r.Intn(8) == 0 && y+1 <= 255 && g.blockRIDAt(c, preview, chunkX, chunkZ, x, y+1, z) == g.airRID {
		g.setRIDIfInChunk(c, chunkX, chunkZ, x, y, z, g.doubleTallGrassLowerRID)
		g.setRIDIfInChunk(c, chunkX, chunkZ, x, y+1, z, g.doubleTallGrassUpperRID)
		return
	}
	g.setRIDIfInChunk(c, chunkX, chunkZ, x, y, z, g.shortGrassRID)
}

func (g *Overworld) placeFlower(c *chunk.Chunk, preview map[world.ChunkPos]*chunk.Chunk, chunkX, chunkZ int, x, y, z int, biomeID int, r *mc112.Rand) {
	if y < 1 || y > 255 {
		return
	}
	if g.blockRIDAt(c, preview, chunkX, chunkZ, x, y, z) != g.airRID {
		return
	}
	below := g.blockRIDAt(c, preview, chunkX, chunkZ, x, y-1, z)
	if below != g.grassRID {
		return
	}

	var flower uint32
	switch mcbiome.ID(biomeID) {
	case mcbiome.Swamp, mcbiome.SwampHills:
		flower = g.blueOrchidRID
	default:
		if r.Intn(3) == 0 {
			flower = g.dandelionRID
		} else {
			flower = g.poppyRID
		}
	}
	g.setRIDIfInChunk(c, chunkX, chunkZ, x, y, z, flower)
}

func (g *Overworld) placeDeadBush(c *chunk.Chunk, preview map[world.ChunkPos]*chunk.Chunk, chunkX, chunkZ int, x, y, z int) {
	if y < 1 || y > 255 {
		return
	}
	if g.blockRIDAt(c, preview, chunkX, chunkZ, x, y, z) != g.airRID {
		return
	}
	below := g.blockRIDAt(c, preview, chunkX, chunkZ, x, y-1, z)
	if below != g.sandRID && below != g.redSandRID && below != g.terracottaRID {
		return
	}
	g.setRIDIfInChunk(c, chunkX, chunkZ, x, y, z, g.deadBushRID)
}

func (g *Overworld) placeCactus(c *chunk.Chunk, preview map[world.ChunkPos]*chunk.Chunk, chunkX, chunkZ int, x, y, z int, r *mc112.Rand) {
	if y < 1 || y > 255 {
		return
	}
	if g.blockRIDAt(c, preview, chunkX, chunkZ, x, y, z) != g.airRID {
		return
	}
	below := g.blockRIDAt(c, preview, chunkX, chunkZ, x, y-1, z)
	if below != g.sandRID && below != g.redSandRID {
		return
	}
	height := 1 + int(r.Intn(3))
	for yy := 0; yy < height; yy++ {
		if y+yy > 255 {
			break
		}
		if g.blockRIDAt(c, preview, chunkX, chunkZ, x, y+yy, z) != g.airRID {
			break
		}
		g.setRIDIfInChunk(c, chunkX, chunkZ, x, y+yy, z, g.cactusRID)
	}
}

func (g *Overworld) placeSugarCane(c *chunk.Chunk, preview map[world.ChunkPos]*chunk.Chunk, chunkX, chunkZ int, x, y, z int, r *mc112.Rand) {
	if y < 1 || y > 255 {
		return
	}
	if g.blockRIDAt(c, preview, chunkX, chunkZ, x, y, z) != g.airRID {
		return
	}
	below := g.blockRIDAt(c, preview, chunkX, chunkZ, x, y-1, z)
	if below != g.grassRID && below != g.dirtRID && below != g.sandRID && below != g.redSandRID {
		return
	}
	// Vanilla checks water adjacency at the block below.
	hasWater := false
	for _, face := range cube.HorizontalFaces() {
		nx, nz := x, z
		switch face {
		case cube.FaceNorth:
			nz--
		case cube.FaceSouth:
			nz++
		case cube.FaceWest:
			nx--
		case cube.FaceEast:
			nx++
		}
		if g.blockRIDAt(c, preview, chunkX, chunkZ, nx, y-1, nz) == g.waterRID {
			hasWater = true
			break
		}
	}
	if !hasWater {
		return
	}
	height := 1 + int(r.Intn(3))
	for yy := 0; yy < height; yy++ {
		if y+yy > 255 {
			break
		}
		if g.blockRIDAt(c, preview, chunkX, chunkZ, x, y+yy, z) != g.airRID {
			break
		}
		g.setRIDIfInChunk(c, chunkX, chunkZ, x, y+yy, z, g.sugarCaneRID)
	}
}

func (g *Overworld) placeSunflower(c *chunk.Chunk, preview map[world.ChunkPos]*chunk.Chunk, chunkX, chunkZ int, x, y, z int) {
	if y < 1 || y+1 > 255 {
		return
	}
	if g.blockRIDAt(c, preview, chunkX, chunkZ, x, y, z) != g.airRID || g.blockRIDAt(c, preview, chunkX, chunkZ, x, y+1, z) != g.airRID {
		return
	}
	below := g.blockRIDAt(c, preview, chunkX, chunkZ, x, y-1, z)
	if below != g.grassRID {
		return
	}
	g.setRIDIfInChunk(c, chunkX, chunkZ, x, y, z, g.sunflowerLowerRID)
	g.setRIDIfInChunk(c, chunkX, chunkZ, x, y+1, z, g.sunflowerUpperRID)
}
