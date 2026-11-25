package nether

import (
	"log/slog"
	"math/bits"
	"sync"
	"sync/atomic"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/world"
	dfbiome "github.com/df-mc/dragonfly/server/world/biome"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/structure"
	overworldFeatures "github.com/df-mc/dragonfly/server/world/generator/advanced/overworld"
)

const netherSeaLevel = 32

const (
	nbAir uint8 = iota
	nbNetherrack
	nbLavaStill
	nbBedrock
	nbGravel
	nbSoulSand
	nbSoulSoil
	nbBasalt
	nbBlackstone
	nbCrimsonNylium
	nbWarpedNylium
	nbNetherWartBlock
	nbWarpedWartBlock
	nbCount
)

// Nether generates Java Edition 26.3 Nether chunks.
//
// The implementation is concurrency-safe and may be called from multiple generator workers.
type Nether struct {
	seed int64

	airRID               uint32
	netherrackRID        uint32
	bedrockRID           uint32
	lavaStillRID         uint32
	lavaFlowRID          uint32
	gravelRID            uint32
	soulSandRID          uint32
	soulSoilRID          uint32
	basaltRID            uint32
	blackstoneRID        uint32
	crimsonNyliumRID     uint32
	warpedNyliumRID      uint32
	netherWartBlockRID   uint32
	warpedWartBlockRID   uint32
	netherBricksRID      uint32
	netherBrickFenceRIDs [16]uint32
	netherWartRID        uint32
	netherBrickStairsRID [4]uint32
	spawnerRID           uint32
	chestRID             uint32

	biomeIDs      [5]uint32
	biomeZoomSeed int64
	biomes        *vanilla.NetherBiomes
	density       *vanilla.NetherDensity
	material      *vanilla.NetherMaterialNoise

	caves         *netherCaves
	features      *overworldFeatures.ModernFeatures
	featureSource *netherFeatureSource

	complexMu      sync.Mutex
	complexStarts  map[world.ChunkPos]netherComplexResult
	complexFlights map[world.ChunkPos]*netherComplexFlight
	fortressStates fortressStateStore
	fossils        netherFossilCache
	portals        netherPortalCache
	pool           sync.Pool
	hooks          atomic.Pointer[Hooks]
}

type netherScratch struct {
	vertices [425]float32
	blocks   [16 * 16 * 128]uint8
	biomes   netherChunkBiomes
}

// New creates a Nether generator using the seed provided.
func New(seed int64) *Nether {
	return NewNether(seed)
}

