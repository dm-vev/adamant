package overworld

import (
	"fmt"
	"math"
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/structure"
)

type featureTickReservation struct {
	pos      cube.Pos
	kind     chunk.ScheduledTickKind
	typeName string
}

type featureRegion struct {
	f            *ModernFeatures
	c            *chunk.Chunk
	writeOriginX int32
	writeOriginZ int32
	target       world.ChunkPos
	// hasTarget is false for a region that records the features of an
	// origin: every position is then read from the overlay and the source.
	hasTarget bool
	// recording collects the writes and side outputs of the origin step that
	// runs while it is set.
	recording *originStep
	result    *world.GenerationResult
	// overlay holds the states written to the region.
	overlay featureOverlay
	// columns holds the overlay tops and heightmap heights of the columns,
	// by featureColumnKey.
	columns map[uint64]featureColumn
	// base is the source of the region when it is the generated terrain,
	// whose immutable chunks baseChunks keeps once read.
	base                    *modernTerrainSource
	baseChunks              map[world.ChunkPos]*chunk.Chunk
	lastBasePos             world.ChunkPos
	lastBase                *chunk.Chunk
	regionRandoms           map[[2]int32]*vanilla.Random
	scheduledTicks          map[featureTickReservation]struct{}
	blockActorRecords       map[cube.Pos]structure.Block
	blockActorData          map[cube.Pos]map[string]any
	nativeProjections       map[cube.Pos]struct{}
	treeDecorationPositions *treePositionSet
}

func newFeatureRegion(f *ModernFeatures, c *chunk.Chunk, target world.ChunkPos, result *world.GenerationResult) *featureRegion {
	if f == nil || c == nil || result == nil {
		panic("overworld: invalid feature region")
	}
	var base *modernTerrainSource
	if source, ok := f.source.(modernTerrainSource); ok && source.generator != nil {
		base = &source
	}
	return &featureRegion{
		f: f, c: c, target: target, hasTarget: true, result: result, base: base,
		scheduledTicks:    make(map[featureTickReservation]struct{}),
		blockActorRecords: make(map[cube.Pos]structure.Block),
		blockActorData:    make(map[cube.Pos]map[string]any),
		nativeProjections: make(map[cube.Pos]struct{}),
		regionRandoms:     make(map[[2]int32]*vanilla.Random),
	}
}

// release returns the pooled memory of the region. The region must not be
// used afterwards.
func (r *featureRegion) release() {
	r.overlay.release()
}

// newRecordingRegion returns a region without a target chunk, which records
// what an origin's features write. registry and r describe the chunks the
// records are applied to.
func newRecordingRegion(f *ModernFeatures, registry chunk.BlockRegistry, r cube.Range) *featureRegion {
	region := newFeatureRegion(f, chunk.New(registry, r), world.ChunkPos{}, &world.GenerationResult{})
	region.hasTarget = false
	return region
}

func (r *featureRegion) SetOrigin(chunkX, chunkZ int32) {
	r.writeOriginX, r.writeOriginZ = chunkX, chunkZ
}

// inTarget reports whether p lies in the target chunk.
func (r *featureRegion) inTarget(p featurePos) bool {
	return r.hasTarget && featureInsideTarget(r.target, p)
}

// recordsAt reports whether side outputs at p are kept: those of the target
// chunk, or any of the origin being recorded.
func (r *featureRegion) recordsAt(p featurePos) bool {
	return r.inTarget(p) || r.recording != nil
}

// markPostProcessing marks pos for post-processing in the target chunk or in
// the record of the running origin.
func (r *featureRegion) markPostProcessing(pos cube.Pos) {
	if r.inTarget(featurePos{int32(pos[0]), int32(pos[1]), int32(pos[2])}) {
		r.result.MarkForPostProcessing(pos, r.c.Range())
	} else if r.recording != nil {
		r.recording.markers = append(r.recording.markers, pos)
	}
}

// addEntity adds an entity spawned at pos to the target chunk or to the
// record of the running origin, which spawns it for the chunk it lands in.
func (r *featureRegion) addEntity(pos cube.Pos, spawn func() *world.EntityHandle) {
	if r.inTarget(featurePos{int32(pos[0]), int32(pos[1]), int32(pos[2])}) {
		r.result.Entities = append(r.result.Entities, spawn())
	} else if r.recording != nil {
		r.recording.entities = append(r.recording.entities, recordedEntity{pos: pos, spawn: spawn})
	}
}

// scheduleTick schedules a generated tick in the target chunk or in the
// record of the running origin.
func (r *featureRegion) scheduleTick(tick chunk.ScheduledTick) {
	if r.inTarget(featurePos{int32(tick.Pos[0]), int32(tick.Pos[1]), int32(tick.Pos[2])}) {
		r.result.ScheduleTick(tick)
	} else if r.recording != nil {
		r.recording.ticks = append(r.recording.ticks, tick)
	}
}

func absFeatureInt(v int32) uint32 {
	if v < 0 {
		return uint32(-int64(v))
	}
	return uint32(v)
}

// State returns the Java state at p. The properties are shared with every
// other read of the same state and must not be modified: copy them first.
func (r *featureRegion) State(p featurePos) (string, map[string]any) {
	state := r.nativeState(p)
	if id := state.CanonicalID(r.c.BlockRegistry()); id != 0 {
		if name, properties, ok := gamedata.BlockStateView(id); ok {
			return name, properties
		}
	}
	canonicalState, err := world.CanonicalState(r.c.BlockRegistry(), state)
	if err != nil {
		panic(err)
	}
	return canonicalState.Name, canonicalState.Properties
}

// StateID returns the canonical Java state ID at p, or zero for a native
// state without one.
func (r *featureRegion) StateID(p featurePos) uint32 {
	return r.nativeState(p).CanonicalID(r.c.BlockRegistry())
}

// featureColumnKey packs the column x, z for the maps of a region.
func featureColumnKey(x, z int32) uint64 {
	return uint64(uint32(x)) | uint64(uint32(z))<<32
}

func (r *featureRegion) nativeState(p featurePos) world.NativeState {
	if p.y < r.MinY() || p.y > r.MaxY() {
		return r.f.source.BaseState(p.x, p.y, p.z)
	}
	if state, ok := r.overlay.get(p); ok {
		return state
	}
	if r.inTarget(p) {
		return world.NativeStateAt(r.c, uint8(p.x&15), int16(p.y), uint8(p.z&15))
	}
	return r.baseState(p)
}

