package overworld

import (
	"slices"
	"sync"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/biome"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/structure"
)

func init() { world.RegisterRuntimeFeatures(&runtimeFeatures{}) }

// runtimeFeatures places the Java features of the embedded registry into a
// live world, as ConfiguredFeature.place and PlacedFeature.place do when a
// plant grows. Features read and write the world through a recording region
// whose source is the world itself; what they wrote is applied to the world
// once they placed.
type runtimeFeatures struct {
	once     sync.Once
	features *ModernFeatures
}

// boneMealFeatureTag holds the configured features of
// #minecraft:can_spawn_from_bone_meal.
var boneMealFeatureTag = map[string]bool{
	"flower_default": true, "flower_flower_forest": true, "flower_swamp": true, "flower_plain": true,
	"flower_meadow": true, "flower_cherry": true, "wildflower": true, "flower_pale_garden": true,
}

func (r *runtimeFeatures) base() *ModernFeatures {
	r.once.Do(func() { r.features = NewModernFeatures(0, liveFeatureSource{}) })
	return r.features
}

// PlaceConfigured ...
func (r *runtimeFeatures) PlaceConfigured(tx *world.Tx, name string, pos cube.Pos, random *vanilla.Random) bool {
	return r.place(tx, pos, func(f *ModernFeatures, region *featureRegion, origin featurePos) bool {
		return f.placeConfiguredRegion(region, name, origin, random)
	})
}

// PlacePlaced ...
func (r *runtimeFeatures) PlacePlaced(tx *world.Tx, name string, pos cube.Pos, random *vanilla.Random) bool {
	return r.place(tx, pos, func(_ *ModernFeatures, region *featureRegion, origin featurePos) bool {
		return region.PlacePlacedFeature(name, [3]int32{origin.x, origin.y, origin.z}, random)
	})
}

// FeatureConfig ...
func (r *runtimeFeatures) FeatureConfig(name string) (string, map[string]any, bool) {
	config, ok := r.base().configured[normalizeResource(name)]
	return config.typeName, config.config, ok
}

// BoneMealFeatures ports BiomeGenerationSettings.getBoneMealFeatures: the
// configured features, in the biome's feature order, that its placed
// features reference and #minecraft:can_spawn_from_bone_meal holds.
func (r *runtimeFeatures) BoneMealFeatures(tx *world.Tx, pos cube.Pos) []string {
	f := r.base()
	b, ok := f.biomes[normalizeResource(liveFeatureSource{tx: tx}.BiomeAt(int32(pos[0]), int32(pos[1]), int32(pos[2])))]
	if !ok {
		return nil
	}
	var out []string
	var collect func(name string)
	collect = func(name string) {
		name = normalizeResource(name)
		if boneMealFeatureTag[name] {
			out = append(out, "minecraft:"+name)
		}
		config, ok := f.configured[name]
		if !ok {
			return
		}
		for _, nested := range nestedPlacedFeatures(config.config) {
			if placed, ok := f.placed[normalizeResource(nested)]; ok {
				collect(placed.feature)
			}
		}
	}
	for _, stage := range b.stages {
		for _, placed := range stage {
			collect(f.placed[placed].feature)
		}
	}
	return out
}

// nestedPlacedFeatures returns the placed features a selector configured
// feature chooses from, by name.
func nestedPlacedFeatures(config map[string]any) []string {
	var names []string
	add := func(v any) {
		if name, ok := v.(string); ok {
			names = append(names, name)
		}
	}
	for _, key := range []string{"default", "feature_true", "feature_false"} {
		add(config[key])
	}
	for _, key := range []string{"features"} {
		list, _ := config[key].([]any)
		for _, entry := range list {
			if m, ok := entry.(map[string]any); ok {
				add(m["feature"])
			} else {
				add(entry)
			}
		}
	}
	return names
}

// place runs a feature against the world tx holds and applies what it wrote
// if it placed.
func (r *runtimeFeatures) place(tx *world.Tx, pos cube.Pos, run func(*ModernFeatures, *featureRegion, featurePos) bool) bool {
	base := r.base()
	f := &ModernFeatures{
		seed: base.seed, source: liveFeatureSource{tx: tx}, biomes: base.biomes, placed: base.placed,
		configured: base.configured, carvers: base.carvers, temperatures: base.temperatures, featureRanks: base.featureRanks,
	}
	region := newRecordingRegion(f, world.DefaultBlockRegistry, tx.Range())
	step := &originStep{}
	region.recording = step
	region.SetOrigin(int32(pos[0]>>4), int32(pos[2]>>4))
	if !run(f, region, featurePos{int32(pos[0]), int32(pos[1]), int32(pos[2])}) {
		return false
	}
	step.seal()
	applyRuntimeStep(tx, step)
	return true
}

