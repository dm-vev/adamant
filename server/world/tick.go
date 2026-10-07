package world

import (
	"math"
	"math/rand/v2"
	"slices"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/go-gl/mathgl/mgl64"
)

// ticker implements World ticking methods.
type ticker struct {
	interval time.Duration
}

type entityChunkRef struct {
	col *Column
	pos ChunkPos
}

const (
	tpsSampleSize              = 20
	tpsWarningThreshold        = 19.0
	passiveMaintenanceInterval = 80
	maxNeighbourUpdatesPerTick = 16384
)

// tickLoop starts ticking the World 20 times every second, updating all
// entities, blocks and other features such as the time and weather of the
// world, as required.
func (t ticker) tickLoop(w *World) {
	tc := time.NewTicker(t.interval)
	defer tc.Stop()
	lastTick := time.Now()
	var (
		durationSum time.Duration
		ticksCount  int
		warned      bool
	)
	for {
		select {
		case <-tc.C:
			tickStart := time.Now()
			duration := tickStart.Sub(lastTick)
			lastTick = tickStart
			if duration > 0 {
				durationSum += duration
				ticksCount++
				if ticksCount >= tpsSampleSize {
					avg := durationSum / time.Duration(ticksCount)
					if avg > 0 {
						tps := 1.0 / avg.Seconds()
						w.tps.Store(math.Float64bits(tps))
						if tps < tpsWarningThreshold {
							if !warned {
								w.conf.Log.Warn("TPS dropped below threshold.", "tps", tps)
								warned = true
							}
						} else if warned {
							warned = false
						}
					} else {
						w.tps.Store(math.Float64bits(0))
					}
					durationSum = 0
					ticksCount = 0
				}
			}
			<-w.exec(t.tick)
		case <-w.closing:
			// World is being closed: Stop ticking and get rid of a task.
			w.running.Done()
			return
		}
	}
}

// AdvanceTick advances the World by a single tick. It is generally only useful
// for Worlds created with Config.Synchronous set: other Worlds tick
// automatically 20 times per second. Synchronous Worlds tick loaded chunks
// even when no viewers are present.
func (w *World) AdvanceTick() {
	<-w.exec(ticker{}.tick)
}

// tick performs a tick on the World and updates the time, weather, blocks and
// entities that require updates.
func (t ticker) tick(tx *Tx) {
	viewers, loaders := tx.World().allViewers()
	w := tx.World()
	defer w.releaseViewers(viewers)

	var spawnX, spawnZ int
	resolveSpawnY := false
	w.set.Lock()
	if s := w.set.Spawn; s[1] > tx.Range()[1] && w.Dimension() == Overworld {
		spawnX, spawnZ = s[0], s[2]
		resolveSpawnY = true
	}
	w.set.Unlock()

	if resolveSpawnY {
		// Vanilla will set the spawn position's Y value to max to indicate that
		// the player should spawn at the highest position in the world.
		y := tx.highestObstructingBlock(spawnX, spawnZ) + 1
		w.set.Lock()
		if s := w.set.Spawn; s[0] == spawnX && s[2] == spawnZ && s[1] > tx.Range()[1] && w.Dimension() == Overworld {
			w.set.Spawn[1] = y
		}
		w.set.Unlock()
	}

	w.set.Lock()
	if len(viewers) == 0 && w.set.CurrentTick != 0 && !w.conf.Synchronous {
		// Don't continue ticking if no viewers are in the world. Synchronous
		// worlds only tick on explicit AdvanceTick calls, so they always tick.
		w.set.Unlock()
		return
	}
	if w.advance {
		w.set.CurrentTick++
		if w.set.TimeCycle {
			w.set.Time++
		}
		if w.set.WeatherCycle {
			w.advanceWeather()
		}
	} else {
		w.tick++
	}

	rain, thunder, tick, tim := w.set.Raining, w.set.Thundering && w.set.Raining, w.currentTickLocked(), int(w.set.Time)
	timeCycle := w.set.TimeCycle

	tryAdvanceDay := false
	if w.set.RequiredSleepTicks > 0 {
		w.set.RequiredSleepTicks--
		tryAdvanceDay = w.set.RequiredSleepTicks <= 0
	}
	w.set.Unlock()
	t.tickInhabitedTime(w, loaders)

	if tryAdvanceDay {
		t.tryAdvanceDay(tx, timeCycle)
	}

	if tick%20 == 0 {
		dimensionTimeCycle := w.Dimension().TimeCycle()
		for _, viewer := range viewers {
			if dimensionTimeCycle && timeCycle {
				viewer.ViewTime(tim)
			}
			if w.Dimension().WeatherCycle() {
				viewer.ViewWeather(rain, thunder)
			}
		}
	}
	if thunder {
		w.tickLightning(tx)
	}

	if w.dragonFight != nil {
		w.dragonFight.Tick(tx)
	}
	t.tickEntities(tx, tick)
	if spawnTick := w.EntityRegistry().Config().NaturalSpawnTick; spawnTick != nil {
		spawnTick(tx)
	}
	tx.TickRaids()
	w.scheduledUpdates.tick(tx, tick)
	t.tickBlocksRandomly(tx, loaders, tick)
	t.performNeighbourUpdates(tx)
	w.redstone.tick(tx, tick)
}

