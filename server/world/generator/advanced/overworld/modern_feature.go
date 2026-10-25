package overworld

import (
	"fmt"
	"maps"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/structure"
)

// ModernFeatureSource supplies the immutable terrain and biome view used by Java placed features.
// Feature writes are applied only to the chunk being generated, while source reads may cross its border.
type ModernFeatureSource interface {
	BiomeAt(x, y, z int32) string
	NoiseBiomeAt(quartX, quartY, quartZ int32) string
	BaseHeight(x, z int32, oceanFloor bool) int32
	SeaLevel() int32
	BaseBlock(x, y, z int32) world.Block
	FeatureBiomeNames() []string
	BaseState(x, y, z int32) world.NativeState
	UseLegacyRandomSource() bool
	HasSkyLight() bool
	GenerationDepth() int32
	FluidTickDelay(name string) int32
	PlaceStructureStep(pos world.ChunkPos, step int, env structure.ProcessorEnvironment, emit func(structure.Placement))
}

// baseTopSource is a ModernFeatureSource that knows the top of its base
// terrain columns, which heightmap scans of a column start below.
type baseTopSource interface {
	// BaseTop returns a height at or above the highest non-air base state of
	// the column.
	BaseTop(x, z int32) int32
}

type modernBiomeFeatures struct {
	stages            [][]string
	carvers           []string
	frozenTemperature bool
	hasPrecipitation  bool
}

type modernFeaturePlacement struct {
	feature   string
	modifiers []map[string]any
}

type modernFeatureConfig struct {
	typeName string
	config   map[string]any
}

// ModernFeatures is an immutable, fully linked view of Java's biome decoration graph.
// Resolving it once up front ensures malformed/missing data-pack references cannot silently turn into air.
type ModernFeatures struct {
	seed         int64
	source       ModernFeatureSource
	biomes       map[string]modernBiomeFeatures
	placed       map[string]modernFeaturePlacement
	configured   map[string]modernFeatureConfig
	carvers      map[string]modernFeatureConfig
	temperatures map[string]float64
	featureRanks [][]map[string]int
	// origins caches what the features of each origin chunk write, shared by
	// the chunks around it that are decorated with them.
	origins originRecordCache
	// contexts caches what the structures and features of each origin chunk
	// write against the base terrain alone, shared by the origins it
	// precedes as the context their records are computed against.
	contexts originRecordCache
	// parallel is true if the records a chunk owns are computed at once.
	parallel bool
	// heightmaps caches the heightmap predicates of each native state.
	heightmaps sync.Map
}

// NewModernFeatures loads the actual embedded Java biome, placed-feature, configured-feature and carver data.
func NewModernFeatures(seed int64, source ModernFeatureSource) *ModernFeatures {
	if source == nil {
		panic("overworld: nil feature source")
	}
	f := &ModernFeatures{
		seed: seed, source: source,
		biomes:       make(map[string]modernBiomeFeatures),
		placed:       make(map[string]modernFeaturePlacement),
		configured:   make(map[string]modernFeatureConfig),
		carvers:      make(map[string]modernFeatureConfig),
		temperatures: make(map[string]float64),
	}
	biomeNames := requireResourceNames("biome")
	for _, name := range biomeNames {
		data := requireJSON("biome", name)
		if temperature, ok := data["temperature"].(float64); ok {
			f.temperatures[name] = temperature
		}
		stages, ok := data["features"].([]any)
		if !ok {
			panic(fmt.Sprintf("biome %q has no feature stages", name))
		}
		modifier, _ := data["temperature_modifier"].(string)
		precipitation, _ := data["has_precipitation"].(bool)
		biome := modernBiomeFeatures{stages: make([][]string, len(stages)), frozenTemperature: modifier == "frozen", hasPrecipitation: precipitation}
		for stage, entries := range stages {
			values, err := resourceReferences(entries)
			if err != nil {
				panic(fmt.Sprintf("biome %q feature stage %d: %v", name, stage, err))
			}
			biome.stages[stage] = values
		}
		if rawCarvers, exists := data["carvers"]; exists {
			values, err := resourceReferences(rawCarvers)
			if err != nil {
				panic(fmt.Sprintf("biome %q carvers: %v", name, err))
			}
			biome.carvers = values
		}
		f.biomes[name] = biome
	}

	for _, name := range requireResourceNames("placed_feature") {
		data := requireJSON("placed_feature", name)
		feature, ok := data["feature"].(string)
		if !ok {
			panic(fmt.Sprintf("placed feature %q has no configured feature", name))
		}
		modifiers, ok := data["placement"].([]any)
		if !ok {
			panic(fmt.Sprintf("placed feature %q has no placement list", name))
		}
		p := modernFeaturePlacement{feature: normalizeResource(feature), modifiers: make([]map[string]any, 0, len(modifiers))}
		for i, value := range modifiers {
			modifier, ok := value.(map[string]any)
			if !ok {
				panic(fmt.Sprintf("placed feature %q modifier %d is malformed", name, i))
			}
			p.modifiers = append(p.modifiers, modifier)
		}
		f.placed[name] = p
	}
	for _, name := range requireResourceNames("configured_feature") {
		data := requireJSON("configured_feature", name)
		typeName, ok := data["type"].(string)
		if !ok {
			panic(fmt.Sprintf("configured feature %q has no type", name))
		}
		normalizedType := normalizeResource(typeName)
		f.configured[name] = modernFeatureConfig{typeName: normalizedType, config: data}
	}
	for _, name := range requireResourceNames("configured_carver") {
		data := requireJSON("configured_carver", name)
		typeName, ok := data["type"].(string)
		if !ok {
			panic(fmt.Sprintf("configured carver %q has no type", name))
		}
		delete(data, "type")
		f.carvers[name] = modernFeatureConfig{typeName: normalizeResource(typeName), config: data}
	}
	for biomeName, biome := range f.biomes {
		for _, stage := range biome.stages {
			for _, placed := range stage {
				p, ok := f.placed[placed]
				if !ok {
					panic(fmt.Sprintf("biome %q references missing placed feature %q", biomeName, placed))
				}
				if _, ok := f.configured[p.feature]; !ok {
					panic(fmt.Sprintf("placed feature %q references missing configured feature %q", placed, p.feature))
				}
			}
		}
		for _, carver := range biome.carvers {
			if _, ok := f.carvers[carver]; !ok {
				panic(fmt.Sprintf("biome %q references missing configured carver %q", biomeName, carver))
			}
		}
	}
	f.buildFeatureRanks()
	return f
}