// baseState returns the state of the base terrain at p.
func (r *featureRegion) baseState(p featurePos) world.NativeState {
	if r.base == nil {
		return r.f.source.BaseState(p.x, p.y, p.z)
	}
	c := r.baseChunk(world.ChunkPos{p.x >> 4, p.z >> 4})
	if p.y < int32(c.Range().Min()) || p.y > int32(c.Range().Max()) {
		return r.f.source.BaseState(p.x, p.y, p.z)
	}
	return world.NativeStateAt(c, uint8(p.x&15), int16(p.y), uint8(p.z&15))
}

// baseChunk returns the base chunk at pos, looking it up in the source once
// for the region.
func (r *featureRegion) baseChunk(pos world.ChunkPos) *chunk.Chunk {
	if r.lastBase != nil && r.lastBasePos == pos {
		return r.lastBase
	}
	c, ok := r.baseChunks[pos]
	if !ok {
		c = r.base.BaseChunk(pos)
		if r.baseChunks == nil {
			r.baseChunks = make(map[world.ChunkPos]*chunk.Chunk)
		}
		r.baseChunks[pos] = c
	}
	r.lastBasePos, r.lastBase = pos, c
	return c
}

func (r *featureRegion) Block(p featurePos) world.Block {
	state := r.nativeState(p)
	registry, ok := r.c.BlockRegistry().(world.BlockRegistry)
	if !ok {
		panic("overworld: feature region registry cannot resolve native blocks")
	}
	block, found := registry.BlockByRuntimeID(state.Block)
	if !found {
		panic("overworld: feature region encountered unknown primary block runtime ID")
	}
	return block
}

func (r *featureRegion) Set(p featurePos, state world.NativeState) bool {
	return r.setWithFlags(p, state, 3)
}

func (r *featureRegion) withinWriteArea(p featurePos) bool {
	return absFeatureInt(p.x>>4-r.writeOriginX) <= 1 && absFeatureInt(p.z>>4-r.writeOriginZ) <= 1
}

func (r *featureRegion) setWithFlags(p featurePos, state world.NativeState, flags int) bool {
	if p.y < r.MinY() || p.y > r.MaxY() {
		return false
	}
	if count := uint32(r.c.BlockRegistry().BlockCount()); state.Block >= count {
		panic("overworld: feature region cannot set unknown primary block runtime ID")
	} else if state.Liquid >= count {
		panic("overworld: feature region cannot set unknown liquid runtime ID")
	}
	if !r.withinWriteArea(p) {
		return false
	}
	pos := cube.Pos{int(p.x), int(p.y), int(p.z)}
	if _, projected := r.nativeProjections[pos]; projected {
		delete(r.nativeProjections, pos)
		r.removeBlockEntityPayloads(pos)
	}
	previous := r.nativeState(p)
	oldID, newID := previous.CanonicalID(r.c.BlockRegistry()), state.CanonicalID(r.c.BlockRegistry())
	oldName, _ := gamedata.BlockStateName(oldID)
	newName, _ := gamedata.BlockStateName(newID)
	changedBlock := oldName != newName
	if oldName == "" && newName == "" {
		changedBlock = previous.Block != state.Block
	}
	if changedBlock {
		r.removeBlockEntityPayloads(cube.Pos{int(p.x), int(p.y), int(p.z)})
	}
	r.overlay.set(p, state)
	r.noteOverlay(p)
	r.invalidatePlacementHeight(p)
	if r.inTarget(p) {
		x, z := uint8(p.x&15), uint8(p.z&15)
		world.SetNativeState(r.c, x, int16(p.y), z, state)
	}
	if r.recording != nil {
		r.recording.write(p, previous, state)
	}
	if r.recordsAt(p) && flags&16 == 0 {
		r.markBlockPostProcessing(p, state)
	}
	if !changedBlock {
		if _, exists := r.blockActorRecords[cube.Pos{int(p.x), int(p.y), int(p.z)}]; exists {
			canonicalState, err := world.CanonicalState(r.c.BlockRegistry(), state)
			if err != nil {
				panic(err)
			}
			r.refreshTemplateActorState(cube.Pos{int(p.x), int(p.y), int(p.z)}, canonicalState)
		}
	}
	return true
}

func (r *featureRegion) removeBlockEntityPayloads(pos cube.Pos) {
	delete(r.blockActorRecords, pos)
	delete(r.blockActorData, pos)
	if r.recording != nil {
		delete(r.recording.actors, pos)
	}
	entities := r.result.BlockEntities
	kept := entities[:0]
	for _, actor := range entities {
		if actor.Pos != pos {
			kept = append(kept, actor)
		}
	}
	r.result.BlockEntities = kept
}

func (r *featureRegion) Height(x, z int32, oceanFloor bool) int32 {
	return r.f.source.BaseHeight(x, z, oceanFloor)
}

func (r *featureRegion) MinY() int32 { return int32(r.c.Range()[0]) }
func (r *featureRegion) MaxY() int32 { return int32(r.c.Range()[1]) }

func featureSetNativeBlockFlag2(region *featureRegion, p featurePos, state world.NativeState, actor world.Block) bool {
	if !region.setWithFlags(p, state, 2) {
		return false
	}
	if actor != nil {
		region.recordTemplateActor(structure.Block{Pos: [3]int32{p.x, p.y, p.z}, NativeBlock: actor})
	}
	return true
}

func featureSetNativeBlockFlag19(region *featureRegion, p featurePos, state world.NativeState, actor world.Block) bool {
	if !region.setWithFlags(p, state, 19) {
		return false
	}
	if actor != nil {
		region.recordTemplateActor(structure.Block{Pos: [3]int32{p.x, p.y, p.z}, NativeBlock: actor})
	}
	return true
}

