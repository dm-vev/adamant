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
	// SaveInterval specifies how often a World should be automatically saved to
	// disk. This includes chunks, entities and level.dat data. If ReadOnly is
	// set to false, changing SaveInterval will have no effect.
	// By default, SaveInterval is set to 10 minutes. Setting SaveInterval to
	// a negative number disables automatic saving entirely.
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
	// strike lightning. If set to nil, RandSource defaults to a `rand.PCG`
	// source seeded with `time.Now().UnixNano()`. PCG is significantly faster
	// than `rand.ChaCha8` on 64-bit systems at the expense of poorer
	// statistical distribution, which is acceptable here.
	// See https://go.dev/blog/chacha8rand.
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

	if conf.RandSource == nil {
		t := uint64(time.Now().UnixNano())
		conf.RandSource = rand.NewPCG(t, t)
	}
	s := conf.Provider.Settings()
	conf.Seed = s.ResolveSeed(conf.Seed)
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
		queueClosing:        make(chan struct{}),
		queueWake:           make(chan struct{}, 1),
		closeStarted:        make(chan struct{}),
		closing:             make(chan struct{}),
		queue:               make(chan transaction, 128),
		generatorQueue:      newGenerationQueue(),
		r:                   rand.New(conf.RandSource),
		conf:                conf,
		ra:                  conf.Dim.Range(),
		set:                 s,
		providerUse:         providerUse,
		tick:                currentTick,
		activeColumnIndex:   make(map[ChunkPos]int),
		entityColumnIndex:   make(map[ChunkPos]int),
	}
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
	if conf.Synchronous {
		<-w.exec(t.tick)
	} else {
		c := make(chan struct{})
		normalTransaction{c: c, f: t.tick}.Run(w)
	}
	if !conf.Synchronous {
		w.queueing.Add(1)
		w.running.Add(2)

		go t.tickLoop(w)
		go w.autoSave()
		go w.handleTransactions()
	}

	constructed = true
	return w
}
