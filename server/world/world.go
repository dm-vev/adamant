package world

import (
	"log/slog"
	"math/rand/v2"
	"sync/atomic"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/google/uuid"
)

// World implements a Minecraft world. It manages all aspects of what players
// can see, such as blocks, entities and particles. World generally provides a
// synchronised state: All entities, blocks and players usually operate in this
// world, so World ensures that all its methods will always be safe for
// simultaneous calls. A nil *World is safe to use but not functional.
type World struct {
	txQueue
	lifecycle
	clock
	rules
	structureState
	blockState
	generation
	lightQueue
	randomness
	poiState
	raidState
	endState
	saveState
	viewerState
	faultState

	conf Config
	// log is the logger of the world; genLog and saveLog are those of its
	// generation, lighting and saves. Each is the logger of the Config
	// scoped to a component of package logging.
	log, genLog, saveLog *slog.Logger
	ra                   cube.Range
	weather
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
	chunkCount             atomic.Int64
	entityCount            atomic.Int64
	blockTickingPass       bool
	pendingBlockTickChunks map[ChunkPos]struct{}
	// stats is what the world counts of itself, for metrics.
	stats *Stats
	// Scheduled updates are partitioned by native registry: blocks drain before
	// fluids, and each queue owns ticks by their scheduled position.
	scheduledBlockTicks          *scheduledTickQueue
	scheduledFluidTicks          *scheduledTickQueue
	scheduledTickBlocks          map[scheduledTickTypeKey]Block
	redstone                     *redstoneEngine
	neighbourUpdates             neighbourUpdateQueue
	deferredNeighbours           []neighbourUpdate
	scratchPrefetch              []prefetchItem
	scratchRegionRefs            []columnRef
	scratchViews                 []ReadView
	scratchRandom                []cube.Pos
	scratchRandomScan            []randomTickScan
	scratchRandomFound           [][]cube.Pos
	scratchBlockEntities         []blockEntityTickRef
	scratchLoaderAreas           []loaderActiveArea
	scratchFocus                 []ChunkPos
	scratchInhabitedPlayers      []inhabitedTimePlayer
	scratchPostProcessingColumns []columnRef
	activeColumns                []columnRef
	activeColumnIndex            map[ChunkPos]int
	entityColumns                []columnRef
	entityColumnIndex            map[ChunkPos]int
	// entityTickingStamp changes every tick. The columns whose entities
	// tick this tick hold it, plus one.
	entityTickingStamp uint64
	// entityTickingSeen holds the areas whose columns were stamped this
	// tick, so that loaders standing together stamp theirs once.
	entityTickingSeen map[loaderActiveArea]struct{}
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