// NewNether creates a Nether generator using the seed provided.
func NewNether(seed int64) *Nether {
	g := &Nether{
		seed:               seed,
		complexStarts:      make(map[world.ChunkPos]netherComplexResult),
		complexFlights:     make(map[world.ChunkPos]*netherComplexFlight),
		airRID:             world.BlockRuntimeID(block.Air{}),
		netherrackRID:      world.BlockRuntimeID(block.Netherrack{}),
		bedrockRID:         world.BlockRuntimeID(block.Bedrock{}),
		lavaStillRID:       world.BlockRuntimeID(block.Lava{Depth: 8, Still: true}),
		lavaFlowRID:        world.BlockRuntimeID(block.Lava{Depth: 8, Still: false}),
		gravelRID:          world.BlockRuntimeID(block.Gravel{}),
		soulSandRID:        world.BlockRuntimeID(block.SoulSand{}),
		soulSoilRID:        world.BlockRuntimeID(block.SoulSoil{}),
		basaltRID:          world.BlockRuntimeID(block.Basalt{}),
		blackstoneRID:      world.BlockRuntimeID(block.Blackstone{Type: block.NormalBlackstone()}),
		crimsonNyliumRID:   world.BlockRuntimeID(block.Nylium{}),
		warpedNyliumRID:    world.BlockRuntimeID(block.Nylium{Warped: true}),
		netherWartBlockRID: world.BlockRuntimeID(block.NetherWartBlock{}),
		warpedWartBlockRID: world.BlockRuntimeID(block.NetherWartBlock{Warped: true}),
		netherBricksRID:    world.BlockRuntimeID(block.NetherBricks{}),
		netherWartRID:      world.BlockRuntimeID(block.NetherWart{Age: 0}),
		biomeIDs: [5]uint32{
			uint32(dfbiome.NetherWastes{}.EncodeBiome()),
			uint32(dfbiome.SoulSandValley{}.EncodeBiome()),
			uint32(dfbiome.CrimsonForest{}.EncodeBiome()),
			uint32(dfbiome.WarpedForest{}.EncodeBiome()),
			uint32(dfbiome.BasaltDeltas{}.EncodeBiome()),
		},
		biomeZoomSeed: vanilla.BiomeZoomSeed(seed),
		biomes:        vanilla.NewNetherBiomes(seed),
		density:       vanilla.NewNetherDensity(seed),
		material:      vanilla.NewNetherMaterialNoise(seed),
	}
	g.initLookupTables()
	for mask := range g.netherBrickFenceRIDs {
		g.netherBrickFenceRIDs[mask] = world.BlockRuntimeID(block.NetherBrickFence{
			NorthConnection: mask&1 != 0, SouthConnection: mask&2 != 0,
			WestConnection: mask&4 != 0, EastConnection: mask&8 != 0,
		})
	}

	if b, ok := world.BlockByName("minecraft:mob_spawner", nil); ok {
		g.spawnerRID = world.BlockRuntimeID(b)
	}

	g.chestRID = world.BlockRuntimeID(block.Chest{Facing: cube.North})
	for _, dir := range []cube.Direction{cube.North, cube.South, cube.West, cube.East} {
		g.netherBrickStairsRID[dir] = world.BlockRuntimeID(block.Stairs{Block: block.NetherBricks{}, Facing: dir})
	}

	g.caves = newNetherCaves(seed, g)
	g.pool.New = func() any { return &netherScratch{} }
	g.featureSource = newNetherFeatureSource(g)
	g.features = overworldFeatures.NewModernFeatures(seed, g.featureSource)
	return g
}

func (g *Nether) initLookupTables() {
	tables := &g.fossils.tables
	tables.blockRIDs = [nbCount]uint32{
		nbAir:             g.airRID,
		nbNetherrack:      g.netherrackRID,
		nbLavaStill:       g.lavaStillRID,
		nbBedrock:         g.bedrockRID,
		nbGravel:          g.gravelRID,
		nbSoulSand:        g.soulSandRID,
		nbSoulSoil:        g.soulSoilRID,
		nbBasalt:          g.basaltRID,
		nbBlackstone:      g.blackstoneRID,
		nbCrimsonNylium:   g.crimsonNyliumRID,
		nbWarpedNylium:    g.warpedNyliumRID,
		nbNetherWartBlock: g.netherWartBlockRID,
		nbWarpedWartBlock: g.warpedWartBlockRID,
	}
	for kind, rid := range tables.blockRIDs {
		if name, _, ok := world.DefaultBlockRegistry.RuntimeIDToState(rid); ok {
			_, tables.blockUncarvable[kind] = netherCaveUncarvableBlocks[name]
		}
	}
	biomeNames := [5]string{
		"minecraft:nether_wastes",
		"minecraft:soul_sand_valley",
		"minecraft:crimson_forest",
		"minecraft:warped_forest",
		"minecraft:basalt_deltas",
	}
	for i, name := range biomeNames {
		allowed, err := gamedata.BiomeTagContains("#minecraft:has_structure/nether_fossil", name)
		if err != nil {
			panic(err)
		}
		tables.fossilAllowedBiomes[i] = allowed
		for col := range tables.uniformBiomes[i] {
			tables.uniformBiomes[i][col] = g.biomeIDs[i]
		}
	}
}

// SetLog sets where the structures of the generator log. It must be called
// before the first chunk is generated.
func (g *Nether) SetLog(log *slog.Logger) { g.features.SetLog(log) }

// TuneGeneration sizes the caches of the generator for the workers of the
// World and sets whether a chunk computes its neighbourhood in parallel.
func (g *Nether) TuneGeneration(tuning world.GenerationTuning) {
	g.featureSource.terrain = newNetherTerrainCache(max(2048, 128*tuning.Workers))
	g.features.Tune(tuning.Workers, tuning.Parallel)
}

