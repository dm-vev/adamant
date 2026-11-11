package world

import (
	"log/slog"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/scoreboard"
	"github.com/google/uuid"
)

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
	// lightReadNear holds the chunks undeferSpreadsNear undeferred the
	// spreads around since light was last spread: light spreading on from
	// them is spread as far as light near them reads it, and waits further
	// out.
	lightReadNear []ChunkPos
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
	// light filled at once: half the processors, so that a player loading
	// the columns around them leaves processors to the owner.
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
