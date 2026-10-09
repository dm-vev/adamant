package overworld

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"

	java26 "github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/internal/javadata"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/javastructure"
)

// modernStructures plans structure starts from Java structure-set assets. Prepared starts are immutable;
// density and placement therefore never depend on chunk generation order.
type modernStructures struct {
	seed           int64
	source         ModernFeatureSource
	sets           []modernStructureSet
	rings          map[string]map[[2]int32]struct{}
	structureSteps map[int][]modernStructureStepEntry
	startCache     map[modernStructureStartKey]modernStructureStartResult
	startFlights   map[modernStructureStartKey]chan modernStructureStartResult
	startCacheMu   sync.Mutex
}

type modernStructureStepEntry struct {
	name  string
	index int32
}

type modernStructureSet struct {
	name       string
	placement  map[string]any
	structures []modernStructureEntry
}
type modernStructureEntry struct {
	name   string
	weight int32
}

type modernStructurePiece interface {
	Bounds() javastructure.Box
	Place(clip javastructure.Bounds, env javastructure.ProcessorEnvironment) (javastructure.Placement, error)
}

type modernJigsawPiece struct {
	piece   javastructure.JigsawPiece
	palette int
	seed    int64
}

func (p modernJigsawPiece) Bounds() javastructure.Box { return p.piece.Bounds }
func (p modernJigsawPiece) Place(clip javastructure.Bounds, env javastructure.ProcessorEnvironment) (javastructure.Placement, error) {
	return p.piece.PlaceWithEnvironment(p.palette, clip, p.seed, env)
}

type modernStructureStart struct {
	name      string
	step      int
	pieces    []modernStructurePiece
	rigid     []javastructure.RigidPiece
	junctions []javastructure.Junction
	box       javastructure.Box
	terrain   javastructure.TerrainAdjustment
}

type modernStructureBuildInput struct {
	seed           int64
	name           string
	chunkX, chunkZ int32
	config         map[string]any
	source         ModernFeatureSource
	random         *java26.Random
}

type modernStructureBuilder func(modernStructureBuildInput) (modernStructureStart, bool, error)

var modernStructureBuilders = map[string]modernStructureBuilder{
	"jigsaw": buildModernJigsawStructure,
}

var javaStructureRegistryOrder = []string{
	"abandoned_camp_dappled_forest", "mineshaft", "igloo", "abandoned_camp_old_growth_birch_forest",
	"abandoned_camp_meadow", "ruined_portal_desert", "abandoned_camp_forest", "ancient_city",
	"abandoned_camp_pale_garden", "swamp_hut", "fortress", "stronghold", "ruined_portal_ocean",
	"shipwreck", "ocean_ruin_warm", "abandoned_camp_taiga", "shipwreck_beached",
	"abandoned_camp_swamp", "ruined_portal_swamp", "abandoned_camp_snowy_taiga",
	"ruined_portal_jungle", "nether_fossil", "abandoned_camp_cherry_grove", "desert_pyramid",
	"mineshaft_mesa", "abandoned_camp_old_growth_spruce_taiga", "monument", "end_city", "mansion",
	"village_desert", "village_plains", "ruined_portal_nether", "abandoned_camp_bamboo_jungle",
	"abandoned_camp_flower_forest", "village_snowy", "abandoned_camp_wooded_badlands",
	"bastion_remnant", "ocean_ruin_cold", "pillager_outpost", "buried_treasure",
	"abandoned_camp_windswept_forest", "ruined_portal", "jungle_pyramid", "trial_chambers",
	"abandoned_camp_birch_forest", "village_taiga", "village_savanna", "ruined_portal_mountain",
	"abandoned_camp_sparse_jungle", "trail_ruins", "abandoned_camp_savanna",
	"abandoned_camp_old_growth_pine_taiga",
}

var javaDecorationStepOrdinals = map[string]int{
	"raw_generation": 0, "lakes": 1, "local_modifications": 2, "underground_structures": 3,
	"surface_structures": 4, "strongholds": 5, "underground_ores": 6,
	"underground_decoration": 7, "fluid_springs": 8, "vegetal_decoration": 9,
	"top_layer_modification": 10,
}

