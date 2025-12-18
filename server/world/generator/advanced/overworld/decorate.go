package overworld

import (
	"github.com/df-mc/dragonfly/server/block"
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
	sandPatchesPerChunk   int
	gravelPatchesPerChunk int
	clayPerChunk          int
	mushroomsPerChunk     int
	bigMushroomsPerChunk  int
	waterlilyPerChunk     int

	generateFalls bool

	sunflowers bool

	jungleVines  bool
	jungleMelons bool
}

func decoratorSettingsForBiome(biomeID int) decoratorSettings {
	switch mcbiome.ID(biomeID) {
	case mcbiome.Plains:
		return decoratorSettings{
			treesPerChunk: 0, extraTreeChance: 0.05, flowersPerChunk: 4, grassPerChunk: 10,
			sandPatchesPerChunk: 3, gravelPatchesPerChunk: 1, clayPerChunk: 1,
			generateFalls: true,
		}
	case mcbiome.SunflowerPlains:
		return decoratorSettings{
			treesPerChunk: 0, extraTreeChance: 0.05, flowersPerChunk: 4, grassPerChunk: 10, sunflowers: true,
			sandPatchesPerChunk: 3, gravelPatchesPerChunk: 1, clayPerChunk: 1,
			generateFalls: true,
		}

	case mcbiome.Forest:
		return decoratorSettings{
			treesPerChunk: 10, extraTreeChance: 0.0, flowersPerChunk: 2, grassPerChunk: 2,
			sandPatchesPerChunk: 3, gravelPatchesPerChunk: 1, clayPerChunk: 1,
			generateFalls: true,
		}
	case mcbiome.FlowerForest:
		return decoratorSettings{
			treesPerChunk: 6, extraTreeChance: 0.0, flowersPerChunk: 100, grassPerChunk: 1,
			sandPatchesPerChunk: 3, gravelPatchesPerChunk: 1, clayPerChunk: 1,
			generateFalls: true,
		}
	case mcbiome.BirchForest, mcbiome.BirchForestHills, mcbiome.TallBirchForest, mcbiome.TallBirchHills:
		return decoratorSettings{
			treesPerChunk: 10, extraTreeChance: 0.0, flowersPerChunk: 2, grassPerChunk: 2,
			sandPatchesPerChunk: 3, gravelPatchesPerChunk: 1, clayPerChunk: 1,
			generateFalls: true,
		}
	case mcbiome.DarkForest, mcbiome.DarkForestHills:
		return decoratorSettings{
			treesPerChunk: 20, extraTreeChance: 0.0, flowersPerChunk: 1, grassPerChunk: 2,
			sandPatchesPerChunk: 3, gravelPatchesPerChunk: 1, clayPerChunk: 1,
			mushroomsPerChunk: 1, bigMushroomsPerChunk: 1,
			generateFalls: true,
		}

	case mcbiome.Taiga, mcbiome.TaigaHills, mcbiome.TaigaMountains,
		mcbiome.SnowyTaiga, mcbiome.SnowyTaigaHills, mcbiome.SnowyTaigaMountains:
		return decoratorSettings{
			treesPerChunk: 10, extraTreeChance: 0.0, flowersPerChunk: 1, grassPerChunk: 1,
			sandPatchesPerChunk: 3, gravelPatchesPerChunk: 1, clayPerChunk: 1,
			mushroomsPerChunk: 1,
			generateFalls: true,
		}
	case mcbiome.GiantTreeTaiga, mcbiome.GiantTreeTaigaHills, mcbiome.GiantSpruceTaiga, mcbiome.GiantSpruceTaigaHills:
		return decoratorSettings{
			treesPerChunk: 10, extraTreeChance: 0.0, flowersPerChunk: 1, grassPerChunk: 7, deadBushPerChunk: 1,
			sandPatchesPerChunk: 3, gravelPatchesPerChunk: 1, clayPerChunk: 1,
			mushroomsPerChunk: 3,
			generateFalls: true,
		}

	case mcbiome.Jungle, mcbiome.JungleHills, mcbiome.ModifiedJungle:
		return decoratorSettings{
			treesPerChunk: 50, extraTreeChance: 0.0, flowersPerChunk: 4, grassPerChunk: 25,
			sandPatchesPerChunk: 3, gravelPatchesPerChunk: 1, clayPerChunk: 1,
			generateFalls: true,
			jungleVines: true, jungleMelons: true,
		}
	case mcbiome.JungleEdge, mcbiome.ModifiedJungleEdge:
		return decoratorSettings{
			treesPerChunk: 2, extraTreeChance: 0.0, flowersPerChunk: 4, grassPerChunk: 25,
			sandPatchesPerChunk: 3, gravelPatchesPerChunk: 1, clayPerChunk: 1,
			generateFalls: true,
			jungleVines: true, jungleMelons: true,
		}

	case mcbiome.Savanna, mcbiome.SavannaPlateau, mcbiome.ShatteredSavanna, mcbiome.ShatteredSavannaPlateau:
		return decoratorSettings{
			treesPerChunk: 1, extraTreeChance: 0.1, flowersPerChunk: 4, grassPerChunk: 20,
			sandPatchesPerChunk: 3, gravelPatchesPerChunk: 1, clayPerChunk: 1,
			generateFalls: true,
		}

	case mcbiome.Swamp, mcbiome.SwampHills:
		return decoratorSettings{
			treesPerChunk: 2, extraTreeChance: 0.0, flowersPerChunk: 1, grassPerChunk: 5, deadBushPerChunk: 1, reedsPerChunk: 10,
			clayPerChunk: 1, waterlilyPerChunk: 4, sandPatchesPerChunk: 0, gravelPatchesPerChunk: 0,
			mushroomsPerChunk: 8,
			generateFalls: true,
		}

	case mcbiome.Desert, mcbiome.DesertHills, mcbiome.DesertM:
		return decoratorSettings{
			treesPerChunk: 0, extraTreeChance: 0.0, flowersPerChunk: 0, grassPerChunk: 0, deadBushPerChunk: 2, reedsPerChunk: 50, cactiPerChunk: 10,
			sandPatchesPerChunk: 3, gravelPatchesPerChunk: 1, clayPerChunk: 1,
			generateFalls: true,
		}

	default:
		// Vanilla BiomeDecorator defaults.
		return decoratorSettings{
			treesPerChunk: 0, extraTreeChance: 0.1, flowersPerChunk: 2, grassPerChunk: 1,
			sandPatchesPerChunk: 3, gravelPatchesPerChunk: 1, clayPerChunk: 1,
			generateFalls: true,
		}
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

	for i := 0; i < treeCount; i++ {
		wx := originX + int(r.Intn(16)) + 8
		wz := originZ + int(r.Intn(16)) + 8
		baseY := g.surfaceYAt(c, preview, chunkX, chunkZ, wx, wz)
		if baseY <= 0 || baseY >= 255 {
			continue
		}
		startY := baseY + 1
		kind := g.pickTreeKind(biomeID, r)

		// We must keep random consumption identical across chunks when simulating the same origin chunk,
		// otherwise cross-chunk decorations diverge and get clipped. Tree shape randomness is consumed
		// inside the gen* functions (height, etc), even when apply=false.
		apply := intersectsXZ(chunkMinX, chunkMinZ, chunkMaxX, chunkMaxZ, wx, wz, g.treeMaxRadius(kind))
		switch kind {
		case treeOak:
			if r.Intn(10) == 0 {
				g.genBigOak(c, preview, chunkX, chunkZ, wx, startY, wz, r, apply)
			} else {
				g.genSimpleTree(c, preview, chunkX, chunkZ, wx, startY, wz, r, g.oakLogRID, g.oakLeavesRID, apply)
			}
		case treeBirch:
			g.genSimpleTree(c, preview, chunkX, chunkZ, wx, startY, wz, r, g.birchLogRID, g.birchLeavesRID, apply)
		case treeSpruce:
			g.genSpruce(c, preview, chunkX, chunkZ, wx, startY, wz, r, apply)
		case treeJungle:
			g.genJungle(c, preview, chunkX, chunkZ, wx, startY, wz, r, apply)
		case treeAcacia:
			g.genAcacia(c, preview, chunkX, chunkZ, wx, startY, wz, r, apply)
		case treeDarkOak:
			g.genDarkOak(c, preview, chunkX, chunkZ, wx, startY, wz, r, apply)
		}
	}

	// Patches: sand, clay, gravel (vanilla uses getTopSolidOrLiquidBlock + checks WATER at the returned position).
	for i := 0; i < s.sandPatchesPerChunk; i++ {
		wx := originX + int(r.Intn(16)) + 8
		wz := originZ + int(r.Intn(16)) + 8
		wy := g.topSolidOrLiquidYAt(c, preview, chunkX, chunkZ, wx, wz)
		g.genSandLikePatch(c, preview, chunkX, chunkZ, chunkMinX, chunkMinZ, chunkMaxX, chunkMaxZ, wx, wy, wz, r, g.sandRID, 7, 2)
	}
	for i := 0; i < s.clayPerChunk; i++ {
		wx := originX + int(r.Intn(16)) + 8
		wz := originZ + int(r.Intn(16)) + 8
		wy := g.topSolidOrLiquidYAt(c, preview, chunkX, chunkZ, wx, wz)
		g.genSandLikePatch(c, preview, chunkX, chunkZ, chunkMinX, chunkMinZ, chunkMaxX, chunkMaxZ, wx, wy, wz, r, g.clayRID, 4, 1)
	}
	for i := 0; i < s.gravelPatchesPerChunk; i++ {
		wx := originX + int(r.Intn(16)) + 8
		wz := originZ + int(r.Intn(16)) + 8
		wy := g.topSolidOrLiquidYAt(c, preview, chunkX, chunkZ, wx, wz)
		g.genSandLikePatch(c, preview, chunkX, chunkZ, chunkMinX, chunkMinZ, chunkMaxX, chunkMaxZ, wx, wy, wz, r, g.gravelRID, 6, 2)
	}

	for i := 0; i < s.flowersPerChunk; i++ {
		wx := originX + int(r.Intn(16)) + 8
		wz := originZ + int(r.Intn(16)) + 8
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
		topY := g.surfaceYAt(c, preview, chunkX, chunkZ, wx, wz)
		limit := topY * 2
		if limit <= 0 {
			continue
		}
		wy := int(r.Intn(int32(limit)))
		g.placeDeadBush(c, preview, chunkX, chunkZ, wx, wy, wz)
	}

	// Vanilla always does an extra 10 reed attempts.
	reedsTries := s.reedsPerChunk + 10
	for i := 0; i < reedsTries; i++ {
		wx := originX + int(r.Intn(16)) + 8
		wz := originZ + int(r.Intn(16)) + 8
		topY := g.surfaceYAt(c, preview, chunkX, chunkZ, wx, wz)
		limit := topY * 2
		if limit <= 0 {
			continue
		}
		wy := int(r.Intn(int32(limit)))
		g.placeSugarCane(c, preview, chunkX, chunkZ, wx, wy, wz, r)
	}

	// Waterlilies.
	for i := 0; i < s.waterlilyPerChunk; i++ {
		wx := originX + int(r.Intn(16)) + 8
		wz := originZ + int(r.Intn(16)) + 8
		topY := g.surfaceYAt(c, preview, chunkX, chunkZ, wx, wz)
		limit := topY * 2
		if limit <= 0 {
			continue
		}
		wy := int(r.Intn(int32(limit)))
		g.placeWaterlily(c, preview, chunkX, chunkZ, wx, wy, wz, r)
	}

	// Big mushrooms.
	for i := 0; i < s.bigMushroomsPerChunk; i++ {
		wx := originX + int(r.Intn(16)) + 8
		wz := originZ + int(r.Intn(16)) + 8
		baseY := g.surfaceYAt(c, preview, chunkX, chunkZ, wx, wz)
		g.genBigMushroom(c, preview, chunkX, chunkZ, wx, baseY+1, wz, r)
	}

	// Mushrooms (best-effort).
	for i := 0; i < s.mushroomsPerChunk; i++ {
		if r.Intn(4) == 0 {
			wx := originX + int(r.Intn(16)) + 8
			wz := originZ + int(r.Intn(16)) + 8
			baseY := g.surfaceYAt(c, preview, chunkX, chunkZ, wx, wz)
			g.placeMushroom(c, preview, chunkX, chunkZ, wx, baseY+1, wz, r, true)
		}
		if r.Intn(8) == 0 {
			wx := originX + int(r.Intn(16)) + 8
			wz := originZ + int(r.Intn(16)) + 8
			topY := g.surfaceYAt(c, preview, chunkX, chunkZ, wx, wz)
			limit := topY * 2
			if limit <= 0 {
				continue
			}
			wy := int(r.Intn(int32(limit)))
			g.placeMushroom(c, preview, chunkX, chunkZ, wx, wy, wz, r, false)
		}
	}
	if r.Intn(4) == 0 {
		wx := originX + int(r.Intn(16)) + 8
		wz := originZ + int(r.Intn(16)) + 8
		topY := g.surfaceYAt(c, preview, chunkX, chunkZ, wx, wz)
		limit := topY * 2
		if limit > 0 {
			wy := int(r.Intn(int32(limit)))
			g.placeMushroom(c, preview, chunkX, chunkZ, wx, wy, wz, r, true)
		}
	}
	if r.Intn(8) == 0 {
		wx := originX + int(r.Intn(16)) + 8
		wz := originZ + int(r.Intn(16)) + 8
		topY := g.surfaceYAt(c, preview, chunkX, chunkZ, wx, wz)
		limit := topY * 2
		if limit > 0 {
			wy := int(r.Intn(int32(limit)))
			g.placeMushroom(c, preview, chunkX, chunkZ, wx, wy, wz, r, false)
		}
	}

	// Pumpkins.
	if r.Intn(32) == 0 {
		wx := originX + int(r.Intn(16)) + 8
		wz := originZ + int(r.Intn(16)) + 8
		topY := g.surfaceYAt(c, preview, chunkX, chunkZ, wx, wz)
		limit := topY * 2
		if limit > 0 {
			wy := int(r.Intn(int32(limit)))
			g.genPumpkins(c, preview, chunkX, chunkZ, wx, wy, wz, r)
		}
	}

	// Jungle extras.
	if s.jungleMelons {
		wx := originX + int(r.Intn(16)) + 8
		wz := originZ + int(r.Intn(16)) + 8
		topY := g.surfaceYAt(c, preview, chunkX, chunkZ, wx, wz)
		limit := topY * 2
		if limit > 0 {
			wy := int(r.Intn(int32(limit)))
			g.genMelons(c, preview, chunkX, chunkZ, wx, wy, wz, r)
		}
	}
	if s.jungleVines {
		for i := 0; i < 50; i++ {
			wx := originX + int(r.Intn(16)) + 8
			wz := originZ + int(r.Intn(16)) + 8
			g.genVines(c, preview, chunkX, chunkZ, wx, 128, wz, r)
		}
	}

	for i := 0; i < s.cactiPerChunk; i++ {
		wx := originX + int(r.Intn(16)) + 8
		wz := originZ + int(r.Intn(16)) + 8
		topY := g.surfaceYAt(c, preview, chunkX, chunkZ, wx, wz)
		limit := topY * 2
		if limit <= 0 {
			continue
		}
		wy := int(r.Intn(int32(limit)))
		g.placeCactus(c, preview, chunkX, chunkZ, wx, wy, wz, r)
	}

	// Liquid springs (falls).
	if s.generateFalls {
		for i := 0; i < 50; i++ {
			wx := originX + int(r.Intn(16)) + 8
			wz := originZ + int(r.Intn(16)) + 8
			i17 := int(r.Intn(248)) + 8
			if i17 <= 0 {
				continue
			}
			wy := int(r.Intn(int32(i17)))
			g.genLiquidSpring(c, preview, chunkX, chunkZ, wx, wy, wz, g.waterRID)
		}

		for i := 0; i < 20; i++ {
			wx := originX + int(r.Intn(16)) + 8
			wz := originZ + int(r.Intn(16)) + 8
			wy := int(r.Intn(r.Intn(r.Intn(240)+8) + 8))
			g.genLiquidSpring(c, preview, chunkX, chunkZ, wx, wy, wz, g.lavaRID)
		}
	}

	if s.sunflowers {
		for i := 0; i < 10; i++ {
			wx := originX + int(r.Intn(16)) + 8
			wz := originZ + int(r.Intn(16)) + 8
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

func intersectsXZ(chunkMinX, chunkMinZ, chunkMaxX, chunkMaxZ, centerX, centerZ, radius int) bool {
	minX, maxX := centerX-radius, centerX+radius
	minZ, maxZ := centerZ-radius, centerZ+radius
	return maxX >= chunkMinX && maxZ >= chunkMinZ && minX <= chunkMaxX && minZ <= chunkMaxZ
}

func (g *Overworld) treeMaxRadius(kind treeKind) int {
	// Conservative bounds to avoid cross-chunk clipping.
	switch kind {
	case treeOak, treeBirch:
		return 5
	case treeSpruce:
		return 5
	case treeJungle:
		return 6
	case treeAcacia:
		return 6
	case treeDarkOak:
		return 7
	default:
		return 7
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

func (g *Overworld) topSolidOrLiquidYAt(c *chunk.Chunk, preview map[world.ChunkPos]*chunk.Chunk, chunkX, chunkZ int, worldX, worldZ int) int {
	for y := 255; y >= 0; y-- {
		rid := g.blockRIDAt(c, preview, chunkX, chunkZ, worldX, y, worldZ)
		if rid != g.airRID && !g.isLeaves(rid) {
			return y
		}
	}
	return 0
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

func (g *Overworld) genSandLikePatch(
	c *chunk.Chunk,
	preview map[world.ChunkPos]*chunk.Chunk,
	chunkX, chunkZ int,
	chunkMinX, chunkMinZ, chunkMaxX, chunkMaxZ int,
	x, y, z int,
	r *mc112.Rand,
	placeRID uint32,
	radius int,
	yExtent int,
) {
	if y <= 0 || y > 255 {
		return
	}
	if g.blockRIDAt(c, preview, chunkX, chunkZ, x, y, z) != g.waterRID {
		return
	}
	i := int(r.Intn(int32(radius-2))) + 2
	if !intersectsXZ(chunkMinX, chunkMinZ, chunkMaxX, chunkMaxZ, x, z, i) {
		return
	}
	for xx := x - i; xx <= x+i; xx++ {
		dx := xx - x
		for zz := z - i; zz <= z+i; zz++ {
			dz := zz - z
			if dx*dx+dz*dz > i*i {
				continue
			}
			for yy := y - yExtent; yy <= y+yExtent; yy++ {
				if yy < 0 || yy > 255 {
					continue
				}
				if xx>>4 != chunkX || zz>>4 != chunkZ {
					continue
				}
				cur := c.Block(uint8(xx&15), int16(yy), uint8(zz&15), 0)
				if placeRID == g.clayRID {
					if cur == g.dirtRID || cur == g.clayRID {
						c.SetBlock(uint8(xx&15), int16(yy), uint8(zz&15), 0, placeRID)
					}
					continue
				}
				if cur == g.dirtRID || cur == g.grassRID {
					c.SetBlock(uint8(xx&15), int16(yy), uint8(zz&15), 0, placeRID)
				}
			}
		}
	}
}

func (g *Overworld) genLiquidSpring(c *chunk.Chunk, preview map[world.ChunkPos]*chunk.Chunk, chunkX, chunkZ int, x, y, z int, liquidRID uint32) {
	if y <= 0 || y >= 255 {
		return
	}
	if x>>4 != chunkX || z>>4 != chunkZ {
		return
	}

	if g.blockRIDAt(c, preview, chunkX, chunkZ, x, y+1, z) != g.stoneRID {
		return
	}
	if g.blockRIDAt(c, preview, chunkX, chunkZ, x, y-1, z) != g.stoneRID {
		return
	}
	cur := c.Block(uint8(x&15), int16(y), uint8(z&15), 0)
	if cur != g.airRID && cur != g.stoneRID {
		return
	}

	stoneSides := 0
	airSides := 0
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
		if g.blockRIDAt(c, preview, chunkX, chunkZ, nx, y, nz) == g.stoneRID {
			stoneSides++
		}
		if g.blockRIDAt(c, preview, chunkX, chunkZ, nx, y, nz) == g.airRID {
			airSides++
		}
	}
	if stoneSides == 3 && airSides == 1 {
		c.SetBlock(uint8(x&15), int16(y), uint8(z&15), 0, liquidRID)
	}
}

func (g *Overworld) placeWaterlily(c *chunk.Chunk, preview map[world.ChunkPos]*chunk.Chunk, chunkX, chunkZ int, x, y, z int, r *mc112.Rand) {
	for i := 0; i < 10; i++ {
		wx := x + int(r.Intn(8)) - int(r.Intn(8))
		wy := y + int(r.Intn(4)) - int(r.Intn(4))
		wz := z + int(r.Intn(8)) - int(r.Intn(8))

		if wx>>4 != chunkX || wz>>4 != chunkZ || wy < 1 || wy > 255 {
			continue
		}
		if c.Block(uint8(wx&15), int16(wy), uint8(wz&15), 0) != g.airRID {
			continue
		}
		if g.blockRIDAt(c, preview, chunkX, chunkZ, wx, wy-1, wz) != g.waterRID {
			continue
		}
		c.SetBlock(uint8(wx&15), int16(wy), uint8(wz&15), 0, g.lilyPadRID)
	}
}

func (g *Overworld) placeMushroom(c *chunk.Chunk, preview map[world.ChunkPos]*chunk.Chunk, chunkX, chunkZ int, x, y, z int, r *mc112.Rand, brown bool) {
	name := "minecraft:red_mushroom"
	if brown {
		name = "minecraft:brown_mushroom"
	}
	b, ok := world.BlockByName(name, nil)
	if !ok {
		// If the block isn't in the runtime table, it doesn't exist for this build.
		return
	}
	rid := world.BlockRuntimeID(b)

	for i := 0; i < 64; i++ {
		wx := x + int(r.Intn(8)) - int(r.Intn(8))
		wy := y + int(r.Intn(4)) - int(r.Intn(4))
		wz := z + int(r.Intn(8)) - int(r.Intn(8))

		if wx>>4 != chunkX || wz>>4 != chunkZ || wy < 1 || wy > 255 {
			continue
		}
		if c.Block(uint8(wx&15), int16(wy), uint8(wz&15), 0) != g.airRID {
			continue
		}
		below := g.blockRIDAt(c, preview, chunkX, chunkZ, wx, wy-1, wz)
		if below != g.grassRID && below != g.dirtRID && below != g.myceliumRID && below != g.podzolRID {
			continue
		}
		c.SetBlock(uint8(wx&15), int16(wy), uint8(wz&15), 0, rid)
	}
}

func (g *Overworld) genBigMushroom(c *chunk.Chunk, preview map[world.ChunkPos]*chunk.Chunk, chunkX, chunkZ int, x, y, z int, r *mc112.Rand) {
	brown, okBrown := world.BlockByName("minecraft:brown_mushroom_block", nil)
	red, okRed := world.BlockByName("minecraft:red_mushroom_block", nil)
	if !okBrown && !okRed {
		return
	}

	mushRID := uint32(0)
	if okBrown && okRed {
		if r.Intn(2) == 0 {
			mushRID = world.BlockRuntimeID(brown)
		} else {
			mushRID = world.BlockRuntimeID(red)
		}
	} else if okBrown {
		mushRID = world.BlockRuntimeID(brown)
	} else {
		mushRID = world.BlockRuntimeID(red)
	}

	height := int(r.Intn(3)) + 4
	if r.Intn(12) == 0 {
		height *= 2
	}
	if y < 1 || y+height+1 >= 256 {
		return
	}
	if x>>4 != chunkX || z>>4 != chunkZ {
		return
	}
	soil := g.blockRIDAt(c, preview, chunkX, chunkZ, x, y-1, z)
	if soil != g.dirtRID && soil != g.grassRID && soil != g.myceliumRID {
		return
	}

	for yy := 0; yy < height; yy++ {
		if y+yy > 255 {
			break
		}
		c.SetBlock(uint8(x&15), int16(y+yy), uint8(z&15), 0, mushRID)
	}
	capY := y + height
	rad := 2
	for dx := -rad; dx <= rad; dx++ {
		for dz := -rad; dz <= rad; dz++ {
			wx, wz := x+dx, z+dz
			if wx>>4 != chunkX || wz>>4 != chunkZ {
				continue
			}
			if dx*dx+dz*dz > rad*rad+1 {
				continue
			}
			if capY >= 0 && capY <= 255 {
				c.SetBlock(uint8(wx&15), int16(capY), uint8(wz&15), 0, mushRID)
			}
		}
	}
}

func (g *Overworld) genPumpkins(c *chunk.Chunk, preview map[world.ChunkPos]*chunk.Chunk, chunkX, chunkZ int, x, y, z int, r *mc112.Rand) {
	for i := 0; i < 64; i++ {
		wx := x + int(r.Intn(8)) - int(r.Intn(8))
		wy := y + int(r.Intn(4)) - int(r.Intn(4))
		wz := z + int(r.Intn(8)) - int(r.Intn(8))
		if wx>>4 != chunkX || wz>>4 != chunkZ || wy < 1 || wy > 255 {
			continue
		}
		if c.Block(uint8(wx&15), int16(wy), uint8(wz&15), 0) != g.airRID {
			continue
		}
		if g.blockRIDAt(c, preview, chunkX, chunkZ, wx, wy-1, wz) != g.grassRID {
			continue
		}
		facing := cube.Direction(r.Intn(4))
		rid := world.BlockRuntimeID(block.Pumpkin{Facing: facing})
		c.SetBlock(uint8(wx&15), int16(wy), uint8(wz&15), 0, rid)
	}
}

func (g *Overworld) genMelons(c *chunk.Chunk, preview map[world.ChunkPos]*chunk.Chunk, chunkX, chunkZ int, x, y, z int, r *mc112.Rand) {
	melonRID := world.BlockRuntimeID(block.Melon{})
	for i := 0; i < 16; i++ {
		wx := x + int(r.Intn(8)) - int(r.Intn(8))
		wy := y + int(r.Intn(4)) - int(r.Intn(4))
		wz := z + int(r.Intn(8)) - int(r.Intn(8))
		if wx>>4 != chunkX || wz>>4 != chunkZ || wy < 1 || wy > 255 {
			continue
		}
		if c.Block(uint8(wx&15), int16(wy), uint8(wz&15), 0) != g.airRID {
			continue
		}
		if g.blockRIDAt(c, preview, chunkX, chunkZ, wx, wy-1, wz) != g.grassRID {
			continue
		}
		c.SetBlock(uint8(wx&15), int16(wy), uint8(wz&15), 0, melonRID)
	}
}

func (g *Overworld) genVines(c *chunk.Chunk, preview map[world.ChunkPos]*chunk.Chunk, chunkX, chunkZ int, x, y, z int, r *mc112.Rand) {
	_ = r
	if y < 1 || y > 255 {
		return
	}
	if x>>4 != chunkX || z>>4 != chunkZ {
		return
	}
	vine, ok := world.BlockByName("minecraft:vine", nil)
	if !ok {
		return
	}
	if c.Block(uint8(x&15), int16(y), uint8(z&15), 0) != g.airRID {
		return
	}
	hasSupport := false
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
		if g.blockRIDAt(c, preview, chunkX, chunkZ, nx, y, nz) != g.airRID {
			hasSupport = true
			break
		}
	}
	if !hasSupport {
		return
	}
	c.SetBlock(uint8(x&15), int16(y), uint8(z&15), 0, world.BlockRuntimeID(vine))
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
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

func (g *Overworld) genSimpleTree(c *chunk.Chunk, preview map[world.ChunkPos]*chunk.Chunk, chunkX, chunkZ int, x, y, z int, r *mc112.Rand, logRID, leavesRID uint32, apply bool) {
	height := 4 + int(r.Intn(3))
	if !apply {
		return
	}
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

func (g *Overworld) genBigOak(c *chunk.Chunk, preview map[world.ChunkPos]*chunk.Chunk, chunkX, chunkZ int, x, y, z int, r *mc112.Rand, apply bool) {
	height := 7 + int(r.Intn(5))
	if !apply {
		return
	}
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

func (g *Overworld) genSpruce(c *chunk.Chunk, preview map[world.ChunkPos]*chunk.Chunk, chunkX, chunkZ int, x, y, z int, r *mc112.Rand, apply bool) {
	height := 6 + int(r.Intn(5))
	if !apply {
		return
	}
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

func (g *Overworld) genJungle(c *chunk.Chunk, preview map[world.ChunkPos]*chunk.Chunk, chunkX, chunkZ int, x, y, z int, r *mc112.Rand, apply bool) {
	height := 8 + int(r.Intn(7))
	if !apply {
		return
	}
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

func (g *Overworld) genAcacia(c *chunk.Chunk, preview map[world.ChunkPos]*chunk.Chunk, chunkX, chunkZ int, x, y, z int, r *mc112.Rand, apply bool) {
	height := 5 + int(r.Intn(3))
	if !apply {
		return
	}
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

func (g *Overworld) genDarkOak(c *chunk.Chunk, preview map[world.ChunkPos]*chunk.Chunk, chunkX, chunkZ int, x, y, z int, r *mc112.Rand, apply bool) {
	height := 6 + int(r.Intn(3))
	if !apply {
		return
	}
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
	double := r.Intn(8) == 0
	if x>>4 != chunkX || z>>4 != chunkZ {
		return
	}
	if g.blockRIDAt(c, preview, chunkX, chunkZ, x, y, z) != g.airRID {
		return
	}
	below := g.blockRIDAt(c, preview, chunkX, chunkZ, x, y-1, z)
	if below != g.grassRID {
		return
	}
	if double && y+1 <= 255 && g.blockRIDAt(c, preview, chunkX, chunkZ, x, y+1, z) == g.airRID {
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
	if x>>4 != chunkX || z>>4 != chunkZ {
		return
	}
	if g.blockRIDAt(c, preview, chunkX, chunkZ, x, y, z) != g.airRID {
		return
	}
	below := g.blockRIDAt(c, preview, chunkX, chunkZ, x, y-1, z)
	if below != g.grassRID {
		return
	}
	g.setRIDIfInChunk(c, chunkX, chunkZ, x, y, z, flower)
}

func (g *Overworld) placeDeadBush(c *chunk.Chunk, preview map[world.ChunkPos]*chunk.Chunk, chunkX, chunkZ int, x, y, z int) {
	if y < 1 || y > 255 {
		return
	}
	if x>>4 != chunkX || z>>4 != chunkZ {
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
	height := 1 + int(r.Intn(3))
	if x>>4 != chunkX || z>>4 != chunkZ {
		return
	}
	if g.blockRIDAt(c, preview, chunkX, chunkZ, x, y, z) != g.airRID {
		return
	}
	below := g.blockRIDAt(c, preview, chunkX, chunkZ, x, y-1, z)
	if below != g.sandRID && below != g.redSandRID {
		return
	}
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
	height := 1 + int(r.Intn(3))
	if x>>4 != chunkX || z>>4 != chunkZ {
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
	if x>>4 != chunkX || z>>4 != chunkZ {
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