type featureOrderNode struct{ step, index int }

func (f *ModernFeatures) buildFeatureRanks() {
	names := f.source.FeatureBiomeNames()
	maxSteps := 0
	for _, name := range names {
		biome, ok := f.biomes[normalizeResource(name)]
		if !ok {
			panic("overworld: feature biome list references unknown biome " + name)
		}
		if len(biome.stages) > maxSteps {
			maxSteps = len(biome.stages)
		}
	}
	ids := make(map[string]int)
	edges := make(map[featureOrderNode]map[featureOrderNode]struct{})
	for _, name := range names {
		biome := f.biomes[normalizeResource(name)]
		sequence := make([]featureOrderNode, 0)
		for step, features := range biome.stages {
			for _, feature := range features {
				index, ok := ids[feature]
				if !ok {
					index = len(ids)
					ids[feature] = index
				}
				node := featureOrderNode{step, index}
				if _, ok := edges[node]; !ok {
					edges[node] = make(map[featureOrderNode]struct{})
				}
				sequence = append(sequence, node)
			}
		}
		for i := 0; i+1 < len(sequence); i++ {
			edges[sequence[i]][sequence[i+1]] = struct{}{}
		}
	}
	nodes := make([]featureOrderNode, 0, len(edges))
	for node := range edges {
		nodes = append(nodes, node)
	}
	less := func(a, b featureOrderNode) bool {
		if a.step != b.step {
			return a.step < b.step
		}
		return a.index < b.index
	}
	sort.Slice(nodes, func(i, j int) bool { return less(nodes[i], nodes[j]) })
	discovered, visiting := make(map[featureOrderNode]bool), make(map[featureOrderNode]bool)
	reverseTopological := make([]featureOrderNode, 0, len(nodes))
	var visit func(featureOrderNode)
	visit = func(node featureOrderNode) {
		if discovered[node] {
			return
		}
		if visiting[node] {
			panic("overworld: cyclic biome feature ordering")
		}
		visiting[node] = true
		children := make([]featureOrderNode, 0, len(edges[node]))
		for child := range edges[node] {
			children = append(children, child)
		}
		sort.Slice(children, func(i, j int) bool { return less(children[i], children[j]) })
		for _, child := range children {
			visit(child)
		}
		delete(visiting, node)
		discovered[node] = true
		reverseTopological = append(reverseTopological, node)
	}
	for _, node := range nodes {
		visit(node)
	}
	for left, right := 0, len(reverseTopological)-1; left < right; left, right = left+1, right-1 {
		reverseTopological[left], reverseTopological[right] = reverseTopological[right], reverseTopological[left]
	}
	f.featureRanks = make([][]map[string]int, maxSteps)
	for step := range f.featureRanks {
		f.featureRanks[step] = []map[string]int{make(map[string]int)}
	}
	namesByID := make([]string, len(ids))
	for name, index := range ids {
		namesByID[index] = name
	}
	for _, node := range reverseTopological {
		name := namesByID[node.index]
		if _, exists := f.featureRanks[node.step][0][name]; !exists {
			f.featureRanks[node.step][0][name] = len(f.featureRanks[node.step][0])
		}
	}
}