// GenerateChunk generates a single Nether chunk at the position passed.
func (g *Nether) GenerateChunk(pos world.ChunkPos, c *chunk.Chunk, context world.GenerationContext) world.GenerationResult {
	chunkX, chunkZ := int(pos[0]), int(pos[1])

	scratch := g.pool.Get().(*netherScratch)
	biomesReady := g.prepareTerrainWithScratch(pos, c, scratch)
	if biomesReady {
		g.fillBiomesWith(chunkX, chunkZ, c, &scratch.biomes)
	} else {
		g.fillBiomes(chunkX, chunkZ, c)
	}
	g.pool.Put(scratch)

	hooks := g.hooks.Load()
	if hooks != nil {
		applyPreDecorationHooks(pos, c, hooks)
	}

	result := world.GenerationResult{}
	g.featureSource.remember(pos, c)
	decoration := g.features.Decorate(pos, c, context)
	result.Entities = append(result.Entities, decoration.Entities...)
	result.BlockEntities = append(result.BlockEntities, decoration.BlockEntities...)
	result.ScheduledTicks = append(result.ScheduledTicks, decoration.ScheduledTicks...)
	result.PostProcessing = decoration.PostProcessing
	if hooks != nil && hooks.OnDecorate != nil {
		rng := decorationHookRandom(g.seed, pos)
		hooks.OnDecorate(pos, c, &rng)
	}
	// NoiseGeneratorSettings.nether does not disable mob generation.
	result.SpawnOriginalMobs = true
	return result
}

// prepareTerrain is shared by published chunks and decoration's neighboring
// terrain views. Raw-column queries deliberately bypass these structure,
// material and carving stages.
func (g *Nether) prepareTerrain(pos world.ChunkPos, c *chunk.Chunk) {
	scratch := g.pool.Get().(*netherScratch)
	g.prepareTerrainWithScratch(pos, c, scratch)
	g.pool.Put(scratch)
}

func (g *Nether) prepareTerrainWithScratch(pos world.ChunkPos, c *chunk.Chunk, scratch *netherScratch) bool {
	chunkX, chunkZ := int(pos[0]), int(pos[1])
	beard := g.fossilBeard(pos)
	if c.Range().Min() == 0 && c.Range().Max() >= 127 {
		g.prepareHeightsGrid(chunkX, chunkZ, scratch, beard)
		g.applyNetherMaterialGrid(chunkX, chunkZ, scratch)
		g.caves.generateGrid(chunkX, chunkZ, &scratch.blocks)
		g.flushGridToChunk(c, &scratch.blocks)
		return true
	}
	g.prepareHeights(chunkX, chunkZ, c, scratch, beard)
	g.applyNetherMaterial(chunkX, chunkZ, c)
	g.carveCaves(chunkX, chunkZ, c)
	return false
}

// DefaultSpawn returns the default Nether spawn position.
func (*Nether) DefaultSpawn(world.Dimension) cube.Pos {
	return cube.Pos{0, netherSeaLevel + 2, 0}
}

func (g *Nether) fillBiomes(chunkX, chunkZ int, c *chunk.Chunk) {
	var biomes netherChunkBiomes
	biomes.init(g, chunkX, chunkZ)
	g.fillBiomesWith(chunkX, chunkZ, c, &biomes)
}

func (g *Nether) fillBiomesWith(chunkX, chunkZ int, c *chunk.Chunk, biomes *netherChunkBiomes) {
	if biomes.uniform {
		c.FillBiomes2D(g.fossils.tables.uniformBiomes[biomes.uniformIndex][:])
		return
	}
	if biomes.allColumnsUniform {
		var grid [256]uint32
		baseX, baseZ := int32(chunkX*16), int32(chunkZ*16)
		for z := uint8(0); z < 16; z++ {
			pz := int(((baseZ + int32(z) - 2) >> 2) - biomes.minQuartZ)
			rowBase := int(z) << 4
			for x := uint8(0); x < 16; x++ {
				px := int(((baseX + int32(x) - 2) >> 2) - biomes.minQuartX)
				grid[rowBase|int(x)] = g.biomeIDs[biomes.indices[px][pz]]
			}
		}
		c.FillBiomes2D(grid[:])
		return
	}
	minY, maxY := int16(c.Range().Min()), int16(c.Range().Max())
	baseX, baseZ := int32(chunkX*16), int32(chunkZ*16)
	for x := uint8(0); x < 16; x++ {
		worldX := baseX + int32(x)
		px := int(((worldX - 2) >> 2) - biomes.minQuartX)
		for z := uint8(0); z < 16; z++ {
			worldZ := baseZ + int32(z)
			pz := int(((worldZ - 2) >> 2) - biomes.minQuartZ)
			if biomes.cellUniform[px][pz] {
				biomeID := g.biomeIDs[biomes.indices[px][pz]]
				for y := minY; y <= maxY; y++ {
					c.SetBiome(x, y, z, biomeID)
				}
				continue
			}
			for y := minY; y <= maxY; y++ {
				index := biomes.fiddledIndex(worldX, int32(y), worldZ)
				c.SetBiome(x, y, z, g.biomeIDs[index])
			}
		}
	}
}