type modernStructureContext struct {
	seed           int64
	starts         []modernStructureStart
	structureSteps map[int][]modernStructureStepEntry
	beardifier     *javastructure.Beardifier
	environment    javastructure.ProcessorEnvironment
}

func newModernStructures(seed int64, source ModernFeatureSource) *modernStructures {
	if source == nil {
		panic("overworld: nil Java structure source")
	}
	s := &modernStructures{
		seed:           seed,
		source:         source,
		rings:          make(map[string]map[[2]int32]struct{}),
		structureSteps: make(map[int][]modernStructureStepEntry),
		startCache:     make(map[modernStructureStartKey]modernStructureStartResult),
		startFlights:   make(map[modernStructureStartKey]chan modernStructureStartResult),
	}
	names, err := javadata.ResourceNames("structure_set")
	if err != nil {
		panic(err)
	}
	for _, name := range names {
		data, err := javadata.LoadJSON("structure_set", name)
		if err != nil {
			panic(err)
		}
		placement, ok := data["placement"].(map[string]any)
		if !ok {
			panic("overworld: malformed Java structure set placement " + name)
		}
		entries, ok := data["structures"].([]any)
		if !ok {
			panic("overworld: malformed Java structure set structures " + name)
		}
		set := modernStructureSet{name: name, placement: placement}
		for _, raw := range entries {
			entry, ok := raw.(map[string]any)
			if !ok {
				panic("overworld: malformed Java structure entry " + name)
			}
			structure, ok := entry["structure"].(string)
			if !ok {
				panic("overworld: missing Java structure id " + name)
			}
			weight := int32(1)
			if value, ok := entry["weight"].(float64); ok {
				weight = int32(value)
			}
			set.structures = append(set.structures, modernStructureEntry{
				name:   normalizeJavaResource(structure),
				weight: weight,
			})
		}
		s.sets = append(s.sets, set)
		if normalizeJavaResource(stringValue(placement["type"])) == "concentric_rings" {
			s.rings[name] = s.concentricRingChunks(placement)
		}
	}
	s.loadStructureSteps()
	return s
}

func (s *modernStructures) loadStructureSteps() {
	names, err := javadata.ResourceNames("structure")
	if err != nil {
		panic(err)
	}
	available := make(map[string]struct{}, len(names))
	for _, name := range names {
		available[name] = struct{}{}
	}
	for _, name := range javaStructureRegistryOrder {
		if _, ok := available[name]; !ok {
			panic("overworld: missing Java structure registry entry " + name)
		}
		delete(available, name)
		config, err := javadata.LoadJSON("structure", name)
		if err != nil {
			panic(err)
		}
		stepName := normalizeJavaResource(stringValue(config["step"]))
		step, ok := javaDecorationStepOrdinals[stepName]
		if !ok {
			panic("overworld: unknown Java structure decoration step " + stepName)
		}
		index := int32(len(s.structureSteps[step]))
		s.structureSteps[step] = append(s.structureSteps[step], modernStructureStepEntry{name: "minecraft:" + name, index: index})
	}
	if len(available) != 0 {
		missing := make([]string, 0, len(available))
		for name := range available {
			missing = append(missing, name)
		}
		sort.Strings(missing)
		panic(fmt.Sprintf("overworld: Java structure registry order omitted entries %v", missing))
	}
}

