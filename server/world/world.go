package world

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"iter"
	"maps"
	"math"
	"math/rand/v2"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/goleveldb/leveldb"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// World implements a Minecraft world. It manages all aspects of what players
// can see, such as blocks, entities and particles. World generally provides a
// synchronised state: All entities, blocks and players usually operate in this
// world, so World ensures that all its methods will always be safe for
// simultaneous calls. A nil *World is safe to use but not functional.
type columnRef struct {
	pos ChunkPos
	col *Column
}

type loaderActiveArea struct {
	pos      ChunkPos
	radius   int32
	radiusSq int64
}

type World struct {
	conf Config
	ra   cube.Range

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
	// tick is the per-world tick counter used by worlds that do not advance the
	// shared Settings, such as the Nether, End and custom dimensions.
	tick    int64
	handler atomic.Pointer[Handler]

	weather

	// closeStarted closes as soon as World.Close begins, before the close
	// transaction runs; closing closes once the world stops ticking.
	closeStarted chan struct{}
	closing      chan struct{}
	running      sync.WaitGroup

	generatorRunning sync.WaitGroup
	generatorEnqueue sync.WaitGroup

	// chunks holds a cache of chunks currently loaded. These chunks are cleared
	// from this map after some time of not being used.
	chunks        map[ChunkPos]*Column
	chunkRequests map[ChunkPos][]chunkCallback

	// entities holds a map of entities currently loaded and metadata associated
	// with them, such as the last chunk position they were located in and a
	// cached instance of the opened entity. The cached entity instance allows us
	// to reuse the same Go object across ticks instead of re-opening it each
	// iteration, which significantly reduces pressure on the garbage collector
	// and CPU usage during the entity pipeline.
	entities map[*EntityHandle]*entityState

	// chunkCount and entityCount track the map sizes so callers outside the world
	// goroutine can read counts without racing map mutations.
	chunkCount  atomic.Int64
	entityCount atomic.Int64

	r *rand.Rand

	// weatherRand is used for out-of-band weather changes to avoid racing the
	// main world RNG, which is owned by the tick loop.
	weatherRandMu sync.Mutex
	weatherRand   *rand.Rand

	tps atomic.Uint64

	// scheduledUpdates is a map of tick time values indexed by the block
	// position at which an update is scheduled. If the current tick exceeds the
	// tick value passed, the block update will be performed and the entry will
	// be removed from the map.
	scheduledUpdates *scheduledTickQueue
	redstone         *redstoneEngine
	neighbourUpdates []neighbourUpdate

	scratchRandom        []cube.Pos
	scratchBlockEntities []cube.Pos
	scratchLoaderAreas   []loaderActiveArea

	activeColumns     []columnRef
	activeColumnIndex map[ChunkPos]int
	entityColumns     []columnRef
	entityColumnIndex map[ChunkPos]int

	viewerMu sync.Mutex
	viewers  map[*Loader]Viewer

	generatorQueue chan generationTask
	// generatorQueueSaturation counts how often chunk generation tasks had to
	// wait because the worker queue was full. We use this to
	// rate-limit backpressure warnings so operators can tune queue/worker sizes.
	generatorQueueSaturation atomic.Uint64
	lastQueueSaturationLog   atomic.Uint64
}

// currentTickLocked returns the tick used to update this world. The caller
// must hold the shared settings lock.
func (w *World) currentTickLocked() int64 {
	if w.advance {
		return w.set.CurrentTick
	}
	return w.tick
}

const (
	TimeDay      = 1000
	TimeNoon     = 6000
	TimeSunset   = 12000
	TimeNight    = 13000
	TimeMidnight = 18000
	TimeSunrise  = 23000
	TimeFull     = 24000
)

type entityState struct {
	pos               ChunkPos
	ent               Entity
	lastTick          int64
	lastProcessedTick int64
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
}

func (s *entityState) entity(tx *Tx, handle *EntityHandle) Entity {
	if s == nil {
		return nil
	}
	if s.ent == nil {
		s.ent = handle.mustEntity(tx)
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
	}
	if binder, ok := s.ent.(interface{ BindTransaction(*Tx) }); ok {
		binder.BindTransaction(tx)
	}
	return s.ent
}

type generationTask struct {
	pos ChunkPos
	col *Column
}

type chunkCallback func(tx *Tx, col *Column)