func (r *featureRegion) SetNativeBlock(p featurePos, state world.NativeState, actor world.Block) bool {
	if !r.Set(p, state) {
		return false
	}
	if actor != nil {
		r.recordTemplateActor(structure.Block{
			Pos:         [3]int32{p.x, p.y, p.z},
			NativeBlock: actor,
		})
	}
	return true
}
func (r *featureRegion) SampleNativeState(provider any, p featurePos, random *vanilla.Random) (world.NativeState, world.Block, bool) {
	placement, ok := sampleBlockState(r, p, provider, random)
	if !ok {
		return world.NativeState{}, nil, false
	}
	state, actor := r.resolveFeatureBlock(placement, p)
	return state, actor, true
}
func (r *featureRegion) resolveFeatureBlock(placement structure.Block, p featurePos) (world.NativeState, world.Block) {
	state, err := placement.ResolveNative(r.c.BlockRegistry(), r.nativeState(p))
	if err != nil {
		panic(err)
	}
	return state, placement.NativeBlock
}
func featureStateHasFluid(c *chunk.Chunk, state world.NativeState) bool {
	for _, rid := range [2]uint32{state.Block, state.Liquid} {
		name, _, ok := c.BlockRegistry().RuntimeIDToState(rid)
		if !ok {
			panic("overworld: unknown runtime ID while checking feature fluid state")
		}
		name = normalizeResource(name)
		if name == "water" || name == "lava" {
			return true
		}
	}
	return false
}

func (r *featureRegion) markBlockPostProcessing(p featurePos, state world.NativeState) {
	id := state.CanonicalID(r.c.BlockRegistry())
	if id == 0 {
		// Native-only placements, including flowing-fluid palette variants,
		// have no Java block callback. They retain their native capabilities.
		return
	}
	name, ok := gamedata.BlockStateName(id)
	if !ok {
		panic("overworld: feature write has an invalid canonical identity")
	}
	switch normalizeResource(name) {
	case "brown_mushroom", "red_mushroom":
		r.markPostProcessing(cube.Pos{int(p.x), int(p.y), int(p.z)})
	case "soul_sand", "magma_block":
		above := featurePos{p.x, p.y + 1, p.z}
		name, _ := r.State(above)
		if normalizeResource(name) != "air" && normalizeResource(name) != "cave_air" && normalizeResource(name) != "void_air" {
			r.markPostProcessing(cube.Pos{int(above.x), int(above.y), int(above.z)})
		}
	}
}

func (r *featureRegion) markAboveForPostProcessing(p featurePos) {
	for offset := int32(1); offset <= 2; offset++ {
		marked := featurePos{p.x, p.y + offset, p.z}
		name, _ := r.State(marked)
		name = normalizeResource(name)
		if name == "air" || name == "cave_air" || name == "void_air" {
			return
		}
		r.markPostProcessing(cube.Pos{int(marked.x), int(marked.y), int(marked.z)})
	}
}
func (r *featureRegion) scheduledBlockID(state world.NativeState) uint32 {
	if state.Block == r.c.BlockRegistry().AirRuntimeID() {
		return state.Liquid
	}
	return state.Block
}

// PlaceValue executes a configured Feature holder, never a PlacedFeature holder.
func (r *featureRegion) PlaceValue(value any, origin featurePos, random *vanilla.Random) bool {
	switch feature := value.(type) {
	case string:
		return r.f.placeConfiguredRegion(r, normalizeResource(feature), origin, random)
	case map[string]any:
		typeName, ok := feature["type"].(string)
		if !ok {
			panic("overworld: inline feature has no type")
		}
		return r.f.placeConfiguredData(r, normalizeResource(typeName), feature, origin, random)
	default:
		panic("overworld: malformed feature reference")
	}
}

// PlacePlacedFeature executes a PlacedFeature holder at an exact structure position.
func (r *featureRegion) PlacePlacedFeature(value any, origin [3]int32, random *vanilla.Random) bool {
	if random == nil {
		panic("overworld: structure feature has no random source")
	}
	position := featurePos{x: origin[0], y: origin[1], z: origin[2]}
	if name, ok := value.(string); ok {
		placed, exists := r.f.placed[normalizeResource(name)]
		if !exists {
			panic("overworld: missing placed feature " + name)
		}
		return r.f.placeWithModifiers(r, placed.feature, placed.modifiers, position, random, "")
	}
	if definition, ok := value.(map[string]any); ok {
		feature, exists := definition["feature"]
		if !exists {
			panic("overworld: inline placed feature has no feature holder")
		}
		return r.f.placeWithModifiers(r, feature, inlinePlacementModifiers(definition["placement"]), position, random, "")
	}
	panic(fmt.Sprintf("overworld: malformed placed-feature holder %T", value))
}

func (f *ModernFeatures) placeConfiguredRegion(region *featureRegion, name string, origin featurePos, random *vanilla.Random) bool {
	config, ok := f.configured[normalizeResource(name)]
	if !ok {
		panic("overworld: missing configured feature " + name)
	}
	return f.placeConfiguredData(region, config.typeName, config.config, origin, random)
}