func (s *modernStructures) concentricRingChunks(placement map[string]any) map[[2]int32]struct{} {
	count := int(number(placement["count"]))
	distance := int(number(placement["distance"]))
	spread := int(number(placement["spread"]))
	out := make(map[[2]int32]struct{}, count)
	if count <= 0 {
		return out
	}
	random := java26.NewLegacyRandom(s.seed)
	angle := random.NextDouble() * 2 * math.Pi
	positionInCircle, circle := 0, 0
	preferredBiomes := stringValue(placement["preferred_biomes"])
	for index := range count {
		dist := float64(4*distance+distance*circle*6) + (random.NextDouble()-0.5)*float64(distance)*2.5
		x := javaRound(math.Cos(angle) * dist)
		z := javaRound(math.Sin(angle) * dist)
		biomeRandom := java26.NewLegacyRandom(random.NextLong())
		if preferredBiomes != "" {
			if located, ok := s.locatePreferredBiome(x, z, preferredBiomes, &biomeRandom); ok {
				x, z = located[0], located[1]
			}
		}
		out[[2]int32{x, z}] = struct{}{}
		angle += 2 * math.Pi / float64(spread)
		positionInCircle++
		if positionInCircle == spread {
			circle++
			positionInCircle = 0
			spread += 2 * spread / (circle + 1)
			if spread > count-index {
				spread = count - index
			}
			angle += random.NextDouble() * 2 * math.Pi
		}
	}
	return out
}
func (s *modernStructures) locatePreferredBiome(chunkX, chunkZ int32, preferredBiomes string, random *java26.Random) ([2]int32, bool) {
	centerX, centerZ := chunkX*16+8, chunkZ*16+8
	quartX, quartZ := centerX>>2, centerZ>>2
	const radius int32 = 28
	found := int32(0)
	var selected [2]int32
	matches := make(map[string]bool)
	for dz := -radius; dz <= radius; dz++ {
		for dx := -radius; dx <= radius; dx++ {
			biome := normalizeJavaResource(s.source.JavaNoiseBiomeAt(quartX+dx, 0, quartZ+dz))
			if !strings.Contains(biome, ":") {
				biome = "minecraft:" + biome
			}
			allowed, known := matches[biome]
			if !known {
				var err error
				allowed, err = javadata.BiomeTagContains(preferredBiomes, biome)
				if err != nil {
					panic(err)
				}
				matches[biome] = allowed
			}
			if !allowed {
				continue
			}
			if found == 0 || random.NextInt(found+1) == 0 {
				selected = [2]int32{(quartX + dx) >> 2, (quartZ + dz) >> 2}
			}
			found++
		}
	}
	return selected, found != 0
}

func (s *modernStructures) Prepare(pos world.ChunkPos) *modernStructureContext {
	ctx := &modernStructureContext{seed: s.seed, structureSteps: s.structureSteps, environment: s.sourceEnvironment()}
	for dz := int32(-8); dz <= 8; dz++ {
		for dx := int32(-8); dx <= 8; dx++ {
			startX, startZ := int32(pos[0])+dx, int32(pos[1])+dz
			for _, set := range s.sets {
				candidate, ok := s.candidate(set, startX, startZ)
				if !ok {
					continue
				}
				start, ok := s.makeStart(set, candidate[0], candidate[1])
				if !ok {
					continue
				}
				if intersectsChunk(start.box, pos) {
					ctx.starts = append(ctx.starts, start)
				}
			}
		}
	}
	pieces := make([]javastructure.RigidPiece, 0)
	junctions := make([]javastructure.Junction, 0)
	for _, start := range ctx.starts {
		pieces = append(pieces, start.rigid...)
		junctions = append(junctions, start.junctions...)
	}
	ctx.beardifier = javastructure.NewBeardifier(pieces, junctions)
	return ctx
}

func (s *modernStructures) candidate(set modernStructureSet, x, z int32) ([2]int32, bool) {
	placement := set.placement
	kind := normalizeJavaResource(stringValue(placement["type"]))
	if kind == "concentric_rings" {
		_, ok := s.rings[set.name][[2]int32{x, z}]
		return [2]int32{x, z}, ok
	}
	if kind != "random_spread" {
		return [2]int32{}, false
	}
	spacing := int32(number(placement["spacing"]))
	separation := int32(number(placement["separation"]))
	salt := int32(number(placement["salt"]))
	distribution := javastructure.Linear
	if normalizeJavaResource(stringValue(placement["spread_type"])) == "triangular" {
		distribution = javastructure.Triangular
	}
	candidate, err := javastructure.PotentialChunk(s.seed, x, z, salt, spacing, separation, distribution)
	if err != nil {
		panic(fmt.Sprintf("overworld: structure set %s: %v", set.name, err))
	}
	if candidate[0] != x || candidate[1] != z {
		return [2]int32{}, false
	}
	probability := float32(number(placement["frequency"]))
	method := frequencyMethod(stringValue(placement["frequency_reduction_method"]))
	if probability != 0 && !javastructure.FrequencyApplies(s.seed, salt, x, z, probability, method) {
		return [2]int32{}, false
	}
	return candidate, true
}