type inhabitedTimePlayer struct {
	pos  mgl64.Vec3
	area loaderActiveArea
}

// tickInhabitedTime follows the server's player-based chunk ticking rules:
// only real, non-spectator player viewers can age a nearby chunk, and chunks
// outside a loader's simulation area or the 128-block player radius do not age.
func (t ticker) tickInhabitedTime(w *World, loaders []*Loader) {
	if len(w.activeColumns) == 0 {
		return
	}
	simRadius := int32(w.tickRange())
	players := w.scratchInhabitedPlayers[:0]
	for _, loader := range loaders {
		player, ok := loader.viewer.(interface {
			GameMode() GameMode
			Position() mgl64.Vec3
			H() *EntityHandle
		})
		if !ok || player.H() == nil || player.GameMode() == GameModeSpectator {
			continue
		}
		area := loader.activeArea(simRadius)
		if area.radius == 0 {
			continue
		}
		players = append(players, inhabitedTimePlayer{pos: player.Position(), area: area})
	}
	w.scratchInhabitedPlayers = players
	for _, ref := range w.activeColumns {
		for _, player := range players {
			area := player.area
			dx, dz := int64(ref.pos[0])-int64(area.pos[0]), int64(ref.pos[1])-int64(area.pos[1])
			if dx*dx+dz*dz > area.radiusSq {
				continue
			}
			chunkX, chunkZ := float64(ref.pos[0])*16+8, float64(ref.pos[1])*16+8
			x, z := player.pos[0]-chunkX, player.pos[2]-chunkZ
			if x*x+z*z < 128*128 {
				ref.col.InhabitedTime++
				break
			}
		}
	}
}

// performNeighbourUpdates performs all block updates that came as a result of a neighbouring block being changed.
func (t ticker) performNeighbourUpdates(tx *Tx) {
	w := tx.World()
	examined, processed, retained := 0, 0, 0
	// Finish updates produced by this chain before a later tick can change its
	// source again. Keep the existing budget and deferred-chunk fairness.
	for examined < len(w.neighbourUpdates) && processed < maxNeighbourUpdatesPerTick {
		update := w.neighbourUpdates[examined]
		examined++
		pos, changedNeighbour := update.pos, update.neighbour
		b, ready := tx.BlockLoaded(pos)
		if !ready {
			if loaded, ready := tx.ChunkState(chunkPosFromBlockPos(pos)); loaded && !ready {
				w.neighbourUpdates[retained] = update
				retained++
			}
			continue
		}
		processed++
		if ticker, ok := b.(NeighbourUpdateTicker); ok {
			ticker.NeighbourUpdateTick(pos, changedNeighbour, tx)
		}
		if liquid, ok := tx.additionalLiquid(pos); ok {
			if ticker, ok := liquid.(NeighbourUpdateTicker); ok {
				ticker.NeighbourUpdateTick(pos, changedNeighbour, tx)
			}
		}
	}
	remaining := len(w.neighbourUpdates) - examined
	copy(w.neighbourUpdates[retained:], w.neighbourUpdates[examined:])
	w.neighbourUpdates = w.neighbourUpdates[:retained+remaining]
	// Move generating entries behind unexamined and newly queued updates without allocating.
	slices.Reverse(w.neighbourUpdates[:retained])
	slices.Reverse(w.neighbourUpdates[retained:])
	slices.Reverse(w.neighbourUpdates)
}

