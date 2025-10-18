package world

import (
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
	"github.com/df-mc/dragonfly/server/event"
	"github.com/df-mc/dragonfly/server/internal/sliceutil"
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

	queue        chan transaction
	queueClosing chan struct{}
	queueing     sync.WaitGroup

	// advance is a bool that specifies if this World should advance the current
	// tick, time and weather saved in the Settings struct held by the World.
	advance bool

	o sync.Once

	set     *Settings
	handler atomic.Pointer[Handler]

	weather

	closing chan struct{}
	running sync.WaitGroup

	// chunks holds a cache of chunks currently loaded. These chunks are cleared
	// from this map after some time of not being used.
	chunks map[ChunkPos]*Column

	// entities holds a map of entities currently loaded and metadata associated
	// with them, such as the last chunk position they were located in and a
	// cached instance of the opened entity. The cached entity instance allows us
	// to reuse the same Go object across ticks instead of re-opening it each
	// iteration, which significantly reduces pressure on the garbage collector
	// and CPU usage during the entity pipeline.
	entities map[*EntityHandle]*entityState

	r *rand.Rand

	tps atomic.Uint64

	// scheduledUpdates is a map of tick time values indexed by the block
	// position at which an update is scheduled. If the current tick exceeds the
	// tick value passed, the block update will be performed and the entry will
	// be removed from the map.
	scheduledUpdates *scheduledTickQueue
	neighbourUpdates []neighbourUpdate

	scratchRandom           []cube.Pos
	scratchBlockEntities    []cube.Pos
	scratchLoaderAreas      []loaderActiveArea
	scratchActiveEntities   []*EntityHandle
	scratchSleepingEntities []*EntityHandle
	scratchActiveRefs       map[*EntityHandle]entityChunkRef
	scratchSleepingRefs     map[*EntityHandle]entityChunkRef

	activeColumns     []columnRef
	activeColumnIndex map[ChunkPos]int
	entityColumns     []columnRef
	entityColumnIndex map[ChunkPos]int

	sleepMu         sync.Mutex
	sleepingPlayers map[uuid.UUID]cube.Pos

	viewerMu sync.Mutex
	viewers  map[*Loader]Viewer

	generatorQueue chan generationTask
	// generatorQueueSaturation counts how often chunk generation tasks had to be
	// enqueued asynchronously because the worker queue was full. We use this to
	// rate-limit backpressure warnings so operators can tune queue/worker sizes.
	generatorQueueSaturation atomic.Uint64
	lastQueueSaturationLog   atomic.Uint64
}

type entityState struct {
	pos      ChunkPos
	ent      Entity
	lastTick int64
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
	if binder, ok := s.ent.(interface{ bindTx(*Tx) }); ok {
		binder.bindTx(tx)
	}
	return s.ent
}

type generationTask struct {
	pos ChunkPos
	col *Column
}

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
	w.set.Lock()
	defer w.set.Unlock()
	return w.set.Name
}

// Dimension returns the Dimension assigned to the World in world.New. The sky
// colour and behaviour of a variety of world features differ based on the
// Dimension.
func (w *World) Dimension() Dimension {
	return w.conf.Dim
}

// Range returns the range in blocks of the World (min and max). It is
// equivalent to calling World.Dimension().Range().
func (w *World) Range() cube.Range {
	return w.ra
}

// CurrentTick returns the current tick counter of the world.
func (w *World) CurrentTick() int64 {
	if w == nil {
		return 0
	}
	w.set.Lock()
	defer w.set.Unlock()
	return w.set.CurrentTick
}

// TPS returns the current average ticks per second of the world. The value is
// averaged over the last tpsSampleSize ticks and may be zero if no samples have
// been recorded yet.
func (w *World) TPS() float64 {
	return math.Float64frombits(w.tps.Load())
}

// LoadedChunkCount returns the number of chunks currently kept in memory by the
// world.
func (w *World) LoadedChunkCount() int {
	return len(w.chunks)
}

// EntityCount returns the number of entities tracked by the world.
func (w *World) EntityCount() int {
	return len(w.entities)
}

// ExecFunc is a function that performs a synchronised transaction on a World.
type ExecFunc func(tx *Tx)