func (f *ModernFeatures) placeConfiguredData(region *featureRegion, typeName string, data map[string]any, origin featurePos, random *vanilla.Random) bool {
	config := modernFeatureConfig{typeName: typeName, config: data}
	switch config.typeName {
	case "no_op":
		return true
	case "void_start_platform":
		return placeModernVoidStartPlatform(region, origin)
	case "bonus_chest":
		return placeModernBonusChest(region, origin, random)
	case "sculk_patch":
		return placeModernSculkPatch(region, config.config, origin, random)
	case "template":
		return placeModernTemplateFeature(region, config.config, origin, random)
	case "end_gateway":
		return placeModernEndGateway(region, config.config, origin)
	case "end_island":
		return placeModernEndIsland(region, origin, random)
	case "end_platform":
		return placeModernEndPlatform(region, origin)
	case "end_podium":
		return placeModernEndPodium(region, config.config, origin)
	case "end_spike":
		return placeModernEndSpike(region, config.config, origin, random)
	case "chorus_plant":
		return placeModernChorusPlant(region, origin, random)
	case "sequence":
		features, ok := config.config["features"].([]any)
		if !ok {
			return region.PlacePlacedFeature(config.config["features"], [3]int32{origin.x, origin.y, origin.z}, random)
		}
		if len(features) == 0 {
			panic("overworld: malformed sequence feature")
		}
		for _, child := range features {
			if !region.PlacePlacedFeature(child, [3]int32{origin.x, origin.y, origin.z}, random) {
				return false
			}
		}
		return true
	case "random_boolean_selector":
		feature := config.config["feature_false"]
		if random.NextBoolean() {
			feature = config.config["feature_true"]
		}
		return region.PlacePlacedFeature(feature, [3]int32{origin.x, origin.y, origin.z}, random)
	case "simple_random_selector":
		features, ok := config.config["features"].([]any)
		if !ok {
			random.NextInt(1)
			return region.PlacePlacedFeature(config.config["features"], [3]int32{origin.x, origin.y, origin.z}, random)
		}
		if len(features) == 0 {
			panic("overworld: malformed simple random selector")
		}
		return region.PlacePlacedFeature(features[random.NextInt(int32(len(features)))], [3]int32{origin.x, origin.y, origin.z}, random)
	case "random_selector":
		features, ok := config.config["features"].([]any)
		if !ok {
			panic("overworld: malformed random selector")
		}
		for _, raw := range features {
			entry := raw.(map[string]any)
			chance := float32(entry["chance"].(float64))
			if random.NextFloat() < chance {
				return region.PlacePlacedFeature(entry["feature"], [3]int32{origin.x, origin.y, origin.z}, random)
			}
		}
		return region.PlacePlacedFeature(config.config["default"], [3]int32{origin.x, origin.y, origin.z}, random)
	case "weighted_random_selector":
		features, ok := config.config["features"].([]any)
		if !ok {
			panic("overworld: malformed weighted random selector")
		}
		total := int64(0)
		for _, raw := range features {
			weight := intProviderValue(raw.(map[string]any)["weight"])
			if weight < 0 {
				panic("overworld: negative weighted random selector weight")
			}
			total += int64(weight)
		}
		if total == 0 {
			return false
		}
		if total > math.MaxInt32 {
			panic("overworld: weighted random selector weight sum exceeds int32")
		}
		choice := random.NextInt(int32(total))
		for _, raw := range features {
			entry := raw.(map[string]any)
			weight := intProviderValue(entry["weight"])
			if choice < weight {
				value, ok := entry["data"]
				if !ok {
					panic("overworld: weighted selector entry has no data")
				}
				return region.PlacePlacedFeature(value, [3]int32{origin.x, origin.y, origin.z}, random)
			}
			choice -= weight
		}
	case "overlay":
		features, ok := config.config["features"].([]any)
		if !ok {
			return region.PlacePlacedFeature(config.config["features"], [3]int32{origin.x, origin.y, origin.z}, random)
		}
		if len(features) == 0 {
			panic("overworld: malformed overlay feature")
		}
		placed := false
		for _, feature := range features {
			placed = region.PlacePlacedFeature(feature, [3]int32{origin.x, origin.y, origin.z}, random) || placed
		}
		return placed
	case "simple_block":
		placement, ok := sampleOptionalBlockState(region, origin, config.config["to_place"], random)
		if !ok {
			return false
		}
		sourceState := featureBlockStateID(placement)
		return world.PlaceGenerationSimpleBlock(region, sourceState,
			cube.Pos{int(origin.x), int(origin.y), int(origin.z)}, config.config["schedule_tick"] == true)
	case "freeze_top_layer":
		return region.freezeTopLayerRegion(origin)
	case "ore":
		return placeModernOreRegion(region, config.config, origin, random)
	case "tree", "fallen_tree", "huge_brown_mushroom", "huge_red_mushroom", "huge_fungus", "bamboo", "root_system":
		handled, placed := placeModernTreeFamily(region, config.typeName, config.config, origin, random)
		if handled {
			return placed
		}
	case "geode", "large_dripstone", "speleothem", "speleothem_cluster", "vegetation_patch", "waterlogged_vegetation_patch", "multiface_growth", "lake", "monster_room", "fossil", "spike", "iceberg", "blue_ice", "netherrack_replace_blobs", "scattered_ore", "delta_feature", "random_neighbor_spread", "underwater_magma":
		handled, placed := placeModernCaveFamily(region, config.typeName, config.config, origin, random)
		if handled {
			return placed
		}
		handled, placed = placeModernIceFamily(region, config.typeName, config.config, origin, random)
		if handled {
			return placed
		}
	case "disk":
		return placeModernDiskRegion(region, config.config, origin, random)
	case "stepped_column_cluster":
		return placeModernSteppedColumnCluster(region, config.config, origin, random)
	case "single_block_pillar":
		return placeModernSingleBlockPillar(region, config.config, origin, random)
	case "projected_random_patchy_square":
		return placeModernProjectedPatchySquare(region, config.config, origin, random)
	case "block_blob", "block_pile", "fill_layer", "replace_single_block":
		if handled, placed := placeModernTerrainMisc(region, config.typeName, config.config, origin, random); handled {
			return placed
		}
	case "coral_tree", "coral_claw":
		if handled, placed := placeModernGrowthMisc(region, config.typeName, config.config, origin, random); handled {
			return placed
		}
	case "vines":
		return placeModernVines(region, origin)
	case "spring_feature":
		return placeModernSpringRegion(region, config.config, origin, random)
	case "block_column":
		return placeModernBlockColumnRegion(region, config.config, origin, random)
	default:
		panic("overworld: unsupported configured feature " + config.typeName)
	}
	panic("overworld: feature family failed to claim " + config.typeName)
}

