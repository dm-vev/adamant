package mc112

import (
	"math"
	"sync"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
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

	bedrockRID    uint32
	obsidianRID   uint32
	ironBarsRID   uint32
	endPortalRID  uint32
	endGatewayRID uint32

	chorusPlantRID    uint32
	chorusFlowerRID   uint32
	chorusFlowerDead  uint32
	chorusMaxDistance int

	podiumOnce sync.Once
	podiumY    int

	spikesOnce sync.Once
	spikes     []endSpike

	endCityRandOnce sync.Once
	endCityRandJ    int64
	endCityRandK    int64
	endCityCache    sync.Map // world.ChunkPos -> *endCityStructure (nil when absent)

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
		bedrockRID:    world.BlockRuntimeID(block.Bedrock{}),
		obsidianRID:   world.BlockRuntimeID(block.Obsidian{}),
		ironBarsRID:   world.BlockRuntimeID(block.IronBars{}),
		endPortalRID:  world.BlockRuntimeID(block.EndPortal{}),
		endGatewayRID: world.BlockRuntimeID(block.EndGateway{}),
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
	g.generateTerrain(chunkX, chunkZ, c, s)

	g.generateCentralStructures(chunkX, chunkZ, c)
	g.populate(chunkX, chunkZ, c, r)
}

func (g *End) generateTerrain(chunkX, chunkZ int, c *chunk.Chunk, s *endScratch) {
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
}

type endSpike struct {
	x       int
	z       int
	radius  int
	height  int
	guarded bool
}

func (g *End) generateCentralStructures(chunkX, chunkZ int, c *chunk.Chunk) {
	// Central End features are limited to a small area around (0, 0).
	if abs(chunkX) > 8 || abs(chunkZ) > 8 {
		return
	}
	g.generateExitPodium(chunkX, chunkZ, c)
	g.generateObsidianSpikes(chunkX, chunkZ, c)
	g.generateSpawnPlatform(chunkX, chunkZ, c)
}

func (g *End) generateSpawnPlatform(chunkX, chunkZ int, c *chunk.Chunk) {
	// Matches the platform created by Teleporter.placeInPortal when entering the End in Java 1.12.
	const (
		spawnX = 100
		spawnY = 50
		spawnZ = 0
	)

	// Teleporter uses j = floor(posY)-1 with posY=spawnY.
	baseY := spawnY - 2 // obsidian layer at y = j-1
	for dx := -2; dx <= 2; dx++ {
		for dz := -2; dz <= 2; dz++ {
			wx := spawnX + dx
			wz := spawnZ + dz
			setBlockWorld(c, chunkX, chunkZ, wx, baseY, wz, g.obsidianRID)
			for wy := baseY + 1; wy <= baseY+3; wy++ {
				setBlockWorld(c, chunkX, chunkZ, wx, wy, wz, g.airRID)
			}
		}
	}
}

func (g *End) generateExitPodium(chunkX, chunkZ int, c *chunk.Chunk) {
	y := g.exitPodiumY()
	minY, maxY := c.Range().Min(), c.Range().Max()
	if y < minY || y > maxY {
		return
	}

	const (
		rOuterSq = 3.5 * 3.5
		rInnerSq = 2.5 * 2.5
	)

	for wx := -4; wx <= 4; wx++ {
		for wz := -4; wz <= 4; wz++ {
			dx := float64(wx)
			dz := float64(wz)
			distSq := dx*dx + dz*dz
			if distSq > rOuterSq {
				continue
			}

			for wy := y - 1; wy <= y+32; wy++ {
				if wy < minY || wy > maxY {
					continue
				}

				switch {
				case wy < y:
					if distSq <= rInnerSq {
						setBlockWorld(c, chunkX, chunkZ, wx, wy, wz, g.bedrockRID)
					} else {
						setBlockWorld(c, chunkX, chunkZ, wx, wy, wz, g.endStoneRID)
					}
				case wy > y:
					setBlockWorld(c, chunkX, chunkZ, wx, wy, wz, g.airRID)
				default:
					if distSq > rInnerSq {
						setBlockWorld(c, chunkX, chunkZ, wx, wy, wz, g.bedrockRID)
					} else {
						setBlockWorld(c, chunkX, chunkZ, wx, wy, wz, g.endPortalRID)
					}
				}
			}
		}
	}

	for i := 0; i < 4; i++ {
		setBlockWorld(c, chunkX, chunkZ, 0, y+i, 0, g.bedrockRID)
	}

	centreY := y + 2
	for _, dir := range cube.Directions() {
		offset := directionOffset(dir)
		torch := block.Torch{Type: block.NormalFire(), Facing: dir.Opposite().Face()}
		setBlockWorld(c, chunkX, chunkZ, offset[0], centreY, offset[2], world.BlockRuntimeID(torch))
	}
}

