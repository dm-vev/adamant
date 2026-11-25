package end

import (
	"math"
	"math/bits"
	"sync"
	"sync/atomic"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
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

	baseNoise   *vanilla.BlendedNoise
	islandNoise *vanilla.SimplexNoise

	endStoneRID uint32
	airRID      uint32
	biomeID     uint32

	bedrockRID    uint32
	obsidianRID   uint32
	ironBarsRIDs  [16]uint32
	fireRID       uint32
	endGatewayRID uint32

	chorusMaxDistance int

	spikes       [10]vanilla.EndSpike
	endCityCache sync.Map

	pool  sync.Pool
	hooks atomic.Pointer[Hooks]
}

type endStateKey struct{}

type endRuntimeState struct {
	endStoneStateID        uint32
	airStateID             uint32
	biomes2D               [256]uint32
	endStoneSupportsChorus bool
	endCityCache           endCityStartCache
	terrainCache           endTerrainCache
	islandTiles            endIslandTileCache
}

type endScratch struct {
	heights   []float32
	region    *endFeatureRegion
	positions []cube.Pos
}

func newEndRuntimeState(airRID, endStoneRID, biomeID uint32) *endRuntimeState {
	supportsChorus, err := gamedata.BlockTagContains("minecraft:supports_chorus_plant", "minecraft:end_stone")
	if err != nil {
		panic(err)
	}
	st := &endRuntimeState{
		endStoneStateID:        world.NativeState{Block: endStoneRID, Liquid: airRID}.CanonicalID(world.DefaultBlockRegistry),
		airStateID:             world.NativeState{Block: airRID, Liquid: airRID}.CanonicalID(world.DefaultBlockRegistry),
		endStoneSupportsChorus: supportsChorus,
	}
	for i := range st.biomes2D {
		st.biomes2D[i] = biomeID
	}
	return st
}

func (g *End) state() *endRuntimeState {
	if v, ok := g.endCityCache.Load(endStateKey{}); ok {
		return v.(*endRuntimeState)
	}
	st := newEndRuntimeState(g.airRID, g.endStoneRID, g.biomeID)
	actual, _ := g.endCityCache.LoadOrStore(endStateKey{}, st)
	return actual.(*endRuntimeState)
}

// New creates an End generator using the seed provided.
func New(seed int64) *End {
	return NewEnd(seed)
}

