package overworld

import (
	"math"
	"sync"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/mc112"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/overworld/internal/genlayer"
	mcbiome "github.com/df-mc/dragonfly/server/world/generator/advanced/overworld/internal/biome"
)

const (
	javaSeaLevel = 63
)

// Overworld generates terrain for the overworld using the Minecraft Java Edition 1.12 algorithm.
//
// Note: This is an incremental port. Terrain noise, surface replacement, carving, the biome pipeline, and initial
// population/structures are implemented, but many structures and decorations are still missing.
//
// The implementation is concurrency-safe and may be called from multiple generator workers.
type Overworld struct {
	seed int64

	minLimitPerlinNoise *mc112.NoiseOctaves
	maxLimitPerlinNoise *mc112.NoiseOctaves
	mainPerlinNoise     *mc112.NoiseOctaves
	surfaceNoise        *mc112.NoisePerlin
	temperatureNoise    *mc112.NoisePerlin
	grassColorNoise     *mc112.NoisePerlin
	mesaBandNoise       *mc112.NoisePerlin
	scaleNoise          *mc112.NoiseOctaves // Unused for now (kept to match RNG consumption).
	depthNoise          *mc112.NoiseOctaves
	forestNoise         *mc112.NoiseOctaves // Unused for now (kept to match RNG consumption).

	biomeWeights [25]float64

	mapGenJ int64
	mapGenK int64

	// cached runtime IDs
	airRID                uint32
	stoneRID              uint32
	dirtRID               uint32
	grassRID              uint32
	myceliumRID           uint32
	podzolRID             uint32
	coarseDirtRID         uint32
	farmlandRID           uint32
	bedrockRID            uint32
	waterRID              uint32
	lavaRID               uint32
	gravelRID             uint32
	sandRID               uint32
	redSandRID            uint32
	sandstoneRID          uint32
	redSandstoneRID       uint32
	terracottaRID         uint32
	stainedTerracottaRIDs [16]uint32
	mesaBands             [64]uint32

	// decoration runtime IDs
	shortGrassRID           uint32
	doubleTallGrassLowerRID uint32
	doubleTallGrassUpperRID uint32
	sunflowerLowerRID       uint32
	sunflowerUpperRID       uint32

	dandelionRID  uint32
	poppyRID      uint32
	blueOrchidRID uint32

	lilyPadRID uint32
	clayRID    uint32

	iceRID       uint32
	snowLayerRID uint32

	deadBushRID  uint32
	cactusRID    uint32
	sugarCaneRID uint32

	oakLogRID     uint32
	spruceLogRID  uint32
	birchLogRID   uint32
	jungleLogRID  uint32
	acaciaLogRID  uint32
	darkOakLogRID uint32

	oakLeavesRID     uint32
	spruceLeavesRID  uint32
	birchLeavesRID   uint32
	jungleLeavesRID  uint32
	acaciaLeavesRID  uint32
	darkOakLeavesRID uint32

	// structure runtime IDs (best-effort, used for vanilla structure ports)
	oakPlanksRID     uint32
	darkOakPlanksRID uint32
	oakFenceRID      uint32
	darkOakFenceRID  uint32
	railRID          uint32
	webRID           uint32
	torchRID         uint32

	carvable map[uint32]struct{}

	biomes [256]biomeDef

	biomeProvider *biomeProvider

	populationQueue chan populationJob
	popOnce         sync.Once
	world           worldPointer

	scatteredCache sync.Map // world.ChunkPos -> *scatteredStructure (nil when absent)
	mineshaftCache sync.Map // world.ChunkPos -> *mineshaftStructure (nil when absent)
	previewCache   *previewCache
	biomeDataCache *biomeDataCache
	previewScratch sync.Pool
	lakeShapePool  sync.Pool

	pool sync.Pool
}

type biomeDef struct {
	baseHeight      float32
	heightVariation float32

	topRID    uint32
	fillerRID uint32
	biomeID   uint32
	mcID      mcbiome.ID
}

type scratch struct {
	heightMap []float64

	mainNoise []float64
	minNoise  []float64
	maxNoise  []float64
	depth     []float64

	surfaceBuf []float64
}