func (r *featureRegion) freezeTopLayerRegion(origin featurePos) bool {
	registry := r.c.BlockRegistry()
	ice, err := world.ResolveState(registry, world.BlockState{Name: "minecraft:ice"})
	if err != nil {
		panic(err)
	}
	snow, err := world.ResolveState(registry, world.BlockState{Name: "minecraft:snow"})
	if err != nil {
		panic(err)
	}
	snowID := snow.CanonicalID(registry)
	for dx := int32(0); dx < 16; dx++ {
		for dz := int32(0); dz < 16; dz++ {
			x, z := origin.x+dx, origin.z+dz
			y := r.placementHeight(x, z, "MOTION_BLOCKING")
			top, below := featurePos{x, y, z}, featurePos{x, y - 1, z}
			// Source selects the biome at topPos for both predicates, even
			// when the block below belongs to a different vertical biome.
			biome := normalizeResource(r.f.source.BiomeAt(x, y, z))
			if r.biomeShouldFreezeFor(biome, below, false) {
				r.Set(below, ice)
			}
			if !r.f.biomes[biome].hasPrecipitation || r.biomeTemperatureFor(biome, top) >= float32(.15) ||
				y < r.MinY() || y > r.MaxY() || r.blockLight(top) >= 10 {
				continue
			}
			stateID := r.nativeState(top).CanonicalID(registry)
			name, _ := gamedata.BlockStateName(stateID)
			if name != "minecraft:air" && name != "minecraft:cave_air" && name != "minecraft:void_air" && name != "minecraft:snow" {
				continue
			}
			if !world.GenerationBlockCanSurvive(r, snowID, cube.Pos{int(x), int(y), int(z)}) {
				continue
			}
			r.Set(top, snow)
			belowID := r.nativeState(below).CanonicalID(registry)
			if _, exists := gamedata.BlockStateBool(belowID, "snowy"); exists {
				snowyID, err := gamedata.BlockStateWithProperty(belowID, "snowy", true)
				if err != nil {
					panic(err)
				}
				r.SetBlockState(cube.Pos{int(x), int(y - 1), int(z)}, snowyID, 2)
			}
		}
	}
	return true
}
func placeModernOreRegion(region *featureRegion, config map[string]any, origin featurePos, random *vanilla.Random) bool {
	size := int32(featureNumber(config["size"]))
	targets, ok := config["targets"].([]any)
	if !ok || size < 0 || size > 64 {
		panic("overworld: malformed ore configuration")
	}
	var targetBuffer [2]modernOreTarget
	oreTargets := targetBuffer[:0]
	if len(targets) > len(targetBuffer) {
		oreTargets = make([]modernOreTarget, 0, len(targets))
	}
	for _, raw := range targets {
		entry := raw.(map[string]any)
		placement, ok := featureStateBlock(entry["state"])
		if !ok {
			panic("overworld: malformed ore target state")
		}
		state, actor := region.resolveFeatureBlock(placement, origin)
		predicate, ok := entry["target"].(map[string]any)
		if !ok {
			panic("overworld: malformed ore target predicate")
		}
		oreTargets = append(oreTargets, modernOreTarget{rid: state, actor: actor, predicate: predicate})
	}
	// OreFeature samples the entire ellipsoid chain before examining blocks.
	// The source codec caps size at 64; both scratch buffers fit on the stack.
	angle := random.NextFloat() * float32(math.Pi)
	spread := float32(size) / 8
	maxRadius := int32(math.Ceil(float64((float32(size)/16*2 + 1) / 2)))
	startX := float64(origin.x) + math.Sin(float64(angle))*float64(spread)
	endX := float64(origin.x) - math.Sin(float64(angle))*float64(spread)
	startZ := float64(origin.z) + math.Cos(float64(angle))*float64(spread)
	endZ := float64(origin.z) - math.Cos(float64(angle))*float64(spread)
	startY := float64(origin.y + random.NextInt(3) - 2)
	endY := float64(origin.y + random.NextInt(3) - 2)
	horizontalRadius := int32(math.Ceil(float64(spread))) + maxRadius
	xStart, yStart, zStart := origin.x-horizontalRadius, origin.y-2-maxRadius, origin.z-horizontalRadius
	sizeXZ, sizeY := 2*horizontalRadius, 2*(2+maxRadius)
	belowSurface := false
	for x := xStart; x <= xStart+sizeXZ && !belowSurface; x++ {
		for z := zStart; z <= zStart+sizeXZ; z++ {
			if yStart <= region.placementHeight(x, z, "OCEAN_FLOOR_WG") {
				belowSurface = true
				break
			}
		}
	}
	if !belowSurface {
		return false
	}
	var spheres [64]struct{ x, y, z, radius float64 }
	for i := int32(0); i < size; i++ {
		step := float32(i) / float32(size)
		t := float64(step)
		scale := random.NextDouble() * float64(size) / 16
		sphere := &spheres[i]
		sphere.x = startX + t*(endX-startX)
		sphere.y = startY + t*(endY-startY)
		sphere.z = startZ + t*(endZ-startZ)
		sphere.radius = (float64(vanilla.Sin(float64(float32(math.Pi)*step))+1)*scale + 1) / 2
	}
	for i := int32(0); i < size-1; i++ {
		if spheres[i].radius <= 0 {
			continue
		}
		for j := i + 1; j < size; j++ {
			if spheres[j].radius <= 0 {
				continue
			}
			dx, dy, dz := spheres[i].x-spheres[j].x, spheres[i].y-spheres[j].y, spheres[i].z-spheres[j].z
			dr := spheres[i].radius - spheres[j].radius
			if dr*dr > dx*dx+dy*dy+dz*dz {
				if dr > 0 {
					spheres[j].radius = -1
				} else {
					spheres[i].radius = -1
				}
			}
		}
	}
	// Maximum source bounding volume: 26*14*26 bits for a size-64 vein.
	var tested [148]uint64
	discard := float32(number(config["discard_chance_on_air_exposure"]))
	placed := false
	for i := int32(0); i < size; i++ {
		sphere := &spheres[i]
		radius := sphere.radius
		if radius < 0 {
			continue
		}
		minX := max(int32(math.Floor(sphere.x-radius)), xStart)
		minY := max(int32(math.Floor(sphere.y-radius)), yStart)
		minZ := max(int32(math.Floor(sphere.z-radius)), zStart)
		maxX := max(int32(math.Floor(sphere.x+radius)), minX)
		maxY := max(int32(math.Floor(sphere.y+radius)), minY)
		maxZ := max(int32(math.Floor(sphere.z+radius)), minZ)
		for x := minX; x <= maxX; x++ {
			dx := (float64(x) + .5 - sphere.x) / radius
			if dx*dx >= 1 {
				continue
			}
			for y := minY; y <= maxY; y++ {
				dy := (float64(y) + .5 - sphere.y) / radius
				if dx*dx+dy*dy >= 1 {
					continue
				}
				for z := minZ; z <= maxZ; z++ {
					dz := (float64(z) + .5 - sphere.z) / radius
					if dx*dx+dy*dy+dz*dz >= 1 || y < region.MinY() || y > region.MaxY() {
						continue
					}
					index := x - xStart + (y-yStart)*sizeXZ + (z-zStart)*sizeXZ*sizeY
					word, bit := index>>6, uint64(1)<<uint(index&63)
					if tested[word]&bit != 0 {
						continue
					}
					tested[word] |= bit
					p := featurePos{x, y, z}
					// BulkSectionAccess checks the source-origin write window
					// before predicates or air-exposure RNG consume the stream.
					if !region.withinWriteArea(p) {
						continue
					}
					stateID := region.nativeState(p).CanonicalID(region.c.BlockRegistry())
					for _, target := range oreTargets {
						if !featureRuleMatches(p, target.predicate, stateID, random) {
							continue
						}
						if discard > 0 && (discard >= 1 || random.NextFloat() < discard) {
							exposed := false
							for _, offset := range [6]featurePos{{y: -1}, {y: 1}, {z: -1}, {z: 1}, {x: -1}, {x: 1}} {
								neighbour := featurePos{x + offset.x, y + offset.y, z + offset.z}
								name, _ := gamedata.BlockStateName(region.nativeState(neighbour).CanonicalID(region.c.BlockRegistry()))
								if treeIsAirName(name) {
									exposed = true
									break
								}
							}
							if exposed {
								continue
							}
						}
						// Java writes through BulkSectionAccess, not WorldGenLevel:
						// no automatic postprocessing, but native actor payloads remain valid.
						if region.setWithFlags(p, target.rid, 16) && target.actor != nil {
							region.recordTemplateActor(structure.Block{Pos: [3]int32{p.x, p.y, p.z}, NativeBlock: target.actor})
						}
						placed = true
						break
					}
				}
			}
		}
	}
	return placed
}
func placeModernDiskRegion(region *featureRegion, config map[string]any, origin featurePos, random *vanilla.Random) bool {
	radius := sampleInt(config["radius"], random)
	halfHeight := int32(featureNumber(config["half_height"]))
	predicate, ok := config["target"].(map[string]any)
	if !ok {
		panic("overworld: malformed disk target predicate")
	}
	placed := false
	// BlockPos.betweenClosed advances X before Z; each column runs top-down.
	for zi := range int(2*radius + 1) {
		dz := int32(zi) - radius
		for xi := range int(2*radius + 1) {
			dx := int32(xi) - radius
			if dx*dx+dz*dz > radius*radius {
				continue
			}
			placedAbove := false
			for yi := range int(2*halfHeight + 1) {
				dy := halfHeight - int32(yi)
				p := featurePos{origin.x + dx, origin.y + dy, origin.z + dz}
				if !featurePredicate(region, p, predicate) {
					placedAbove = false
					continue
				}
				block, ok := sampleOptionalBlockState(region, p, config["state_provider"], random)
				if !ok {
					continue
				}
				state, err := block.ResolveNative(region.c.BlockRegistry(), region.nativeState(p))
				if err != nil {
					panic(err)
				}
				featureSetNativeBlockFlag2(region, p, state, block.NativeBlock)
				if !placedAbove {
					region.markAboveForPostProcessing(p)
				}
				placed = true
				placedAbove = true
			}
		}
	}
	return placed
}