func registerModernStructureBuilder(kind string, builder modernStructureBuilder) {
	if kind == "" || builder == nil {
		panic("overworld: invalid modern structure builder")
	}
	if _, exists := modernStructureBuilders[kind]; exists {
		panic("overworld: duplicate modern structure builder " + kind)
	}
	modernStructureBuilders[kind] = builder
}

func (s *modernStructures) makeStart(set modernStructureSet, cx, cz int32) (modernStructureStart, bool) {
	key := modernStructureStartKey{set: set.name, x: cx, z: cz}
	s.startCacheMu.Lock()
	if result, ok := s.startCache[key]; ok {
		s.startCacheMu.Unlock()
		if result.err != nil {
			panic(result.err)
		}
		return result.start, result.valid
	}
	if flight, ok := s.startFlights[key]; ok {
		s.startCacheMu.Unlock()
		result := <-flight
		if result.err != nil {
			panic(result.err)
		}
		return result.start, result.valid
	}
	flight := make(chan modernStructureStartResult, 1)
	s.startFlights[key] = flight
	s.startCacheMu.Unlock()

	start, valid, err := s.buildStart(set, cx, cz)
	result := modernStructureStartResult{start: start, valid: valid, err: err}
	s.startCacheMu.Lock()
	if len(s.startCache) >= 4096 {
		for old := range s.startCache {
			delete(s.startCache, old)
			break
		}
	}
	s.startCache[key] = result
	delete(s.startFlights, key)
	flight <- result
	close(flight)
	s.startCacheMu.Unlock()
	if err != nil {
		panic(err)
	}
	return start, valid
}

type modernStructureStartKey struct {
	set string
	x   int32
	z   int32
}

type modernStructureStartResult struct {
	start modernStructureStart
	valid bool
	err   error
}

func (s *modernStructures) buildStart(set modernStructureSet, cx, cz int32) (start modernStructureStart, valid bool, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			start = modernStructureStart{}
			valid = false
			err = fmt.Errorf("overworld: Java structure build at %d,%d failed: %v", cx, cz, recovered)
		}
	}()
	random := largeFeatureRandom(s.seed, cx, cz)
	entry := chooseModernStructure(set.structures, &random)
	if entry.name == "" {
		return modernStructureStart{}, false, nil
	}
	config, err := javadata.LoadJSON("structure", strings.TrimPrefix(entry.name, "minecraft:"))
	if err != nil {
		return modernStructureStart{}, false, err
	}
	kind := normalizeJavaResource(stringValue(config["type"]))
	builder, ok := modernStructureBuilders[kind]
	if !ok {
		biome := normalizeJavaResource(s.source.JavaNoiseBiomeAt(cx*4+2, 0, cz*4+2))
		if !strings.Contains(biome, ":") {
			biome = "minecraft:" + biome
		}
		allowed, err := javadata.BiomeTagContains(stringValue(config["biomes"]), biome)
		if err != nil {
			return modernStructureStart{}, false, err
		}
		if !allowed {
			return modernStructureStart{}, false, nil
		}
		return modernStructureStart{}, false, fmt.Errorf("overworld: Java structure %q has unsupported type %q", entry.name, kind)
	}
	start, valid, err = builder(modernStructureBuildInput{
		seed: s.seed, name: entry.name, chunkX: cx, chunkZ: cz,
		config: config, source: s.source, random: &random,
	})
	if err != nil {
		return modernStructureStart{}, false, fmt.Errorf("overworld: build Java structure %s at %d,%d: %w", entry.name, cx, cz, err)
	}
	if valid {
		stepName := normalizeJavaResource(stringValue(config["step"]))
		step, ok := javaDecorationStepOrdinals[stepName]
		if !ok {
			return modernStructureStart{}, false, fmt.Errorf("overworld: unknown Java structure decoration step %q", stepName)
		}
		start.name, start.step = entry.name, step
	}
	return start, valid, nil
}

