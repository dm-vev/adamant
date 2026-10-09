package nether

import (
	"fmt"
	"sync"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/javastructure"
)

// netherFeatureSource exposes the immutable base-terrain and biome queries used
// by Java structure processors and placed-feature modifiers.
type netherFeatureSource struct {
	generator    *Nether
	mu           sync.Mutex
	chunks       map[world.ChunkPos]*chunk.Chunk
	order        []world.ChunkPos
	terrain      map[world.ChunkPos]*chunk.Chunk
	terrainOrder []world.ChunkPos
}

func newNetherFeatureSource(g *Nether) *netherFeatureSource {
	return &netherFeatureSource{generator: g, chunks: make(map[world.ChunkPos]*chunk.Chunk), terrain: make(map[world.ChunkPos]*chunk.Chunk)}
}

func (s *netherFeatureSource) UseLegacyRandomSource() bool { return false }

func (*netherFeatureSource) SeaLevel() int32 { return netherSeaLevel }

func (s *netherFeatureSource) JavaBiomeAt(x, y, z int32) string {
	quartX, quartY, quartZ := java26.FiddledBiomeQuart(s.generator.biomeZoomSeed, x, y, z)
	return s.JavaNoiseBiomeAt(quartX, quartY, quartZ)
}

func (s *netherFeatureSource) JavaNoiseBiomeAt(quartX, quartY, quartZ int32) string {
	_ = quartY // Nether biome climate is two-dimensional.
	return s.generator.biomes.Biome(quartX*4, quartZ*4)
}

func (*netherFeatureSource) JavaFeatureBiomeNames() []string {
	return []string{
		"minecraft:nether_wastes",
		"minecraft:soul_sand_valley",
		"minecraft:crimson_forest",
		"minecraft:warped_forest",
		"minecraft:basalt_deltas",
	}
}

func (s *netherFeatureSource) BaseBlock(x, y, z int32) world.Block {
	if y < 0 || y > 127 {
		b, ok := world.BlockByName("minecraft:air", nil)
		if !ok {
			panic("nether: air block is not registered")
		}
		return b
	}
	rid := s.BaseState(x, y, z).Block
	b, ok := world.BlockByRuntimeID(rid)
	if !ok {
		panic("nether: Java feature source encountered unknown block runtime ID")
	}
	return b
}

func (s *netherFeatureSource) BaseState(x, y, z int32) world.NativeState {
	if y < 0 || y > 127 {
		air := world.BlockRuntimeID(block.Air{})
		return world.NativeState{Block: air, Liquid: air}
	}
	c := s.baseChunk(world.ChunkPos{x >> 4, z >> 4})
	return world.NativeStateAt(c, uint8(x&15), int16(y), uint8(z&15))
}

func (s *netherFeatureSource) BaseHeight(x, z int32, oceanFloor bool) int32 {
	_ = oceanFloor // Nether base terrain has no water; both vanilla heightmaps select solid blocks.
	c := s.terrainChunk(world.ChunkPos{x >> 4, z >> 4})
	for y := int32(c.Range().Max()); y >= int32(c.Range().Min()); y-- {
		rid := c.Block(uint8(x&15), int16(y), uint8(z&15), 0)
		if rid != c.BlockRegistry().AirRuntimeID() && !c.BlockRegistry().LiquidBlock(rid) {
			return y + 1
		}
	}
	return int32(c.Range().Min())
}

func (s *netherFeatureSource) remember(pos world.ChunkPos, c *chunk.Chunk) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.chunks[pos]; !exists {
		s.order = append(s.order, pos)
	}
	s.chunks[pos] = c.Clone()
	for len(s.order) > 16 {
		old := s.order[0]
		s.order = s.order[1:]
		delete(s.chunks, old)
	}
}

func (s *netherFeatureSource) baseChunk(pos world.ChunkPos) *chunk.Chunk {
	s.mu.Lock()
	cached := s.chunks[pos]
	s.mu.Unlock()
	if cached != nil {
		return cached
	}
	return s.terrainChunk(pos)
}