func netherBiomeIndex(name string) int {
	switch name {
	case "minecraft:soul_sand_valley":
		return 1
	case "minecraft:crimson_forest":
		return 2
	case "minecraft:warped_forest":
		return 3
	case "minecraft:basalt_deltas":
		return 4
	default:
		return 0
	}
}

type netherChunkBiomes struct {
	g                 *Nether
	minQuartX         int32
	minQuartZ         int32
	uniformIndex      uint8
	uniform           bool
	allColumnsUniform bool
	indices           [6][6]uint8
	cellUniform       [5][5]bool
}

func newNetherChunkBiomes(g *Nether, chunkX, chunkZ int) netherChunkBiomes {
	var b netherChunkBiomes
	b.init(g, chunkX, chunkZ)
	return b
}

func (b *netherChunkBiomes) init(g *Nether, chunkX, chunkZ int) {
	b.g = g
	b.minQuartX = int32(chunkX*4 - 1)
	b.minQuartZ = int32(chunkZ*4 - 1)
	first := uint8(0)
	allSame := true
	for x := int32(0); x < 6; x++ {
		bx := (b.minQuartX + x) * 4
		for z := int32(0); z < 6; z++ {
			idx := uint8(netherBiomeIndex(g.biomes.Biome(bx, (b.minQuartZ+z)*4)))
			b.indices[x][z] = idx
			if x == 0 && z == 0 {
				first = idx
			} else if idx != first {
				allSame = false
			}
		}
	}
	b.uniformIndex = first
	b.uniform = allSame
	if allSame {
		b.allColumnsUniform = true
		return
	}
	allCols := true
	for x := 0; x < 5; x++ {
		for z := 0; z < 5; z++ {
			v := b.indices[x][z]
			u := b.indices[x+1][z] == v && b.indices[x][z+1] == v && b.indices[x+1][z+1] == v
			b.cellUniform[x][z] = u
			if !u {
				allCols = false
			}
		}
	}
	b.allColumnsUniform = allCols
}

func (b *netherChunkBiomes) fiddledIndex(x, y, z int32) int {
	quartX, _, quartZ := vanilla.FiddledBiomeQuart(b.g.biomeZoomSeed, x, y, z)
	return int(b.indices[quartX-b.minQuartX][quartZ-b.minQuartZ])
}

func (b *netherChunkBiomes) index(x, y, z int32) int {
	if b.uniform {
		return int(b.uniformIndex)
	}
	px := int(((x - 2) >> 2) - b.minQuartX)
	pz := int(((z - 2) >> 2) - b.minQuartZ)
	if b.cellUniform[px][pz] {
		return int(b.indices[px][pz])
	}
	return b.fiddledIndex(x, y, z)
}

func fillNetherCell(blocks *[16 * 16 * 128]uint8, cellX, cellY, cellZ int, fill uint8) {
	yBase := cellY * 8
	for dx := range 4 {
		xBase := (cellX*4 + dx) << 11
		for dz := range 4 {
			col := blocks[xBase|((cellZ*4+dz)<<7)|yBase : xBase|((cellZ*4+dz)<<7)|yBase+8]
			col[0], col[1], col[2], col[3] = fill, fill, fill, fill
			col[4], col[5], col[6], col[7] = fill, fill, fill, fill
		}
	}
}

