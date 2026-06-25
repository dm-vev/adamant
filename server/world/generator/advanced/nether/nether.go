package nether

import (
	"math"
	"sync"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	dfbiome "github.com/df-mc/dragonfly/server/world/biome"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/mc112"
)

const (
	netherSeaLevel = 63
)

// Nether generates terrain for the Nether dimension using the Minecraft Java Edition 1.12 algorithm.
//
// The implementation is concurrency-safe and may be called from multiple generator workers.
type Nether struct {
	seed int64

	lperlinNoise1 *mc112.NoiseOctaves
	lperlinNoise2 *mc112.NoiseOctaves
	perlinNoise1  *mc112.NoiseOctaves

	slowsandGravelNoiseGen        *mc112.NoiseOctaves
	netherrackExclusivityNoiseGen *mc112.NoiseOctaves
	scaleNoise                    *mc112.NoiseOctaves
	depthNoise                    *mc112.NoiseOctaves

	mapGenJ int64
	mapGenK int64

	airRID        uint32
	netherrackRID uint32
	bedrockRID    uint32
	lavaStillRID  uint32
	lavaFlowRID   uint32
	gravelRID     uint32
	soulSandRID   uint32
	dirtRID       uint32
	grassRID      uint32
	glowstoneRID  uint32
	fireRID       uint32
	brownMushroomRID uint32
	redMushroomRID   uint32
	quartzOreRID  uint32
	magmaRID      uint32
	netherBricksRID     uint32
	netherBrickFenceRID uint32
	netherWartRID       uint32
	netherBrickStairsRID [4]uint32
	spawnerRID uint32
	chestRID   uint32

	biomeID uint32

	caves *netherCaves

	bridgeCache sync.Map // world.ChunkPos -> *netherBridgeStructure (nil when absent)

	pool sync.Pool
}

type netherScratch struct {
	buffer []float64

	slowsandNoise []float64
	gravelNoise   []float64
	depthBuffer   []float64

	pnr       []float64
	ar        []float64
	br        []float64
	noiseData []float64
	dr        []float64
}

// New creates a Nether generator using the seed provided.
func New(seed int64) *Nether {
	return NewNether(seed)
}

// NewNether creates a Nether generator using the seed provided.
func NewNether(seed int64) *Nether {
	r := mc112.NewRand(seed)
	mapGenRand := mc112.NewRand(seed)

	g := &Nether{
		seed:          seed,
		lperlinNoise1: mc112.NewNoiseOctaves(r, 16),
		lperlinNoise2: mc112.NewNoiseOctaves(r, 16),
		perlinNoise1:  mc112.NewNoiseOctaves(r, 8),

		slowsandGravelNoiseGen:        mc112.NewNoiseOctaves(r, 4),
		netherrackExclusivityNoiseGen: mc112.NewNoiseOctaves(r, 4),
		scaleNoise:                    mc112.NewNoiseOctaves(r, 10),
		depthNoise:                    mc112.NewNoiseOctaves(r, 16),

		mapGenJ: mapGenRand.Long(),
		mapGenK: mapGenRand.Long(),

		airRID:        world.BlockRuntimeID(block.Air{}),
		netherrackRID: world.BlockRuntimeID(block.Netherrack{}),
		bedrockRID:    world.BlockRuntimeID(block.Bedrock{}),
		lavaStillRID:  world.BlockRuntimeID(block.Lava{Depth: 8, Still: true}),
		lavaFlowRID:   world.BlockRuntimeID(block.Lava{Depth: 8, Still: false}),
		gravelRID:     world.BlockRuntimeID(block.Gravel{}),
		soulSandRID:   world.BlockRuntimeID(block.SoulSand{}),
		dirtRID:       world.BlockRuntimeID(block.Dirt{}),
		grassRID:      world.BlockRuntimeID(block.Grass{}),
		glowstoneRID:  world.BlockRuntimeID(block.Glowstone{}),
		fireRID:       world.BlockRuntimeID(block.Fire{}),
		quartzOreRID:  world.BlockRuntimeID(block.NetherQuartzOre{}),
		netherBricksRID:     world.BlockRuntimeID(block.NetherBricks{}),
		netherBrickFenceRID: world.BlockRuntimeID(block.NetherBrickFence{}),
		netherWartRID:       world.BlockRuntimeID(block.NetherWart{Age: 0}),

		biomeID: uint32(dfbiome.NetherWastes{}.EncodeBiome()),
	}

	if b, ok := world.BlockByName("minecraft:brown_mushroom", nil); ok {
		g.brownMushroomRID = world.BlockRuntimeID(b)
	}
	if b, ok := world.BlockByName("minecraft:red_mushroom", nil); ok {
		g.redMushroomRID = world.BlockRuntimeID(b)
	}
	if b, ok := world.BlockByName("minecraft:magma", nil); ok {
		g.magmaRID = world.BlockRuntimeID(b)
	}
	if b, ok := world.BlockByName("minecraft:mob_spawner", nil); ok {
		g.spawnerRID = world.BlockRuntimeID(b)
	}

	g.chestRID = world.BlockRuntimeID(block.Chest{Facing: cube.North})
	for _, dir := range []cube.Direction{cube.North, cube.South, cube.West, cube.East} {
		g.netherBrickStairsRID[dir] = world.BlockRuntimeID(block.Stairs{Block: block.NetherBricks{}, Facing: dir})
	}

	g.caves = newNetherCaves(seed, g)

	g.pool.New = func() any {
		return &netherScratch{
			buffer:        make([]float64, 5*17*5),
			slowsandNoise: make([]float64, 16*16),
			gravelNoise:   make([]float64, 16*16),
			depthBuffer:   make([]float64, 16*16),
		}
	}

	return g
}