// PlaceStep emits the starts belonging to one Java decoration step in registry order.
func (c *modernStructureContext) PlaceStep(pos world.ChunkPos, bounds cube.Range, step int, env javastructure.ProcessorEnvironment, emit func(javastructure.Placement)) {
	if emit == nil {
		panic("overworld: nil Java structure placement emitter")
	}
	minX, minZ := int32(pos[0])*16, int32(pos[1])*16
	clip := javastructure.Bounds{
		Min: [3]int32{minX, int32(bounds[0]), minZ},
		Max: [3]int32{minX + 15, int32(bounds[1]), minZ + 15},
	}
	env.WorldSeed = c.seed
	env.SkipEntities = false
	for _, entry := range c.structureSteps[step] {
		random := java26.NewXoroshiroRandom(c.seed)
		decorationSeed := random.SetDecorationSeed(c.seed, minX, minZ)
		random.SetFeatureSeed(decorationSeed, entry.index, int32(step))
		env.Random = &random
		for _, start := range c.starts {
			if start.name != entry.name || start.step != step {
				continue
			}
			for _, piece := range start.pieces {
				placement, err := piece.Place(clip, env)
				if err != nil {
					panic(err)
				}
				placement.StructurePieceWrites = structurePieceUsesSetBlock(piece)
				if _, template := piece.(modernJigsawPiece); template {
					placement.TemplateUpdateFlags, placement.TemplateKnownShape = 18, true
				} else if isTemplateStructurePiece(piece) {
					placement.TemplateUpdateFlags, placement.TemplateKnownShape = 2, false
				}
				emit(placement)
			}
		}
	}
}

func isTemplateStructurePiece(piece modernStructurePiece) bool {
	switch piece.(type) {
	case modernIglooPiece, modernMansionPiece, modernOceanRuinPiece, modernRuinedPortal, modernShipwreckPiece, *modernShipwreckPiece:
		return true
	default:
		return false
	}
}

func structurePieceUsesSetBlock(piece modernStructurePiece) bool {
	switch piece.(type) {
	case modernSwampHutPiece, modernJungleTemple, minePiece, modernOceanMonumentPiece, modernDesertPyramid, strongholdPiece, modernBuriedTreasurePiece:
		return true
	default:
		return false
	}
}

func buildModernJigsawStructure(input modernStructureBuildInput) (modernStructureStart, bool, error) {
	config := input.config
	pool := strings.TrimPrefix(stringValue(config["start_pool"]), "minecraft:")
	if pool == "" {
		return modernStructureStart{}, false, fmt.Errorf("jigsaw structure has no start_pool")
	}
	originX, originZ := input.chunkX*16, input.chunkZ*16
	startHeight, ok := config["start_height"].(map[string]any)
	if !ok {
		return modernStructureStart{}, false, fmt.Errorf("jigsaw structure has malformed start_height")
	}
	heightRange := world.Overworld.Range()
	originY := sampleJavaHeight(startHeight, input.random, int32(heightRange.Min()), int32(heightRange.Max())+1, input.source.SeaLevel())
	terrain := terrainAdjustment(stringValue(config["terrain_adaptation"]))
	liquidSettings, err := javastructure.ParseLiquidSettings(config["liquid_settings"])
	if err != nil {
		return modernStructureStart{}, false, err
	}
	options := javastructure.JigsawOptions{
		Pool:                    pool,
		StartJigsaw:             strings.TrimPrefix(stringValue(config["start_jigsaw_name"]), "minecraft:"),
		Origin:                  [3]int32{originX, originY, originZ},
		MaxDepth:                int(number(config["size"])),
		MaxDistance:             int32(number(config["max_distance_from_center"])),
		ExpansionHack:           config["use_expansion_hack"] == true,
		Seed:                    input.seed,
		Terrain:                 terrain,
		ProjectStartToHeightmap: config["project_start_to_heightmap"] != nil,
		HeightAt:                func(x, z int32) int32 { return input.source.BaseHeight(x, z, false) },
		LiquidSettings:          liquidSettings,
	}
	result, err := javastructure.BuildJigsaw(options, input.random)
	if err != nil {
		return modernStructureStart{}, false, err
	}
	if len(result.Pieces) == 0 {
		return modernStructureStart{}, false, nil
	}
	pieces := make([]modernStructurePiece, len(result.Pieces))
	for i, piece := range result.Pieces {
		pieces[i] = modernJigsawPiece{piece: piece, palette: 0, seed: input.seed}
	}
	start := modernStructureStart{
		name: input.name, pieces: pieces, rigid: result.RigidPieces,
		junctions: result.Junctions, terrain: terrain,
		box: emptyStructureBox(),
	}
	root := result.Pieces[0]
	centerX := (root.Bounds.Min[0] + root.Bounds.Max[0]) / 2
	centerZ := (root.Bounds.Min[2] + root.Bounds.Max[2]) / 2
	biome := normalizeJavaResource(input.source.JavaNoiseBiomeAt(centerX>>2, (root.Bounds.Min[1]+root.GroundLevelDelta)>>2, centerZ>>2))
	if !strings.Contains(biome, ":") {
		biome = "minecraft:" + biome
	}
	allowed, err := javadata.BiomeTagContains(stringValue(config["biomes"]), biome)
	if err != nil {
		return modernStructureStart{}, false, err
	}
	if !allowed {
		return modernStructureStart{}, false, nil
	}
	for _, piece := range result.Pieces {
		start.box = unionStructureBox(start.box, piece.Bounds)
	}
	return start, true, nil
}

