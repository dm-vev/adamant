package overworld

import (
	"fmt"
	"math"
	"runtime"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/structure"
)

type modernTerrainScratch struct {
	lattice         vanilla.OverworldChunk
	aquifer         vanilla.OverworldAquifer
	biomeChunks     [3][3]*chunkBiomes
	biomePos        world.ChunkPos
	quartBiomeFn    vanilla.QuartBiomes
	blocks          [24][4096]uint32
	subUniformBlock [24]uint32
	subMixedBlock   [24]bool
	subNonAir       [24]bool
	biomes          [24][4096]uint8
	subUniformBiome [24]uint8
	heights         [18][18]int16
}

func newModernTerrainScratch() *modernTerrainScratch {
	s := &modernTerrainScratch{}
	s.quartBiomeFn = func(quartX, quartY, quartZ int32) uint8 {
		biomes := s.biomeChunks[quartX>>2-s.biomePos[0]+1][quartZ>>2-s.biomePos[1]+1]
		return biomes.index[quartX&3][quartZ&3][quartY-chunkBiomeMinQuartY]
	}
	return s
}

func (w *modernTerrainScratch) biomeInChunk(subIdx, cellIdx int) uint8 {
	if u := w.subUniformBiome[subIdx]; u != 0xFF {
		return u
	}
	return w.biomes[subIdx][cellIdx]
}

func (w *modernTerrainScratch) biomeAt(x, y, z, worldX, worldZ int) uint8 {
	if y < modernMinY || y > modernMaxY {
		return w.lattice.BiomeIndexAt(int32(worldX), int32(y), int32(worldZ))
	}
	subIdx := (y - modernMinY) >> 4
	if u := w.subUniformBiome[subIdx]; u != 0xFF {
		return u
	}
	return w.biomes[subIdx][x<<8|z<<4|((y-modernMinY)&15)]
}

func (w *modernTerrainScratch) setFlatBlock(subIdx, cellIdx int, rid, air uint32) {
	w.blocks[subIdx][cellIdx] = rid
	if rid != air {
		w.subNonAir[subIdx] = true
	}
	if !w.subMixedBlock[subIdx] && rid != w.subUniformBlock[subIdx] {
		w.subMixedBlock[subIdx] = true
	}
}

func (w *modernTerrainScratch) columnHeight(x, z int, air uint32) int {
	colOff := x<<8 | z<<4
	for subIdx := 23; subIdx >= 0; subIdx-- {
		if !w.subNonAir[subIdx] {
			continue
		}
		buf := &w.blocks[subIdx]
		baseY := modernMinY + subIdx*16
		for ly := 15; ly >= 0; ly-- {
			if buf[colOff|ly] != air {
				return baseY + ly + 1
			}
		}
	}
	return modernMinY
}

// terrainColumn has two acyclic stages: structure height queries use raw noise;
// decoration reads surfaced/carved terrain with structure beardification. Neither
// cache contains placed features or live entity handles.
func (g *Overworld) terrainColumn(pos world.ChunkPos, raw bool) *previewCacheEntry {
	cache := g.previewCache
	if raw {
		cache = g.rawCache
	}
	return cache.load(pos, func() *previewCacheEntry {
		return g.buildTerrainColumn(pos, raw)
	})
}

func (g *Overworld) acquireTerrainScratch() *modernTerrainScratch {
	g.terrainScratchMu.Lock()
	if n := len(g.terrainScratchFree); n > 0 {
		work := g.terrainScratchFree[n-1]
		g.terrainScratchFree[n-1] = nil
		g.terrainScratchFree = g.terrainScratchFree[:n-1]
		g.terrainScratchMu.Unlock()
		return work
	}
	g.terrainScratchMu.Unlock()
	return newModernTerrainScratch()
}