// Decorate places the structures and features of the chunk at pos and the
// features of its neighbours that reach into it. The features of every
// origin chunk come from one record, computed once against the origins that
// precede it, so a feature crossing a chunk border is the same in every chunk
// it reaches. The structures of the chunk are placed as it decorates, since
// their pieces keep state from chunk to chunk.
func (f *ModernFeatures) Decorate(pos world.ChunkPos, c *chunk.Chunk, context world.GenerationContext) world.GenerationResult {
	result := world.GenerationResult{}
	region := newFeatureRegion(f, c, pos, &result)
	origins := decorationOrigins(pos)
	// The records other chunks are computing are waited for only after this
	// chunk computed those it took on.
	var records [len(origins)]*originRecordEntry
	for i, origin := range origins {
		records[i] = f.originRecord(origin, c.BlockRegistry(), c.Range(), context)
	}
	for i, origin := range origins {
		record := records[i].wait()
		region.SetOrigin(origin[0], origin[1])
		for step := range record.steps {
			if origin == pos {
				f.placeStructures(region, origin, step, context, false)
			}
			record.steps[step].apply(region)
		}
		f.origins.release(origin, len(origins))
	}
	region.release()
	return result
}

// DecorateInOneRegion decorates the chunk at pos by running the nine origins
// around it one after another in a single region, as one Java WorldGenRegion
// applying applyBiomeDecoration for each of them does. The features of the
// neighbours then depend on the chunk being decorated, so generators use
// Decorate. This is the composition Java decoration captures are taken in.
func (f *ModernFeatures) DecorateInOneRegion(pos world.ChunkPos, c *chunk.Chunk, context world.GenerationContext) world.GenerationResult {
	result := world.GenerationResult{}
	region := newFeatureRegion(f, c, pos, &result)
	for _, origin := range decorationOrigins(pos) {
		f.decorateOrigin(region, origin, context, origin != pos, nil)
	}
	return result
}

// Tune sizes the record caches for workers generating chunks at once and
// sets whether the records a chunk owns are computed in parallel. It must be
// called before any chunk is decorated.
func (f *ModernFeatures) Tune(workers int, parallel bool) {
	f.parallel = parallel
	capacity := max(originRecordCacheSize, 64*workers)
	f.origins.capacity, f.contexts.capacity = capacity, capacity
}

// generationSteps returns the number of decoration steps of an origin.
func (f *ModernFeatures) generationSteps() int {
	return max(len(f.featureRanks), 11)
}

// decorateOrigin runs every decoration step of origin in region: its
// structures, which only read shared piece state if transient, then its
// features. The features are recorded into record if it is not nil.
func (f *ModernFeatures) decorateOrigin(region *featureRegion, origin world.ChunkPos, context world.GenerationContext, transient bool, record *originRecord) {
	region.SetOrigin(origin[0], origin[1])
	possible := f.possibleBiomes(region, origin)
	for step := range f.generationSteps() {
		if record != nil {
			region.structureRecording = &record.steps[step]
		}
		f.placeStructures(region, origin, step, context, transient)
		region.structureRecording = nil
		if record != nil {
			region.recording = &record.steps[step]
		}
		f.placeFeatures(region, origin, step, possible)
		region.recording = nil
	}
}

// decorateContext runs every decoration step of origin in region, recording
// the writes of its structures and features alike into one step.
func (f *ModernFeatures) decorateContext(region *featureRegion, origin world.ChunkPos, context world.GenerationContext, record *originStep) {
	region.SetOrigin(origin[0], origin[1])
	possible := f.possibleBiomes(region, origin)
	region.recording = record
	for step := range f.generationSteps() {
		f.placeStructures(region, origin, step, context, true)
		f.placeFeatures(region, origin, step, possible)
	}
	region.recording = nil
}

// possibleBiomes returns the noise biomes Java's decoration of origin
// considers: those of the quarts around the chunk at every height.
func (f *ModernFeatures) possibleBiomes(region *featureRegion, origin world.ChunkPos) map[string]struct{} {
	originX, originZ := origin[0]<<4, origin[1]<<4
	minQuartY, maxQuartY := region.MinY()>>2, region.MaxY()>>2
	minQuartX, maxQuartX := (originX>>2)-4, (originX>>2)+7
	minQuartZ, maxQuartZ := (originZ>>2)-4, (originZ>>2)+7
	possible := make(map[string]struct{})
	if source, ok := f.source.(modernTerrainSource); ok && source.generator != nil {
		// The quarts span the origin chunk and the chunks around it.
		for dz := int32(-1); dz <= 1; dz++ {
			for dx := int32(-1); dx <= 1; dx++ {
				for _, biome := range source.NoiseBiomesInChunk(world.ChunkPos{origin[0] + dx, origin[1] + dz}, minQuartY, maxQuartY) {
					name := normalizeResource(biome)
					if _, ok := f.biomes[name]; !ok {
						panic("overworld: noise biome source returned unknown biome " + name)
					}
					possible[name] = struct{}{}
				}
			}
		}
		return possible
	}
	for qy := minQuartY; qy <= maxQuartY; qy++ {
		for qz := minQuartZ; qz <= maxQuartZ; qz++ {
			for qx := minQuartX; qx <= maxQuartX; qx++ {
				name := normalizeResource(f.source.NoiseBiomeAt(qx, qy, qz))
				if _, ok := f.biomes[name]; !ok {
					panic("overworld: noise biome source returned unknown biome " + name)
				}
				possible[name] = struct{}{}
			}
		}
	}
	return possible
}