// tickBlocksRandomly executes random block ticks in loaded chunks within range of loaders. Synchronous worlds tick
// every loaded chunk because they have no background loaders.
func (t ticker) tickBlocksRandomly(tx *Tx, loaders []*Loader, tick int64) {
	w := tx.World()
	var (
		r = int32(w.tickRange())
		g randUint4
	)
	if r == 0 {
		// NOP if the simulation distance is 0.
		return
	}
	columns := w.activeColumns
	if w.conf.Synchronous {
		columns = make([]columnRef, 0, len(w.chunks))
		for pos, col := range w.chunks {
			columns = append(columns, columnRef{pos: pos, col: col})
		}
		slices.SortFunc(columns, func(a, b columnRef) int {
			if a.pos[0] < b.pos[0] || a.pos[0] == b.pos[0] && a.pos[1] < b.pos[1] {
				return -1
			}
			if a.pos != b.pos {
				return 1
			}
			return 0
		})
	}
	if len(columns) == 0 {
		return
	}
	randomTickSpeed := w.conf.RandomTickSpeed
	doRandomTicks := randomTickSpeed > 0

	areas := w.scratchLoaderAreas
	if cap(areas) < len(loaders) {
		areas = make([]loaderActiveArea, 0, len(loaders))
	} else {
		areas = areas[:0]
	}
	for _, loader := range loaders {
		areas = append(areas, loader.activeArea(r))
	}
	w.scratchLoaderAreas = areas

	blockEntities := w.scratchBlockEntities[:0]
	randomBlocks := w.scratchRandom[:0]

	if !doRandomTicks {
		for _, ref := range columns {
			if !w.conf.Synchronous && !columnWithinAreas(ref.pos, areas) {
				continue
			}
			c := ref.col
			if c == nil {
				continue
			}
			blockEntities = append(blockEntities, c.tickerBlockEntityPositions()...)
		}
		for _, pos := range blockEntities {
			tb := tx.chunk(chunkPosFromBlockPos(pos)).BlockEntities[pos].(TickerBlock)
			tb.Tick(tick, pos, tx)
		}
		w.scratchLoaderAreas = areas[:0]
		w.scratchRandom = randomBlocks[:0]
		w.scratchBlockEntities = blockEntities[:0]
		return
	}

	minSubY := tx.Range().Min() >> 4

	for _, ref := range columns {
		if !w.conf.Synchronous && !columnWithinAreas(ref.pos, areas) {
			continue
		}
		c := ref.col
		if c == nil {
			continue
		}
		blockEntities = append(blockEntities, c.tickerBlockEntityPositions()...)

		cx, cz := int(ref.pos[0]<<4), int(ref.pos[1]<<4)
		subChunks := c.Sub()
		randomSubIndices := c.randomTickSubChunkIndices(w.conf.Blocks)
		if len(randomSubIndices) == 0 {
			continue
		}

		// We generate up to j random positions for every sub chunk that may contain random-tickable blocks.
		for j := 0; j < randomTickSpeed; j++ {
			x, y, z := g.uint4(w.r), g.uint4(w.r), g.uint4(w.r)

			for _, subIndex := range randomSubIndices {
				sub := subChunks[subIndex]
				// Generally we would want to make sure the block has its block entities, but provided blocks
				// with block entities are generally ticked already, we are safe to assume that blocks
				// implementing the RandomTicker don't rely on additional block entity data.
				if rid := sub.Layers()[0].At(x, y, z); tx.World().conf.Blocks.RandomTickBlock(rid) {
					subY := (subIndex + minSubY) << 4
					randomBlocks = append(randomBlocks, cube.Pos{cx + int(x), subY + int(y), cz + int(z)})

					// Only generate new coordinates if a tickable block was actually found. If not, we can just re-use
					// the coordinates for the next sub chunk.
					x, y, z = g.uint4(w.r), g.uint4(w.r), g.uint4(w.r)
				}
			}
		}
	}

	for _, pos := range randomBlocks {
		if rb, ok := tx.Block(pos).(RandomTicker); ok {
			rb.RandomTick(pos, tx, w.r)
		}
	}
	for _, pos := range blockEntities {
		tb := tx.chunk(chunkPosFromBlockPos(pos)).BlockEntities[pos].(TickerBlock)
		tb.Tick(tick, pos, tx)
	}

	w.scratchLoaderAreas = areas[:0]
	w.scratchRandom = randomBlocks[:0]
	w.scratchBlockEntities = blockEntities[:0]
}