// applyRuntimeStep writes what a feature placed into the world: each block
// whose state is still the one the feature found, then the block actors,
// scheduled ticks and entities it left.
func applyRuntimeStep(tx *world.Tx, step *originStep) {
	actors := make(map[cube.Pos]world.Block, len(step.ordered))
	for _, a := range step.ordered {
		pos := cube.Pos{int(a.entry.Pos[0]), int(a.entry.Pos[1]), int(a.entry.Pos[2])}
		for _, payload := range (structure.Placement{Blocks: []structure.Block{a.entry}}).BlockEntityPayloads() {
			if payload.Pos != pos {
				continue
			}
			b := world.DefaultBlockRegistry.BlockByRuntimeIDOrAir(a.state.Block)
			if prototype, ok := b.(world.NBTer); ok {
				actors[pos] = world.DecodeNBT(prototype, payload.Data, world.DefaultBlockRegistry).(world.Block)
			}
		}
	}
	for _, w := range step.writes {
		pos := cube.Pos{int(w.pos.x), int(w.pos.y), int(w.pos.z)}
		if tx.NativeState(pos) != w.before {
			continue
		}
		tx.SetFeatureState(pos, w.after, actors[pos])
	}
	for _, tick := range step.ticks {
		if tick.Kind == chunk.FluidTick {
			if liquid, ok := tx.Liquid(tick.Pos); ok {
				tx.ScheduleBlockUpdate(tick.Pos, liquid, 0)
			}
			continue
		}
		tx.ScheduleBlockUpdate(tick.Pos, tx.Block(tick.Pos), 0)
	}
	for _, e := range step.entities {
		tx.AddEntity(e.spawn())
	}
}

// liveFeatureSource is the source of a feature placed at runtime: the blocks,
// heights and biomes of the world tx holds.
type liveFeatureSource struct{ tx *world.Tx }

// nativeBiomes names the Java biome of each native biome ID.
var nativeBiomes = sync.OnceValue(func() map[uint32]string {
	m := make(map[uint32]string, len(modernBiomeIDs)+6)
	for i, id := range modernBiomeIDs {
		if _, ok := m[id]; !ok {
			m[id] = vanilla.OverworldBiomeName(uint8(i))
		}
	}
	for b, name := range map[world.Biome]string{
		biome.NetherWastes{}: "nether_wastes", biome.SoulSandValley{}: "soul_sand_valley", biome.CrimsonForest{}: "crimson_forest",
		biome.WarpedForest{}: "warped_forest", biome.BasaltDeltas{}: "basalt_deltas", biome.End{}: "the_end",
	} {
		m[uint32(b.EncodeBiome())] = "minecraft:" + name
	}
	return m
})

func (s liveFeatureSource) BiomeAt(x, y, z int32) string {
	if s.tx == nil {
		return "minecraft:plains"
	}
	if name, ok := nativeBiomes()[uint32(s.tx.Biome(cube.Pos{int(x), int(y), int(z)}).EncodeBiome())]; ok {
		return name
	}
	return "minecraft:plains"
}

func (s liveFeatureSource) NoiseBiomeAt(quartX, quartY, quartZ int32) string {
	return s.BiomeAt(quartX<<2, quartY<<2, quartZ<<2)
}

func (s liveFeatureSource) BaseHeight(x, z int32, oceanFloor bool) int32 {
	lowest := int32(s.tx.Range()[0])
	for y := int32(s.tx.HighestBlock(int(x), int(z))); y >= lowest; y-- {
		state := s.BaseState(x, y, z)
		if state.Block == world.DefaultBlockRegistry.AirRuntimeID() {
			continue
		}
		if !oceanFloor || blocksMotionOnOceanFloor(state) {
			return y + 1
		}
	}
	return lowest
}

// blocksMotionOnOceanFloor reports whether a block counts for the
// OCEAN_FLOOR heightmap: it blocks motion and is not a fluid.
func blocksMotionOnOceanFloor(state world.NativeState) bool {
	canonicalState, err := world.CanonicalState(world.DefaultBlockRegistry, world.NativeState{Block: state.Block, Liquid: world.DefaultBlockRegistry.AirRuntimeID()})
	if err != nil || canonicalState.Name == "minecraft:water" || canonicalState.Name == "minecraft:lava" {
		return false
	}
	matches, _ := gamedata.BlockTagContains("minecraft:blocks_motion_in_heightmap", canonicalState.Name)
	return matches
}

func (s liveFeatureSource) SeaLevel() int32 {
	switch s.tx.World().Dimension() {
	case world.Nether:
		return 32
	case world.End:
		return 0
	}
	return modernSeaLevel
}

func (s liveFeatureSource) BaseBlock(x, y, z int32) world.Block {
	return s.tx.Block(cube.Pos{int(x), int(y), int(z)})
}

func (liveFeatureSource) FeatureBiomeNames() []string {
	return slices.Clone(modernBiomeNames[:])
}

func (s liveFeatureSource) BaseState(x, y, z int32) world.NativeState {
	return s.tx.NativeState(cube.Pos{int(x), int(y), int(z)})
}

func (liveFeatureSource) UseLegacyRandomSource() bool { return false }

func (s liveFeatureSource) HasSkyLight() bool {
	return s.tx.World().Dimension() == world.Overworld
}

func (s liveFeatureSource) GenerationDepth() int32 {
	r := s.tx.Range()
	return int32(r[1] - r[0] + 1)
}

func (s liveFeatureSource) FluidTickDelay(name string) int32 {
	if name == "minecraft:lava" {
		if s.tx.World().Dimension() == world.Nether {
			return 10
		}
		return 30
	}
	return 5
}

func (liveFeatureSource) PlaceStructureStep(world.ChunkPos, int, structure.ProcessorEnvironment, func(structure.Placement)) {
}