func placeModernSpringRegion(region *featureRegion, config map[string]any, p featurePos, _ *vanilla.Random) bool {
	validRaw, ok := config["valid_blocks"]
	if !ok {
		panic("overworld: malformed spring valid blocks")
	}
	valid := func(pos featurePos) bool {
		name, _ := gamedata.BlockStateName(region.nativeState(pos).CanonicalID(region.c.BlockRegistry()))
		return featureBlockHolderContains(validRaw, name)
	}
	if !valid(featurePos{p.x, p.y + 1, p.z}) || config["requires_block_below"] != false && !valid(featurePos{p.x, p.y - 1, p.z}) {
		return false
	}
	name, _ := gamedata.BlockStateName(region.nativeState(p).CanonicalID(region.c.BlockRegistry()))
	if !treeIsAirName(name) && !featureBlockHolderContains(validRaw, name) {
		return false
	}
	rock, holes := int32(0), int32(0)
	for _, d := range [5]featurePos{{x: -1}, {x: 1}, {z: -1}, {z: 1}, {y: -1}} {
		neighbor := featurePos{p.x + d.x, p.y + d.y, p.z + d.z}
		name, _ := gamedata.BlockStateName(region.nativeState(neighbor).CanonicalID(region.c.BlockRegistry()))
		if featureBlockHolderContains(validRaw, name) {
			rock++
		}
		if treeIsAirName(name) {
			holes++
		}
	}
	wantRock, wantHole := int32(4), int32(1)
	if value, exists := config["rock_count"]; exists {
		wantRock = int32(featureNumber(value))
	}
	if value, exists := config["hole_count"]; exists {
		wantHole = int32(featureNumber(value))
	}
	if rock != wantRock || holes != wantHole {
		return false
	}
	fluid, ok := config["state"].(map[string]any)
	if !ok {
		panic("overworld: malformed spring fluid state")
	}
	fluidType, ok := fluid["id"].(string)
	if !ok {
		panic("overworld: malformed spring fluid type")
	}
	var properties map[string]any
	if value, exists := fluid["properties"]; exists {
		var valid bool
		properties, valid = value.(map[string]any)
		if !valid {
			panic("overworld: malformed spring fluid properties")
		}
	}
	id, err := world.FluidLegacyBlock(fluidType, properties)
	if err != nil {
		panic(err)
	}
	region.setWithFlags(p, world.ResolveStateID(region.c.BlockRegistry(), id), 2)
	region.scheduleTick(chunk.ScheduledTick{
		Pos: cube.Pos{int(p.x), int(p.y), int(p.z)}, Kind: chunk.FluidTick, Type: world.NormaliseBlockName(fluidType),
	})
	return true
}

func placeModernBlockColumnRegion(region *featureRegion, config map[string]any, origin featurePos, random *vanilla.Random) bool {
	direction, ok := config["direction"].(string)
	if !ok {
		panic("overworld: malformed block column direction")
	}
	var dx, dy, dz int32
	switch direction {
	case "down":
		dy = -1
	case "up":
		dy = 1
	case "north":
		dz = -1
	case "south":
		dz = 1
	case "west":
		dx = -1
	case "east":
		dx = 1
	default:
		panic("overworld: unsupported block column direction " + direction)
	}
	layers, ok := config["layers"].([]any)
	if !ok {
		panic("overworld: malformed block column layers")
	}
	var heights [8]int32
	layerHeights := heights[:]
	if len(layers) > len(heights) {
		layerHeights = make([]int32, len(layers))
	} else {
		layerHeights = layerHeights[:len(layers)]
	}
	totalHeight := int32(0)
	for i, raw := range layers {
		layer, ok := raw.(map[string]any)
		if !ok {
			panic("overworld: malformed block column layer")
		}
		layerHeights[i] = sampleInt(layer["height"], random)
		if layerHeights[i] < 0 {
			panic("overworld: negative block column layer height")
		}
		totalHeight += layerHeights[i]
	}
	if totalHeight == 0 {
		return false
	}
	allowed, ok := config["allowed_placement"].(map[string]any)
	if !ok {
		panic("overworld: malformed block column predicate")
	}
	next := featurePos{origin.x + dx, origin.y + dy, origin.z + dz}
	validHeight := int32(0)
	for range totalHeight {
		if !featurePredicate(region, next, allowed) {
			break
		}
		validHeight++
		next.x += dx
		next.y += dy
		next.z += dz
	}
	if validHeight < totalHeight {
		amount := totalHeight - validHeight
		prioritizeTip := config["prioritize_tip"] == true
		start, step, end := len(layerHeights)-1, -1, -1
		if prioritizeTip {
			start, step, end = 0, 1, len(layerHeights)
		}
		for i := start; i != end && amount > 0; i += step {
			remove := layerHeights[i]
			if remove > amount {
				remove = amount
			}
			layerHeights[i] -= remove
			amount -= remove
		}
	}
	p := origin
	for i, raw := range layers {
		layer := raw.(map[string]any)
		for range layerHeights[i] {
			state, actor, ok := region.SampleNativeState(layer["provider"], p, random)
			if !ok {
				panic("overworld: unresolved block column provider")
			}
			featureSetNativeBlockFlag2(region, p, state, actor)
			p.x += dx
			p.y += dy
			p.z += dz
		}
	}
	return true
}

