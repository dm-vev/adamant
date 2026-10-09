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

func (*netherFeatureSource) JavaHasSkyLight() bool { return false }

func (*netherFeatureSource) JavaFluidTickDelay(name string) int32 {
	switch name {
	case "minecraft:water":
		return 5
	case "minecraft:lava":
		return 10
	default:
		panic("nether: unsupported generation fluid tick " + name)
	}
}

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
		env.Random = s.structureStepRandom(pos, 18, step)
		s.placeBastionStructureStep(pos, env, emit)
		env.Random = s.structureStepRandom(pos, 32, step)
		s.placeRuinedPortalStep(pos, env, emit)
	case 7:
		env.Random = s.structureStepRandom(pos, 1, step)
		s.generator.placeFortressStructureStep(pos, env, emit)
		env.Random = s.structureStepRandom(pos, 2, step)
		s.placeNetherFossilStep(pos, env, emit)
	}
}

func (s *netherFeatureSource) structureStepRandom(pos world.ChunkPos, index, step int) *java26.Random {
	originX, originZ := int32(pos[0])*16, int32(pos[1])*16
	random := java26.NewWorldgenRandom(s.generator.seed)
	decorationSeed := random.SetDecorationSeed(s.generator.seed, originX, originZ)
	random.SetFeatureSeed(decorationSeed, int32(index), int32(step))
	return &random
}

func (s *netherFeatureSource) placeBastionStructureStep(pos world.ChunkPos, env javastructure.ProcessorEnvironment, emit func(javastructure.Placement)) {
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
			start, err := s.generator.complexStart(sourceX, sourceZ)
			if err != nil {
				panic(err)
			}
			if start == nil || start.name != "minecraft:bastion_remnant" {
				continue
			}
			pieceEnv := env
			pieceEnv.WorldSeed = s.generator.seed
			pieceEnv.StartPos = start.startPos
			for i := range start.bastion.Pieces {
				piece := &start.bastion.Pieces[i]
				bounds := &piece.Bounds
				if bounds.Max[0] < clip.Min[0] || bounds.Min[0] > clip.Max[0] ||
					bounds.Max[1] < clip.Min[1] || bounds.Min[1] > clip.Max[1] ||
					bounds.Max[2] < clip.Min[2] || bounds.Min[2] > clip.Max[2] {
					continue
				}
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
	g.prepareTerrain(pos, c)
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