func (s *netherFeatureSource) PlaceStructureStep(pos world.ChunkPos, step int, env javastructure.ProcessorEnvironment, emit func(javastructure.Placement)) {
	switch step {
	case 4:
		env.Random = s.structureStepRandom(pos, 26, step)
		s.placeRuinedPortalStep(pos, env, emit)
		env.Random = s.structureStepRandom(pos, 31, step)
		s.placeBastionStructureStep(pos, env, emit)
	case 7:
		env.Random = s.structureStepRandom(pos, 1, step)
		s.placeFortressStructureStep(pos, env, emit)
		env.Random = s.structureStepRandom(pos, 2, step)
		s.placeNetherFossilStep(pos, env, emit)
	}
}

func (s *netherFeatureSource) structureStepRandom(pos world.ChunkPos, index, step int) *java26.Random {
	originX, originZ := int32(pos[0])*16, int32(pos[1])*16
	random := java26.NewXoroshiroRandom(s.generator.seed)
	decorationSeed := random.SetDecorationSeed(s.generator.seed, originX, originZ)
	random.SetFeatureSeed(decorationSeed, int32(index), int32(step))
	return &random
}

func (s *netherFeatureSource) placeBastionStructureStep(pos world.ChunkPos, env javastructure.ProcessorEnvironment, emit func(javastructure.Placement)) {
	const salt, spacing, separation = int32(30084232), int32(27), int32(4)
	targetX, targetZ := int32(pos[0]), int32(pos[1])
	clip := javastructure.Bounds{
		Min: [3]int32{targetX * 16, 0, targetZ * 16},
		Max: [3]int32{targetX*16 + 15, 127, targetZ*16 + 15},
	}
	if env.Random == nil {
		panic("nether: Java structure placement has no step random source")
	}
	for sourceZ := targetZ - 6; sourceZ <= targetZ+6; sourceZ++ {
		for sourceX := targetX - 6; sourceX <= targetX+6; sourceX++ {
			candidate, err := javastructure.PotentialChunk(s.generator.seed, sourceX, sourceZ, salt, spacing, separation, javastructure.Linear)
			if err != nil {
				panic(err)
			}
			if candidate[0] != sourceX || candidate[1] != sourceZ {
				continue
			}
			startX, startZ := sourceX*16, sourceZ*16
			if env.Random.NextInt(5) < 2 {
				continue
			}
			if env.JavaBiomeAt(startX, 33, startZ) == "minecraft:basalt_deltas" {
				continue
			}
			jigsaw, err := javastructure.BuildJigsaw(javastructure.JigsawOptions{
				Pool:          "bastion/starts",
				Origin:        [3]int32{startX, 33, startZ},
				MaxDepth:      6,
				MaxDistance:   80,
				ExpansionHack: false,
				Seed:          s.generator.seed,
				Terrain:       javastructure.TerrainNone,
				HeightAt:      func(x, z int32) int32 { return s.BaseHeight(x, z, false) },
			}, env.Random)
			if err != nil {
				panic(fmt.Errorf("build Java bastion at %d,%d: %w", sourceX, sourceZ, err))
			}
			pieceEnv := env
			pieceEnv.WorldSeed = s.generator.seed
			pieceEnv.StartPos = [3]int32{startX, 33, startZ}
			for _, piece := range jigsaw.Pieces {
				placement, err := piece.PlaceWithEnvironment(0, clip, s.generator.seed, pieceEnv)
				if err != nil {
					panic(fmt.Errorf("place Java bastion at %d,%d: %w", sourceX, sourceZ, err))
				}
				placement, err = placement.AdaptItemFrameEntities()
				if err != nil {
					panic(fmt.Errorf("adapt Java bastion at %d,%d: %w", sourceX, sourceZ, err))
				}
				emit(placement)
			}
		}
	}
}