func environmentScanPlacement(region *featureRegion, in []featurePos, modifier map[string]any, minY, maxY int32) []featurePos {
	direction, ok := modifier["direction_of_search"].(string)
	if !ok {
		panic("overworld: malformed environment_scan direction")
	}
	var step int32
	switch direction {
	case "up":
		step = 1
	case "down":
		step = -1
	default:
		panic("overworld: unsupported environment_scan direction " + direction)
	}
	target, ok := modifier["target_condition"].(map[string]any)
	if !ok {
		panic("overworld: malformed environment_scan target condition")
	}
	allowed, hasAllowed := modifier["allowed_search_condition"].(map[string]any)
	if _, exists := modifier["allowed_search_condition"]; exists && !hasAllowed {
		panic("overworld: malformed environment_scan allowed condition")
	}
	maxStepsValue, ok := modifier["max_steps"].(float64)
	if !ok || maxStepsValue < 1 || maxStepsValue > 32 || maxStepsValue != math.Trunc(maxStepsValue) {
		panic("overworld: malformed environment_scan max_steps")
	}
	maxSteps := int32(maxStepsValue)
	out := make([]featurePos, 0, len(in))
	for _, origin := range in {
		if hasAllowed && !featurePredicate(region, origin, allowed) {
			continue
		}
		pos := origin
		found, outside := false, false
		for range maxSteps {
			if featurePredicate(region, pos, target) {
				out = append(out, pos)
				found = true
				break
			}
			pos.y += step
			if pos.y < minY || pos.y >= maxY {
				outside = true
				break
			}
			if hasAllowed && !featurePredicate(region, pos, allowed) {
				break
			}
		}
		if !found && !outside && featurePredicate(region, pos, target) {
			out = append(out, pos)
		}
	}
	return out
}

func featureFluidName(region *featureRegion, p featurePos, properties map[string]any) string {
	state := region.nativeState(p)
	registry := region.c.BlockRegistry()
	if state.Liquid != registry.AirRuntimeID() {
		name, fluidProperties, ok := runtimeIDStateView(registry, state.Liquid)
		if !ok {
			panic("overworld: invalid native liquid runtime ID in fluid predicate")
		}
		switch normalizeResource(name) {
		case "water", "flowing_water", "lava", "flowing_lava":
			return featureLiquidFluidName(normalizeResource(name), fluidProperties)
		default:
			panic("overworld: unsupported native liquid in fluid predicate: " + name)
		}
	}
	name, nativeProperties, ok := runtimeIDStateView(registry, state.Block)
	if !ok {
		panic("overworld: invalid native block runtime ID in fluid predicate")
	}
	name = normalizeResource(name)
	switch name {
	case "water", "flowing_water", "lava", "flowing_lava":
		return featureLiquidFluidName(name, nativeProperties)
	case "bubble_column", "kelp", "kelp_plant", "seagrass", "tall_seagrass":
		return "water"
	}
	if properties["waterlogged"] == true {
		return "water"
	}
	return "empty"
}

// runtimeIDStateView resolves a native runtime ID like RuntimeIDToState. The
// properties may be shared with the registry and must not be modified.
func runtimeIDStateView(registry chunk.BlockRegistry, rid uint32) (string, map[string]any, bool) {
	if basic, ok := registry.(*world.BasicBlockRegistry); ok {
		return basic.RuntimeIDToStateView(rid)
	}
	return registry.RuntimeIDToState(rid)
}

func featureLiquidFluidName(name string, properties map[string]any) string {
	depth, ok := properties["liquid_depth"].(int32)
	if !ok {
		panic("overworld: native liquid has no int32 liquid_depth")
	}
	name = strings.TrimPrefix(name, "flowing_")
	if depth != 0 {
		if name == "water" {
			return "flowing_water"
		}
		return "flowing_lava"
	}
	return name
}

func featureFluidTagContains(tag, fluid string) bool {
	switch normalizeResource(strings.TrimPrefix(tag, "#")) {
	case "water", "supports_sugar_cane_adjacently", "entity_floatable":
		return fluid == "water" || fluid == "flowing_water"
	case "lava":
		return fluid == "lava" || fluid == "flowing_lava"
	case "supports_lily_pad", "supports_frogspawn", "bubble_column_can_occupy",
		"axolotl_tries_to_find", "dolphin_tries_to_find", "frog_tries_to_find_land_near":
		return fluid == "water"
	default:
		panic("overworld: unresolved fluid tag #" + normalizeResource(tag))
	}
}

func featureFluidHolderContains(raw any, fluid string) bool {
	matches := func(value string) bool {
		if strings.HasPrefix(value, "#") {
			return featureFluidTagContains(value, fluid)
		}
		name := normalizeResource(value)
		switch name {
		case "empty", "water", "flowing_water", "lava", "flowing_lava":
			return name == fluid
		default:
			panic("overworld: unresolved fluid " + value)
		}
	}
	switch values := raw.(type) {
	case string:
		return matches(values)
	case []any:
		for _, value := range values {
			name, ok := value.(string)
			if !ok {
				panic("overworld: malformed fluid holder set")
			}
			if matches(name) {
				return true
			}
		}
		return false
	default:
		panic("overworld: malformed fluid holder set")
	}
}

