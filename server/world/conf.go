package world

import (
	"fmt"
	"log/slog"
	"math"
	"math/rand/v2"
	"runtime"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/google/uuid"
)

type blockRegistrySetter interface {
	// SetBlockRegistry updates the registry used by the provider to encode and decode blocks.
	// Config.New calls it with Config.Blocks after applying the default registry and finalizing it.
	SetBlockRegistry(BlockRegistry)
}

// Config may be used to create a new World. It holds a variety of fields that
// influence the World.
type Config struct {
	// Log is the Logger that will be used to log errors and debug messages to.
	// If set to nil, slog.Default() is set.
	Log *slog.Logger
	// Dim is the Dimension of the World. If set to nil, the World will use
	// Overworld as its dimension. The dimension set here influences, among
	// others, the sky colour, weather/time and liquid behaviour in that World.
	Dim Dimension
	// PortalDestination is a function that returns the destination World for a
	// portal of a specific Dimension type. If set to nil, no portals will
	// function. If the function returns a nil world for a Dimension, only
	// portals of that specific Dimension type will not function.
	PortalDestination func(dim Dimension) *World
	// PortalDisabledMessage should return the message to broadcast when portals to a
	// specific dimension are disabled. Returning an empty string suppresses the message.
	PortalDisabledMessage func(dim Dimension) string
	// DefaultWorld should return the primary world that players fall back to when no other
	// dimension is available. If left nil or returning nil, the World itself is treated as the
	// default for any lookups.
	DefaultWorld func() *World
	// Provider is the Provider implementation used to read and write World
	// data. If set to nil, the Provider used will be NopProvider, which does
	// not store any data to disk.
	Provider Provider
	// Seed is selected for a new World only. A stored provider seed takes
	// precedence, including zero. Initialise a custom Generator with the seed
	// returned by Provider.Settings().ResolveSeed before constructing the World.
	Seed int64
	// Generator is the Generator implementation used to generate new areas of
	// the World. If set to nil, the Generator used will be NopGenerator, which
	// generates completely empty chunks.
	Generator Generator
	// GeneratorProfile selects how chunk generation uses the CPUs of the host:
	// the default worker count and, for generators that support it, their
	// cache sizes and parallelism. The zero value picks a profile from the
	// number of logical CPUs.
	GeneratorProfile GeneratorProfile
	// GeneratorWorkers specifies the number of background workers used to run
	// chunk generation. If set to 0 or lower, GeneratorProfile selects it.
	// Large terrain preloads may benefit from a higher worker count, but
	// profiling is recommended when the generator is constrained by external
	// I/O (for example LevelDB access).
	GeneratorWorkers int
	// GeneratorQueueSize specifies how many chunk generation tasks may wait for
	// a worker before a backlog warning is logged. Further tasks still queue:
	// chunk requests never wait for the workers. If set to 0 or lower, a size
	// proportional to the worker count is selected automatically. A sustained
	// backlog suggests raising GeneratorWorkers.
	GeneratorQueueSize int
	// ReadOnly specifies if the World should be read-only, meaning no new data
	// will be written to the Provider.
	ReadOnly bool
	// SaveChunks selects which loaded columns a save or an unload stores:
	// the zero value, ChunkSavingModified, stores only those the provider may
	// lack, while ChunkSavingAll stores every column read or generated, as
	// for pregenerating a world. A column standing in for one that failed to
	// load is never stored.
	SaveChunks ChunkSaving
	// SaveInterval specifies how often a World should be automatically saved to
	// disk. This includes chunks, entities and level.dat data. If ReadOnly is
	// set to false, changing SaveInterval will have no effect.
	// By default, SaveInterval is set to 10 minutes. Setting SaveInterval to
	// a negative number disables automatic saving entirely, as a server does
	// for its worlds, which it saves with its checkpoints (CaptureAll).
	SaveInterval time.Duration
	// ChunkUnloadInterval specifies how often unused chunks should be unloaded
	// from memory when no longer in use. By default, this is set to 2 minutes.
	// ChunkUnloadInterval should not be used to prevent chunks from unloading
	// altogether. This should be done using a Loader with a custom Viewer.
	ChunkUnloadInterval time.Duration
	// RandomTickSpeed specifies the rate at which blocks should be ticked in
	// the World. By default, each sub chunk has 3 blocks randomly ticked per
	// sub chunk, so the default value is 3. Setting this value to -1 or lower
	// will stop random ticking altogether, while setting it higher results in
	// faster ticking.
	RandomTickSpeed int
	// RandSource is the rand.Source used for generation of random numbers in a
	// World, such as when selecting blocks to tick or when deciding where to
	// strike lightning. If nil, it uses Bedrock's MT19937 raw stream seeded
	// from the low 32 bits of the resolved world seed. An explicitly supplied
	// source retains Go's rand.Source semantics.
	RandSource rand.Source
	// Entities is an EntityRegistry with all Entity types registered that may
	// be added to the World.
	Entities EntityRegistry

	// Blocks is the BlockRegistry used by the World.
	// If left nil, DefaultBlockRegistry is used. For a non-default registry,
	// use NewBlockRegistry(), register blocks/states, and call Finalize().
	Blocks BlockRegistry

	// Synchronous removes the World's own background goroutines. Immediate tasks
	// from World.Do and Call run on the calling goroutine, the World is not saved
	// or unloaded automatically, and time only passes on explicit
	// World.AdvanceTick calls. World.DoAfter and entity work scheduled before an
	// entity enters a world still use background goroutines and wall-clock
	// delays; callers must synchronise on the returned Task. This makes
	// Synchronous Worlds well suited to unit tests that need a World to interact
	// with.
	// A Synchronous World must be driven from one goroutine. Do, Call and
	// AdvanceTick are not safe to call concurrently, including from delayed
	// item or death callbacks.
	Synchronous bool
	// Regions lets the entities of parts of the World far apart, as around
	// players far from each other, tick at the same time on goroutines of
	// their own. The Handler of the World is still called by one goroutine
	// at a time, but the handlers of its entities, such as those of players,
	// may then be called from several goroutines at once, each for a
	// different part of the World. A Synchronous World ticks its regions one
	// after another.
	Regions bool
	// Fault, if not nil, is called with every panic the World recovered from
	// and blamed on part of its game, as an entity or a block, after it was
	// logged. Logs are rate-limited, Fault is not: it is meant for metrics or
	// a plugin. It is called on the goroutine that recovered, which in a
	// region phase is a worker of a region: it may be called by several
	// goroutines at once and must not wait for the World.
	Fault func(f Fault)
	// StagingLimit is how many bytes the columns the World unloaded since it
	// was last saved may take, encoded, before it asks to be saved at once.
	// Those columns are staged, rather than written as they unload, so that
	// the disk holds the World as it was at a save. By default it is
	// DefaultStagingLimit; a negative limit is none.
	StagingLimit int
	// StagingFull, if not nil, is called when the columns staged exceed the
	// StagingLimit, with the bytes they take, once until the World is next
	// saved: a server saves all its worlds then. If nil, the World saves
	// itself, as World.Save does. It is called on a goroutine of its own.
	StagingFull func(w *World, bytes int)
	// Fatal, if not nil, is called once, with the first panic the World
	// could not blame on anything, as one of the bookkeeping of its tick or
	// one of a subsystem of the tick in 20 ticks in a row. The World then
	// failed: it no longer simulates, and its ticks only have the
	// controllers of its players act, so that their clients can leave it.
	// Its transactions still run, and closing it still saves it. A server
	// stops in order on it, saving what it can. It is called on the owner
	// of the World, which it must not wait for.
	Fatal func(f Fault)
}

