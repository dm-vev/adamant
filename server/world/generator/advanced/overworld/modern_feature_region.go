package overworld

import (
	"fmt"
	"math"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/structure"
)

const (
	featFlagComputed uint32 = 1 << iota
	featFlagAir
	featFlagExactAir
	featFlagWorldSurface
	featFlagMotion
	featFlagMushroom
	featFlagBubbleBase
	featFlagStoneOre
	featFlagDeepslateOre
	featFlagBaseStone
	featFlagHeightSpecificOre
	featFlagSolid
	featFlagReplaceable
	featFlagShapeOK
	featFlagCanonicalErr
	featFlagCollisionFull

	featFlagSturdyShift     = 16 // 6 bits: cube.Face 0..5 (bits 16..21)
	featFlagAttachmentShift = 22 // 6 bits: cube.Face 0..5 (bits 22..27)

	featFlagFire               uint32 = 1 << 28
	featFlagSculkReplaceable   uint32 = 1 << 29
	featFlagSculkReplaceableWG uint32 = 1 << 30
	featFlagSculkBehavior      uint32 = 1 << 31
)

var defaultFeatureMeta = sync.OnceValue(func() []atomic.Uint32 {
	return make([]atomic.Uint32, world.DefaultBlockRegistry.BlockCount())
})

var defaultFeatureNames = sync.OnceValue(func() []atomic.Pointer[string] {
	return make([]atomic.Pointer[string], world.DefaultBlockRegistry.BlockCount())
})

func featureMetaForDefaultRID(rid uint32) uint32 {
	table := defaultFeatureMeta()
	if int(rid) >= len(table) {
		return featFlagCanonicalErr
	}
	if flags := table[rid].Load(); flags != 0 {
		return flags
	}
	airRID := world.DefaultBlockRegistry.AirRuntimeID()
	ns := world.NativeState{Block: rid, Liquid: airRID}
	flags := featFlagComputed
	if id := ns.CanonicalID(world.DefaultBlockRegistry); id != 0 {
		if rawName, ok := gamedata.BlockStateName(id); ok {
			name := normalizeResource(rawName)
			s := rawName
			defaultFeatureNames()[rid].Store(&s)
			if name == "air" {
				flags |= featFlagExactAir | featFlagAir
			} else if treeIsAirName(rawName) {
				flags |= featFlagAir
			}
			if name == "brown_mushroom" || name == "red_mushroom" {
				flags |= featFlagMushroom
			}
			if name == "soul_sand" || name == "magma_block" {
				flags |= featFlagBubbleBase
			}
			if blockTagContains("stone_ore_replaceables", name) {
				flags |= featFlagStoneOre
			}
			if blockTagContains("deepslate_ore_replaceables", name) {
				flags |= featFlagDeepslateOre
			}
			if blockTagContains("base_stone_overworld", name) {
				flags |= featFlagBaseStone
			}
			if blockTagContains("height_specific_ore_replaceables", name) {
				flags |= featFlagHeightSpecificOre
			}
			if name == "sculk" || name == "sculk_vein" {
				flags |= featFlagSculkBehavior
			}
			if blockTagContains("fire", name) {
				flags |= featFlagFire
			}
			if blockTagContains("sculk_replaceable", name) {
				flags |= featFlagSculkReplaceable
			}
			if blockTagContains("sculk_replaceable_world_gen", name) {
				flags |= featFlagSculkReplaceableWG
			}
		}
		if shape, err := gamedata.BlockGenerationShapeID(id); err == nil {
			flags |= featFlagShapeOK | (uint32(shape.Sturdy) << featFlagSturdyShift) | (uint32(shape.Attachment) << featFlagAttachmentShift)
			if shape.Replaceable {
				flags |= featFlagReplaceable
			}
			if shape.CollisionFull {
				flags |= featFlagCollisionFull
			}
		}
	}
	if canonical, err := world.CanonicalState(world.DefaultBlockRegistry, ns); err == nil {
		if defaultFeatureNames()[rid].Load() == nil {
			s := canonical.Name
			defaultFeatureNames()[rid].Store(&s)
		}
		name := normalizeResource(canonical.Name)
		if name != "air" && name != "cave_air" && name != "void_air" {
			flags |= featFlagWorldSurface
		}
		if blockTagContains("minecraft:blocks_motion_in_heightmap", name) {
			flags |= featFlagMotion
		}
		if solid, err := gamedata.BlockIsSolid(name); err == nil && solid {
			flags |= featFlagSolid
		} else if err != nil {
			flags |= featFlagCanonicalErr
		}
	} else {
		flags |= featFlagCanonicalErr
	}
	table[rid].Store(flags)
	return flags
}

type featureTickReservation struct {
	pos      cube.Pos
	kind     chunk.ScheduledTickKind
	typeName string
}

type featureRegion struct {
	f            *ModernFeatures
	c            *chunk.Chunk
	registry     chunk.BlockRegistry
	minY, maxY   int32
	airRID       uint32
	blockCount   uint32
	isDefaultReg bool
	writeOriginX int32
	writeOriginZ int32
	target       world.ChunkPos
	// hasTarget is false for a region that records the features of an
	// origin: every position is then read from the overlay and the source.
	hasTarget bool
	// recording collects the writes and side outputs of the origin step that
	// runs while it is set.
	recording *originStep
	// recordBeyondOrigin limits recording to positions outside the origin
	// chunk: those of a piece placed beyond its chunk, whose origin chunk
	// places it itself.
	recordBeyondOrigin bool
	// structureRecording is the step the structures placed beyond their
	// chunk record into while an origin record is computed.
	structureRecording *originStep
	result             *world.GenerationResult
	// overlay holds the states written to the region.
	overlay featureOverlay
	// columns holds the overlay tops and heightmap heights of the columns,
	// by featureColumnKey.
	columns map[uint64]featureColumn
	// base is the source of the region when it is the generated terrain,
	// whose immutable chunks baseGrid and baseChunks keep once read.
	base            *modernTerrainSource
	baseGridOriginX int32
	baseGridOriginZ int32
	baseGridValid   bool
	baseGrid        [25]*chunk.Chunk
	heightsGrid     [25]*worldgenHeights
	baseChunks      map[world.ChunkPos]*chunk.Chunk
	lastBasePos     world.ChunkPos
	lastBase        *chunk.Chunk
	lastBaseMinY    int32
	lastBaseMaxY    int32
	// lastHeights holds the WORLDGEN heightmaps of the base terrain of the
	// chunk at lastHeightsPos, last read by worldgenHeight.
	lastHeightsPos          world.ChunkPos
	lastHeights             *worldgenHeights
	placementPending        []placementFrame
	placementOut            []featurePos
	currentOreName          string
	lastOreName             string
	lastOreTargets          [2]fastOreTarget
	lastOreCount            int
	lastOreAllFast          bool
	lastOreMask             uint32
	regionRandoms           map[[2]int32]*vanilla.Random
	scheduledTicks          map[featureTickReservation]struct{}
	blockActorRecords       map[cube.Pos]structure.Block
	blockActorData          map[cube.Pos]map[string]any
	nativeProjections       map[cube.Pos]struct{}
	treeDecorationPositions *treePositionSet
}