func (g *Overworld) releaseTerrainScratch(work *modernTerrainScratch) {
	work.biomeChunks = [3][3]*chunkBiomes{}
	limit := runtime.GOMAXPROCS(0) + 2
	g.terrainScratchMu.Lock()
	if len(g.terrainScratchFree) < limit {
		g.terrainScratchFree = append(g.terrainScratchFree, work)
	}
	g.terrainScratchMu.Unlock()
}

func (g *Overworld) clearTerrainScratchPool() {
	g.terrainScratchMu.Lock()
	clear(g.terrainScratchFree)
	g.terrainScratchFree = nil
	g.terrainScratchMu.Unlock()
}

func (g *Overworld) buildTerrainColumn(pos world.ChunkPos, raw bool) *previewCacheEntry {
	work := g.acquireTerrainScratch()
	defer g.releaseTerrainScratch(work)
	baseX, baseZ := pos[0]*16, pos[1]*16
	var biomes vanilla.QuartBiomes
	if !raw {
		g.loadQuartBiomes(pos, &work.biomeChunks)
		work.biomePos = pos
		biomes = work.quartBiomeFn
		computeSubUniformBiomes(work)
	}
	work.lattice.Prepare(g.noise, g.zoomSeed, baseX, baseZ, biomes)
	work.aquifer.Prepare(g.noise, baseX, baseZ)
	c := chunk.New(world.DefaultBlockRegistry, cube.Range{int(modernMinY), int(modernMaxY)})
	entry := &previewCacheEntry{pos: pos, c: c}
	var terrainResult world.GenerationResult
	var beardMinY, beardMaxY int32 = 1, 0
	if !raw {
		entry.structures = g.modernStructures.Prepare(pos)
		beardMinY, beardMaxY = beardifierYBounds(entry.structures, pos)
	}
	g.fillBaseTerrain(work, c, entry.structures, baseX, baseZ, beardMinY, beardMaxY, raw, &terrainResult)
	if !raw {
		fillChunkBiomes(c, work, baseX, baseZ)
		g.modernSurface.applyFlat(pos, work, c.BlockRegistry(), g.airRID, g.waterRID, g.lavaRID)
		flushBlocksToChunk(c, work, g.airRID)
		carved := g.modernFeatures.Carve(pos, c, modernCarverFluid{
			aquifer:  &work.aquifer,
			surface:  g.modernSurface,
			lattice:  &work.lattice,
			chunk:    c,
			pos:      pos,
			airRID:   g.airRID,
			waterRID: g.waterRID,
			lavaRID:  g.lavaRID,
		})
		appendPostProcessing(&terrainResult, carved.PostProcessing)
		entry.postProcessing = terrainResult.PostProcessing
	} else {
		flushBlocksToChunk(c, work, g.airRID)
	}
	c.Compact()
	return entry
}

func (g *Overworld) fillBaseTerrain(work *modernTerrainScratch, c *chunk.Chunk, structures *modernStructureContext, baseX, baseZ, beardMinY, beardMaxY int32, raw bool, terrainResult *world.GenerationResult) {
	materials := [...]uint32{g.stoneRID, g.airRID, g.waterRID, g.lavaRID}
	for x := range 16 {
		for z := range 16 {
			work.heights[x+1][z+1] = modernMinY
		}
	}
	hasBeard := beardMinY <= beardMaxY
	r := c.Range()
	for subIdx := 23; subIdx >= 0; subIdx-- {
		subMinY := int32(modernMinY) + int32(subIdx)*16
		subMaxY := subMinY + 15
		subBeard := hasBeard && subMaxY >= beardMinY && subMinY <= beardMaxY
		blkBuf := &work.blocks[subIdx]
		var firstRID uint32
		mixed, nonAir := false, false
		for z := range int32(16) {
			worldZ := baseZ + z
			for x := range int32(16) {
				worldX := baseX + x
				colOff := int(x)<<8 | int(z)<<4
				colH := work.heights[x+1][z+1]
				for ly := int32(15); ly >= 0; ly-- {
					y := subMinY + ly
					density := work.lattice.Sample(x, y, z)
					if subBeard && y >= beardMinY && y <= beardMaxY {
						density += structures.AdjustDensity(worldX, y, worldZ)
					}
					material, schedule := work.aquifer.Substance(worldX, y, worldZ, float64(density))
					rid := materials[material]
					blkBuf[colOff|int(ly)] = rid
					if rid != g.airRID {
						nonAir = true
						if colH == modernMinY {
							colH = int16(y + 1)
						}
					}
					if !mixed {
						if z == 0 && x == 0 && ly == 15 {
							firstRID = rid
						} else if rid != firstRID {
							mixed = true
						}
					}
					if !raw && schedule && (material == vanilla.AquiferWater || material == vanilla.AquiferLava) {
						terrainResult.MarkForPostProcessing(cube.Pos{int(worldX), int(y), int(worldZ)}, r)
					}
				}
				work.heights[x+1][z+1] = colH
			}
		}
		work.subNonAir[subIdx] = nonAir
		work.subMixedBlock[subIdx] = mixed
		work.subUniformBlock[subIdx] = firstRID
	}
}