// placeStructures places the structure pieces of one decoration step of
// origin. Transient placements read shared piece state without changing it.
func (f *ModernFeatures) placeStructures(region *featureRegion, origin world.ChunkPos, step int, context world.GenerationContext, transient bool) {
	originX, originZ := origin[0]<<4, origin[1]<<4
	structureRandom := featureRandom(f.seed, originX, originZ, 0, step)
	env := structure.ProcessorEnvironment{
		BaseBlock:  func(x, y, z int32) world.Block { return region.Block(featurePos{x, y, z}) },
		BiomeAt:    f.source.BiomeAt,
		BaseHeight: region.Height,
		SeaLevel:   f.source.SeaLevel(),
		MotionBlockingNoLeavesHeight: func(x, z int32) int32 {
			return region.placementHeight(x, z, "MOTION_BLOCKING_NO_LEAVES")
		},
		BlockStateID: func(x, y, z int32) (uint32, error) {
			return region.BlockStateID(cube.Pos{int(x), int(y), int(z)})
		},
		HasChestBlockEntity: func(x, y, z int32) bool {
			return region.hasChestBlockEntity(cube.Pos{int(x), int(y), int(z)})
		},
		SetBlockEntityLootTable: func(x, y, z int32, table string, seed int64) {
			region.setBlockEntityLootTable(cube.Pos{int(x), int(y), int(z)}, table, seed)
		},
		CanWriteBlock: func(x, y, z int32) bool {
			return region.withinWriteArea(featurePos{x, y, z})
		},
		Range:                   region.Range(),
		TransientStructureState: transient,
		WorldSeed:               f.seed,
		Random:                  &structureRandom,
		LevelRandom:             region.RegionRandom(),
		GenerationContext:       context,
		PlaceFeature: func(feature any, pos [3]int32, random *vanilla.Random) bool {
			return region.PlacePlacedFeature(feature, pos, random)
		},
	}
	f.source.PlaceStructureStep(origin, step, env, func(placement structure.Placement) { f.applyStructurePlacement(region, placement) })
}

// placeFeatures places the features of one decoration step of origin in
// rank order.
func (f *ModernFeatures) placeFeatures(region *featureRegion, origin world.ChunkPos, step int, possible map[string]struct{}) {
	if step >= len(f.featureRanks) {
		return
	}
	originX, originZ := origin[0]<<4, origin[1]<<4
	ranks := f.featureRanks[step][0]
	features := make([]string, 0)
	selected := make(map[string]struct{})
	for name := range possible {
		biome := f.biomes[name]
		if step >= len(biome.stages) {
			continue
		}
		for _, feature := range biome.stages[step] {
			if _, exists := selected[feature]; !exists {
				selected[feature] = struct{}{}
				features = append(features, feature)
			}
		}
	}
	sort.Slice(features, func(i, j int) bool { return ranks[features[i]] < ranks[features[j]] })
	for _, name := range features {
		random := featureRandom(f.seed, originX, originZ, ranks[name], step)
		f.placeFeature(region, originX, originZ, name, random)
	}
}

func featureRandom(seed int64, chunkX, chunkZ int32, index, stage int) vanilla.Random {
	r := vanilla.NewWorldgenRandom(seed)
	decorationSeed := r.SetDecorationSeed(seed, chunkX, chunkZ)
	r.SetFeatureSeed(decorationSeed, int32(index), int32(stage))
	return r
}

func (f *ModernFeatures) placeFeature(region *featureRegion, originX, originZ int32, name string, random vanilla.Random) bool {
	placed, ok := f.placed[name]
	if !ok {
		panic("overworld: missing placed feature " + name)
	}
	return f.placeWithModifiers(region, placed.feature, placed.modifiers, featurePos{x: originX, z: originZ}, &random, name)
}

func (f *ModernFeatures) biomeHasFeature(biomeName, placedName string) bool {
	biome, ok := f.biomes[biomeName]
	if !ok {
		return false
	}
	for _, stage := range biome.stages {
		for _, name := range stage {
			if name == placedName {
				return true
			}
		}
	}
	return false
}

type featurePos struct{ x, y, z int32 }

func featureInsideTarget(pos world.ChunkPos, p featurePos) bool {
	return p.x>>4 == int32(pos[0]) && p.z>>4 == int32(pos[1])
}