var featureRegionPool = sync.Pool{New: func() any {
	return &featureRegion{
		columns:           make(map[uint64]featureColumn, 128),
		placementPending:  make([]placementFrame, 0, 256),
		placementOut:      make([]featurePos, 0, 256),
		scheduledTicks:    make(map[featureTickReservation]struct{}),
		blockActorRecords: make(map[cube.Pos]structure.Block),
		blockActorData:    make(map[cube.Pos]map[string]any),
		nativeProjections: make(map[cube.Pos]struct{}),
		regionRandoms:     make(map[[2]int32]*vanilla.Random),
	}
}}

type recordingChunkKey struct {
	registry chunk.BlockRegistry
	r        cube.Range
}

var recordingChunkCache sync.Map // recordingChunkKey -> *chunk.Chunk

func recordingDummyChunk(registry chunk.BlockRegistry, r cube.Range) *chunk.Chunk {
	key := recordingChunkKey{registry: registry, r: r}
	if cached, ok := recordingChunkCache.Load(key); ok {
		return cached.(*chunk.Chunk)
	}
	c := chunk.New(registry, r)
	actual, _ := recordingChunkCache.LoadOrStore(key, c)
	return actual.(*chunk.Chunk)
}

func newFeatureRegion(f *ModernFeatures, c *chunk.Chunk, target world.ChunkPos, result *world.GenerationResult) *featureRegion {
	if f == nil || c == nil || result == nil {
		panic("overworld: invalid feature region")
	}
	var base *modernTerrainSource
	if source, ok := f.source.(modernTerrainSource); ok && source.generator != nil {
		base = &source
	}
	reg := c.BlockRegistry()
	cr := c.Range()
	r := featureRegionPool.Get().(*featureRegion)
	r.f = f
	r.c = c
	r.registry = reg
	r.minY, r.maxY = int32(cr[0]), int32(cr[1])
	r.airRID = reg.AirRuntimeID()
	r.blockCount = uint32(reg.BlockCount())
	r.isDefaultReg = reg == world.DefaultBlockRegistry
	r.writeOriginX, r.writeOriginZ = 0, 0
	r.target = target
	r.hasTarget = true
	r.recording = nil
	r.recordBeyondOrigin = false
	r.structureRecording = nil
	r.result = result
	r.base = base
	r.baseGridOriginX, r.baseGridOriginZ = target[0]-2, target[1]-2
	r.baseGridValid = true
	r.lastBasePos, r.lastBase = world.ChunkPos{}, nil
	r.lastBaseMinY, r.lastBaseMaxY = 0, 0
	r.lastHeightsPos, r.lastHeights = world.ChunkPos{}, nil
	r.placementPending = r.placementPending[:0]
	r.placementOut = r.placementOut[:0]
	r.currentOreName = ""
	r.lastOreName = ""
	r.treeDecorationPositions = nil
	return r
}

// release returns the pooled memory of the region. The region must not be
// used afterwards.
func (r *featureRegion) release() {
	r.overlay.release()
	if len(r.columns) > 2048 {
		r.columns = make(map[uint64]featureColumn, 128)
	} else {
		clear(r.columns)
	}
	if cap(r.placementPending) > 4096 {
		r.placementPending = make([]placementFrame, 0, 256)
	} else {
		r.placementPending = r.placementPending[:0]
	}
	if cap(r.placementOut) > 4096 {
		r.placementOut = make([]featurePos, 0, 256)
	} else {
		r.placementOut = r.placementOut[:0]
	}
	clear(r.baseChunks)
	clear(r.regionRandoms)
	clear(r.scheduledTicks)
	clear(r.blockActorRecords)
	clear(r.blockActorData)
	clear(r.nativeProjections)
	r.baseGrid = [25]*chunk.Chunk{}
	r.heightsGrid = [25]*worldgenHeights{}
	r.f = nil
	r.c = nil
	r.registry = nil
	r.recording = nil
	r.structureRecording = nil
	r.result = nil
	r.base = nil
	r.lastBase = nil
	r.lastHeights = nil
	r.currentOreName = ""
	r.lastOreName = ""
	r.lastOreTargets = [2]fastOreTarget{}
	r.treeDecorationPositions = nil
	featureRegionPool.Put(r)
}

// newRecordingRegion returns a region without a target chunk, which records
// what an origin's features write. registry and r describe the chunks the
// records are applied to.
func newRecordingRegion(f *ModernFeatures, registry chunk.BlockRegistry, r cube.Range) *featureRegion {
	region := newFeatureRegion(f, recordingDummyChunk(registry, r), world.ChunkPos{}, &world.GenerationResult{})
	region.hasTarget = false
	region.baseGridValid = false
	return region
}

func (r *featureRegion) SetOrigin(chunkX, chunkZ int32) {
	r.writeOriginX, r.writeOriginZ = chunkX, chunkZ
	if !r.hasTarget && !r.baseGridValid {
		r.baseGridOriginX, r.baseGridOriginZ = chunkX-2, chunkZ-2
		r.baseGridValid = true
	}
}

// inTarget reports whether p lies in the target chunk.
func (r *featureRegion) inTarget(p featurePos) bool {
	return r.hasTarget && featureInsideTarget(r.target, p)
}

// recordsAt reports whether side outputs at p are kept: those of the target
// chunk, or any of the origin being recorded.
func (r *featureRegion) recordsAt(p featurePos) bool {
	return r.inTarget(p) || r.recordingAt(p) != nil
}

// recordingAt returns the step recording what is written at p, if any.
func (r *featureRegion) recordingAt(p featurePos) *originStep {
	if r.recordBeyondOrigin && p.x>>4 == r.writeOriginX && p.z>>4 == r.writeOriginZ {
		return nil
	}
	return r.recording
}

// markPostProcessing marks pos for post-processing in the target chunk or in
// the record of the running origin.
func (r *featureRegion) markPostProcessing(pos cube.Pos) {
	p := featurePos{int32(pos[0]), int32(pos[1]), int32(pos[2])}
	if r.inTarget(p) {
		r.result.MarkForPostProcessing(pos, r.c.Range())
	} else if recording := r.recordingAt(p); recording != nil {
		recording.markers = append(recording.markers, pos)
	}
}