func (g *End) exitPodiumY() int {
	g.podiumOnce.Do(func() {
		// Match DragonFightManager's selection of the exit portal location:
		// find the top solid block at (0, 0) and place the podium one block below.
		s := g.pool.Get().(*endScratch)
		defer g.pool.Put(s)

		heights := g.getHeights(s, 0, 0, 0, 3, 33, 3)
		top := 0

		for k1 := 0; k1 < 32; k1++ {
			d1 := heights[(0*3+0)*33+k1+0]
			d5 := (heights[(0*3+0)*33+k1+1] - d1) * 0.25

			for l1 := 0; l1 < 4; l1++ {
				if d1 > 0.0 {
					y := l1 + k1*4
					if y > top {
						top = y
					}
				}
				d1 += d5
			}
		}

		if top > 0 {
			g.podiumY = top - 1
		}
	})
	return g.podiumY
}

func (g *End) generateObsidianSpikes(chunkX, chunkZ int, c *chunk.Chunk) {
	for _, spike := range g.spikeList() {
		g.generateSpike(chunkX, chunkZ, c, spike)
	}
}

func (g *End) spikeList() []endSpike {
	g.spikesOnce.Do(func() {
		// BiomeEndDecorator.getSpikesForWorld, Java 1.12:
		// Random(worldSeed).nextLong() & 65535, used as seed for shuffling [0..9].
		r0 := newJavaRand(g.seed)
		seed := uint64(r0.Long()) & 65535

		perm := make([]int, 10)
		for i := range perm {
			perm[i] = i
		}
		r := newJavaRand(int64(seed))
		shuffleInts(r, perm)

		spikes := make([]endSpike, 0, 10)
		for i := 0; i < 10; i++ {
			angle := 2.0 * (-math.Pi + (math.Pi/10.0)*float64(i))
			x := int(42.0 * math.Cos(angle))
			z := int(42.0 * math.Sin(angle))

			l := perm[i]
			radius := 2 + l/3
			height := 76 + l*3
			guarded := l == 1 || l == 2
			spikes = append(spikes, endSpike{x: x, z: z, radius: radius, height: height, guarded: guarded})
		}
		g.spikes = spikes
	})
	return g.spikes
}

func (g *End) generateSpike(chunkX, chunkZ int, c *chunk.Chunk, spike endSpike) {
	wxMin, wxMax := spike.x-spike.radius, spike.x+spike.radius
	wzMin, wzMax := spike.z-spike.radius, spike.z+spike.radius

	chunkWXMin := chunkX << 4
	chunkWZMin := chunkZ << 4
	chunkWXMax := chunkWXMin + 15
	chunkWZMax := chunkWZMin + 15

	if wxMax < chunkWXMin || wxMin > chunkWXMax || wzMax < chunkWZMin || wzMin > chunkWZMax {
		return
	}

	minY, maxY := c.Range().Min(), c.Range().Max()
	yMax := min(maxY, spike.height+10)

	rSq := spike.radius*spike.radius + 1
	for wx := max(wxMin, chunkWXMin); wx <= min(wxMax, chunkWXMax); wx++ {
		dx := wx - spike.x
		for wz := max(wzMin, chunkWZMin); wz <= min(wzMax, chunkWZMax); wz++ {
			dz := wz - spike.z
			distSq := dx*dx + dz*dz

			for wy := 0; wy <= yMax; wy++ {
				if wy < minY || wy > maxY {
					continue
				}
				if distSq <= rSq && wy < spike.height {
					setBlockWorld(c, chunkX, chunkZ, wx, wy, wz, g.obsidianRID)
				} else if wy > 65 {
					setBlockWorld(c, chunkX, chunkZ, wx, wy, wz, g.airRID)
				}
			}
		}
	}

	if spike.height < minY || spike.height > maxY {
		return
	}

	if spike.guarded {
		for dx := -2; dx <= 2; dx++ {
			for dz := -2; dz <= 2; dz++ {
				x := spike.x + dx
				z := spike.z + dz
				if abs(dx) == 2 || abs(dz) == 2 {
					setBlockWorld(c, chunkX, chunkZ, x, spike.height, z, g.ironBarsRID)
					setBlockWorld(c, chunkX, chunkZ, x, spike.height+1, z, g.ironBarsRID)
					setBlockWorld(c, chunkX, chunkZ, x, spike.height+2, z, g.ironBarsRID)
				}
				setBlockWorld(c, chunkX, chunkZ, x, spike.height+3, z, g.ironBarsRID)
			}
		}
	}

	setBlockWorld(c, chunkX, chunkZ, spike.x, spike.height, spike.z, g.bedrockRID)
}