func columnWithinAreas(pos ChunkPos, areas []loaderActiveArea) bool {
	for _, area := range areas {
		dx := pos[0] - area.pos[0]
		if dx > area.radius || dx < -area.radius {
			continue
		}
		dz := pos[1] - area.pos[1]
		if dz > area.radius || dz < -area.radius {
			continue
		}
		dist := int64(dx)*int64(dx) + int64(dz)*int64(dz)
		if dist <= area.radiusSq {
			return true
		}
	}
	return false
}

// tickEntities ticks all entities in the world, making sure they are still located in the correct chunks and
// updating where necessary.
//
// The implementation purposefully separates entities into "active" (chunks with at least one viewer) and
// "sleeping" (chunks currently unseen) cohorts. Only the active cohort is processed every tick. Sleeping
// chunks are serviced on a coarse cadence to avoid spending CPU time on areas of the world that no player can
// currently interact with. This mirrors the behaviour of the vanilla server simulation distance and greatly
// reduces per-tick iteration costs on large worlds while still keeping important counters such as entity age and
// fire timers consistent.
func (t ticker) tickEntities(tx *Tx, tick int64) {
	const sleepMaintenanceInterval = 40 // ~2 seconds between maintenance passes for sleeping chunks.

	w := tx.World()

	lazyMaintenance := tick%sleepMaintenanceInterval == 0

	// We iterate over the cached entity column list to partition entity handles. The references keep track of the
	// originating column so that we can update viewer lists or perform removals without having to search for the
	// owning chunk again later in the tick.
	for _, ref := range w.entityColumns {
		col := ref.col
		if col == nil || len(col.Entities) == 0 {
			continue
		}
		if w.conf.Synchronous || len(col.viewers) > 0 {
			t.tickEntityColumn(tx, tick, ref.pos, col, true)
			continue
		}
		if !lazyMaintenance {
			continue
		}
		t.tickEntityColumn(tx, tick, ref.pos, col, false)
	}
}

func (t ticker) tickEntityColumn(tx *Tx, tick int64, pos ChunkPos, col *Column, active bool) {
	ref := entityChunkRef{col: col, pos: pos}
	for index := 0; index < len(col.Entities); {
		handle := col.Entities[index]
		t.tickEntityHandle(tx, tick, handle, ref, active)
		if index < len(col.Entities) && col.Entities[index] == handle {
			index++
		}
	}
}