// addEntity adds an entity spawned at pos to the target chunk or to the
// record of the running origin, which spawns it for the chunk it lands in.
func (r *featureRegion) addEntity(pos cube.Pos, spawn func() *world.EntityHandle) {
	p := featurePos{int32(pos[0]), int32(pos[1]), int32(pos[2])}
	if r.inTarget(p) {
		r.result.Entities = append(r.result.Entities, spawn())
	} else if recording := r.recordingAt(p); recording != nil {
		recording.entities = append(recording.entities, recordedEntity{pos: pos, spawn: spawn})
	}
}

// scheduleTick schedules a generated tick in the target chunk or in the
// record of the running origin.
func (r *featureRegion) scheduleTick(tick chunk.ScheduledTick) {
	p := featurePos{int32(tick.Pos[0]), int32(tick.Pos[1]), int32(tick.Pos[2])}
	if r.inTarget(p) {
		r.result.ScheduleTick(tick)
	} else if recording := r.recordingAt(p); recording != nil {
		recording.ticks = append(recording.ticks, tick)
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
	if id := state.CanonicalID(r.registry); id != 0 {
		if name, properties, ok := gamedata.BlockStateView(id); ok {
			return name, properties
		}
	}
	canonicalState, err := world.CanonicalState(r.registry, state)
	if err != nil {
		panic(err)
	}
	return canonicalState.Name, canonicalState.Properties
}

func (r *featureRegion) stateName(p featurePos) string {
	state := r.nativeState(p)
	if r.isDefaultReg && state.Liquid == r.airRID && state.StateID == 0 {
		names := defaultFeatureNames()
		if int(state.Block) < len(names) {
			if ptr := names[state.Block].Load(); ptr != nil {
				return *ptr
			}
			featureMetaForDefaultRID(state.Block)
			if ptr := names[state.Block].Load(); ptr != nil {
				return *ptr
			}
		}
	}
	if id := state.CanonicalID(r.registry); id != 0 {
		if name, ok := gamedata.BlockStateName(id); ok {
			return name
		}
	}
	canonicalState, err := world.CanonicalState(r.registry, state)
	if err != nil {
		panic(err)
	}
	return canonicalState.Name
}

// StateID returns the canonical Java state ID at p, or zero for a native
// state without one.
func (r *featureRegion) StateID(p featurePos) uint32 {
	return r.nativeState(p).CanonicalID(r.registry)
}

// featureColumnKey packs the column x, z for the maps of a region.
func featureColumnKey(x, z int32) uint64 {
	return uint64(uint32(x)) | uint64(uint32(z))<<32
}

func (r *featureRegion) nativeState(p featurePos) world.NativeState {
	if p.y < r.minY || p.y > r.maxY {
		return r.f.source.BaseState(p.x, p.y, p.z)
	}
	if state, ok := r.overlay.get(p); ok {
		return state
	}
	if r.hasTarget && p.x>>4 == r.target[0] && p.z>>4 == r.target[1] {
		b, l, id := r.c.Native(uint8(p.x&15), int16(p.y), uint8(p.z&15))
		return world.NativeState{Block: b, Liquid: l, StateID: id}
	}
	return r.baseState(p)
}

// baseState returns the state of the base terrain at p.
func (r *featureRegion) baseState(p featurePos) world.NativeState {
	if r.base == nil {
		return r.f.source.BaseState(p.x, p.y, p.z)
	}
	pos := world.ChunkPos{p.x >> 4, p.z >> 4}
	c := r.lastBase
	if c == nil || r.lastBasePos != pos {
		c = r.baseChunkSlow(pos)
	}
	if p.y < r.lastBaseMinY || p.y > r.lastBaseMaxY {
		return r.f.source.BaseState(p.x, p.y, p.z)
	}
	b, l, id := c.Native(uint8(p.x&15), int16(p.y), uint8(p.z&15))
	return world.NativeState{Block: b, Liquid: l, StateID: id}
}

// baseChunk returns the base chunk at pos, looking it up in the source once
// for the region.
func (r *featureRegion) baseChunk(pos world.ChunkPos) *chunk.Chunk {
	if r.lastBase != nil && r.lastBasePos == pos {
		return r.lastBase
	}
	return r.baseChunkSlow(pos)
}

func (r *featureRegion) baseChunkSlow(pos world.ChunkPos) *chunk.Chunk {
	var c *chunk.Chunk
	if r.baseGridValid {
		gx, gz := uint32(pos[0]-r.baseGridOriginX), uint32(pos[1]-r.baseGridOriginZ)
		if gx < 5 && gz < 5 {
			idx := gx*5 + gz
			c = r.baseGrid[idx]
			if c == nil {
				c = r.base.BaseChunk(pos)
				r.baseGrid[idx] = c
			}
			cr := c.Range()
			r.lastBasePos, r.lastBase, r.lastBaseMinY, r.lastBaseMaxY = pos, c, int32(cr[0]), int32(cr[1])
			return c
		}
	}
	var ok bool
	c, ok = r.baseChunks[pos]
	if !ok {
		c = r.base.BaseChunk(pos)
		if r.baseChunks == nil {
			r.baseChunks = make(map[world.ChunkPos]*chunk.Chunk)
		}
		r.baseChunks[pos] = c
	}
	cr := c.Range()
	r.lastBasePos, r.lastBase, r.lastBaseMinY, r.lastBaseMaxY = pos, c, int32(cr[0]), int32(cr[1])
	return c
}

func (r *featureRegion) Block(p featurePos) world.Block {
	state := r.nativeState(p)
	registry, ok := r.registry.(world.BlockRegistry)
	if !ok {
		panic("overworld: feature region registry cannot resolve native blocks")
	}
	block, found := registry.BlockByRuntimeID(state.Block)
	if !found {
		panic("overworld: feature region encountered unknown primary block runtime ID")
	}
	return block
}

func (r *featureRegion) baseBlockName(p featurePos) string {
	state := r.nativeState(p)
	if r.isDefaultReg {
		name, _, found := world.DefaultBlockRegistry.RuntimeIDToStateView(state.Block)
		if !found {
			panic("overworld: feature region encountered unknown primary block runtime ID")
		}
		return name
	}
	name, _ := r.Block(p).EncodeBlock()
	return name
}

func (r *featureRegion) Set(p featurePos, state world.NativeState) bool {
	return r.setWithFlags(p, state, 3)
}

func (r *featureRegion) withinWriteArea(p featurePos) bool {
	return absFeatureInt(p.x>>4-r.writeOriginX) <= 1 && absFeatureInt(p.z>>4-r.writeOriginZ) <= 1
}

func (r *featureRegion) noteOverlayAndInvalidateHeight(p featurePos, inTarget bool) {
	needOverlayTop := !inTarget
	if needOverlayTop && r.base != nil {
		if h := r.cachedWorldgenHeights(world.ChunkPos{p.x >> 4, p.z >> 4}); h != nil {
			if p.y < int32(h.height[0][(p.x&15)|(p.z&15)<<4]) {
				needOverlayTop = false
			}
		}
	}
	if !needOverlayTop {
		if len(r.columns) == 0 {
			return
		}
		key := featureColumnKey(p.x, p.z)
		if column, ok := r.columns[key]; ok && column.known&^heightmapFrozenKinds != 0 {
			column.known &= heightmapFrozenKinds
			r.columns[key] = column
		}
		return
	}
	key := featureColumnKey(p.x, p.z)
	column, ok := r.columns[key]
	dirty := false
	if !ok || !column.hasOverlay || column.overlayTop < p.y {
		column.overlayTop, column.hasOverlay = p.y, true
		dirty = true
	}
	if column.known&^heightmapFrozenKinds != 0 {
		column.known &= heightmapFrozenKinds
		dirty = true
	}
	if dirty {
		if r.columns == nil {
			r.columns = make(map[uint64]featureColumn, 128)
		}
		r.columns[key] = column
	}
}

func (r *featureRegion) setWithFlags(p featurePos, state world.NativeState, flags int) bool {
	return r.setWithFlagsPrev(p, state, world.NativeState{}, false, flags)
}

func (r *featureRegion) setWithFlagsPrev(p featurePos, state, prev world.NativeState, hasPrev bool, flags int) bool {
	if p.y < r.minY || p.y > r.maxY {
		return false
	}
	if state.Block >= r.blockCount {
		panic("overworld: feature region cannot set unknown primary block runtime ID")
	} else if state.Liquid >= r.blockCount {
		panic("overworld: feature region cannot set unknown liquid runtime ID")
	}
	if !r.withinWriteArea(p) {
		return false
	}
	inTarget := r.hasTarget && p.x>>4 == r.target[0] && p.z>>4 == r.target[1]
	recording := r.recordingAt(p)
	recordStruct := recording == nil && r.structureRecording != nil && p.x>>4 == r.writeOriginX && p.z>>4 == r.writeOriginZ
	if len(r.nativeProjections) != 0 {
		pos := cube.Pos{int(p.x), int(p.y), int(p.z)}
		if _, projected := r.nativeProjections[pos]; projected {
			delete(r.nativeProjections, pos)
			r.removeBlockEntityPayloads(pos)
		}
	}
	hasActors := len(r.blockActorRecords) != 0 || len(r.blockActorData) != 0 || len(r.result.BlockEntities) != 0 || (recording != nil && len(recording.actors) != 0)
	previous := prev
	if !hasPrev && (recording != nil || recordStruct || hasActors) {
		previous = r.nativeState(p)
	}
	changedBlock := true
	if hasActors {
		oldID, newID := previous.CanonicalID(r.registry), state.CanonicalID(r.registry)
		oldName, _ := gamedata.BlockStateName(oldID)
		newName, _ := gamedata.BlockStateName(newID)
		changedBlock = oldName != newName
		if oldName == "" && newName == "" {
			changedBlock = previous.Block != state.Block
		}
		if changedBlock {
			r.removeBlockEntityPayloads(cube.Pos{int(p.x), int(p.y), int(p.z)})
		}
	}
	r.overlay.set(p, state)
	r.noteOverlayAndInvalidateHeight(p, inTarget)
	if inTarget {
		world.SetNativeState(r.c, uint8(p.x&15), int16(p.y), uint8(p.z&15), state)
	}
	if recording != nil {
		recording.write(p, previous, state)
	} else if recordStruct {
		r.structureRecording.writeStruct(p, previous, state)
	}
	if (inTarget || recording != nil) && flags&16 == 0 {
		r.markBlockPostProcessing(p, state)
	}
	if hasActors && !changedBlock {
		pos := cube.Pos{int(p.x), int(p.y), int(p.z)}
		if _, exists := r.blockActorRecords[pos]; exists {
			canonicalState, err := world.CanonicalState(r.registry, state)
			if err != nil {
				panic(err)
			}
			r.refreshTemplateActorState(pos, canonicalState)
		}
	}
	return true
}

func (r *featureRegion) removeBlockEntityPayloads(pos cube.Pos) {
	if len(r.blockActorRecords) != 0 {
		delete(r.blockActorRecords, pos)
	}
	if len(r.blockActorData) != 0 {
		delete(r.blockActorData, pos)
	}
	if recording := r.recordingAt(featurePos{int32(pos[0]), int32(pos[1]), int32(pos[2])}); recording != nil && len(recording.actors) != 0 {
		delete(recording.actors, pos)
	}
	if len(r.result.BlockEntities) != 0 {
		entities := r.result.BlockEntities
		kept := entities[:0]
		for _, actor := range entities {
			if actor.Pos != pos {
				kept = append(kept, actor)
			}
		}
		r.result.BlockEntities = kept
	}
}

func (r *featureRegion) Height(x, z int32, oceanFloor bool) int32 {
	return r.f.source.BaseHeight(x, z, oceanFloor)
}

func (r *featureRegion) MinY() int32 { return r.minY }
func (r *featureRegion) MaxY() int32 { return r.maxY }

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
	var state world.NativeState
	var err error
	if placement.NativeBlock == nil {
		state, err = placement.ResolveNative(r.registry)
	} else {
		state, err = placement.ResolveNative(r.registry, r.nativeState(p))
	}
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
	if r.isDefaultReg && state.Liquid == r.airRID && state.StateID == 0 {
		flags := featureMetaForDefaultRID(state.Block)
		if flags&(featFlagMushroom|featFlagBubbleBase) == 0 {
			return
		}
		if flags&featFlagMushroom != 0 {
			r.markPostProcessing(cube.Pos{int(p.x), int(p.y), int(p.z)})
			return
		}
		above := featurePos{p.x, p.y + 1, p.z}
		if !r.isAirPos(above) {
			r.markPostProcessing(cube.Pos{int(above.x), int(above.y), int(above.z)})
		}
		return
	}
	id := state.CanonicalID(r.registry)
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
		if !r.isAirPos(above) {
			r.markPostProcessing(cube.Pos{int(above.x), int(above.y), int(above.z)})
		}
	}
}