func (g *Nether) prepareHeightsGrid(chunkX, chunkZ int, s *netherScratch, beard *structure.Beardifier) {
	g.density.Fill(int32(chunkX), int32(chunkZ), &s.vertices)
	blocks := &s.blocks
	const cellThreshold = float32(1e-4)
	for cellX := range 4 {
		for cellZ := range 4 {
			for cellY := range 16 {
				base := (cellX*5+cellZ)*17 + cellY
				v000, v001 := s.vertices[base], s.vertices[base+1]
				v010, v011 := s.vertices[base+17], s.vertices[base+18]
				v100, v101 := s.vertices[base+85], s.vertices[base+86]
				v110, v111 := s.vertices[base+102], s.vertices[base+103]
				if beard == nil {
					if v000 >= cellThreshold && v001 >= cellThreshold && v010 >= cellThreshold && v011 >= cellThreshold &&
						v100 >= cellThreshold && v101 >= cellThreshold && v110 >= cellThreshold && v111 >= cellThreshold {
						fillNetherCell(blocks, cellX, cellY, cellZ, nbNetherrack)
						continue
					}
					if v000 <= -cellThreshold && v001 <= -cellThreshold && v010 <= -cellThreshold && v011 <= -cellThreshold &&
						v100 <= -cellThreshold && v101 <= -cellThreshold && v110 <= -cellThreshold && v111 <= -cellThreshold {
						fill := nbAir
						if cellY*8 < netherSeaLevel {
							fill = nbLavaStill
						}
						fillNetherCell(blocks, cellX, cellY, cellZ, fill)
						continue
					}
				}
				negFill := nbAir
				if cellY*8 < netherSeaLevel {
					negFill = nbLavaStill
				}
				for dz := range 4 {
					fz := float32(dz) * 0.25
					z00 := float32(v000 + float32((v010-v000)*fz))
					z01 := float32(v001 + float32((v011-v001)*fz))
					z10 := float32(v100 + float32((v110-v100)*fz))
					z11 := float32(v101 + float32((v111-v101)*fz))
					localZ := cellZ*4 + dz
					for dx := range 4 {
						fx := float32(dx) * 0.25
						density := float32(z00 + float32((z10-z00)*fx))
						top := float32(z01 + float32((z11-z01)*fx))
						localX := cellX*4 + dx
						colBase := (localX << 11) | (localZ << 7) | (cellY * 8)
						if beard == nil {
							if density >= cellThreshold && top >= cellThreshold {
								col := blocks[colBase : colBase+8]
								col[0], col[1], col[2], col[3] = nbNetherrack, nbNetherrack, nbNetherrack, nbNetherrack
								col[4], col[5], col[6], col[7] = nbNetherrack, nbNetherrack, nbNetherrack, nbNetherrack
								continue
							}
							if density <= -cellThreshold && top <= -cellThreshold {
								col := blocks[colBase : colBase+8]
								col[0], col[1], col[2], col[3] = negFill, negFill, negFill, negFill
								col[4], col[5], col[6], col[7] = negFill, negFill, negFill, negFill
								continue
							}
							step := float32((top - density) * 0.125)
							for dy := range 8 {
								value := max(float32(-1), min(float32(1), density))
								value = float32(value/2 - float32(float32(value*value)*value)/24)
								density = float32(density + step)
								if value > 0 {
									blocks[colBase+dy] = nbNetherrack
								} else {
									blocks[colBase+dy] = negFill
								}
							}
						} else {
							step := float32((top - density) * 0.125)
							worldX, worldZ := int32(chunkX*16+localX), int32(chunkZ*16+localZ)
							for dy := range 8 {
								y := cellY*8 + dy
								value := max(float32(-1), min(float32(1), density))
								value = float32(value/2 - float32(float32(value*value)*value)/24)
								value = float32(value + beard.AdjustDensity(worldX, int32(y), worldZ))
								density = float32(density + step)
								if value > 0 {
									blocks[colBase+dy] = nbNetherrack
								} else {
									blocks[colBase+dy] = negFill
								}
							}
						}
					}
				}
			}
		}
	}
}

