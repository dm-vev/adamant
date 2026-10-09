package overworld

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/df-mc/dragonfly/server/entity"
	java26 "github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/internal/javadata"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/javastructure"
)

// ModernFeatureSource supplies the immutable terrain and biome view used by Java placed features.
// Feature writes are applied only to the chunk being generated, while source reads may cross its border.
type ModernFeatureSource interface {
	JavaBiomeAt(x, y, z int32) string
	JavaNoiseBiomeAt(quartX, quartY, quartZ int32) string
	BaseHeight(x, z int32, oceanFloor bool) int32
	SeaLevel() int32
	BaseBlock(x, y, z int32) world.Block
	JavaFeatureBiomeNames() []string
	BaseState(x, y, z int32) world.NativeState
	UseLegacyRandomSource() bool
	JavaHasSkyLight() bool
	JavaFluidTickDelay(name string) int32
	PlaceStructureStep(pos world.ChunkPos, step int, env javastructure.ProcessorEnvironment, emit func(javastructure.Placement))
}

type modernBiomeFeatures struct {
	stages  [][]string
	carvers []string
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
}

// NewModernFeatures loads the actual embedded Java biome, placed-feature, configured-feature and carver data.
func NewModernFeatures(seed int64, source ModernFeatureSource) *ModernFeatures {
	if source == nil {
		panic("overworld: nil Java feature source")
	}
	f := &ModernFeatures{
		seed: seed, source: source,
		biomes:       make(map[string]modernBiomeFeatures),
		placed:       make(map[string]modernFeaturePlacement),
		configured:   make(map[string]modernFeatureConfig),
		carvers:      make(map[string]modernFeatureConfig),
		temperatures: make(map[string]float64),
	}
	biomeNames := requireJavaResourceNames("biome")
	for _, name := range biomeNames {
		data := requireJavaJSON("biome", name)
		if temperature, ok := data["temperature"].(float64); ok {
			f.temperatures[name] = temperature
		}
		stages, ok := data["features"].([]any)
		if !ok {
			panic(fmt.Sprintf("Java biome %q has no feature stages", name))
		}
		biome := modernBiomeFeatures{stages: make([][]string, len(stages))}
		for stage, entries := range stages {
			values, err := javaResourceReferences(entries)
			if err != nil {
				panic(fmt.Sprintf("Java biome %q feature stage %d: %v", name, stage, err))
			}
			biome.stages[stage] = values
		}
		if rawCarvers, exists := data["carvers"]; exists {
			values, err := javaResourceReferences(rawCarvers)
			if err != nil {
				panic(fmt.Sprintf("Java biome %q carvers: %v", name, err))
			}
			biome.carvers = values
		}
		f.biomes[name] = biome
	}

	for _, name := range requireJavaResourceNames("placed_feature") {
		data := requireJavaJSON("placed_feature", name)
		feature, ok := data["feature"].(string)
		if !ok {
			panic(fmt.Sprintf("Java placed feature %q has no configured feature", name))
		}
		modifiers, ok := data["placement"].([]any)
		if !ok {
			panic(fmt.Sprintf("Java placed feature %q has no placement list", name))
		}
		p := modernFeaturePlacement{feature: normalizeJavaResource(feature), modifiers: make([]map[string]any, 0, len(modifiers))}
		for i, value := range modifiers {
			modifier, ok := value.(map[string]any)
			if !ok {
				panic(fmt.Sprintf("Java placed feature %q modifier %d is malformed", name, i))
			}
			p.modifiers = append(p.modifiers, modifier)
		}
		f.placed[name] = p
	}
	for _, name := range requireJavaResourceNames("configured_feature") {
		data := requireJavaJSON("configured_feature", name)
		typeName, ok := data["type"].(string)
		if !ok {
			panic(fmt.Sprintf("Java configured feature %q has no type", name))
		}
		normalizedType := normalizeJavaResource(typeName)
		if normalizedType == "spring_feature" {
			normalizeSpringFeatureConfig(data)
		}
		f.configured[name] = modernFeatureConfig{typeName: normalizedType, config: data}
	}
	for _, name := range requireJavaResourceNames("configured_carver") {
		data := requireJavaJSON("configured_carver", name)
		typeName, ok := data["type"].(string)
		if !ok {
			panic(fmt.Sprintf("Java configured carver %q has no type", name))
		}
		delete(data, "type")
		f.carvers[name] = modernFeatureConfig{typeName: normalizeJavaResource(typeName), config: data}
	}
	for biomeName, biome := range f.biomes {
		for _, stage := range biome.stages {
			for _, placed := range stage {
				p, ok := f.placed[placed]
				if !ok {
					panic(fmt.Sprintf("Java biome %q references missing placed feature %q", biomeName, placed))
				}
				if _, ok := f.configured[p.feature]; !ok {
					panic(fmt.Sprintf("Java placed feature %q references missing configured feature %q", placed, p.feature))
				}
			}
		}
		for _, carver := range biome.carvers {
			if _, ok := f.carvers[carver]; !ok {
				panic(fmt.Sprintf("Java biome %q references missing configured carver %q", biomeName, carver))
			}
		}
	}
	f.buildFeatureRanks()
	return f
}

