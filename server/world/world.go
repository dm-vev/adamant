package world

import (
	"fmt"
	"log/slog"
	"maps"
	"math/rand/v2"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/scoreboard"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/google/uuid"
)

type columnRef struct {
	pos ChunkPos
	col *Column
}

type loaderActiveArea struct {
	pos      ChunkPos
	radius   int32
	radiusSq int64
	square   bool
}

// World implements a Minecraft world. It manages all aspects of what players
// can see, such as blocks, entities and particles. World generally provides a
// synchronised state: All entities, blocks and players usually operate in this
// world, so World ensures that all its methods will always be safe for
// simultaneous calls. A nil *World is safe to use but not functional.
type World struct {
	conf Config
	// log is the logger of the world; genLog and saveLog are those of its
	// generation, lighting and saves. Each is the logger of the Config
	// scoped to a component of package logging.
	log, genLog, saveLog *slog.Logger
	ra                   cube.Range

	// pendingLight holds, per chunk, the positions whose blocks changed in
	// how they affect light since the light was last brought up to date.
	pendingLight map[ChunkPos][]cube.Pos
	// pendingSpreads holds, in the order they became possible, the chunks
	// whose light is to be spread into the chunks around them, all of which
	// were lit: flushLight spreads them together.
	pendingSpreads []pendingSpread
	// deferredSpreads holds, by the chunk in their middle, the spreads of
	// light far from every column whose entities tick, which wait until a
	// column near them comes to tick or light near them is read.
	deferredSpreads map[ChunkPos]pendingSpread
	// blockUpdates holds the columns whose viewers are yet to be shown
	// changes of their blocks.
	blockUpdates blockUpdateQueue

	queue         chan transaction
	queueClosing  chan struct{}
	queueMu       sync.Mutex
	queueOverflow []transaction
	queueWake     chan struct{}
	queueing      sync.WaitGroup

	// scheduleMu serialises task scheduling against the close transitions
	// below. scheduling counts in-flight scheduled work that close must drain.
	scheduleMu sync.Mutex
	scheduling sync.WaitGroup
	// closed flips once close starts; new tasks fail with ErrWorldClosed.
	// closeAcceptingEntityTasks is true only during the close transaction, when
	// entity Close methods may still schedule final work that close drains.
	closed                    atomic.Bool
	closeAcceptingEntityTasks atomic.Bool

	// advance is a bool that specifies if this World should advance the current
	// tick, time and weather saved in the Settings struct held by the World.
	advance bool

	o sync.Once

	set         *Settings
	providerUse *providerRef
	// border is dimension-owned; Settings' mutex guards its external API.
	border worldBorder
	// tick is the per-world tick counter used by worlds that do not advance the
	// shared Settings, such as the Nether, End and custom dimensions.
	tick               int64
	tickFrozen         atomic.Bool
	stepTicks          atomic.Int64
	sprintTicks        atomic.Int64
	tickRate           atomic.Uint64
	tickTimes          tickTimes
	forcedChunksMu     sync.RWMutex
	forcedChunks       map[ChunkPos]struct{}
	forcedChunksLoaded bool
	scoreboardOnce     sync.Once
	scoreboardModel    *scoreboard.Model
	// gameRules caches persisted non-native Java gamerules.
	gameRulesMu               sync.Mutex
	gameRules                 map[string]any
	handler                   atomic.Pointer[Handler]
	structureReferenceQueryMu sync.Mutex
	// spawnStructures caches the structures overriding natural spawns in
	// each chunk.
	spawnStructures           spawnStructureCache
	structureReferenceMu      sync.Mutex
	structureReferences       map[StructureReference]struct{}
	structureReferencesLoaded bool
	structureReferencesErr    error

	weather

	// closeStarted closes as soon as World.Close begins, before the close
	// transaction runs; closing closes once the world stops ticking.
	closeStarted chan struct{}
	closing      chan struct{}
	running      sync.WaitGroup

	generatorRunning sync.WaitGroup
	// lightFills bounds how many columns read from the provider have their
	// light filled at once.
	lightFills chan struct{}
	// prefetched holds the columns being read from the provider ahead of
	// their load, by position, and readSlots bounds how many are read at
	// once: see prefetchColumn. Only the owner touches the map.
	prefetched map[ChunkPos]*columnRead
	readSlots  chan struct{}
	// releaser is the generator if it releases its caches when generation
	// idles: releaseTimer fires releaseAfter after the last chunk was
	// generated, guarded by releaseMu. See armCacheRelease.
	releaser     ReleasingGenerator
	releaseMu    sync.Mutex
	releaseTimer *time.Timer
	releaseAfter time.Duration

	// chunks holds a cache of chunks currently loaded. These chunks are cleared
	// from this map after some time of not being used.
	chunks map[ChunkPos]*Column
	// lastChunk caches the settled column most recently read at
	// lastChunkPos, as ServerChunkCache keeps its last chunks. It is cleared
	// whenever the column at a position is replaced or removed.
	lastChunkPos  ChunkPos
	lastChunk     *Column
	chunkRequests map[ChunkPos][]chunkCallback
	// unsettled holds the columns in chunks that were not settled yet when
	// last seen, so that a region phase may settle them first.
	unsettled map[ChunkPos]*Column
	regions   regionState
	// workerLocals holds the values Tx.Local and ReadView.Local keep for the
	// world owner, first, and the other workers of parallel phases.
	workerLocals []map[any]any

	// entities holds a map of entities currently loaded and metadata associated
	// with them, such as the last chunk position they were located in and a
	// cached instance of the opened entity. The cached entity instance allows us
	// to reuse the same Go object across ticks instead of re-opening it each
	// iteration, which significantly reduces pressure on the garbage collector
	// and CPU usage during the entity pipeline.
	entities map[*EntityHandle]*entityState
	// players holds the handles in entities that are players, so that
	// finding the players does not walk every entity.
	players map[*EntityHandle]struct{}
	// playerList holds the handles in players in the order they were added,
	// as Java's ServerLevel lists its players. It is replaced rather than
	// changed, so that a reader may keep it while players come and go.
	playerList []*EntityHandle
	// playerMoves counts the players coming, leaving and moving, so that
	// playerGrid is built again once they did.
	playerMoves        atomic.Uint64
	playerGrid         playerGrid
	queuedEntityTicks  []*EntityHandle
	entitiesByUUID     map[uuid.UUID]*EntityHandle
	entitiesByUniqueID map[int64]*EntityHandle
	pendingRiders      map[uuid.UUID][]*EntityHandle

	// chunkCount and entityCount track the map sizes so callers outside the world
	// goroutine can read counts without racing map mutations.
	chunkCount  atomic.Int64
	entityCount atomic.Int64

	r               *rand.Rand
	lootSequences   map[string]*vanilla.Random
	javaRandom      vanilla.Random
	javaRandomReady bool
	// gameEventBlocks caches the game event flags of block runtime IDs.
	gameEventBlocks        ridCache[uint8]
	nativeGaussianFloat    nativeGaussianFloatState
	blockTickingPass       bool
	pendingBlockTickChunks map[ChunkPos]struct{}

	// weatherRand is used for out-of-band weather changes to avoid racing the
	// main world RNG, which is owned by the tick loop.
	weatherRandMu sync.Mutex
	weatherRand   *rand.Rand

	autoSaveDisabled atomic.Bool
	tps              atomic.Uint64
	// stats is what the world counts of itself, for metrics.
	stats *Stats
	// tickEnded is when the last tick the tick loop waited for ended, in
	// nanoseconds since the Unix epoch, beside tickStarted: see TickState.
	// made is when the world was made.
	tickEnded atomic.Int64
	made      time.Time

	// Scheduled updates are partitioned by native registry: blocks drain before
	// fluids, and each queue owns ticks by their scheduled position.
	scheduledBlockTicks *scheduledTickQueue
	scheduledFluidTicks *scheduledTickQueue
	scheduledTickBlocks map[scheduledTickTypeKey]Block
	redstone            *redstoneEngine
	neighbourUpdates    neighbourUpdateQueue
	deferredNeighbours  []neighbourUpdate
	pois                map[cube.Pos]POI
	// poiClasses classifies the POI of every block state by runtime ID. It is
	// built on first use, once the block registry is final.
	poiClasses        []poiClass
	poiClassesOnce    sync.Once
	raids             map[int64]*Raid
	raidByRaider      map[uuid.UUID]int64
	nextRaidID        int64
	dragonFight       DragonFightController
	dragonArena       *Loader
	javaHeightmapTags [2]ridCache[bool]

	scratchPrefetch      []prefetchItem
	scratchRegionRefs    []columnRef
	scratchViews         []ReadView
	scratchRandom        []cube.Pos
	scratchRandomScan    []randomTickScan
	scratchRandomFound   [][]cube.Pos
	scratchBlockEntities []blockEntityTickRef
	scratchLoaderAreas   []loaderActiveArea
	scratchFocus         []ChunkPos
	// saver writes saved columns in the background; it is nil for
	// synchronous and read-only worlds, which write them at once.
	saver *chunkSaver
	// saves counts the columns saved, on the world goroutine. A column saved
	// takes the count as its savedIn, which tells a failed write of that
	// save from a later save of the column.
	saves uint64
	// captures counts the captures of the world for a save, on the world
	// goroutine. dataWritten is the count of the last capture whose data
	// was written, under dataMu: the data of a capture is written off the
	// world goroutine, and that of an older one never after a newer one.
	captures    uint64
	dataMu      sync.Mutex
	dataWritten uint64
	// holds is the number of HoldTransfers holding transfers out of the
	// world, and transfersOut the number under way, both under the mutex of
	// transfers.
	holds                        int
	transfersOut                 int
	scratchInhabitedPlayers      []inhabitedTimePlayer
	scratchPostProcessingColumns []columnRef

	activeColumns     []columnRef
	activeColumnIndex map[ChunkPos]int
	entityColumns     []columnRef
	entityColumnIndex map[ChunkPos]int

	// entityTickingStamp changes every tick. The columns whose entities
	// tick this tick hold it, plus one.
	entityTickingStamp uint64
	// entityTickingSeen holds the areas whose columns were stamped this
	// tick, so that loaders standing together stamp theirs once.
	entityTickingSeen map[loaderActiveArea]struct{}

	viewerMu      sync.Mutex
	viewers       map[*Loader]Viewer
	viewerLoaders []*Loader

	generatorQueue *generationQueue
	// generatorQueueSaturation counts how often more chunk generation tasks
	// waited than GeneratorQueueSize. We use this to rate-limit backlog
	// warnings so operators can tune the worker count.
	generatorQueueSaturation atomic.Uint64
	lastQueueSaturationLog   atomic.Uint64

	// faults holds the log of the panics recovered in the world and the
	// offenders quarantined.
	faults faults
	// guard is the tick guard of the transactions of the owner.
	guard tickGuard
	// blockFaults holds the blocks whose ticks panicked.
	blockFaults blockFaults
	// stepFaults holds the subsystems of the tick that panicked, by name.
	stepFaults map[string]*stepFault
	// tickStarted is when the tick the tick loop waits for was queued, in
	// nanoseconds since the Unix epoch, or 0 between ticks: the watchdog
	// reads it.
	tickStarted atomic.Int64
	// testTickHook, if not nil, is called by each tick that simulates,
	// before anything else of the world runs: tests make the bookkeeping
	// of the tick panic with it.
	testTickHook func(tx *Tx)
	// testClockHook, if not nil, is called by each tick that advances the
	// clock of the world, under the lock of its settings: a test panics in it.
	testClockHook func()
}