// transaction is a type that may be added to the transaction queue of a World.
// Its Run method is called when the transaction is taken out of the queue.
type transaction interface {
	Run(w *World)
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

// CurrentTick returns the current tick counter of the world.
func (w *World) CurrentTick() int64 {
	if w == nil {
		return 0
	}
	w.set.Lock()
	defer w.set.Unlock()
	return w.currentTickLocked()
}

// TPS returns the current average ticks per second of the world. The value is
// averaged over the last tpsSampleSize ticks and may be zero if no samples have
// been recorded yet.
func (w *World) TPS() float64 {
	if w == nil {
		return 0
	}
	return math.Float64frombits(w.tps.Load())
}

// LoadedChunkCount returns the number of chunks currently kept in memory by the
// world.
func (w *World) LoadedChunkCount() int {
	if w == nil {
		return 0
	}
	return int(w.chunkCount.Load())
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

// execFunc is a function that performs a synchronised transaction on a World.
type execFunc func(tx *Tx)

// ExecFunc is kept for Adamant callers that still use the legacy blocking API.
type ExecFunc func(tx *Tx)

// Exec performs a legacy blocking transaction. New off-owner code should use
// Do or Call so scheduling cannot block the caller when the queue is full.
func (w *World) Exec(f ExecFunc) <-chan struct{} {
	return w.exec(execFunc(f))
}

// exec runs f on the World, bypassing the closed check that Do/DoAfter/Call
// apply — reserved for the World's own machinery (ticking, saving, chunk
// unload, the close transaction), which must queue work after close begins.
// The returned channel closes when done; waiting on it from the owner deadlocks.
func (w *World) exec(f execFunc) <-chan struct{} {
	c := make(chan struct{})
	if w == nil {
		// Nil worlds are treated as no-ops; return a closed channel so callers don't block.
		close(c)
		return c
	}
	ntx := normalTransaction{c: c, f: f}
	if w.conf.Synchronous {
		ntx.Run(w)
		return c
	}
	select {
	case w.queue <- ntx:
	case <-w.queueClosing:
		close(c)
	}
	return c
}

func (w *World) weakExec(valid func() bool, cond *sync.Cond, f execFunc, allowClosed bool) <-chan bool {
	c := make(chan bool, 1)
	if w.conf.Synchronous {
		run := valid == nil || valid()
		if run {
			// As in weakTransaction.Run, f must not run under cond.L: it may
			// relock it, e.g. through RemoveEntity.
			cond.L.Unlock()
			tx := newTx(w)
			f(tx)
			tx.close()
			tx.runDeferred()
			cond.L.Lock()
		}
		c <- run
		return c
	}
	w.scheduleMu.Lock()
	if w.closed.Load() && !w.closeAcceptingEntityTasks.Load() && !allowClosed {
		w.scheduleMu.Unlock()
		c <- false
		return c
	}
	wtx := weakTransaction{c: c, f: f, valid: valid, cond: cond}
	w.enqueueTransaction(wtx)
	w.scheduleMu.Unlock()
	return c
}

// enqueueTransaction queues tx without blocking the owner. Once the bounded
// channel fills, the owner drains the overflow itself instead of creating one
// blocked goroutine per request.
func (w *World) enqueueTransaction(tx transaction) {
	w.queueMu.Lock()
	defer w.queueMu.Unlock()

	if len(w.queueOverflow) == 0 {
		select {
		case w.queue <- tx:
			return
		default:
		}
	}
	w.scheduling.Add(1)
	w.queueOverflow = append(w.queueOverflow, tx)
	select {
	case w.queueWake <- struct{}{}:
	default:
	}
}

func (w *World) nextOverflowTransaction() (transaction, bool) {
	w.queueMu.Lock()
	defer w.queueMu.Unlock()
	if len(w.queueOverflow) == 0 {
		return nil, false
	}
	tx := w.queueOverflow[0]
	w.queueOverflow[0] = nil
	w.queueOverflow = w.queueOverflow[1:]
	return tx, true
}

// handleTransactions continuously reads transactions from the queue and runs
// them.
func (w *World) handleTransactions() {
	for {
		select {
		case tx := <-w.queue:
			tx.Run(w)
			continue
		default:
		}
		if tx, ok := w.nextOverflowTransaction(); ok {
			tx.Run(w)
			w.scheduling.Done()
			continue
		}
		select {
		case tx := <-w.queue:
			tx.Run(w)
		case <-w.queueWake:
		case <-w.queueClosing:
			w.queueing.Done()
			return
		}
	}
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
	rid := c.Block(uint8(pos[0]), int16(pos[1]), uint8(pos[2]), 0)
	if w.conf.Blocks.NBTBlock(rid) {
		if b, ok := c.BlockEntities[pos]; ok {
			return b, true
		}
	}
	return w.conf.Blocks.BlockByRuntimeIDOrAir(rid), true
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
		c.BlockEntities[pos] = nbtB
		c.invalidateTickerBlockEntities()
		c.invalidateBlockEntityPayloads()
		for v := range c.viewers {
			v.ViewBlockUpdate(pos, nbtB, 0)
		}
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
		tx.World().conf.Log.Error("biome not found by ID", "ID", id)
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
	return int(tx.chunk(ChunkPos{int32(x >> 4), int32(z >> 4)}).HighestBlock(uint8(x), uint8(z)))
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
	if opts == nil {
		opts = &SetOpts{}
	}

	x, y, z := uint8(pos[0]), int16(pos[1]), uint8(pos[2])
	c := tx.chunk(chunkPosFromBlockPos(pos))

	rid := w.conf.Blocks.BlockRuntimeID(b)
	redstoneAfterRelevant := isRedstoneRelevant(b)
	needOldBlock := !opts.DisableRedstoneUpdates || !redstoneAfterRelevant
	needOldRID := needOldBlock || (rid != w.conf.Blocks.AirRuntimeID() && !opts.DisableLiquidDisplacement)

	var oldRID uint32
	if needOldRID {
		oldRID = c.Block(x, y, z, 0)
	}
	var oldBlock Block
	if needOldBlock {
		oldBlock = w.conf.Blocks.BlockByRuntimeIDOrAir(oldRID)
		if w.conf.Blocks.NBTBlock(oldRID) {
			if blockEntity, ok := c.BlockEntities[pos]; ok {
				oldBlock = blockEntity
			}
		}
	}

	var before uint32
	if rid != w.conf.Blocks.AirRuntimeID() && !opts.DisableLiquidDisplacement {
		before = oldRID
	}

	c.modified = true
	c.SetBlock(x, y, z, 0, rid)
	c.invalidateRandomTickSubChunks()
	c.invalidateSubChunkHeightMaps()
	c.invalidateNetworkSubChunkPayloads()
	if w.conf.Blocks.NBTBlock(rid) {
		c.BlockEntities[pos] = b
		c.invalidateTickerBlockEntities()
		c.invalidateBlockEntityPayloads()
	} else {
		delete(c.BlockEntities, pos)
		c.invalidateTickerBlockEntities()
		c.invalidateBlockEntityPayloads()
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
			c.forEachViewer(func(viewer Viewer) {
				viewer.ViewBlockUpdate(pos, secondLayer, 1)
			})
		}
	}
	if redstoneAfterRelevant || (needOldBlock && isRedstoneRelevant(oldBlock)) {
		w.redstone.forget(pos)
	}

	c.forEachViewer(func(viewer Viewer) {
		viewer.ViewBlockUpdate(pos, b, 0)
	})

	if !opts.DisableBlockUpdates {
		w.doBlockUpdatesAround(pos)
	}
	if !opts.DisableRedstoneUpdates {
		w.redstone.invalidateAroundBlockChange(pos, oldBlock, b, RedstoneUpdateCauseBlockUpdate, w.Range())
	}
}

// setBlockEntity updates block entity data without triggering block updates.
func (tx *Tx) setBlockEntity(pos cube.Pos, b Block) {
	w := tx.World()
	if pos.OutOfBounds(w.Range()) {
		// Fast way out.
		return
	}
	c := tx.chunk(chunkPosFromBlockPos(pos))

	rid := w.conf.Blocks.BlockRuntimeID(b)
	if !w.conf.Blocks.NBTBlock(rid) || c.Block(uint8(pos[0]), int16(pos[1]), uint8(pos[2]), 0) != rid {
		tx.setBlock(pos, b, nil)
		return
	}
	c.BlockEntities[pos] = b
	c.modified = true
}

// setBiome sets the Biome at the position passed. If a chunk is not yet loaded
// at that position, the chunk is first loaded or generated if it could not be
// found in the world save.
func (tx *Tx) setBiome(pos cube.Pos, b Biome) {
	if pos.OutOfBounds(tx.Range()) {
		// Fast way out.
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
	w := tx.World()
	dim := s.Dimensions()
	width, height, length := dim[0], dim[1], dim[2]
	maxX, maxY, maxZ := pos[0]+width, pos[1]+height, pos[2]+length
	f := func(x, y, z int) Block {
		return tx.block(cube.Pos{pos[0] + x, pos[1] + y, pos[2] + z})
	}

	// We approach this on a per-chunk basis, so that we can keep only one chunk
	// in memory at a time while not needing to acquire a new chunk lock for
	// every block. This also allows us not to send block updates, but instead
	// send a single chunk update once.
	for chunkX := pos[0] >> 4; chunkX <= maxX>>4; chunkX++ {
		for chunkZ := pos[2] >> 4; chunkZ <= maxZ>>4; chunkZ++ {
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
							if b != nil {
								rid := w.conf.Blocks.BlockRuntimeID(b)
								sub.SetBlock(uint8(xOffset), uint8(yOffset), uint8(zOffset), 0, rid)
								c.invalidateRandomTickSubChunks()
								c.invalidateSubChunkHeightMaps()
								c.invalidateNetworkSubChunkPayloads()

								nbtPos := cube.Pos{xOffset, yOffset, zOffset}
								if w.conf.Blocks.NBTBlock(rid) {
									c.BlockEntities[nbtPos] = b
									c.invalidateTickerBlockEntities()
									c.invalidateBlockEntityPayloads()
								} else {
									delete(c.BlockEntities, nbtPos)
									c.invalidateTickerBlockEntities()
									c.invalidateBlockEntityPayloads()
								}
							}
							if liq != nil {
								sub.SetBlock(uint8(xOffset), uint8(yOffset), uint8(zOffset), 1, w.conf.Blocks.BlockRuntimeID(liq))
							} else if len(sub.Layers()) > 1 {
								sub.SetBlock(uint8(xOffset), uint8(yOffset), uint8(zOffset), 1, w.conf.Blocks.AirRuntimeID())
							}
						}
					}
				}
			}
			c.SetBlock(0, 0, 0, 0, c.Block(0, 0, 0, 0)) // Make sure the heightmap is recalculated.
			c.modified = true

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
	c := tx.chunk(chunkPosFromBlockPos(pos))
	x, y, z := uint8(pos[0]), int16(pos[1]), uint8(pos[2])

	id := c.Block(x, y, z, 0)
	b, ok := w.conf.Blocks.BlockByRuntimeID(id)
	if !ok {
		w.conf.Log.Error("Liquid: no block with runtime ID", "ID", id)
		return nil, false
	}
	if liq, ok := b.(Liquid); ok {
		return liq, true
	}
	id = c.Block(x, y, z, 1)

	b, ok = w.conf.Blocks.BlockByRuntimeID(id)
	if !ok {
		w.conf.Log.Error("Liquid: no block with runtime ID", "ID", id)
		return nil, false
	}
	liq, ok := b.(Liquid)
	return liq, ok
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
	chunkPos := chunkPosFromBlockPos(pos)
	c := tx.chunk(chunkPos)
	if b == nil {
		w.removeLiquids(c, pos)
		w.doBlockUpdatesAround(pos)
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
		for v := range c.viewers {
			v.ViewBlockUpdate(pos, b, 0)
		}
	} else {
		c.SetBlock(x, y, z, 1, rid)
		for v := range c.viewers {
			v.ViewBlockUpdate(pos, b, 1)
		}
	}
	c.modified = true

	w.doBlockUpdatesAround(pos)
	w.redstone.invalidateAround(pos, pos, RedstoneUpdateCauseBlockUpdate, w.Range())
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
			for v := range c.viewers {
				v.ViewBlockUpdate(pos, air, 0)
			}
		}
		noneLeft = true
	}
	if _, changed := w.removeLiquidOnLayer(c, x, y, z, 1); changed {
		for v := range c.viewers {
			v.ViewBlockUpdate(pos, air, 1)
		}
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
		w.conf.Log.Error("removeLiquidOnLayer: no block with runtime ID", "ID", id)
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
		w.conf.Log.Error("additionalLiquid: no block with runtime ID", "ID", id)
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
	if pos[1] < w.ra[0] {
		// Fast way out.
		return 0
	}
	if pos[1] > w.ra[1] {
		// Above the rest of the world, so full skylight.
		return 15
	}
	c, ok := w.loadedChunk(chunkPosFromBlockPos(pos))
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
	if pos[1] < w.ra[0] {
		// Fast way out.
		return 0
	}
	if pos[1] > w.ra[1] {
		// Above the rest of the world, so full skylight.
		return 15
	}
	return tx.chunk(chunkPosFromBlockPos(pos)).SkyLight(uint8(pos[0]), int16(pos[1]), uint8(pos[2]))
}