type featureOrderNode struct{ step, index int }

func (f *ModernFeatures) buildFeatureRanks() {
	names := f.source.JavaFeatureBiomeNames()
	maxSteps := 0
	for _, name := range names {
		biome, ok := f.biomes[normalizeJavaResource(name)]
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
		biome := f.biomes[normalizeJavaResource(name)]
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
			panic("overworld: cyclic Java biome feature ordering")
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

func (f *ModernFeatures) Decorate(pos world.ChunkPos, c *chunk.Chunk) world.GenerationResult {
	result := world.GenerationResult{}
	region := newFeatureRegion(f, c, pos, &result)
	minQuartY, maxQuartY := region.MinY()>>2, region.MaxY()>>2
	for oz := int32(-1); oz <= 1; oz++ {
		for ox := int32(-1); ox <= 1; ox++ {
			originChunkX, originChunkZ := int32(pos[0])+ox, int32(pos[1])+oz
			originX, originZ := originChunkX<<4, originChunkZ<<4
			region.SetOrigin(originChunkX, originChunkZ)
			possible := make(map[string]struct{})
			minQuartX, maxQuartX := (originX>>2)-4, (originX>>2)+7
			minQuartZ, maxQuartZ := (originZ>>2)-4, (originZ>>2)+7
			for qy := minQuartY; qy <= maxQuartY; qy++ {
				for qz := minQuartZ; qz <= maxQuartZ; qz++ {
					for qx := minQuartX; qx <= maxQuartX; qx++ {
						name := normalizeJavaResource(f.source.JavaNoiseBiomeAt(qx, qy, qz))
						if _, ok := f.biomes[name]; !ok {
							panic("overworld: Java noise biome source returned unknown biome " + name)
						}
						possible[name] = struct{}{}
					}
				}
			}
			generationSteps := max(len(f.featureRanks), 11)
			for step := 0; step < generationSteps; step++ {
				structureRandom := javaFeatureRandom(f.seed, originX, originZ, 0, step)
				env := javastructure.ProcessorEnvironment{
					BaseBlock:   func(x, y, z int32) world.Block { return region.Block(featurePos{x, y, z}) },
					JavaBiomeAt: f.source.JavaBiomeAt,
					BaseHeight:  region.Height,
					WorldSeed:   f.seed,
					Random:      &structureRandom,
					PlaceFeature: func(feature any, pos [3]int32, random *java26.Random) bool {
						return region.PlacePlacedFeature(feature, pos, random)
					},
				}
				f.source.PlaceStructureStep(world.ChunkPos{originChunkX, originChunkZ}, step, env, func(placement javastructure.Placement) { f.applyStructurePlacement(region, placement) })
				if step >= len(f.featureRanks) {
					continue
				}
				rankSets := f.featureRanks[step]
				ranks := rankSets[0]
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
					random := javaFeatureRandom(f.seed, originX, originZ, ranks[name], step)
					f.placeFeature(region, originX, originZ, name, random)
				}
			}
		}
	}
	return result
}

func javaFeatureRandom(seed int64, chunkX, chunkZ int32, index, stage int) java26.Random {
	r := java26.NewXoroshiroRandom(seed)
	xScale, zScale := r.NextLong()|1, r.NextLong()|1
	decorationSeed := int64(chunkX)*xScale + int64(chunkZ)*zScale ^ seed
	return java26.NewXoroshiroRandom(decorationSeed + int64(index) + int64(10000*stage))
}

func (f *ModernFeatures) placeFeature(region *featureRegion, originX, originZ int32, name string, random java26.Random) bool {
	placed, ok := f.placed[name]
	if !ok {
		panic("overworld: missing Java placed feature " + name)
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

func featureStateBlock(value any) (javastructure.Block, bool) {
	if name, ok := value.(string); ok {
		if !strings.Contains(name, ":") {
			name = "minecraft:" + name
		}
		return featureJavaBlock(world.BlockState{Name: name}), true
	}
	state, ok := value.(map[string]any)
	if !ok {
		return javastructure.Block{}, false
	}
	name, ok := state["id"].(string)
	if !ok {
		return javastructure.Block{}, false
	}
	var properties map[string]any
	if raw, exists := state["properties"]; exists {
		values, ok := raw.(map[string]any)
		if !ok {
			panic("overworld: malformed Java block-state provider")
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
	return featureJavaBlock(world.BlockState{Name: name, Properties: properties}), true
}

func featureJavaBlock(state world.BlockState) javastructure.Block {
	actor, ok, err := javastructure.NativeBlockForJavaState(state)
	if err != nil {
		panic(err)
	}
	placement := javastructure.Block{State: state}
	if ok {
		placement.NativeBlock = actor
	}
	return placement
}

func sampleJavaBlockState(region *featureRegion, p featurePos, provider any, r *java26.Random) (javastructure.Block, bool) {
	provider = resolveJavaStateProvider(provider)
	state, ok := provider.(map[string]any)
	if !ok {
		panic("overworld: malformed Java block state provider")
	}
	if typeName, ok := state["type"].(string); ok {
		switch normalizeJavaResource(typeName) {
		case "weighted":
			entries, ok := state["entries"].([]any)
			if !ok {
				panic("overworld: malformed weighted state provider")
			}
			total := int32(0)
			for _, raw := range entries {
				entry := raw.(map[string]any)
				total += int32(featureNumber(entry["weight"]))
			}
			if total <= 0 {
				panic("overworld: empty weighted block state provider")
			}
			n := r.NextInt(total)
			for _, raw := range entries {
				entry := raw.(map[string]any)
				weight := int32(featureNumber(entry["weight"]))
				if n < weight {
					return featureStateBlock(entry["data"])
				}
				n -= weight
			}
			panic("overworld: weighted block state selection escaped its total")
		case "random_block":
			raw, ok := state["blocks"].(string)
			if !ok {
				panic("overworld: malformed random_block provider")
			}
			blocks := []string{}
			if strings.HasPrefix(raw, "#") {
				blocks = javaBlockTagNames(normalizeJavaResource(strings.TrimPrefix(raw, "#")))
			} else {
				blocks = []string{normalizeJavaResource(raw)}
			}
			if len(blocks) == 0 {
				return javastructure.Block{}, false
			}
			return featureStateBlock(blocks[r.NextInt(int32(len(blocks)))])
		case "rotated":
			block, ok := sampleJavaBlockState(region, p, state["state"], r)
			if !ok {
				return javastructure.Block{}, false
			}
			properties := cloneFeatureProperties(block.State.Properties)
			direction, explicit := state["direction"].(string)
			if !explicit {
				direction = []string{"north", "east", "south", "west"}[r.NextInt(4)]
			}
			if _, ok := properties["facing"]; ok {
				properties["facing"] = direction
			}
			block.State.Properties = properties
			return featureJavaBlock(block.State), true
		case "randomized_int":
			property, ok := state["property"].(string)
			if !ok {
				panic("overworld: malformed randomized_int provider")
			}
			block, ok := sampleJavaBlockState(region, p, state["source"], r)
			if !ok {
				return javastructure.Block{}, false
			}
			properties := javadata.BlockPropertiesWithDefaults(block.State.Name, block.State.Properties)
			if _, integer := properties[property].(int32); !integer {
				return block, true
			}
			if len(properties) == len(block.State.Properties) {
				properties = cloneFeatureProperties(properties)
			}
			properties[property] = sampleJavaInt(state["values"], r)
			block.State.Properties = properties
			return featureJavaBlock(block.State), true
		case "noise", "noise_threshold", "dual_noise":
			return sampleJavaNoiseStateProvider(p, state, r)
		case "rule_based":
			if sampled, ok := sampleJavaRuleState(region, p, state, r); ok {
				return sampled, true
			}
			name, properties := region.State(p)
			return featureJavaBlock(world.BlockState{Name: name, Properties: properties}), true
		default:
			panic("overworld: unsupported Java block state provider " + normalizeJavaResource(typeName))
		}
	}
	return featureStateBlock(state)
}

func sampleOptionalJavaBlockState(region *featureRegion, p featurePos, provider any, r *java26.Random) (javastructure.Block, bool) {
	provider = resolveJavaStateProvider(provider)
	if state, ok := provider.(map[string]any); ok {
		if kind, _ := state["type"].(string); normalizeJavaResource(kind) == "rule_based" {
			return sampleJavaRuleState(region, p, state, r)
		}
	}
	return sampleJavaBlockState(region, p, provider, r)
}

func sampleJavaRuleState(region *featureRegion, p featurePos, state map[string]any, r *java26.Random) (javastructure.Block, bool) {
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
			if sampled, ok := sampleOptionalJavaBlockState(region, p, then, r); ok {
				return sampled, true
			}
		}
	}
	if fallback, exists := state["fallback"]; exists {
		return sampleOptionalJavaBlockState(region, p, fallback, r)
	}
	return javastructure.Block{}, false
}

func cloneFeatureProperties(properties map[string]any) map[string]any {
	clone := make(map[string]any, len(properties))
	for key, value := range properties {
		clone[key] = value
	}
	return clone
}

func javaBlockTagNames(tag string) []string {
	names, err := javadata.BlockTagMembers(tag)
	if err != nil {
		panic(err)
	}
	for i := range names {
		names[i] = normalizeJavaResource(names[i])
	}
	return names
}
func requireJavaJSON(kind, name string) map[string]any {
	data, err := javadata.LoadJSON(kind, name)
	if err != nil {
		panic(err)
	}
	return data
}

func requireJavaResourceNames(kind string) []string {
	names, err := javadata.ResourceNames(kind)
	if err != nil {
		panic(err)
	}
	return names
}

// Java holder-set codecs accept either one reference or an ordered list.
func javaResourceReferences(raw any) ([]string, error) {
	switch values := raw.(type) {
	case string:
		return []string{normalizeJavaResource(values)}, nil
	case []any:
		names := make([]string, len(values))
		for i, value := range values {
			name, ok := value.(string)
			if !ok {
				return nil, fmt.Errorf("resource reference %d is not a string", i)
			}
			names[i] = normalizeJavaResource(name)
		}
		return names, nil
	default:
		return nil, fmt.Errorf("resource references must be a string or list, got %T", raw)
	}
}

func normalizeJavaResource(name string) string {
	return strings.TrimPrefix(name, "minecraft:")
}

func featureNumber(v any) int {
	n, ok := v.(float64)
	if !ok || n > 100000 || n < -100000 {
		panic(fmt.Sprintf("overworld: invalid Java feature number %v", v))
	}
	return int(n)
}

var biomeInfoNoise = func() *java26.SimplexNoise {
	r := java26.NewLegacyRandom(2345)
	return java26.NewSimplexNoiseWithoutOffset(&r)
}()

func sampleJavaHeight(provider map[string]any, r *java26.Random, worldMinY, worldMaxY, seaLevel int32) int32 {
	typeName, ok := provider["type"].(string)
	if !ok {
		panic("overworld: Java height provider has no type")
	}
	bound := func(v any) int32 { return int32(featureBound(v, worldMinY, worldMaxY, seaLevel)) }
	minY, maxY := bound(provider["min_inclusive"]), bound(provider["max_inclusive"])
	if maxY < minY {
		panic("overworld: inverted Java height provider")
	}
	switch normalizeJavaResource(typeName) {
	case "uniform":
		return minY + r.NextInt(maxY-minY+1)
	case "trapezoid":
		width := maxY - minY + 1
		a, b := r.NextInt(width), r.NextInt(width)
		return minY + (a+b)/2
	case "biased_to_bottom":
		return minY + int32(r.NextInt(r.NextInt(maxY-minY+1)+1))
	case "very_biased_to_bottom":
		return minY + int32(r.NextInt(r.NextInt(r.NextInt(maxY-minY+1)+1)+1))
	default:
		panic("overworld: unsupported Java height provider " + normalizeJavaResource(typeName))
	}
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

func javaBlockTagContains(tag, blockName string) bool {
	matches, err := javadata.BlockTagContains(tag, blockName)
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

func orePredicateMatches(p featurePos, predicate map[string]any, blockName string) bool {
	typeName, _ := predicate["predicate_type"].(string)
	switch normalizeJavaResource(typeName) {
	case "tag_match":
		tag, _ := predicate["tag"].(string)
		return javaBlockTagContains(normalizeJavaResource(tag), normalizeJavaResource(blockName))
	case "height_match":
		return p.y >= int32(featureNumber(predicate["min_inclusive"])) && p.y <= int32(featureNumber(predicate["max_inclusive"]))
	case "all_of":
		rules := predicate["rules"].([]any)
		for _, v := range rules {
			if !orePredicateMatches(p, v.(map[string]any), blockName) {
				return false
			}
		}
		return true
	case "any_of":
		rules := predicate["rules"].([]any)
		for _, v := range rules {
			if orePredicateMatches(p, v.(map[string]any), blockName) {
				return true
			}
		}
		return false
	case "not":
		return !orePredicateMatches(p, predicate["rule"].(map[string]any), blockName)
	case "block_match":
		name, _ := predicate["block"].(string)
		return normalizeJavaResource(name) == normalizeJavaResource(blockName)
	default:
		panic("overworld: unsupported Java ore target predicate " + normalizeJavaResource(typeName))
	}
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
	panic(fmt.Sprintf("overworld: malformed Java height bound %v", v))
}

// applyStructurePlacement adapts Java descriptors, applies allowed blocks, and publishes target metadata.
func (f *ModernFeatures) applyStructurePlacement(region *featureRegion, placement javastructure.Placement) {
	sourceBlockCount := len(placement.Blocks)
	payloadBlocks, written := applyTemplateFluidsForStructureWrites(
		region, placement.Blocks, placement.StructurePieceWrites, templatePlacementFlags(placement),
	)
	for _, entry := range payloadBlocks {
		region.recordTemplateActor(entry)
	}
	for _, tick := range placement.ScheduledTicks {
		region.reserveJavaTick(tick)
	}
	if placement.TemplateUpdateFlags != 0 && !placement.TemplateKnownShape {
		world.UpdateJavaTemplateShapes(region, written, placement.TemplateUpdateFlags)
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
		if featureInsideTarget(region.target, featurePos{int32(pos[0]), int32(pos[1]), int32(pos[2])}) {
			region.result.MarkForPostProcessing(pos, region.c.Range())
		}
	}
	entities := make([]javastructure.Entity, 0, len(placement.Entities))
	for _, record := range placement.Entities {
		x, z := int32(math.Floor(record.Pos[0])), int32(math.Floor(record.Pos[2]))
		if x>>4 == int32(region.target[0]) && z>>4 == int32(region.target[1]) {
			entities = append(entities, record)
		}
	}
	if len(entities) != 0 {
		handles, err := (javastructure.Placement{Entities: entities}).NewEntityHandles(entity.NewFromJavaNBT)
		if err != nil {
			panic(err)
		}
		region.result.Entities = append(region.result.Entities, handles...)
	}
}

func templatePlacementFlags(placement javastructure.Placement) int {
	if placement.TemplateUpdateFlags != 0 {
		return placement.TemplateUpdateFlags
	}
	return 3
}