// Exec performs a synchronised transaction f on a World. Exec returns a channel
// that is closed once the transaction is complete.
func (w *World) Exec(f ExecFunc) <-chan struct{} {
	c := make(chan struct{})
	w.queue <- normalTransaction{c: c, f: f}
	return c
}

func (w *World) weakExec(invalid *atomic.Bool, cond *sync.Cond, f ExecFunc) <-chan bool {
	c := make(chan bool, 1)
	w.queue <- weakTransaction{c: c, f: f, invalid: invalid, cond: cond}
	return c
}

// handleTransactions continuously reads transactions from the queue and runs
// them.
func (w *World) handleTransactions() {
	for {
		select {
		case tx := <-w.queue:
			tx.Run(w)
		case <-w.queueClosing:
			w.queueing.Done()
			return
		}
	}
}

// EntityRegistry returns the EntityRegistry that was passed to the World's
// Config upon construction.
func (w *World) EntityRegistry() EntityRegistry {
	return w.conf.Entities
}

// block reads a block from the position passed. If a chunk is not yet loaded
// at that position, the chunk is loaded, or generated if it could not be found
// in the world save, and the block returned.
func (w *World) block(pos cube.Pos) Block {
	return w.blockInChunk(w.chunk(chunkPosFromBlockPos(pos)), pos)
}

// blockInChunk reads a block from a chunk at the position passed. The block
// is assumed to be within the chunk passed.
func (w *World) blockInChunk(c *Column, pos cube.Pos) Block {
	if pos.OutOfBounds(w.ra) {
		// Fast way out.
		return air()
	}
	rid := c.Block(uint8(pos[0]), int16(pos[1]), uint8(pos[2]), 0)
	if nbtBlocks[rid] {
		// The block was also a block entity, so we look it up in the map.
		if b, ok := c.BlockEntities[pos]; ok {
			return b
		}
		// Despite being a block with NBT, the block didn't actually have any
		// stored NBT yet. We add it here and update the block.
		nbtB := blockByRuntimeIDOrAir(rid).(NBTer).DecodeNBT(map[string]any{}).(Block)
		c.BlockEntities[pos] = nbtB
		for v := range c.viewers {
			v.ViewBlockUpdate(pos, nbtB, 0)
		}
		return nbtB
	}
	return blockByRuntimeIDOrAir(rid)
}

// biome reads the Biome at the position passed. If a chunk is not yet loaded
// at that position, the chunk is loaded, or generated if it could not be found
// in the world save, and the Biome returned.
func (w *World) biome(pos cube.Pos) Biome {
	if pos.OutOfBounds(w.Range()) {
		// Fast way out.
		return ocean()
	}
	id := int(w.chunk(chunkPosFromBlockPos(pos)).Biome(uint8(pos[0]), int16(pos[1]), uint8(pos[2])))
	b, ok := BiomeByID(id)
	if !ok {
		w.conf.Log.Error("biome not found by ID", "ID", id)
	}
	return b
}

// highestLightBlocker gets the Y value of the highest fully light blocking
// block at the x and z values passed in the World.
func (w *World) highestLightBlocker(x, z int) int {
	return int(w.chunk(ChunkPos{int32(x >> 4), int32(z >> 4)}).HighestLightBlocker(uint8(x), uint8(z)))
}

// highestBlock looks up the highest non-air block in the World at a specific x
// and z The y value of the highest block is returned, or 0 if no blocks were
// present in the column.
func (w *World) highestBlock(x, z int) int {
	return int(w.chunk(ChunkPos{int32(x >> 4), int32(z >> 4)}).HighestBlock(uint8(x), uint8(z)))
}

