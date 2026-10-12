package end

import (
	"math"
	"sync"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/world"
	dfbiome "github.com/df-mc/dragonfly/server/world/biome"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/go-gl/mathgl/mgl64"
)

// End generates Java 26 End terrain and deterministic chunk features.
//
// The implementation is concurrency-safe and may be called from multiple generator workers.
type End struct {
	seed           int64
	biomeZoomSeed  int64
	motionBlocking sync.Map // native runtime ID -> Java heightmap tag membership

	baseNoise   *java26.BlendedNoise
	islandNoise *java26.SimplexNoise

	endStoneRID uint32
	airRID      uint32
	biomeID     uint32

	bedrockRID    uint32
	obsidianRID   uint32
	ironBarsRIDs  [16]uint32
	fireRID       uint32
	endGatewayRID uint32

	chorusPlantStates [64]world.NativeState
	chorusFlowerRIDs  [6]uint32
	chorusMaxDistance int

	spikes       [10]java26.EndSpike
	endCityCache sync.Map // world.ChunkPos -> *endCityStructure (nil when absent)

	pool sync.Pool
}

type endScratch struct {
	heights []float32
}

// New creates an End generator using the seed provided.
func New(seed int64) *End {
	return NewEnd(seed)
}

// NewEnd creates an End generator using the seed provided.
func NewEnd(seed int64) *End {
	terrainRandom := java26.NewLegacyRandom(seed)
	islandRandom := java26.NewLegacyRandom(seed)
	islandRandom.Consume(17292)

	g := &End{
		seed:              seed,
		biomeZoomSeed:     java26.BiomeZoomSeed(seed),
		spikes:            java26.EndSpikes(seed),
		baseNoise:         java26.NewBlendedNoise(&terrainRandom, 0.25, 0.25, 80, 160, 4),
		islandNoise:       java26.NewSimplexNoiseWithoutOffset(&islandRandom),
		endStoneRID:       world.BlockRuntimeID(block.EndStone{}),
		airRID:            world.BlockRuntimeID(block.Air{}),
		biomeID:           uint32(dfbiome.End{}.EncodeBiome()),
		bedrockRID:        world.BlockRuntimeID(block.Bedrock{}),
		obsidianRID:       world.BlockRuntimeID(block.Obsidian{}),
		ironBarsRIDs:      [16]uint32{},
		fireRID:           world.BlockRuntimeID(block.Fire{Type: block.NormalFire()}),
		endGatewayRID:     world.BlockRuntimeID(block.EndGateway{}),
		chorusMaxDistance: 8,
	}
	g.pool.New = func() any { return &endScratch{heights: make([]float32, 3*33*3)} }
	for age := range g.chorusFlowerRIDs {
		g.chorusFlowerRIDs[age] = mustStateRID("minecraft:chorus_flower", map[string]any{"age": int32(age)})
	}
	for mask := range g.chorusPlantStates {
		state, err := world.ResolveState(world.DefaultBlockRegistry, world.BlockState{
			Name: "minecraft:chorus_plant",
			Properties: map[string]any{
				"down": mask&1 != 0, "up": mask&2 != 0,
				"north": mask&4 != 0, "east": mask&8 != 0,
				"south": mask&16 != 0, "west": mask&32 != 0,
			},
		})
		if err != nil {
			panic(err)
		}
		g.chorusPlantStates[mask] = state
	}
	for mask := range g.ironBarsRIDs {
		g.ironBarsRIDs[mask] = world.BlockRuntimeID(block.IronBars{
			NorthConnection: mask&1 != 0,
			EastConnection:  mask&2 != 0,
			SouthConnection: mask&4 != 0,
			WestConnection:  mask&8 != 0,
		})
	}
	return g
}

// GenerateChunk generates a single End chunk at the position passed.
func (g *End) GenerateChunk(pos world.ChunkPos, c *chunk.Chunk, context world.GenerationContext) world.GenerationResult {
	s := g.pool.Get().(*endScratch)
	defer g.pool.Put(s)

	chunkX, chunkZ := int(pos[0]), int(pos[1])
	region := newEndFeatureRegion(g, chunkX, chunkZ, c, s)
	var result world.GenerationResult
	g.populate(chunkX, chunkZ, region, &result)
	region.publishTarget(chunkX, chunkZ, c)
	return result
}