// New returns a Java Edition 1.12-style overworld generator using the seed provided.
func New(seed int64) *Overworld {
	return NewOverworld(seed)
}

// NewOverworld returns a Java Edition 1.12-style overworld generator using the seed provided.
func NewOverworld(seed int64) *Overworld {
	r := mc112.NewRand(seed)

	mapGenRand := mc112.NewRand(seed)

	railRID, _ := chunk.StateToRuntimeID("minecraft:rail", nil)
	webRID, _ := chunk.StateToRuntimeID("minecraft:web", nil)
	torchRID, _ := chunk.StateToRuntimeID("minecraft:torch", nil)

	g := &Overworld{
		seed:                seed,
		minLimitPerlinNoise: mc112.NewNoiseOctaves(r, 16),
		maxLimitPerlinNoise: mc112.NewNoiseOctaves(r, 16),
		mainPerlinNoise:     mc112.NewNoiseOctaves(r, 8),
		surfaceNoise:        mc112.NewNoisePerlin(r, 4),
		temperatureNoise:    mc112.NewNoisePerlin(mc112.NewRand(1234), 1),
		grassColorNoise:     mc112.NewNoisePerlin(mc112.NewRand(2345), 1),
		mesaBandNoise:       mc112.NewNoisePerlin(mc112.NewRand(seed), 1),
		scaleNoise:          mc112.NewNoiseOctaves(r, 10),
		depthNoise:          mc112.NewNoiseOctaves(r, 16),
		forestNoise:         mc112.NewNoiseOctaves(r, 8),

		mapGenJ: mapGenRand.Long(),
		mapGenK: mapGenRand.Long(),

		airRID:          world.BlockRuntimeID(block.Air{}),
		stoneRID:        world.BlockRuntimeID(block.Stone{}),
		dirtRID:         world.BlockRuntimeID(block.Dirt{}),
		grassRID:        world.BlockRuntimeID(block.Grass{}),
		myceliumRID:     world.BlockRuntimeID(block.Mycelium{}),
		podzolRID:       world.BlockRuntimeID(block.Podzol{}),
		coarseDirtRID:   world.BlockRuntimeID(block.Dirt{Coarse: true}),
		farmlandRID:     world.BlockRuntimeID(block.Farmland{}),
		bedrockRID:      world.BlockRuntimeID(block.Bedrock{}),
		waterRID:        world.BlockRuntimeID(block.Water{Depth: 8, Still: true}),
		lavaRID:         world.BlockRuntimeID(block.Lava{Depth: 8, Still: false}),
		gravelRID:       world.BlockRuntimeID(block.Gravel{}),
		sandRID:         world.BlockRuntimeID(block.Sand{}),
		redSandRID:      world.BlockRuntimeID(block.Sand{Red: true}),
		sandstoneRID:    world.BlockRuntimeID(block.Sandstone{}),
		redSandstoneRID: world.BlockRuntimeID(block.Sandstone{Red: true}),
		terracottaRID:   world.BlockRuntimeID(block.Terracotta{}),

		shortGrassRID:           world.BlockRuntimeID(block.ShortGrass{}),
		doubleTallGrassLowerRID: world.BlockRuntimeID(block.DoubleTallGrass{Type: block.NormalDoubleTallGrass()}),
		doubleTallGrassUpperRID: world.BlockRuntimeID(block.DoubleTallGrass{Type: block.NormalDoubleTallGrass(), UpperPart: true}),
		sunflowerLowerRID:       world.BlockRuntimeID(block.DoubleFlower{Type: block.Sunflower()}),
		sunflowerUpperRID:       world.BlockRuntimeID(block.DoubleFlower{Type: block.Sunflower(), UpperPart: true}),

		dandelionRID:  world.BlockRuntimeID(block.Flower{Type: block.Dandelion()}),
		poppyRID:      world.BlockRuntimeID(block.Flower{Type: block.Poppy()}),
		blueOrchidRID: world.BlockRuntimeID(block.Flower{Type: block.BlueOrchid()}),

		lilyPadRID: world.BlockRuntimeID(block.LilyPad{}),
		clayRID:    world.BlockRuntimeID(block.Clay{}),

		iceRID:       world.BlockRuntimeID(block.Ice{}),
		snowLayerRID: world.BlockRuntimeID(block.SnowLayer{Layers: 1}),

		deadBushRID:  world.BlockRuntimeID(block.DeadBush{}),
		cactusRID:    world.BlockRuntimeID(block.Cactus{}),
		sugarCaneRID: world.BlockRuntimeID(block.SugarCane{}),

		oakLogRID:     world.BlockRuntimeID(block.Log{Wood: block.OakWood(), Axis: cube.Y}),
		spruceLogRID:  world.BlockRuntimeID(block.Log{Wood: block.SpruceWood(), Axis: cube.Y}),
		birchLogRID:   world.BlockRuntimeID(block.Log{Wood: block.BirchWood(), Axis: cube.Y}),
		jungleLogRID:  world.BlockRuntimeID(block.Log{Wood: block.JungleWood(), Axis: cube.Y}),
		acaciaLogRID:  world.BlockRuntimeID(block.Log{Wood: block.AcaciaWood(), Axis: cube.Y}),
		darkOakLogRID: world.BlockRuntimeID(block.Log{Wood: block.DarkOakWood(), Axis: cube.Y}),

		oakLeavesRID:     world.BlockRuntimeID(block.Leaves{Wood: block.OakWood(), ShouldUpdate: true}),
		spruceLeavesRID:  world.BlockRuntimeID(block.Leaves{Wood: block.SpruceWood(), ShouldUpdate: true}),
		birchLeavesRID:   world.BlockRuntimeID(block.Leaves{Wood: block.BirchWood(), ShouldUpdate: true}),
		jungleLeavesRID:  world.BlockRuntimeID(block.Leaves{Wood: block.JungleWood(), ShouldUpdate: true}),
		acaciaLeavesRID:  world.BlockRuntimeID(block.Leaves{Wood: block.AcaciaWood(), ShouldUpdate: true}),
		darkOakLeavesRID: world.BlockRuntimeID(block.Leaves{Wood: block.DarkOakWood(), ShouldUpdate: true}),

		oakPlanksRID:     world.BlockRuntimeID(block.Planks{Wood: block.OakWood()}),
		darkOakPlanksRID: world.BlockRuntimeID(block.Planks{Wood: block.DarkOakWood()}),
		oakFenceRID:      world.BlockRuntimeID(block.WoodFence{Wood: block.OakWood()}),
		darkOakFenceRID:  world.BlockRuntimeID(block.WoodFence{Wood: block.DarkOakWood()}),
		railRID:          railRID,
		webRID:           webRID,
		torchRID:         torchRID,

		populationQueue: make(chan populationJob, 65536),
		previewCache:    newPreviewCache(1024),
		biomeDataCache:  newBiomeDataCache(2048),
	}
	g.previewScratch.New = func() any {
		return make(map[world.ChunkPos]*chunk.Chunk, 9)
	}
	g.lakeShapePool.New = func() any {
		return make([]bool, 16*16*8)
	}

	for i, c := range item.Colours() {
		g.stainedTerracottaRIDs[i] = world.BlockRuntimeID(block.StainedTerracotta{Colour: c})
	}
	g.initMesaBands()

	for x := -2; x <= 2; x++ {
		for z := -2; z <= 2; z++ {
			f := float64(x*x+z*z) + 0.2
			g.biomeWeights[(x+2)+(z+2)*5] = float64(10.0) / math.Sqrt(f)
		}
	}

	g.initBiomeDefs()
	g.initCarving()
	g.biomeProvider = newBiomeProvider(seed)

	g.pool.New = func() any {
		return &scratch{
			heightMap:  make([]float64, 5*33*5),
			mainNoise:  make([]float64, 5*33*5),
			minNoise:   make([]float64, 5*33*5),
			maxNoise:   make([]float64, 5*33*5),
			depth:      make([]float64, 5*5*1),
			surfaceBuf: make([]float64, 16*16),
		}
	}
	return g
}