func (t ticker) tickEntityHandle(tx *Tx, tick int64, handle *EntityHandle, ref entityChunkRef, active bool) {
	w := tx.World()
	state := handle.state
	if state == nil {
		state = w.entities[handle]
		if state != nil {
			handle.state = state
		}
	}
	if state == nil {
		return
	}
	if state.lastProcessedTick == tick {
		return
	}
	state.lastProcessedTick = tick

	chunkPos := chunkPosFromVec3(handle.data.Pos)
	var entity Entity

	if state.lastTick == 0 {
		state.lastTick = tick
	}
	if active && state.pos == chunkPos {
		state.nextPassiveTick = tick + passiveMaintenanceInterval
		delta := tick - state.lastTick
		if delta <= 1 && state.tickerChecked && !state.isTicker {
			state.lastTick = tick
			return
		}
	}
	if state.pos != chunkPos {
		oldPos := state.pos
		state.pos = chunkPos

		newChunk := tx.chunk(chunkPos)
		newChunk.addEntity(handle)
		newChunk.modified = true
		w.addEntityColumn(chunkPos, newChunk)

		var viewers map[Viewer]struct{}
		if oldPos == ref.pos && ref.col != nil {
			if ref.col.removeEntity(handle) {
				ref.col.modified = true
			}
			if len(ref.col.Entities) == 0 {
				w.removeEntityColumn(ref.pos)
			}
			viewers = ref.col.viewers
		} else if old, ok := w.chunks[oldPos]; ok {
			if old.removeEntity(handle) {
				old.modified = true
			}
			if len(old.Entities) == 0 {
				w.removeEntityColumn(oldPos)
			}
			viewers = old.viewers
		}

		if len(viewers) > 0 || len(newChunk.viewers) > 0 {
			// Entity handles lazily open their backing implementation. We memoise the first load so repeated
			// viewers or ticker calls reuse the same pointer and avoid repeated provider work each tick.
			if entity == nil {
				entity = state.entity(tx, handle)
			}
			ent := entity
			for v := range viewers {
				if _, ok := newChunk.viewers[v]; !ok {
					v.HideEntity(ent)
				}
			}
			for v := range newChunk.viewers {
				if _, ok := viewers[v]; !ok {
					showEntity(ent, v)
				}
			}
		}
	}

	if !active {
		// Sleeping entities are only maintained intermittently. Rather than ticking behavioural logic
		// every frame we only advance bookkeeping values (age, fire) and run clean-up such as despawning
		// expired items. This keeps dormant areas cheap to maintain while ensuring that, once a viewer
		// arrives, the entity state can immediately catch up from the persisted counters.
		if state.nextPassiveTick == 0 {
			state.nextPassiveTick = tick + passiveMaintenanceInterval
		}
		if tick < state.nextPassiveTick {
			return
		}
		if delta := tick - state.lastTick; delta > 0 {
			inc := time.Duration(delta) * (time.Second / 20)
			handle.data.Age += inc
			if handle.data.FireDuration > 0 {
				if inc >= handle.data.FireDuration {
					handle.data.FireDuration = 0
				} else {
					handle.data.FireDuration -= inc
				}
			}
			state.lastTick = tick
		}
		state.nextPassiveTick = tick + passiveMaintenanceInterval
		if state.isItem && handle.data.Age >= 5*time.Minute {
			if entity == nil {
				entity = state.entity(tx, handle)
			}
			if entity != nil {
				ent := entity
				_ = ent.Close()
			}
		}
		return
	}

	if delta := tick - state.lastTick; delta > 1 {
		// We collapsed multiple ticks: apply the same accounting vanilla would have done each frame so
		// behaviours that rely on entity age or fire duration stay in sync even if an entity temporarily left
		// the active area.
		inc := time.Duration(delta-1) * (time.Second / 20)
		handle.data.Age += inc
		if handle.data.FireDuration > 0 {
			if inc >= handle.data.FireDuration {
				handle.data.FireDuration = 0
			} else {
				handle.data.FireDuration -= inc
			}
		}
	}
	state.lastTick = tick
	state.nextPassiveTick = tick + passiveMaintenanceInterval
	if !state.tickerChecked || state.isTicker {
		// We must rebind the entity to the current transaction whenever it is about to tick. The bound
		// Tx expires at the end of each frame, so behaviours that capture the Tx (fire, name tags, etc.) rely
		// on bindTx being invoked every time we service the entity. The previous implementation called
		// state.entity each tick; keep that contract so that helpers never observe a stale, closed Tx.
		if entity == nil {
			entity = state.entity(tx, handle)
		}
	}
	if state.isTicker && state.ticker != nil {
		state.ticker.Tick(tx, tick)
	}
}

// randUint4 is a structure used to generate random uint4s.
type randUint4 struct {
	x uint64
	n uint8
}

// uint4 returns a random uint4.
func (g *randUint4) uint4(r *rand.Rand) uint8 {
	if g.n == 0 {
		g.x = r.Uint64()
		g.n = 16
	}
	val := g.x & 0b1111

	g.x >>= 4
	g.n--
	return uint8(val)
}

// scheduledTickQueue implements a queue for scheduled block updates. Scheduled
// block updates are both position and block type specific.
type scheduledTickQueue struct {
	ticks         []scheduledTick
	scheduledTicks map[scheduledTickIndex]int64
	pendingTicks   []scheduledTick
	processing     bool
	currentTick   int64
}

type scheduledTick struct {
	pos   cube.Pos
	b     Block
	btype string
	t     int64
}

type scheduledTickIndex struct {
	pos      cube.Pos
	typeName string
}

// newScheduledTickQueue creates a queue for scheduled block ticks.
func newScheduledTickQueue(tick int64) *scheduledTickQueue {
	return &scheduledTickQueue{scheduledTicks: make(map[scheduledTickIndex]int64), currentTick: tick}
}