// DefaultSpawn returns the vanilla End arrival position.
func (*End) DefaultSpawn(world.Dimension) cube.Pos {
	return cube.Pos{100, 50, 0}
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
							density := squeeze32(d15)
							if density > 0 {
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

func (g *End) generateCentralStructures(origin endFeatureOrigin, targetX, targetZ int, result *world.GenerationResult, random *endRand) {
	if abs(origin.chunkX) > 8 || abs(origin.chunkZ) > 8 {
		return
	}
	g.generateObsidianSpikes(origin, targetX, targetZ, result, random)
}

func (g *End) generateObsidianSpikes(origin endFeatureOrigin, targetX, targetZ int, result *world.GenerationResult, random *endRand) {
	for i := range g.spikes {
		g.generateSpike(origin, targetX, targetZ, &g.spikes[i], result, random)
	}
}

func (g *End) generateSpike(origin endFeatureOrigin, targetX, targetZ int, spike *java26.EndSpike, result *world.GenerationResult, random *endRand) {
	if spike.X>>4 != origin.chunkX || spike.Z>>4 != origin.chunkZ {
		return
	}
	minChunkX, maxChunkX := origin.chunkX-endReplayRadius, origin.chunkX+endReplayRadius
	minChunkZ, maxChunkZ := origin.chunkZ-endReplayRadius, origin.chunkZ+endReplayRadius
	wxMin := max(spike.X-spike.Radius, minChunkX<<4)
	wxMax := min(spike.X+spike.Radius, (maxChunkX<<4)+15)
	wzMin := max(spike.Z-spike.Radius, minChunkZ<<4)
	wzMax := min(spike.Z+spike.Radius, (maxChunkZ<<4)+15)
	if wxMin > wxMax || wzMin > wzMax {
		return
	}
	minY, maxY := origin.region.minY, origin.region.maxY
	yMax := min(maxY, spike.Height+10)
	materials := [...]uint32{0, g.airRID, g.obsidianRID, g.bedrockRID, g.fireRID}
	for wx := wxMin; wx <= wxMax; wx++ {
		for wz := wzMin; wz <= wzMax; wz++ {
			for wy := minY; wy <= yMax; wy++ {
				material, connections := spike.BlockAt(wx, wy, wz, minY)
				if material == java26.EndSpikeUnchanged {
					continue
				}
				rid := g.ironBarsRIDs[connections]
				if material != java26.EndSpikeIronBars {
					rid = materials[material]
				}
				origin.setBlock(wx, wy, wz, rid)
			}
		}
	}
	yaw := float64(random.random.NextFloat() * 360)
	if origin.isTarget(targetX, targetZ) {
		result.Entities = append(result.Entities, entity.NewEndCrystal(world.EntitySpawnOpts{
			Position: mgl64.Vec3{float64(spike.X) + 0.5, float64(spike.Height + 1), float64(spike.Z) + 0.5},
			Rotation: cube.Rotation{yaw, 0},
		}, entity.EndCrystalBehaviourConfig{ShowBase: true}))
	}
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

func (g *End) getHeights(s *endScratch, xOffset, yOffset, zOffset, xSize, ySize, zSize int) []float32 {
	total := xSize * ySize * zSize
	heights := s.heights
	if len(heights) != total {
		heights = make([]float32, total)
		s.heights = heights
	}

	index := 0
	for x := 0; x < xSize; x++ {
		blockX := (xOffset + x) * 8
		for z := 0; z < zSize; z++ {
			blockZ := (zOffset + z) * 8
			islandDensity := (g.islandHeightValue(xOffset/2, zOffset/2, x, z) - 8) / 128
			for y := 0; y < ySize; y++ {
				blockY := yOffset + y*4
				density := islandDensity + g.baseNoise.Sample(int32(blockX), int32(blockY), int32(blockZ))
				topFactor := 1 - clamp32(float32(blockY-56)/256, 0, 1)
				density = lerp32(topFactor, -23.4375, density)
				bottomFactor := clamp32(float32(blockY-4)/28, 0, 1)
				density = lerp32(bottomFactor, -0.234375, density)
				heights[index] = density * 0.64
				index++
			}
		}
	}
	return heights
}

func lerp32(alpha, first, second float32) float32 {
	return first + alpha*(second-first)
}
func squeeze32(value float32) float32 {
	value = clamp32(value, -1, 1)
	return value/2 - value*value*value/24
}

func (g *End) islandHeightValue(chunkX, chunkZ, x, z int) float32 {
	return g.endIslandHeight((chunkX*2+x)*8, (chunkZ*2+z)*8)
}

func (g *End) endIslandHeight(blockX, blockZ int) float32 {
	sectionX, sectionZ := blockX/8, blockZ/8
	chunkX, chunkZ := sectionX/2, sectionZ/2
	subX, subZ := sectionX%2, sectionZ%2

	dx, dz := float32(blockX), float32(blockZ)
	mainIsland := clamp32(100-float32(math.Sqrt(float64(dx*dx+dz*dz))), -100, 80)
	outerIslands := float32(-100)
	for islandOffsetX := -12; islandOffsetX <= 12; islandOffsetX++ {
		for islandOffsetZ := -12; islandOffsetZ <= 12; islandOffsetZ++ {
			islandX, islandZ := int64(chunkX+islandOffsetX), int64(chunkZ+islandOffsetZ)
			if islandX*islandX+islandZ*islandZ <= 4096 || g.islandNoise.Sample2D(float64(islandX), float64(islandZ)) >= float64(float32(-0.9)) {
				continue
			}
			sizeValue := float32(abs64(islandX))*3439 + float32(abs64(islandZ))*147
			size := float32(math.Mod(float64(sizeValue), 13)) + 9
			offsetX := float32(subX - islandOffsetX*2)
			offsetZ := float32(subZ - islandOffsetZ*2)
			candidate := clamp32(100-float32(math.Sqrt(float64(offsetX*offsetX+offsetZ*offsetZ)))*size, -100, 80)
			if candidate > outerIslands {
				outerIslands = candidate
			}
		}
	}
	if outerIslands > mainIsland {
		return outerIslands
	}
	return mainIsland
}

func clamp32(value, lower, upper float32) float32 {
	if value < lower {
		return lower
	}
	if value > upper {
		return upper
	}
	return value
}

func abs64(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}

// javaNoiseBiomeAt resolves TheEndBiomeSource at unzoomed Java quart
// coordinates. It intentionally returns Java's source biome key rather than
// the native Bedrock biome ID, whose palette may contain only minecraft:the_end.
func (g *End) javaNoiseBiomeAt(quartX, quartY, quartZ int32) string {
	blockX, blockZ := quartX*4, quartZ*4
	chunkX, chunkZ := blockX>>4, blockZ>>4
	if int64(chunkX)*int64(chunkX)+int64(chunkZ)*int64(chunkZ) <= 4096 {
		return "minecraft:the_end"
	}

	// TheEndBiomeSource samples erosion at the centre of the containing
	// chunk. For NoiseGeneratorSettings.END, erosion is END_ISLANDS, whose
	// value is (getHeightValue - 8) / 128.
	weirdBlockX := int((chunkX*2 + 1) * 8)
	weirdBlockZ := int((chunkZ*2 + 1) * 8)
	erosion := (g.endIslandHeight(weirdBlockX, weirdBlockZ) - 8) / 128
	switch {
	case erosion > 0.25:
		return "minecraft:end_highlands"
	case erosion >= -0.0625:
		return "minecraft:end_midlands"
	case erosion < -0.21875:
		return "minecraft:small_end_islands"
	default:
		return "minecraft:end_barrens"
	}
}

func (g *End) populate(chunkX, chunkZ int, region *endFeatureRegion, result *world.GenerationResult) {
	for originZ := chunkZ - endReplayRadius; originZ <= chunkZ+endReplayRadius; originZ++ {
		for originX := chunkX - endReplayRadius; originX <= chunkX+endReplayRadius; originX++ {
			origin := endFeatureOrigin{region: region, chunkX: originX, chunkZ: originZ}
			g.populateOrigin(chunkX, chunkZ, origin, result)
		}
	}
}

func (g *End) generateEndGatewayStructure(origin endFeatureOrigin, worldX, worldY, worldZ int, result *world.GenerationResult, targetX, targetZ int) {
	for wx := worldX - 1; wx <= worldX+1; wx++ {
		for wy := worldY - 2; wy <= worldY+2; wy++ {
			for wz := worldZ - 1; wz <= worldZ+1; wz++ {
				flagX, flagY, flagZ := wx == worldX, wy == worldY, wz == worldZ
				flagY2 := abs(wy-worldY) == 2
				switch {
				case flagX && flagY && flagZ:
					origin.setBlock(wx, wy, wz, g.endGatewayRID)
				case flagY:
					origin.setBlock(wx, wy, wz, g.airRID)
				case flagY2 && flagX && flagZ:
					origin.setBlock(wx, wy, wz, g.bedrockRID)
				case (flagX || flagZ) && !flagY2:
					origin.setBlock(wx, wy, wz, g.bedrockRID)
				default:
					origin.setBlock(wx, wy, wz, g.airRID)
				}
			}
		}
	}
	if origin.isTarget(targetX, targetZ) && worldY >= origin.region.minY && worldY <= origin.region.maxY {
		result.BlockEntities = append(result.BlockEntities, chunk.BlockEntity{
			Pos: cube.Pos{worldX, worldY, worldZ},
			Data: map[string]any{
				"id":            "EndGateway",
				"Age":           int64(0),
				"ExitPortal":    map[string]any{"X": int32(100), "Y": int32(50), "Z": int32(0)},
				"ExactTeleport": uint8(1),
			},
		})
	}
}

// generateEndPlatform places end_platform's fixed (100,49,0) origin in the
// central End biome's top-layer stage. Its foundation is one block below it.
func (g *End) generateEndPlatform(origin endFeatureOrigin) {
	if origin.chunkX != 6 || origin.chunkZ != 0 {
		return
	}
	for z := -2; z <= 2; z++ {
		for x := 98; x <= 102; x++ {
			for y := 48; y <= 51; y++ {
				rid := g.airRID
				if y == 48 {
					rid = g.obsidianRID
				}
				if origin.block(x, y, z) != rid {
					origin.setBlock(x, y, z, rid)
				}
			}
		}
	}
}

func (g *End) generateEndIsland(origin endFeatureOrigin, centreX, centreY, centreZ int, r *endRand) {
	minY, maxY := origin.region.minY, origin.region.maxY
	f := float64(r.Intn(3)) + 4
	for i := 0; f > 0.5; i-- {
		y := centreY + i
		if y < minY || y > maxY {
			f -= float64(r.Intn(2)) + 0.5
			continue
		}
		start, end := int(math.Floor(-f)), int(math.Ceil(f))
		rad := (f + 1.0) * (f + 1.0)
		for dx := start; dx <= end; dx++ {
			for dz := start; dz <= end; dz++ {
				if float64(dx*dx+dz*dz) <= rad {
					origin.setBlock(centreX+dx, y, centreZ+dz, g.endStoneRID)
				}
			}
		}
		f -= float64(r.Intn(2)) + 0.5
	}
}

func (g *End) generateChorusPlant(region endFeatureOrigin, x, y, z int, r *endRand, maxDistance int) {
	g.setChorusPlant(region, x, y, z)
	g.growChorusRecursive(region, x, y, z, r, x, z, maxDistance, 0)
}

// setChorusPlant matches getStateWithConnections at the instant of the write.
// Growth explicitly refreshes the preceding segment after adding its neighbor.
func (g *End) setChorusPlant(region endFeatureOrigin, x, y, z int) {
	if y < region.region.minY || y > region.region.maxY || !region.inWriteRadius(x, z) {
		return
	}
	mask := 0
	for side, offset := range [...]cube.Pos{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {1, 0, 0}, {0, 0, 1}, {-1, 0, 0}} {
		rid := region.block(x+offset[0], y+offset[1], z+offset[2])
		connected := rid == g.chorusPlantStates[0].Block
		if !connected {
			for _, flower := range g.chorusFlowerRIDs {
				if rid == flower {
					connected = true
					break
				}
			}
		}
		// Java 26.3's supports_chorus_plant tag contains only end_stone.
		if connected || side == 0 && rid == g.endStoneRID {
			mask |= 1 << side
		}
	}
	region.region.setLayers(x, y, z, g.chorusPlantStates[mask])
}

func (g *End) growChorusRecursive(region endFeatureOrigin, x, y, z int, r *endRand, originX, originZ, maxDistance, depth int) {
	i := int(r.Intn(4) + 1)
	if depth == 0 {
		i++
	}
	for j := 0; j < i; j++ {
		yy := y + j + 1
		if !g.areAllHorizontalNeighborsAir(region, x, yy, z, -1) {
			return
		}
		g.setChorusPlant(region, x, yy, z)
		g.setChorusPlant(region, x, yy-1, z)
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
			if !g.isAir(region, nx, ny, nz) || !g.isAir(region, nx, ny-1, nz) {
				continue
			}
			if !g.areAllHorizontalNeighborsAir(region, nx, ny, nz, opposite) {
				continue
			}
			branched = true
			g.setChorusPlant(region, nx, ny, nz)
			g.setChorusPlant(region, x, ny, z)
			g.growChorusRecursive(region, nx, ny, nz, r, originX, originZ, maxDistance, depth+1)
		}
	}
	if !branched {
		region.setBlock(x, y+i, z, g.chorusFlowerRIDs[5])
	}
}

func (g *End) isAir(region endFeatureOrigin, x, y, z int) bool {
	return region.block(x, y, z) == g.airRID
}

func (g *End) areAllHorizontalNeighborsAir(region endFeatureOrigin, x, y, z, excluding int) bool {
	dirs := [4][2]int{{0, -1}, {1, 0}, {0, 1}, {-1, 0}}
	for i, d := range dirs {
		if i != excluding && !g.isAir(region, x+d[0], y, z+d[1]) {
			return false
		}
	}
	return true
}

func randomHorizontal(r *endRand) (dx, dz, opposite int) {
	switch r.Intn(4) {
	case 0:
		return 0, -1, 2 // North, opposite South.
	case 1:
		return 1, 0, 3 // East, opposite West.
	case 2:
		return 0, 1, 0 // South, opposite North.
	default:
		return -1, 0, 1 // West, opposite East.
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