// GenerateChunk generates a single overworld chunk at the position passed.
func (g *Overworld) GenerateChunk(pos world.ChunkPos, c *chunk.Chunk) {
	s := g.pool.Get().(*scratch)
	defer g.pool.Put(s)

	chunkX, chunkZ := int(pos[0]), int(pos[1])
	biomeData := g.biomeDataForChunk(chunkX, chunkZ)

	var biomesForGeneration [10 * 10]*biomeDef
	var biomes [16 * 16]*biomeDef
	for i, id := range biomeData.genIDs {
		biomesForGeneration[i] = g.biomeDef(id)
	}
	for i, id := range biomeData.biomeIDs {
		biomes[i] = g.biomeDef(id)
	}

	g.setBlocksInChunk(chunkX, chunkZ, c, biomesForGeneration[:], s)
	r := mc112.NewRand(int64(chunkX)*341873128712 + int64(chunkZ)*132897987541)
	g.replaceBiomeBlocks(chunkX, chunkZ, c, biomes[:], r, s)
	g.applySwampWaterlilies(chunkX, chunkZ, c, biomeData.biomeIDs[:])
	g.carve(chunkX, chunkZ, c, biomes[:])
	popRand := g.chunkPopulationRand(chunkX, chunkZ)
	villageGenerated := g.generateStructures(chunkX, chunkZ, c, popRand)
	g.populateLakes(chunkX, chunkZ, c, villageGenerated)
	g.populateOresInChunk(chunkX, chunkZ, c)
	g.decorate(chunkX, chunkZ, c)
	g.freezeAndSnow(chunkX, chunkZ, c, biomeData.biomeIDs[:])
	g.fillBiomes(c, biomes[:])
}

