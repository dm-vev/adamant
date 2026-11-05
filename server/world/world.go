package world

import (
	"context"
	"fmt"
	"iter"
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

type entityState struct {
	pos               ChunkPos
	ent               Entity
	lastTick          int64
	lastProcessedTick int64
	lastPostBlockTick int64
	// firstTickPending distinguishes the spawn-time processed marker from an
	// actual entity tick. A block may explicitly admit a new actor this tick.
	firstTickPending bool
	// nextPassiveTick is the next scheduled tick at which the entity should receive
	// maintenance updates such as ageing and fire decay while it is outside of the
	// active simulation range.
	nextPassiveTick int64
	// isItem caches whether the entity type is a dropped item (minecraft:item).
	// This avoids calling EncodeEntity() for every entity on every tick.
	isItem bool
	// isTicker caches whether the entity implements TickerEntity so we can avoid
	// repeating expensive type assertions in the hot tick path.
	isTicker      bool
	tickerChecked bool
	ticker        TickerEntity
	// faults counts the panics of the entity, which no longer ticks once
	// quarantined.
	faults      faultCount
	quarantined bool
	// binds records whether ent rebinds to each transaction it is used in,
	// so that it may be handed out again instead of a fresh view.
	binds            bool
	binder           interface{ BindTransaction(*Tx) }
	postBlockTicker  interface{ TickAfterBlockUpdates(*Tx, int64) }
	preBlockTicker   interface{ TickBeforeBlockUpdates(*Tx, int64) }
	lastPreBlockTick int64
	// bound is the transaction binder was bound to last.
	bound *Tx
}

func (s *entityState) entity(tx *Tx, handle *EntityHandle) Entity {
	if s == nil {
		return nil
	}
	if s.ent == nil {
		s.ent = handle.mustEntity(tx)
		s.binder, s.binds = s.ent.(interface{ BindTransaction(*Tx) })
	}
	if !s.tickerChecked {
		if ticker, ok := s.ent.(TickerEntity); ok {
			s.ticker = ticker
			s.isTicker = true
		} else {
			s.ticker = nil
			s.isTicker = false
		}
		s.tickerChecked = true
		s.postBlockTicker, _ = s.ent.(interface{ TickAfterBlockUpdates(*Tx, int64) })
		s.preBlockTicker, _ = s.ent.(interface{ TickBeforeBlockUpdates(*Tx, int64) })
	}
	if s.binds {
		s.bind(tx)
	}
	return s.ent
}

// bind binds the entity of s to tx. A synchronous World runs a transaction
// started in another at once, as EntityHandle.Do, World.Exec and Call do
// from a transaction: if tx is such a transaction and the entity was bound
// to the one it runs in, tx binds it back to that one as it closes, so that
// the transaction it ran in goes on using the entity. It would otherwise be
// left bound to tx, which ended.
func (s *entityState) bind(tx *Tx) {
	if prev := s.bound; prev != tx {
		if prev != nil && prev.nests(tx) {
			tx.rebound = append(tx.rebound, rebinding{s: s, prev: prev})
		}
		s.bound = tx
	}
	s.binder.BindTransaction(tx)
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

// EntityCount returns the number of entities tracked by the world.
func (w *World) EntityCount() int {
	if w == nil {
		return 0
	}
	return int(w.entityCount.Load())
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

// block reads a block from the position passed. If a chunk is not yet loaded
// at that position, the chunk is loaded, or generated if it could not be found
// in the world save, and the block returned.
func (tx *Tx) block(pos cube.Pos) Block {
	return tx.World().blockInChunk(tx.chunk(chunkPosFromBlockPos(pos)), pos)
}

// blockLoaded reads a block from a position only if its chunk is already loaded.
func (w *World) blockLoaded(pos cube.Pos) (Block, bool) {
	if pos.OutOfBounds(w.ra) {
		return w.conf.Blocks.Air(), false
	}
	c, ok := w.chunks[chunkPosFromBlockPos(pos)]
	if !ok || !c.Ready() {
		return w.conf.Blocks.Air(), false
	}
	return w.loadedBlockIn(c, pos), true
}

// loadedBlockIn reads the block at pos from the ready column c holding it
// without storing anything in c.
func (w *World) loadedBlockIn(c *Column, pos cube.Pos) Block {
	rid := c.Block(uint8(pos[0]), int16(pos[1]), uint8(pos[2]), 0)
	if w.conf.Blocks.NBTBlock(rid) {
		if b, ok := c.BlockEntities[pos]; ok {
			return b
		}
	}
	return w.conf.Blocks.BlockByRuntimeIDOrAir(rid)
}

// reconcileMissingBlockStates applies only opted-in missing-state reconstruction. It runs on the world owner after a
// chunk becomes ready, and revisits pending states in the chunk and its surrounding chunks.
func (w *World) reconcileMissingBlockStates(pos ChunkPos) {
	type candidate struct {
		column     *Column
		pos        cube.Pos
		properties []string
		order      int
	}
	candidates := make([]candidate, 0)
	for _, offset := range [...]ChunkPos{{}, {-1, 0}, {1, 0}, {0, -1}, {0, 1}, {-1, -1}, {-1, 1}, {1, -1}, {1, 1}} {
		candidatePos := ChunkPos{pos[0] + offset[0], pos[1] + offset[1]}
		col, ok := w.chunks[candidatePos]
		if !ok || !col.Ready() || len(col.missingBlockStates) == 0 {
			continue
		}
		for blockPos, state := range col.missingBlockStates {
			current, loaded := w.blockLoaded(blockPos)
			if !loaded {
				continue
			}
			reconstructor, ok := current.(MissingStateReconstructor)
			if !ok {
				if col.CanonicalizeMissingBlockState(blockPos, 0) {
					col.modified = true
					col.blockVersion = blockVersionClock.Add(1)
					delete(col.missingBlockStates, blockPos)
				}
				continue
			}
			candidates = append(candidates, candidate{
				column: col, pos: blockPos, properties: state.properties,
				order: reconstructor.ReconstructionOrder(),
			})
		}
	}
	if len(candidates) == 0 {
		return
	}
	slices.SortFunc(candidates, func(a, b candidate) int {
		if a.order < b.order {
			return -1
		}
		if a.order > b.order {
			return 1
		}
		for _, axis := range [...]int{1, 0, 2} {
			if a.pos[axis] < b.pos[axis] {
				return -1
			}
			if a.pos[axis] > b.pos[axis] {
				return 1
			}
		}
		return 0
	})
	for _, candidate := range candidates {
		current, loaded := w.blockLoaded(candidate.pos)
		if !loaded {
			continue
		}
		reconstructor, ok := current.(MissingStateReconstructor)
		if !ok {
			continue
		}
		source := &loadedWorldSource{w: w, complete: true, order: candidate.order}
		updated, retained := w.retainedJavaBlock(candidate.column, candidate.pos, current)
		if !retained {
			updated = reconstructor.ReconstructMissingState(candidate.pos, candidate.properties, source)
		}
		if updated == nil || !source.complete {
			continue
		}
		rid := w.conf.Blocks.BlockRuntimeID(updated)
		oldRID := candidate.column.Block(uint8(candidate.pos[0]), int16(candidate.pos[1]), uint8(candidate.pos[2]), 0)
		if oldRID != rid {
			oldLight := w.cellLightAt(candidate.column, candidate.pos)
			candidate.column.SetBlock(uint8(candidate.pos[0]), int16(candidate.pos[1]), uint8(candidate.pos[2]), 0, rid)
			w.relightIfChanged(candidate.column, candidate.pos, oldLight)
			candidate.column.invalidateRandomTickSubChunks()
			candidate.column.invalidateSubChunkHeightMaps()
			candidate.column.invalidateNetworkSubChunkPayloads()
			candidate.column.modified = true
			w.viewBlockUpdate(candidate.column, candidate.pos, updated, 0)
		}
		if candidate.column.CanonicalizeMissingBlockState(candidate.pos, 0) {
			candidate.column.modified = true
			candidate.column.blockVersion = blockVersionClock.Add(1)
			delete(candidate.column.missingBlockStates, candidate.pos)
		}
	}
}

// blockInChunk reads a block from a chunk at the position passed. The block
// is assumed to be within the chunk passed.
func (w *World) blockInChunk(c *Column, pos cube.Pos) Block {
	if pos.OutOfBounds(w.ra) {
		// Fast way out.
		return w.conf.Blocks.Air()
	}
	rid := c.Block(uint8(pos[0]), int16(pos[1]), uint8(pos[2]), 0)
	if w.conf.Blocks.NBTBlock(rid) {
		// The block was also a block entity, so we look it up in the block entity map.
		if b, ok := c.BlockEntities[pos]; ok {
			return b
		}
		// Despite being a block with NBT, the block didn't actually have any
		// stored NBT yet. We add it here and update the block.
		nbtB := DecodeNBT(w.conf.Blocks.BlockByRuntimeIDOrAir(rid).(NBTer), map[string]any{}, w.conf.Blocks).(Block)
		nbtB = restoreJavaBlockActor(c.Chunk, pos, nbtB)
		c.updateBlockEntity(pos, nbtB)
		c.invalidateBlockEntityPayloads()
		w.viewBlockUpdate(c, pos, nbtB, 0)
		return nbtB
	}
	return w.conf.Blocks.BlockByRuntimeIDOrAir(rid)
}

// biome reads the Biome at the position passed. If a chunk is not yet loaded
// at that position, the chunk is loaded, or generated if it could not be found
// in the world save, and the Biome returned.
func (tx *Tx) biome(pos cube.Pos) Biome {
	if pos.OutOfBounds(tx.Range()) {
		// Fast way out.
		return ocean()
	}
	id := int(tx.chunk(chunkPosFromBlockPos(pos)).Biome(uint8(pos[0]), int16(pos[1]), uint8(pos[2])))
	b, ok := BiomeByID(id)
	if !ok {
		tx.World().log.Error("biome not found by ID", "ID", id)
		return unknownBiome{id: id}
	}
	return b
}

// HighestLightBlocker gets the Y value of the highest fully light blocking
// block at the x and z values passed in the World. It must not be called from
// within a transaction; use Tx.HighestLightBlocker instead.
func (w *World) HighestLightBlocker(x, z int) int {
	y, _ := Call(context.Background(), w, func(tx *Tx) (int, error) {
		return tx.highestLightBlocker(x, z), nil
	})
	return y
}

// highestLightBlocker gets the Y value of the highest fully light blocking
// block at the x and z values passed in the World.
func (tx *Tx) highestLightBlocker(x, z int) int {
	return int(tx.chunk(ChunkPos{int32(x >> 4), int32(z >> 4)}).HighestLightBlocker(uint8(x), uint8(z)))
}

// highestBlock looks up the highest non-air block in the World at a specific x
// and z The y value of the highest block is returned, or 0 if no blocks were
// present in the column.
func (tx *Tx) highestBlock(x, z int) int {
	return int(tx.chunk(ChunkPos{int32(x >> 4), int32(z >> 4)}).highestBlock(uint8(x), uint8(z)))
}

// surfaceHeights caches the highest block in each x and z of a column, as
// Java keeps the WORLD_SURFACE height map of a chunk rather than scanning
// it, which mobs spawning naturally ask of every chunk every tick. Setting a
// block through Column.SetBlock keeps it up to date, as Java updates the
// height map with every block set; changing blocks otherwise drops it.
type surfaceHeights struct {
	y     [256]int16
	stale [256]bool
}

// highestBlock returns what HighestBlock returns for x and z, from the
// cache, which the first call fills for the whole column at once.
func (c *Column) highestBlock(x, z uint8) int16 {
	s := c.surface
	if s == nil {
		s = &surfaceHeights{}
		c.HighestBlocks(&s.y)
		c.surface = s
	}
	i := int(x&15)<<4 | int(z&15)
	if s.stale[i] {
		s.y[i], s.stale[i] = c.HighestBlock(x&15, z&15), false
	}
	return s.y[i]
}

// surfaceBlockSet updates the highest block cached for x and z once the
// block at x, y and z of the first layer was set to the runtime ID passed.
func (c *Column) surfaceBlockSet(x uint8, y int16, z uint8, rid uint32) {
	s := c.surface
	if s == nil {
		return
	}
	i := int(x&15)<<4 | int(z&15)
	if s.stale[i] {
		return
	}
	if air := rid == c.BlockRegistry().AirRuntimeID(); !air && y > s.y[i] {
		s.y[i] = y
	} else if air && y == s.y[i] {
		// The highest block was removed: find the next one when asked.
		s.stale[i] = true
	}
}

// highestObstructingBlock returns the highest block in the World at a given x
// and z that has at least a solid top or bottom face.
func (tx *Tx) highestObstructingBlock(x, z int) int {
	yHigh := tx.highestBlock(x, z)
	src := worldSource{tx: tx}
	for y := yHigh; y >= tx.Range()[0]; y-- {
		pos := cube.Pos{x, y, z}
		m := tx.block(pos).Model()
		if m.FaceSolid(pos, cube.FaceUp, src) || m.FaceSolid(pos, cube.FaceDown, src) {
			return y
		}
	}
	return tx.Range()[0]
}

// SetOpts holds several parameters that may be set to disable updates in the
// World of different kinds as a result of a call to SetBlock.
type SetOpts struct {
	// DisableBlockUpdates makes SetBlock not update any neighbouring blocks as
	// a result of the SetBlock call.
	DisableBlockUpdates bool
	// DisableLiquidDisplacement disables the displacement of liquid blocks to
	// the second layer (or back to the first layer, if it already was on the
	// second layer). Disabling this is not widely recommended unless
	// performance is very important, or where it is known no liquid can be
	// present anyway.
	DisableLiquidDisplacement bool
	// DisableRedstoneUpdates makes SetBlock not invalidate the redstone engine
	// around the changed block. This is used by the redstone engine while
	// applying its own block-state updates to avoid duplicate same-tick work.
	DisableRedstoneUpdates bool

	// Source mutations publish both native layers together and apply Java's
	// independent notification and block-entity side-effect flags.
	nativeState             *NativeState
	disableRemovalHandler   bool
	disablePlacementHandler bool
	disableViewUpdates      bool
	// circuitPublication marks the engine's own output, rather than an
	// external replacement that may recreate a native component.
	circuitPublication bool
}

// setBlock writes a block to the position passed. If a chunk is not yet loaded
// at that position, the chunk is first loaded or generated if it could not be
// found in the world save. setBlock panics if the block passed has not yet
// been registered using RegisterBlock(). Nil may be passed as the block to set
// the block to air.
//
// A SetOpts struct may be passed to additionally modify behaviour of setBlock,
// specifically to improve performance under specific circumstances. Nil should
// be passed where performance is not essential, to make sure the world is
// updated adequately.
//
// setBlock should be avoided in situations where performance is critical when
// needing to set a lot of blocks to the world. BuildStructure may be used
// instead.
func (tx *Tx) setBlock(pos cube.Pos, b Block, opts *SetOpts) {
	w := tx.World()
	if pos.OutOfBounds(w.Range()) {
		// Fast way out.
		return
	}
	if tx.region != nil {
		if !tx.regionWrites(pos) {
			tx.deferToOwner(func(tx *Tx) { tx.setBlock(pos, b, opts) })
			return
		}
		tx.lockShared()
		defer tx.unlockShared()
	}
	if b == nil {
		b = w.conf.Blocks.Air()
	}
	if opts == nil {
		opts = &SetOpts{}
	}

	x, y, z := uint8(pos[0]), int16(pos[1]), uint8(pos[2])
	c := tx.chunk(chunkPosFromBlockPos(pos))

	rid := w.conf.Blocks.BlockRuntimeID(b)
	oldLight := w.cellLightAt(c, pos)
	redstoneAfterRelevant := isRedstoneRelevant(b)
	needOldBlock := !opts.DisableRedstoneUpdates || !redstoneAfterRelevant

	oldRID := c.Block(x, y, z, 0)
	needOldBlock = needOldBlock || w.conf.Blocks.NBTBlock(oldRID)
	oldBlock := w.conf.Blocks.BlockByRuntimeIDOrAir(oldRID)
	if w.conf.Blocks.NBTBlock(oldRID) {
		if blockEntity, ok := c.BlockEntities[pos]; ok {
			oldBlock = blockEntity
		}
	}
	b = w.replacePositionTrackingBlock(tx, pos, oldBlock, b)
	if handler, ok := oldBlock.(BlockRemovalHandler); ok && !opts.disableRemovalHandler {
		handler.Removed(pos, tx, b)
	}

	var before uint32
	if rid != w.conf.Blocks.AirRuntimeID() && !opts.DisableLiquidDisplacement {
		before = oldRID
	}
	lightBefore := w.cellLightAt(c, pos)

	c.modified = true
	if opts.nativeState == nil {
		c.SetBlock(x, y, z, 0, rid)
	} else {
		SetNativeState(c.Chunk, x, y, z, *opts.nativeState)
		c.surfaceBlockSet(x, y, z, opts.nativeState.Block)
	}
	if _, pending := c.missingBlockStates[pos]; pending && c.CanonicalizeMissingBlockState(pos, 0) {
		delete(c.missingBlockStates, pos)
	}
	c.blockVersion = blockVersionClock.Add(1)
	c.invalidateRandomTickSubChunks()
	c.invalidateSubChunkHeightMaps()
	c.invalidateNetworkSubChunkPayloads()
	if w.conf.Blocks.NBTBlock(rid) {
		c.updateBlockEntity(pos, b)
		c.invalidateBlockEntityPayloads()
	} else {
		c.updateBlockEntity(pos, nil)
		c.invalidateBlockEntityPayloads()
	}
	if len(c.faultyActors) != 0 {
		if a, ok := c.faultyActors[pos]; ok && c.tickerBlockEntityVersions[pos] != a.version {
			// The block entity that panicked was removed or replaced.
			w.forgetActor(c, pos)
		}
	}
	if len(w.blockFaults.m) != 0 {
		w.blockSet(pos, b)
	}

	if !opts.DisableLiquidDisplacement {
		var secondLayer Block

		airRID := w.conf.Blocks.AirRuntimeID()
		if rid == airRID {
			if li := c.Block(x, y, z, 1); li != airRID {
				c.SetBlock(x, y, z, 0, li)
				c.SetBlock(x, y, z, 1, airRID)
				c.invalidateRandomTickSubChunks()
				c.invalidateSubChunkHeightMaps()
				c.invalidateNetworkSubChunkPayloads()
				secondLayer = w.conf.Blocks.Air()
				b = w.conf.Blocks.BlockByRuntimeIDOrAir(li)
			}
		} else if w.conf.Blocks.LiquidDisplacingBlock(rid) {
			if w.conf.Blocks.LiquidBlock(before) {
				l := w.conf.Blocks.BlockByRuntimeIDOrAir(before)
				if b.(LiquidDisplacer).CanDisplace(l.(Liquid)) {
					c.SetBlock(x, y, z, 1, before)
					secondLayer = l
				}
			}
		} else if li := c.Block(x, y, z, 1); li != airRID {
			c.SetBlock(x, y, z, 1, airRID)
			secondLayer = w.conf.Blocks.Air()
		}

		if secondLayer != nil {
			w.viewBlockUpdate(c, pos, secondLayer, 1)
		}
	}
	w.relightIfChanged(c, pos, lightBefore)
	if redstoneAfterRelevant || (needOldBlock && isRedstoneRelevant(oldBlock)) {
		w.redstone.forget(pos)
	}
	w.relightIfChanged(c, pos, oldLight)
	w.redstone.replaceCircuit(pos, oldBlock, b, opts.circuitPublication)

	if !opts.disableViewUpdates {
		w.viewBlockUpdate(c, pos, b, 0)
		if opts.nativeState != nil {
			w.viewBlockUpdate(c, pos, w.conf.Blocks.BlockByRuntimeIDOrAir(opts.nativeState.Liquid), 1)
		}
	}

	if !opts.DisableBlockUpdates {
		tx.updateAroundSetBlock(pos, oldBlock, b)
	}
	if !opts.DisableRedstoneUpdates {
		w.redstone.invalidateAroundBlockChange(pos, oldBlock, b, RedstoneUpdateCauseBlockUpdate, w.Range())
	}
	w.updatePOI(pos, b)
	if handler, ok := b.(PlacementHandler); ok && !opts.disablePlacementHandler {
		handler.Placed(pos, tx, oldBlock)
	}
}

// setBlockEntity updates block entity data without triggering block updates.
func (tx *Tx) setBlockEntity(pos cube.Pos, b Block) {
	w := tx.World()
	if pos.OutOfBounds(w.Range()) {
		// Fast way out.
		return
	}
	if tx.region != nil {
		if !tx.regionWrites(pos) {
			tx.deferToOwner(func(tx *Tx) { tx.setBlockEntity(pos, b) })
			return
		}
		tx.lockShared()
		defer tx.unlockShared()
	}
	c := tx.chunk(chunkPosFromBlockPos(pos))

	rid := w.conf.Blocks.BlockRuntimeID(b)
	if !w.conf.Blocks.NBTBlock(rid) || c.Block(uint8(pos[0]), int16(pos[1]), uint8(pos[2]), 0) != rid {
		tx.setBlock(pos, b, nil)
		return
	}
	b = w.replacePositionTrackingBlock(tx, pos, c.BlockEntities[pos], b)
	c.updateBlockEntity(pos, b)
	c.blockVersion = blockVersionClock.Add(1)
	// Chunk and sub-chunk responses cache encoded block entities.
	c.invalidateBlockEntityPayloads()
	c.modified = true
}

func (w *World) replacePositionTrackingBlock(tx *Tx, pos cube.Pos, old, replacement Block) Block {
	oldTracked, oldOK := old.(PositionTrackingBlock)
	newTracked, newOK := replacement.(PositionTrackingBlock)
	if oldOK && oldTracked.TrackingHandle() != 0 && (!newOK || newTracked.TrackingHandle() != oldTracked.TrackingHandle()) {
		// Untracking tells every viewer of the world.
		tx.deferToOwner(func(*Tx) { w.UntrackPosition(pos) })
	}
	if newOK && newTracked.TrackingHandle() != 0 {
		return newTracked.WithTrackingHandle(w.TrackPosition(pos, newTracked.TrackingHandle()))
	}
	return replacement
}

// setBiome sets the Biome at the position passed. If a chunk is not yet loaded
// at that position, the chunk is first loaded or generated if it could not be
// found in the world save.
func (tx *Tx) setBiome(pos cube.Pos, b Biome) {
	if pos.OutOfBounds(tx.Range()) {
		// Fast way out.
		return
	}
	if tx.region != nil && !tx.regionWrites(pos) {
		tx.deferToOwner(func(tx *Tx) { tx.setBiome(pos, b) })
		return
	}
	c := tx.chunk(chunkPosFromBlockPos(pos))
	c.modified = true
	c.SetBiome(uint8(pos[0]), int16(pos[1]), uint8(pos[2]), uint32(b.EncodeBiome()))
	c.invalidateNetworkBiomePayload()
}

// buildStructure builds a Structure passed at a specific position in the
// world. Unlike setBlock, it takes a Structure implementation, which provides
// blocks to be placed at a specific location. buildStructure is specifically
// optimised to be able to process a large batch of chunks simultaneously and
// will do so within much less time than separate setBlock calls would. The
// method operates on a per-chunk basis, setting all blocks within a single
// chunk part of the Structure before moving on to the next chunk.
func (tx *Tx) buildStructure(pos cube.Pos, s Structure) {
	if tx.region != nil {
		// A structure is relit across the chunks around it at once.
		tx.deferToOwner(func(tx *Tx) { tx.buildStructure(pos, s) })
		return
	}
	w := tx.World()
	dim := s.Dimensions()
	width, height, length := dim[0], dim[1], dim[2]
	if width <= 0 || height <= 0 || length <= 0 {
		return
	}
	maxX, maxY, maxZ := pos[0]+width, pos[1]+height, pos[2]+length
	f := func(x, y, z int) Block {
		return tx.block(cube.Pos{pos[0] + x, pos[1] + y, pos[2] + z})
	}

	// We approach this on a per-chunk basis, so that we can keep only one chunk
	// in memory at a time while not needing to acquire a new chunk lock for
	// every block. This also allows us not to send block updates, but instead
	// send a single chunk update once.
	var touched []ChunkPos
	defer func() { w.relightChunks(touched) }()
	for chunkX := pos[0] >> 4; chunkX <= (maxX-1)>>4; chunkX++ {
		for chunkZ := pos[2] >> 4; chunkZ <= (maxZ-1)>>4; chunkZ++ {
			chunkPos := ChunkPos{int32(chunkX), int32(chunkZ)}
			c := tx.chunk(chunkPos)

			baseX, baseZ := chunkX<<4, chunkZ<<4
			for i, sub := range c.Sub() {
				baseY := (i + (w.Range()[0] >> 4)) << 4
				if baseY>>4 < pos[1]>>4 {
					continue
				} else if baseY >= maxY {
					break
				}

				for localY := 0; localY < 16; localY++ {
					yOffset := baseY + localY
					if yOffset > w.Range()[1] || yOffset >= maxY {
						// We've hit the height limit for blocks.
						break
					} else if yOffset < w.Range()[0] || yOffset < pos[1] {
						// We've got a block below the minimum, but other blocks might still reach above
						// it, so don't break but continue.
						continue
					}
					for localX := 0; localX < 16; localX++ {
						xOffset := baseX + localX
						if xOffset < pos[0] || xOffset >= maxX {
							continue
						}
						for localZ := 0; localZ < 16; localZ++ {
							zOffset := baseZ + localZ
							if zOffset < pos[2] || zOffset >= maxZ {
								continue
							}
							b, liq := s.At(xOffset-pos[0], yOffset-pos[1], zOffset-pos[2], f)
							lightPos := cube.Pos{xOffset, yOffset, zOffset}
							oldLight := w.cellLightAt(c, lightPos)
							if b != nil {
								nbtPos := cube.Pos{xOffset, yOffset, zOffset}
								oldRID := sub.Block(uint8(xOffset), uint8(yOffset), uint8(zOffset), 0)
								oldBlock := w.conf.Blocks.BlockByRuntimeIDOrAir(oldRID)
								if w.conf.Blocks.NBTBlock(oldRID) {
									if blockEntity, ok := c.BlockEntities[nbtPos]; ok {
										oldBlock = blockEntity
									}
								}
								b = w.replacePositionTrackingBlock(tx, nbtPos, oldBlock, b)
								if handler, ok := oldBlock.(BlockRemovalHandler); ok {
									handler.Removed(nbtPos, tx, b)
								}
								rid := w.conf.Blocks.BlockRuntimeID(b)
								sub.SetBlock(uint8(xOffset), uint8(yOffset), uint8(zOffset), 0, rid)
								c.invalidateRandomTickSubChunks()
								c.invalidateSubChunkHeightMaps()
								c.invalidateNetworkSubChunkPayloads()

								if w.conf.Blocks.NBTBlock(rid) {
									c.updateBlockEntity(nbtPos, b)
									c.invalidateBlockEntityPayloads()
								} else {
									c.updateBlockEntity(nbtPos, nil)
									c.invalidateBlockEntityPayloads()
								}
								w.updatePOI(nbtPos, b)
								w.redstone.replaceCircuit(nbtPos, oldBlock, b, false)
							}
							if liq != nil {
								sub.SetBlock(uint8(xOffset), uint8(yOffset), uint8(zOffset), 1, w.conf.Blocks.BlockRuntimeID(liq))
							} else if len(sub.Layers()) > 1 {
								sub.SetBlock(uint8(xOffset), uint8(yOffset), uint8(zOffset), 1, w.conf.Blocks.AirRuntimeID())
							}
							w.relightIfChanged(c, lightPos, oldLight)
						}
					}
				}
			}
			c.SetBlock(0, 0, 0, 0, c.Block(0, 0, 0, 0)) // Make sure the heightmap is recalculated.
			// The structure set its blocks past SetBlock.
			c.surface = nil
			c.modified = true
			touched = append(touched, chunkPos)

			// After setting all blocks of the structure within a single chunk,
			// we show the new chunk to all viewers once.
			for viewer := range c.viewers {
				viewer.ViewChunk(chunkPos, w.Dimension(), c)
			}
		}
	}
}

// liquid attempts to return a Liquid block at the position passed. This
// Liquid may be in the foreground or in any other layer. If found, the Liquid
// is returned. If not, the bool returned is false.
func (tx *Tx) liquid(pos cube.Pos) (Liquid, bool) {
	w := tx.World()
	if pos.OutOfBounds(w.Range()) {
		// Fast way out.
		return nil, false
	}
	return w.liquidInColumn(tx.chunk(chunkPosFromBlockPos(pos)), pos)
}

// liquidInColumn returns the liquid at pos in any layer of the column c
// holding it.
func (w *World) liquidInColumn(c *Column, pos cube.Pos) (Liquid, bool) {
	x, y, z := uint8(pos[0]), int16(pos[1]), uint8(pos[2])

	id := c.Block(x, y, z, 0)
	if !w.conf.Blocks.LiquidBlock(id) {
		// Most blocks hold no liquid: only look the layers up as blocks if
		// the registry says one is a liquid.
		id = c.Block(x, y, z, 1)
		if !w.conf.Blocks.LiquidBlock(id) {
			return nil, false
		}
	}
	b, ok := w.conf.Blocks.BlockByRuntimeID(id)
	if !ok {
		w.log.Error("Liquid: no block with runtime ID", "ID", id)
		return nil, false
	}
	if liq, ok := b.(Liquid); ok {
		return liq, true
	}
	id = c.Block(x, y, z, 1)

	b, ok = w.conf.Blocks.BlockByRuntimeID(id)
	if !ok {
		w.log.Error("Liquid: no block with runtime ID", "ID", id)
		return nil, false
	}
	liq, ok := b.(Liquid)
	return liq, ok
}

// mayContainLiquid reports whether a block from lo to hi, inclusive, may
// hold a liquid: it is false only if none of the sub chunks the range
// touches has a liquid in the palette of any of its layers.
func (tx *Tx) mayContainLiquid(lo, hi cube.Pos) bool {
	w := tx.World()
	r := w.Range()
	minY, maxY := max(lo[1], r[0]), min(hi[1], r[1])
	if minY > maxY {
		return false
	}
	low, high := chunkPosFromBlockPos(lo), chunkPosFromBlockPos(hi)
	for cx := low[0]; cx <= high[0]; cx++ {
		for cz := low[1]; cz <= high[1]; cz++ {
			if w.subChunksMayContainLiquid(tx.chunk(ChunkPos{cx, cz}), minY, maxY) {
				return true
			}
		}
	}
	return false
}

// subChunksMayContainLiquid reports whether a palette of a layer of the sub
// chunks of c from minY to maxY, both within the world's range, holds a
// liquid.
func (w *World) subChunksMayContainLiquid(c *Column, minY, maxY int) bool {
	subs := c.Sub()
	for i := c.SubIndex(int16(minY)); i <= c.SubIndex(int16(maxY)); i++ {
		for _, layer := range subs[i].Layers() {
			palette := layer.Palette()
			for j := range palette.Len() {
				if w.conf.Blocks.LiquidBlock(palette.Value(uint16(j))) {
					return true
				}
			}
		}
	}
	return false
}

// setLiquid sets a Liquid at a specific position in the World. Unlike
// setBlock, setLiquid will not necessarily overwrite any existing blocks. It
// will instead be in the same position as a block currently there, unless
// there already is a Liquid at that position, in which case it will be
// overwritten. If nil is passed for the Liquid, any Liquid currently present
// will be removed.
func (tx *Tx) setLiquid(pos cube.Pos, b Liquid) {
	w := tx.World()
	if pos.OutOfBounds(w.Range()) {
		// Fast way out.
		return
	}
	if tx.region != nil {
		if !tx.regionWrites(pos) {
			tx.deferToOwner(func(tx *Tx) { tx.setLiquid(pos, b) })
			return
		}
		tx.lockShared()
		defer tx.unlockShared()
	}
	chunkPos := chunkPosFromBlockPos(pos)
	c := tx.chunk(chunkPos)
	lightBefore := w.cellLightAt(c, pos)
	defer w.relightIfChanged(c, pos, lightBefore)
	if b == nil {
		w.removeLiquids(c, pos)
		tx.doBlockUpdatesAround(pos)
		w.redstone.invalidateAround(pos, pos, RedstoneUpdateCauseBlockUpdate, w.Range())
		return
	}
	x, y, z := uint8(pos[0]), int16(pos[1]), uint8(pos[2])
	if !replaceable(w, c, pos, b) {
		if displacer, ok := w.blockInChunk(c, pos).(LiquidDisplacer); !ok || !displacer.CanDisplace(b) {
			return
		}
	}
	rid := w.conf.Blocks.BlockRuntimeID(b)
	if w.removeLiquids(c, pos) {
		c.SetBlock(x, y, z, 0, rid)
		c.invalidateRandomTickSubChunks()
		c.invalidateSubChunkHeightMaps()
		c.invalidateNetworkSubChunkPayloads()
		w.viewBlockUpdate(c, pos, b, 0)
	} else {
		c.SetBlock(x, y, z, 1, rid)
		w.viewBlockUpdate(c, pos, b, 1)
	}
	c.modified = true

	tx.doBlockUpdatesAround(pos)
	w.redstone.invalidateAround(pos, pos, RedstoneUpdateCauseBlockUpdate, w.Range())
	if handler, ok := b.(LiquidPlacementHandler); ok {
		handler.LiquidPlaced(pos, tx)
	}
}

// removeLiquids removes any liquid blocks that may be present at a specific
// block position in the chunk passed. The bool returned specifies if no blocks
// were left on the foreground layer.
func (w *World) removeLiquids(c *Column, pos cube.Pos) bool {
	x, y, z := uint8(pos[0]), int16(pos[1]), uint8(pos[2])
	air := w.conf.Blocks.Air()

	noneLeft := false
	if noLeft, changed := w.removeLiquidOnLayer(c, x, y, z, 0); noLeft {
		if changed {
			w.viewBlockUpdate(c, pos, air, 0)
		}
		noneLeft = true
	}
	if _, changed := w.removeLiquidOnLayer(c, x, y, z, 1); changed {
		w.viewBlockUpdate(c, pos, air, 1)
	}
	return noneLeft
}

// removeLiquidOnLayer removes a liquid block from a specific layer in the
// chunk passed, returning true if successful.
func (w *World) removeLiquidOnLayer(c *Column, x uint8, y int16, z, layer uint8) (bool, bool) {
	id := c.Block(x, y, z, layer)
	airRID := w.conf.Blocks.AirRuntimeID()

	b, ok := w.conf.Blocks.BlockByRuntimeID(id)
	if !ok {
		w.log.Error("removeLiquidOnLayer: no block with runtime ID", "ID", id)
		return false, false
	}
	if _, ok := b.(Liquid); ok {
		c.SetBlock(x, y, z, layer, airRID)
		c.invalidateRandomTickSubChunks()
		c.invalidateSubChunkHeightMaps()
		c.invalidateNetworkSubChunkPayloads()
		return true, true
	}
	return id == airRID, false
}

// additionalLiquid checks if the block at a position has additional liquid on
// another layer and returns the liquid if so.
func (tx *Tx) additionalLiquid(pos cube.Pos) (Liquid, bool) {
	w := tx.World()
	if pos.OutOfBounds(w.Range()) {
		// Fast way out.
		return nil, false
	}
	c := tx.chunk(chunkPosFromBlockPos(pos))
	id := c.Block(uint8(pos[0]), int16(pos[1]), uint8(pos[2]), 1)

	b, ok := w.conf.Blocks.BlockByRuntimeID(id)
	if !ok {
		w.log.Error("additionalLiquid: no block with runtime ID", "ID", id)
		return nil, false
	}
	liq, ok := b.(Liquid)
	return liq, ok
}

// light returns the light level at the position passed. This is the highest of
// the sky and block light. The light value returned is a value in the range
// 0-15, where 0 means there is no light present, whereas 15 means the block is
// fully lit.
func (tx *Tx) light(pos cube.Pos) uint8 {
	w := tx.World()
	w.flushLight()
	if pos[1] < w.ra[0] {
		// Fast way out.
		w.flushLight()
		return 0
	}
	if !hasSkyLight(w.conf.Dim) {
		// Without a sky, only blocks light the world.
		return tx.blockLight(pos)
	}
	if pos[1] > w.ra[1] {
		// Above the rest of the world, so full skylight.
		w.flushLight()
		return 15
	}
	chunkPos := chunkPosFromBlockPos(pos)
	c, ok := w.loadedChunk(chunkPos)
	w.flushLightAt(chunkPos, c)
	if !ok {
		return 0
	}
	return c.Light(uint8(pos[0]), int16(pos[1]), uint8(pos[2]))
}

// skyLight returns the skylight level at the position passed. This light level
// is not influenced by blocks that emit light, such as torches. The light
// value, similarly to light, is a value in the range 0-15, where 0 means no
// light is present.
func (tx *Tx) skyLight(pos cube.Pos) uint8 {
	w := tx.World()
	if pos[1] < w.ra[0] || !hasSkyLight(w.conf.Dim) {
		// Fast way out.
		w.flushLight()
		return 0
	}
	if pos[1] > w.ra[1] {
		// Above the rest of the world, so full skylight.
		w.flushLight()
		return 15
	}
	chunkPos := chunkPosFromBlockPos(pos)
	c := tx.chunk(chunkPos)
	w.flushLightAt(chunkPos, c)
	return c.SkyLight(uint8(pos[0]), int16(pos[1]), uint8(pos[2]))
}

// temperature returns the temperature in the World at a specific position.
// Higher altitudes and different biomes influence the temperature returned.
func (tx *Tx) temperature(pos cube.Pos) float64 {
	const (
		tempDrop = 1.0 / 600
		seaLevel = 64
	)
	diff := max(pos[1]-seaLevel, 0)
	return tx.biome(pos).Temperature() - float64(diff)*tempDrop
}

// addEntity adds an EntityHandle to a World. The Entity will be visible to all
// viewers of the World that have the chunk at the EntityHandle's position. If
// the chunk that the EntityHandle is in is not yet loaded, it will first be
// loaded. addEntity panics if the EntityHandle is already in a world.
// addEntity returns the Entity created by the EntityHandle.
func (w *World) addEntity(tx *Tx, handle *EntityHandle) Entity {
	tx.lockShared()
	defer tx.unlockShared()
	handle.setAndUnlockWorld(w)
	// An entity transferred is in a world again.
	handle.endTransfer()
	pos := chunkPosFromVec3(handle.data.Pos)
	w.set.Lock()
	currentTick := w.currentTickLocked()
	w.set.Unlock()
	state := &entityState{
		pos:               pos,
		lastTick:          currentTick,
		lastProcessedTick: currentTick,
		firstTickPending:  true,
		isItem:            handle.t.EncodeEntity() == "minecraft:item",
	}
	if _, ok := w.entities[handle]; !ok {
		w.entityCount.Add(1)
	}
	w.entities[handle] = state
	w.trackPlayer(handle)
	w.entitiesByUUID[handle.id] = handle
	w.entitiesByUniqueID[handle.UniqueID()] = handle
	handle.state = state
	if tx.region == nil {
		w.resolveEntityRiding(handle)
	} else if handle.pendingVehicle != uuid.Nil || len(w.pendingRiders[handle.id]) > 0 {
		// Riders waiting for the entity may be in other regions.
		tx.deferToOwner(func(*Tx) {
			if handle.state == state {
				w.resolveEntityRiding(handle)
			}
		})
	}

	if r := tx.region; r != nil {
		if _, ok := r.loaded(w, pos); !ok {
			// The column is not loaded or another region's: the owner puts the
			// entity in it after the phase. Until then no column lists it.
			e := state.entity(tx, handle)
			handle.finalizeSpawn(e)
			w.Handler().HandleEntitySpawn(tx, e)
			handle.markWorldReady(w)
			r.deferWork(func(tx *Tx) {
				if handle.state != state || handle.w != w {
					return
				}
				c := w.placeEntity(tx, handle, state)
				e := state.entity(tx, handle)
				for v := range c.viewers {
					showEntity(e, v)
				}
			}, handle)
			return e
		}
	}
	c := w.placeEntity(tx, handle, state)
	e := state.entity(tx, handle)
	handle.finalizeSpawn(e)
	for v := range c.viewers {
		// Show the entity to all viewers in the chunk of the entity.
		showEntity(e, v)
	}
	w.Handler().HandleEntitySpawn(tx, e)
	handle.markWorldReady(w)
	return e
}

// placeEntity lists the entity of handle, just added to the world, in the
// column at its chunk, state.pos, loading the column if needed, and returns
// the column.
func (w *World) placeEntity(tx *Tx, handle *EntityHandle, state *entityState) *Column {
	// Entity membership is owner-only even while the terrain worker fills the
	// column. Like Java's entity sections, attaching an entity must not await
	// terrain generation; block reads retain their normal readiness checks.
	pos := state.pos
	c, loaded := w.chunks[pos]
	if !loaded {
		var err error
		if c, err = w.loadChunk(pos); err != nil {
			w.log.Error("load chunk: "+err.Error(), "X", pos[0], "Z", pos[1])
		}
	}
	if c.Ready() && tx.region == nil {
		c = tx.chunk(pos)
	}
	c.addEntity(handle)
	if c.Ready() {
		// The worker owns this flag before publication and dirties it itself.
		c.modified = true
	}
	w.addEntityColumn(pos, c)
	return c
}

// removeEntity removes an Entity from the World that is currently present in
// it. Any viewers of the Entity will no longer be able to see it.
// removeEntity returns the EntityHandle of the Entity. After removing an Entity
// from the World, the Entity is no longer usable.
func (w *World) removeEntity(e Entity, tx *Tx) *EntityHandle {
	tx.lockShared()
	defer tx.unlockShared()
	handle := e.H()
	state, found := w.entities[handle]
	if !found {
		// The entity currently isn't in this world.
		return nil
	}
	pos := state.pos
	w.Handler().HandleEntityDespawn(tx, e)

	// The entity's chunk is loaded, though it may still be generating if the
	// entity was teleported there. In a region phase, an entity whose column
	// is not the region's was added in the phase and is listed in none yet.
	c, loaded := tx.loadedColumn(pos)
	if loaded {
		if c.removeEntity(handle) && c.Ready() {
			c.modified = true
		}
		if len(c.Entities) == 0 {
			w.removeEntityColumn(pos)
		}
	}

	tx.deferToOwner(func(*Tx) { w.removeEntityFromViewLayers(e) })
	if loaded {
		for v := range c.viewers {
			v.HideEntity(e)
		}
	}
	w.removePendingRider(handle)
	w.forgetEntity(handle, state)
	delete(w.entities, handle)
	w.untrackPlayer(handle)
	delete(w.entitiesByUUID, handle.id)
	delete(w.entitiesByUniqueID, handle.UniqueID())
	w.entityCount.Add(-1)
	handle.state = nil
	handle.unsetAndLockWorld()
	return handle
}

// removeEntityFromViewLayers removes stale overrides for despawned entities. Entities that own a ViewLayer,
// such as players, are skipped because they may be removed temporarily when respawning or changing worlds.
func (w *World) removeEntityFromViewLayers(e Entity) {
	if _, ok := e.(viewLayerViewer); ok {
		return
	}
	viewers, _ := w.allViewers()
	for _, viewer := range viewers {
		v, ok := viewer.(viewLayerViewer)
		if !ok || v.ViewLayer() == nil {
			continue
		}
		v.ViewLayer().remove(e)
	}
	w.releaseViewers(viewers)
}

// entitiesWithin returns an iterator that yields all entities contained within
// the cube.BBox passed.
func (w *World) entitiesWithin(tx *Tx, box cube.BBox) iter.Seq[Entity] {
	return func(yield func(Entity) bool) {
		minPos, maxPos := chunkPosFromVec3(box.Min()), chunkPosFromVec3(box.Max())

		for x := minPos[0]; x <= maxPos[0]; x++ {
			for z := minPos[1]; z <= maxPos[1]; z++ {
				c, ok := tx.loadedColumn(ChunkPos{x, z})
				if !ok || len(c.Entities) == 0 {
					// The chunk wasn't loaded, so there are no entities here.
					continue
				}
				more := c.iterateEntities(func(handle *EntityHandle) bool {
					if !box.Vec3Within(handle.data.Pos) {
						return true
					}
					// An entity removed while the column is walked has no
					// state any more.
					state := handle.state
					if state == nil {
						return true
					}
					return yield(state.entity(tx, handle))
				})
				if !more {
					return
				}
			}
		}
	}
}

// entitiesOfTypesWithin yields the entities entitiesWithin yields for box
// whose type encodes to one of types, in the same order, without opening
// the others.
func (w *World) entitiesOfTypesWithin(tx *Tx, box cube.BBox, types []string) iter.Seq[Entity] {
	return func(yield func(Entity) bool) {
		minPos, maxPos := chunkPosFromVec3(box.Min()), chunkPosFromVec3(box.Max())
		for x := minPos[0]; x <= maxPos[0]; x++ {
			for z := minPos[1]; z <= maxPos[1]; z++ {
				c, ok := tx.loadedColumn(ChunkPos{x, z})
				if !ok || len(c.Entities) == 0 {
					continue
				}
				more := c.iterateEntities(func(handle *EntityHandle) bool {
					if !box.Vec3Within(handle.data.Pos) || !slices.Contains(types, handle.t.EncodeEntity()) {
						return true
					}
					state := handle.state
					if state == nil {
						return true
					}
					return yield(state.entity(tx, handle))
				})
				if !more {
					return
				}
			}
		}
	}
}

// tickingEntities yields the entities of the columns tickEntities ticks
// actively, in its order.
func (w *World) tickingEntities(tx *Tx) iter.Seq[Entity] {
	return func(yield func(Entity) bool) {
		refs := w.entityColumns
		if r := tx.region; r != nil {
			refs = r.refs
		}
		for _, ref := range refs {
			col := ref.col
			if col == nil || len(col.Entities) == 0 || !col.Ready() || !w.entitiesTick(col) || !w.entityTickingReady(ref.pos) {
				continue
			}
			more := col.iterateEntities(func(handle *EntityHandle) bool {
				state := handle.state
				if state == nil {
					return true
				}
				return yield(state.entity(tx, handle))
			})
			if !more {
				return
			}
		}
	}
}

// allEntities returns an iterator that yields all entities in the World.
func (w *World) allEntities(tx *Tx) iter.Seq[Entity] {
	return w.entitiesOfType(tx, nil)
}

// entitiesOfType returns an iterator that yields the entities in the World
// whose type match returns true for, or all of them if match is nil.
func (w *World) entitiesOfType(tx *Tx, match func(EntityType) bool) iter.Seq[Entity] {
	return func(yield func(Entity) bool) {
		// The handles are listed first, so entities may join or leave the
		// world while they are walked.
		tx.lockShared()
		handles := make([]*EntityHandle, 0, len(w.entities))
		for handle := range w.entities {
			if match != nil && !match(handle.t) {
				continue
			}
			if r := tx.region; r == nil || r.ownsEntity(w, handle) {
				handles = append(handles, handle)
			}
		}
		tx.unlockShared()
		for _, handle := range handles {
			state := handle.state
			if state == nil || handle.w != w {
				continue
			}
			if ent := state.entity(tx, handle); ent != nil {
				if !yield(ent) {
					return
				}
			}
		}
	}
}

// allPlayers returns an iterator that yields all player entities in the World.
func (w *World) allPlayers(tx *Tx) iter.Seq[Entity] {
	return func(yield func(Entity) bool) {
		// A snapshot of the players, as yielding may add or remove some.
		tx.lockShared()
		handles := w.playerList
		if r := tx.region; r != nil {
			handles = make([]*EntityHandle, 0, len(w.playerList))
			for _, handle := range w.playerList {
				if r.ownsEntity(w, handle) {
					handles = append(handles, handle)
				}
			}
		}
		tx.unlockShared()
		for _, handle := range handles {
			state := handle.state
			if state == nil || handle.w != w {
				continue
			}
			if ent := state.entity(tx, handle); ent != nil {
				if !yield(ent) {
					return
				}
			}
		}
	}
}

// trackPlayer records a handle just put in entities if it is a player.
func (w *World) trackPlayer(handle *EntityHandle) {
	if _, ok := w.players[handle]; ok || handle.t.EncodeEntity() != "minecraft:player" {
		return
	}
	w.players[handle] = struct{}{}
	w.playerList = append(slices.Clip(w.playerList), handle)
	w.playerMoves.Add(1)
}

// untrackPlayer forgets a handle leaving entities if it is a player.
func (w *World) untrackPlayer(handle *EntityHandle) {
	if _, ok := w.players[handle]; !ok {
		return
	}
	delete(w.players, handle)
	i := slices.Index(w.playerList, handle)
	w.playerList = slices.Concat(w.playerList[:i], w.playerList[i+1:])
	w.playerMoves.Add(1)
}

// scheduleBlockUpdate schedules a live update after a specific delay. Liquids
// are stored in the distinct fluid queue; other blocks use the block queue.
func (w *World) scheduleBlockUpdate(pos cube.Pos, b Block, delay time.Duration) {
	if pos.OutOfBounds(w.Range()) {
		return
	}
	queue := w.scheduledBlockTicks
	if _, ok := b.(Liquid); ok {
		queue = w.scheduledFluidTicks
	}
	if queue.schedule(w.conf.Blocks, pos, b, delay) {
		w.markScheduledTickColumnModified(pos)
	}
}

func (w *World) markScheduledTickColumnModified(pos cube.Pos) {
	if col, ok := w.chunks[chunkPosFromBlockPos(pos)]; ok {
		col.modified = true
	}
}

// resolveScheduledTickBlock maps a Java registry type back to a registered
// block implementation for dispatching a persisted or generated tick.
func (w *World) resolveScheduledTickBlock(tick chunk.ScheduledTick) (Block, bool) {
	if tick.Kind != chunk.BlockTick && tick.Kind != chunk.FluidTick {
		return nil, false
	}
	if tick.Kind == chunk.FluidTick && tick.Type == "minecraft:empty" {
		// EmptyFluid's legacy block is native air. It still owns a Source tick
		// record, even though dispatch against an empty fluid changes no blocks.
		return w.conf.Blocks.BlockByRuntimeID(w.conf.Blocks.AirRuntimeID())
	}
	key := scheduledTickTypeKey{kind: tick.Kind, typeName: tick.Type}
	if b, ok := w.scheduledTickBlocks[key]; ok {
		return b, true
	}
	if tick.Kind == chunk.BlockTick {
		if _, err := gamedata.BlockStateID(tick.Type, nil); err == nil {
			native, err := ResolveState(w.conf.Blocks, BlockState{Name: tick.Type})
			if err == nil {
				if b, ok := w.conf.Blocks.BlockByRuntimeID(native.Block); ok {
					if w.scheduledTickBlocks == nil {
						w.scheduledTickBlocks = make(map[scheduledTickTypeKey]Block)
					}
					w.scheduledTickBlocks[key] = b
					return b, true
				}
			}
		}
	}
	for rid := uint32(0); rid < uint32(w.conf.Blocks.BlockCount()); rid++ {
		b, ok := w.conf.Blocks.BlockByRuntimeID(rid)
		if !ok {
			continue
		}
		var typeName string
		if tick.Kind == chunk.FluidTick {
			liquid, ok := b.(Liquid)
			if !ok {
				continue
			}
			typeName = scheduledFluidTickType(liquid)
		} else {
			typeName = scheduledTickType(w.conf.Blocks, b)
		}
		if typeName == tick.Type {
			if w.scheduledTickBlocks == nil {
				w.scheduledTickBlocks = make(map[scheduledTickTypeKey]Block)
			}
			w.scheduledTickBlocks[key] = b
			return b, true
		}
	}
	return nil, false
}

// scheduleGeneratedTick installs a worldless generation result on the owner
// thread. Source ProtoChunkTicks converts every generated delay to zero.
func (w *World) scheduleGeneratedTick(tick chunk.ScheduledTick) {
	_, ok := w.resolveScheduledTickBlock(tick)
	if !ok {
		panic(fmt.Sprintf("generated tick: unknown %v type %q", tick.Kind, tick.Type))
	}
	queue := w.scheduledBlockTicks
	if tick.Kind == chunk.FluidTick {
		queue = w.scheduledFluidTicks
	}
	if queue.insertTypeTick(tick.Pos, tick.Type, 0, tick.Priority, true) {
		w.markScheduledTickColumnModified(tick.Pos)
	}
}

// doBlockUpdatesAround schedules block updates directly around and on the
// position passed.
func (tx *Tx) doBlockUpdatesAround(pos cube.Pos) {
	w := tx.World()
	if w == nil || pos.OutOfBounds(w.Range()) {
		return
	}
	tx.lockShared()
	defer tx.unlockShared()
	changed := pos

	tx.updateNeighbour(pos, changed)
	pos.Neighbours(func(pos cube.Pos) {
		tx.updateNeighbour(pos, changed)
	}, w.Range())
}

func (tx *Tx) updateNeighbour(pos, changedNeighbour cube.Pos) {
	tx.lockShared()
	defer tx.unlockShared()
	deferred := true
	if b, loaded := tx.BlockLoaded(pos); loaded {
		if listener, ok := b.(NeighbourChangeListener); ok {
			listener.NeighbourChanged(pos, changedNeighbour, tx)
			deferred = false
		}
		if liquid, ok := tx.additionalLiquid(pos); ok {
			if listener, ok := liquid.(NeighbourChangeListener); ok {
				listener.NeighbourChanged(pos, changedNeighbour, tx)
			}
		}
	}
	if deferred {
		tx.World().updateNeighbour(pos, changedNeighbour)
	}
}

// neighbourUpdate represents a position that needs to be updated because of a
// neighbour that changed.
type neighbourUpdate struct {
	pos, neighbour cube.Pos
}

// updateNeighbour ticks the position passed as a result of the neighbour
// passed being updated.
func (w *World) updateNeighbour(pos, changedNeighbour cube.Pos) {
	w.neighbourUpdates.push(neighbourUpdate{pos: pos, neighbour: changedNeighbour})
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

// SaveWorld saves the world of the transaction like World.Save, from within
// the transaction, where Save would wait for the goroutine running it. As
// Java's save-all without flush, it does not wait for the chunks to reach
// the disk.
func (tx *Tx) SaveWorld() {
	if tx.region != nil {
		tx.deferToOwner(func(tx *Tx) { tx.SaveWorld() })
		return
	}
	_ = tx.World().capture(tx, false, nil).write(false)
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

// chunk reads a chunk from the position passed. If a chunk at that position is
// not yet loaded, the chunk is loaded from the provider, or generated if it
// did not yet exist. Additionally, chunks newly loaded have the light in them
// calculated before they are returned.
func (tx *Tx) chunk(pos ChunkPos) *Column {
	if r := tx.region; r != nil {
		return r.columnOrVoid(tx.w, pos)
	}
	w := tx.World()
	if w.lastChunk != nil && w.lastChunkPos == pos {
		return w.lastChunk
	}
	c, ok := w.chunks[pos]
	if ok && c.settled {
		w.lastChunkPos, w.lastChunk = pos, c
		return c
	}
	if ok {
		w.waitColumnReady(pos, c)
		c.ensureLight(w, pos)
		w.finishChunkRequest(tx, pos, c)
		if c.settled = c.Ready() && c.poisIndexed && c.circuitsIndexed; c.settled {
			delete(w.unsettled, pos)
		}
		return c
	}
	c, err := w.loadChunk(pos)
	w.waitColumnReady(pos, c)
	c.ensureLight(w, pos)
	w.finishChunkRequest(tx, pos, c)
	if err != nil {
		w.log.Error("load chunk: "+err.Error(), "X", pos[0], "Z", pos[1])
	}
	return c
}

// LoadArea loads the chunks overlapping the horizontal block area from min to
// max, generating those that do not exist yet ahead of the chunks nothing
// waits for. It does not wait for them: the returned channel is closed once
// every chunk is ready, so a caller off the world goroutine can wait for an
// area without holding up the ticks of the world.
func (tx *Tx) LoadArea(min, max cube.Pos) <-chan struct{} {
	w := tx.World()
	done := make(chan struct{})
	if tx.region != nil {
		// Regions load no chunks: the owner starts loading them after the
		// phase.
		tx.deferToOwner(func(tx *Tx) {
			loaded := tx.LoadArea(min, max)
			go func() {
				<-loaded
				close(done)
			}()
		})
		return done
	}
	minX, minZ, maxX, maxZ := int32(min[0]>>4), int32(min[2]>>4), int32(max[0]>>4), int32(max[2]>>4)
	pending := int((maxX - minX + 1) * (maxZ - minZ + 1))
	if pending <= 0 {
		close(done)
		return done
	}
	ready := func(*Tx, *Column) {
		if pending--; pending == 0 {
			close(done)
		}
	}
	for x := minX; x <= maxX; x++ {
		for z := minZ; z <= maxZ; z++ {
			pos := ChunkPos{x, z}
			if !w.loadChunkAsync(tx, pos, ready) {
				ready(tx, nil)
				continue
			}
			if w.generatorQueue != nil {
				w.generatorQueue.prioritize(pos)
			}
		}
	}
	return done
}

// pendingMissingBlockState retains a partial state until its horizontal neighbours are available.
type pendingMissingBlockState struct {
	properties []string
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

func (w *World) addActiveColumn(pos ChunkPos, col *Column) {
	if w.activeColumnIndex == nil {
		w.activeColumnIndex = make(map[ChunkPos]int)
	}
	if idx, ok := w.activeColumnIndex[pos]; ok {
		w.activeColumns[idx].col = col
		return
	}
	w.activeColumns = append(w.activeColumns, columnRef{pos: pos, col: col})
	w.activeColumnIndex[pos] = len(w.activeColumns) - 1
}

func (w *World) removeActiveColumn(pos ChunkPos) {
	if len(w.activeColumns) == 0 {
		return
	}
	idx, ok := w.activeColumnIndex[pos]
	if !ok {
		return
	}
	last := len(w.activeColumns) - 1
	if idx != last {
		w.activeColumns[idx] = w.activeColumns[last]
		w.activeColumnIndex[w.activeColumns[idx].pos] = idx
	}
	w.activeColumns = w.activeColumns[:last]
	delete(w.activeColumnIndex, pos)
}

func (w *World) addEntityColumn(pos ChunkPos, col *Column) {
	if col == nil || len(col.Entities) == 0 {
		w.removeEntityColumn(pos)
		return
	}
	if w.entityColumnIndex == nil {
		w.entityColumnIndex = make(map[ChunkPos]int)
	}
	if idx, ok := w.entityColumnIndex[pos]; ok {
		w.entityColumns[idx].col = col
		return
	}
	w.entityColumns = append(w.entityColumns, columnRef{pos: pos, col: col})
	w.entityColumnIndex[pos] = len(w.entityColumns) - 1
}

func (w *World) removeEntityColumn(pos ChunkPos) {
	if len(w.entityColumns) == 0 {
		return
	}
	idx, ok := w.entityColumnIndex[pos]
	if !ok {
		return
	}
	last := len(w.entityColumns) - 1
	if idx != last {
		w.entityColumns[idx] = w.entityColumns[last]
		w.entityColumnIndex[w.entityColumns[idx].pos] = idx
	}
	w.entityColumns = w.entityColumns[:last]
	delete(w.entityColumnIndex, pos)
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