// New creates a new World using the Config conf. The World returned will start
// ticking as soon as a viewer is added to it and is otherwise ready for use.
func (conf Config) New() *World {
	if conf.Log == nil {
		conf.Log = slog.Default()
	}
	if conf.Dim == nil {
		conf.Dim = Overworld
	}
	if conf.SaveInterval == 0 {
		conf.SaveInterval = time.Minute * 10
	}
	if conf.StagingLimit == 0 {
		conf.StagingLimit = DefaultStagingLimit
	}
	if conf.ChunkUnloadInterval <= 0 {
		conf.ChunkUnloadInterval = time.Minute * 2
	}
	if conf.Generator == nil {
		conf.Generator = NopGenerator{}
	}
	conf.GeneratorProfile = conf.GeneratorProfile.resolve(runtime.NumCPU())
	if conf.GeneratorWorkers <= 0 {
		conf.GeneratorWorkers = conf.GeneratorProfile.workers(runtime.NumCPU())
	}
	if tunable, ok := conf.Generator.(TunableGenerator); ok {
		tunable.TuneGeneration(GenerationTuning{Workers: conf.GeneratorWorkers, Parallel: conf.GeneratorProfile == GeneratorProfileManyCores})
	}
	if conf.GeneratorQueueSize <= 0 {
		conf.GeneratorQueueSize = conf.GeneratorWorkers * 4
	}
	if conf.Provider == nil {
		// If no provider is set, use the default settings and the default spawn position from the generator.
		s := defaultSettings()
		s.Spawn = conf.Generator.DefaultSpawn(conf.Dim)
		conf.Provider = NopProvider{Set: s}
	}
	switch provider := conf.Provider.(type) {
	case NopProvider:
		if provider.Set == nil {
			provider.Set = defaultSettings()
			conf.Provider = provider
		}
	case *NopProvider:
		if provider.Set == nil {
			provider.Set = defaultSettings()
		}
	}
	if conf.RandomTickSpeed == 0 {
		conf.RandomTickSpeed = 3
	}
	if conf.Blocks == nil {
		conf.Blocks = DefaultBlockRegistry
	}

	// Initialize the passed block registry and also initialize the default block registry which
	// is used in some vanilla paths.
	conf.Blocks.Finalize()
	DefaultBlockRegistry.Finalize()
	providerUse := retainProvider(conf.Provider, !conf.ReadOnly)
	constructed := false
	var w *World
	defer func() {
		if constructed {
			return
		}
		if w != nil {
			w.closed.Store(true)
			close(w.closeStarted)
			w.generatorRunning.Wait()
			w.set.Lock()
			w.set.unregisterWorldLocked(w)
			w.set.Unlock()
		}
		if releaseProvider(providerUse) {
			_, _ = providerUse.closeProvider(conf.Provider)
		}
	}()
	if provider, ok := conf.Provider.(blockRegistrySetter); ok {
		provider.SetBlockRegistry(conf.Blocks)
	}
	if err := providerUse.loadPositionTracker(conf.Provider); err != nil {
		conf.Log.Error("load position tracking data: " + err.Error())
	}
	if err := loadGenerationState(conf.Provider, conf.Generator, conf.Dim); err != nil {
		panic(fmt.Errorf("load generation state: %w", err))
	}

	s := conf.Provider.Settings()
	conf.Seed = s.ResolveSeed(conf.Seed)
	if conf.RandSource == nil {
		conf.RandSource = newLevelRandomSource(uint32(conf.Seed))
	}
	s.Lock()
	settingsLocked := true
	defer func() {
		if settingsLocked {
			s.Unlock()
		}
	}()
	currentTick := s.CurrentTick
	w = &World{
		scheduledBlockTicks: newScheduledTickQueue(currentTick),
		scheduledFluidTicks: newScheduledFluidTickQueue(currentTick),
		redstone:            newRedstoneEngine(currentTick),
		entities:            make(map[*EntityHandle]*entityState),
		players:             make(map[*EntityHandle]struct{}),
		entitiesByUUID:      make(map[uuid.UUID]*EntityHandle),
		entitiesByUniqueID:  make(map[int64]*EntityHandle),
		lootSequences:       make(map[string]*vanilla.Random),
		pois:                make(map[cube.Pos]POI),
		raids:               make(map[int64]*Raid),
		raidByRaider:        make(map[uuid.UUID]int64),
		viewers:             make(map[*Loader]Viewer),
		chunks:              make(map[ChunkPos]*Column),
		chunkRequests:       make(map[ChunkPos][]chunkCallback),
		unsettled:           make(map[ChunkPos]*Column),
		lightFills:          make(chan struct{}, runtime.NumCPU()),
		workerLocals:        []map[any]any{make(map[any]any)},
		queueClosing:        make(chan struct{}),
		queueWake:           make(chan struct{}, 1),
		closeStarted:        make(chan struct{}),
		closing:             make(chan struct{}),
		queue:               make(chan transaction, 128),
		generatorQueue:      newGenerationQueue(),
		stats:               newStats(),
		r:                   rand.New(conf.RandSource),
		conf:                conf,
		ra:                  conf.Dim.Range(),
		set:                 s,
		providerUse:         providerUse,
		tick:                currentTick,
		activeColumnIndex:   make(map[ChunkPos]int),
		entityColumnIndex:   make(map[ChunkPos]int),
	}
	w.scheduledBlockTicks.faults = &w.blockFaults
	w.scheduledFluidTicks.faults = &w.blockFaults
	if err := w.loadBorder(); err != nil {
		panic(err)
	}
	if !conf.Synchronous && !conf.ReadOnly {
		w.saver = newChunkSaver(w)
	}
	w.loadLootRandomSequences()
	w.loadVillagePOIs()
	w.loadRaids()
	if s.worlds == nil {
		s.worlds = make(map[*World]struct{})
	}
	if s.owner == nil {
		s.owner = w
		w.advance = true
	}
	s.worlds[w] = struct{}{}
	s.Unlock()
	settingsLocked = false
	if err := w.loadDragonFight(); err != nil {
		panic(err)
	}
	w.weather = weather{w: w}
	var h Handler = NopHandler{}
	w.handler.Store(&h)
	w.tps.Store(math.Float64bits(20))

	t := ticker{interval: time.Second / 20}
	if !conf.Synchronous {
		// The initial tick may generate the spawn column. Its readiness must not
		// depend on workers that are only started after that tick returns.
		w.running.Add(conf.GeneratorWorkers)
		w.generatorRunning.Add(conf.GeneratorWorkers)
		for range conf.GeneratorWorkers {
			go w.generatorWorker()
		}
	}
	// The first tick is not guarded as the others are: a panic of it is one
	// of constructing the World, which New undoes and raises.
	if conf.Synchronous {
		<-w.execTick(t.tickWorld)
	} else {
		c := make(chan struct{})
		normalTransaction{c: c, f: t.tickWorld, tick: true}.Run(w)
	}
	if !conf.Synchronous {
		w.queueing.Add(1)
		w.running.Add(3)

		go t.tickLoop(w)
		go w.autoSave()
		go w.watchTicks()
		go w.handleTransactions()
	}

	constructed = true
	return w
}
