package nether
import (
	"sync"
	"sort"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	dfbiome "github.com/df-mc/dragonfly/server/world/biome"
	"github.com/df-mc/dragonfly/server/world/chunk"
	overworldFeatures "github.com/df-mc/dragonfly/server/world/generator/advanced/overworld"
	"github.com/df-mc/dragonfly/server/internal/java26"
)

const netherSeaLevel = 32

// Nether generates Java Edition 26.3 Nether chunks.
//
// The implementation is concurrency-safe and may be called from multiple generator workers.
type Nether struct {
	seed int64


	airRID                  uint32
	netherrackRID           uint32
	bedrockRID              uint32
	lavaStillRID            uint32
	lavaFlowRID             uint32
	gravelRID               uint32
	soulSandRID             uint32
	soulSoilRID             uint32
	basaltRID               uint32
	blackstoneRID           uint32
	crimsonNyliumRID        uint32
	warpedNyliumRID         uint32
	netherWartBlockRID      uint32
	warpedWartBlockRID      uint32
	netherBricksRID      uint32
	netherBrickFenceRID  uint32
	netherWartRID        uint32
	netherBrickStairsRID [4]uint32
	spawnerRID           uint32
	chestRID             uint32

	biomeIDs         [5]uint32
	biomeZoomSeed    int64
	biomes           *java26.NetherBiomes
	density          *java26.NetherDensity
	material         *java26.NetherMaterialNoise


	caves *netherCaves
	features *overworldFeatures.ModernFeatures
	featureSource *netherFeatureSource

	bridgeGenerationMu sync.Mutex
	bridgePayloadMu sync.Mutex

	pendingBlockEntities sync.Map // cube.Pos -> chunk.BlockEntity
	pool sync.Pool
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
		seed:           seed,
		airRID:         world.BlockRuntimeID(block.Air{}),
		netherrackRID:  world.BlockRuntimeID(block.Netherrack{}),
		bedrockRID:     world.BlockRuntimeID(block.Bedrock{}),
		lavaStillRID:   world.BlockRuntimeID(block.Lava{Depth: 8, Still: true}),
		lavaFlowRID:    world.BlockRuntimeID(block.Lava{Depth: 8, Still: false}),
		gravelRID:      world.BlockRuntimeID(block.Gravel{}),
		soulSandRID:    world.BlockRuntimeID(block.SoulSand{}),
		soulSoilRID:    world.BlockRuntimeID(block.SoulSoil{}),
		basaltRID:      world.BlockRuntimeID(block.Basalt{}),
		blackstoneRID:  world.BlockRuntimeID(block.Blackstone{Type: block.NormalBlackstone()}),
		crimsonNyliumRID: world.BlockRuntimeID(block.Nylium{}),
		warpedNyliumRID: world.BlockRuntimeID(block.Nylium{Warped: true}),
		netherWartBlockRID: world.BlockRuntimeID(block.NetherWartBlock{}),
		warpedWartBlockRID: world.BlockRuntimeID(block.NetherWartBlock{Warped: true}),
		netherBricksRID: world.BlockRuntimeID(block.NetherBricks{}),
		netherBrickFenceRID: world.BlockRuntimeID(block.NetherBrickFence{}),
		netherWartRID: world.BlockRuntimeID(block.NetherWart{Age: 0}),
		biomeIDs: [5]uint32{
			uint32(dfbiome.NetherWastes{}.EncodeBiome()),
			uint32(dfbiome.SoulSandValley{}.EncodeBiome()),
			uint32(dfbiome.CrimsonForest{}.EncodeBiome()),
			uint32(dfbiome.WarpedForest{}.EncodeBiome()),
			uint32(dfbiome.BasaltDeltas{}.EncodeBiome()),
		},
		biomeZoomSeed: java26.BiomeZoomSeed(seed),
		biomes:        java26.NewNetherBiomes(seed),
		density:       java26.NewNetherDensity(seed),
		material:      java26.NewNetherMaterialNoise(seed),
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

// GenerateChunk generates a single Nether chunk at the position passed.
func (g *Nether) GenerateChunk(pos world.ChunkPos, c *chunk.Chunk) world.GenerationResult {
	s := g.pool.Get().(*netherScratch)
	defer g.pool.Put(s)

	chunkX, chunkZ := int(pos[0]), int(pos[1])

	g.prepareHeights(chunkX, chunkZ, c, s)
	g.applyJavaNetherMaterial(chunkX, chunkZ, c)
	g.carveCaves(chunkX, chunkZ, c)
	result := world.GenerationResult{}
	g.fillBiomes(chunkX, chunkZ, c)
	g.featureSource.remember(pos, c)
	decoration := g.features.Decorate(pos, c)
	result.Entities = append(result.Entities, decoration.Entities...)
	result.BlockEntities = append(result.BlockEntities, decoration.BlockEntities...)
	result.ScheduledBlocks = append(result.ScheduledBlocks, decoration.ScheduledBlocks...)
	return result
}

// DefaultSpawn returns the default Nether spawn position.
func (*Nether) DefaultSpawn(world.Dimension) cube.Pos {
	return cube.Pos{0, netherSeaLevel + 2, 0}
}

func (g *Nether) fillBiomes(chunkX, chunkZ int, c *chunk.Chunk) {
	minY, maxY := int16(c.Range().Min()), int16(c.Range().Max())
	for x := uint8(0); x < 16; x++ {
		for z := uint8(0); z < 16; z++ {
			worldX, worldZ := int32(chunkX*16)+int32(x), int32(chunkZ*16)+int32(z)
			for y := minY; y <= maxY; y++ {
				index := g.biomeIndexAt(worldX, int32(y), worldZ)
				c.SetBiome(x, y, z, g.biomeIDs[index])
			}
		}
	}
}

func (g *Nether) biomeIndexAt(x, y, z int32) int {
	quartX, _, quartZ := java26.FiddledBiomeQuart(g.biomeZoomSeed, x, y, z)
	switch g.biomes.Biome(quartX*4, quartZ*4) {
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

func (g *Nether) prepareHeights(chunkX, chunkZ int, c *chunk.Chunk, s *netherScratch) {
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
				for dy := range 8 {
					fy := float32(dy) * 0.125
					y := cellY*8 + dy
					if y < minY || y > maxY {
						continue
					}
					a00 := float32(v000 + (v001-v000)*fy)
					a01 := float32(v010 + (v011-v010)*fy)
					a10 := float32(v100 + (v101-v100)*fy)
					a11 := float32(v110 + (v111-v110)*fy)
					for dx := range 4 {
						fx := float32(dx) * 0.25
						left := float32(a00 + (a10-a00)*fx)
						right := float32(a01 + (a11-a01)*fx)
						for dz := range 4 {
							fz := float32(dz) * 0.25
							density := float32(left + (right-left)*fz)
							var rid uint32
							if density > 0 {
								rid = g.netherrackRID
							} else if y < 32 {
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

func (g *Nether) takeBlockEntityPayloads(pos world.ChunkPos) []chunk.BlockEntity {
	out := make([]chunk.BlockEntity, 0)
	g.pendingBlockEntities.Range(func(key, value any) bool {
		blockPos := key.(cube.Pos)
		if int32(blockPos[0])>>4 != pos[0] || int32(blockPos[2])>>4 != pos[1] {
			return true
		}
		if payload, loaded := g.pendingBlockEntities.LoadAndDelete(key); loaded {
			out = append(out, payload.(chunk.BlockEntity))
		}
		return true
	})
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i].Pos, out[j].Pos
		if a[0] != b[0] { return a[0] < b[0] }
		if a[1] != b[1] { return a[1] < b[1] }
		return a[2] < b[2]
	})
	return out
}