// New creates a new initialised world. The world may be used right away, but
// it will not be saved or loaded from files until it has been given a
// different provider than the default. (NopProvider) By default, the name of
// the world will be 'World'.
func New() *World {
	var conf Config
	return conf.New()
}

// Name returns the display name of the world. Generally, this name is
// displayed at the top of the player list in the pause screen in-game. If a
// provider is set, the name will be updated according to the name that it
// provides.
func (w *World) Name() string {
	if w == nil {
		return "World"
	}
	w.set.Lock()
	defer w.set.Unlock()
	return w.set.Name
}

// Seed returns the immutable generation seed selected when the World opened.
func (w *World) Seed() int64 {
	if w == nil {
		return 0
	}
	return w.conf.Seed
}

// Log returns the logger of the world, which game code that runs in it, as
// the blocks and items of a transaction, logs through.
func (w *World) Log() *slog.Logger {
	if w == nil || w.log == nil {
		return slog.Default()
	}
	return w.log
}

// Dimension returns the Dimension assigned to the World in world.New. The sky
// colour and behaviour of a variety of world features differ based on the
// Dimension.
func (w *World) Dimension() Dimension {
	if w == nil {
		return Overworld
	}
	return w.conf.Dim
}

// Range returns the range in blocks of the World (min and max). It is
// equivalent to calling World.Dimension().Range().
func (w *World) Range() cube.Range {
	if w == nil {
		return Overworld.Range()
	}
	return w.ra
}