func (r *featureRegion) isAirPos(p featurePos) bool {
	ns := r.nativeState(p)
	if r.isDefaultReg && ns.Liquid == r.airRID && ns.StateID == 0 {
		if ns.Block == r.airRID {
			return true
		}
		return featureMetaForDefaultRID(ns.Block)&featFlagAir != 0
	}
	name, _ := gamedata.BlockStateName(ns.CanonicalID(r.registry))
	return treeIsAirName(name)
}

func (r *featureRegion) markAboveForPostProcessing(p featurePos) {
	for offset := int32(1); offset <= 2; offset++ {
		marked := featurePos{p.x, p.y + offset, p.z}
		if r.isAirPos(marked) {
			return
		}
		r.markPostProcessing(cube.Pos{int(marked.x), int(marked.y), int(marked.z)})
	}
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
	normName := normalizeResource(name)
	config, ok := f.configured[normName]
	if !ok {
		panic("overworld: missing configured feature " + name)
	}
	if config.typeName == "ore" {
		region.currentOreName = normName
		placed := placeModernOreRegion(region, config.config, origin, random)
		region.currentOreName = ""
		return placed
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

type fastOreTarget struct {
	rid           world.NativeState
	actor         world.Block
	predicate     map[string]any
	tagFlag       uint32
	heightTagFlag uint32
	heightMin     int32
	heightMax     int32
}

func classifyOreTagName(tag string) uint32 {
	switch normalizeResource(tag) {
	case "stone_ore_replaceables":
		return featFlagStoneOre
	case "deepslate_ore_replaceables":
		return featFlagDeepslateOre
	case "base_stone_overworld":
		return featFlagBaseStone
	case "height_specific_ore_replaceables":
		return featFlagHeightSpecificOre
	default:
		return 0
	}
}

func classifyOreTagMatch(rule map[string]any) uint32 {
	typeName, _ := rule["predicate_type"].(string)
	if normalizeResource(typeName) != "tag_match" {
		return 0
	}
	tag, _ := rule["tag"].(string)
	return classifyOreTagName(tag)
}

func classifyOreRule(rule map[string]any) (tagFlag, heightTagFlag uint32, heightMin, heightMax int32) {
	if flag := classifyOreTagMatch(rule); flag != 0 {
		return flag, 0, 0, 0
	}
	typeName, _ := rule["predicate_type"].(string)
	if normalizeResource(typeName) != "any_of" {
		return 0, 0, 0, 0
	}
	branches, ok := rule["rules"].([]any)
	if !ok || len(branches) != 2 {
		return 0, 0, 0, 0
	}
	b0, ok0 := branches[0].(map[string]any)
	b1, ok1 := branches[1].(map[string]any)
	if !ok0 || !ok1 || normalizeResource(stringValue(b0["predicate_type"])) != "all_of" || normalizeResource(stringValue(b1["predicate_type"])) != "all_of" {
		return 0, 0, 0, 0
	}
	r0, ok0 := b0["rules"].([]any)
	r1, ok1 := b1["rules"].([]any)
	if !ok0 || !ok1 || len(r0) != 2 || len(r1) != 2 {
		return 0, 0, 0, 0
	}
	r00, _ := r0[0].(map[string]any)
	r01, _ := r0[1].(map[string]any)
	r10, _ := r1[0].(map[string]any)
	r11, _ := r1[1].(map[string]any)
	hFlag := classifyOreTagMatch(r00)
	if hFlag == 0 || normalizeResource(stringValue(r01["predicate_type"])) != "height_match" {
		return 0, 0, 0, 0
	}
	if normalizeResource(stringValue(r10["predicate_type"])) != "not" {
		return 0, 0, 0, 0
	}
	negInner, _ := r10["rule"].(map[string]any)
	if classifyOreTagMatch(negInner) != hFlag {
		return 0, 0, 0, 0
	}
	baseFlag := classifyOreTagMatch(r11)
	if baseFlag == 0 {
		return 0, 0, 0, 0
	}
	return baseFlag, hFlag, int32(featureNumber(r01["min_inclusive"])), int32(featureNumber(r01["max_inclusive"]))
}

func (r *featureRegion) isOreAirExposed(x, y, z int32) bool {
	for _, offset := range [6]featurePos{{y: -1}, {y: 1}, {z: -1}, {z: 1}, {x: -1}, {x: 1}} {
		if r.isAirPos(featurePos{x + offset.x, y + offset.y, z + offset.z}) {
			return true
		}
	}
	return false
}

type cachedOreTargetSet struct {
	targets      []fastOreTarget
	allFastTags  bool
	combinedMask uint32
}

func (r *featureRegion) resolveOreTargets(targets []any, origin featurePos, buf *[2]fastOreTarget) ([]fastOreTarget, bool, uint32) {
	if r.currentOreName != "" {
		if r.lastOreName == r.currentOreName {
			return r.lastOreTargets[:r.lastOreCount], r.lastOreAllFast, r.lastOreMask
		}
		if r.isDefaultReg {
			if cached, ok := r.f.oreTargets.Load(r.currentOreName); ok {
				c := cached.(cachedOreTargetSet)
				return c.targets, c.allFastTags, c.combinedMask
			}
		}
	}
	oreTargets := buf[:0]
	if len(targets) > len(buf) {
		oreTargets = make([]fastOreTarget, 0, len(targets))
	}
	allFastTags := true
	cacheable := r.currentOreName != ""
	var combinedMask uint32
	for _, raw := range targets {
		entry := raw.(map[string]any)
		placement, ok := featureStateBlock(entry["state"])
		if !ok {
			panic("overworld: malformed ore target state")
		}
		state, actor := r.resolveFeatureBlock(placement, origin)
		if actor != nil {
			cacheable = false
		}
		predicate, ok := entry["target"].(map[string]any)
		if !ok {
			panic("overworld: malformed ore target predicate")
		}
		tagFlag, hFlag, hMin, hMax := classifyOreRule(predicate)
		if tagFlag == 0 {
			allFastTags = false
		}
		combinedMask |= tagFlag | hFlag
		oreTargets = append(oreTargets, fastOreTarget{rid: state, actor: actor, predicate: predicate, tagFlag: tagFlag, heightTagFlag: hFlag, heightMin: hMin, heightMax: hMax})
	}
	if cacheable {
		if r.isDefaultReg {
			copied := make([]fastOreTarget, len(oreTargets))
			copy(copied, oreTargets)
			r.f.oreTargets.Store(r.currentOreName, cachedOreTargetSet{targets: copied, allFastTags: allFastTags, combinedMask: combinedMask})
		}
		if len(oreTargets) <= len(r.lastOreTargets) {
			r.lastOreName = r.currentOreName
			r.lastOreCount = len(oreTargets)
			r.lastOreAllFast = allFastTags
			r.lastOreMask = combinedMask
			copy(r.lastOreTargets[:], oreTargets)
		}
	}
	return oreTargets, allFastTags, combinedMask
}

func placeModernOreRegion(region *featureRegion, config map[string]any, origin featurePos, random *vanilla.Random) bool {
	size := int32(featureNumber(config["size"]))
	targets, ok := config["targets"].([]any)
	if !ok || size < 0 || size > 64 {
		panic("overworld: malformed ore configuration")
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
	belowSurface := region.base != nil && yStart <= region.worldgenHeight(origin.x, origin.z, 3)
	for x := xStart; x <= xStart+sizeXZ && !belowSurface; x++ {
		for z := zStart; z <= zStart+sizeXZ; z++ {
			if yStart <= region.placementHeightFast(x, z, "OCEAN_FLOOR_WG") {
				belowSurface = true
				break
			}
		}
	}
	if !belowSurface {
		return false
	}
	var targetBuffer [2]fastOreTarget
	oreTargets, allFastTags, combinedMask := region.resolveOreTargets(targets, origin, &targetBuffer)
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
	minRegionY, maxRegionY := region.minY, region.maxY
	writeMinX, writeMaxX := (region.writeOriginX-1)<<4, (region.writeOriginX+2)<<4-1
	writeMinZ, writeMaxZ := (region.writeOriginZ-1)<<4, (region.writeOriginZ+2)<<4-1
	zStride := sizeXZ * sizeY
	for i := int32(0); i < size; i++ {
		sphere := &spheres[i]
		radius := sphere.radius
		if radius < 0 {
			continue
		}
		minX := max(int32(math.Floor(sphere.x-radius)), xStart)
		minY := max(int32(math.Floor(sphere.y-radius)), yStart)
		minZ := max(int32(math.Floor(sphere.z-radius)), zStart)
		maxX := min(max(int32(math.Floor(sphere.x+radius)), minX), writeMaxX)
		maxY := min(max(int32(math.Floor(sphere.y+radius)), minY), maxRegionY)
		maxZ := min(max(int32(math.Floor(sphere.z+radius)), minZ), writeMaxZ)
		minX, minY, minZ = max(minX, writeMinX), max(minY, minRegionY), max(minZ, writeMinZ)
		for x := minX; x <= maxX; x++ {
			dx := (float64(x) + .5 - sphere.x) / radius
			dx2 := dx * dx
			if dx2 >= 1 {
				continue
			}
			for y := minY; y <= maxY; y++ {
				dy := (float64(y) + .5 - sphere.y) / radius
				dx2dy2 := dx2 + dy*dy
				if dx2dy2 >= 1 {
					continue
				}
				xyIdx := x - xStart + (y-yStart)*sizeXZ
				for z := minZ; z <= maxZ; z++ {
					index := xyIdx + (z-zStart)*zStride
					word, bit := index>>6, uint64(1)<<uint(index&63)
					if tested[word]&bit != 0 {
						continue
					}
					dz := (float64(z) + .5 - sphere.z) / radius
					if dx2dy2+dz*dz >= 1 {
						continue
					}
					tested[word] |= bit
					if region.placeOreVoxel(featurePos{x, y, z}, oreTargets, allFastTags, combinedMask, discard, random) {
						placed = true
					}
				}
			}
		}
	}
	return placed
}

func (r *featureRegion) placeOreVoxel(p featurePos, oreTargets []fastOreTarget, allFastTags bool, combinedMask uint32, discard float32, random *vanilla.Random) bool {
	ns := r.nativeState(p)
	useFast := r.isDefaultReg && ns.Liquid == r.airRID && ns.StateID == 0
	var fastMeta uint32
	if useFast {
		if ns.Block == r.airRID && allFastTags {
			return false
		}
		fastMeta = featureMetaForDefaultRID(ns.Block)
		if allFastTags && fastMeta&combinedMask == 0 {
			return false
		}
	}
	var stateID uint32
	hasStateID := false
	for _, target := range oreTargets {
		if useFast && target.tagFlag != 0 {
			if target.heightTagFlag != 0 && fastMeta&target.heightTagFlag != 0 {
				if p.y < target.heightMin || p.y > target.heightMax {
					continue
				}
			} else if fastMeta&target.tagFlag == 0 {
				continue
			}
		} else {
			if !hasStateID {
				stateID = ns.CanonicalID(r.registry)
				hasStateID = true
			}
			if !featureRuleMatches(p, target.predicate, stateID, random) {
				continue
			}
		}
		if discard > 0 && (discard >= 1 || random.NextFloat() < discard) && r.isOreAirExposed(p.x, p.y, p.z) {
			continue
		}
		if r.setWithFlagsPrev(p, target.rid, ns, true, 16) && target.actor != nil {
			r.recordTemplateActor(structure.Block{Pos: [3]int32{p.x, p.y, p.z}, NativeBlock: target.actor})
		}
		return true
	}
	return false
}
func placeModernDiskRegion(region *featureRegion, config map[string]any, origin featurePos, random *vanilla.Random) bool {
	radius := sampleInt(config["radius"], random)
	halfHeight := int32(featureNumber(config["half_height"]))
	predicate, ok := config["target"].(map[string]any)
	if !ok {
		panic("overworld: malformed disk target predicate")
	}
	var predArena fastPredArena
	predRoot := predArena.compile(predicate)
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
				if !predArena.eval(region, p, predRoot) {
					placedAbove = false
					continue
				}
				block, ok := sampleOptionalBlockState(region, p, config["state_provider"], random)
				if !ok {
					continue
				}
				state, actor := region.resolveFeatureBlock(block, p)
				featureSetNativeBlockFlag2(region, p, state, actor)
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

const (
	fpFallback uint8 = iota
	fpTrue
	fpSolid
	fpReplaceable
	fpSturdyFace
	fpAirTag
	fpExactAir
	fpTagFlag
	fpTagName
	fpBlockHolder
	fpNot
	fpAllOf
	fpAnyOf
)

type fastPredNode struct {
	kind       uint8
	face       cube.Face
	childCount uint8
	children   [4]uint8
	offset     [3]int32
	tagFlag    uint32
	tagName    string
	holder     any
	raw        map[string]any
}

type fastPredArena struct {
	nodes [12]fastPredNode
	count uint8
}

func (a *fastPredArena) addFallback(raw map[string]any) uint8 {
	idx := a.count
	a.count++
	a.nodes[idx] = fastPredNode{kind: fpFallback, raw: raw}
	return idx
}

func parsePredOffset(predicate map[string]any) ([3]int32, bool) {
	var offset [3]int32
	raw, exists := predicate["offset"]
	if !exists {
		return offset, true
	}
	values, ok := raw.([]any)
	if !ok || len(values) != 3 {
		return offset, false
	}
	for i, v := range values {
		n, ok := v.(float64)
		if !ok || n < -16 || n > 16 || n != math.Trunc(n) {
			return offset, false
		}
		offset[i] = int32(n)
	}
	return offset, true
}

func (a *fastPredArena) compileList(kind uint8, predicate map[string]any) fastPredNode {
	values, ok := predicate["predicates"].([]any)
	if !ok || len(values) > 4 || int(a.count)+len(values) > len(a.nodes) {
		return fastPredNode{kind: fpFallback, raw: predicate}
	}
	node := fastPredNode{kind: kind, childCount: uint8(len(values))}
	for i, v := range values {
		childMap, ok := v.(map[string]any)
		if !ok {
			return fastPredNode{kind: fpFallback, raw: predicate}
		}
		cIdx := a.compile(childMap)
		if cIdx == 0xFF {
			return fastPredNode{kind: fpFallback, raw: predicate}
		}
		node.children[i] = cIdx
	}
	return node
}

func (a *fastPredArena) compile(predicate map[string]any) uint8 {
	if int(a.count) >= len(a.nodes) {
		return 0xFF
	}
	typeName, ok := predicate["type"].(string)
	if !ok {
		return a.addFallback(predicate)
	}
	kind := normalizeResource(typeName)
	var offset [3]int32
	switch kind {
	case "matching_blocks", "matching_block_tag", "has_sturdy_face", "solid", "replaceable":
		offset, ok = parsePredOffset(predicate)
		if !ok {
			return a.addFallback(predicate)
		}
	}
	idx := a.count
	a.count++
	switch kind {
	case "true":
		a.nodes[idx] = fastPredNode{kind: fpTrue}
	case "solid":
		a.nodes[idx] = fastPredNode{kind: fpSolid, offset: offset}
	case "replaceable":
		a.nodes[idx] = fastPredNode{kind: fpReplaceable, offset: offset}
	case "has_sturdy_face":
		dir, ok := predicate["direction"].(string)
		if !ok {
			a.nodes[idx] = fastPredNode{kind: fpFallback, raw: predicate}
			return idx
		}
		face, ok := parseSturdyFace(dir)
		if !ok {
			a.nodes[idx] = fastPredNode{kind: fpFallback, raw: predicate}
			return idx
		}
		a.nodes[idx] = fastPredNode{kind: fpSturdyFace, face: face, offset: offset}
	case "matching_block_tag":
		tag, ok := predicate["tag"].(string)
		if !ok {
			a.nodes[idx] = fastPredNode{kind: fpFallback, raw: predicate}
			return idx
		}
		a.nodes[idx] = compileTagNode(tag, offset)
	case "matching_blocks":
		holder := predicate["blocks"]
		if s, ok := holder.(string); ok {
			if tag, isTag := strings.CutPrefix(s, "#"); isTag {
				a.nodes[idx] = compileTagNode(tag, offset)
				return idx
			}
			if s == "minecraft:air" || s == "air" {
				a.nodes[idx] = fastPredNode{kind: fpExactAir, offset: offset}
				return idx
			}
		}
		a.nodes[idx] = fastPredNode{kind: fpBlockHolder, holder: holder, offset: offset}
	case "not":
		child, ok := predicate["predicate"].(map[string]any)
		if !ok || int(a.count) >= len(a.nodes) {
			a.nodes[idx] = fastPredNode{kind: fpFallback, raw: predicate}
			return idx
		}
		cIdx := a.compile(child)
		if cIdx == 0xFF {
			a.nodes[idx] = fastPredNode{kind: fpFallback, raw: predicate}
			return idx
		}
		a.nodes[idx] = fastPredNode{kind: fpNot, childCount: 1, children: [4]uint8{cIdx}}
	case "all_of":
		a.nodes[idx] = a.compileList(fpAllOf, predicate)
	case "any_of":
		a.nodes[idx] = a.compileList(fpAnyOf, predicate)
	default:
		a.nodes[idx] = fastPredNode{kind: fpFallback, raw: predicate}
	}
	return idx
}

func parseSturdyFace(direction string) (cube.Face, bool) {
	switch direction {
	case "down":
		return cube.FaceDown, true
	case "up":
		return cube.FaceUp, true
	case "north":
		return cube.FaceNorth, true
	case "south":
		return cube.FaceSouth, true
	case "west":
		return cube.FaceWest, true
	case "east":
		return cube.FaceEast, true
	default:
		return 0, false
	}
}

func compileTagNode(tag string, offset [3]int32) fastPredNode {
	norm := normalizeResource(tag)
	if norm == "air" {
		return fastPredNode{kind: fpAirTag, offset: offset}
	}
	if flag := classifyOreTagName(norm); flag != 0 {
		return fastPredNode{kind: fpTagFlag, tagFlag: flag, tagName: norm, offset: offset}
	}
	return fastPredNode{kind: fpTagName, tagName: norm, offset: offset}
}

func (a *fastPredArena) eval(region *featureRegion, p featurePos, idx uint8) bool {
	n := &a.nodes[idx]
	switch n.kind {
	case fpTrue:
		return true
	case fpSolid:
		return region.isSolidPos(featurePos{p.x + n.offset[0], p.y + n.offset[1], p.z + n.offset[2]})
	case fpReplaceable:
		return region.isReplaceablePos(featurePos{p.x + n.offset[0], p.y + n.offset[1], p.z + n.offset[2]})
	case fpSturdyFace:
		return region.hasSturdyFacePos(featurePos{p.x + n.offset[0], p.y + n.offset[1], p.z + n.offset[2]}, n.face)
	case fpAirTag:
		return region.isAirPos(featurePos{p.x + n.offset[0], p.y + n.offset[1], p.z + n.offset[2]})
	case fpExactAir:
		q := featurePos{p.x + n.offset[0], p.y + n.offset[1], p.z + n.offset[2]}
		if region.isDefaultReg {
			ns := region.nativeState(q)
			if ns.Liquid == region.airRID && ns.StateID == 0 {
				return ns.Block == region.airRID || featureMetaForDefaultRID(ns.Block)&featFlagExactAir != 0
			}
		}
		return normalizeResource(region.stateName(q)) == "air"
	case fpTagFlag:
		q := featurePos{p.x + n.offset[0], p.y + n.offset[1], p.z + n.offset[2]}
		if region.isDefaultReg {
			ns := region.nativeState(q)
			if ns.Liquid == region.airRID && ns.StateID == 0 {
				return featureMetaForDefaultRID(ns.Block)&n.tagFlag != 0
			}
		}
		return blockTagContains(n.tagName, normalizeResource(region.stateName(q)))
	case fpTagName:
		q := featurePos{p.x + n.offset[0], p.y + n.offset[1], p.z + n.offset[2]}
		return blockTagContains(n.tagName, normalizeResource(region.stateName(q)))
	case fpBlockHolder:
		q := featurePos{p.x + n.offset[0], p.y + n.offset[1], p.z + n.offset[2]}
		return featureBlockHolderContains(n.holder, normalizeResource(region.stateName(q)))
	case fpNot:
		return !a.eval(region, p, n.children[0])
	case fpAllOf:
		for i := uint8(0); i < n.childCount; i++ {
			if !a.eval(region, p, n.children[i]) {
				return false
			}
		}
		return true
	case fpAnyOf:
		for i := uint8(0); i < n.childCount; i++ {
			if a.eval(region, p, n.children[i]) {
				return true
			}
		}
		return false
	default:
		return featurePredicate(region, p, n.raw)
	}
}

func environmentScanSingle(region *featureRegion, origin featurePos, modifier map[string]any, minY, maxY int32, out []featurePos) []featurePos {
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
	var allowedArena fastPredArena
	var allowedRoot uint8
	if hasAllowed {
		allowedRoot = allowedArena.compile(allowed)
		if !allowedArena.eval(region, origin, allowedRoot) {
			return out
		}
	}
	var targetArena fastPredArena
	targetRoot := targetArena.compile(target)
	pos := origin
	found, outside := false, false
	for range maxSteps {
		if targetArena.eval(region, pos, targetRoot) {
			out = append(out, pos)
			found = true
			break
		}
		pos.y += step
		if pos.y < minY || pos.y >= maxY {
			outside = true
			break
		}
		if hasAllowed && !allowedArena.eval(region, pos, allowedRoot) {
			break
		}
	}
	if !found && !outside && targetArena.eval(region, pos, targetRoot) {
		out = append(out, pos)
	}
	return out
}

func environmentScanPlacement(region *featureRegion, in []featurePos, modifier map[string]any, minY, maxY int32) []featurePos {
	out := make([]featurePos, 0, len(in))
	for _, origin := range in {
		out = environmentScanSingle(region, origin, modifier, minY, maxY, out)
	}
	return out
}

func featureFluidName(region *featureRegion, p featurePos, properties map[string]any) string {
	state := region.nativeState(p)
	registry := region.registry
	if state.Liquid != region.airRID {
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

func (r *featureRegion) isSolidPos(p featurePos) bool {
	ns := r.nativeState(p)
	if r.isDefaultReg && ns.Liquid == r.airRID && ns.StateID == 0 {
		flags := featureMetaForDefaultRID(ns.Block)
		if flags&featFlagCanonicalErr == 0 {
			return flags&featFlagSolid != 0
		}
	}
	return featureSolid(normalizeResource(r.stateName(p)))
}

func (r *featureRegion) hasSturdyFacePos(p featurePos, face cube.Face) bool {
	state := r.nativeState(p)
	if r.isDefaultReg && state.Liquid == r.airRID && state.StateID == 0 {
		flags := featureMetaForDefaultRID(state.Block)
		if flags&featFlagShapeOK != 0 {
			return flags&(1<<(featFlagSturdyShift+uint(face))) != 0
		}
	}
	if id := state.CanonicalID(r.registry); id != 0 {
		shape, err := gamedata.BlockGenerationShapeID(id)
		if err != nil {
			panic(err)
		}
		return shape.Sturdy&(1<<uint(face)) != 0
	}
	canonicalState, err := world.CanonicalState(r.registry, state)
	if err != nil {
		panic(err)
	}
	shape, err := gamedata.BlockGenerationShape(normalizeResource(canonicalState.Name), canonicalState.Properties)
	if err != nil {
		panic(err)
	}
	return shape.Sturdy&(1<<uint(face)) != 0
}

func (r *featureRegion) isReplaceablePos(p featurePos) bool {
	state := r.nativeState(p)
	if r.isDefaultReg && state.Liquid == r.airRID && state.StateID == 0 {
		flags := featureMetaForDefaultRID(state.Block)
		if flags&featFlagShapeOK != 0 {
			return flags&featFlagReplaceable != 0
		}
	}
	shape, err := gamedata.BlockGenerationShapeID(state.CanonicalID(r.registry))
	if err != nil {
		panic(err)
	}
	return shape.Replaceable
}

func (r *featureRegion) matchesBlockTag(p featurePos, tag string) bool {
	normTag := normalizeResource(tag)
	var flag uint32
	switch normTag {
	case "air":
		return r.isAirPos(p)
	case "stone_ore_replaceables":
		flag = featFlagStoneOre
	case "deepslate_ore_replaceables":
		flag = featFlagDeepslateOre
	case "base_stone_overworld":
		flag = featFlagBaseStone
	case "height_specific_ore_replaceables":
		flag = featFlagHeightSpecificOre
	}
	if flag != 0 && r.isDefaultReg {
		ns := r.nativeState(p)
		if ns.Liquid == r.airRID && ns.StateID == 0 {
			return featureMetaForDefaultRID(ns.Block)&flag != 0
		}
	}
	return blockTagContains(normTag, normalizeResource(r.stateName(p)))
}

func (r *featureRegion) matchesBlockHolder(p featurePos, holder any) bool {
	if s, ok := holder.(string); ok {
		if tag, isTag := strings.CutPrefix(s, "#"); isTag {
			return r.matchesBlockTag(p, tag)
		}
		if (s == "minecraft:air" || s == "air") && r.isDefaultReg {
			ns := r.nativeState(p)
			if ns.Liquid == r.airRID && ns.StateID == 0 {
				if ns.Block == r.airRID {
					return true
				}
				return featureMetaForDefaultRID(ns.Block)&featFlagExactAir != 0
			}
		}
	}
	return featureBlockHolderContains(holder, normalizeResource(r.stateName(p)))
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
		return p.y >= region.minY && p.y <= region.maxY
	case "height_range":
		seaLevel := region.f.source.SeaLevel()
		minY := featureBound(predicate["min_inclusive"], region.minY, region.maxY+1, seaLevel)
		maxY := featureBound(predicate["max_inclusive"], region.minY, region.maxY+1, seaLevel)
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
		return region.isReplaceablePos(p)
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
	case "solid":
		return region.isSolidPos(p)
	case "matching_blocks":
		return region.matchesBlockHolder(p, predicate["blocks"])
	case "matching_block_tag":
		tag, ok := predicate["tag"].(string)
		if !ok {
			panic("overworld: malformed matching_block_tag predicate")
		}
		return region.matchesBlockTag(p, tag)
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
		return region.hasSturdyFacePos(p, face)
	case "matching_fluids":
		fluids, exists := predicate["fluids"]
		if !exists {
			panic("overworld: matching_fluids predicate has no fluid holder set")
		}
		_, properties := region.State(p)
		return featureFluidHolderContains(fluids, featureFluidName(region, p, properties))
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
