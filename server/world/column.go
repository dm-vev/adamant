package world

import (
	"slices"
	"sync"
	"sync/atomic"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

type columnRef struct {
	pos ChunkPos
	col *Column
}

// needsSave reports whether c may hold changes the provider lacks. Entities
// and block entities change without marking their column modified, as an
// entity walking, the contents of a chest or a furnace smelting do, so a
// column holding any is saved every time.
func (c *Column) needsSave() bool {
	return c.modified || len(c.Entities) != 0 || len(c.BlockEntities) != 0
}

// compact compacts the palettes of the column unless no block changed since
// it was last compacted.
func (c *Column) compact() {
	if c.compactedVersion == c.blockVersion {
		return
	}
	c.Compact()
	c.compactedVersion = c.blockVersion
}

type Column struct {
	// modified is set when a block, biome, entity or scheduled tick of the
	// column changes, and cleared once the column is saved. savedIn numbers
	// that save (World.saves): if writing it fails, the column is modified
	// again, unless it was saved again since.
	modified bool
	savedIn  uint64
	// complete is set on a column read from the provider or generated in
	// full. A column whose generation was cut short, by the world closing or
	// a panic of the generator, holds air and is not complete, nor is the
	// one standing in for a column that failed to load.
	complete bool
	// loadErr is the error loading the column from the provider failed
	// with, if the column is the empty one standing in for it. Such a column
	// is never saved: it would overwrite what the provider holds, which may
	// still be recovered, with air. It wraps errGenerationPanicked for a
	// column whose generation panicked, which is not saved either: it
	// would keep what the generator made of it so far, instead of being
	// generated again. loadErrLogged is set once that was logged.
	loadErr       error
	loadErrLogged bool
	InhabitedTime uint64
	// Native LevelChunk::mSnowRandomValue is independent of the level RNG
	// and initialized from a fixed constant by the chunk constructor.
	snowRandomValue uint32
	// poisIndexed is set once the POIs of the column were indexed after it
	// became ready. Block writes keep them current from then on.
	poisIndexed bool
	// circuitsIndexed records installation of transient circuit components
	// from this column's stored or generated block permutations.
	circuitsIndexed bool
	// settled is set once the column is ready, lit, indexed and its
	// generation result consumed, so reads need no more setup.
	settled bool
	// blockVersion changes whenever a block of the column changes. Versions
	// come from one clock for every world, so a version is never reused, not
	// even by a later column at the same position or in another world.
	blockVersion uint64
	// compactedVersion is the block version the column was last compacted
	// at: compacting it again before a block changes would change nothing.
	compactedVersion uint64
	// surface caches the highest blocks of the column.
	surface *surfaceHeights
	// entityTickingNeighbours and blockTickingNeighbours are set once the
	// chunks within two and within one of the column were all found ready,
	// until one of them leaves.
	entityTickingNeighbours bool
	blockTickingNeighbours  bool
	// entityTickingStamp is the entityTickingStamp of the world, plus one,
	// of the last tick the entities of the column ticked in.
	entityTickingStamp uint64
	// fillingLight is set on a column read from the provider whose light is
	// filled on another goroutine. Closing it waits for that, rather than
	// dropping the entities read with it as it drops those of a column still
	// generating.
	fillingLight bool
	// generation is written by the worker before markReady and consumed once
	// by the world owner before the column is published or saved.
	generation GenerationResult
	// postProcessing survives FULL publication and is consumed only when the
	// centre and its eight neighbours are ready for block ticking.
	postProcessing [][]uint16

	*chunk.Chunk
	Entities      []*EntityHandle
	entityIndices map[*EntityHandle]int
	// entityIterators counts the iterations over Entities in progress. While
	// it is positive, changes copy the slice first, so iterations walk the
	// list as it was when they began without copying it themselves.
	entityIterators                 int
	BlockEntities                   map[cube.Pos]Block
	missingBlockStates              map[cube.Pos]pendingMissingBlockState
	randomTickSubChunksDirty        bool
	cachedRandomTickSubChunkIndices []int
	tickerBlockEntitiesDirty        bool
	cachedTickerBlockEntities       []blockEntityTickRef
	tickerBlockEntityVersions       map[cube.Pos]uint64
	nextTickerBlockEntityVersion    uint64
	// faultyActors holds what the column keeps of the block entities whose
	// ticks panicked, by position.
	faultyActors                    map[cube.Pos]*actorFault
	subChunkHeightMaps              map[int16]cachedSubChunkHeightMap
	networkBiomePayload             cachedBlockEntityPayload
	limitedChunkPayload             cachedBlockEntityPayload
	chunkBlockEntityPayload         cachedBlockEntityPayload
	chunkBlockEntityPayloadNoBorder cachedBlockEntityPayload
	subChunkBlockEntityPayloads     map[int16]cachedBlockEntityPayload

	// ownCache holds the payloads of sub-chunks and of the chunk of no
	// key, Cache(nil), and caches those of the keys adapters asked for.
	ownCache ColumnCache
	caches   map[*SlotKey]*ColumnCache

	viewers map[Viewer]struct{}
	loaders []*Loader
	// pendingBlocks are the changes of blocks the viewers are yet to be
	// shown.
	pendingBlocks pendingBlockUpdates

	// generationState is that of the generation of a column not read from
	// the provider: generationQueued until a worker starts it, or
	// generationAbandoned once nothing wanted it before one did.
	generationState atomic.Uint32

	ready           atomic.Bool
	readyCh         chan struct{}
	lightOnce       sync.Once
	lightSpreadOnce sync.Once
	lightReady      atomic.Bool
}

// newColumn returns a new Column wrapper around the chunk.Chunk passed.
func newColumn(c *chunk.Chunk) *Column {
	return &Column{
		blockVersion:             blockVersionClock.Add(1),
		Chunk:                    c,
		snowRandomValue:          0x283ae83,
		BlockEntities:            map[cube.Pos]Block{},
		randomTickSubChunksDirty: true,
		tickerBlockEntitiesDirty: true,
		readyCh:                  make(chan struct{}),
		viewers:                  make(map[Viewer]struct{}),
	}
}

// newQueuedColumn returns a Column to be generated. The worker that generates
// it gives it its chunk: a column waiting in the queue holds none.
func newQueuedColumn() *Column {
	c := newColumn(nil)
	c.generationState.Store(generationQueued)
	return c
}

// blockVersionClock hands out the block versions of columns.
var blockVersionClock atomic.Uint64

// SetBlock sets the runtime ID of a block of the column and gives the column
// a new block version.
func (c *Column) SetBlock(x uint8, y int16, z uint8, layer uint8, rid uint32) {
	c.blockVersion = blockVersionClock.Add(1)
	c.Chunk.SetBlock(x, y, z, layer, rid)
	if layer == 0 {
		c.surfaceBlockSet(x, y, z, rid)
	}
}

// addEntity appends an entity handle to the column and tracks its slot for O(1) removal.
func (c *Column) addEntity(handle *EntityHandle) {
	if c.entityIndices == nil {
		c.entityIndices = make(map[*EntityHandle]int, 4)
	}
	c.detachIteratedEntities()
	c.entityIndices[handle] = len(c.Entities)
	c.Entities = append(c.Entities, handle)
}

// detachIteratedEntities gives the column a copy of its entity list before
// a change while iterations walk the list.
func (c *Column) detachIteratedEntities() {
	if c.entityIterators > 0 {
		c.Entities = slices.Clone(c.Entities)
	}
}

// iterateEntities calls f for each entity in the column as it was when the
// iteration began, until f returns false. It reports whether f never did.
func (c *Column) iterateEntities(f func(handle *EntityHandle) bool) bool {
	c.entityIterators++
	defer func() { c.entityIterators-- }()
	for _, handle := range c.Entities {
		if !f(handle) {
			return false
		}
	}
	return true
}

// removeEntity removes an entity handle from the column in O(1).
func (c *Column) removeEntity(handle *EntityHandle) bool {
	if len(c.Entities) == 0 {
		return false
	}
	index, ok := c.entityIndices[handle]
	if !ok {
		return false
	}

	c.detachIteratedEntities()
	last := len(c.Entities) - 1
	lastHandle := c.Entities[last]
	c.Entities[index] = lastHandle
	c.Entities[last] = nil
	c.Entities = c.Entities[:last]
	delete(c.entityIndices, handle)

	if index != last {
		c.entityIndices[lastHandle] = index
	}
	if len(c.Entities) == 0 {
		clear(c.entityIndices)
	}
	return true
}

// resetEntities drops entity references and indices from the column.
func (c *Column) resetEntities() {
	if c.entityIterators > 0 {
		c.Entities = nil
	} else {
		clear(c.Entities)
		c.Entities = c.Entities[:0]
	}
	if len(c.entityIndices) > 0 {
		clear(c.entityIndices)
	}
}

func (c *Column) invalidateRandomTickSubChunks() {
	c.randomTickSubChunksDirty = true
}

// updateBlockEntity preserves actor insertion order across ordinary state/NBT
// updates. Native storage appends actors and erases them without swapping the
// last entry into the hole; its tick pass shuffles a copy of this vector.
func (c *Column) updateBlockEntity(pos cube.Pos, b Block) {
	old := c.BlockEntities[pos]
	// Native storage shuffles every block actor, including actors whose
	// virtual tick does no work. Filtering out barrels/droppers here changes
	// the shared RNG stream even though they need no Go Tick callback.
	hadActor, hasActor := old != nil, b != nil
	if hadActor || hasActor {
		c.tickerBlockEntityPositions()
		oldKind, newKind := uint64(0), uint64(0)
		if old != nil {
			oldKind, _ = old.Hash()
		}
		if b != nil {
			newKind, _ = b.Hash()
		}
		if hadActor && (!hasActor || oldKind != newKind) {
			c.cachedTickerBlockEntities = slices.DeleteFunc(c.cachedTickerBlockEntities, func(ref blockEntityTickRef) bool { return ref.pos == pos })
			delete(c.tickerBlockEntityVersions, pos)
		}
		if hasActor && (!hadActor || oldKind != newKind) {
			c.cachedTickerBlockEntities = append(c.cachedTickerBlockEntities, c.newTickerBlockEntityRef(pos))
		}
	}
	if b == nil {
		delete(c.BlockEntities, pos)
	} else {
		c.BlockEntities[pos] = b
	}
}

func (c *Column) invalidateSubChunkHeightMaps() {
	clear(c.subChunkHeightMaps)
}

func (c *Column) invalidateNetworkBiomePayload() {
	c.networkBiomePayload = cachedBlockEntityPayload{}
	c.limitedChunkPayload = cachedBlockEntityPayload{}
	c.invalidateKeyedPayloads(false)
}

func (c *Column) invalidateNetworkSubChunkPayloads() {
	c.invalidateKeyedPayloads(true)
}

func (c *Column) invalidateBlockEntityPayloads() {
	c.chunkBlockEntityPayload = cachedBlockEntityPayload{}
	c.chunkBlockEntityPayloadNoBorder = cachedBlockEntityPayload{}
	clear(c.subChunkBlockEntityPayloads)
	c.invalidateKeyedPayloads(false)
}

// invalidateKeyedPayloads clears the payload of the chunk of every key, and
// those of its sub-chunks if subChunks is set.
func (c *Column) invalidateKeyedPayloads(subChunks bool) {
	c.ownCache.invalidate(subChunks)
	for _, cache := range c.caches {
		cache.invalidate(subChunks)
	}
}

// CachedSubChunkHeightMap returns cached height map data for a sub-chunk.
func (c *Column) CachedSubChunkHeightMap(ind int16) (byte, []int8, bool) {
	if c.subChunkHeightMaps == nil {
		return 0, nil, false
	}
	heightMap, ok := c.subChunkHeightMaps[ind]
	return heightMap.mapType, heightMap.mapData, ok && heightMap.ready
}

// CacheSubChunkHeightMap stores height map data for a sub-chunk.
func (c *Column) CacheSubChunkHeightMap(ind int16, mapType byte, mapData []int8) {
	if c.subChunkHeightMaps == nil {
		c.subChunkHeightMaps = make(map[int16]cachedSubChunkHeightMap, 4)
	}
	c.subChunkHeightMaps[ind] = cachedSubChunkHeightMap{
		mapType: mapType,
		mapData: mapData,
		ready:   true,
	}
}

// CachedNetworkBiomePayload returns the cached network biome payload for the column.
func (c *Column) CachedNetworkBiomePayload() ([]byte, bool) {
	return c.networkBiomePayload.payload, c.networkBiomePayload.ready
}

// CacheNetworkBiomePayload stores the network biome payload for the column.
func (c *Column) CacheNetworkBiomePayload(payload []byte) {
	c.networkBiomePayload = cachedBlockEntityPayload{payload: payload, ready: true}
}

// CachedNetworkSubChunkPayload returns the cached encoded network payload
// for a sub-chunk, that of Cache(nil).
func (c *Column) CachedNetworkSubChunkPayload(ind int16) ([]byte, bool) {
	return c.ownCache.CachedNetworkSubChunkPayload(ind)
}

// CacheNetworkSubChunkPayload stores the encoded network payload for a
// sub-chunk, that of Cache(nil).
func (c *Column) CacheNetworkSubChunkPayload(ind int16, payload []byte) {
	c.ownCache.CacheNetworkSubChunkPayload(ind, payload)
}

// CachedLimitedChunkPayload returns the cached limited chunk raw payload.
func (c *Column) CachedLimitedChunkPayload() ([]byte, bool) {
	return c.limitedChunkPayload.payload, c.limitedChunkPayload.ready
}

// CacheLimitedChunkPayload stores the limited chunk raw payload.
func (c *Column) CacheLimitedChunkPayload(payload []byte) {
	c.limitedChunkPayload = cachedBlockEntityPayload{payload: payload, ready: true}
}

// CachedFullChunkPayload returns the cached full chunk raw payload, that
// of Cache(nil).
func (c *Column) CachedFullChunkPayload() ([]byte, bool) {
	return c.ownCache.CachedFullChunkPayload()
}

// CacheFullChunkPayload stores the full chunk raw payload, that of
// Cache(nil).
func (c *Column) CacheFullChunkPayload(payload []byte) {
	c.ownCache.CacheFullChunkPayload(payload)
}

// CachedChunkBlockEntityPayload returns a cached encoded block entity payload for the column.
func (c *Column) CachedChunkBlockEntityPayload(noBorder bool) ([]byte, bool) {
	if noBorder {
		return c.chunkBlockEntityPayloadNoBorder.payload, c.chunkBlockEntityPayloadNoBorder.ready
	}
	return c.chunkBlockEntityPayload.payload, c.chunkBlockEntityPayload.ready
}

// CacheChunkBlockEntityPayload stores an encoded block entity payload for the column.
func (c *Column) CacheChunkBlockEntityPayload(noBorder bool, payload []byte) {
	entry := cachedBlockEntityPayload{payload: payload, ready: true}
	if noBorder {
		c.chunkBlockEntityPayloadNoBorder = entry
		return
	}
	c.chunkBlockEntityPayload = entry
}

// CachedSubChunkBlockEntityPayload returns a cached encoded block entity payload for a sub-chunk.
func (c *Column) CachedSubChunkBlockEntityPayload(ind int16) ([]byte, bool) {
	if c.subChunkBlockEntityPayloads == nil {
		return nil, false
	}
	payload, ok := c.subChunkBlockEntityPayloads[ind]
	return payload.payload, ok && payload.ready
}

// CacheSubChunkBlockEntityPayload stores an encoded block entity payload for a sub-chunk.
func (c *Column) CacheSubChunkBlockEntityPayload(ind int16, payload []byte) {
	if c.subChunkBlockEntityPayloads == nil {
		c.subChunkBlockEntityPayloads = make(map[int16]cachedBlockEntityPayload, 4)
	}
	c.subChunkBlockEntityPayloads[ind] = cachedBlockEntityPayload{payload: payload, ready: true}
}

func (c *Column) randomTickSubChunkIndices(br BlockRegistry) []int {
	if !c.randomTickSubChunksDirty {
		return c.cachedRandomTickSubChunkIndices
	}
	indices := c.cachedRandomTickSubChunkIndices[:0]
	for i, sub := range c.Sub() {
		if sub.Empty() {
			continue
		}
		layers := sub.Layers()
		if len(layers) == 0 {
			continue
		}
		pal := layers[0].Palette()
		for pi := 0; pi < pal.Len(); pi++ {
			if rid := pal.Value(uint16(pi)); br.RandomTickBlock(rid) {
				indices = append(indices, i)
				break
			}
		}
	}
	c.cachedRandomTickSubChunkIndices = indices
	c.randomTickSubChunksDirty = false
	return c.cachedRandomTickSubChunkIndices
}

type blockEntityTickRef struct {
	pos     cube.Pos
	version uint64
}

func (c *Column) newTickerBlockEntityRef(pos cube.Pos) blockEntityTickRef {
	if c.tickerBlockEntityVersions == nil {
		c.tickerBlockEntityVersions = make(map[cube.Pos]uint64)
	}
	c.nextTickerBlockEntityVersion++
	c.tickerBlockEntityVersions[pos] = c.nextTickerBlockEntityVersion
	return blockEntityTickRef{pos: pos, version: c.nextTickerBlockEntityVersion}
}

func (c *Column) tickerBlockEntityPositions() []blockEntityTickRef {
	if !c.tickerBlockEntitiesDirty {
		return c.cachedTickerBlockEntities
	}
	positions := c.cachedTickerBlockEntities[:0]
	for pos := range c.BlockEntities {
		positions = append(positions, c.newTickerBlockEntityRef(pos))
	}
	// Generated columns may supply only a map. Ordinary placement and decoded
	// actor lists use updateBlockEntity and retain their explicit insertion order.
	slices.SortFunc(positions, func(a, b blockEntityTickRef) int { return compareBlockPos(a.pos, b.pos) })
	c.cachedTickerBlockEntities = positions
	c.tickerBlockEntitiesDirty = false
	return c.cachedTickerBlockEntities
}

type cachedBlockEntityPayload struct {
	payload []byte
	ready   bool
}

type cachedSubChunkHeightMap struct {
	mapType byte
	mapData []int8
	ready   bool
}

// The states of the generation of a column.
const (
	generationNone uint32 = iota
	generationQueued
	generationStarted
	generationAbandoned
)

// startGeneration records that a worker starts generating the column, and
// reports whether it may: not once the column was abandoned.
func (c *Column) startGeneration() bool {
	return c.generationState.Swap(generationStarted) != generationAbandoned
}

// abandonGeneration abandons the generation of a column whose task is still
// queued, and reports whether it did: a worker that takes the task then
// skips it. A column a worker started is generated in full.
func (c *Column) abandonGeneration() bool {
	return c.generationState.CompareAndSwap(generationQueued, generationAbandoned)
}

// abandoned reports whether the column's generation was abandoned.
func (c *Column) abandoned() bool {
	return c.generationState.Load() == generationAbandoned
}

// Ready reports whether the Column has finished generating.
func (c *Column) Ready() bool {
	return c.ready.Load()
}

// waitReady blocks until the Column is marked ready.
func (c *Column) waitReady() {
	if c.ready.Load() {
		return
	}
	<-c.readyCh
}

// markReady marks the Column as generated and unblocks any waiters.
func (c *Column) markReady() {
	if c.ready.Swap(true) {
		return
	}
	close(c.readyCh)
}

// fillLight calculates a column's own light. It is safe to run on a generation worker before readiness is published.
func (c *Column) fillLight(pos ChunkPos) {
	c.lightOnce.Do(func() {
		chunk.LightArea([]*chunk.Chunk{c.Chunk}, int(pos[0]), int(pos[1])).Fill()
		c.lightReady.Store(true)
	})
}

// ensureLight fills the column and spreads it to ready neighbours once on the world owner.
func (c *Column) ensureLight(w *World, pos ChunkPos) {
	c.fillLight(pos)
	c.lightSpreadOnce.Do(func() {
		w.calculateLight(pos)
	})
}
