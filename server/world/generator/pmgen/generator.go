package pmgen

import (
	"sync/atomic"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/world"
	dfbiome "github.com/df-mc/dragonfly/server/world/biome"
	"github.com/df-mc/dragonfly/server/world/chunk"
	pmBiome "github.com/df-mc/dragonfly/server/world/generator/pmgen/biome"
	"github.com/df-mc/dragonfly/server/world/generator/pmgen/populate"
	"github.com/df-mc/dragonfly/server/world/generator/pmgen/rand"
)

const SmoothSize = 2

var gaussianKernel = [5][5]float64{
	{
		1.4715177646858,
		2.141045714076,
		2.4261226388505,
		2.141045714076,
		1.4715177646858,
	},
	{
		2.141045714076,
		3.1152031322856,
		3.5299876103384,
		3.1152031322856,
		2.141045714076,
	},
	{
		2.4261226388505,
		3.5299876103384,
		4,
		3.5299876103384,
		2.4261226388505,
	},
	{
		2.141045714076,
		3.1152031322856,
		3.5299876103384,
		3.1152031322856,
		2.141045714076,
	},
	{
		1.4715177646858,
		2.141045714076,
		2.4261226388505,
		2.141045714076,
		1.4715177646858,
	},
}

type Generator struct {
	seed        int64
	waterHeight int
	noise       *simplex
	selector    *biomeSelector

	world atomic.Pointer[world.World]

	// cached runtime IDs initialised after the block registry is finalised
	bedrockRID uint32
	stoneRID   uint32
	airRID     uint32
	waterRID   uint32
}

// Runtime IDs must not be resolved at package init time because the
// world block registry is finalised during server construction. We cache
// them per-generator in New once the world is bound.

// New creates a pm-gen generator independent of a world. Population is
// started when BindWorld is called.
func New(seed int64) *Generator {
	r := rand.NewRandom(seed)
	noise := newSimplex(r, 4, 1.0/4, 1.0/32)
	r.SetSeed(seed)
	selector := newBiomeSelector(r)
	selector.recalculate()

	g := &Generator{
		seed:     seed,
		noise:    noise,
		selector: selector,
	}

	// Resolve and cache commonly used runtime IDs now that the registry
	// has been finalised by the server initialisation.
	g.bedrockRID = world.BlockRuntimeID(block.Bedrock{})
	g.stoneRID = world.BlockRuntimeID(block.Stone{})
	g.airRID = world.BlockRuntimeID(block.Air{})
	g.waterRID = world.BlockRuntimeID(block.Water{Depth: 8, Still: true})

	return g
}

// BindWorld starts population routines bound to the provided world. Safe to
// call multiple times; population starts only once.
func (g *Generator) BindWorld(w *world.World) {
	g.world.Store(w)
}