func (c *modernStructureContext) AdjustDensity(x, y, z int32) float32 {
	return c.beardifier.AdjustDensity(x, y, z)
}

func (c *modernStructureContext) PlacePreview(pos world.ChunkPos, ch *chunk.Chunk) *modernStructurePayloads {
	return c.place(pos, ch)
}

func (c *modernStructureContext) Place(pos world.ChunkPos, ch *chunk.Chunk) world.GenerationResult {
	return c.PlacePreview(pos, ch).GenerationData()
}

type modernStructurePayloads struct {
	blockEntities []chunk.BlockEntity
	entities      []javastructure.Entity
}

func (p *modernStructurePayloads) GenerationData() world.GenerationResult {
	if p == nil {
		return world.GenerationResult{}
	}
	result := world.GenerationResult{
		BlockEntities: make([]chunk.BlockEntity, len(p.blockEntities)),
	}
	for index, payload := range p.blockEntities {
		result.BlockEntities[index] = chunk.BlockEntity{
			Pos:  payload.Pos,
			Data: cloneStructureNBT(payload.Data),
		}
	}
	entities := make([]javastructure.Entity, len(p.entities))
	for index, raw := range p.entities {
		entities[index] = raw
		entities[index].NBT = cloneStructureNBT(raw.NBT)
	}
	handles, err := (javastructure.Placement{Entities: entities}).NewEntityHandles(entity.NewFromJavaNBT)
	if err != nil {
		panic(err)
	}
	result.Entities = handles
	return result
}

func (c *modernStructureContext) place(pos world.ChunkPos, ch *chunk.Chunk) *modernStructurePayloads {
	payloads := &modernStructurePayloads{}
	minY, maxY := int32(ch.Range()[0]), int32(ch.Range()[1])
	clip := javastructure.Bounds{
		Min: [3]int32{int32(pos[0]) * 16, minY, int32(pos[1]) * 16},
		Max: [3]int32{int32(pos[0])*16 + 15, maxY, int32(pos[1])*16 + 15},
	}
	registry := ch.BlockRegistry()
	environment := c.environment
	environment.SkipEntities = false
	for _, start := range c.starts {
		for _, piece := range start.pieces {
			placement, err := piece.Place(clip, environment)
			if err != nil {
				panic(err)
			}
			placement, err = placement.AdaptItemFrameEntities()
			if err != nil {
				panic(err)
			}
			for _, placed := range placement.Blocks {
				x, y, z := uint8(placed.Pos[0]&15), int16(placed.Pos[1]), uint8(placed.Pos[2]&15)
				current := world.NativeStateAt(ch, x, y, z)
				state, err := placed.ResolveNative(registry, current)
				if err != nil {
					panic(err)
				}
				world.SetNativeState(ch, x, y, z, state)
			}
			for _, placed := range placement.PostShapeBlocks {
				x, y, z := uint8(placed.Pos[0]&15), int16(placed.Pos[1]), uint8(placed.Pos[2]&15)
				current := world.NativeStateAt(ch, x, y, z)
				state, err := placed.ResolveNative(registry, current)
				if err != nil {
					panic(err)
				}
				world.SetNativeState(ch, x, y, z, state)
			}
			payloads.blockEntities = append(payloads.blockEntities, placement.BlockEntityPayloads()...)
			payloads.blockEntities = append(payloads.blockEntities, (javastructure.Placement{Blocks: placement.PostShapeBlocks}).BlockEntityPayloads()...)
			payloads.entities = append(payloads.entities, placement.Entities...)
			payloads.entities = append(payloads.entities, placement.PostShapeEntities...)
		}
	}
	return payloads
}

