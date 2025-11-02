package world

import (
	"maps"
	"math"
	"math/rand/v2"
	"slices"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/sliceutil"
)

// ticker implements World ticking methods.
type ticker struct {
	interval time.Duration
}

type entityChunkRef struct {
	col *Column
	pos ChunkPos
}

func clearEntityRefMap(m map[*EntityHandle]entityChunkRef) {
	for k := range m {
		delete(m, k)
	}
}

const (
	tpsSampleSize              = 20
	tpsWarningThreshold        = 19.0
	passiveMaintenanceInterval = 80
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
			<-w.Exec(t.tick)
		case <-w.closing:
			// World is being closed: Stop ticking and get rid of a task.
			w.running.Done()
			return
		}
	}
}

// tick performs a tick on the World and updates the time, weather, blocks and
// entities that require updates.
func (t ticker) tick(tx *Tx) {
	viewers, loaders := tx.World().allViewers()
	w := tx.World()
	defer w.releaseViewers(viewers)

	w.set.Lock()
	if s := w.set.Spawn; s[1] > tx.Range()[1] {
		// Vanilla will set the spawn position's Y value to max to indicate that
		// the player should spawn at the highest position in the world.
		w.set.Spawn[1] = w.highestObstructingBlock(s[0], s[2]) + 1
	}
    if len(viewers) == 0 && w.set.CurrentTick != 0 && len(w.entities) == 0 {
        // Don't continue ticking if the world has no viewers and no active entities.
        // This allows dimensions like the End to keep updating (e.g. liquid flow)
        // while a player entity is present even if a viewer wasn't registered.
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
	}

	rain, thunder, tick, tim := w.set.Raining, w.set.Thundering && w.set.Raining, w.set.CurrentTick, int(w.set.Time)
	timeCycle := w.set.TimeCycle

	tryAdvanceDay := false
	if w.set.RequiredSleepTicks > 0 {
		w.set.RequiredSleepTicks--
		tryAdvanceDay = w.set.RequiredSleepTicks <= 0
	}

	w.set.Unlock()

	if tryAdvanceDay {
		t.tryAdvanceDay(tx, timeCycle)
	}

	if tick%20 == 0 {
		for _, viewer := range viewers {
			if w.Dimension().TimeCycle() {
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

	t.tickEntities(tx, tick)
	w.scheduledUpdates.tick(tx, tick)
	t.tickBlocksRandomly(tx, loaders, tick)
	t.performNeighbourUpdates(tx)
}

// performNeighbourUpdates performs all block updates that came as a result of a neighbouring block being changed.
func (t ticker) performNeighbourUpdates(tx *Tx) {
	w := tx.World()
	updates := w.neighbourUpdates
	limit := len(updates)
	for i := 0; i < limit; i++ {
		update := updates[i]
		pos, changedNeighbour := update.pos, update.neighbour
		if ticker, ok := tx.Block(pos).(NeighbourUpdateTicker); ok {
			ticker.NeighbourUpdateTick(pos, changedNeighbour, tx)
		}
		if liquid, ok := tx.World().additionalLiquid(pos); ok {
			if ticker, ok := liquid.(NeighbourUpdateTicker); ok {
				ticker.NeighbourUpdateTick(pos, changedNeighbour, tx)
			}
		}
	}
	if len(w.neighbourUpdates) > limit {
		remaining := w.neighbourUpdates[limit:]
		copy(w.neighbourUpdates, remaining)
		w.neighbourUpdates = w.neighbourUpdates[:len(remaining)]
		return
	}
	w.neighbourUpdates = w.neighbourUpdates[:0]
}

// tickBlocksRandomly executes random block ticks in each sub chunk in the world that has at least one viewer
// registered from the viewers passed.
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
	if len(w.activeColumns) == 0 {
		return
	}

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

	for _, ref := range w.activeColumns {
		if !columnWithinAreas(ref.pos, areas) {
			continue
		}
		c := ref.col
		if c == nil {
			continue
		}
		for be := range c.BlockEntities {
			blockEntities = append(blockEntities, be)
		}

		cx, cz := int(ref.pos[0]<<4), int(ref.pos[1]<<4)

		// We generate up to j random positions for every sub chunk.
		for j := 0; j < w.conf.RandomTickSpeed; j++ {
			x, y, z := g.uint4(w.r), g.uint4(w.r), g.uint4(w.r)

			for i, sub := range c.Sub() {
				if sub.Empty() {
					// SubChunk is empty, so skip it right away.
					continue
				}
				// Generally we would want to make sure the block has its block entities, but provided blocks
				// with block entities are generally ticked already, we are safe to assume that blocks
				// implementing the RandomTicker don't rely on additional block entity data.
				if rid := sub.Layers()[0].At(x, y, z); randomTickBlocks[rid] {
					subY := (i + (tx.Range().Min() >> 4)) << 4
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
		if tb, ok := tx.Block(pos).(TickerBlock); ok {
			tb.Tick(tick, pos, tx)
		}
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

	active := w.scratchActiveEntities
	if cap(active) == 0 {
		active = make([]*EntityHandle, 0, 64)
	}
	active = active[:0]

	sleeping := w.scratchSleepingEntities
	if cap(sleeping) == 0 {
		sleeping = make([]*EntityHandle, 0, 64)
	}
	sleeping = sleeping[:0]

	activeChunks := w.scratchActiveRefs
	if activeChunks == nil {
		activeChunks = make(map[*EntityHandle]entityChunkRef)
		w.scratchActiveRefs = activeChunks
	} else {
		clearEntityRefMap(activeChunks)
	}

	sleepingChunks := w.scratchSleepingRefs
	if sleepingChunks == nil {
		sleepingChunks = make(map[*EntityHandle]entityChunkRef)
		w.scratchSleepingRefs = sleepingChunks
	} else {
		clearEntityRefMap(sleepingChunks)
	}

	// We iterate over the cached entity column list to partition entity handles. The maps keep track of the
	// originating column so that we can update viewer lists or perform removals without having to search for the
	// owning chunk again later in the tick.

	for _, ref := range w.entityColumns {
		col := ref.col
		if col == nil || len(col.Entities) == 0 {
			continue
		}
		if len(col.viewers) > 0 {
			for _, handle := range col.Entities {
				active = append(active, handle)
				activeChunks[handle] = entityChunkRef{col: col, pos: ref.pos}
			}
			continue
		}
		if !lazyMaintenance {
			continue
		}
		for _, handle := range col.Entities {
			sleeping = append(sleeping, handle)
			sleepingChunks[handle] = entityChunkRef{col: col, pos: ref.pos}
		}
	}

	for _, handle := range active {
		t.tickEntityHandle(tx, tick, handle, activeChunks[handle], true)
	}
	if lazyMaintenance {
		for _, handle := range sleeping {
			t.tickEntityHandle(tx, tick, handle, sleepingChunks[handle], false)
		}
	}

	w.scratchActiveEntities = active[:0]
	w.scratchSleepingEntities = sleeping[:0]
	clearEntityRefMap(activeChunks)
	clearEntityRefMap(sleepingChunks)
}

func (t ticker) tickEntityHandle(tx *Tx, tick int64, handle *EntityHandle, ref entityChunkRef, active bool) {
	w := tx.World()
	state := w.entities[handle]
	if state == nil {
		return
	}

	chunkPos := chunkPosFromVec3(handle.data.Pos)

	var (
		entity       Entity
		entityLoaded bool
	)
	loadEntity := func() Entity {
		// Entity handles lazily open their backing implementation. We memoise the first load so repeated
		// viewers or ticker calls reuse the same pointer and avoid repeated provider work each tick.
		if !entityLoaded {
			entity = state.entity(tx, handle)
			entityLoaded = true
		}
		return entity
	}

	if state.lastTick == 0 {
		state.lastTick = tick
	}
	if state.pos != chunkPos {
		oldPos := state.pos
		state.pos = chunkPos

		newChunk := w.chunk(chunkPos)
		newChunk.Entities = append(newChunk.Entities, handle)
		newChunk.modified = true
		w.addEntityColumn(chunkPos, newChunk)

		var viewers map[Viewer]struct{}
		if oldPos == ref.pos && ref.col != nil {
			ref.col.Entities = sliceutil.DeleteVal(ref.col.Entities, handle)
			ref.col.modified = true
			if len(ref.col.Entities) == 0 {
				w.removeEntityColumn(ref.pos)
			}
			viewers = ref.col.viewers
		} else if old, ok := w.chunks[oldPos]; ok {
			old.Entities = sliceutil.DeleteVal(old.Entities, handle)
			old.modified = true
			if len(old.Entities) == 0 {
				w.removeEntityColumn(oldPos)
			}
			viewers = old.viewers
		}

		if len(viewers) > 0 || len(newChunk.viewers) > 0 {
			ent := loadEntity()
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
			if ent := loadEntity(); ent != nil {
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
		loadEntity()
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
	furthestTicks map[scheduledTickIndex]int64
	currentTick   int64
}

type scheduledTick struct {
	pos   cube.Pos
	b     Block
	bhash uint64
	t     int64
}

type scheduledTickIndex struct {
	pos  cube.Pos
	hash uint64
}

// newScheduledTickQueue creates a queue for scheduled block ticks.
func newScheduledTickQueue(tick int64) *scheduledTickQueue {
	return &scheduledTickQueue{furthestTicks: make(map[scheduledTickIndex]int64), currentTick: tick}
}

// tick processes scheduled ticks, calling ScheduledTicker.ScheduledTick for any
// block update that is scheduled for the tick passed, and removing it from the
// queue.
func (queue *scheduledTickQueue) tick(tx *Tx, tick int64) {
	queue.currentTick = tick

	w := tx.World()
	for _, t := range queue.ticks {
		if t.t > tick {
			continue
		}
		b := tx.Block(t.pos)
		if ticker, ok := b.(ScheduledTicker); ok && BlockHash(b) == t.bhash {
			ticker.ScheduledTick(t.pos, tx, w.r)
		} else if liquid, ok := tx.World().additionalLiquid(t.pos); ok && BlockHash(liquid) == t.bhash {
			if ticker, ok := liquid.(ScheduledTicker); ok {
				ticker.ScheduledTick(t.pos, tx, w.r)
			}
		}
	}

	// Clear scheduled ticks that were processed from the queue.
	queue.ticks = slices.DeleteFunc(queue.ticks, func(t scheduledTick) bool {
		return t.t <= tick
	})
	maps.DeleteFunc(queue.furthestTicks, func(index scheduledTickIndex, t int64) bool {
		return t <= tick
	})
}

// schedule schedules a block update at the position passed for the block type
// passed after a specific delay. A block update is only scheduled if no block
// update with the same position and block type is already scheduled at a later
// time than the newly scheduled update.
func (queue *scheduledTickQueue) schedule(pos cube.Pos, b Block, delay time.Duration) {
	resTick := queue.currentTick + int64(max(delay/(time.Second/20), 1))
	index := scheduledTickIndex{pos: pos, hash: BlockHash(b)}
	if t, ok := queue.furthestTicks[index]; ok && t >= resTick {
		// Already have a tick scheduled for this position that will occur after
		// the delay passed. Block updates can only be scheduled if they are
		// after any currently scheduled updates.
		return
	}
	queue.furthestTicks[index] = resTick
	queue.ticks = append(queue.ticks, scheduledTick{pos: pos, t: resTick, b: b, bhash: index.hash})
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
	// Also remove any furthest tick entries that belong to this chunk to avoid
	// retaining references after the chunk is closed.
	maps.DeleteFunc(queue.furthestTicks, func(index scheduledTickIndex, _ int64) bool {
		return chunkPosFromBlockPos(index.pos) == pos
	})
}

// add adds a slice of scheduled ticks to the queue. It assumes no duplicate
// ticks are present in the slice.
func (queue *scheduledTickQueue) add(ticks []scheduledTick) {
	queue.ticks = append(queue.ticks, ticks...)
	for _, t := range ticks {
		index := scheduledTickIndex{pos: t.pos, hash: t.bhash}
		if existing, ok := queue.furthestTicks[index]; ok {
			// Make sure we find the furthest tick for each of the ticks added.
			// Some ticks may have the same block and position, in which case we
			// need to set the furthest tick.
			queue.furthestTicks[index] = max(existing, t.t)
		}
	}
}