func (s *netherFeatureSource) placeFortressStructureStep(pos world.ChunkPos, env javastructure.ProcessorEnvironment, emit func(javastructure.Placement)) {
	if env.Random == nil {
		panic("nether: Java structure placement has no step random source")
	}
	s.generator.bridgePayloadMu.Lock()
	defer s.generator.bridgePayloadMu.Unlock()
	base := s.terrainChunk(pos)
	placed := base.Clone()
	replay := s.generator.applyNetherBridge(int(pos[0]), int(pos[1]), placed, env.Random, true)
	blockEntities := make(map[cube.Pos]map[string]any)
	for _, payload := range s.generator.takeBlockEntityPayloads(pos) {
		blockEntities[payload.Pos] = payload.Data
	}
	placement := javastructure.Placement{
		PostProcessing: replay.PostProcessing,
		ScheduledTicks: replay.ScheduledTicks,
	}
	for x := uint8(0); x < 16; x++ {
		for z := uint8(0); z < 16; z++ {
			for y := int16(0); y < 128; y++ {
				before := world.NativeStateAt(base, x, y, z)
				after := world.NativeStateAt(placed, x, y, z)
				if before == after {
					continue
				}
				state, err := world.JavaState(placed.BlockRegistry(), after)
				if err != nil {
					panic(err)
				}
				worldX, worldZ := int32(pos[0])*16+int32(x), int32(pos[1])*16+int32(z)
				block := javastructure.Block{Pos: [3]int32{worldX, int32(y), worldZ}, State: state}
				if data := blockEntities[cube.Pos{int(worldX), int(y), int(worldZ)}]; data != nil {
					block.NBT = data
				}
				placement.Blocks = append(placement.Blocks, block)
			}
		}
	}
	if len(placement.Blocks) != 0 || len(placement.PostProcessing) != 0 || len(placement.ScheduledTicks) != 0 {
		emit(placement)
	}
}

func (s *netherFeatureSource) placeNetherFossilStep(pos world.ChunkPos, env javastructure.ProcessorEnvironment, emit func(javastructure.Placement)) {
	const salt, spacing, separation = int32(14357921), int32(2), int32(1)
	targetX, targetZ := int32(pos[0]), int32(pos[1])
	clip := javastructure.Bounds{
		Min: [3]int32{targetX * 16, 0, targetZ * 16},
		Max: [3]int32{targetX*16 + 15, 127, targetZ*16 + 15},
	}
	for sourceZ := targetZ - 3; sourceZ <= targetZ+3; sourceZ++ {
		for sourceX := targetX - 3; sourceX <= targetX+3; sourceX++ {
			candidate, err := javastructure.PotentialChunk(s.generator.seed, sourceX, sourceZ, salt, spacing, separation, javastructure.Linear)
			if err != nil {
				panic(err)
			}
			if candidate[0] != sourceX || candidate[1] != sourceZ {
				continue
			}
			if env.Random == nil {
				panic("nether: Java fossil placement has no step random source")
			}
			x := sourceX*16 + env.Random.NextInt(16)
			z := sourceZ*16 + env.Random.NextInt(16)
			y := int32(32) + env.Random.NextInt(94)
			for y > netherSeaLevel {
				currentName, _ := env.BaseBlock(x, y, z).EncodeBlock()
				y--
				belowName, _ := env.BaseBlock(x, y, z).EncodeBlock()
				sturdy := belowName != "minecraft:air" && belowName != "minecraft:lava" && belowName != "minecraft:flowing_lava"
				if currentName == "minecraft:air" && (belowName == "minecraft:soul_sand" || sturdy) {
					break
				}
			}
			if y <= netherSeaLevel {
				continue
			}
			if env.JavaBiomeAt(x, y, z) != "minecraft:soul_sand_valley" {
				continue
			}
			rotation := javastructure.Rotation(env.Random.NextInt(4))
			templateNumber := env.Random.NextInt(14) + 1
			templateName := fmt.Sprintf("nether_fossils/fossil_%d", templateNumber)
			template := loadNetherTemplate(templateName)
			origin := [3]int32{x, y, z}
			placement, err := template.Place(0, origin, rotation, javastructure.NoMirror, clip, s.generator.seed)
			if err != nil {
				panic(fmt.Errorf("place Java Nether fossil %s: %w", templateName, err))
			}
			filtered := javastructure.Placement{
				Entities: placement.Entities, PostProcessing: placement.PostProcessing,
				ScheduledTicks: placement.ScheduledTicks, StructurePieceWrites: placement.StructurePieceWrites,
			}
			for _, pieceBlock := range placement.Blocks {
				switch pieceBlock.State.Name {
				case "minecraft:air", "minecraft:structure_block", "minecraft:structure_void":
					continue
				}
				javastructure.SeedRandomizableContainerLoot(&pieceBlock, env.Random)
				filtered.Blocks = append(filtered.Blocks, pieceBlock)
			}
			if len(filtered.Blocks) != 0 || len(filtered.Entities) != 0 {
				filtered, err = filtered.AdaptItemFrameEntities()
				if err != nil {
					panic(fmt.Errorf("adapt Java Nether fossil: %w", err))
				}
				emit(filtered)
			}
			min, max := netherTemplateBounds(template, origin, rotation)
			random := java26.NewLegacyRandom(s.generator.seed)
			base := random.ForkPositional().At((min[0]+max[0])/2, (min[1]+max[1])/2, (min[2]+max[2])/2)
			if base.NextFloat() >= 0.5 {
				continue
			}
			ghastPos := [3]int32{
				min[0] + base.NextInt(max32(1, max[0]-min[0]+1)),
				min[1],
				min[2] + base.NextInt(max32(1, max[2]-min[2]+1)),
			}
			if ghastPos[0]>>4 != targetX || ghastPos[2]>>4 != targetZ || ghastPos[1] < clip.Min[1] || ghastPos[1] > clip.Max[1] {
				continue
			}
			name, _ := env.BaseBlock(ghastPos[0], ghastPos[1], ghastPos[2]).EncodeBlock()
			if name != "minecraft:air" {
				continue
			}
			facing := []string{"north", "east", "south", "west"}[base.NextInt(4)]
			emit(javastructure.Placement{Blocks: []javastructure.Block{{
				Pos: ghastPos,
				State: world.BlockState{Name: "minecraft:dried_ghast", Properties: map[string]any{
					"minecraft:cardinal_direction": facing,
					"rehydration_level":            int32(0),
				}},
			}}})
		}
	}
}