func (g *Overworld) biomeDataForChunk(chunkX, chunkZ int) *biomeData {
	pos := world.ChunkPos{int32(chunkX), int32(chunkZ)}
	if data, ok := g.biomeDataCache.get(pos); ok {
		return data
	}

	genIDs := g.biomeProvider.biomesForGeneration(chunkX*4-2, chunkZ*4-2, 10, 10)
	biomeIDs := g.biomeProvider.biomes(chunkX*16, chunkZ*16, 16, 16)

	data := &biomeData{}
	copy(data.genIDs[:], genIDs)
	copy(data.biomeIDs[:], biomeIDs)
	genlayer.ReleaseInts(genIDs)
	genlayer.ReleaseInts(biomeIDs)

	g.biomeDataCache.add(pos, data)
	return data
}

func (g *Overworld) biomeIDAt(worldX, worldZ int) int {
	chunkX, chunkZ := worldX>>4, worldZ>>4
	data := g.biomeDataForChunk(chunkX, chunkZ)
	x, z := worldX&15, worldZ&15
	return data.biomeIDs[z*16+x]
}

func (g *Overworld) applySwampWaterlilies(chunkX, chunkZ int, c *chunk.Chunk, biomeIDs []int) {
	// Matches the extra swamp terrain logic in BiomeSwamp.genTerrainBlocks (Java 1.12).
	// It fills local depressions at y=62 with water and sometimes places a waterlily at y=63.
	if len(biomeIDs) != 16*16 {
		return
	}
	chunkBaseX, chunkBaseZ := chunkX<<4, chunkZ<<4
	for x := 0; x < 16; x++ {
		for z := 0; z < 16; z++ {
			id := biomeIDs[x+z*16]
			if mcbiome.ID(id) != mcbiome.Swamp && mcbiome.ID(id) != mcbiome.SwampHills {
				continue
			}
			wx := chunkBaseX + x
			wz := chunkBaseZ + z
			d0 := g.grassColorNoise.GetValue(float64(wx)*0.25, float64(wz)*0.25)
			if d0 <= 0.0 {
				continue
			}

			top := int(c.HighestBlock(uint8(x), uint8(z)))
			if top != 62 {
				continue
			}
			if c.Block(uint8(x), int16(62), uint8(z), 0) == g.waterRID {
				continue
			}
			c.SetBlock(uint8(x), int16(62), uint8(z), 0, g.waterRID)
			if d0 < 0.12 && c.Block(uint8(x), int16(63), uint8(z), 0) == g.airRID {
				c.SetBlock(uint8(x), int16(63), uint8(z), 0, g.lilyPadRID)
			}
		}
	}
}

func (g *Overworld) fillBiomes(c *chunk.Chunk, biomes []*biomeDef) {
	var biomeColumns [16 * 16]uint32
	for i, b := range biomes {
		biomeColumns[i] = b.biomeID
	}
	c.FillBiomes2D(biomeColumns[:])
}