// DefaultSpawn returns the default spawn position for nether worlds.
func (g *Nether) DefaultSpawn(dim world.Dimension) cube.Pos {
	return cube.Pos{0, dim.Range().Min() + 1, 0}
}

// GenerateChunk generates a single Nether chunk at the position passed.
// DefaultSpawn returns the default spawn position for nether worlds.
func (g *Nether) GenerateChunk(pos world.ChunkPos, c *chunk.Chunk) {
	s := g.pool.Get().(*netherScratch)
	defer g.pool.Put(s)

	chunkX, chunkZ := int(pos[0]), int(pos[1])
	r := mc112.NewRand(int64(chunkX)*341873128712 + int64(chunkZ)*132897987541)

	g.prepareHeights(chunkX, chunkZ, c, s)
	g.buildSurfaces(chunkX, chunkZ, c, r, s)
	g.carveCaves(chunkX, chunkZ, c)
	g.populate(chunkX, chunkZ, c, r)

	g.fillBiomes(c)
}

func (g *Nether) fillBiomes(c *chunk.Chunk) {
	minY, maxY := int16(c.Range().Min()), int16(c.Range().Max())
	for x := uint8(0); x < 16; x++ {
		for z := uint8(0); z < 16; z++ {
			for y := minY; y <= maxY; y++ {
				c.SetBiome(x, y, z, g.biomeID)
			}
		}
	}
}