// featureStateRID resolves literal non-actor states. Actor-backed states must retain their NativeBlock through SampleNativeState and SetNativeBlock.
func featureStateRID(c *chunk.Chunk, state map[string]any) (world.NativeState, bool) {
	placement, ok := featureStateBlock(state)
	if !ok {
		return world.NativeState{}, false
	}
	if placement.NativeBlock != nil {
		panic("overworld: actor-backed feature state must use SampleNativeState and SetNativeBlock")
	}
	native, err := placement.ResolveNative(c.BlockRegistry())
	if err != nil {
		panic(err)
	}
	return native, true
}

func featureStateBlock(value any) (structure.Block, bool) {
	if name, ok := value.(string); ok {
		if !strings.Contains(name, ":") {
			name = "minecraft:" + name
		}
		return featureBlock(world.BlockState{Name: name}), true
	}
	state, ok := value.(map[string]any)
	if !ok {
		return structure.Block{}, false
	}
	name, ok := state["id"].(string)
	if !ok {
		return structure.Block{}, false
	}
	var properties map[string]any
	if raw, exists := state["properties"]; exists {
		values, ok := raw.(map[string]any)
		if !ok {
			panic("overworld: malformed block-state provider")
		}
		properties = make(map[string]any, len(values))
		for key, value := range values {
			if text, ok := value.(string); ok {
				switch text {
				case "true":
					value = true
				case "false":
					value = false
				default:
					if n, err := strconv.ParseInt(text, 10, 32); err == nil {
						value = int32(n)
					}
				}
			}
			properties[key] = value
		}
	}
	if !strings.Contains(name, ":") {
		name = "minecraft:" + name
	}
	return featureBlock(world.BlockState{Name: name, Properties: properties}), true
}

func featureBlock(state world.BlockState) structure.Block {
	actor, ok, err := structure.NativeBlockForState(state)
	if err != nil {
		panic(err)
	}
	placement := structure.Block{State: state}
	if ok {
		placement.NativeBlock = actor
	}
	return placement
}

func sampleBlockState(region *featureRegion, p featurePos, provider any, r *vanilla.Random) (structure.Block, bool) {
	provider = resolveStateProvider(provider)
	state, ok := provider.(map[string]any)
	if !ok {
		panic("overworld: malformed block state provider")
	}
	if typeName, ok := state["type"].(string); ok {
		switch normalizeResource(typeName) {
		case "simple":
			return featureStateBlock(state["state"])
		case "copy_properties":
			block, ok := sampleBlockState(region, p, state["source"], r)
			if !ok {
				return structure.Block{}, false
			}
			id := featureBlockStateID(block)
			next, err := gamedata.BlockStateWithPropertiesOf(id, region.nativeState(p).CanonicalID(region.c.BlockRegistry()))
			if err != nil {
				panic(err)
			}
			return featureBlockWithStateID(block, id, next), true
		case "weighted":
			entries, ok := state["entries"].([]any)
			if !ok {
				panic("overworld: malformed weighted state provider")
			}
			total := int32(0)
			for _, raw := range entries {
				entry := raw.(map[string]any)
				weight := intProviderValue(entry["weight"])
				if weight < 0 || total > int32(2147483647)-weight {
					panic("overworld: invalid block state weight")
				}
				total += weight
			}
			if total <= 0 {
				panic("overworld: empty weighted block state provider")
			}
			n := r.NextInt(total)
			for _, raw := range entries {
				entry := raw.(map[string]any)
				weight := intProviderValue(entry["weight"])
				if n < weight {
					return featureStateBlock(entry["data"])
				}
				n -= weight
			}
			panic("overworld: weighted block state selection escaped its total")
		case "random_block":
			if block, ok := sampleRandomBlockState(state["blocks"], r); ok {
				return block, true
			}
			name, properties := region.State(p)
			return featureBlock(world.BlockState{Name: name, Properties: maps.Clone(properties)}), true
		case "rotated":
			direction, explicit := state["direction"].(string)
			if !explicit {
				direction = [...]string{"down", "up", "north", "south", "west", "east"}[r.NextInt(6)]
			}
			block, ok := sampleBlockState(region, p, state["state"], r)
			if !ok {
				return structure.Block{}, false
			}
			id := featureBlockStateID(block)
			next := id
			if gamedata.BlockStateHasEnumValue(id, "axis", "y") {
				axis := "y"
				switch direction {
				case "east", "west":
					axis = "x"
				case "north", "south":
					axis = "z"
				}
				var err error
				next, err = gamedata.BlockStateWithProperty(next, "axis", axis)
				if err != nil {
					panic(err)
				}
			}
			fullFacing := gamedata.BlockStateHasEnumValue(id, "facing", "up")
			horizontalFacing := direction != "up" && direction != "down" && gamedata.BlockStateHasEnumValue(id, "facing", "north") && !gamedata.BlockStateHasEnumValue(id, "facing", "down")
			if fullFacing || horizontalFacing {
				var err error
				next, err = gamedata.BlockStateWithProperty(next, "facing", direction)
				if err != nil {
					panic(err)
				}
			}
			return featureBlockWithStateID(block, id, next), true
		case "randomized_int":
			property, ok := state["property"].(string)
			if !ok {
				panic("overworld: malformed randomized_int provider")
			}
			block, ok := sampleBlockState(region, p, state["source"], r)
			if !ok {
				return structure.Block{}, false
			}
			id := featureBlockStateID(block)
			if _, integer := gamedata.BlockStateInt(id, property); !integer {
				return block, true
			}
			next, err := gamedata.BlockStateWithProperty(id, property, sampleInt(state["values"], r))
			if err != nil {
				panic(err)
			}
			return featureBlockWithStateID(block, id, next), true
		case "noise", "noise_threshold", "dual_noise":
			return sampleNoiseStateProvider(p, state, r)
		case "rule_based":
			if sampled, ok := sampleRuleState(region, p, state, r); ok {
				return sampled, true
			}
			name, properties := region.State(p)
			return featureBlock(world.BlockState{Name: name, Properties: maps.Clone(properties)}), true
		default:
			panic("overworld: unsupported block state provider " + normalizeResource(typeName))
		}
	}
	return featureStateBlock(state)
}