func (g *Overworld) setBlocksInChunk(chunkX, chunkZ int, c *chunk.Chunk, biomesForGeneration []*biomeDef, s *scratch) {
	heightMap := g.getHeights(s, chunkX*4, 0, chunkZ*4, 5, 33, 5, biomesForGeneration)

	// Fill only the vanilla 0..255 range. The overworld dimension range may be larger in Bedrock.
	minY := int16(0)
	maxY := int16(255)
	if yMin := int16(c.Range().Min()); yMin > minY {
		minY = yMin
	}
	if yMax := int16(c.Range().Max()); yMax < maxY {
		maxY = yMax
	}

	type terrainSubStorage struct {
		initialised bool
		storage    *chunk.PalettedStorage
		stoneIndex uint16
		waterIndex uint16
	}
	var terrainBySub [16]terrainSubStorage
	subMin := c.SubIndex(minY)

	for xCell := 0; xCell < 4; xCell++ {
		for zCell := 0; zCell < 4; zCell++ {
			for yCell := 0; yCell < 32; yCell++ {
				d1 := heightMap[((xCell+0)*5+(zCell+0))*33+yCell+0]
				d2 := heightMap[((xCell+0)*5+(zCell+1))*33+yCell+0]
				d3 := heightMap[((xCell+1)*5+(zCell+0))*33+yCell+0]
				d4 := heightMap[((xCell+1)*5+(zCell+1))*33+yCell+0]

				d5 := (heightMap[((xCell+0)*5+(zCell+0))*33+yCell+1] - d1) * 0.125
				d6 := (heightMap[((xCell+0)*5+(zCell+1))*33+yCell+1] - d2) * 0.125
				d7 := (heightMap[((xCell+1)*5+(zCell+0))*33+yCell+1] - d3) * 0.125
				d8 := (heightMap[((xCell+1)*5+(zCell+1))*33+yCell+1] - d4) * 0.125

				for yStep := 0; yStep < 8; yStep++ {
					d10 := d1
					d11 := d2
					d12 := (d3 - d1) * 0.25
					d13 := (d4 - d2) * 0.25

					y := int16(yCell*8 + yStep)
					if y < minY || y > maxY {
						d1 += d5
						d2 += d6
						d3 += d7
						d4 += d8
						continue
					}

					for xStep := 0; xStep < 4; xStep++ {
						d15 := d10
						d16 := (d11 - d10) * 0.25

						x := uint8(xStep + xCell*4)
						subIndex := c.SubIndex(y) - subMin
						sub := &terrainBySub[subIndex]
						for zStep := 0; zStep < 4; zStep++ {
							z := uint8(zStep + zCell*4)
							if d15 > 0.0 {
								if !sub.initialised {
									sub.storage, sub.stoneIndex, sub.waterIndex = c.LayerStorageWithTwoRuntimeIDs(y, 0, g.stoneRID, g.waterRID)
									sub.initialised = true
								}
								sub.storage.SetPaletteIndex(x, uint8(y), z, sub.stoneIndex)
							} else if int(y) < javaSeaLevel {
								if !sub.initialised {
									sub.storage, sub.stoneIndex, sub.waterIndex = c.LayerStorageWithTwoRuntimeIDs(y, 0, g.stoneRID, g.waterRID)
									sub.initialised = true
								}
								sub.storage.SetPaletteIndex(x, uint8(y), z, sub.waterIndex)
							}
							d15 += d16
						}

						d10 += d12
						d11 += d13
					}

					d1 += d5
					d2 += d6
					d3 += d7
					d4 += d8
				}
			}
		}
	}
}

