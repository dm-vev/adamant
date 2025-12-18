package overworld

import (
	"math"
	"sync"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/mc112"
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
	scaleNoise          *mc112.NoiseOctaves // Unused for now (kept to match RNG consumption).
	depthNoise          *mc112.NoiseOctaves
	forestNoise         *mc112.NoiseOctaves // Unused for now (kept to match RNG consumption).

	biomeWeights [25]float64

	mapGenJ int64
	mapGenK int64

	// cached runtime IDs
	airRID          uint32
	stoneRID        uint32
	dirtRID         uint32
	grassRID        uint32
	myceliumRID     uint32
	podzolRID       uint32
	bedrockRID      uint32
	waterRID        uint32
	lavaRID         uint32
	gravelRID       uint32
	sandRID         uint32
	redSandRID      uint32
	sandstoneRID    uint32
	redSandstoneRID uint32
	terracottaRID   uint32

	carvable map[uint32]struct{}

	biomes [256]biomeDef

	biomeProvider *biomeProvider

	populationQueue chan populationJob
	popOnce         sync.Once
	world           worldPointer

	scatteredCache sync.Map // world.ChunkPos -> *scatteredStructure (nil when absent)

	pool sync.Pool
}

type biomeDef struct {
	baseHeight      float32
	heightVariation float32

	topRID    uint32
	fillerRID uint32
	biomeID   uint32
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

	g := &Overworld{
		seed:                seed,
		minLimitPerlinNoise: mc112.NewNoiseOctaves(r, 16),
		maxLimitPerlinNoise: mc112.NewNoiseOctaves(r, 16),
		mainPerlinNoise:     mc112.NewNoiseOctaves(r, 8),
		surfaceNoise:        mc112.NewNoisePerlin(r, 4),
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
		bedrockRID:      world.BlockRuntimeID(block.Bedrock{}),
		waterRID:        world.BlockRuntimeID(block.Water{Depth: 8, Still: true}),
		lavaRID:         world.BlockRuntimeID(block.Lava{Depth: 8, Still: false}),
		gravelRID:       world.BlockRuntimeID(block.Gravel{}),
		sandRID:         world.BlockRuntimeID(block.Sand{}),
		redSandRID:      world.BlockRuntimeID(block.Sand{Red: true}),
		sandstoneRID:    world.BlockRuntimeID(block.Sandstone{}),
		redSandstoneRID: world.BlockRuntimeID(block.Sandstone{Red: true}),
		terracottaRID:   world.BlockRuntimeID(block.Terracotta{}),

		populationQueue: make(chan populationJob, 65536),
	}

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

	genIDs := g.biomeProvider.biomesForGeneration(chunkX*4-2, chunkZ*4-2, 10, 10)
	biomeIDs := g.biomeProvider.biomes(chunkX*16, chunkZ*16, 16, 16)

	var biomesForGeneration [10 * 10]*biomeDef
	var biomes [16 * 16]*biomeDef
	for i, id := range genIDs {
		biomesForGeneration[i] = g.biomeDef(id)
	}
	for i, id := range biomeIDs {
		biomes[i] = g.biomeDef(id)
	}

	g.setBlocksInChunk(chunkX, chunkZ, c, biomesForGeneration[:], s)
	r := mc112.NewRand(int64(chunkX)*341873128712 + int64(chunkZ)*132897987541)
	g.replaceBiomeBlocks(chunkX, chunkZ, c, biomes[:], r, s)
	g.carve(chunkX, chunkZ, c, biomes[:])
	g.generateStructures(chunkX, chunkZ, c)
	g.fillBiomes(c, biomes[:])
}

func (g *Overworld) fillBiomes(c *chunk.Chunk, biomes []*biomeDef) {
	minY, maxY := int16(c.Range().Min()), int16(c.Range().Max())
	for x := uint8(0); x < 16; x++ {
		for z := uint8(0); z < 16; z++ {
			biomeID := biomes[int(z)+int(x)*16].biomeID
			for y := minY; y <= maxY; y++ {
				c.SetBiome(x, y, z, biomeID)
			}
		}
	}
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
						for zStep := 0; zStep < 4; zStep++ {
							z := uint8(zStep + zCell*4)
							if d15 > 0.0 {
								c.SetBlock(x, y, z, 0, g.stoneRID)
							} else if int(y) < javaSeaLevel {
								c.SetBlock(x, y, z, 0, g.waterRID)
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

	// Depth noise is 2D (ySize = 1).
	s.depth = g.depthNoise.GenerateNoiseOctaves(s.depth, xOffset, 0, zOffset, xSize, 1, zSize, 200.0, 0.5, 200.0)

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

			depthVal := s.depth[x+z*xSize] / 8000.0
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
			b := biomes[int(z)+int(x)*16]

			noiseVal := s.surfaceBuf[int(z)+int(x)*16]
			thickness := int32(noiseVal/3.0 + 3.0 + r.Float64()*0.25)

			top := b.topRID
			filler := b.fillerRID

			layer := int32(-1)

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
					if thickness <= 0 {
						top = g.airRID
						filler = g.stoneRID
					} else if int(y) >= javaSeaLevel-4 && int(y) <= javaSeaLevel+1 {
						top = b.topRID
						filler = b.fillerRID
					}
					if int(y) < javaSeaLevel && top == g.airRID {
						top = g.waterRID
					}
					layer = thickness

					if int(y) >= javaSeaLevel-1 {
						c.SetBlock(x, y, z, 0, top)
					} else if int(y) < javaSeaLevel-7-int(thickness) {
						c.SetBlock(x, y, z, 0, g.gravelRID)
					} else {
						c.SetBlock(x, y, z, 0, filler)
					}
					continue
				}

				if layer > 0 {
					layer--
					c.SetBlock(x, y, z, 0, filler)

					if layer == 0 && thickness > 1 {
						if filler == g.sandRID {
							layer = int32(r.Intn(4) + int32(max(0, int(y)-(javaSeaLevel-1))))
							filler = g.sandstoneRID
						} else if filler == g.redSandRID {
							layer = int32(r.Intn(4) + int32(max(0, int(y)-(javaSeaLevel-1))))
							filler = g.redSandstoneRID
						}
					}
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