// weatherRandIntN returns a random number for weather transitions without
// contending with the tick loop RNG.
func (w *World) weatherRandIntN(n int) int {
	w.weatherRandMu.Lock()
	defer w.weatherRandMu.Unlock()

	if w.weatherRand == nil {
		seed := uint64(time.Now().UnixNano())
		w.weatherRand = rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	}
	return w.weatherRand.IntN(n)
}

// BlockRegistry returns the BlockRegistry used by the World.
func (w *World) BlockRegistry() BlockRegistry {
	if w == nil {
		return DefaultBlockRegistry
	}
	return w.conf.Blocks
}

// EntityRegistry returns the EntityRegistry that was passed to the World's
// Config upon construction.
func (w *World) EntityRegistry() EntityRegistry {
	if w == nil {
		return EntityRegistry{}
	}
	return w.conf.Entities
}

// Handle changes the current Handler of the world. As a result, events called
// by the world will call the methods of the Handler passed. Handle sets the
// world's Handler to NopHandler if nil is passed.
func (w *World) Handle(h Handler) {
	if w == nil {
		return
	}
	if h == nil {
		h = NopHandler{}
	}
	w.handler.Store(&h)
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

// Handler returns the Handler of the world.
func (w *World) Handler() Handler {
	if w == nil {
		return NopHandler{}
	}
	h := *w.handler.Load()
	if w.inRegionPhase() {
		if _, nop := h.(NopHandler); !nop {
			return regionHandler{h: h}
		}
	}
	return h
}

// Column represents the data of a chunk including the (block) entities and
// viewers and loaders.

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

// columnTo converts a Column to a chunk.Column so that it can be written to
// a provider.
func (w *World) columnTo(col *Column, pos ChunkPos) *chunk.Column {
	scheduled := make([]chunk.SavedScheduledTick, 0, 8)
	scheduled = append(scheduled, w.scheduledBlockTicks.fromChunk(pos)...)
	scheduled = append(scheduled, w.scheduledFluidTicks.fromChunk(pos)...)
	for i := range scheduled {
		scheduled[i].NativeBlock = w.nativeScheduledTickState(col, scheduled[i].ScheduledTick)
	}
	c := &chunk.Column{
		Chunk:          col.Chunk,
		Entities:       make([]chunk.Entity, 0, len(col.Entities)),
		BlockEntities:  make([]chunk.BlockEntity, 0, len(col.BlockEntities)),
		ScheduledTicks: scheduled,
		InhabitedTime:  col.InhabitedTime,
		PostProcessing: col.postProcessing,
	}
	for _, e := range col.Entities {
		if e.t.EncodeEntity() == "minecraft:player" {
			// Player entities are persisted separately from chunk data and should not be stored in the
			// chunk provider. Keeping them out of the provider avoids stale player entities being read
			// back after a restart.
			continue
		}

		data, ok := w.saveEntityNBT(e)
		if !ok {
			continue
		}
		c.Entities = append(c.Entities, chunk.Entity{ID: e.UniqueID(), Data: data})
	}
	for pos, be := range col.BlockEntities {
		data, ok := w.saveBlockEntityNBT(pos, be)
		if !ok {
			continue
		}
		if _, actor := data["id"]; !actor {
			// A placeholder state without block actor data, such as one a
			// world holds for a state Adamant does not implement.
			continue
		}
		c.BlockEntities = append(c.BlockEntities, chunk.BlockEntity{Pos: pos, Data: BlockActorNBT(data, pos)})
	}
	// Scheduled ticks are emitted above in per-queue insertion order. Their
	// positions, rather than the generated source column, determine ownership.
	return c
}

// saveEntityNBT returns the NBT the entity of e is saved with, as EntityNBT
// does. A panic of encoding it leaves the entity out of the save of its
// column, which still saves: it is reported as a fault of saving, and false
// returned.
func (w *World) saveEntityNBT(e *EntityHandle) (data map[string]any, ok bool) {
	defer func() {
		if r := recover(); r != nil {
			f := newFault(FaultSave, r)
			f.Type, f.Pos, f.Entity = e.t.EncodeEntity(), cube.PosFromVec3(e.data.Pos), e
			w.reportFault(f, quarantineKey{})
		}
	}()
	return w.EntityNBT(e), true
}

// saveBlockEntityNBT returns the NBT of the block entity be at pos, as
// saveEntityNBT does for an entity.
func (w *World) saveBlockEntityNBT(pos cube.Pos, be Block) (data map[string]any, ok bool) {
	defer func() {
		if r := recover(); r != nil {
			f := newFault(FaultSave, r)
			f.Type, f.Pos = w.blockFaultType(be), pos
			w.reportFault(f, quarantineKey{})
		}
	}()
	return be.(NBTer).EncodeNBT(), true
}

// EntityNBT encodes the native NBT an entity is saved with, in the world
// save and in structure templates.
func (w *World) EntityNBT(e *EntityHandle) map[string]any {
	data := MergeNativeEntityNBT(e.encodeNBT(), e.t.EncodeNBT(&e.data))
	if encode := w.conf.Entities.Config().EncodeSharedNBT; encode != nil {
		maps.Copy(data, encode(&e.data))
	}
	data["identifier"] = BedrockEntityIdentifier(e.t.EncodeEntity())
	return data
}

// entityFromNBT decodes an entity of a type from the native NBT it was saved
// with, giving it the unique ID passed. It returns nil for NBT that Java
// Entity.load refuses, which drops the entity.
func (w *World) entityFromNBT(t EntityType, id int64, data map[string]any) *EntityHandle {
	return withBlockRegistryNBT(data, w.conf.Blocks, func() *EntityHandle {
		h := entityFromData(t, id, data)
		if h == nil {
			return nil
		}
		if decode := w.conf.Entities.Config().DecodeSharedNBT; decode != nil {
			decode(&h.data, data)
		}
		return h
	})
}

// nativeScheduledTickState keeps the actual native state when it still matches
// the queued Java type. A stale tick retains its old type rather than ticking a
// different block after the save is opened by BDS.
func (w *World) nativeScheduledTickState(col *Column, tick chunk.ScheduledTick) uint32 {
	withinHeight := !tick.Pos.OutOfBounds(w.Range())
	if withinHeight && tick.Kind == chunk.BlockTick {
		b := w.blockInChunk(col, tick.Pos)
		state := NativeStateAt(col.Chunk, uint8(tick.Pos[0]), int16(tick.Pos[1]), uint8(tick.Pos[2]))
		name, canonical := gamedata.BlockStateName(state.CanonicalID(w.conf.Blocks))
		if canonical && name == tick.Type || !canonical && scheduledTickType(w.conf.Blocks, b) == tick.Type {
			return w.conf.Blocks.BlockRuntimeID(b)
		}
	} else if withinHeight && tick.Kind == chunk.FluidTick {
		for layer := uint8(0); layer < 2; layer++ {
			rid := col.Block(uint8(tick.Pos[0]&15), int16(tick.Pos[1]), uint8(tick.Pos[2]&15), layer)
			b, ok := w.conf.Blocks.BlockByRuntimeID(rid)
			if liquid, isLiquid := b.(Liquid); ok && isLiquid && scheduledFluidTickMatches(liquid, tick.Type) {
				return rid
			}
		}
	}
	b, ok := w.resolveScheduledTickBlock(tick)
	if !ok {
		panic(fmt.Sprintf("save scheduled tick: unknown %v type %q", tick.Kind, tick.Type))
	}
	return w.conf.Blocks.BlockRuntimeID(b)
}

// columnFrom converts a chunk.Column to a Column after reading it from a
// provider. The Column is marked ready if ready is set.
func (w *World) columnFrom(c *chunk.Column, pos ChunkPos, ready bool) *Column {
	col := newColumn(c.Chunk)
	col.InhabitedTime = c.InhabitedTime
	col.postProcessing = c.PostProcessing
	states := c.Chunk.MissingBlockStates()
	if len(states) != 0 {
		col.missingBlockStates = make(map[cube.Pos]pendingMissingBlockState, len(states))
	}
	for _, state := range states {
		if state.Layer != 0 {
			continue
		}
		state.Position[0] += int(pos[0]) * 16
		state.Position[2] += int(pos[1]) * 16
		col.missingBlockStates[state.Position] = pendingMissingBlockState{properties: state.Properties}
	}
	col.Entities = make([]*EntityHandle, 0, len(c.Entities))
	if len(c.Entities) > 0 {
		col.entityIndices = make(map[*EntityHandle]int, len(c.Entities))
	}
	col.BlockEntities = make(map[cube.Pos]Block, len(c.BlockEntities))
	for _, e := range c.Entities {
		eid, ok := e.Data["identifier"].(string)
		if !ok {
			w.log.Error("read column: entity without identifier field", "ID", e.ID)
			continue
		}
		if eid == "minecraft:player" {
			// Players are managed separately from chunk entities, so ignore persisted player entries that
			// may have been saved by older versions.
			continue
		}
		t, ok := w.conf.Entities.Lookup(eid)
		if !ok {
			w.log.Error("read column: unknown entity type", "ID", e.ID, "type", eid)
			continue
		}
		h := w.entityFromNBT(t, e.ID, e.Data)
		if h == nil {
			w.log.Error("read column: entity has an invalid position or rotation", "ID", e.ID, "type", eid)
			continue
		}
		col.addEntity(h)
	}
	w.decodeBlockEntities(col, w.dropStaleBlockEntities(col, c.BlockEntities))
	blockTicks := make([]scheduledTick, 0, len(c.ScheduledTicks))
	fluidTicks := make([]scheduledTick, 0, len(c.ScheduledTicks))
	for _, saved := range c.ScheduledTicks {
		update := saved.ScheduledTick
		var b Block
		var ok bool
		if update.Type == "" {
			b, ok = w.conf.Blocks.BlockByRuntimeID(saved.NativeBlock)
		} else {
			b, ok = w.resolveScheduledTickBlock(update)
		}
		if !ok {
			w.log.Error("read column: cannot resolve scheduled tick type", "type", update.Type, "kind", update.Kind, "position", update.Pos)
			continue
		}
		typeName := update.Type
		if typeName == "" {
			if liquid, fluid := b.(Liquid); fluid {
				update.Kind = chunk.FluidTick
				typeName = scheduledFluidTickType(liquid)
			} else {
				typeName = scheduledTickType(w.conf.Blocks, b)
			}
		}
		tick := scheduledTick{
			pos:      update.Pos,
			btype:    typeName,
			t:        int64(saved.Delay),
			deferred: true,
			priority: update.Priority,
		}
		switch update.Kind {
		case chunk.BlockTick:
			blockTicks = append(blockTicks, tick)
		case chunk.FluidTick:
			fluidTicks = append(fluidTicks, tick)
		default:
			w.log.Error("read column: invalid scheduled tick kind", "kind", update.Kind, "position", update.Pos)
		}
	}
	w.scheduledBlockTicks.add(blockTicks)
	w.scheduledFluidTicks.add(fluidTicks)
	if ready {
		col.markReady()
	}
	return col
}

// dropStaleBlockEntities removes saved block entities of blocks that have
// none, as BDS discards block actors without a matching block. Worlds saved
// while such a block was still unimplemented hold these records. The column
// is marked modified so it is saved again without them.
func (w *World) dropStaleBlockEntities(col *Column, entities []chunk.BlockEntity) []chunk.BlockEntity {
	var kept []chunk.BlockEntity
	for i, be := range entities {
		rid := col.Chunk.Block(uint8(be.Pos[0]), int16(be.Pos[1]), uint8(be.Pos[2]), 0)
		if w.conf.Blocks.NBTBlock(rid) {
			if kept != nil {
				kept = append(kept, be)
			}
			continue
		}
		if kept == nil {
			kept = append(make([]chunk.BlockEntity, 0, len(entities)), entities[:i]...)
		}
	}
	if kept == nil {
		return entities
	}
	col.modified = true
	w.log.Debug("read column: dropped block entities of blocks without one", "count", len(entities)-len(kept))
	return kept
}

// decodeBlockEntities uses the same registry and position tracking for saved
// and generated block entities.
func (w *World) decodeBlockEntities(c *Column, entities []chunk.BlockEntity) {
	for _, be := range entities {
		rid := c.Chunk.Block(uint8(be.Pos[0]), int16(be.Pos[1]), uint8(be.Pos[2]), 0)
		b, ok := w.conf.Blocks.BlockByRuntimeID(rid)
		if !ok {
			w.log.Error("read column: no block with runtime ID", "ID", rid)
			continue
		}
		nb, ok := b.(NBTer)
		if !ok {
			w.log.Error("read column: block with nbt does not implement NBTer", "block", fmt.Sprintf("%#v", b))
			continue
		}
		decoded := DecodeNBT(nb, be.Data, w.conf.Blocks).(Block)
		decoded = restoreJavaBlockActor(c.Chunk, be.Pos, decoded)
		if tracked, ok := decoded.(PositionTrackingBlock); ok && tracked.TrackingHandle() != 0 {
			decoded = tracked.WithTrackingHandle(w.TrackPosition(be.Pos, tracked.TrackingHandle()))
		}
		c.updateBlockEntity(be.Pos, decoded)
	}
}