// Time returns the current time of the world. The time is incremented every
// 1/20th of a second, unless World.StopTime() is called.
func (w *World) Time() int {
	if w == nil {
		return 0
	}
	w.set.Lock()
	defer w.set.Unlock()
	return int(w.set.Time)
}

// SetTime sets the new time of the world. SetTime will always work, regardless
// of whether the time is stopped or not.
func (w *World) SetTime(new int) {
	if w == nil {
		return
	}
	w.set.Lock()
	w.set.Time = int64(new)
	w.set.Unlock()

	viewers, _ := w.allViewers()
	for _, viewer := range viewers {
		viewer.ViewTime(new)
	}
	w.releaseViewers(viewers)
}

// StopTime stops the time in the world. When called, the time will no longer
// cycle and the world will remain at the time when StopTime is called. The
// time may be restarted by calling World.StartTime().
func (w *World) StopTime() {
	w.enableTimeCycle(false)
}

// StartTime restarts the time in the world. When called, the time will start
// cycling again and the day/night cycle will continue. The time may be stopped
// again by calling World.StopTime().
func (w *World) StartTime() {
	w.enableTimeCycle(true)
}

// TimeCycle returns whether time cycle is enabled.
func (w *World) TimeCycle() bool {
	if w == nil {
		return false
	}
	w.set.Lock()
	defer w.set.Unlock()
	return w.set.TimeCycle
}

// enableTimeCycle enables or disables the time cycling of the World.
func (w *World) enableTimeCycle(v bool) {
	if w == nil {
		return
	}
	w.set.Lock()
	defer w.set.Unlock()
	w.set.TimeCycle = v
	viewers, _ := w.allViewers()
	defer w.releaseViewers(viewers)
	for _, viewer := range viewers {
		viewer.ViewTimeCycle(v)
	}
}

// PlayersSleepingPercentage returns the configured percentage of players required to sleep before the night is skipped.
func (w *World) PlayersSleepingPercentage() int32 {
	if w == nil {
		return 100
	}
	w.set.Lock()
	defer w.set.Unlock()
	if w.set.PlayersSleepingPercentage <= 0 {
		return 100
	}
	return w.set.PlayersSleepingPercentage
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

// addParticle spawns a Particle at a given position in the World. Viewers that
// are viewing the chunk will be shown the particle.
func (w *World) addParticle(pos mgl64.Vec3, p Particle) {
	p.Spawn(w, pos)
	viewers := w.viewersOf(pos)
	for _, viewer := range viewers {
		viewer.ViewParticle(pos, p)
	}
	w.releaseViewers(viewers)
}

// playSound plays a sound at a specific position in the World. Viewers of that
// position will be able to hear the sound if they are close enough.
func (w *World) playSound(tx *Tx, pos mgl64.Vec3, s Sound) {
	ctx := tx.Event()
	if w.Handler().HandleSound(ctx, s, pos); ctx.Cancelled() {
		return
	}
	s.Play(w, pos)
	viewers := w.viewersOf(pos)
	for _, viewer := range viewers {
		viewer.ViewSound(pos, s)
	}
	w.releaseViewers(viewers)
}

// addEntity adds an EntityHandle to a World. The Entity will be visible to all
// viewers of the World that have the chunk at the EntityHandle's position. If
// the chunk that the EntityHandle is in is not yet loaded, it will first be
// loaded. addEntity panics if the EntityHandle is already in a world.
// addEntity returns the Entity created by the EntityHandle.
func (w *World) addEntity(tx *Tx, handle *EntityHandle) Entity {
	handle.setAndUnlockWorld(w)
	pos := chunkPosFromVec3(handle.data.Pos)
	w.set.Lock()
	currentTick := w.currentTickLocked()
	w.set.Unlock()
	state := &entityState{
		pos:               pos,
		lastTick:          currentTick,
		lastProcessedTick: currentTick,
		isItem:            handle.t.EncodeEntity() == "minecraft:item",
	}
	if _, ok := w.entities[handle]; !ok {
		w.entityCount.Add(1)
	}
	w.entities[handle] = state
	handle.state = state

	c := tx.chunk(pos)
	c.addEntity(handle)
	c.modified = true
	w.addEntityColumn(pos, c)

	e := state.entity(tx, handle)
	for v := range c.viewers {
		// Show the entity to all viewers in the chunk of the entity.
		showEntity(e, v)
	}
	w.Handler().HandleEntitySpawn(tx, e)
	handle.markWorldReady(w)
	return e
}

// removeEntity removes an Entity from the World that is currently present in
// it. Any viewers of the Entity will no longer be able to see it.
// removeEntity returns the EntityHandle of the Entity. After removing an Entity
// from the World, the Entity is no longer usable.
func (w *World) removeEntity(e Entity, tx *Tx) *EntityHandle {
	handle := e.H()
	state, found := w.entities[handle]
	if !found {
		// The entity currently isn't in this world.
		return nil
	}
	pos := state.pos
	w.Handler().HandleEntityDespawn(tx, e)

	c := tx.chunk(pos)
	if c.removeEntity(handle) {
		c.modified = true
	}
	if len(c.Entities) == 0 {
		w.removeEntityColumn(pos)
	}

	w.removeEntityFromViewLayers(e)
	for v := range c.viewers {
		v.HideEntity(e)
	}
	delete(w.entities, handle)
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
				c, ok := w.chunks[ChunkPos{x, z}]
				if !ok {
					// The chunk wasn't loaded, so there are no entities here.
					continue
				}
				for _, handle := range slices.Clone(c.Entities) {
					if !box.Vec3Within(handle.data.Pos) {
						continue
					}
					state := w.entities[handle]
					if state == nil {
						continue
					}
					if !yield(state.entity(tx, handle)) {
						return
					}
				}
			}
		}
	}
}