func computeSubUniformBiomes(work *modernTerrainScratch) {
	for subIdx := range 24 {
		qyStart := max(0, subIdx*4-1)
		qyEnd := min(chunkBiomeQuartsY-1, subIdx*4+4)
		first := work.biomeChunks[1][1].index[0][0][qyStart]
		uniform := true
		for dx := range 6 {
			cx, lx := 1, dx-1
			switch dx {
			case 0:
				cx, lx = 0, 3
			case 5:
				cx, lx = 2, 0
			}
			for dz := range 6 {
				cz, lz := 1, dz-1
				switch dz {
				case 0:
					cz, lz = 0, 3
				case 5:
					cz, lz = 2, 0
				}
				col := &work.biomeChunks[cx][cz].index[lx][lz]
				for qy := qyStart; qy <= qyEnd; qy++ {
					if col[qy] != first {
						uniform = false
						break
					}
				}
				if !uniform {
					break
				}
			}
			if !uniform {
				break
			}
		}
		if uniform {
			work.subUniformBiome[subIdx] = first
		} else {
			work.subUniformBiome[subIdx] = 0xFF
		}
	}
}

func biomeChunkCoord(c int) (chunkIdx, localIdx int) {
	if c == 0 {
		return 0, 3
	}
	if c == 5 {
		return 2, 0
	}
	return 1, c - 1
}

