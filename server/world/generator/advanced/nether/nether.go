package nether

import (
	"sync"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/world"
	dfbiome "github.com/df-mc/dragonfly/server/world/biome"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/structure"
	overworldFeatures "github.com/df-mc/dragonfly/server/world/generator/advanced/overworld"
)

const netherSeaLevel = 32

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
}
type netherScratch struct {
	vertices [425]float32
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

// TuneGeneration sizes the caches of the generator for the workers of the
// World and sets whether a chunk computes its neighbourhood in parallel.
func (g *Nether) TuneGeneration(tuning world.GenerationTuning) {
	g.featureSource.terrain = newNetherTerrainCache(max(2048, 128*tuning.Workers))
	g.features.Tune(tuning.Workers, tuning.Parallel)
}

// GenerateChunk generates a single Nether chunk at the position passed.
func (g *Nether) GenerateChunk(pos world.ChunkPos, c *chunk.Chunk, context world.GenerationContext) world.GenerationResult {
	chunkX, chunkZ := int(pos[0]), int(pos[1])

	g.prepareTerrain(pos, c)
	result := world.GenerationResult{}
	g.fillBiomes(chunkX, chunkZ, c)
	g.featureSource.remember(pos, c)
	decoration := g.features.Decorate(pos, c, context)
	result.Entities = append(result.Entities, decoration.Entities...)
	result.BlockEntities = append(result.BlockEntities, decoration.BlockEntities...)
	result.ScheduledTicks = append(result.ScheduledTicks, decoration.ScheduledTicks...)
	result.PostProcessing = decoration.PostProcessing
	return result
}

// prepareTerrain is shared by published chunks and decoration's neighboring
// terrain views. Raw-column queries deliberately bypass these structure,
// material and carving stages.
func (g *Nether) prepareTerrain(pos world.ChunkPos, c *chunk.Chunk) {
	chunkX, chunkZ := int(pos[0]), int(pos[1])
	beard := g.fossilBeard(pos)
	scratch := g.pool.Get().(*netherScratch)
	g.prepareHeights(chunkX, chunkZ, c, scratch, beard)
	g.pool.Put(scratch)
	g.applyNetherMaterial(chunkX, chunkZ, c)
	g.carveCaves(chunkX, chunkZ, c)
}

// DefaultSpawn returns the default Nether spawn position.
func (*Nether) DefaultSpawn(world.Dimension) cube.Pos {
	return cube.Pos{0, netherSeaLevel + 2, 0}
}

func (g *Nether) fillBiomes(chunkX, chunkZ int, c *chunk.Chunk) {
	minY, maxY := int16(c.Range().Min()), int16(c.Range().Max())
	biomes := newNetherChunkBiomes(g, chunkX, chunkZ)
	for x := uint8(0); x < 16; x++ {
		for z := uint8(0); z < 16; z++ {
			worldX, worldZ := int32(chunkX*16)+int32(x), int32(chunkZ*16)+int32(z)
			for y := minY; y <= maxY; y++ {
				index := biomes.index(worldX, int32(y), worldZ)
				c.SetBiome(x, y, z, g.biomeIDs[index])
			}
		}
	}
}

func (g *Nether) biomeIndexAt(x, y, z int32) int {
	quartX, _, quartZ := vanilla.FiddledBiomeQuart(g.biomeZoomSeed, x, y, z)
	return netherBiomeIndex(g.biomes.Biome(quartX*4, quartZ*4))
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
	g         *Nether
	minQuartX int32
	minQuartZ int32
	indices   [6][6]uint8
	computed  [6][6]bool
}

func newNetherChunkBiomes(g *Nether, chunkX, chunkZ int) netherChunkBiomes {
	return netherChunkBiomes{
		g:         g,
		minQuartX: int32(chunkX*4 - 1),
		minQuartZ: int32(chunkZ*4 - 1),
	}
}

func (b *netherChunkBiomes) indexAtQuart(quartX, quartZ int32) int {
	x, z := int(quartX-b.minQuartX), int(quartZ-b.minQuartZ)
	if !b.computed[x][z] {
		b.indices[x][z] = uint8(netherBiomeIndex(b.g.biomes.Biome(quartX*4, quartZ*4)))
		b.computed[x][z] = true
	}
	return int(b.indices[x][z])
}

func (b *netherChunkBiomes) index(x, y, z int32) int {
	quartX, _, quartZ := vanilla.FiddledBiomeQuart(b.g.biomeZoomSeed, x, y, z)
	return b.indexAtQuart(quartX, quartZ)
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