// allEntities returns an iterator that yields all entities in the World.
func (w *World) allEntities(tx *Tx) iter.Seq[Entity] {
	return func(yield func(Entity) bool) {
		for handle, state := range maps.Clone(w.entities) {
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
		for handle, state := range maps.Clone(w.entities) {
			if handle.t.EncodeEntity() == "minecraft:player" {
				if ent := state.entity(tx, handle); ent != nil {
					if !yield(ent) {
						return
					}
				}
			}
		}
	}
}

// Spawn returns the spawn of the world. Every new player will by default spawn
// on this position in the world when joining.
func (w *World) Spawn() cube.Pos {
	if w == nil {
		return cube.Pos{}
	}

	if w.Dimension() == End {
		return cube.Pos{100, 50}
	} else if w.Dimension() == Nether {
		return cube.Pos{}
	}

	w.set.Lock()
	defer w.set.Unlock()
	return w.set.Spawn
}

// SetSpawn sets the spawn of the world to a different position. The player
// will be spawned in the centre of this position when newly joining.
func (w *World) SetSpawn(pos cube.Pos) {
	if w == nil {
		return
	}

	// nether has no spawn point and end spawn point is always 100 50 0.
	if w.Dimension() == Nether || w.Dimension() == End {
		return
	}

	w.set.Lock()
	w.set.Spawn = pos
	w.set.Unlock()

	viewers, _ := w.allViewers()
	for _, viewer := range viewers {
		viewer.ViewWorldSpawn(pos)
	}
	w.releaseViewers(viewers)
}

// PlayerSpawn returns the spawn position of a player with a UUID in this World.
func (w *World) PlayerSpawn(id uuid.UUID) cube.Pos {
	if w == nil {
		return cube.Pos{}
	}
	pos, exist, err := w.conf.Provider.LoadPlayerSpawnPosition(id)
	if err != nil {
		w.conf.Log.Error("load player spawn: "+err.Error(), "ID", id)
		return w.Spawn()
	}
	if !exist {
		return w.Spawn()
	}
	return pos
}

// SetPlayerSpawn sets the spawn position of a player with a UUID in this
// World. If the player has a spawn in the world, the player will be teleported
// to this location on respawn.
func (w *World) SetPlayerSpawn(id uuid.UUID, pos cube.Pos) {
	if w == nil {
		return
	}
	if err := w.conf.Provider.SavePlayerSpawnPosition(id, pos); err != nil {
		w.conf.Log.Error("save player spawn: "+err.Error(), "ID", id)
	}
}

// SetRequiredSleepDuration sets the duration of time players in the world must sleep for, in order to advance to the
// next day.
func (w *World) SetRequiredSleepDuration(duration time.Duration) {
	if w == nil {
		return
	}
	w.set.Lock()
	defer w.set.Unlock()
	w.set.RequiredSleepTicks = duration.Milliseconds() / 50
}

// DefaultGameMode returns the default game mode of the world. When players
// join, they are given this game mode. The default game mode may be changed
// using SetDefaultGameMode().
func (w *World) DefaultGameMode() GameMode {
	if w == nil {
		return GameModeSurvival
	}
	w.set.Lock()
	defer w.set.Unlock()
	return w.set.DefaultGameMode
}

// SetTickRange sets the range in chunks around each Viewer that will have the
// chunks (their blocks and entities) ticked when the World is ticked.
func (w *World) SetTickRange(v int) {
	if w == nil {
		return
	}
	w.set.Lock()
	defer w.set.Unlock()
	w.set.TickRange = int32(v)
}

// tickRange returns the tick range around each Viewer.
func (w *World) tickRange() int {
	w.set.Lock()
	defer w.set.Unlock()
	return int(w.set.TickRange)
}

// SetDefaultGameMode changes the default game mode of the world. When players
// join, they are then given that game mode.
func (w *World) SetDefaultGameMode(mode GameMode) {
	if w == nil {
		return
	}
	w.set.Lock()
	defer w.set.Unlock()
	w.set.DefaultGameMode = mode
}

// Difficulty returns the difficulty of the world. Properties of mobs in the
// world and the player's hunger will depend on this difficulty.
func (w *World) Difficulty() Difficulty {
	if w == nil {
		return DifficultyNormal
	}
	w.set.Lock()
	defer w.set.Unlock()
	return w.set.Difficulty
}

// SetDifficulty changes the difficulty of a world.
func (w *World) SetDifficulty(d Difficulty) {
	if w == nil {
		return
	}
	w.set.Lock()
	defer w.set.Unlock()
	w.set.Difficulty = d
}

// scheduleBlockUpdate schedules a block update at the position passed for the
// block type passed after a specific delay. If the block at that position does
// not handle block updates, nothing will happen.
// Block updates are both block and position specific. A block update is only
// scheduled if no block update with the same position and block type is
// already scheduled at a later time than the newly scheduled update.
func (w *World) scheduleBlockUpdate(pos cube.Pos, b Block, delay time.Duration) {
	if pos.OutOfBounds(w.Range()) {
		return
	}
	w.scheduledUpdates.schedule(w.conf.Blocks, pos, b, delay)
}

// doBlockUpdatesAround schedules block updates directly around and on the
// position passed.
func (w *World) doBlockUpdatesAround(pos cube.Pos) {
	if w == nil || pos.OutOfBounds(w.Range()) {
		return
	}
	changed := pos

	w.updateNeighbour(pos, changed)
	pos.Neighbours(func(pos cube.Pos) {
		w.updateNeighbour(pos, changed)
	}, w.Range())
}

// neighbourUpdate represents a position that needs to be updated because of a
// neighbour that changed.
type neighbourUpdate struct {
	pos, neighbour cube.Pos
}

// updateNeighbour ticks the position passed as a result of the neighbour
// passed being updated.
func (w *World) updateNeighbour(pos, changedNeighbour cube.Pos) {
	w.neighbourUpdates = append(w.neighbourUpdates, neighbourUpdate{pos: pos, neighbour: changedNeighbour})
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

// viewersOf returns all viewers viewing the position passed.
//
// The method deliberately borrows a slice from viewerSlicePool so the caller can iterate without allocating. The
// caller must eventually hand the slice back through releaseViewers to maintain the pool's effectiveness. We have to
// pay special attention to reusing buffers here because these lookups happen every time entities broadcast state or
// packets are fanned out to observers.
func (w *World) viewersOf(pos mgl64.Vec3) []Viewer {
	c, ok := w.chunks[chunkPosFromVec3(pos)]
	if !ok || len(c.viewers) == 0 {
		return nil
	}
	viewers := viewerSlicePool.Get().([]Viewer)
	if cap(viewers) < len(c.viewers)+1 {
		viewerSlicePool.Put(viewers[:0])
		viewers = make([]Viewer, 0, len(c.viewers)+1)
	} else {
		viewers = viewers[:0]
	}
	for v := range c.viewers {
		viewers = append(viewers, v)
	}
	return viewers
}

// releaseViewers returns pooled viewer slices to viewerSlicePool. Forgetting to release will degrade the pool and
// reintroduce the very allocations this optimisation was meant to avoid.
func (w *World) releaseViewers(viewers []Viewer) {
	if viewers == nil {
		return
	}
	viewerSlicePool.Put(viewers[:0])
}

// PortalDestination returns the destination World for a portal of a specific
// Dimension. Calling PortalDestination(Nether) on an Overworld World returns
// Nether, while calling PortalDestination(Nether) on a Nether World will
// return the Overworld, for instance. If no destination World is available,
// nil is returned.
func (w *World) PortalDestination(dim Dimension) *World {
	if w == nil {
		return nil
	}
	if w.conf.PortalDestination == nil {
		return nil
	}
	dest := w.conf.PortalDestination(dim)
	if dest == w {
		if fallback := w.DefaultWorld(); fallback != w {
			return fallback
		}
		return nil
	}
	return dest
}

// PortalDisabledMessage resolves the message to display when a portal targeting the
// provided Dimension is disabled. An empty string suppresses any feedback.
func (w *World) PortalDisabledMessage(dim Dimension) string {
	if w == nil {
		return ""
	}
	if w.conf.PortalDisabledMessage == nil {
		return ""
	}
	return w.conf.PortalDisabledMessage(dim)
}

// DefaultWorld returns the primary world configured for this server. If no explicit default
// callback is provided, the world itself is returned so respawn logic always has a destination.
func (w *World) DefaultWorld() *World {
	if w == nil {
		return nil
	}
	if w.conf.DefaultWorld == nil {
		return w
	}
	if def := w.conf.DefaultWorld(); def != nil {
		return def
	}
	return w
}

// Save saves the World to the provider.
func (w *World) Save() {
	if w == nil {
		return
	}
	<-w.exec(w.save(w.saveChunk))
}

// save saves all loaded chunks to the World's provider.
func (w *World) save(f func(*Tx, ChunkPos, *Column)) execFunc {
	return func(tx *Tx) {
		if w.conf.ReadOnly {
			return
		}
		w.conf.Log.Debug("Saving chunks in memory to disk...")
		for pos, c := range w.chunks {
			f(tx, pos, c)
		}
		w.conf.Log.Debug("Updating level.dat values...")
		w.conf.Provider.SaveSettings(w.set)
	}
}

// saveChunk saves a chunk and its entities to disk after compacting the chunk.
func (w *World) saveChunk(_ *Tx, pos ChunkPos, c *Column) {
	// Column generation runs in background workers. Saving is performed on the world transaction goroutine and must
	// not access columns that are still being generated.
	if !c.Ready() {
		return
	}
	if !w.conf.ReadOnly && c.modified {
		c.Compact()
		if err := w.conf.Provider.StoreColumn(pos, w.conf.Dim, w.columnTo(c, pos)); err != nil {
			w.conf.Log.Error("save chunk: "+err.Error(), "X", pos[0], "Z", pos[1])
		}
	}
}

// closeChunk saves a chunk and its entities to disk after compacting the chunk.
// Afterwards, scheduled updates from that chunk are removed and all entities
// in it are closed.
func (w *World) closeChunk(tx *Tx, pos ChunkPos, c *Column) {
	for _, e := range slices.Clone(c.Entities) {
		if _, ok := e.Entity(tx); ok {
			continue
		}
		if c.removeEntity(e) {
			if _, ok := w.entities[e]; ok {
				delete(w.entities, e)
				w.entityCount.Add(-1)
			}
		}
	}
	w.saveChunk(tx, pos, c)
	w.scheduledUpdates.removeChunk(pos)
	w.redstone.removeChunk(pos)
	w.removeActiveColumn(pos)
	w.removeEntityColumn(pos)
	// Note: We close c.Entities here because some entities may remove
	// themselves from the world in their Close method, which can lead to
	// unexpected conditions.
	ready := c.Ready()
	if !ready {
		// Prevent shutdown from blocking on unfinished generation.
		c.markReady()
	}
	for _, e := range slices.Clone(c.Entities) {
		ent, ok := e.Entity(tx)
		if !ok {
			continue
		}
		if ready {
			if closer, ok := ent.(interface{ CloseIn(*Tx) error }); ok {
				// Avoid ExecWorld deadlocks by closing entities via the active Tx.
				_ = closer.CloseIn(tx)
			} else {
				_ = ent.Close()
			}
			continue
		}

		w.Handler().HandleEntityDespawn(tx, ent)
		for v := range c.viewers {
			v.HideEntity(ent)
		}
		if _, ok := w.entities[e]; ok {
			delete(w.entities, e)
			w.entityCount.Add(-1)
		}
		e.state = nil
		e.unsetAndLockWorld()
		_ = e.Close()
	}
	c.resetEntities()
	if _, ok := w.chunks[pos]; ok {
		delete(w.chunks, pos)
		w.chunkCount.Add(-1)
	}
}

// Close closes the world and saves all chunks currently loaded.
func (w *World) Close() error {
	if w == nil {
		return nil
	}
	w.o.Do(w.close)
	return nil
}

// close stops the World from ticking, saves all chunks to the Provider and
// updates the world's settings.
func (w *World) close() {
	w.scheduleMu.Lock()
	w.closed.Store(true)
	close(w.closeStarted)
	w.scheduleMu.Unlock()

	w.scheduling.Wait()
	w.generatorEnqueue.Wait()
	w.generatorRunning.Wait()
	w.scheduleMu.Lock()
	w.closeAcceptingEntityTasks.Store(true)
	w.scheduleMu.Unlock()
	<-w.exec(func(tx *Tx) {
		// Let user code run anything that needs to be finished before closing.
		w.Handler().HandleClose(tx)
		tx.runDeferred()
		w.Handle(NopHandler{})
		clear(w.chunkRequests)

		w.save(w.closeChunk)(tx)
	})
	w.scheduleMu.Lock()
	w.closeAcceptingEntityTasks.Store(false)
	w.scheduleMu.Unlock()
	w.scheduling.Wait()

	close(w.closing)
	w.running.Wait()

	close(w.queueClosing)
	w.queueing.Wait()

	w.set.Lock()
	w.set.unregisterWorldLocked(w)
	w.set.Unlock()
	if !releaseProvider(w.providerUse) {
		return
	}

	w.conf.Log.Debug("Closing provider...")
	if err := w.conf.Provider.Close(); err != nil {
		w.conf.Log.Error("close world provider: " + err.Error())
	}
}

// allViewers returns all viewers and loaders, regardless of where in the world
// they are viewing.
func (w *World) allViewers() ([]Viewer, []*Loader) {
	w.viewerMu.Lock()
	defer w.viewerMu.Unlock()

	viewers := viewerSlicePool.Get().([]Viewer)
	if cap(viewers) < len(w.viewers) {
		viewerSlicePool.Put(viewers[:0])
		viewers = make([]Viewer, 0, len(w.viewers))
	} else {
		viewers = viewers[:0]
	}
	loaders := make([]*Loader, 0, len(w.viewers))
	for k, v := range w.viewers {
		viewers = append(viewers, v)
		loaders = append(loaders, k)
	}
	return viewers, loaders
}

// addWorldViewer adds a viewer to the world. Should only be used while the
// viewer isn't viewing any chunks.
func (w *World) addWorldViewer(l *Loader) {
	w.viewerMu.Lock()
	w.viewers[l] = l.viewer
	w.viewerMu.Unlock()

	l.viewer.ViewTime(w.Time())
	l.viewer.ViewTimeCycle(w.TimeCycle())
	w.set.Lock()
	raining, thundering := w.set.Raining, w.set.Raining && w.set.Thundering
	w.set.Unlock()
	l.viewer.ViewWeather(raining, thundering)
	l.viewer.ViewWorldSpawn(w.Spawn())
}

// addViewer adds a viewer to the World at a given position. Any events that
// happen in the chunk at that position, such as block and entity changes, will
// be sent to the viewer.
func (w *World) addViewer(pos ChunkPos, c *Column, loader *Loader, viewer Viewer) {
	if viewer != nil {
		c.viewers[viewer] = struct{}{}
	}
	c.loaders = append(c.loaders, loader)

	w.addActiveColumn(pos, c)
}

func (w *World) viewChunkEntities(tx *Tx, c *Column, viewer Viewer) {
	for _, entity := range c.Entities {
		if ent, ok := entity.Entity(tx); ok {
			showEntity(ent, viewer)
		}
	}
}

// removeViewer removes a viewer from a chunk position. All entities will be
// hidden from the viewer and no more calls will be made when events in the
// chunk happen.
func (w *World) removeViewer(tx *Tx, pos ChunkPos, loader *Loader) {
	w.removeViewerFrom(tx, pos, nil, loader, loader.viewer, true)
}

// rollbackViewer removes a partially published chunk without closing it, so it may be retried.
func (w *World) rollbackViewer(tx *Tx, pos ChunkPos, c *Column, loader *Loader, viewer Viewer) {
	w.removeViewerFrom(tx, pos, c, loader, viewer, false)
}

func (w *World) removeViewerFrom(tx *Tx, pos ChunkPos, expected *Column, loader *Loader, viewer Viewer, closeUnused bool) {
	if w == nil {
		return
	}
	c, ok := w.chunks[pos]
	if !ok || expected != nil && c != expected {
		return
	}
	if i := slices.Index(c.loaders, loader); i != -1 {
		c.loaders = slices.Delete(c.loaders, i, i+1)
	}

	if len(c.loaders) == 0 {
		w.removeActiveColumn(pos)
	}

	// Hide all entities in the chunk from the viewer.
	delete(c.viewers, viewer)
	if viewer != nil {
		for _, entity := range c.Entities {
			if ent, ok := entity.Entity(tx); ok {
				viewer.HideEntity(ent)
			}
		}
	}

	if closeUnused && len(c.viewers) == 0 && len(c.loaders) == 0 {
		w.closeChunk(tx, pos, c)
	}
}

// Handler returns the Handler of the world.
func (w *World) Handler() Handler {
	if w == nil {
		return NopHandler{}
	}
	return *w.handler.Load()
}

// showEntity shows an Entity to a viewer of the world. It makes sure
// everything of the Entity, including the items held, is shown.
func showEntity(e Entity, viewer Viewer) {
	viewer.ViewEntity(e)
	viewer.ViewEntityItems(e)
	viewer.ViewEntityArmour(e)
}

// loadedChunk returns chunk & true only if chunk at position passed is loaded.
func (w *World) loadedChunk(pos ChunkPos) (*Column, bool) {
	c, ok := w.chunks[pos]
	return c, ok && c.Ready() && c.lightReady.Load()
}

// chunk reads a chunk from the position passed. If a chunk at that position is
// not yet loaded, the chunk is loaded from the provider, or generated if it
// did not yet exist. Additionally, chunks newly loaded have the light in them
// calculated before they are returned.
func (tx *Tx) chunk(pos ChunkPos) *Column {
	w := tx.World()
	c, ok := w.chunks[pos]
	if ok {
		c.waitReady()
		c.ensureLight(w, pos)
		w.finishChunkRequest(tx, pos, c)
		return c
	}
	c, err := w.loadChunk(pos)
	if !c.Ready() {
		c.waitReady()
	}
	c.ensureLight(w, pos)
	w.finishChunkRequest(tx, pos, c)
	if err != nil {
		w.conf.Log.Error("load chunk: "+err.Error(), "X", pos[0], "Z", pos[1])
	}
	return c
}

func (w *World) chunkLoaded(pos ChunkPos) bool {
	if c, ok := w.chunks[pos]; ok {
		return c.Ready()
	}
	return false
}

// loadChunk loads or generates a chunk (column) for the given position.
//
// Behavior summary:
//  1. If the chunk exists in persistent storage, load it and mark as ready.
//  2. If not found, create a new column and generate it asynchronously.
//  3. If an unexpected error occurs, return an empty ready column to prevent blocking.
//
// This function guarantees that the returned *Column will eventually become ready,
// even if generation is canceled due to shutdown.
func (w *World) loadChunk(pos ChunkPos) (*Column, error) {
	// Attempt to load the column from the persistent provider (e.g. LevelDB).
	column, err := w.conf.Provider.LoadColumn(pos, w.conf.Dim)

	switch {
	case err == nil:
		// Case 1: Column successfully loaded from persistent storage.
		col := w.columnFrom(column, pos)
		col.fillLight(pos)
		if _, ok := w.chunks[pos]; !ok {
			w.chunkCount.Add(1)
		}
		w.chunks[pos] = col

		// Register all entities contained in this column into the world.
		w.set.Lock()
		currentTick := w.currentTickLocked()
		w.set.Unlock()
		for _, e := range col.Entities {
			e.setAndUnlockWorld(w)
			if _, ok := w.entities[e]; !ok {
				w.entityCount.Add(1)
			}
			state := &entityState{
				pos:               pos,
				lastTick:          currentTick,
				lastProcessedTick: currentTick,
				isItem:            e.t.EncodeEntity() == "minecraft:item",
			}
			w.entities[e] = state
			e.state = state
			e.markWorldReady(w)
		}

		if len(col.Entities) > 0 {
			w.addEntityColumn(pos, col)
		}

		return col, nil

	case errors.Is(err, leveldb.ErrNotFound):
		// Case 2: Column not found in storage — needs generation.
		// Create a new empty column filled with air.
		col := newColumn(chunk.New(w.conf.Blocks, w.Range()))
		if _, ok := w.chunks[pos]; !ok {
			w.chunkCount.Add(1)
		}
		w.chunks[pos] = col

		// Schedule asynchronous generation.
		// generateChunkAsync is shutdown-safe and will mark ready if closing.
		w.generateChunkAsync(pos, col)

		return col, nil

	default:
		// Case 3: Unexpected error occurred (I/O failure, corruption, etc.)
		// To avoid deadlocks, return a ready empty column and the error.
		col := newColumn(chunk.New(w.conf.Blocks, w.Range()))
		col.fillLight(pos)
		col.markReady()
		// Keep the placeholder column tracked so callers don't mutate an untracked chunk on errors.
		if _, ok := w.chunks[pos]; !ok {
			w.chunkCount.Add(1)
		}
		w.chunks[pos] = col
		return col, err
	}
}

// loadChunkAsync loads or generates the chunk at pos in the background,
// calling callback once ready. It returns false if it could not be scheduled.
func (w *World) loadChunkAsync(tx *Tx, pos ChunkPos, callback chunkCallback) bool {
	if w.closed.Load() {
		return false
	}
	if c, ok := w.chunks[pos]; ok {
		w.chunkRequests[pos] = append(w.chunkRequests[pos], callback)
		if c.Ready() {
			c.ensureLight(w, pos)
			w.finishChunkRequest(tx, pos, c)
		}
		return true
	}

	w.chunkRequests[pos] = append(w.chunkRequests[pos], callback)
	c, err := w.loadChunk(pos)
	if err != nil {
		w.conf.Log.Error("load chunk: "+err.Error(), "X", pos[0], "Z", pos[1])
	}
	if c.Ready() {
		c.ensureLight(w, pos)
		w.finishChunkRequest(tx, pos, c)
	}
	return true
}

// finishChunkRequest publishes a ready chunk to all loaders waiting for it.
// It must run on the world owner.
func (w *World) finishChunkRequest(tx *Tx, pos ChunkPos, c *Column) {
	callbacks := w.chunkRequests[pos]
	if len(callbacks) == 0 || !c.Ready() {
		return
	}
	delete(w.chunkRequests, pos)
	if w.closed.Load() {
		return
	}
	for _, callback := range callbacks {
		callback := callback
		tx.Defer(func(tx *Tx) {
			if w.closed.Load() {
				return
			}
			callback(tx, c)
		})
	}
}

// generateChunkAsync schedules an asynchronous chunk generation task for the given position.
// It ensures that no new tasks are enqueued once the world begins shutting down.
// If shutdown is in progress, the column is immediately marked as ready to avoid deadlocks.
//
// This prevents chunks from being stuck in a "not ready" state during shutdown,
// which could otherwise cause Close() or c.waitReady() to block forever.
func (w *World) generateChunkAsync(pos ChunkPos, col *Column) {
	task := generationTask{pos: pos, col: col}
	w.scheduleMu.Lock()
	if w.closed.Load() {
		w.scheduleMu.Unlock()
		col.markReady()
		return
	}
	if w.conf.Synchronous {
		w.scheduleMu.Unlock()
		w.runGenerationTask(task)
		return
	}

	select {
	case w.generatorQueue <- task:
		w.scheduleMu.Unlock()
	default:
		w.generatorEnqueue.Add(1)
		w.scheduleMu.Unlock()
		w.handleGeneratorBackpressure()
		select {
		case <-w.closeStarted:
			col.markReady()
		case w.generatorQueue <- task:
		}
		w.generatorEnqueue.Done()
	}
}

// generatorWorker continuously processes generation tasks from the generator queue.
// Each worker runs in its own goroutine and terminates gracefully when w.closing is closed.
//
// Behavior:
//   - Processes tasks received from w.generatorQueue by invoking runGenerationTask.
//   - On shutdown, drains any remaining tasks in the queue to ensure that all
//     columns are marked ready and no goroutine remains blocked waiting for generation.
func (w *World) generatorWorker() {
	defer w.running.Done()
	defer w.generatorRunning.Done()

	for {
		select {
		case task := <-w.generatorQueue:
			// A new generation task is available — process it immediately.
			w.runGenerationTask(task)

		case <-w.closeStarted:
			// Shutdown signal received — mark all remaining queued columns as ready.
			w.generatorEnqueue.Wait()
			w.drainGenerationQueue()
			return
		}
	}
}

// runGenerationTask executes the chunk generation logic for a given task.
// It ensures that the associated column is always marked as ready, even if
// the generation panics or fails unexpectedly.
//
// This design guarantees that no waiting goroutine (e.g., loadChunk callers)
// will hang indefinitely due to an unmarked column.
func (w *World) runGenerationTask(task generationTask) {
	// Chunk generation is performed on background workers. World transactions must treat not-yet-ready columns as
	// immutable: once markReady() is called, the generation worker will no longer mutate the column. The ready flag
	// acts as a synchronization point to safely publish generated chunk contents to the transaction goroutine.
	defer func() {
		// Always recover from panics during generation to prevent worker termination.
		if r := recover(); r != nil {
			w.conf.Log.Error(
				"generate chunk: panic",
				"error", fmt.Sprint(r),
				"X", task.pos[0],
				"Z", task.pos[1],
			)
		}
		task.col.fillLight(task.pos)
		task.col.markReady()
		if !w.closed.Load() {
			w.Do(func(tx *Tx) {
				task.col.ensureLight(w, task.pos)
				w.finishChunkRequest(tx, task.pos, task.col)
			})
		}
	}()

	// Perform the actual chunk generation.
	// The generator implementation is responsible for populating the chunk’s data.
	w.conf.Generator.GenerateChunk(task.pos, task.col.Chunk)

	task.col.BlockEntities = w.generatedBlockEntities(task.pos, task.col.Chunk)
	if task.col.BlockEntities == nil {
		task.col.BlockEntities = map[cube.Pos]Block{}
	}
	task.col.invalidateNetworkBiomePayload()
	task.col.invalidateNetworkSubChunkPayloads()
	task.col.invalidateBlockEntityPayloads()
	task.col.modified = true
}

// generatedBlockEntities produces default block entity data for NBT blocks in a newly generated chunk.
// The returned map is safe to assign on the world's transaction goroutine.
func (w *World) generatedBlockEntities(pos ChunkPos, c *chunk.Chunk) map[cube.Pos]Block {
	if c == nil {
		return nil
	}

	// Most generated chunks do not contain NBT blocks, so allocate lazily to avoid churn.
	var blockEntities map[cube.Pos]Block
	baseX, baseZ := int(pos[0]<<4), int(pos[1]<<4)

	for subIndex, sub := range c.Sub() {
		if sub.Empty() || len(sub.Layers()) == 0 {
			continue
		}
		storage := sub.Layers()[0]

		// Fast-path: Skip sub chunks whose palette doesn't contain any NBT blocks.
		paletteHasNBT := false
		pal := storage.Palette()
		for i := 0; i < pal.Len(); i++ {
			rid := pal.Value(uint16(i))
			if w.conf.Blocks.NBTBlock(rid) {
				paletteHasNBT = true
				break
			}
		}
		if !paletteHasNBT {
			continue
		}

		subY := int(c.SubY(int16(subIndex)))
		for x := byte(0); x < 16; x++ {
			for z := byte(0); z < 16; z++ {
				for y := byte(0); y < 16; y++ {
					rid := storage.At(x, y, z)
					if !w.conf.Blocks.NBTBlock(rid) {
						continue
					}
					worldPos := cube.Pos{baseX + int(x), subY + int(y), baseZ + int(z)}
					if _, ok := blockEntities[worldPos]; ok {
						continue
					}
					nbt := map[string]any{}
					if w.conf.Dim == End {
						if name, _, ok := w.conf.Blocks.RuntimeIDToState(rid); ok && name == "minecraft:end_gateway" {
							nbt = map[string]any{
								"ExitPortal": map[string]any{
									"X": int32(100),
									"Y": int32(50),
									"Z": int32(0),
								},
								"ExactTeleport": uint8(1),
							}
						} else if name == "minecraft:wall_banner" {
							// End Cities use purple wall banners (base colour only).
							nbt = map[string]any{
								"Base": int32(5),
							}
						}
					}
					nbtB := DecodeNBT(w.conf.Blocks.BlockByRuntimeIDOrAir(rid).(NBTer), nbt, w.conf.Blocks).(Block)
					if blockEntities == nil {
						blockEntities = make(map[cube.Pos]Block, 8)
					}
					blockEntities[worldPos] = nbtB
				}
			}
		}
	}
	return blockEntities
}

// drainGenerationQueue flushes any remaining tasks in the generator queue.
// It is called during shutdown to ensure that every column waiting for
// generation is marked as ready, preventing potential deadlocks.
//
// This function runs until the queue is empty.
func (w *World) drainGenerationQueue() {
	for {
		select {
		case task := <-w.generatorQueue:
			// Mark the column as ready without performing generation,
			// since the world is shutting down and workers will not continue.
			task.col.markReady()

		default:
			// Queue is empty — exit the draining loop.
			return
		}
	}
}

// handleGeneratorBackpressure increments backpressure counters and emits a throttled
// warning when the generator queue saturates. This gives operators concrete guidance on
// adjusting parallelism or profiling I/O bottlenecks under heavy terrain generation load.
func (w *World) handleGeneratorBackpressure() {
	count := w.generatorQueueSaturation.Add(1)
	now := uint64(time.Now().UnixNano())
	last := w.lastQueueSaturationLog.Load()

	if last != 0 && time.Duration(now-last) < time.Minute {
		return
	}
	if !w.lastQueueSaturationLog.CompareAndSwap(last, now) {
		return
	}

	w.conf.Log.Warn(
		"world generator queue saturated: chunk generation backlog detected.",
		"queued_tasks", count,
		"queue_size", cap(w.generatorQueue),
		"workers", w.conf.GeneratorWorkers,
	)
}

// calculateLight calculates the light in the chunk passed and spreads the
// light of any surrounding neighbours if they have all chunks loaded around it
// as a result of the one passed.
func (w *World) calculateLight(centre ChunkPos) {
	for x := int32(-1); x <= 1; x++ {
		for z := int32(-1); z <= 1; z++ {
			// For all the neighbours of this chunk, if they exist, check if all
			// neighbours of that chunk now exist because of this one.
			pos := ChunkPos{centre[0] + x, centre[1] + z}
			if _, ok := w.chunks[pos]; ok {
				// Attempt to spread the light of all neighbours into the
				// surrounding ones.
				w.spreadLight(pos)
			}
		}
	}
}

// spreadLight spreads the light from the chunk passed at the position passed
// to all neighbours if each of them is loaded.
func (w *World) spreadLight(pos ChunkPos) {
	c := make([]*chunk.Chunk, 0, 9)
	for z := int32(-1); z <= 1; z++ {
		for x := int32(-1); x <= 1; x++ {
			neighbourPos := ChunkPos{pos[0] + x, pos[1] + z}
			neighbour, ok := w.chunks[neighbourPos]
			if !ok {
				// Not all surrounding chunks existed: Stop spreading light.
				return
			}
			if !neighbour.Ready() || !neighbour.lightReady.Load() {
				// The neighbour chunk hasn't finished generating yet or its light hasn't been initialised
				// yet. We'll spread the light once all chunks involved are ready.
				return
			}
			c = append(c, neighbour.Chunk)
		}
	}
	// All chunks surrounding the current one are present, so we can spread.
	chunk.LightArea(c, int(pos[0])-1, int(pos[1])-1).Spread()
}

// autoSave runs until the world is running, saving and removing chunks that
// are no longer in use.
func (w *World) autoSave() {
	save := &time.Ticker{C: make(<-chan time.Time)}
	if w.conf.SaveInterval > 0 {
		save = time.NewTicker(w.conf.SaveInterval)
		defer save.Stop()
	}
	closeUnused := time.NewTicker(w.conf.ChunkUnloadInterval)
	defer closeUnused.Stop()

	for {
		select {
		case <-closeUnused.C:
			<-w.exec(w.closeUnusedChunks)
		case <-save.C:
			w.Save()
		case <-w.closing:
			w.running.Done()
			return
		}
	}
}

// CollectGarbage closes chunks that have no viewers or loaders and returns the number of
// chunks, entities and block entities that were removed as a result.
func (w *World) CollectGarbage(tx *Tx) (chunksCollected, entitiesCollected, blockEntitiesCollected int) {
	if w == nil {
		return 0, 0, 0
	}
	for pos, c := range w.chunks {
		if !c.Ready() || len(w.chunkRequests[pos]) != 0 || len(c.viewers) != 0 || len(c.loaders) != 0 {
			continue
		}
		chunksCollected++
		entitiesCollected += len(c.Entities)
		blockEntitiesCollected += len(c.BlockEntities)
		w.closeChunk(tx, pos, c)
	}
	return
}

// closeUnusedChunks closes chunks currently not in use by any viewer or loader.
func (w *World) closeUnusedChunks(tx *Tx) {
	w.CollectGarbage(tx)
}

// Column represents the data of a chunk including the (block) entities and
// viewers and loaders.
type Column struct {
	modified bool

	*chunk.Chunk
	Entities                        []*EntityHandle
	entityIndices                   map[*EntityHandle]int
	BlockEntities                   map[cube.Pos]Block
	randomTickSubChunksDirty        bool
	cachedRandomTickSubChunkIndices []int
	tickerBlockEntitiesDirty        bool
	cachedTickerBlockEntities       []cube.Pos
	subChunkHeightMaps              map[int16]cachedSubChunkHeightMap
	networkBiomePayload             cachedBlockEntityPayload
	networkSubChunkPayloads         map[int16]cachedBlockEntityPayload
	limitedChunkPayload             cachedBlockEntityPayload
	fullChunkPayload                cachedBlockEntityPayload
	chunkBlockEntityPayload         cachedBlockEntityPayload
	chunkBlockEntityPayloadNoBorder cachedBlockEntityPayload
	subChunkBlockEntityPayloads     map[int16]cachedBlockEntityPayload

	viewers map[Viewer]struct{}
	loaders []*Loader

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

// viewerSlicePool recycles temporary []Viewer buffers created while broadcasting world state to reduce GC churn.
// The default capacity is intentionally small: most columns have just a handful of viewers, yet larger slices are
// returned to the pool so hot paths can still reuse previously grown allocations instead of re-allocating.
var viewerSlicePool = sync.Pool{
	New: func() any {
		return make([]Viewer, 0, 8)
	},
}

// newColumn returns a new Column wrapper around the chunk.Chunk passed.
func newColumn(c *chunk.Chunk) *Column {
	return &Column{
		Chunk:                    c,
		BlockEntities:            map[cube.Pos]Block{},
		randomTickSubChunksDirty: true,
		tickerBlockEntitiesDirty: true,
		readyCh:                  make(chan struct{}),
		viewers:                  make(map[Viewer]struct{}),
	}
}

// addEntity appends an entity handle to the column and tracks its slot for O(1) removal.
func (c *Column) addEntity(handle *EntityHandle) {
	if c.entityIndices == nil {
		c.entityIndices = make(map[*EntityHandle]int, 4)
	}
	c.entityIndices[handle] = len(c.Entities)
	c.Entities = append(c.Entities, handle)
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
	clear(c.Entities)
	c.Entities = c.Entities[:0]
	if len(c.entityIndices) > 0 {
		clear(c.entityIndices)
	}
}

func (c *Column) invalidateRandomTickSubChunks() {
	c.randomTickSubChunksDirty = true
}

func (c *Column) invalidateTickerBlockEntities() {
	c.tickerBlockEntitiesDirty = true
}

func (c *Column) invalidateSubChunkHeightMaps() {
	clear(c.subChunkHeightMaps)
}

func (c *Column) invalidateNetworkBiomePayload() {
	c.networkBiomePayload = cachedBlockEntityPayload{}
	c.limitedChunkPayload = cachedBlockEntityPayload{}
	c.fullChunkPayload = cachedBlockEntityPayload{}
}

func (c *Column) invalidateNetworkSubChunkPayloads() {
	clear(c.networkSubChunkPayloads)
	c.fullChunkPayload = cachedBlockEntityPayload{}
}

func (c *Column) invalidateBlockEntityPayloads() {
	c.chunkBlockEntityPayload = cachedBlockEntityPayload{}
	c.chunkBlockEntityPayloadNoBorder = cachedBlockEntityPayload{}
	clear(c.subChunkBlockEntityPayloads)
	c.fullChunkPayload = cachedBlockEntityPayload{}
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

// CachedNetworkSubChunkPayload returns the cached encoded network payload for a sub-chunk.
func (c *Column) CachedNetworkSubChunkPayload(ind int16) ([]byte, bool) {
	if c.networkSubChunkPayloads == nil {
		return nil, false
	}
	payload, ok := c.networkSubChunkPayloads[ind]
	return payload.payload, ok && payload.ready
}

// CacheNetworkSubChunkPayload stores the encoded network payload for a sub-chunk.
func (c *Column) CacheNetworkSubChunkPayload(ind int16, payload []byte) {
	if c.networkSubChunkPayloads == nil {
		c.networkSubChunkPayloads = make(map[int16]cachedBlockEntityPayload, 4)
	}
	c.networkSubChunkPayloads[ind] = cachedBlockEntityPayload{payload: payload, ready: true}
}

// CachedLimitedChunkPayload returns the cached limited chunk raw payload.
func (c *Column) CachedLimitedChunkPayload() ([]byte, bool) {
	return c.limitedChunkPayload.payload, c.limitedChunkPayload.ready
}

// CacheLimitedChunkPayload stores the limited chunk raw payload.
func (c *Column) CacheLimitedChunkPayload(payload []byte) {
	c.limitedChunkPayload = cachedBlockEntityPayload{payload: payload, ready: true}
}

// CachedFullChunkPayload returns the cached full chunk raw payload.
func (c *Column) CachedFullChunkPayload() ([]byte, bool) {
	return c.fullChunkPayload.payload, c.fullChunkPayload.ready
}

// CacheFullChunkPayload stores the full chunk raw payload.
func (c *Column) CacheFullChunkPayload(payload []byte) {
	c.fullChunkPayload = cachedBlockEntityPayload{payload: payload, ready: true}
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

func (c *Column) tickerBlockEntityPositions() []cube.Pos {
	if !c.tickerBlockEntitiesDirty {
		return c.cachedTickerBlockEntities
	}
	positions := c.cachedTickerBlockEntities[:0]
	for pos, block := range c.BlockEntities {
		if _, ok := block.(TickerBlock); ok {
			positions = append(positions, pos)
		}
	}
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

// forEachViewer calls the function passed for each viewer in the column.
func (c *Column) forEachViewer(fn func(Viewer)) {
	if len(c.viewers) == 0 {
		return
	}
	for v := range c.viewers {
		fn(v)
	}
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
	scheduled := w.scheduledUpdates.fromChunk(pos)
	c := &chunk.Column{
		Chunk:           col.Chunk,
		Entities:        make([]chunk.Entity, 0, len(col.Entities)),
		BlockEntities:   make([]chunk.BlockEntity, 0, len(col.BlockEntities)),
		ScheduledBlocks: make([]chunk.ScheduledBlockUpdate, 0, len(scheduled)),
		Tick:            w.scheduledUpdates.currentTick,
	}
	for _, e := range col.Entities {
		if e.t.EncodeEntity() == "minecraft:player" {
			// Player entities are persisted separately from chunk data and should not be stored in the
			// chunk provider. Keeping them out of the provider avoids stale player entities being read
			// back after a restart.
			continue
		}

		data := e.encodeNBT()
		maps.Copy(data, e.t.EncodeNBT(&e.data))
		data["identifier"] = e.t.EncodeEntity()
		c.Entities = append(c.Entities, chunk.Entity{ID: int64(binary.LittleEndian.Uint64(e.id[8:])), Data: data})
	}
	for pos, be := range col.BlockEntities {
		c.BlockEntities = append(c.BlockEntities, chunk.BlockEntity{Pos: pos, Data: be.(NBTer).EncodeNBT()})
	}
	for _, t := range scheduled {
		c.ScheduledBlocks = append(c.ScheduledBlocks, chunk.ScheduledBlockUpdate{Pos: t.pos, Block: w.conf.Blocks.BlockRuntimeID(t.b), Tick: t.t})
	}
	return c
}

// columnFrom converts a chunk.Column to a Column after reading it from a
// provider.
func (w *World) columnFrom(c *chunk.Column, _ ChunkPos) *Column {
	col := newColumn(c.Chunk)
	col.Entities = make([]*EntityHandle, 0, len(c.Entities))
	if len(c.Entities) > 0 {
		col.entityIndices = make(map[*EntityHandle]int, len(c.Entities))
	}
	col.BlockEntities = make(map[cube.Pos]Block, len(c.BlockEntities))
	for _, e := range c.Entities {
		eid, ok := e.Data["identifier"].(string)
		if !ok {
			w.conf.Log.Error("read column: entity without identifier field", "ID", e.ID)
			continue
		}
		if eid == "minecraft:player" {
			// Players are managed separately from chunk entities, so ignore persisted player entries that
			// may have been saved by older versions.
			continue
		}
		t, ok := w.conf.Entities.Lookup(eid)
		if !ok {
			w.conf.Log.Error("read column: unknown entity type", "ID", e.ID, "type", eid)
			continue
		}
		col.addEntity(withBlockRegistryNBT(e.Data, w.conf.Blocks, func() *EntityHandle {
			return entityFromData(t, e.ID, e.Data)
		}))
	}
	for _, be := range c.BlockEntities {
		rid := c.Chunk.Block(uint8(be.Pos[0]), int16(be.Pos[1]), uint8(be.Pos[2]), 0)
		b, ok := w.conf.Blocks.BlockByRuntimeID(rid)
		if !ok {
			w.conf.Log.Error("read column: no block with runtime ID", "ID", rid)
			continue
		}
		nb, ok := b.(NBTer)
		if !ok {
			w.conf.Log.Error("read column: block with nbt does not implement NBTer", "block", fmt.Sprintf("%#v", b))
			continue
		}
		col.BlockEntities[be.Pos] = DecodeNBT(nb, be.Data, w.conf.Blocks).(Block)
	}
	scheduled, savedTick := make([]scheduledTick, 0, len(c.ScheduledBlocks)), c.Tick
	for _, t := range c.ScheduledBlocks {
		bl := w.conf.Blocks.BlockByRuntimeIDOrAir(t.Block)
		scheduled = append(scheduled, scheduledTick{
			pos:   t.Pos,
			b:     bl,
			bhash: w.conf.Blocks.BlockHash(bl),
			t:     w.scheduledUpdates.currentTick + (t.Tick - savedTick),
		})
	}
	w.scheduledUpdates.add(scheduled)
	col.markReady()
	return col
}