// NewEnd creates an End generator using the seed provided.
func NewEnd(seed int64) *End {
	terrainRandom := vanilla.NewLegacyRandom(seed)
	islandRandom := vanilla.NewLegacyRandom(seed)
	islandRandom.Consume(17292)

	biomeID := uint32(dfbiome.End{}.EncodeBiome())
	airRID := world.BlockRuntimeID(block.Air{})
	endStoneRID := world.BlockRuntimeID(block.EndStone{})
	g := &End{
		seed:              seed,
		biomeZoomSeed:     vanilla.BiomeZoomSeed(seed),
		spikes:            vanilla.EndSpikes(seed),
		baseNoise:         vanilla.NewBlendedNoise(&terrainRandom, 0.25, 0.25, 80, 160, 4),
		islandNoise:       vanilla.NewSimplexNoiseWithoutOffset(&islandRandom),
		endStoneRID:       endStoneRID,
		airRID:            airRID,
		biomeID:           biomeID,
		bedrockRID:        world.BlockRuntimeID(block.Bedrock{}),
		obsidianRID:       world.BlockRuntimeID(block.Obsidian{}),
		ironBarsRIDs:      [16]uint32{},
		fireRID:           world.BlockRuntimeID(block.Fire{Type: block.NormalFire()}),
		endGatewayRID:     world.BlockRuntimeID(block.EndGateway{}),
		chorusMaxDistance: 8,
	}
	g.endCityCache.Store(endStateKey{}, newEndRuntimeState(airRID, endStoneRID, biomeID))
	g.pool.New = func() any {
		return &endScratch{
			heights: make([]float32, 3*33*3),
			region:  &endFeatureRegion{},
		}
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

	hooks := g.hooks.Load()
	chunkX, chunkZ := int(pos[0]), int(pos[1])
	region := newEndFeatureRegion(g, chunkX, chunkZ, c, s)
	if hooks != nil {
		applyPreDecorationHooks(pos, c, hooks)
	}
	var result world.GenerationResult
	g.populate(chunkX, chunkZ, region, &result)
	region.publishTarget(chunkX, chunkZ, c)
	region.resetForPool()
	if hooks != nil && hooks.OnDecorate != nil {
		rng := decorationHookRandom(g.seed, pos)
		hooks.OnDecorate(pos, c, &rng)
	}
	return result
}

// DefaultSpawn returns the vanilla End arrival position.
func (*End) DefaultSpawn(world.Dimension) cube.Pos {
	return cube.Pos{100, 50, 0}
}

func (g *End) terrainChunk(chunkX, chunkZ int, s *endScratch) *endTerrainChunk {
	st := g.state()
	pos := world.ChunkPos{int32(chunkX), int32(chunkZ)}
	if cached := st.terrainCache.get(pos); cached != nil {
		return cached
	}
	t := g.buildTerrainChunk(chunkX, chunkZ, s)
	return st.terrainCache.put(pos, t)
}

func (g *End) buildTerrainChunk(chunkX, chunkZ int, s *endScratch) *endTerrainChunk {
	heights := g.getHeights(s, chunkX*2, 0, chunkZ*2, 3, 33, 3)
	t := &endTerrainChunk{
		sourceBiome: endGenerationBiome(g.noiseBiomeAt(int32(chunkX)*4, 0, int32(chunkZ)*4)),
	}
	for i1 := 0; i1 < 2; i1++ {
		for j1 := 0; j1 < 2; j1++ {
			for k1 := 0; k1 < 32; k1++ {
				idx00 := ((i1+0)*3+j1+0)*33 + k1
				idx01 := ((i1+0)*3+j1+1)*33 + k1
				idx10 := ((i1+1)*3+j1+0)*33 + k1
				idx11 := ((i1+1)*3+j1+1)*33 + k1
				d1, u1 := heights[idx00], heights[idx00+1]
				d2, u2 := heights[idx01], heights[idx01+1]
				d3, u3 := heights[idx10], heights[idx10+1]
				d4, u4 := heights[idx11], heights[idx11+1]
				if d1 < -1e-4 && d2 < -1e-4 && d3 < -1e-4 && d4 < -1e-4 &&
					u1 < -1e-4 && u2 < -1e-4 && u3 < -1e-4 && u4 < -1e-4 {
					continue
				}

				d5 := (u1 - d1) * 0.25
				d6 := (u2 - d2) * 0.25
				d7 := (u3 - d3) * 0.25
				d8 := (u4 - d4) * 0.25

				subBit := uint8(1) << uint(k1>>2)
				for l1 := 0; l1 < 4; l1++ {
					y := l1 + k1*4
					wordIdx := y >> 6
					yBit := uint64(1) << (uint(y) & 63)

					d10 := d1
					d11 := d2
					d12 := (d3 - d1) * 0.125
					d13 := (d4 - d2) * 0.125

					for i2 := 0; i2 < 8; i2++ {
						d15 := d10
						d16 := (d11 - d10) * 0.125
						colBase := ((i2 + i1*8) << 4) | (j1 * 8)

						for j2 := 0; j2 < 8; j2++ {
							if d15 > 0 && squeeze32(d15) > 0 {
								t.bits[(colBase+j2)<<1|wordIdx] |= yBit
								t.nonEmptySubMask |= subBit
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
	return t
}

func (g *End) generateTerrain(chunkX, chunkZ int, c *chunk.Chunk, s *endScratch) {
	g.terrainChunk(chunkX, chunkZ, s).writeToChunk(c, g.endStoneRID)
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

func (g *End) generateSpike(origin endFeatureOrigin, targetX, targetZ int, spike *vanilla.EndSpike, result *world.GenerationResult, random *endRand) {
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
				if material == vanilla.EndSpikeUnchanged {
					continue
				}
				rid := g.ironBarsRIDs[connections]
				if material != vanilla.EndSpikeIronBars {
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
	c.FillBiomes2D(g.state().biomes2D[:])
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

func (g *End) islandTile(tx, tz int32) endIslandTile {
	tiles := &g.state().islandTiles
	key := [2]int32{tx, tz}
	if tile, ok := tiles.get(key); ok {
		return tile
	}
	var tile endIslandTile
	baseX, baseZ := int64(tx)<<4, int64(tz)<<4
	threshold := float64(float32(-0.9))
	for lz := int64(0); lz < 16; lz++ {
		iz := baseZ + lz
		izSq := iz * iz
		var row uint16
		for lx := int64(0); lx < 16; lx++ {
			ix := baseX + lx
			if ix*ix+izSq > 4096 && g.islandNoise.Sample2D(float64(ix), float64(iz)) < threshold {
				row |= 1 << uint(lx)
			}
		}
		tile[lz] = row
	}
	tiles.put(key, tile)
	return tile
}

func (g *End) endIslandHeight(blockX, blockZ int) float32 {
	sectionX, sectionZ := blockX/8, blockZ/8
	chunkX, chunkZ := sectionX/2, sectionZ/2
	subX, subZ := sectionX%2, sectionZ%2

	dx, dz := float32(blockX), float32(blockZ)
	mainIsland := clamp32(100-float32(math.Sqrt(float64(dx*dx+dz*dz))), -100, 80)
	maxAbsX, maxAbsZ := int64(abs(chunkX)+12), int64(abs(chunkZ)+12)
	if maxAbsX*maxAbsX+maxAbsZ*maxAbsZ <= 4096 {
		return mainIsland
	}

	outerIslands := float32(-100)
	minIX, maxIX := chunkX-12, chunkX+12
	minIZ, maxIZ := chunkZ-12, chunkZ+12
	minTX, maxTX := minIX>>4, maxIX>>4
	minTZ, maxTZ := minIZ>>4, maxIZ>>4
	for tz := minTZ; tz <= maxTZ; tz++ {
		baseZ := tz << 4
		z0, z1 := max(0, minIZ-baseZ), min(15, maxIZ-baseZ)
		for tx := minTX; tx <= maxTX; tx++ {
			baseX := tx << 4
			x0, x1 := max(0, minIX-baseX), min(15, maxIX-baseX)
			xMask := uint16(((uint32(1) << uint(x1-x0+1)) - 1) << uint(x0))
			tile := g.islandTile(int32(tx), int32(tz))
			for lz := z0; lz <= z1; lz++ {
				row := tile[lz] & xMask
				if row == 0 {
					continue
				}
				islandZ := int64(baseZ + lz)
				islandOffsetZ := (baseZ + lz) - chunkZ
				offsetZ := float32(subZ - islandOffsetZ*2)
				offsetZSq := offsetZ * offsetZ
				zSizeTerm := float32(abs64(islandZ)) * 147
				for row != 0 {
					lx := bits.TrailingZeros16(row)
					row &= row - 1
					islandX := int64(baseX + lx)
					islandOffsetX := (baseX + lx) - chunkX
					sizeValue := float32(abs64(islandX))*3439 + zSizeTerm
					size := float32(math.Mod(float64(sizeValue), 13)) + 9
					offsetX := float32(subX - islandOffsetX*2)
					candidate := clamp32(100-float32(math.Sqrt(float64(offsetX*offsetX+offsetZSq)))*size, -100, 80)
					if candidate > outerIslands {
						outerIslands = candidate
					}
				}
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

// noiseBiomeAt resolves TheEndBiomeSource at unzoomed Java quart
// coordinates. It intentionally returns Java's source biome key rather than
// the native Bedrock biome ID, whose palette may contain only minecraft:the_end.
func (g *End) noiseBiomeAt(quartX, quartY, quartZ int32) string {
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

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