func (g *Nether) prepareHeights(chunkX, chunkZ int, c *chunk.Chunk, s *netherScratch) {
	j := netherSeaLevel/2 + 1
	heights := g.getHeights(s, chunkX*4, 0, chunkZ*4, 5, 17, 5)

	minY := int16(c.Range().Min())
	maxY := int16(c.Range().Max())

	for j1 := 0; j1 < 4; j1++ {
		for k1 := 0; k1 < 4; k1++ {
			for l1 := 0; l1 < 16; l1++ {
				d1 := heights[((j1+0)*5+k1+0)*17+l1+0]
				d2 := heights[((j1+0)*5+k1+1)*17+l1+0]
				d3 := heights[((j1+1)*5+k1+0)*17+l1+0]
				d4 := heights[((j1+1)*5+k1+1)*17+l1+0]

				d5 := (heights[((j1+0)*5+k1+0)*17+l1+1] - d1) * 0.125
				d6 := (heights[((j1+0)*5+k1+1)*17+l1+1] - d2) * 0.125
				d7 := (heights[((j1+1)*5+k1+0)*17+l1+1] - d3) * 0.125
				d8 := (heights[((j1+1)*5+k1+1)*17+l1+1] - d4) * 0.125

				for i2 := 0; i2 < 8; i2++ {
					d10 := d1
					d11 := d2
					d12 := (d3 - d1) * 0.25
					d13 := (d4 - d2) * 0.25

					y := int16(i2 + l1*8)
					if y < minY || y > maxY {
						d1 += d5
						d2 += d6
						d3 += d7
						d4 += d8
						continue
					}

					for j2 := 0; j2 < 4; j2++ {
						d15 := d10
						d16 := (d11 - d10) * 0.25

						for k2 := 0; k2 < 4; k2++ {
							var rid uint32
							if int(y) < j {
								rid = g.lavaStillRID
							}
							if d15 > 0.0 {
								rid = g.netherrackRID
							}

							if rid != 0 {
								x := uint8(j2 + j1*4)
								z := uint8(k2 + k1*4)
								c.SetBlock(x, y, z, 0, rid)
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

func (g *Nether) buildSurfaces(chunkX, chunkZ int, c *chunk.Chunk, r *mc112.Rand, s *netherScratch) {
	i := netherSeaLevel + 1

	s.slowsandNoise = g.slowsandGravelNoiseGen.GenerateNoiseOctaves(s.slowsandNoise, chunkX*16, chunkZ*16, 0, 16, 16, 1, 0.03125, 0.03125, 1.0)
	s.gravelNoise = g.slowsandGravelNoiseGen.GenerateNoiseOctaves(s.gravelNoise, chunkX*16, 109, chunkZ*16, 16, 1, 16, 0.03125, 1.0, 0.03125)
	s.depthBuffer = g.netherrackExclusivityNoiseGen.GenerateNoiseOctaves(s.depthBuffer, chunkX*16, chunkZ*16, 0, 16, 16, 1, 0.0625, 0.0625, 0.0625)

	for j := 0; j < 16; j++ {
		for k := 0; k < 16; k++ {
			flag := s.slowsandNoise[j+k*16]+r.Float64()*0.2 > 0.0
			flag1 := s.gravelNoise[j+k*16]+r.Float64()*0.2 > 0.0
			l := int(s.depthBuffer[j+k*16]/3.0 + 3.0 + r.Float64()*0.25)
			i1 := -1
			topRID := g.netherrackRID
			fillerRID := g.netherrackRID

			for j1 := 127; j1 >= 0; j1-- {
				if j1 < 127-int(r.Intn(5)) && j1 > int(r.Intn(5)) {
					rid := c.Block(uint8(k), int16(j1), uint8(j), 0)
					if rid != g.airRID {
						if rid == g.netherrackRID {
							if i1 == -1 {
								if l <= 0 {
									topRID = g.airRID
									fillerRID = g.netherrackRID
								} else if j1 >= i-4 && j1 <= i+1 {
									topRID = g.netherrackRID
									fillerRID = g.netherrackRID
									if flag1 {
										topRID = g.gravelRID
										fillerRID = g.netherrackRID
									}
									if flag {
										topRID = g.soulSandRID
										fillerRID = g.soulSandRID
									}
								}

								if j1 < i && topRID == g.airRID {
									topRID = g.lavaStillRID
								}

								i1 = l
								if j1 >= i-1 {
									c.SetBlock(uint8(k), int16(j1), uint8(j), 0, topRID)
								} else {
									c.SetBlock(uint8(k), int16(j1), uint8(j), 0, fillerRID)
								}
							} else if i1 > 0 {
								i1--
								c.SetBlock(uint8(k), int16(j1), uint8(j), 0, fillerRID)
							}
						}
					} else {
						i1 = -1
					}
				} else {
					c.SetBlock(uint8(k), int16(j1), uint8(j), 0, g.bedrockRID)
				}
			}
		}
	}
}

func (g *Nether) getHeights(s *netherScratch, xOffset, yOffset, zOffset, xSize, ySize, zSize int) []float64 {
	total := xSize * ySize * zSize
	if len(s.buffer) != total {
		s.buffer = make([]float64, total)
	}

	const (
		d0 = 684.412
		d1 = 2053.236
	)
	s.noiseData = g.scaleNoise.GenerateNoiseOctaves(s.noiseData, xOffset, yOffset, zOffset, xSize, 1, zSize, 1.0, 0.0, 1.0)
	s.dr = g.depthNoise.GenerateNoiseOctaves(s.dr, xOffset, yOffset, zOffset, xSize, 1, zSize, 100.0, 0.0, 100.0)
	s.pnr = g.perlinNoise1.GenerateNoiseOctaves(s.pnr, xOffset, yOffset, zOffset, xSize, ySize, zSize, 8.555150000000001, 34.2206, 8.555150000000001)
	s.ar = g.lperlinNoise1.GenerateNoiseOctaves(s.ar, xOffset, yOffset, zOffset, xSize, ySize, zSize, d0, d1, d0)
	s.br = g.lperlinNoise2.GenerateNoiseOctaves(s.br, xOffset, yOffset, zOffset, xSize, ySize, zSize, d0, d1, d0)

	adouble := make([]float64, ySize)
	for j := 0; j < ySize; j++ {
		adouble[j] = math.Cos(float64(j)*math.Pi*6.0/float64(ySize)) * 2.0
		d2 := float64(j)
		if j > ySize/2 {
			d2 = float64(ySize - 1 - j)
		}
		if d2 < 4.0 {
			d2 = 4.0 - d2
			adouble[j] -= d2 * d2 * d2 * 10.0
		}
	}

	i := 0
	for l := 0; l < xSize; l++ {
		for i1 := 0; i1 < zSize; i1++ {
			for k := 0; k < ySize; k++ {
				d4 := adouble[k]
				d5 := s.ar[i] / 512.0
				d6 := s.br[i] / 512.0
				d7 := (s.pnr[i]/10.0 + 1.0) / 2.0

				var d8 float64
				switch {
				case d7 < 0.0:
					d8 = d5
				case d7 > 1.0:
					d8 = d6
				default:
					d8 = d5 + (d6-d5)*d7
				}
				d8 = d8 - d4

				if k > ySize-4 {
					d9 := float64(k-(ySize-4)) / 3.0
					d8 = d8*(1.0-d9) + -10.0*d9
				}

				s.buffer[i] = d8
				i++
			}
		}
	}

	return s.buffer
}