// highestObstructingBlock returns the highest block in the World at a given x
// and z that has at least a solid top or bottom face.
func (w *World) highestObstructingBlock(x, z int) int {
	yHigh := w.highestBlock(x, z)
	src := worldSource{w: w}
	for y := yHigh; y >= w.Range()[0]; y-- {
		pos := cube.Pos{x, y, z}
		m := w.block(pos).Model()
		if m.FaceSolid(pos, cube.FaceUp, src) || m.FaceSolid(pos, cube.FaceDown, src) {
			return y
		}
	}
	return w.Range()[0]
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
func (w *World) setBlock(pos cube.Pos, b Block, opts *SetOpts) {
	if pos.OutOfBounds(w.Range()) {
		// Fast way out.
		return
	}
	if opts == nil {
		opts = &SetOpts{}
	}

	x, y, z := uint8(pos[0]), int16(pos[1]), uint8(pos[2])
	c := w.chunk(chunkPosFromBlockPos(pos))

	rid := BlockRuntimeID(b)

	var before uint32
	if rid != airRID && !opts.DisableLiquidDisplacement {
		before = c.Block(x, y, z, 0)
	}

	c.modified = true
	c.SetBlock(x, y, z, 0, rid)
	if nbtBlocks[rid] {
		c.BlockEntities[pos] = b
	} else {
		delete(c.BlockEntities, pos)
	}

	if !opts.DisableLiquidDisplacement {
		var secondLayer Block

		if rid == airRID {
			if li := c.Block(x, y, z, 1); li != airRID {
				c.SetBlock(x, y, z, 0, li)
				c.SetBlock(x, y, z, 1, airRID)
				secondLayer = air()
				b = blockByRuntimeIDOrAir(li)
			}
		} else if liquidDisplacingBlocks[rid] {
			if liquidBlocks[before] {
				l := blockByRuntimeIDOrAir(before)
				if b.(LiquidDisplacer).CanDisplace(l.(Liquid)) {
					c.SetBlock(x, y, z, 1, before)
					secondLayer = l
				}
			}
		} else if li := c.Block(x, y, z, 1); li != airRID {
			c.SetBlock(x, y, z, 1, airRID)
			secondLayer = air()
		}

		if secondLayer != nil {
			c.forEachViewer(func(viewer Viewer) {
				viewer.ViewBlockUpdate(pos, secondLayer, 1)
			})
		}
	}

	c.forEachViewer(func(viewer Viewer) {
		viewer.ViewBlockUpdate(pos, b, 0)
	})

	if !opts.DisableBlockUpdates {
		w.doBlockUpdatesAround(pos)
	}
}

// setBiome sets the Biome at the position passed. If a chunk is not yet loaded
// at that position, the chunk is first loaded or generated if it could not be
// found in the world save.
func (w *World) setBiome(pos cube.Pos, b Biome) {
	if pos.OutOfBounds(w.Range()) {
		// Fast way out.
		return
	}
	c := w.chunk(chunkPosFromBlockPos(pos))
	c.modified = true
	c.SetBiome(uint8(pos[0]), int16(pos[1]), uint8(pos[2]), uint32(b.EncodeBiome()))
}

// buildStructure builds a Structure passed at a specific position in the
// world. Unlike setBlock, it takes a Structure implementation, which provides
// blocks to be placed at a specific location. buildStructure is specifically
// optimised to be able to process a large batch of chunks simultaneously and
// will do so within much less time than separate setBlock calls would. The
// method operates on a per-chunk basis, setting all blocks within a single
// chunk part of the Structure before moving on to the next chunk.
func (w *World) buildStructure(pos cube.Pos, s Structure) {
	dim := s.Dimensions()
	width, height, length := dim[0], dim[1], dim[2]
	maxX, maxY, maxZ := pos[0]+width, pos[1]+height, pos[2]+length
	f := func(x, y, z int) Block {
		return w.block(cube.Pos{pos[0] + x, pos[1] + y, pos[2] + z})
	}

	// We approach this on a per-chunk basis, so that we can keep only one chunk
	// in memory at a time while not needing to acquire a new chunk lock for
	// every block. This also allows us not to send block updates, but instead
	// send a single chunk update once.
	for chunkX := pos[0] >> 4; chunkX <= maxX>>4; chunkX++ {
		for chunkZ := pos[2] >> 4; chunkZ <= maxZ>>4; chunkZ++ {
			chunkPos := ChunkPos{int32(chunkX), int32(chunkZ)}
			c := w.chunk(chunkPos)

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
								rid := BlockRuntimeID(b)
								sub.SetBlock(uint8(xOffset), uint8(yOffset), uint8(zOffset), 0, rid)

								nbtPos := cube.Pos{xOffset, yOffset, zOffset}
								if nbtBlocks[rid] {
									c.BlockEntities[nbtPos] = b
								} else {
									delete(c.BlockEntities, nbtPos)
								}
							}
							if liq != nil {
								sub.SetBlock(uint8(xOffset), uint8(yOffset), uint8(zOffset), 1, BlockRuntimeID(liq))
							} else if len(sub.Layers()) > 1 {
								sub.SetBlock(uint8(xOffset), uint8(yOffset), uint8(zOffset), 1, airRID)
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
				viewer.ViewChunk(chunkPos, w.Dimension(), c.BlockEntities, c.Chunk)
			}
		}
	}
}