func featureBlockStateID(block structure.Block) uint32 {
	id, err := gamedata.BlockStateID(block.State.Name, block.State.Properties)
	if err != nil {
		panic(err)
	}
	return id
}

func featureBlockWithStateID(block structure.Block, previous, next uint32) structure.Block {
	if previous == next {
		return block
	}
	name, properties, ok := gamedata.BlockStateFromID(next)
	if !ok {
		panic("overworld: invalid sampled block state")
	}
	return featureBlock(world.BlockState{Name: name, Properties: properties})
}

func sampleRandomBlockState(value any, r *vanilla.Random) (structure.Block, bool) {
	switch blocks := value.(type) {
	case string:
		if strings.HasPrefix(blocks, "#") {
			names := blockTagNames(normalizeResource(strings.TrimPrefix(blocks, "#")))
			if len(names) == 0 {
				return structure.Block{}, false
			}
			return featureStateBlock(names[r.NextInt(int32(len(names)))])
		}
		r.NextInt(1)
		return featureStateBlock(blocks)
	case []any:
		if len(blocks) == 0 {
			return structure.Block{}, false
		}
		return featureStateBlock(blocks[r.NextInt(int32(len(blocks)))])
	default:
		panic("overworld: malformed random_block holder set")
	}
}

func sampleOptionalBlockState(region *featureRegion, p featurePos, provider any, r *vanilla.Random) (structure.Block, bool) {
	provider = resolveStateProvider(provider)
	if state, ok := provider.(map[string]any); ok {
		if kind, _ := state["type"].(string); normalizeResource(kind) == "rule_based" {
			return sampleRuleState(region, p, state, r)
		} else if normalizeResource(kind) == "random_block" {
			return sampleRandomBlockState(state["blocks"], r)
		}
	}
	return sampleBlockState(region, p, provider, r)
}

func sampleRuleState(region *featureRegion, p featurePos, state map[string]any, r *vanilla.Random) (structure.Block, bool) {
	rules, ok := state["rules"].([]any)
	if !ok {
		panic("overworld: malformed rule_based provider")
	}
	for _, raw := range rules {
		rule, ok := raw.(map[string]any)
		if !ok {
			panic("overworld: malformed rule_based rule")
		}
		predicate, ok := rule["if_true"].(map[string]any)
		if !ok {
			panic("overworld: malformed rule_based predicate")
		}
		if featurePredicate(region, p, predicate) {
			then, exists := rule["then"]
			if !exists {
				panic("overworld: rule_based rule has no then provider")
			}
			if sampled, ok := sampleOptionalBlockState(region, p, then, r); ok {
				return sampled, true
			}
		}
	}
	if fallback, exists := state["fallback"]; exists {
		return sampleOptionalBlockState(region, p, fallback, r)
	}
	return structure.Block{}, false
}

func cloneFeatureProperties(properties map[string]any) map[string]any {
	clone := make(map[string]any, len(properties))
	for key, value := range properties {
		clone[key] = value
	}
	return clone
}

func blockTagNames(tag string) []string {
	names, err := gamedata.BlockTagMembers(tag)
	if err != nil {
		panic(err)
	}
	for i := range names {
		names[i] = normalizeResource(names[i])
	}
	return names
}
func requireJSON(kind, name string) map[string]any {
	data, err := gamedata.LoadJSON(kind, name)
	if err != nil {
		panic(err)
	}
	return data
}

func requireResourceNames(kind string) []string {
	names, err := gamedata.ResourceNames(kind)
	if err != nil {
		panic(err)
	}
	return names
}

// Java holder-set codecs accept either one reference or an ordered list.
func resourceReferences(raw any) ([]string, error) {
	switch values := raw.(type) {
	case string:
		return []string{normalizeResource(values)}, nil
	case []any:
		names := make([]string, len(values))
		for i, value := range values {
			name, ok := value.(string)
			if !ok {
				return nil, fmt.Errorf("resource reference %d is not a string", i)
			}
			names[i] = normalizeResource(name)
		}
		return names, nil
	default:
		return nil, fmt.Errorf("resource references must be a string or list, got %T", raw)
	}
}