func (g *Nether) flushGridToChunk(c *chunk.Chunk, blocks *[16 * 16 * 128]uint8) {
	invalidatedHeightMap := false
	for subY := 0; subY < 8; subY++ {
		yBase := subY * 16
		var present uint16
		for x := 0; x < 16; x++ {
			xBase := x << 11
			for z := 0; z < 16; z++ {
				col := blocks[xBase|(z<<7)|yBase : xBase|(z<<7)|yBase+16]
				for _, k := range col {
					present |= 1 << k
				}
			}
		}
		nonAir := present &^ (1 << nbAir)
		if nonAir == 0 {
			continue
		}
		firstKind := uint8(bits.TrailingZeros16(nonAir))
		rem := nonAir & (nonAir - 1)
		secondKind := firstKind
		if rem != 0 {
			secondKind = uint8(bits.TrailingZeros16(rem))
			rem &= rem - 1
		}
		subBaseY := int16(yBase)
		blockRIDs := &g.fossils.tables.blockRIDs
		storage, _, _ := c.LayerStorageWithTwoRuntimeIDs(subBaseY, 0, blockRIDs[firstKind], blockRIDs[secondKind])
		seededOrigin := false
		for rem != 0 {
			k := uint8(bits.TrailingZeros16(rem))
			rem &= rem - 1
			storage.Set(0, 0, 0, blockRIDs[k])
			seededOrigin = true
		}
		// Reset (0,0,0) if we used it to seed extra palette entries and its true value is air.
		if seededOrigin && blocks[yBase] == nbAir {
			storage.Set(0, 0, 0, g.airRID)
		}
		for x := uint8(0); x < 16; x++ {
			xBase := int(x) << 11
			for z := uint8(0); z < 16; z++ {
				col := blocks[xBase|(int(z)<<7)|yBase : xBase|(int(z)<<7)|yBase+16]
				for ly := uint8(0); ly < 16; ly++ {
					k := col[ly]
					if k == nbAir {
						continue
					}
					if !invalidatedHeightMap {
						c.SetBlock(x, subBaseY+int16(ly), z, 0, blockRIDs[k])
						invalidatedHeightMap = true
						continue
					}
					storage.Set(x, ly, z, blockRIDs[k])
				}
			}
		}
	}
}

func (g *Nether) prepareHeights(chunkX, chunkZ int, c *chunk.Chunk, s *netherScratch, beard *structure.Beardifier) {
	g.density.Fill(int32(chunkX), int32(chunkZ), &s.vertices)
	minY, maxY := c.Range().Min(), c.Range().Max()
	for cellX := range 4 {
		for cellZ := range 4 {
			for cellY := range 16 {
				base := (cellX*5+cellZ)*17 + cellY
				v000, v001 := s.vertices[base], s.vertices[base+1]
				v010, v011 := s.vertices[base+17], s.vertices[base+18]
				v100, v101 := s.vertices[base+85], s.vertices[base+86]
				v110, v111 := s.vertices[base+102], s.vertices[base+103]
				for dz := range 4 {
					fz := float32(dz) * 0.25
					z00 := float32(v000 + float32((v010-v000)*fz))
					z01 := float32(v001 + float32((v011-v001)*fz))
					z10 := float32(v100 + float32((v110-v100)*fz))
					z11 := float32(v101 + float32((v111-v101)*fz))
					for dx := range 4 {
						fx := float32(dx) * 0.25
						density := float32(z00 + float32((z10-z00)*fx))
						top := float32(z01 + float32((z11-z01)*fx))
						step := float32((top - density) * 0.125)
						worldX, worldZ := int32(chunkX*16+cellX*4+dx), int32(chunkZ*16+cellZ*4+dz)
						for dy := range 8 {
							y := cellY*8 + dy
							value := max(float32(-1), min(float32(1), density))
							value = float32(value/2 - float32(float32(value*value)*value)/24)
							if beard != nil {
								value = float32(value + beard.AdjustDensity(worldX, int32(y), worldZ))
							}
							density = float32(density + step)
							if y < minY || y > maxY {
								continue
							}
							var rid uint32
							if value > 0 {
								rid = g.netherrackRID
							} else if y < netherSeaLevel {
								rid = g.lavaStillRID
							}
							if rid != 0 {
								c.SetBlock(uint8(cellX*4+dx), int16(y), uint8(cellZ*4+dz), 0, rid)
							}
						}
					}
				}
			}
		}
	}
}