// tick processes scheduled ticks, calling ScheduledTicker.ScheduledTick for any
// block update that is scheduled for the tick passed, and removing it from the
// queue.
func (queue *scheduledTickQueue) tick(tx *Tx, tick int64) {
	queue.currentTick = tick
	if len(queue.ticks) == 0 {
		return
	}

	w := tx.World()
	ticks := queue.ticks
	queue.processing = true
	dst := 0
	for i := range ticks {
		t := ticks[i]
		if t.t > tick {
			ticks[dst] = t
			dst++
			continue
		}
		index := scheduledTickIndex{pos: t.pos, typeName: t.btype}
		if queue.scheduledTicks[index] == t.t {
			delete(queue.scheduledTicks, index)
		}
		b := tx.Block(t.pos)
		if ticker, ok := b.(ScheduledTicker); ok && scheduledTickType(w.conf.Blocks, b) == t.btype {
			ticker.ScheduledTick(t.pos, tx, w.r)
		} else if liquid, ok := tx.additionalLiquid(t.pos); ok && scheduledTickType(w.conf.Blocks, liquid) == t.btype {
			if ticker, ok := liquid.(ScheduledTicker); ok {
				ticker.ScheduledTick(t.pos, tx, w.r)
			}
		}
	}
	queue.processing = false
	queue.ticks = append(ticks[:dst], queue.pendingTicks...)
	queue.pendingTicks = queue.pendingTicks[:0]
}

// schedule schedules a block update at the position passed for the block type
// passed after a specific delay. Only the first pending update for a block type
// at a position is kept, matching the source tick queue's unique-tick behavior.
func (queue *scheduledTickQueue) schedule(br BlockRegistry, pos cube.Pos, b Block, delay time.Duration) {
	resTick := queue.currentTick + int64(max(delay/(time.Second/20), 1))
	typeName := scheduledTickType(br, b)
	index := scheduledTickIndex{pos: pos, typeName: typeName}
	if _, ok := queue.scheduledTicks[index]; ok {
		return
	}
	queue.scheduledTicks[index] = resTick
	t := scheduledTick{pos: pos, t: resTick, b: b, btype: typeName}
	if queue.processing {
		queue.pendingTicks = append(queue.pendingTicks, t)
	} else {
		queue.ticks = append(queue.ticks, t)
	}
}

// scheduledTickType returns the Java block type represented by a native block
// state. Scheduled ticks identify a block type, not an individual state.
func scheduledTickType(registry BlockRegistry, b Block) string {
	rid := registry.BlockRuntimeID(b)
	name, properties, ok := registry.RuntimeIDToState(rid)
	if !ok {
		panic("scheduled tick: registry cannot resolve block runtime ID")
	}
	return javaBlockTypeForNative(name, properties)
}

// fromChunk returns all scheduled ticks positioned within a ChunkPos.
func (queue *scheduledTickQueue) fromChunk(pos ChunkPos) []scheduledTick {
	m := make([]scheduledTick, 0, 8)
	for _, t := range queue.ticks {
		if pos == chunkPosFromBlockPos(t.pos) {
			m = append(m, t)
		}
	}
	return m
}

// removeChunk removes all scheduled ticks positioned within a ChunkPos.
func (queue *scheduledTickQueue) removeChunk(pos ChunkPos) {
	queue.ticks = slices.DeleteFunc(queue.ticks, func(tick scheduledTick) bool {
		return chunkPosFromBlockPos(tick.pos) == pos
	})
	queue.pendingTicks = slices.DeleteFunc(queue.pendingTicks, func(tick scheduledTick) bool {
		return chunkPosFromBlockPos(tick.pos) == pos
	})
	for index := range queue.scheduledTicks {
		if chunkPosFromBlockPos(index.pos) == pos {
			delete(queue.scheduledTicks, index)
		}
	}
}

// add adds saved scheduled ticks to the queue, retaining the first tick for
// each position and type just like schedule.
func (queue *scheduledTickQueue) add(ticks []scheduledTick) {
	for _, t := range ticks {
		index := scheduledTickIndex{pos: t.pos, typeName: t.btype}
		if _, exists := queue.scheduledTicks[index]; exists {
			continue
		}
		queue.scheduledTicks[index] = t.t
		queue.ticks = append(queue.ticks, t)
	}
}