func featurePredicate(region *featureRegion, p featurePos, predicate map[string]any) bool {
	typeName, ok := predicate["type"].(string)
	if !ok {
		panic("overworld: block predicate without type")
	}
	kind := normalizeResource(typeName)
	switch kind {
	case "matching_blocks", "matching_block_tag", "matching_fluids", "has_sturdy_face", "solid", "replaceable", "would_survive", "inside_world_bounds":
		if raw, exists := predicate["offset"]; exists {
			offset := featurePredicateVector(raw)
			p.x += offset[0]
			p.y += offset[1]
			p.z += offset[2]
		}
	}
	switch kind {
	case "true":
		return true
	case "inside_world_bounds":
		return p.y >= region.MinY() && p.y <= region.MaxY()
	case "height_range":
		seaLevel := region.f.source.SeaLevel()
		minY := featureBound(predicate["min_inclusive"], region.MinY(), region.MaxY()+1, seaLevel)
		maxY := featureBound(predicate["max_inclusive"], region.MinY(), region.MaxY()+1, seaLevel)
		return float64(p.y) >= minY && float64(p.y) <= maxY
	case "would_survive":
		return featureWouldSurvive(region, p, predicate["state"])
	case "all_of", "any_of":
		values, ok := predicate["predicates"].([]any)
		if !ok {
			panic("overworld: malformed compound block predicate")
		}
		all := kind == "all_of"
		for _, value := range values {
			match := featurePredicate(region, p, value.(map[string]any))
			if match != all {
				return !all
			}
		}
		return all
	case "not":
		child, ok := predicate["predicate"].(map[string]any)
		if !ok {
			panic("overworld: malformed negated block predicate")
		}
		return !featurePredicate(region, p, child)
	case "matching_biomes":
		return featureBiomeHolderContains(predicate["biomes"], region.f.source.BiomeAt(p.x, p.y, p.z))
	case "replaceable":
		shape, err := gamedata.BlockGenerationShapeID(region.nativeState(p).CanonicalID(region.c.BlockRegistry()))
		if err != nil {
			panic(err)
		}
		return shape.Replaceable
	case "unobstructed":
		// WorldGenRegion's entity queries are empty. EntityGetter therefore
		// accepts the unit shape; Source also deliberately ignores offset.
		return true
	case "volume_match":
		minimum := featurePredicateVector(predicate["min"])
		maximum := featurePredicateVector(predicate["max"])
		if minimum[0] > maximum[0] || minimum[1] > maximum[1] || minimum[2] > maximum[2] {
			panic("overworld: volume_match min bound exceeds max bound")
		}
		match, ok := predicate["match"].(map[string]any)
		if !ok {
			panic("overworld: malformed volume_match predicate")
		}
		for x := minimum[0]; x <= maximum[0]; x++ {
			for z := minimum[2]; z <= maximum[2]; z++ {
				for y := minimum[1]; y <= maximum[1]; y++ {
					if !featurePredicate(region, featurePos{p.x + x, p.y + y, p.z + z}, match) {
						return false
					}
				}
			}
		}
		return true
	}
	name, properties := region.State(p)
	name = normalizeResource(name)
	switch kind {
	case "matching_fluids":
		fluids, exists := predicate["fluids"]
		if !exists {
			panic("overworld: matching_fluids predicate has no fluid holder set")
		}
		return featureFluidHolderContains(fluids, featureFluidName(region, p, properties))
	case "matching_blocks":
		return featureBlockHolderContains(predicate["blocks"], name)
	case "matching_block_tag":
		tag, ok := predicate["tag"].(string)
		if !ok {
			panic("overworld: malformed matching_block_tag predicate")
		}
		return blockTagContains(normalizeResource(tag), normalizeResource(name))
	case "has_sturdy_face":
		direction, ok := predicate["direction"].(string)
		if !ok {
			panic("overworld: malformed has_sturdy_face predicate direction")
		}
		var face cube.Face
		switch direction {
		case "down":
			face = cube.FaceDown
		case "up":
			face = cube.FaceUp
		case "north":
			face = cube.FaceNorth
		case "south":
			face = cube.FaceSouth
		case "west":
			face = cube.FaceWest
		case "east":
			face = cube.FaceEast
		default:
			panic("overworld: unsupported has_sturdy_face direction " + direction)
		}
		shape, err := gamedata.BlockGenerationShape(name, properties)
		if err != nil {
			panic(err)
		}
		return shape.Sturdy&(1<<uint(face)) != 0
	case "solid":
		return featureSolid(name)
	default:
		panic("overworld: unsupported block predicate " + normalizeResource(typeName))
	}
}
func featureSolid(name string) bool {
	solid, err := gamedata.BlockIsSolid(name)
	if err != nil {
		panic(err)
	}
	return solid
}

func featureBlockHolderContains(holder any, name string) bool {
	name = normalizeResource(name)
	switch values := holder.(type) {
	case string:
		if tag, ok := strings.CutPrefix(values, "#"); ok {
			return blockTagContains(tag, name)
		}
		return name == normalizeResource(values)
	case []any:
		for _, value := range values {
			if name == normalizeResource(value.(string)) {
				return true
			}
		}
		return false
	default:
		panic("overworld: malformed block holder set")
	}
}

func featurePredicateVector(raw any) [3]int32 {
	values, ok := raw.([]any)
	if !ok || len(values) != 3 {
		panic("overworld: malformed block predicate vector")
	}
	var vector [3]int32
	for i, value := range values {
		n, ok := value.(float64)
		if !ok || n < -16 || n > 16 || n != math.Trunc(n) {
			panic("overworld: invalid block predicate vector")
		}
		vector[i] = int32(n)
	}
	return vector
}

func featureBiomeHolderContains(holder any, biome string) bool {
	biome = normalizeResource(biome)
	switch values := holder.(type) {
	case string:
		if tag, ok := strings.CutPrefix(values, "#"); ok {
			match, err := gamedata.BiomeTagContains(tag, biome)
			if err != nil {
				panic(err)
			}
			return match
		}
		return biome == normalizeResource(values)
	case []any:
		for _, value := range values {
			if biome == normalizeResource(value.(string)) {
				return true
			}
		}
		return false
	default:
		panic("overworld: malformed biome holder set")
	}
}