func normalizeResource(name string) string {
	return strings.TrimPrefix(name, "minecraft:")
}

func featureNumber(v any) int {
	n, ok := v.(float64)
	if !ok || n > 100000 || n < -100000 {
		panic(fmt.Sprintf("overworld: invalid feature number %v", v))
	}
	return int(n)
}

var biomeInfoNoise = func() *vanilla.SimplexNoise {
	r := vanilla.NewLegacyRandom(2345)
	return vanilla.NewSimplexNoiseWithoutOffset(&r)
}()

func sampleHeight(provider map[string]any, r *vanilla.Random, worldMinY, worldMaxY, seaLevel int32) int32 {
	bound := func(v any) int32 { return int32(featureBound(v, worldMinY, worldMaxY, seaLevel)) }
	typeName, typed := provider["type"].(string)
	if !typed {
		return bound(provider)
	}
	typeName = normalizeResource(typeName)
	switch typeName {
	case "constant":
		return bound(provider["value"])
	case "weighted_list":
		distribution, ok := provider["distribution"].([]any)
		if !ok || len(distribution) == 0 {
			panic("overworld: malformed weighted height provider")
		}
		total := int32(0)
		for _, raw := range distribution {
			entry, ok := raw.(map[string]any)
			if !ok {
				panic("overworld: malformed weighted height entry")
			}
			weight := intProviderValue(entry["weight"])
			if weight < 0 || total > int32(2147483647)-weight {
				panic("overworld: invalid height weight")
			}
			total += weight
		}
		if total == 0 {
			panic("overworld: weighted height provider has no positive weight")
		}
		choice := r.NextInt(total)
		for _, raw := range distribution {
			entry := raw.(map[string]any)
			weight := intProviderValue(entry["weight"])
			if choice < weight {
				child, ok := entry["data"].(map[string]any)
				if !ok {
					panic("overworld: weighted height entry has no provider")
				}
				return sampleHeight(child, r, worldMinY, worldMaxY, seaLevel)
			}
			choice -= weight
		}
		panic("overworld: weighted height selection escaped its total")
	case "uniform", "trapezoid", "biased_to_bottom", "very_biased_to_bottom":
		minY, maxY := bound(provider["min_inclusive"]), bound(provider["max_inclusive"])
		switch typeName {
		case "uniform":
			if minY > maxY {
				return minY
			}
			return minY + r.NextInt(maxY-minY+1)
		case "trapezoid":
			if minY > maxY {
				return minY
			}
			plateau := int32(0)
			if value, exists := provider["plateau"]; exists {
				plateau = intProviderValue(value)
			}
			width := maxY - minY
			if plateau >= width {
				return minY + r.NextInt(width+1)
			}
			plateauStart := (width - plateau) / 2
			plateauEnd := width - plateauStart
			return minY + r.NextInt(plateauEnd+1) + r.NextInt(plateauStart+1)
		}
		inner := int32(1)
		if value, exists := provider["inner"]; exists {
			inner = intProviderValue(value)
		}
		if inner < 1 {
			panic("overworld: height inner must be positive")
		}
		extent := maxY - minY - inner + 1
		if extent <= 0 {
			return minY
		}
		if typeName == "biased_to_bottom" {
			limit := r.NextInt(extent)
			return minY + r.NextInt(limit+inner)
		}
		upper := heightNextInt(r, minY+inner, maxY)
		biasedUpper := heightNextInt(r, minY, upper-1)
		return heightNextInt(r, minY, biasedUpper-1+inner)
	default:
		panic("overworld: unsupported height provider " + typeName)
	}
}

// heightNextInt matches Mth.nextInt, which unlike randomBetweenInclusive
// consumes no draw when its endpoints coincide or are inverted.
func heightNextInt(r *vanilla.Random, minimum, maximum int32) int32 {
	if minimum >= maximum {
		return minimum
	}
	return minimum + r.NextInt(maximum-minimum+1)
}

func noiseBasedCount(p featurePos, modifier map[string]any) int {
	factor, ok := modifier["noise_factor"].(float64)
	if !ok || factor == 0 {
		panic("overworld: invalid noise_based_count factor")
	}
	ratio := featureNumber(modifier["noise_to_count_ratio"])
	offset, ok := modifier["noise_offset"].(float64)
	if !ok {
		offset = 0
	}
	noise := biomeInfoNoise.Sample2D(float64(p.x)/factor, float64(p.z)/factor)
	return int(math.Ceil((noise + offset) * float64(ratio)))
}