func (g *Overworld) getHeights(s *scratch, xOffset, yOffset, zOffset, xSize, ySize, zSize int, biomesForGeneration []*biomeDef) []float64 {
	total := xSize * ySize * zSize
	if len(s.heightMap) != total {
		s.heightMap = make([]float64, total)
	}

	const d0 = 684.412
	s.mainNoise = g.mainPerlinNoise.GenerateNoiseOctaves(s.mainNoise, xOffset, yOffset, zOffset, xSize, ySize, zSize, d0/80.0, d0/160.0, d0/80.0)
	s.minNoise = g.minLimitPerlinNoise.GenerateNoiseOctaves(s.minNoise, xOffset, yOffset, zOffset, xSize, ySize, zSize, d0, d0, d0)
	s.maxNoise = g.maxLimitPerlinNoise.GenerateNoiseOctaves(s.maxNoise, xOffset, yOffset, zOffset, xSize, ySize, zSize, d0, d0, d0)

	// Depth noise is 2D in vanilla. Java uses the 2D overload which hardcodes yOffset=10 and yScale=1.0.
	s.depth = g.depthNoise.GenerateNoiseOctaves(s.depth, xOffset, 10, zOffset, xSize, 1, zSize, 200.0, 1.0, 200.0)

	k := 0
	for x := 0; x < xSize; x++ {
		for z := 0; z < zSize; z++ {
			var (
				variationSum float64
				heightSum    float64
				weightSum    float64
			)

			centre := biomesForGeneration[(x+2)+(z+2)*10]
			for dx := -2; dx <= 2; dx++ {
				for dz := -2; dz <= 2; dz++ {
					b := biomesForGeneration[(x+dx+2)+(z+dz+2)*10]
					baseHeight := float64(b.baseHeight)
					heightVariation := float64(b.heightVariation)

					weight := g.biomeWeights[(dx+2)+(dz+2)*5] / (baseHeight + 2.0)
					if b.baseHeight > centre.baseHeight {
						weight /= 2.0
					}
					variationSum += heightVariation * weight
					heightSum += baseHeight * weight
					weightSum += weight
				}
			}

			variation := variationSum / weightSum
			baseHeight := heightSum / weightSum

			variation = variation*0.9 + 0.1
			baseHeight = (baseHeight*4.0 - 1.0) / 8.0

			// NoiseOctaves with ySize=1 writes indices as x*zSize + z.
			depthVal := s.depth[z+x*zSize] / 8000.0
			if depthVal < 0.0 {
				depthVal = -depthVal * 0.3
			}
			depthVal = depthVal*3.0 - 2.0
			if depthVal < 0.0 {
				depthVal /= 2.0
				if depthVal < -1.0 {
					depthVal = -1.0
				}
				depthVal /= 1.4
				depthVal /= 2.0
			} else {
				if depthVal > 1.0 {
					depthVal = 1.0
				}
				depthVal /= 8.0
			}

			d6 := baseHeight + depthVal*0.2
			d6 = d6 * 8.5 / 8.0
			d8 := 8.5 + d6*4.0

			for y := 0; y < ySize; y++ {
				d10 := (float64(y) - d8) * 12.0 * 128.0 / 256.0 / variation
				if d10 < 0.0 {
					d10 *= 4.0
				}

				d2 := s.minNoise[k] / 512.0
				d3 := s.maxNoise[k] / 512.0
				d5 := (s.mainNoise[k]/10.0 + 1.0) / 2.0

				var d4 float64
				switch {
				case d5 < 0.0:
					d4 = d2
				case d5 > 1.0:
					d4 = d3
				default:
					d4 = d2 + (d3-d2)*d5
				}
				d4 = d4 - d10

				if y > 29 {
					d11 := float64(y-29) / 3.0
					d4 = d4*(1.0-d11) + -10.0*d11
				}

				s.heightMap[k] = d4
				k++
			}
		}
	}

	return s.heightMap
}