func fillSubchunkBiomes(storage *chunk.PalettedStorage, work *modernTerrainScratch, subIdx int, baseX, baseZ int32) {
	subMinY := int32(modernMinY) + int32(subIdx)*16
	buf := &work.biomes[subIdx]
	var domIdx, firstVal uint8
	domVotes, allSame, firstSeen := 0, true, false
	vote := func(b uint8, n int) {
		if !firstSeen {
			firstVal, firstSeen = b, true
		} else if b != firstVal {
			allSame = false
		}
		switch {
		case domVotes == 0:
			domIdx, domVotes = b, n
		case domIdx == b:
			domVotes += n
		case domVotes >= n:
			domVotes -= n
		default:
			domIdx, domVotes = b, n-domVotes
		}
	}
	for cx := range 5 {
		xLo, xHi := max(0, cx*4-2), min(15, cx*4+1)
		c0x, l0x := biomeChunkCoord(cx)
		c1x, l1x := biomeChunkCoord(cx + 1)
		for cz := range 5 {
			zLo, zHi := max(0, cz*4-2), min(15, cz*4+1)
			c0z, l0z := biomeChunkCoord(cz)
			c1z, l1z := biomeChunkCoord(cz + 1)
			col00 := &work.biomeChunks[c0x][c0z].index[l0x][l0z]
			col01 := &work.biomeChunks[c0x][c1z].index[l0x][l1z]
			col10 := &work.biomeChunks[c1x][c0z].index[l1x][l0z]
			col11 := &work.biomeChunks[c1x][c1z].index[l1x][l1z]
			for cy := range 5 {
				yLo, yHi := max(0, cy*4-2), min(15, cy*4+1)
				qy0 := max(0, min(subIdx*4+cy-1, chunkBiomeQuartsY-1))
				qy1 := max(0, min(subIdx*4+cy, chunkBiomeQuartsY-1))
				b := col00[qy0]
				if col00[qy1] == b && col01[qy0] == b && col01[qy1] == b &&
					col10[qy0] == b && col10[qy1] == b && col11[qy0] == b && col11[qy1] == b {
					vote(b, (xHi-xLo+1)*(zHi-zLo+1)*(yHi-yLo+1))
					for x := xLo; x <= xHi; x++ {
						for z := zLo; z <= zHi; z++ {
							off := x<<8 | z<<4
							for ly := yLo; ly <= yHi; ly++ {
								buf[off|ly] = b
							}
						}
					}
					continue
				}
				for x := xLo; x <= xHi; x++ {
					wx := baseX + int32(x)
					for z := zLo; z <= zHi; z++ {
						wz := baseZ + int32(z)
						off := x<<8 | z<<4
						for ly := yLo; ly <= yHi; ly++ {
							idx := work.lattice.BiomeIndexAt(wx, subMinY+int32(ly), wz)
							buf[off|ly] = idx
							vote(idx, 1)
						}
					}
				}
			}
		}
	}
	domBID := modernBiomeIDs[domIdx]
	storage.Palette().Replace(func(uint32) uint32 { return domBID })
	if allSame {
		return
	}
	for x := range uint8(16) {
		for z := range uint8(16) {
			off := int(x)<<8 | int(z)<<4
			for ly := range uint8(16) {
				if idx := buf[off|int(ly)]; idx != domIdx {
					storage.Set(x, ly, z, modernBiomeIDs[idx])
				}
			}
		}
	}
}

func fillChunkBiomes(c *chunk.Chunk, work *modernTerrainScratch, baseX, baseZ int32) {
	storages := c.BiomeStorages()
	for subIdx := range 24 {
		storage := storages[subIdx]
		if u := work.subUniformBiome[subIdx]; u != 0xFF {
			bid := modernBiomeIDs[u]
			if storage.Palette().Len() == 1 && storage.At(0, 0, 0) == bid {
				continue
			}
			// Clone before mutating because Chunk.BiomeStorages may share copy-on-write storages.
			storage = storage.Clone()
			storages[subIdx] = storage
			storage.Palette().Replace(func(uint32) uint32 { return bid })
			continue
		}
		// Clone before mutating because Chunk.BiomeStorages may share copy-on-write storages.
		storage = storage.Clone()
		storages[subIdx] = storage
		fillSubchunkBiomes(storage, work, subIdx, baseX, baseZ)
	}
}