// liquid attempts to return a Liquid block at the position passed. This
// Liquid may be in the foreground or in any other layer. If found, the Liquid
// is returned. If not, the bool returned is false.
func (w *World) liquid(pos cube.Pos) (Liquid, bool) {
	if pos.OutOfBounds(w.Range()) {
		// Fast way out.
		return nil, false
	}
	c := w.chunk(chunkPosFromBlockPos(pos))
	x, y, z := uint8(pos[0]), int16(pos[1]), uint8(pos[2])

	id := c.Block(x, y, z, 0)
	b, ok := BlockByRuntimeID(id)
	if !ok {
		w.conf.Log.Error("Liquid: no block with runtime ID", "ID", id)
		return nil, false
	}
	if liq, ok := b.(Liquid); ok {
		return liq, true
	}
	id = c.Block(x, y, z, 1)

	b, ok = BlockByRuntimeID(id)
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
func (w *World) setLiquid(pos cube.Pos, b Liquid) {
	if pos.OutOfBounds(w.Range()) {
		// Fast way out.
		return
	}
	chunkPos := chunkPosFromBlockPos(pos)
	c := w.chunk(chunkPos)
	if b == nil {
		w.removeLiquids(c, pos)
		w.doBlockUpdatesAround(pos)
		return
	}
	x, y, z := uint8(pos[0]), int16(pos[1]), uint8(pos[2])
	if !replaceable(w, c, pos, b) {
		if displacer, ok := w.blockInChunk(c, pos).(LiquidDisplacer); !ok || !displacer.CanDisplace(b) {
			return
		}
	}
	rid := BlockRuntimeID(b)
	if w.removeLiquids(c, pos) {
		c.SetBlock(x, y, z, 0, rid)
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
}

// removeLiquids removes any liquid blocks that may be present at a specific
// block position in the chunk passed. The bool returned specifies if no blocks
// were left on the foreground layer.
func (w *World) removeLiquids(c *Column, pos cube.Pos) bool {
	x, y, z := uint8(pos[0]), int16(pos[1]), uint8(pos[2])

	noneLeft := false
	if noLeft, changed := w.removeLiquidOnLayer(c.Chunk, x, y, z, 0); noLeft {
		if changed {
			for v := range c.viewers {
				v.ViewBlockUpdate(pos, air(), 0)
			}
		}
		noneLeft = true
	}
	if _, changed := w.removeLiquidOnLayer(c.Chunk, x, y, z, 1); changed {
		for v := range c.viewers {
			v.ViewBlockUpdate(pos, air(), 1)
		}
	}
	return noneLeft
}

// removeLiquidOnLayer removes a liquid block from a specific layer in the
// chunk passed, returning true if successful.
func (w *World) removeLiquidOnLayer(c *chunk.Chunk, x uint8, y int16, z, layer uint8) (bool, bool) {
	id := c.Block(x, y, z, layer)

	b, ok := BlockByRuntimeID(id)
	if !ok {
		w.conf.Log.Error("removeLiquidOnLayer: no block with runtime ID", "ID", id)
		return false, false
	}
	if _, ok := b.(Liquid); ok {
		c.SetBlock(x, y, z, layer, airRID)
		return true, true
	}
	return id == airRID, false
}

// additionalLiquid checks if the block at a position has additional liquid on
// another layer and returns the liquid if so.
func (w *World) additionalLiquid(pos cube.Pos) (Liquid, bool) {
	if pos.OutOfBounds(w.Range()) {
		// Fast way out.
		return nil, false
	}
	c := w.chunk(chunkPosFromBlockPos(pos))
	id := c.Block(uint8(pos[0]), int16(pos[1]), uint8(pos[2]), 1)

	b, ok := BlockByRuntimeID(id)
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
func (w *World) light(pos cube.Pos) uint8 {
	if pos[1] < w.ra[0] {
		// Fast way out.
		return 0
	}
	if pos[1] > w.ra[1] {
		// Above the rest of the world, so full skylight.
		return 15
	}
	return w.chunk(chunkPosFromBlockPos(pos)).Light(uint8(pos[0]), int16(pos[1]), uint8(pos[2]))
}

// skyLight returns the skylight level at the position passed. This light level
// is not influenced by blocks that emit light, such as torches. The light
// value, similarly to light, is a value in the range 0-15, where 0 means no
// light is present.
func (w *World) skyLight(pos cube.Pos) uint8 {
	if pos[1] < w.ra[0] {
		// Fast way out.
		return 0
	}
	if pos[1] > w.ra[1] {
		// Above the rest of the world, so full skylight.
		return 15
	}
	return w.chunk(chunkPosFromBlockPos(pos)).SkyLight(uint8(pos[0]), int16(pos[1]), uint8(pos[2]))
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

// enableTimeCycle enables or disables the time cycling of the World.
func (w *World) enableTimeCycle(v bool) {
	if w == nil {
		return
	}
	w.set.Lock()
	defer w.set.Unlock()
	w.set.TimeCycle = v
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
func (w *World) temperature(pos cube.Pos) float64 {
	const (
		tempDrop = 1.0 / 600
		seaLevel = 64
	)
	diff := max(pos[1]-seaLevel, 0)
	return w.biome(pos).Temperature() - float64(diff)*tempDrop
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
	ctx := event.C(tx)
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
	currentTick := w.set.CurrentTick
	w.set.Unlock()
	state := &entityState{pos: pos, lastTick: currentTick, isItem: handle.t.EncodeEntity() == "minecraft:item"}
	w.entities[handle] = state

	c := w.chunk(pos)
	c.Entities, c.modified = append(c.Entities, handle), true
	w.addEntityColumn(pos, c)

	e := state.entity(tx, handle)
	for v := range c.viewers {
		// Show the entity to all viewers in the chunk of the entity.
		showEntity(e, v)
	}
	w.Handler().HandleEntitySpawn(tx, e)
	return e
}

// AddSleepingPlayer registers a player as sleeping at the given bed position.
func (w *World) AddSleepingPlayer(id uuid.UUID, pos cube.Pos) {
	w.sleepMu.Lock()
	if w.sleepingPlayers == nil {
		w.sleepingPlayers = make(map[uuid.UUID]cube.Pos)
	}
	w.sleepingPlayers[id] = pos
	w.sleepMu.Unlock()
}

// RemoveSleepingPlayer removes a sleeping player entry.
func (w *World) RemoveSleepingPlayer(id uuid.UUID) {
	w.sleepMu.Lock()
	if w.sleepingPlayers != nil {
		delete(w.sleepingPlayers, id)
	}
	w.sleepMu.Unlock()
}

// SleepingPlayers returns a snapshot of all players currently sleeping in the world.
func (w *World) SleepingPlayers() map[uuid.UUID]cube.Pos {
	w.sleepMu.Lock()
	defer w.sleepMu.Unlock()
	if len(w.sleepingPlayers) == 0 {
		return nil
	}
	snapshot := make(map[uuid.UUID]cube.Pos, len(w.sleepingPlayers))
	for id, pos := range w.sleepingPlayers {
		snapshot[id] = pos
	}
	return snapshot
}

// SleepingPlayerCount returns the amount of players currently sleeping.
func (w *World) SleepingPlayerCount() int {
	w.sleepMu.Lock()
	count := len(w.sleepingPlayers)
	w.sleepMu.Unlock()
	return count
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

	c := w.chunk(pos)
	c.Entities, c.modified = sliceutil.DeleteVal(c.Entities, handle), true
	if len(c.Entities) == 0 {
		w.removeEntityColumn(pos)
	}

	for v := range c.viewers {
		v.HideEntity(e)
	}
	delete(w.entities, handle)
	handle.unsetAndLockWorld()
	return handle
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
				for _, handle := range c.Entities {
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
		for handle, state := range w.entities {
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
		for handle, state := range w.entities {
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
	w.set.Lock()
	defer w.set.Unlock()
	return w.set.Spawn
}

// SetSpawn sets the spawn of the world to a different position. The player
// will be spawned in the center of this position when newly joining.
func (w *World) SetSpawn(pos cube.Pos) {
	if w == nil {
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
	w.scheduledUpdates.schedule(pos, b, delay)
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
// Dimension. If no destination World could be found, the current World is
// returned. Calling PortalDestination(Nether) on an Overworld World returns
// Nether, while calling PortalDestination(Nether) on a Nether World will
// return the Overworld, for instance.
func (w *World) PortalDestination(dim Dimension) *World {
	if w.conf.PortalDestination == nil {
		return w
	}
	if res := w.conf.PortalDestination(dim); res != nil {
		return res
	}
	return w
}

// Save saves the World to the provider.
func (w *World) Save() {
	<-w.Exec(w.save(w.saveChunk))
}

// save saves all loaded chunks to the World's provider.
func (w *World) save(f func(*Tx, ChunkPos, *Column)) ExecFunc {
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
	w.saveChunk(tx, pos, c)
	w.scheduledUpdates.removeChunk(pos)
	w.removeActiveColumn(pos)
	w.removeEntityColumn(pos)
	// Note: We close c.Entities here because some entities may remove
	// themselves from the world in their Close method, which can lead to
	// unexpected conditions.
	for _, e := range slices.Clone(c.Entities) {
		_ = e.mustEntity(tx).Close()
	}
	clear(c.Entities)
	delete(w.chunks, pos)
}

// Close closes the world and saves all chunks currently loaded.
func (w *World) Close() error {
	w.o.Do(w.close)
	return nil
}

// close stops the World from ticking, saves all chunks to the Provider and
// updates the world's settings.
func (w *World) close() {
	<-w.Exec(func(tx *Tx) {
		// Let user code run anything that needs to be finished before closing.
		w.Handler().HandleClose(tx)
		w.Handle(NopHandler{})

		w.save(w.closeChunk)(tx)
	})

	close(w.closing)
	w.running.Wait()

	close(w.queueClosing)
	w.queueing.Wait()

	if w.set.ref.Add(-1); !w.advance {
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
	w.set.Lock()
	raining, thundering := w.set.Raining, w.set.Raining && w.set.Thundering
	w.set.Unlock()
	l.viewer.ViewWeather(raining, thundering)
	l.viewer.ViewWorldSpawn(w.Spawn())
}

// addViewer adds a viewer to the World at a given position. Any events that
// happen in the chunk at that position, such as block and entity changes, will
// be sent to the viewer.
func (w *World) addViewer(tx *Tx, pos ChunkPos, c *Column, loader *Loader) {
	if loader.viewer != nil {
		c.viewers[loader.viewer] = struct{}{}
	}
	c.loaders = append(c.loaders, loader)

	w.addActiveColumn(pos, c)

	for _, entity := range c.Entities {
		showEntity(entity.mustEntity(tx), loader.viewer)
	}
}

// removeViewer removes a viewer from a chunk position. All entities will be
// hidden from the viewer and no more calls will be made when events in the
// chunk happen.
func (w *World) removeViewer(tx *Tx, pos ChunkPos, loader *Loader) {
	if w == nil {
		return
	}
	c, ok := w.chunks[pos]
	if !ok {
		return
	}
	if i := slices.Index(c.loaders, loader); i != -1 {
		c.loaders = slices.Delete(c.loaders, i, i+1)
	}

	if len(c.loaders) == 0 {
		w.removeActiveColumn(pos)
	}

	// Hide all entities in the chunk from the viewer.
	delete(c.viewers, loader.viewer)
	if loader.viewer != nil {
		for _, entity := range c.Entities {
			loader.viewer.HideEntity(entity.mustEntity(tx))
		}
	}

	if len(c.viewers) == 0 && len(c.loaders) == 0 {
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

// chunk reads a chunk from the position passed. If a chunk at that position is
// not yet loaded, the chunk is loaded from the provider, or generated if it
// did not yet exist. Additionally, chunks newly loaded have the light in them
// calculated before they are returned.
func (w *World) chunk(pos ChunkPos) *Column {
	c, ok := w.chunks[pos]
	if ok {
		c.waitReady()
		c.ensureLight(w, pos)
		return c
	}
	c, err := w.loadChunk(pos)
	if !c.Ready() {
		c.waitReady()
	}
	c.ensureLight(w, pos)
	if err != nil {
		w.conf.Log.Error("load chunk: "+err.Error(), "X", pos[0], "Z", pos[1])
	}
	return c
}

// chunkIfReady attempts to return a chunk from the position passed. If the chunk is not yet generated, the bool
// returned will be false and the chunk will be generated asynchronously.
func (w *World) chunkIfReady(pos ChunkPos) (*Column, bool) {
	if c, ok := w.chunks[pos]; ok {
		if !c.Ready() {
			return c, false
		}
		c.ensureLight(w, pos)
		return c, true
	}
	c, err := w.loadChunk(pos)
	if !c.Ready() {
		return c, false
	}
	c.ensureLight(w, pos)
	if err != nil {
		w.conf.Log.Error("load chunk: "+err.Error(), "X", pos[0], "Z", pos[1])
	}
	return c, true
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
		w.chunks[pos] = col

		// Mark the column ready immediately.
		col.markReady()

		// Register all entities contained in this column into the world.
		w.set.Lock()
		currentTick := w.set.CurrentTick
		w.set.Unlock()
		for _, e := range col.Entities {
			w.entities[e] = &entityState{
				pos:      pos,
				lastTick: currentTick,
				isItem:   e.t.EncodeEntity() == "minecraft:item",
			}
			e.w = w
		}

		if len(col.Entities) > 0 {
			w.addEntityColumn(pos, col)
		}

		return col, nil

	case errors.Is(err, leveldb.ErrNotFound):
		// Case 2: Column not found in storage — needs generation.
		// Create a new empty column filled with air.
		col := newColumn(chunk.New(airRID, w.Range()))
		w.chunks[pos] = col

		// Schedule asynchronous generation.
		// generateChunkAsync is shutdown-safe and will mark ready if closing.
		w.generateChunkAsync(pos, col)

		return col, nil

	default:
		// Case 3: Unexpected error occurred (I/O failure, corruption, etc.)
		// To avoid deadlocks, return a ready empty column and the error.
		col := newColumn(chunk.New(airRID, w.Range()))
		col.markReady()
		return col, err
	}
}

// generateChunkAsync schedules an asynchronous chunk generation task for the given position.
// It ensures that no new tasks are enqueued once the world begins shutting down (w.closing is closed).
// If shutdown is in progress, the column is immediately marked as ready to avoid deadlocks.
//
// This prevents chunks from being stuck in a "not ready" state during shutdown,
// which could otherwise cause Close() or c.waitReady() to block forever.
func (w *World) generateChunkAsync(pos ChunkPos, col *Column) {
	task := generationTask{pos: pos, col: col}

	select {
	case <-w.closing:
		// The world is closing — do not enqueue any new generation tasks.
		// Mark the column as ready immediately, ensuring waiters do not block.
		col.markReady()

	case w.generatorQueue <- task:
		// Successfully enqueued the generation task in the worker queue.
		// A generator worker will pick it up and process it.

	default:
		// The queue is full — fall back to asynchronous enqueue.
		// This allows us to avoid blocking the main thread while still handling
		// backpressure. enqueueGeneration itself respects shutdown signals.
		go w.enqueueGeneration(task)
		w.handleGeneratorBackpressure()
	}
}

// enqueueGeneration tries to enqueue a chunk generation task asynchronously.
// If the world is already shutting down, the column is immediately marked as ready
// so that no goroutine waiting on it will hang indefinitely.
func (w *World) enqueueGeneration(task generationTask) {
	select {
	case <-w.closing:
		// World is closing — skip enqueue, mark column ready.
		task.col.markReady()
	case w.generatorQueue <- task:
		// Successfully enqueued after waiting for space in the queue.
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

	for {
		select {
		case task := <-w.generatorQueue:
			// A new generation task is available — process it immediately.
			w.runGenerationTask(task)

		case <-w.closing:
			// Shutdown signal received — mark all remaining queued columns as ready.
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

		// Mark the column as ready regardless of success or failure.
		task.col.markReady()
	}()

	// Perform the actual chunk generation.
	// The generator implementation is responsible for populating the chunk’s data.
	w.conf.Generator.GenerateChunk(task.pos, task.col.Chunk)
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
	closeUnused := time.NewTicker(time.Minute * 2)
	defer closeUnused.Stop()

	for {
		select {
		case <-closeUnused.C:
			<-w.Exec(w.closeUnusedChunks)
		case <-save.C:
			w.Save()
		case <-w.closing:
			w.running.Done()
			return
		}
	}
}

// CollectGarbage closes chunks that have no viewers and returns the number of
// chunks, entities and block entities that were removed as a result.
func (w *World) CollectGarbage(tx *Tx) (chunksCollected, entitiesCollected, blockEntitiesCollected int) {
	for pos, c := range w.chunks {
		if len(c.viewers) != 0 || len(c.loaders) != 0 {
			continue
		}
		chunksCollected++
		entitiesCollected += len(c.Entities)
		blockEntitiesCollected += len(c.BlockEntities)
		w.closeChunk(tx, pos, c)
	}
	return
}

// closeUnusedChunk is called every 5 minutes by autoSave.
func (w *World) closeUnusedChunks(tx *Tx) {
	w.CollectGarbage(tx)
}

// Column represents the data of a chunk including the (block) entities and
// viewers and loaders.
type Column struct {
	modified bool

	*chunk.Chunk
	Entities      []*EntityHandle
	BlockEntities map[cube.Pos]Block

	viewers map[Viewer]struct{}
	loaders []*Loader

	ready      atomic.Bool
	readyCh    chan struct{}
	lightOnce  sync.Once
	lightReady atomic.Bool
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
		Chunk:         c,
		BlockEntities: map[cube.Pos]Block{},
		readyCh:       make(chan struct{}),
		viewers:       make(map[Viewer]struct{}),
	}
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

// ensureLight fills and spreads light for the Column once.
func (c *Column) ensureLight(w *World, pos ChunkPos) {
	c.lightOnce.Do(func() {
		chunk.LightArea([]*chunk.Chunk{c.Chunk}, int(pos[0]), int(pos[1])).Fill()
		c.lightReady.Store(true)
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
		c.ScheduledBlocks = append(c.ScheduledBlocks, chunk.ScheduledBlockUpdate{Pos: t.pos, Block: BlockRuntimeID(t.b), Tick: t.t})
	}
	return c
}

// columnFrom converts a chunk.Column to a Column after reading it from a
// provider.
func (w *World) columnFrom(c *chunk.Column, _ ChunkPos) *Column {
	col := newColumn(c.Chunk)
	col.Entities = make([]*EntityHandle, 0, len(c.Entities))
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
		col.Entities = append(col.Entities, entityFromData(t, e.ID, e.Data))
	}
	for _, be := range c.BlockEntities {
		rid := c.Chunk.Block(uint8(be.Pos[0]), int16(be.Pos[1]), uint8(be.Pos[2]), 0)
		b, ok := BlockByRuntimeID(rid)
		if !ok {
			w.conf.Log.Error("read column: no block with runtime ID", "ID", rid)
			continue
		}
		nb, ok := b.(NBTer)
		if !ok {
			w.conf.Log.Error("read column: block with nbt does not implement NBTer", "block", fmt.Sprintf("%#v", b))
			continue
		}
		col.BlockEntities[be.Pos] = nb.DecodeNBT(be.Data).(Block)
	}
	scheduled, savedTick := make([]scheduledTick, 0, len(c.ScheduledBlocks)), c.Tick
	for _, t := range c.ScheduledBlocks {
		bl := blockByRuntimeIDOrAir(t.Block)
		scheduled = append(scheduled, scheduledTick{pos: t.Pos, b: bl, bhash: BlockHash(bl), t: w.scheduledUpdates.currentTick + (t.Tick - savedTick)})
	}
	w.scheduledUpdates.add(scheduled)
	col.markReady()
	return col
}
