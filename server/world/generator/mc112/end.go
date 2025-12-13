package mc112

import (
	"math"
	"sync"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/world"
	dfbiome "github.com/df-mc/dragonfly/server/world/biome"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// End generates terrain for the End dimension using the Minecraft Java Edition 1.12 algorithm.
//
// The implementation is concurrency-safe and may be called from multiple generator workers.
type End struct {
	seed int64

	lperlinNoise1 *noiseOctaves
	lperlinNoise2 *noiseOctaves
	perlinNoise1  *noiseOctaves
	noiseGen5     *noiseOctaves // Unused, but instantiated to match Java RNG consumption.
	noiseGen6     *noiseOctaves // Unused, but instantiated to match Java RNG consumption.
	islandNoise   *noiseSimplex

	endStoneRID uint32
	airRID      uint32
	biomeID     uint32

	chorusPlantRID    uint32
	chorusFlowerRID   uint32
	chorusFlowerDead  uint32
	chorusMaxDistance int

	pool sync.Pool
}

type endScratch struct {
	heights []float64
	pnr     []float64
	ar      []float64
	br      []float64
}

// NewEnd creates an End generator using the seed provided.
func NewEnd(seed int64) *End {
	r := newJavaRand(seed)

	g := &End{
		seed:          seed,
		lperlinNoise1: newNoiseOctaves(r, 16),
		lperlinNoise2: newNoiseOctaves(r, 16),
		perlinNoise1:  newNoiseOctaves(r, 8),
		noiseGen5:     newNoiseOctaves(r, 10),
		noiseGen6:     newNoiseOctaves(r, 16),
		islandNoise:   newNoiseSimplex(r),
		endStoneRID:   world.BlockRuntimeID(block.EndStone{}),
		airRID:        world.BlockRuntimeID(block.Air{}),
		biomeID:       uint32(dfbiome.End{}.EncodeBiome()),
		// Use runtime IDs directly: chorus flowers require specific state IDs (age), and this avoids per-block allocations.
		chorusPlantRID:    mustStateRID("minecraft:chorus_plant", nil),
		chorusFlowerRID:   mustStateRID("minecraft:chorus_flower", nil),
		chorusFlowerDead:  findChorusFlowerDeadRID(),
		chorusMaxDistance: 8,
	}
	g.pool.New = func() any {
		// The End height buffer is always 3*33*3.
		const n = 3 * 33 * 3
		return &endScratch{
			heights: make([]float64, n),
			pnr:     make([]float64, n),
			ar:      make([]float64, n),
			br:      make([]float64, n),
		}
	}
	return g
}

// GenerateChunk generates a single End chunk at the position passed.
func (g *End) GenerateChunk(pos world.ChunkPos, c *chunk.Chunk) {
	g.fillBiomes(c)

	s := g.pool.Get().(*endScratch)
	defer g.pool.Put(s)

	chunkX, chunkZ := int(pos[0]), int(pos[1])
	r := newJavaRand(int64(chunkX)*341873128712 + int64(chunkZ)*132897987541)
	heights := g.getHeights(s, chunkX*2, 0, chunkZ*2, 3, 33, 3)

	baseY := int16(c.Range().Min())
	for i1 := 0; i1 < 2; i1++ {
		for j1 := 0; j1 < 2; j1++ {
			for k1 := 0; k1 < 32; k1++ {
				d1 := heights[((i1+0)*3+j1+0)*33+k1+0]
				d2 := heights[((i1+0)*3+j1+1)*33+k1+0]
				d3 := heights[((i1+1)*3+j1+0)*33+k1+0]
				d4 := heights[((i1+1)*3+j1+1)*33+k1+0]

				d5 := (heights[((i1+0)*3+j1+0)*33+k1+1] - d1) * 0.25
				d6 := (heights[((i1+0)*3+j1+1)*33+k1+1] - d2) * 0.25
				d7 := (heights[((i1+1)*3+j1+0)*33+k1+1] - d3) * 0.25
				d8 := (heights[((i1+1)*3+j1+1)*33+k1+1] - d4) * 0.25

				for l1 := 0; l1 < 4; l1++ {
					d10 := d1
					d11 := d2
					d12 := (d3 - d1) * 0.125
					d13 := (d4 - d2) * 0.125

					for i2 := 0; i2 < 8; i2++ {
						d15 := d10
						d16 := (d11 - d10) * 0.125

						for j2 := 0; j2 < 8; j2++ {
							if d15 > 0.0 {
								x := uint8(i2 + i1*8)
								y := baseY + int16(l1+k1*4)
								z := uint8(j2 + j1*8)
								c.SetBlock(x, y, z, 0, g.endStoneRID)
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

	g.populate(chunkX, chunkZ, c, r)
}

func (g *End) fillBiomes(c *chunk.Chunk) {
	minY, maxY := int16(c.Range().Min()), int16(c.Range().Max())
	for x := uint8(0); x < 16; x++ {
		for z := uint8(0); z < 16; z++ {
			for y := minY; y <= maxY; y++ {
				c.SetBiome(x, y, z, g.biomeID)
			}
		}
	}
}

func (g *End) getHeights(s *endScratch, xOffset, yOffset, zOffset, xSize, ySize, zSize int) []float64 {
	total := xSize * ySize * zSize
	heights := s.heights
	if len(heights) != total {
		heights = make([]float64, total)
		s.heights = heights
	}

	d0 := 684.412
	d0 *= 2.0

	s.pnr = g.perlinNoise1.generateNoiseOctaves(s.pnr, xOffset, yOffset, zOffset, xSize, ySize, zSize, d0/80.0, 4.277575000000001, d0/80.0)
	s.ar = g.lperlinNoise1.generateNoiseOctaves(s.ar, xOffset, yOffset, zOffset, xSize, ySize, zSize, d0, 684.412, d0)
	s.br = g.lperlinNoise2.generateNoiseOctaves(s.br, xOffset, yOffset, zOffset, xSize, ySize, zSize, d0, 684.412, d0)

	i := xOffset / 2
	j := zOffset / 2

	k := 0
	for l := 0; l < xSize; l++ {
		for i1 := 0; i1 < zSize; i1++ {
			f := g.islandHeightValue(i, j, l, i1)

			for j1 := 0; j1 < ySize; j1++ {
				d2 := s.ar[k] / 512.0
				d3 := s.br[k] / 512.0
				d5 := (s.pnr[k]/10.0 + 1.0) / 2.0

				var d4 float64
				switch {
				case d5 < 0.0:
					d4 = d2
				case d5 > 1.0:
					d4 = d3
				default:
					d4 = d2 + (d3-d2)*d5
				}

				d4 = d4 - 8.0
				d4 = d4 + float64(f)

				k1 := 2
				if j1 > ySize/2-k1 {
					d6 := float64(float32(j1-(ySize/2-k1)) / 64.0)
					d6 = clamp(d6, 0.0, 1.0)
					d4 = d4*(1.0-d6) + -3000.0*d6
				}

				k1 = 8
				if j1 < k1 {
					d7 := float64(float32(k1-j1) / (float32(k1) - 1.0))
					d4 = d4*(1.0-d7) + -30.0*d7
				}

				heights[k] = d4
				k++
			}
		}
	}

	return heights
}

func (g *End) islandHeightValue(chunkX, chunkZ, x, z int) float32 {
	f := float32(chunkX*2 + x)
	f1 := float32(chunkZ*2 + z)
	f2 := float32(100.0) - float32(math.Sqrt(float64(f*f+f1*f1)))*8.0

	if f2 > 80.0 {
		f2 = 80.0
	}
	if f2 < -100.0 {
		f2 = -100.0
	}

	for i := -12; i <= 12; i++ {
		for j := -12; j <= 12; j++ {
			k := int64(chunkX + i)
			l := int64(chunkZ + j)

			if k*k+l*l > 4096 && g.islandNoise.getValue(float64(k), float64(l)) < -0.8999999761581421 {
				f3 := float32(math.Mod(float64(abs32(float32(k))*3439.0+abs32(float32(l))*147.0), 13.0)) + 9.0

				f = float32(x - i*2)
				f1 = float32(z - j*2)
				f4 := float32(100.0) - float32(math.Sqrt(float64(f*f+f1*f1)))*f3

				if f4 > 80.0 {
					f4 = 80.0
				}
				if f4 < -100.0 {
					f4 = -100.0
				}
				if f4 > f2 {
					f2 = f4
				}
			}
		}
	}
	return f2
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

func (g *End) populate(chunkX, chunkZ int, c *chunk.Chunk, r *javaRand) {
	distSq := int64(chunkX)*int64(chunkX) + int64(chunkZ)*int64(chunkZ)
	if distSq <= 4096 {
		return
	}

	f := g.islandHeightValue(chunkX, chunkZ, 1, 1)
	if f < -20.0 && r.Intn(14) == 0 {
		g.generateEndIsland(c, int(r.Intn(16)), 55+int(r.Intn(16)), int(r.Intn(16)), r)
		if r.Intn(4) == 0 {
			g.generateEndIsland(c, int(r.Intn(16)), 55+int(r.Intn(16)), int(r.Intn(16)), r)
		}
	}

	if f <= 40.0 {
		return
	}

	j := int(r.Intn(5))
	for i := 0; i < j; i++ {
		x := uint8(r.Intn(16))
		z := uint8(r.Intn(16))

		top := c.HighestBlock(x, z)
		if top <= 0 {
			continue
		}
		if c.Block(x, top, z, 0) != g.endStoneRID {
			continue
		}
		if top+1 > int16(c.Range().Max()) {
			continue
		}
		if c.Block(x, top+1, z, 0) != g.airRID {
			continue
		}
		g.generateChorusPlant(c, int(x), int(top+1), int(z), r, g.chorusMaxDistance)
	}
}

func (g *End) generateEndIsland(c *chunk.Chunk, centreX, centreY, centreZ int, r *javaRand) {
	f := float64(r.Intn(3) + 4)
	minY, maxY := int16(c.Range().Min()), int16(c.Range().Max())

	for i := 0; f > 0.5; i-- {
		y := int16(centreY + i)
		if y < minY || y > maxY {
			f -= float64(r.Intn(2)) + 0.5
			continue
		}

		start := int(math.Floor(-f))
		end := int(math.Ceil(f))
		rad := (f + 1.0) * (f + 1.0)

		for dx := start; dx <= end; dx++ {
			for dz := start; dz <= end; dz++ {
				if float64(dx*dx+dz*dz) > rad {
					continue
				}
				x := centreX + dx
				z := centreZ + dz
				if x < 0 || x > 15 || z < 0 || z > 15 {
					continue
				}
				c.SetBlock(uint8(x), y, uint8(z), 0, g.endStoneRID)
			}
		}

		f -= float64(r.Intn(2)) + 0.5
	}
}

func (g *End) generateChorusPlant(c *chunk.Chunk, x, y, z int, r *javaRand, maxDistance int) {
	g.setBlockRuntimeID(c, x, y, z, g.chorusPlantRID)
	g.growChorusRecursive(c, x, y, z, r, x, z, maxDistance, 0)
}

func (g *End) growChorusRecursive(c *chunk.Chunk, x, y, z int, r *javaRand, originX, originZ, maxDistance, depth int) {
	i := int(r.Intn(4) + 1)
	if depth == 0 {
		i++
	}

	for j := 0; j < i; j++ {
		yy := y + j + 1
		if !g.areAllHorizontalNeighborsAir(c, x, yy, z, -1) {
			return
		}
		g.setBlockRuntimeID(c, x, yy, z, g.chorusPlantRID)
	}

	branched := false
	if depth < 4 {
		l := int(r.Intn(4))
		if depth == 0 {
			l++
		}

		for k := 0; k < l; k++ {
			dx, dz, opposite := randomHorizontal(r)
			nx, nz := x+dx, z+dz
			ny := y + i

			if abs(nx-originX) >= maxDistance || abs(nz-originZ) >= maxDistance {
				continue
			}
			if !g.isAir(c, nx, ny, nz) || !g.isAir(c, nx, ny-1, nz) {
				continue
			}
			if !g.areAllHorizontalNeighborsAir(c, nx, ny, nz, opposite) {
				continue
			}

			branched = true
			g.setBlockRuntimeID(c, nx, ny, nz, g.chorusPlantRID)
			g.growChorusRecursive(c, nx, ny, nz, r, originX, originZ, maxDistance, depth+1)
		}
	}

	if !branched {
		g.setBlockRuntimeID(c, x, y+i, z, g.chorusFlowerDead)
	}
}

func (g *End) isAir(c *chunk.Chunk, x, y, z int) bool {
	if x < 0 || x > 15 || z < 0 || z > 15 {
		return false
	}
	yy := int16(y)
	if yy < int16(c.Range().Min()) || yy > int16(c.Range().Max()) {
		return false
	}
	return c.Block(uint8(x), yy, uint8(z), 0) == g.airRID
}

// areAllHorizontalNeighborsAir matches BlockChorusFlower.areAllNeighborsEmpty for the current chunk.
// excluding is a horizontal index 0..3 or -1 for none.
func (g *End) areAllHorizontalNeighborsAir(c *chunk.Chunk, x, y, z, excluding int) bool {
	dirs := [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}
	for i, d := range dirs {
		if i == excluding {
			continue
		}
		if !g.isAir(c, x+d[0], y, z+d[1]) {
			return false
		}
	}
	return true
}

func (g *End) setBlockRuntimeID(c *chunk.Chunk, x, y, z int, rid uint32) {
	if x < 0 || x > 15 || z < 0 || z > 15 {
		return
	}
	yy := int16(y)
	if yy < int16(c.Range().Min()) || yy > int16(c.Range().Max()) {
		return
	}
	c.SetBlock(uint8(x), yy, uint8(z), 0, rid)
}

func randomHorizontal(r *javaRand) (dx, dz, opposite int) {
	switch r.Intn(4) {
	case 0:
		return 1, 0, 1 // +X opposite is -X
	case 1:
		return -1, 0, 0 // -X opposite is +X
	case 2:
		return 0, 1, 3 // +Z opposite is -Z
	default:
		return 0, -1, 2 // -Z opposite is +Z
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func mustStateRID(name string, props map[string]any) uint32 {
	rid, ok := chunk.StateToRuntimeID(name, props)
	if !ok {
		panic("mc112: missing runtime ID for " + name)
	}
	return rid
}

func findChorusFlowerDeadRID() uint32 {
	var (
		bestRID uint32
		bestAge int
		found   bool
	)
	for rid := uint32(0); ; rid++ {
		name, props, ok := chunk.RuntimeIDToState(rid)
		if !ok {
			break
		}
		if name != "minecraft:chorus_flower" {
			continue
		}
		raw, ok := props["age"]
		if !ok {
			continue
		}
		var age int
		switch v := raw.(type) {
		case uint8:
			age = int(v)
		case int32:
			age = int(v)
		default:
			continue
		}

		if !found || age > bestAge {
			bestRID, bestAge, found = rid, age, true
		}
	}
	if !found {
		panic("mc112: missing chorus flower states")
	}
	return bestRID
}