func flushBlocksToChunk(c *chunk.Chunk, work *modernTerrainScratch, airRID uint32) {
	subs := c.Sub()
	for subIdx := range 24 {
		if !work.subNonAir[subIdx] {
			continue
		}
		blkBuf := &work.blocks[subIdx]
		if !work.subMixedBlock[subIdx] {
			rid := work.subUniformBlock[subIdx]
			if rid != airRID {
				subs[subIdx].Layer(0).Palette().Replace(func(uint32) uint32 { return rid })
			}
			continue
		}
		var rid1, rid2 uint32
		airCount := 0
		distinctNonAir := 0
		for _, rid := range blkBuf {
			if rid == airRID {
				airCount++
				continue
			}
			if distinctNonAir == 0 {
				rid1, distinctNonAir = rid, 1
			} else if rid != rid1 {
				if distinctNonAir == 1 {
					rid2, distinctNonAir = rid, 2
				} else if rid != rid2 {
					distinctNonAir = 3
					break
				}
			}
		}
		if distinctNonAir == 0 {
			continue
		}
		if airCount == 0 && distinctNonAir == 1 {
			subs[subIdx].Layer(0).Palette().Replace(func(uint32) uint32 { return rid1 })
			continue
		}
		if distinctNonAir == 1 && airCount < 2048 {
			storage := subs[subIdx].Layer(0)
			storage.Palette().Replace(func(uint32) uint32 { return rid1 })
			for x := range uint8(16) {
				for z := range uint8(16) {
					off := int(x)<<8 | int(z)<<4
					for ly := range uint8(16) {
						if blkBuf[off|int(ly)] == airRID {
							storage.Set(x, ly, z, airRID)
						}
					}
				}
			}
			continue
		}
		var storage *chunk.PalettedStorage
		if distinctNonAir <= 2 {
			if distinctNonAir == 1 {
				rid2 = rid1
			}
			storage, _, _ = c.LayerStorageWithTwoRuntimeIDs(int16(modernMinY+subIdx*16), 0, rid1, rid2)
		} else {
			storage = subs[subIdx].Layer(0)
		}
		for x := range uint8(16) {
			for z := range uint8(16) {
				off := int(x)<<8 | int(z)<<4
				for ly := range uint8(16) {
					if rid := blkBuf[off|int(ly)]; rid != airRID {
						storage.Set(x, ly, z, rid)
					}
				}
			}
		}
	}
}

func beardifierYBounds(ctx *modernStructureContext, pos world.ChunkPos) (int32, int32) {
	if ctx == nil || len(ctx.starts) == 0 {
		return 1, 0
	}
	chunkMinX, chunkMinZ := pos[0]*16, pos[1]*16
	chunkMaxX, chunkMaxZ := chunkMinX+15, chunkMinZ+15
	minY, maxY := int32(math.MaxInt32), int32(math.MinInt32)
	for i := range ctx.starts {
		start := &ctx.starts[i]
		for _, piece := range start.rigid {
			box := piece.Bounds
			if box.Max[0] < chunkMinX-12 || box.Min[0] > chunkMaxX+12 || box.Max[2] < chunkMinZ-12 || box.Min[2] > chunkMaxZ+12 {
				continue
			}
			groundY := box.Min[1] + piece.GroundLevelDelta
			switch piece.Adjustment {
			case structure.TerrainBury, structure.TerrainBeardThin:
				minY, maxY = min(minY, groundY-12), max(maxY, groundY+12)
			case structure.TerrainBeardBox:
				minY, maxY = min(minY, groundY-12), max(maxY, box.Max[1]+12)
			case structure.TerrainEncapsulate:
				minY, maxY = min(minY, box.Min[1]-12), max(maxY, box.Max[1]+12)
			}
		}
		for _, junction := range start.junctions {
			if junction.X > chunkMinX-12 && junction.Z > chunkMinZ-12 && junction.X < chunkMaxX+12 && junction.Z < chunkMaxZ+12 {
				minY, maxY = min(minY, junction.Y-12), max(maxY, junction.Y+12)
			}
		}
	}
	return minY, maxY
}

func appendPostProcessing(result *world.GenerationResult, markers [][]uint16) {
	if len(result.PostProcessing) < len(markers) {
		result.PostProcessing = append(result.PostProcessing, make([][]uint16, len(markers)-len(result.PostProcessing))...)
	}
	for section := range len(markers) {
		if result.PostProcessing[section] == nil {
			result.PostProcessing[section] = append([]uint16{}, markers[section]...)
			continue
		}
		result.PostProcessing[section] = append(result.PostProcessing[section], markers[section]...)
	}
}

// modernTerrainSource separates generator base-column queries from the mutable
// WorldGenRegion used while replaying decoration stages.
type modernTerrainSource struct {
	generator *Overworld
	raw       bool
}

func (modernTerrainSource) UseLegacyRandomSource() bool { return false }

func (modernTerrainSource) HasSkyLight() bool { return true }

func (modernTerrainSource) GenerationDepth() int32 { return modernMaxY - modernMinY + 1 }