func noiseThresholdCount(p featurePos, modifier map[string]any) int {
	level, ok := modifier["noise_level"].(float64)
	if !ok {
		panic("overworld: malformed noise_threshold_count noise_level")
	}
	below, ok := modifier["below_noise"].(float64)
	if !ok {
		panic("overworld: malformed noise_threshold_count below_noise")
	}
	above, ok := modifier["above_noise"].(float64)
	if !ok {
		panic("overworld: malformed noise_threshold_count above_noise")
	}
	noise := biomeInfoNoise.Sample2D(float64(p.x)/200.0, float64(p.z)/200.0)
	if noise < level {
		return int(below)
	}
	return int(above)
}

func blockTagContains(tag, blockName string) bool {
	matches, err := gamedata.BlockTagContains(tag, blockName)
	if err != nil {
		panic(err)
	}
	return matches
}

// featureBlockTagContainsID reports whether the block of state ID id is in a
// block tag. A zero ID, a state without a source block, is in no tag.
func featureBlockTagContainsID(tag string, id uint32) bool {
	matches, err := gamedata.BlockTagContainsID(tag, id)
	if err != nil {
		panic(err)
	}
	return matches
}

type modernOreTarget struct {
	rid       world.NativeState
	actor     world.Block
	predicate map[string]any
}

// featureBound resolves a vertical anchor; worldMaxY is exclusive.
func featureBound(v any, worldMinY, worldMaxY, seaLevel int32) float64 {
	if n, ok := v.(float64); ok {
		return n
	}
	if anchor, ok := v.(map[string]any); ok {
		if n, ok := anchor["absolute"].(float64); ok {
			return n
		}
		if n, ok := anchor["above_bottom"].(float64); ok {
			return float64(worldMinY) + n
		}
		if n, ok := anchor["below_top"].(float64); ok {
			return float64(worldMaxY-1) - n
		}
		if n, ok := anchor["relative_to_sea_level"].(float64); ok {
			return float64(seaLevel) + n
		}
	}
	panic(fmt.Sprintf("overworld: malformed height bound %v", v))
}

// applyStructurePlacement adapts Java descriptors, applies allowed blocks, and publishes target metadata.
func (f *ModernFeatures) applyStructurePlacement(region *featureRegion, placement structure.Placement) {
	// The writes of a piece its origin chunk places beyond itself reach the
	// chunks around through the origin's record, as feature writes do.
	if placement.BeyondChunk && region.structureRecording != nil && region.recording == nil {
		region.recording, region.recordBeyondOrigin = region.structureRecording, true
		defer func() { region.recording, region.recordBeyondOrigin = nil, false }()
	}
	sourceBlockCount := len(placement.Blocks)
	// List pool elements share a descriptor slice, but each source template
	// completes its own fluid pass before the next one replaces any blocks.
	for first := 0; first < sourceBlockCount; {
		end := first + 1
		for end < sourceBlockCount && !placement.Blocks[end].TemplateStart {
			end++
		}
		payloadBlocks, written := applyTemplateFluidsForStructureWrites(
			region, placement.Blocks[first:end], placement.StructurePieceWrites, templatePlacementFlags(placement),
		)
		for _, entry := range payloadBlocks {
			region.recordTemplateActor(entry)
		}
		if end == sourceBlockCount {
			for _, tick := range placement.ScheduledTicks {
				region.reserveTick(tick)
			}
		}
		if placement.TemplateUpdateFlags != 0 && !placement.TemplateKnownShape {
			world.UpdateTemplateShapes(region, written, placement.TemplateUpdateFlags)
		}
		first = end
	}
	if sourceBlockCount == 0 {
		for _, tick := range placement.ScheduledTicks {
			region.reserveTick(tick)
		}
	}
	var err error
	placement, err = placement.AdaptItemFrameEntities()
	if err != nil {
		panic(err)
	}
	frameBlocks := placement.Blocks[sourceBlockCount:]
	applyTemplateProjectionBlocks(region, frameBlocks, 18)
	applyTemplatePostShapeBlocks(region, placement.PostShapeBlocks, templatePlacementFlags(placement))
	placement.Entities = append(placement.Entities, placement.PostShapeEntities...)
	for _, pos := range placement.PostProcessing {
		if region.inTarget(featurePos{int32(pos[0]), int32(pos[1]), int32(pos[2])}) {
			region.result.MarkForPostProcessing(pos, region.c.Range())
		}
	}
	entities := make([]structure.Entity, 0, len(placement.Entities))
	for _, record := range placement.Entities {
		x, z := int32(math.Floor(record.Pos[0])), int32(math.Floor(record.Pos[2]))
		if region.inTarget(featurePos{x: x, z: z}) {
			entities = append(entities, record)
		}
	}
	if len(entities) != 0 {
		handles, err := (structure.Placement{Entities: entities}).NewEntityHandles(entity.NewFromStructureNBT)
		if err != nil {
			panic(err)
		}
		region.result.Entities = append(region.result.Entities, handles...)
	}
}

func templatePlacementFlags(placement structure.Placement) int {
	if placement.TemplateUpdateFlags != 0 {
		return placement.TemplateUpdateFlags
	}
	return 3
}