func (g *Overworld) replaceBiomeBlocks(chunkX, chunkZ int, c *chunk.Chunk, biomes []*biomeDef, r *mc112.Rand, s *scratch) {
	surfaceX := float64(chunkX * 16)
	surfaceZ := float64(chunkZ * 16)
	s.surfaceBuf = g.surfaceNoise.GetRegion(s.surfaceBuf, surfaceX, surfaceZ, 16, 16, 0.0625, 0.0625, 1.0)

	minY := int16(0)
	maxY := int16(255)
	if yMin := int16(c.Range().Min()); yMin > minY {
		minY = yMin
	}
	if yMax := int16(c.Range().Max()); yMax < maxY {
		maxY = yMax
	}
	if maxY < minY {
		return
	}

	for x := uint8(0); x < 16; x++ {
		for z := uint8(0); z < 16; z++ {
			// biomes[] is indexed as x + z*16.
			b := biomes[int(x)+int(z)*16]

			// NoisePerlin.GetRegion outputs x + z*16.
			noiseVal := s.surfaceBuf[int(x)+int(z)*16]
			id := b.mcID
			if isMesaBiome(id) {
				worldX := chunkX*16 + int(x)
				worldZ := chunkZ*16 + int(z)
				g.generateMesaTerrainColumn(c, id, x, z, worldX, worldZ, noiseVal, r, minY, maxY)
				continue
			}

			top := b.topRID
			filler := b.fillerRID

			switch id {
			case mcbiome.Mountains, mcbiome.WoodedMountains, mcbiome.MountainEdge, mcbiome.GravellyMountains, mcbiome.ModifiedGravellyMountains:
				// BiomeHills.genTerrainBlocks: stone/gravel surface based on surface noise (Java 1.12).
				if noiseVal > 1.0 {
					top, filler = g.stoneRID, g.stoneRID
				} else if noiseVal > -1.0 {
					top, filler = g.gravelRID, g.gravelRID
				}

			case mcbiome.GiantTreeTaiga, mcbiome.GiantTreeTaigaHills, mcbiome.GiantSpruceTaiga, mcbiome.GiantSpruceTaigaHills:
				// BiomeTaiga (MEGA/MEGA_SPRUCE) surface patches: podzol/coarse dirt (Java 1.12).
				// (In Java 1.12 podzol is a dirt variant; in Bedrock we map it to `block.Podzol{}`.)
				if noiseVal > 1.75 {
					top, filler = g.coarseDirtRID, g.coarseDirtRID
				} else if noiseVal > -0.95 {
					top, filler = g.podzolRID, g.dirtRID
				} else {
					top, filler = g.grassRID, g.dirtRID
				}
			}

			g.generateBiomeTerrainColumn(c, x, z, top, filler, noiseVal, r, minY, maxY)
		}
	}
}

func (g *Overworld) generateBiomeTerrainColumn(c *chunk.Chunk, x, z uint8, top, filler uint32, noiseVal float64, r *mc112.Rand, minY, maxY int16) {
	thickness := int32(noiseVal/3.0 + 3.0 + r.Float64()*0.25)
	layer := int32(-1)
	topBlock := top
	fillerBlock := filler

	for y := maxY; y >= minY; y-- {
		if y <= int16(r.Intn(5)) {
			c.SetBlock(x, y, z, 0, g.bedrockRID)
			continue
		}

		current := c.Block(x, y, z, 0)
		if current == g.airRID {
			layer = -1
			continue
		}
		if current != g.stoneRID {
			continue
		}

		if layer == -1 {
			topBlock = top
			fillerBlock = filler

			if thickness <= 0 {
				topBlock = g.airRID
				fillerBlock = g.stoneRID
			} else if int(y) >= javaSeaLevel-4 && int(y) <= javaSeaLevel+1 {
				topBlock = top
				fillerBlock = filler
			}

			if int(y) < javaSeaLevel && topBlock == g.airRID {
				topBlock = g.waterRID
			}

			layer = thickness

			if int(y) >= javaSeaLevel-1 {
				c.SetBlock(x, y, z, 0, topBlock)
			} else if int(y) < javaSeaLevel-7-int(thickness) {
				c.SetBlock(x, y, z, 0, g.gravelRID)
			} else {
				c.SetBlock(x, y, z, 0, fillerBlock)
			}
			continue
		}

		if layer > 0 {
			layer--
			c.SetBlock(x, y, z, 0, fillerBlock)

			if layer == 0 && thickness > 1 {
				if fillerBlock == g.sandRID {
					layer = int32(r.Intn(4) + int32(max(0, int(y)-(javaSeaLevel-1))))
					fillerBlock = g.sandstoneRID
				} else if fillerBlock == g.redSandRID {
					layer = int32(r.Intn(4) + int32(max(0, int(y)-(javaSeaLevel-1))))
					fillerBlock = g.redSandstoneRID
				}
			}
		}
	}
}

func (g *Overworld) biomeDef(id int) *biomeDef {
	if id < 0 || id >= len(g.biomes) {
		id = int(mcbiome.Plains)
	}
	return &g.biomes[id]
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