func (modernTerrainSource) FluidTickDelay(name string) int32 {
	switch name {
	case "minecraft:water":
		return 5
	case "minecraft:lava":
		return 30
	default:
		panic("overworld: unsupported generation fluid tick " + name)
	}
}

func (modernTerrainSource) SeaLevel() int32 { return modernSeaLevel }

func (s modernTerrainSource) FeatureBiomeNames() []string {
	return modernBiomeNames[:]
}

// NoiseBiomeAt reads the noise biome from the biomes of its chunk when
// the decoration of the chunks around it sampled them, and samples it alone
// otherwise, as for the scattered positions of structure placement.
func (s modernTerrainSource) NoiseBiomeAt(quartX, quartY, quartZ int32) string {
	return vanilla.OverworldBiomeName(s.noiseBiomeIndexAt(quartX, quartY, quartZ, nil))
}

func (s modernTerrainSource) noiseBiomeIndexAt(quartX, quartY, quartZ int32, hint *vanilla.ClimateHint) uint8 {
	quartY = max(-16, min(quartY, 79))
	pos := world.ChunkPos{quartX >> 2, quartZ >> 2}
	if biomes, ok := s.generator.chunkBiomes.get(pos); ok {
		return biomes.index[quartX&3][quartZ&3][quartY-chunkBiomeMinQuartY]
	}
	if quartY == 0 && quartX&3 == 0 && quartZ&3 == 0 {
		return s.generator.chunkBiomes.originBiome(pos, s.generator.noise)
	}
	return s.generator.noise.SampleBiomeColumn(quartX*4, quartZ*4).BiomeIndexNear(quartY*4, hint)
}

func (s modernTerrainSource) BiomeAt(x, y, z int32) string {
	px, py, pz := (x-2)>>2, (y-2)>>2, (z-2)>>2
	if px&3 < 3 && pz&3 < 3 {
		if biomes, ok := s.generator.chunkBiomes.get(world.ChunkPos{px >> 2, pz >> 2}); ok {
			lx, lz := px&3, pz&3
			ly0 := max(-16, min(py, 79)) - chunkBiomeMinQuartY
			ly1 := max(-16, min(py+1, 79)) - chunkBiomeMinQuartY
			first := biomes.index[lx][lz][ly0]
			if biomes.index[lx][lz][ly1] == first &&
				biomes.index[lx][lz+1][ly0] == first &&
				biomes.index[lx][lz+1][ly1] == first &&
				biomes.index[lx+1][lz][ly0] == first &&
				biomes.index[lx+1][lz][ly1] == first &&
				biomes.index[lx+1][lz+1][ly0] == first &&
				biomes.index[lx+1][lz+1][ly1] == first {
				return vanilla.OverworldBiomeName(first)
			}
		}
	}
	qx, qy, qz := vanilla.FiddledBiomeQuart(s.generator.zoomSeed, x, y, z)
	return s.NoiseBiomeAt(qx, qy, qz)
}

func (s modernTerrainSource) BaseState(x, y, z int32) world.NativeState {
	if y < modernMinY || y > modernMaxY {
		return world.NativeState{Block: s.generator.airRID, Liquid: s.generator.airRID}
	}
	entry := s.generator.terrainColumn(world.ChunkPos{x >> 4, z >> 4}, s.raw)
	return world.NativeStateAt(entry.c, uint8(x&15), int16(y), uint8(z&15))
}

// BaseChunk returns the chunk holding the base states of the chunk at pos
// within the build height.
func (s modernTerrainSource) BaseChunk(pos world.ChunkPos) *chunk.Chunk {
	return s.generator.terrainColumn(pos, s.raw).c
}

func (s modernTerrainSource) BaseBlock(x, y, z int32) world.Block {
	rid := s.BaseState(x, y, z).Block
	b, ok := world.BlockByRuntimeID(rid)
	if !ok {
		panic(fmt.Sprintf("overworld: invalid base runtime ID %d at %d,%d,%d", rid, x, y, z))
	}
	return b
}