func shuffleInts(r *javaRand, list []int) {
	for i := len(list); i > 1; i-- {
		j := int(r.Intn(int32(i)))
		list[i-1], list[j] = list[j], list[i-1]
	}
}

func directionOffset(d cube.Direction) cube.Pos {
	switch d {
	case cube.North:
		return cube.Pos{0, 0, -1}
	case cube.South:
		return cube.Pos{0, 0, 1}
	case cube.West:
		return cube.Pos{-1, 0, 0}
	case cube.East:
		return cube.Pos{1, 0, 0}
	}
	panic("invalid direction")
}

func setBlockWorld(c *chunk.Chunk, chunkX, chunkZ, worldX, worldY, worldZ int, rid uint32) {
	if worldX>>4 != chunkX || worldZ>>4 != chunkZ {
		return
	}
	if int16(worldY) < int16(c.Range().Min()) || int16(worldY) > int16(c.Range().Max()) {
		return
	}
	c.SetBlock(uint8(worldX&15), int16(worldY), uint8(worldZ&15), 0, rid)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
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

	g.applyEndCityStructures(chunkX, chunkZ, c)

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

	// Random End gateways between islands.
	// In Java 1.12 this is placed with a 1/700 chance per populated chunk in the outer islands.
	if r.Intn(700) == 0 {
		x := uint8(r.Intn(16))
		z := uint8(r.Intn(16))

		top := c.HighestBlock(x, z)
		if top <= 0 {
			return
		}
		y := int(top) + 4 + int(r.Intn(7))
		if int16(y) < int16(c.Range().Min()) || int16(y) > int16(c.Range().Max()) {
			return
		}

		worldX := (chunkX << 4) + int(x)
		worldZ := (chunkZ << 4) + int(z)
		g.generateEndGatewayStructure(chunkX, chunkZ, c, worldX, y, worldZ)
	}
}

// generateEndGatewayStructure places the 3x5x3 bedrock frame with an end gateway in the centre, matching
// WorldGenEndGateway in Minecraft Java Edition 1.12.
func (g *End) generateEndGatewayStructure(chunkX, chunkZ int, c *chunk.Chunk, worldX, worldY, worldZ int) {
	for wx := worldX - 1; wx <= worldX+1; wx++ {
		for wy := worldY - 2; wy <= worldY+2; wy++ {
			for wz := worldZ - 1; wz <= worldZ+1; wz++ {
				flagX := wx == worldX
				flagY := wy == worldY
				flagZ := wz == worldZ
				flagY2 := abs(wy-worldY) == 2

				switch {
				case flagX && flagY && flagZ:
					setBlockWorld(c, chunkX, chunkZ, wx, wy, wz, g.endGatewayRID)
				case flagY:
					setBlockWorld(c, chunkX, chunkZ, wx, wy, wz, g.airRID)
				case flagY2 && flagX && flagZ:
					setBlockWorld(c, chunkX, chunkZ, wx, wy, wz, g.bedrockRID)
				case (flagX || flagZ) && !flagY2:
					setBlockWorld(c, chunkX, chunkZ, wx, wy, wz, g.bedrockRID)
				default:
					setBlockWorld(c, chunkX, chunkZ, wx, wy, wz, g.airRID)
				}
			}
		}
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