func cloneStructureNBT(source map[string]any) map[string]any {
	if source == nil {
		return nil
	}
	cloned := make(map[string]any, len(source))
	for key, value := range source {
		cloned[key] = cloneStructureNBTValue(value)
	}
	return cloned
}

func cloneStructureNBTValue(value any) any {
	switch value := value.(type) {
	case map[string]any:
		return cloneStructureNBT(value)
	case []any:
		cloned := make([]any, len(value))
		for index, element := range value {
			cloned[index] = cloneStructureNBTValue(element)
		}
		return cloned
	case []byte:
		return append([]byte(nil), value...)
	case []int8:
		return append([]int8(nil), value...)
	case []int16:
		return append([]int16(nil), value...)
	case []int32:
		return append([]int32(nil), value...)
	case []int64:
		return append([]int64(nil), value...)
	case []float32:
		return append([]float32(nil), value...)
	case []float64:
		return append([]float64(nil), value...)
	case []string:
		return append([]string(nil), value...)
	default:
		return value
	}
}

func (s *modernStructures) sourceEnvironment() javastructure.ProcessorEnvironment {
	return javastructure.ProcessorEnvironment{
		BaseBlock: s.source.BaseBlock,
		JavaBiomeAt: func(x, y, z int32) string {
			return "minecraft:" + normalizeJavaResource(s.source.JavaBiomeAt(x, y, z))
		},
		BaseHeight: s.source.BaseHeight,
	}
}
func intersectsChunk(box javastructure.Box, pos world.ChunkPos) bool {
	minX, minZ := int32(pos[0])*16, int32(pos[1])*16
	maxX, maxZ := minX+15, minZ+15
	return box.Max[0] >= minX && box.Min[0] <= maxX && box.Max[2] >= minZ && box.Min[2] <= maxZ
}

func emptyStructureBox() javastructure.Box {
	return javastructure.Box{
		Min: [3]int32{math.MaxInt32, math.MaxInt32, math.MaxInt32},
		Max: [3]int32{math.MinInt32, math.MinInt32, math.MinInt32},
	}
}

func unionStructureBox(a, b javastructure.Box) javastructure.Box {
	for axis := range 3 {
		if b.Min[axis] < a.Min[axis] {
			a.Min[axis] = b.Min[axis]
		}
		if b.Max[axis] > a.Max[axis] {
			a.Max[axis] = b.Max[axis]
		}
	}
	return a
}

func chooseModernStructure(entries []modernStructureEntry, random *java26.Random) modernStructureEntry {
	var total int32
	for _, entry := range entries {
		total += entry.weight
	}
	if total <= 0 {
		return modernStructureEntry{}
	}
	choice := random.NextInt(total)
	for _, entry := range entries {
		choice -= entry.weight
		if choice < 0 {
			return entry
		}
	}
	return entries[len(entries)-1]
}

func frequencyMethod(name string) javastructure.FrequencyReduction {
	switch normalizeJavaResource(name) {
	case "legacy_type_1":
		return javastructure.FrequencyLegacyType1
	case "legacy_type_2":
		return javastructure.FrequencyLegacyType2
	case "legacy_type_3":
		return javastructure.FrequencyLegacyType3
	default:
		return javastructure.FrequencyDefault
	}
}

func terrainAdjustment(name string) javastructure.TerrainAdjustment {
	switch normalizeJavaResource(name) {
	case "bury":
		return javastructure.TerrainBury
	case "beard_thin":
		return javastructure.TerrainBeardThin
	case "beard_box":
		return javastructure.TerrainBeardBox
	case "encapsulate":
		return javastructure.TerrainEncapsulate
	default:
		return javastructure.TerrainNone
	}
}

func stringValue(value any) string {
	name, _ := value.(string)
	return name
}

func number(value any) float64 {
	switch number := value.(type) {
	case float64:
		return number
	case int:
		return float64(number)
	case int32:
		return float64(number)
	case int64:
		return float64(number)
	default:
		return 0
	}
}
func javaRound(value float64) int32 {
	return int32(math.Floor(value + 0.5))
}