func (g *Generator) GenerateChunk(pos world.ChunkPos, c *chunk.Chunk) {
	r := rand.NewRandom(0xdeadbeef ^ (int64(pos[0]) << 8) ^ int64(pos[1]) ^ g.seed)

	noise := g.noise.getFastNoise3D(16, 128, 16, 4, 8, 4, int64(pos[0])*16, 0, int64(pos[1])*16)

	var (
		biomeCache = make(map[[2]int64]pmBiome.Biome)
		biomeCols  [16][16]pmBiome.Biome
	)
	for x := int64(0); x < 16; x++ {
		for z := int64(0); z < 16; z++ {
			var minSum, maxSum, weightSum float64

			b := g.pickBiome(int64(pos[0])*16+x, int64(pos[1])*16+z)
			biomeCols[x][z] = b
			g.applyBiomeColumn(c, uint8(x), uint8(z), b)

			for sx := int64(-SmoothSize); sx <= SmoothSize; sx++ {
				for sz := int64(-SmoothSize); sz <= SmoothSize; sz++ {
					weight := gaussianKernel[sx+SmoothSize][sz+SmoothSize]

					var adjacent pmBiome.Biome
					if sx == 0 && sz == 0 {
						adjacent = b
					} else {
						i := [2]int64{int64(pos[0])*16 + x + sx, int64(pos[1])*16 + z + sz}
						if bc, ok := biomeCache[i]; ok {
							adjacent = bc
						} else {
							adjacent = g.pickBiome(i[0], i[1])
							biomeCache[i] = adjacent
						}
					}

					min, max := adjacent.Elevation()
					minSum += float64(min-1) * weight
					maxSum += float64(max) * weight

					weightSum += weight
				}
			}

			minSum /= weightSum
			maxSum /= weightSum

			smoothHeight := (maxSum - minSum) / 2

			for y := 0; y < 128; y++ {
				if y == 0 {
					c.SetBlock(uint8(x), int16(y), uint8(z), 0, g.bedrockRID)
					continue
				}
				const waterHeight = 62

				noiseValue := noise[x][z][y] - 1.0/smoothHeight*(float64(y)-smoothHeight-minSum)
				if noiseValue > 0 {
					c.SetBlock(uint8(x), int16(y), uint8(z), 0, g.stoneRID)
				} else if y <= waterHeight {
					c.SetBlock(uint8(x), int16(y), uint8(z), 0, g.waterRID)
				}
			}
		}
	}

	for x := uint8(0); x < 16; x++ {
		for z := uint8(0); z < 16; z++ {
			b := biomeCols[x][z]
			cov := b.GroundCover()
			if len(cov) > 0 {
				var diffY int16
				if (cov[0].Model() != model.Solid{}) {
					diffY = 1
				}

				start := min(127, c.HighestLightBlocker(x, z)+diffY)
				end := start - int16(len(cov))
				for y := start; y > end && y >= 0; y-- {
					blockDef := cov[start-y]
					current := c.Block(x, y, z, 0)
					if current == g.airRID && (blockDef.Model() == model.Solid{}) {
						break
					}
					if _, ok := blockDef.(block.LiquidRemovable); ok {
						bl, _ := world.BlockByRuntimeID(current)
						if _, ok = bl.(world.Liquid); ok {
							continue
						}
					}

					rid := world.BlockRuntimeID(blockDef)
					c.SetBlock(x, y, z, 0, rid)
				}
			}
		}
	}

	centreBiome := biomeCols[7][7]
	if w := g.world.Load(); w != nil {
		for _, populator := range append([]populate.Populator{populate.Ore{Types: []populate.OreType{
			{block.CoalOre{}, block.Stone{}, 20, 16, 0, 128},
			{block.IronOre{}, block.Stone{}, 20, 8, 0, 64},
			//{ block.RedstoneOre{}, block.Stone{}, 8, 7, 0, 16 }, // TODO
			{block.LapisOre{}, block.Stone{}, 1, 6, 0, 32},
			{block.GoldOre{}, block.Stone{}, 2, 8, 0, 32},
			{block.DiamondOre{}, block.Stone{}, 1, 7, 0, 16},
			{block.Dirt{}, block.Stone{}, 20, 32, 0, 128},
			{block.Gravel{}, block.Stone{}, 10, 16, 0, 128},
		}}}, centreBiome.Populators()...) {
			populator.Populate(w, pos, c, r)
		}
	}
}

func (g *Generator) applyBiomeColumn(c *chunk.Chunk, x, z uint8, b pmBiome.Biome) {
	encoded := uint32(dragonflyBiomeFor(b).EncodeBiome())
	for y := int16(0); y < 128; y++ {
		c.SetBiome(x, y, z, encoded)
	}
}

func (g *Generator) pickBiome(x, z int64) pmBiome.Biome {
	hash := x*2345803 ^ z*9236449 ^ g.seed
	hash *= hash + 223
	xNoise := hash >> 20 & 3
	zNoise := hash >> 22 & 3
	if xNoise == 3 {
		xNoise = 1
	}
	if zNoise == 3 {
		zNoise = 1
	}

	return g.selector.pickBiome(x+xNoise-1, z+zNoise-1)
}

func dragonflyBiomeFor(b pmBiome.Biome) world.Biome {
	switch b.(type) {
	case pmBiome.Ocean:
		return dfbiome.Ocean{}
	case pmBiome.Plains:
		return dfbiome.Plains{}
	case pmBiome.Desert:
		return dfbiome.Desert{}
	case pmBiome.Mountains, pmBiome.SmallMountains:
		return dfbiome.WindsweptHills{}
	case pmBiome.Forest:
		return dfbiome.Forest{}
	case pmBiome.Taiga:
		return dfbiome.Taiga{}
	case pmBiome.Swamp:
		return dfbiome.Swamp{}
	case pmBiome.River:
		return dfbiome.River{}
	case pmBiome.IcePlains:
		return dfbiome.SnowyPlains{}
	case pmBiome.BirchForest:
		return dfbiome.BirchForest{}
	default:
		return dfbiome.Plains{}
	}
}

func min(a, b int16) int16 {
	if a < b {
		return a
	}
	return b
}