func (s modernTerrainSource) BaseHeight(x, z int32, oceanFloor bool) int32 {
	entry := s.generator.terrainColumn(world.ChunkPos{x >> 4, z >> 4}, s.raw)
	lx, lz := uint8(x&15), uint8(z&15)
	for y := int32(entry.c.HighestBlock(lx, lz)); y >= modernMinY; y-- {
		rid := entry.c.Block(lx, int16(y), lz, 0)
		if rid == s.generator.airRID {
			continue
		}
		if !oceanFloor || s.generator.blocksMotionInHeightmap(rid) {
			return y + 1
		}
	}
	return modernMinY
}

func (s modernTerrainSource) PlaceStructureStep(pos world.ChunkPos, step int, env structure.ProcessorEnvironment, emit func(structure.Placement)) {
	entry := s.generator.terrainColumn(pos, false)
	entry.structures.PlaceStep(pos, entry.c.Range(), step, env, emit)
}

func (g *Overworld) blocksMotionInHeightmap(rid uint32) bool {
	if rid == g.stoneRID {
		return true
	}
	if rid == g.airRID || rid == g.waterRID || rid == g.lavaRID {
		return false
	}
	if value, ok := g.motionBlocking.Load(rid); ok {
		return value.(bool)
	}
	state, err := world.CanonicalState(world.DefaultBlockRegistry, world.NativeState{Block: rid, Liquid: g.airRID})
	if err != nil {
		panic(err)
	}
	matches, err := gamedata.BlockTagContains("minecraft:blocks_motion_in_heightmap", state.Name)
	if err != nil {
		panic(err)
	}
	g.motionBlocking.Store(rid, matches)
	return matches
}

type modernCarverFluid struct {
	aquifer  *vanilla.OverworldAquifer
	surface  *modernSurface
	lattice  *vanilla.OverworldChunk
	chunk    *chunk.Chunk
	pos      world.ChunkPos
	airRID   uint32
	waterRID uint32
	lavaRID  uint32
}

func (a modernCarverFluid) CarveSubstance(x, y, z int32, density float32) (world.Block, bool) {
	material, schedule := a.aquifer.Substance(x, y, z, float64(density))
	switch material {
	case vanilla.AquiferSolid:
		return nil, false
	case vanilla.AquiferAir:
		return block.Air{}, false
	case vanilla.AquiferWater:
		return block.Water{Depth: 8, Still: true}, schedule
	case vanilla.AquiferLava:
		return block.Lava{Depth: 8, Still: true}, schedule
	default:
		panic("overworld: invalid aquifer material")
	}
}

func (a modernCarverFluid) CarveTopMaterial(x, y, z int32, hasFluid bool) (world.Block, bool) {
	rid, matched := a.surface.TopMaterial(a.pos, a.chunk, a.lattice, x, y, z, hasFluid)
	if !matched {
		return nil, false
	}
	material, ok := world.BlockByRuntimeID(rid)
	if !ok {
		panic(fmt.Sprintf("overworld: invalid carver top material runtime ID %d", rid))
	}
	return material, true
}

func (a modernCarverFluid) carveNativeSubstance(x, y, z int32, density float32) (rid uint32, isFluid, schedule, ok bool) {
	material, sched := a.aquifer.Substance(x, y, z, float64(density))
	switch material {
	case vanilla.AquiferSolid:
		return 0, false, false, false
	case vanilla.AquiferAir:
		return a.airRID, false, false, true
	case vanilla.AquiferWater:
		return a.waterRID, true, sched, true
	case vanilla.AquiferLava:
		return a.lavaRID, true, sched, true
	default:
		panic("overworld: invalid aquifer material")
	}
}

func (a modernCarverFluid) carveNativeTopMaterial(x, y, z int32, hasFluid bool) (uint32, bool) {
	return a.surface.TopMaterial(a.pos, a.chunk, a.lattice, x, y, z, hasFluid)
}