var netherTemplateCache sync.Map

func loadNetherTemplate(name string) *javastructure.Template {
	if cached, ok := netherTemplateCache.Load(name); ok {
		return cached.(*javastructure.Template)
	}
	template, err := javastructure.Load(name)
	if err != nil {
		panic(err)
	}
	actual, _ := netherTemplateCache.LoadOrStore(name, template)
	return actual.(*javastructure.Template)
}

func netherTemplateBounds(template *javastructure.Template, origin [3]int32, rotation javastructure.Rotation) ([3]int32, [3]int32) {
	var min, max [3]int32
	for axis := range 3 {
		min[axis], max[axis] = 1<<30, -1<<30
	}
	for x := int32(0); x < 2; x++ {
		for y := int32(0); y < 2; y++ {
			for z := int32(0); z < 2; z++ {
				corner := [3]int32{x * (template.Size[0] - 1), y * (template.Size[1] - 1), z * (template.Size[2] - 1)}
				worldPos := javastructure.TransformPosition(corner, javastructure.NoMirror, rotation)
				for axis := range 3 {
					worldPos[axis] += origin[axis]
					min[axis] = min32(min[axis], worldPos[axis])
					max[axis] = max32(max[axis], worldPos[axis])
				}
			}
		}
	}
	return min, max
}

func min32(a, b int32) int32 {
	if a < b {
		return a
	}
	return b
}

func max32(a, b int32) int32 {
	if a > b {
		return a
	}
	return b
}

func (s *netherFeatureSource) terrainChunk(pos world.ChunkPos) *chunk.Chunk {
	s.mu.Lock()
	cached := s.terrain[pos]
	s.mu.Unlock()
	if cached != nil {
		return cached
	}
	g := s.generator
	c := chunk.New(world.DefaultBlockRegistry, cube.Range{0, 127})
	scratch := g.pool.Get().(*netherScratch)
	g.prepareHeights(int(pos[0]), int(pos[1]), c, scratch)
	g.pool.Put(scratch)
	g.applyJavaNetherMaterial(int(pos[0]), int(pos[1]), c)
	g.carveCaves(int(pos[0]), int(pos[1]), c)
	s.mu.Lock()
	if cached = s.terrain[pos]; cached == nil {
		s.terrain[pos] = c.Clone()
		s.terrainOrder = append(s.terrainOrder, pos)
		for len(s.terrainOrder) > 16 {
			old := s.terrainOrder[0]
			s.terrainOrder = s.terrainOrder[1:]
			delete(s.terrain, old)
		}
		cached = s.terrain[pos]
	}
	s.mu.Unlock()
	return cached
}
