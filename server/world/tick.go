package world

import (
	"cmp"
	"math"
	"math/rand/v2"
	"runtime/debug"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/parallel"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/go-gl/mathgl/mgl64"
)

// ticker implements World ticking methods.
type ticker struct {
	interval time.Duration
}

type entityChunkRef struct {
	col *Column
	pos ChunkPos
	// waiting is set while chunks within two chunks of pos are still
	// generating, so that only players in the chunk tick.
	waiting bool
}

const (
	tpsSampleSize              = 20
	tpsWarningThreshold        = 19.0
	passiveMaintenanceInterval = 80
	maxNeighbourUpdatesPerTick = 16384
	// overloadedThreshold and overloadWarningInterval are Java's
	// MinecraftServer OVERLOADED_THRESHOLD_NANOS and
	// OVERLOADED_WARNING_INTERVAL_NANOS.
	overloadedThreshold     = time.Second
	overloadWarningInterval = 10 * time.Second
)

// tickLoop starts ticking the World 20 times every second, updating all
// entities, blocks and other features such as the time and weather of the
// world, as required. Like Java's MinecraftServer.runServer, ticks keep a
// fixed schedule: each is due one interval after the one before, however
// long ticking took, and ticks that fell behind run back to back until the
// world caught up.
func (t ticker) tickLoop(w *World) {
	next := time.Now().Add(t.interval)
	tc := time.NewTimer(t.interval)
	defer tc.Stop()
	lastTick := time.Now()
	var (
		durationSum         time.Duration
		ticksCount          int
		warned              bool
		lastOverloadWarning time.Time
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
			// next is when this tick was due. Like Java, a world more than a
			// second and twenty ticks behind skips the ticks it missed, at
			// most once per overload warning, then schedules the next tick.
			sprinting := w.sprintTicks.Load() > 0
			interval := t.interval
			if rate := w.TickRate(); rate > 0 {
				interval = time.Duration(float64(time.Second) / rate)
			}
			if sprinting {
				next, lastOverloadWarning = tickStart, tickStart
				interval = time.Millisecond
			} else if behind := tickStart.Sub(next); behind > overloadedThreshold+20*interval && next.Sub(lastOverloadWarning) >= overloadWarningInterval+100*interval {
				ticks := behind / interval
				w.conf.Log.Warn("Can't keep up! Is the server overloaded?", "behind", behind.Round(time.Millisecond), "ticks", int64(ticks))
				next = next.Add(ticks * interval)
				lastOverloadWarning = next
			}
			next = next.Add(interval)
			<-w.exec(t.tick)
			tc.Reset(time.Until(next))
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
	start := time.Now()
	defer func() { tx.World().tickTimes.record(time.Since(start)) }()
	viewers, loaders := tx.World().allViewers()
	w := tx.World()
	defer w.flushLight()
	defer w.releaseViewers(viewers)
	for _, v := range viewers {
		if b, ok := v.(BatchViewer); ok {
			b.BeginBatch()
		}
	}
	defer func() {
		for _, v := range viewers {
			if b, ok := v.(BatchViewer); ok {
				b.EndBatch()
			}
		}
	}()
	w.focusGeneration(loaders)
	w.updateEntityTickingAreas(loaders)
	w.loadForcedChunks()
	for pos := range w.forcedChunks {
		w.loadForcedChunk(pos)
		if c := w.chunks[pos]; c != nil && c.Ready() {
			tx.chunk(pos)
		}
	}
	runStep := w.tickFrozen.Load() && w.stepTicks.Load() > 0
	if w.tickFrozen.Load() && !runStep {
		t.tickFrozenPlayers(tx, w.CurrentTick())
		return
	}

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
	if len(viewers) == 0 && w.set.CurrentTick != 0 && !w.conf.Synchronous && len(w.forcedChunks) == 0 && w.sprintTicks.Load() == 0 && !runStep {
		// Don't continue ticking if no viewers or forced chunks are in the world.
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
	if runStep {
		w.stepTicks.Add(-1)
	}
	w.border.tick()
	rain, thunder, tick, tim := w.set.Raining, w.set.Thundering && w.set.Raining, w.currentTickLocked(), int(w.set.Time)
	timeCycle := w.set.TimeCycle

	tryAdvanceDay := false
	if w.set.RequiredSleepTicks > 0 {
		w.set.RequiredSleepTicks--
		tryAdvanceDay = w.set.RequiredSleepTicks <= 0
	}
	w.set.Unlock()
	// Both Java registries share the same game-time clock, even though the
	// block queue drains before the fluid queue.
	w.scheduledBlockTicks.currentTick = tick
	w.scheduledFluidTicks.currentTick = tick
	t.tickInhabitedTime(w, loaders)
	t.promoteGeneration(tx, loaders)

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
	if !w.tickFrozen.Load() || runStep {
		if thunder {
			w.tickLightning(tx)
		}
		if w.dragonFight != nil {
			w.dragonFight.Tick(tx)
		}
		t.tickEntities(tx, tick)
		if collide := w.EntityRegistry().Config().EntityCollisions; collide != nil {
			collide(tx)
		}
		if spawnTick := w.EntityRegistry().Config().NaturalSpawnTick; spawnTick != nil {
			spawnTick(tx)
		}
		tx.TickRaids()
		w.scheduledBlockTicks.tick(tx, tick, loaders...)
		w.scheduledFluidTicks.tick(tx, tick, loaders...)
		t.tickBlocksRandomly(tx, loaders, tick)
		t.performNeighbourUpdates(tx)
		w.redstone.tick(tx, tick)
	}
	if w.sprintTicks.Load() > 0 {
		w.sprintTicks.Add(-1)
	}

}

// tickFrozenPlayers preserves player-side processing while suppressing world simulation.
func (t ticker) tickFrozenPlayers(tx *Tx, tick int64) {
	w := tx.World()
	for _, ref := range w.entityColumns {
		col := ref.col
		if col == nil || !col.Ready() {
			continue
		}
		for index := 0; index < len(col.Entities); {
			handle := col.Entities[index]
			if handle.Type().EncodeEntity() == "minecraft:player" {
				t.tickEntityHandle(tx, tick, handle, entityChunkRef{col: col, pos: ref.pos}, true)
			}
			if index < len(col.Entities) && col.Entities[index] == handle {
				index++
			}
		}
	}
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
	// Charge deferred entries to the same work budget: a generating chunk must
	// not turn a bounded batch into a scan of the entire backlog.
	deferred := w.deferredNeighbours[:0]
	for examined := 0; examined < maxNeighbourUpdatesPerTick && w.neighbourUpdates.count != 0; examined++ {
		update := w.neighbourUpdates.pop()
		pos, changedNeighbour := update.pos, update.neighbour
		b, ready := tx.BlockLoaded(pos)
		if !ready {
			if loaded, ready := tx.ChunkState(chunkPosFromBlockPos(pos)); loaded && !ready {
				deferred = append(deferred, update)
			}
			continue
		}
		if ticker, ok := b.(NeighbourUpdateTicker); ok {
			ticker.NeighbourUpdateTick(pos, changedNeighbour, tx)
		}
		if liquid, ok := tx.additionalLiquid(pos); ok {
			if ticker, ok := liquid.(NeighbourUpdateTicker); ok {
				ticker.NeighbourUpdateTick(pos, changedNeighbour, tx)
			}
		}
	}
	// Leave unexamined and newly produced work ahead of generating entries.
	for _, update := range deferred {
		w.neighbourUpdates.push(update)
	}
	w.deferredNeighbours = deferred[:0]
}

// tickBlocksRandomly executes random block ticks in loaded chunks within range of loaders. Synchronous worlds tick
// every loaded chunk because they have no background loaders.
func (t ticker) tickBlocksRandomly(tx *Tx, loaders []*Loader, tick int64) {
	w := tx.World()
	var (
		r = int32(w.tickRange())
		g randUint4
	)
	if r == 0 && len(w.forcedChunks) == 0 {
		// NOP if the simulation distance is 0 and no forced chunks exist.
		return
	}
	columns := w.activeColumns
	if w.conf.Synchronous || len(w.forcedChunks) != 0 {
		columns = make([]columnRef, 0, len(w.chunks))
		for pos, col := range w.chunks {
			if w.conf.Synchronous {
				columns = append(columns, columnRef{pos: pos, col: col})
			} else if _, forced := w.forcedChunks[pos]; forced {
				columns = append(columns, columnRef{pos: pos, col: col})
			}
		}
		if !w.conf.Synchronous {
			for _, ref := range w.activeColumns {
				if _, forced := w.forcedChunks[ref.pos]; !forced {
					columns = append(columns, ref)
				}
			}
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

	areas := w.scratchLoaderAreas[:0]
	if cap(areas) < len(loaders)+len(w.forcedChunks) {
		areas = make([]loaderActiveArea, 0, len(loaders)+len(w.forcedChunks))
	}
	for _, loader := range loaders {
		areas = append(areas, loader.activeArea(r))
	}
	for pos := range w.forcedChunks {
		areas = append(areas, loaderActiveArea{pos: pos, radius: 0})
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
			if !w.blockTickingReady(ref.pos, c) {
				continue
			}
			blockEntities = append(blockEntities, c.tickerBlockEntityPositions()...)
			w.tickPrecipitation(tx, ref.pos)
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
		if !w.blockTickingReady(ref.pos, c) {
			continue
		}
		blockEntities = append(blockEntities, c.tickerBlockEntityPositions()...)
		w.tickPrecipitation(tx, ref.pos)

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

// updateEntityTickingAreas stamps the columns whose entities tick this
// tick: those within the simulation distance of the world of one of the
// loaders passed, as Java ticks the entities of the chunks within the
// simulation distance of players, and those forced to load. The entities of
// a column loaded later tick from the next tick on.
func (w *World) updateEntityTickingAreas(loaders []*Loader) {
	w.entityTickingStamp++
	if w.entityTickingSeen == nil {
		w.entityTickingSeen = make(map[loaderActiveArea]struct{})
	}
	clear(w.entityTickingSeen)
	r := int32(w.tickRange())
	for _, loader := range loaders {
		w.stampEntityTicking(loader.activeArea(r))
	}
	w.forcedChunksMu.RLock()
	for pos := range w.forcedChunks {
		w.stampEntityTicking(loaderActiveArea{pos: pos})
	}
	w.forcedChunksMu.RUnlock()
}

// stampEntityTicking stamps the loaded columns within area.
func (w *World) stampEntityTicking(area loaderActiveArea) {
	if _, ok := w.entityTickingSeen[area]; ok {
		return
	}
	w.entityTickingSeen[area] = struct{}{}
	for dx := -area.radius; dx <= area.radius; dx++ {
		for dz := -area.radius; dz <= area.radius; dz++ {
			if int64(dx)*int64(dx)+int64(dz)*int64(dz) > area.radiusSq {
				continue
			}
			if c, ok := w.chunks[ChunkPos{area.pos[0] + dx, area.pos[1] + dz}]; ok {
				c.entityTickingStamp = w.entityTickingStamp + 1
			}
		}
	}
}

// entitiesTick reports whether the entities of col tick this tick. Those
// of every column of a synchronous world do.
func (w *World) entitiesTick(col *Column) bool {
	return w.conf.Synchronous || col.entityTickingStamp == w.entityTickingStamp+1
}

// entitiesTickAt reports whether the entities of the chunk at pos tick this
// tick.
func (w *World) entitiesTickAt(pos ChunkPos) bool {
	if w.conf.Synchronous {
		return true
	}
	c, ok := w.chunks[pos]
	return ok && w.entitiesTick(c)
}

// tickEntities ticks all entities in the world, making sure they are still located in the correct chunks and
// updating where necessary.
//
// The implementation purposefully separates entities into "active" (chunks within the simulation distance of a
// loader) and "sleeping" (chunks further away) cohorts. Only the active cohort is processed every tick. Sleeping
// chunks are serviced on a coarse cadence to avoid spending CPU time on areas of the world that no player can
// currently interact with. This mirrors the behaviour of the vanilla server simulation distance and greatly
// reduces per-tick iteration costs on large worlds while still keeping important counters such as entity age and
// fire timers consistent.
func (t ticker) tickEntities(tx *Tx, tick int64) {
	const sleepMaintenanceInterval = 40 // ~2 seconds between maintenance passes for sleeping chunks.

	w := tx.World()
	t.prefetchEntities(tx)

	lazyMaintenance := tick%sleepMaintenanceInterval == 0

	if w.conf.Regions {
		// The columns ticking actively tick in regions, those sleeping on the
		// owner afterwards.
		active := w.scratchRegionRefs[:0]
		for _, ref := range w.entityColumns {
			if col := ref.col; col != nil && len(col.Entities) > 0 && col.Ready() && w.entitiesTick(col) {
				active = append(active, ref)
			}
		}
		ticked := t.tickRegions(tx, tick, active)
		clear(active)
		w.scratchRegionRefs = active[:0]
		if ticked {
			if lazyMaintenance {
				for _, ref := range slices.Clone(w.entityColumns) {
					if col := ref.col; col != nil && len(col.Entities) > 0 && col.Ready() && !w.entitiesTick(col) {
						t.tickEntityColumn(tx, tick, ref.pos, col, false)
					}
				}
			}
			return
		}
	}

	// We iterate over the cached entity column list to partition entity handles. The references keep track of the
	// originating column so that we can update viewer lists or perform removals without having to search for the
	// owning chunk again later in the tick.
	for _, ref := range w.entityColumns {
		col := ref.col
		if col == nil || len(col.Entities) == 0 || !col.Ready() {
			continue
		}
		if w.entitiesTick(col) {
			t.tickEntityColumn(tx, tick, ref.pos, col, true)
			continue
		}
		if !lazyMaintenance {
			continue
		}
		t.tickEntityColumn(tx, tick, ref.pos, col, false)
	}
}

// prefetchMinimum is the fewest entities the parallel phase before the
// entity ticks runs for: below it, spreading the work costs more than it
// saves.
const prefetchMinimum = 64

// prefetchEntities runs the parallel phase before the entities tick: each
// entity about to tick actively that implements Prefetcher reads part of
// what its tick will read, on all cores, while nothing writes to the world.
// The entities are opened on the owner first, as opening binds them to tx.
func (t ticker) prefetchEntities(tx *Tx) {
	w := tx.World()
	items := w.scratchPrefetch[:0]
	for _, ref := range w.entityColumns {
		col := ref.col
		if col == nil || len(col.Entities) == 0 || !col.Ready() || !w.entitiesTick(col) || !w.entityTickingReady(ref.pos) {
			continue
		}
		for _, handle := range col.Entities {
			state := handle.state
			if state == nil {
				continue
			}
			if p, ok := state.entity(tx, handle).(Prefetcher); ok {
				items = append(items, prefetchItem{e: state.ent, p: p})
			}
		}
	}
	if len(items) >= prefetchMinimum {
		views := w.scratchViews[:0]
		w.growWorkerLocals()
		for worker := range parallel.Workers() {
			views = append(views, ReadView{w: w, locals: w.workerLocals[worker]})
		}
		var failed atomic.Pointer[parallel.Panic]
		parallel.ForWorkers(len(items), 16, func(worker, i int) {
			v := &views[worker]
			v.Reset()
			defer func() {
				// Prefetching only spares the tick work: an entity whose
				// prefetch fails reads everything in its tick instead.
				if r := recover(); r != nil {
					failed.CompareAndSwap(nil, &parallel.Panic{Value: r, Stack: debug.Stack()})
				}
			}()
			items[i].p.Prefetch(items[i].e, v)
		})
		if p := failed.Load(); p != nil {
			w.conf.Log.Error("prefetch entity: panic", "panic", p.Value, "stack", string(p.Stack))
		}
		w.scratchViews = views[:0]
	}
	clear(items)
	w.scratchPrefetch = items[:0]
}

// prefetchItem is an entity prefetching in the parallel phase.
type prefetchItem struct {
	e Entity
	p Prefetcher
}

func (t ticker) tickEntityColumn(tx *Tx, tick int64, pos ChunkPos, col *Column, active bool) {
	ref := entityChunkRef{col: col, pos: pos, waiting: active && !tx.World().entityTickingReady(pos)}
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
		// The entity left the world earlier this tick, as an item merged
		// into another one does, while its column was being ticked.
		return
	}
	if state.lastProcessedTick == tick {
		return
	}
	state.lastProcessedTick = tick
	// Java ticks the vehicle before its passengers. Marking first also stops
	// malformed cyclic saved links from recursing indefinitely.
	if active && handle.data.Riding != nil {
		if vehicle := handle.data.Riding; tx.entityInWorld(vehicle) {
			vehicleState := vehicle.state
			vehicleRef := entityChunkRef{pos: vehicleState.pos, col: w.chunks[vehicleState.pos], waiting: !w.entityTickingReady(vehicleState.pos)}
			t.tickEntityHandle(tx, tick, vehicle, vehicleRef, true)
		}
	}

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
		var moved bool
		entity, moved = w.moveEntityChunk(tx, handle, state, chunkPos, ref)
		if c := w.chunks[chunkPos]; !moved || c == nil || !c.Ready() {
			// As in Java, an entity in a chunk that is not loaded yet, like a
			// player teleported into ungenerated terrain, waits for it.
			return
		}
	}
	waiting := ref.waiting
	if chunkPos != ref.pos {
		waiting = active && !w.entityTickingReady(chunkPos)
	}
	if waiting && handle.t.EncodeEntity() != "minecraft:player" {
		// Java ticks the entities of a chunk only once the chunks within two
		// chunks of it are generated: until then they keep still and do not
		// age, and never stall the tick reading a chunk still generating.
		state.lastTick = tick
		return
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

// moveEntityChunk moves the entity from the chunk it is listed in, ref if
// that is its listed chunk, to chunkPos and shows or hides it to the viewers
// that gain or lose it. It returns the entity if it had to open it. The new
// chunk is loaded or queued for generation without waiting for it.
//
// In a region phase, an entity moving to a chunk the region does not own or
// that is not loaded is moved by the owner after the phase instead, and
// moveEntityChunk reports it did not move it.
func (w *World) moveEntityChunk(tx *Tx, handle *EntityHandle, state *entityState, chunkPos ChunkPos, ref entityChunkRef) (Entity, bool) {
	if r := tx.region; r != nil {
		tx.lockShared()
		defer tx.unlockShared()
		if _, ok := r.loaded(w, chunkPos); !ok {
			r.deferred = append(r.deferred, func(tx *Tx) {
				if handle.state != state || handle.w != w {
					return
				}
				if now := chunkPosFromVec3(handle.data.Pos); now != state.pos {
					w.moveEntityChunk(tx, handle, state, now, entityChunkRef{pos: state.pos, col: w.chunks[state.pos]})
				}
			})
			return nil, false
		}
	}
	oldPos := state.pos
	state.pos = chunkPos

	newChunk, ok := w.chunks[chunkPos]
	if !ok {
		var err error
		if newChunk, err = w.loadChunk(chunkPos); err != nil {
			w.conf.Log.Error("load chunk: "+err.Error(), "X", chunkPos[0], "Z", chunkPos[1])
		}
	}
	newChunk.addEntity(handle)
	if newChunk.Ready() {
		// The generator worker owns the flag of a chunk it is still filling and
		// marks it modified itself.
		newChunk.modified = true
	}
	w.addEntityColumn(chunkPos, newChunk)

	var viewers map[Viewer]struct{}
	if oldPos == ref.pos && ref.col != nil {
		if ref.col.removeEntity(handle) && ref.col.Ready() {
			ref.col.modified = true
		}
		if len(ref.col.Entities) == 0 {
			w.removeEntityColumn(ref.pos)
		}
		viewers = ref.col.viewers
	} else if old, ok := w.chunks[oldPos]; ok {
		if old.removeEntity(handle) && old.Ready() {
			old.modified = true
		}
		if len(old.Entities) == 0 {
			w.removeEntityColumn(oldPos)
		}
		viewers = old.viewers
	}

	if len(viewers) == 0 && len(newChunk.viewers) == 0 {
		return nil, true
	}
	// Entity handles lazily open their backing implementation. The opened
	// entity is returned so the caller reuses the same pointer.
	ent := state.entity(tx, handle)
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
	return ent, true
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

// scheduledTickQueue stores one Java tick registry's scheduled updates.
type scheduledTickQueue struct {
	// chunks holds the ticks of each chunk in the order they were scheduled.
	chunks         map[ChunkPos]*scheduledChunkTicks
	dispatch       []scheduledTick
	run            []scheduledTick
	selected       []scheduledTick
	groups         []scheduledTickGroup
	groupHeap      []int
	scheduledTicks map[scheduledTickIndex]int64
	pendingTicks   []scheduledTick
	deferredChunks map[ChunkPos]int
	saveTicks      []scheduledTick
	processing     bool
	fluid          bool
	currentTick    int64
	nextOrder      int64
}

// scheduledChunkTicks holds the ticks of a chunk in the order they were
// scheduled, so that a tick only visits the chunks with ticks due.
type scheduledChunkTicks struct {
	ticks []scheduledTick
	// due is the earliest trigger tick of the ticks not deferred.
	due int64
}

// updateDue recomputes the earliest trigger tick of the chunk.
func (list *scheduledChunkTicks) updateDue() {
	list.due = math.MaxInt64
	for _, t := range list.ticks {
		if !t.deferred {
			list.due = min(list.due, t.t)
		}
	}
}

// push appends t to the ticks of its chunk.
func (queue *scheduledTickQueue) push(t scheduledTick) {
	pos := chunkPosFromBlockPos(t.pos)
	list := queue.chunks[pos]
	if list == nil {
		list = &scheduledChunkTicks{due: math.MaxInt64}
		if queue.chunks == nil {
			queue.chunks = make(map[ChunkPos]*scheduledChunkTicks)
		}
		queue.chunks[pos] = list
	}
	list.ticks = append(list.ticks, t)
	if !t.deferred {
		list.due = min(list.due, t.t)
	}
}

type scheduledTick struct {
	pos      cube.Pos
	btype    string
	t        int64
	priority int32
	order    int64
	// queueIndex is the index of the tick among the ticks of its chunk.
	queueIndex int
	deferred   bool // t is a relative saved delay until block-ticking promotion.
}

type scheduledTickGroup struct {
	end, next int
}

type scheduledTickIndex struct {
	pos      cube.Pos
	typeName string
}

type scheduledTickTypeKey struct {
	kind     chunk.ScheduledTickKind
	typeName string
}

const maxScheduledTicksPerTick = 65536

// newScheduledTickQueue creates a queue for block ticks.
func newScheduledTickQueue(tick int64) *scheduledTickQueue {
	return newScheduledTickQueueForKind(tick, false)
}

// newScheduledFluidTickQueue creates a queue for fluid ticks.
func newScheduledFluidTickQueue(tick int64) *scheduledTickQueue {
	return newScheduledTickQueueForKind(tick, true)
}

func newScheduledTickQueueForKind(tick int64, fluid bool) *scheduledTickQueue {
	return &scheduledTickQueue{
		scheduledTicks: make(map[scheduledTickIndex]int64),
		currentTick:    tick,
		fluid:          fluid,
	}
}

// tick collects at most one source tick budget from loaded, ticking chunks,
// then dispatches the collected batch. Fluid ticks run after the block queue.
func (queue *scheduledTickQueue) tick(tx *Tx, tick int64, loaders ...*Loader) {
	queue.currentTick = tick
	if len(queue.chunks) == 0 {
		return
	}

	w := tx.World()
	areas := w.scratchLoaderAreas[:0]
	if !w.conf.Synchronous {
		simulationRadius := int32(w.tickRange())
		if len(loaders) != 0 {
			for _, loader := range loaders {
				areas = append(areas, loader.activeArea(simulationRadius))
			}
		} else {
			for loader := range w.viewers {
				areas = append(areas, loader.activeArea(simulationRadius))
			}
		}
	}
	queue.dispatch = queue.dispatch[:0]
	for pos, list := range queue.chunks {
		if list.due > tick || !w.scheduledTickChunkEligible(pos, areas) {
			continue
		}
		for i, t := range list.ticks {
			if t.deferred || t.t > tick {
				continue
			}
			t.queueIndex = i
			queue.dispatch = append(queue.dispatch, t)
		}
	}
	slices.SortFunc(queue.dispatch, func(a, b scheduledTick) int {
		aPos, bPos := chunkPosFromBlockPos(a.pos), chunkPosFromBlockPos(b.pos)
		if aPos[0] < bPos[0] {
			return -1
		}
		if aPos[0] > bPos[0] {
			return 1
		}
		if aPos[1] < bPos[1] {
			return -1
		}
		if aPos[1] > bPos[1] {
			return 1
		}
		return compareScheduledTickDrainOrder(a, b)
	})

	queue.groups = queue.groups[:0]
	queue.groupHeap = queue.groupHeap[:0]
	for start := 0; start < len(queue.dispatch); {
		pos := chunkPosFromBlockPos(queue.dispatch[start].pos)
		end := start + 1
		for end < len(queue.dispatch) && chunkPosFromBlockPos(queue.dispatch[end].pos) == pos {
			end++
		}
		group := len(queue.groups)
		queue.groups = append(queue.groups, scheduledTickGroup{end: end, next: start})
		queue.pushScheduledTickGroup(group)
		start = end
	}

	queue.run = queue.run[:0]
	for len(queue.groupHeap) != 0 && len(queue.run) < maxScheduledTicksPerTick {
		group := queue.popScheduledTickGroup()
		current := &queue.groups[group]
		queue.run = append(queue.run, queue.dispatch[current.next])
		current.next++
		if current.next < current.end {
			queue.pushScheduledTickGroup(group)
		}
	}
	queue.removeSelected()

	// LevelChunkTicks.poll removes every collected identity during collection,
	// before any callback can schedule a replacement of another collected tick.
	for _, t := range queue.run {
		delete(queue.scheduledTicks, scheduledTickIndex{pos: t.pos, typeName: t.btype})
		if col, ok := w.chunks[chunkPosFromBlockPos(t.pos)]; ok {
			col.modified = true
		}
	}

	queue.processing = true
	for _, t := range queue.run {
		if t.btype == "" {
			continue
		} // cancelled by an earlier callback in this batch
		if queue.fluid {
			liquid, ok := tx.Liquid(t.pos)
			if !ok || !scheduledFluidTickMatches(liquid, t.btype) {
				continue
			}
			switch t.btype {
			case "minecraft:water", "minecraft:flowing_water", "minecraft:lava", "minecraft:flowing_lava":
				context := javaShapeContext{tx: tx, pos: t.pos}
				state, fluid := javaFluidStateAt(&context, t.pos)
				javaTickFluid(&context, state, fluid)
				continue
			}
			if ticker, ok := liquid.(ScheduledTicker); ok {
				ticker.ScheduledTick(t.pos, tx, w.r)
			}
			continue
		}
		switch t.btype {
		case "minecraft:water", "minecraft:lava", "minecraft:bubble_column":
			context := javaShapeContext{tx: tx, pos: t.pos}
			state := tx.javaBlockStateID(t.pos)
			if state != 0 && javaName(state) == t.btype[len("minecraft:"):] {
				if javaName(state) == "bubble_column" {
					javaTickBubbleColumn(&context, state)
				} else {
					javaTickLiquidBlock(&context, state)
				}
			}
			continue
		}
		b := tx.Block(t.pos)
		if state := tx.javaBlockStateID(t.pos); state != 0 {
			if javaName(state) != strings.TrimPrefix(t.btype, "minecraft:") {
				continue
			}
		} else if scheduledTickType(w.conf.Blocks, b) != t.btype {
			continue
		}
		if ticker, ok := b.(ScheduledTicker); ok {
			ticker.ScheduledTick(t.pos, tx, w.r)
		}
	}
	queue.processing = false
	for _, t := range queue.pendingTicks {
		queue.push(t)
	}
	clear(queue.pendingTicks)
	queue.pendingTicks = queue.pendingTicks[:0]
	queue.dispatch = queue.dispatch[:0]
	queue.run = queue.run[:0]
	queue.groups = queue.groups[:0]
	queue.groupHeap = queue.groupHeap[:0]
	w.scratchLoaderAreas = areas[:0]
}

// cancel removes one type/position identity without disturbing other queued
// types or newly scheduled ticks at that position.
func (queue *scheduledTickQueue) cancel(index scheduledTickIndex) bool {
	_, removed := queue.scheduledTicks[index]
	delete(queue.scheduledTicks, index)
	chunkPos := chunkPosFromBlockPos(index.pos)
	match := func(t scheduledTick) bool { return t.pos == index.pos && t.btype == index.typeName }
	drop := func(t scheduledTick) bool {
		if !match(t) {
			return false
		}
		removed = true
		if t.deferred {
			if queue.deferredChunks[chunkPos] <= 1 {
				delete(queue.deferredChunks, chunkPos)
			} else {
				queue.deferredChunks[chunkPos]--
			}
		}
		return true
	}
	if list := queue.chunks[chunkPos]; list != nil {
		list.ticks = slices.DeleteFunc(list.ticks, drop)
		if len(list.ticks) == 0 {
			delete(queue.chunks, chunkPos)
		} else {
			list.updateDue()
		}
	}
	queue.pendingTicks = slices.DeleteFunc(queue.pendingTicks, drop)
	if queue.processing {
		for i, t := range queue.run {
			if match(t) {
				queue.run[i] = scheduledTick{}
				removed = true
			}
		}
	}
	return removed
}

// removeSelected removes the ticks collected to run from their chunks,
// keeping the order of the others.
func (queue *scheduledTickQueue) removeSelected() {
	queue.selected = append(queue.selected[:0], queue.run...)
	slices.SortFunc(queue.selected, func(a, b scheduledTick) int {
		aPos, bPos := chunkPosFromBlockPos(a.pos), chunkPosFromBlockPos(b.pos)
		return cmp.Or(cmp.Compare(aPos[0], bPos[0]), cmp.Compare(aPos[1], bPos[1]), cmp.Compare(a.queueIndex, b.queueIndex))
	})
	for start := 0; start < len(queue.selected); {
		pos := chunkPosFromBlockPos(queue.selected[start].pos)
		list := queue.chunks[pos]
		dst, next := 0, start
		for i, t := range list.ticks {
			if next < len(queue.selected) && queue.selected[next].queueIndex == i && chunkPosFromBlockPos(queue.selected[next].pos) == pos {
				next++
				continue
			}
			list.ticks[dst] = t
			dst++
		}
		clear(list.ticks[dst:])
		list.ticks = list.ticks[:dst]
		if dst == 0 {
			delete(queue.chunks, pos)
		} else {
			list.updateDue()
		}
		start = next
	}
	clear(queue.selected)
	queue.selected = queue.selected[:0]
}

func (w *World) scheduledTickChunkEligible(pos ChunkPos, areas []loaderActiveArea) bool {
	if !w.chunkLoaded(pos) {
		return false
	}
	col := w.chunks[pos]
	if !w.blockTickingReady(pos, col) {
		return false
	}
	return w.conf.Synchronous || columnWithinAreas(pos, areas) || w.IsChunkForced(pos)
}

func compareScheduledTickDrainOrder(a, b scheduledTick) int {
	if a.t < b.t {
		return -1
	}
	if a.t > b.t {
		return 1
	}
	if a.priority < b.priority {
		return -1
	}
	if a.priority > b.priority {
		return 1
	}
	if a.order < b.order {
		return -1
	}
	if a.order > b.order {
		return 1
	}
	return 0
}

func (queue *scheduledTickQueue) scheduledTickGroupLess(a, b int) bool {
	aTick := queue.dispatch[queue.groups[a].next]
	bTick := queue.dispatch[queue.groups[b].next]
	if aTick.priority != bTick.priority {
		return aTick.priority < bTick.priority
	}
	if aTick.order != bTick.order {
		return aTick.order < bTick.order
	}
	aPos, bPos := chunkPosFromBlockPos(aTick.pos), chunkPosFromBlockPos(bTick.pos)
	if aPos[0] != bPos[0] {
		return aPos[0] < bPos[0]
	}
	return aPos[1] < bPos[1]
}

func (queue *scheduledTickQueue) pushScheduledTickGroup(group int) {
	queue.groupHeap = append(queue.groupHeap, group)
	index := len(queue.groupHeap) - 1
	for index > 0 {
		parent := (index - 1) / 2
		if !queue.scheduledTickGroupLess(queue.groupHeap[index], queue.groupHeap[parent]) {
			break
		}
		queue.groupHeap[index], queue.groupHeap[parent] = queue.groupHeap[parent], queue.groupHeap[index]
		index = parent
	}
}

func (queue *scheduledTickQueue) popScheduledTickGroup() int {
	root := queue.groupHeap[0]
	last := len(queue.groupHeap) - 1
	queue.groupHeap[0] = queue.groupHeap[last]
	queue.groupHeap = queue.groupHeap[:last]
	for index := 0; ; {
		left := index*2 + 1
		if left >= len(queue.groupHeap) {
			break
		}
		best := left
		right := left + 1
		if right < len(queue.groupHeap) && queue.scheduledTickGroupLess(queue.groupHeap[right], queue.groupHeap[left]) {
			best = right
		}
		if !queue.scheduledTickGroupLess(queue.groupHeap[best], queue.groupHeap[index]) {
			break
		}
		queue.groupHeap[index], queue.groupHeap[best] = queue.groupHeap[best], queue.groupHeap[index]
		index = best
	}
	return root
}

// schedule schedules a live update after the requested delay in ticks. Only
// the first pending update for a type at a position is kept.
func (queue *scheduledTickQueue) schedule(br BlockRegistry, pos cube.Pos, b Block, delay time.Duration) bool {
	due := queue.currentTick + int64(max(delay/(time.Second/20), 0))
	return queue.scheduleAt(br, pos, b, due, 0)
}

// scheduleAt inserts a live tick at its absolute trigger tick.
func (queue *scheduledTickQueue) scheduleAt(br BlockRegistry, pos cube.Pos, b Block, due int64, priority int32) bool {
	return queue.insertTick(br, pos, b, due, priority, false)
}

func (queue *scheduledTickQueue) insertTick(br BlockRegistry, pos cube.Pos, b Block, due int64, priority int32, deferred bool) bool {
	var typeName string
	if queue.fluid {
		liquid, ok := b.(Liquid)
		if !ok {
			panic("scheduled fluid tick: type is not a liquid")
		}
		typeName = scheduledFluidTickType(liquid)
	} else {
		typeName = scheduledTickType(br, b)
	}
	return queue.insertTypeTick(pos, typeName, due, priority, deferred)
}

// insertTypeTick retains the source registry identity even when multiple Java
// block types share one native palette representation.
func (queue *scheduledTickQueue) insertTypeTick(pos cube.Pos, typeName string, due int64, priority int32, deferred bool) bool {
	index := scheduledTickIndex{pos: pos, typeName: typeName}
	if _, exists := queue.scheduledTicks[index]; exists {
		return false
	}
	priority = normalizeScheduledTickPriority(priority)
	t := scheduledTick{pos: pos, t: due, btype: typeName, priority: priority, order: queue.nextOrder, deferred: deferred}
	if deferred {
		queue.reserveDeferredChunk(chunkPosFromBlockPos(pos))
	} else {
		queue.nextOrder++
	}
	queue.scheduledTicks[index] = due
	if queue.processing {
		queue.pendingTicks = append(queue.pendingTicks, t)
	} else {
		queue.push(t)
	}
	return true
}

func (queue *scheduledTickQueue) reserveDeferredChunk(pos ChunkPos) {
	if queue.deferredChunks == nil {
		queue.deferredChunks = make(map[ChunkPos]int)
	}
	queue.deferredChunks[pos]++
}

func (queue *scheduledTickQueue) activateChunk(pos ChunkPos, now int64) {
	count := queue.deferredChunks[pos]
	if count == 0 {
		return
	}
	order := -int64(count)
	activate := func(ticks []scheduledTick) {
		for i := range ticks {
			tick := &ticks[i]
			if tick.deferred && chunkPosFromBlockPos(tick.pos) == pos {
				tick.deferred = false
				tick.t += now
				tick.order = order
				order++
				queue.scheduledTicks[scheduledTickIndex{pos: tick.pos, typeName: tick.btype}] = tick.t
			}
		}
	}
	if list := queue.chunks[pos]; list != nil {
		activate(list.ticks)
		list.updateDue()
	}
	activate(queue.pendingTicks)
	delete(queue.deferredChunks, pos)
}

// scheduledTickType returns the Java registry type represented by a native
// block state. Scheduled ticks identify a type, not an individual state.
func scheduledTickType(registry BlockRegistry, b Block) string {
	rid := registry.BlockRuntimeID(b)
	name, properties, ok := registry.RuntimeIDToState(rid)
	if !ok {
		panic("scheduled tick: registry cannot resolve block runtime ID")
	}
	return javaBlockTypeForNative(name, properties)
}

func scheduledFluidTickType(liquid Liquid) string {
	name := liquid.LiquidType()
	if name == "" {
		panic("scheduled fluid tick: empty liquid type")
	}
	if !strings.Contains(name, ":") {
		name = "minecraft:" + name
	}
	// Java LiquidBlock maps level zero to the source fluid. Bedrock's
	// water/flowing_water names independently encode the native Still flag.
	if liquid.LiquidDepth() != 8 || liquid.LiquidFalling() {
		switch name {
		case "minecraft:water":
			return "minecraft:flowing_water"
		case "minecraft:lava":
			return "minecraft:flowing_lava"
		}
	}
	return name
}
func scheduledFluidTickMatches(liquid Liquid, typeName string) bool {
	name := liquid.LiquidType()
	if name == "" {
		panic("scheduled fluid tick: empty liquid type")
	}
	if !strings.Contains(name, ":") {
		name = "minecraft:" + name
	}
	if name == "minecraft:water" {
		return typeName == "minecraft:water" || typeName == "minecraft:flowing_water"
	}
	if name == "minecraft:lava" {
		return typeName == "minecraft:lava" || typeName == "minecraft:flowing_lava"
	}
	return name == typeName
}

func normalizeScheduledTickPriority(priority int32) int32 {
	if priority < -3 {
		return -3
	}
	if priority > 3 {
		return 3
	}
	return priority
}

// fromChunk returns the ticks owned by the chunk containing each scheduled
// position, retaining insertion order for native saved-tick list order.
func (queue *scheduledTickQueue) fromChunk(pos ChunkPos) []chunk.SavedScheduledTick {
	ticks := make([]chunk.SavedScheduledTick, 0, 8)
	kind := chunk.BlockTick
	if queue.fluid {
		kind = chunk.FluidTick
	}
	ordered := queue.saveTicks[:0]
	var listed []scheduledTick
	if list := queue.chunks[pos]; list != nil {
		listed = list.ticks
	}
	for _, pending := range [2][]scheduledTick{listed, queue.pendingTicks} {
		for _, t := range pending {
			if pos == chunkPosFromBlockPos(t.pos) {
				ordered = append(ordered, t)
			}
		}
	}
	slices.SortStableFunc(ordered, func(a, b scheduledTick) int {
		if a.deferred != b.deferred {
			if a.deferred {
				return -1
			}
			return 1
		}
		if a.deferred {
			return 0
		}
		if a.order < b.order {
			return -1
		}
		if a.order > b.order {
			return 1
		}
		return 0
	})
	for _, t := range ordered {
		delay := t.t
		if !t.deferred {
			delay -= queue.currentTick
		}
		ticks = append(ticks, chunk.SavedScheduledTick{
			ScheduledTick: chunk.ScheduledTick{Pos: t.pos, Kind: kind, Type: t.btype, Priority: t.priority},
			Delay:         int32(delay),
		})
	}
	clear(ordered)
	queue.saveTicks = ordered[:0]
	return ticks
}

// removeChunk removes all scheduled ticks positioned within a ChunkPos.
func (queue *scheduledTickQueue) removeChunk(pos ChunkPos) {
	delete(queue.deferredChunks, pos)
	if list := queue.chunks[pos]; list != nil {
		for _, tick := range list.ticks {
			delete(queue.scheduledTicks, scheduledTickIndex{pos: tick.pos, typeName: tick.btype})
		}
		delete(queue.chunks, pos)
	}
	queue.pendingTicks = slices.DeleteFunc(queue.pendingTicks, func(tick scheduledTick) bool {
		if chunkPosFromBlockPos(tick.pos) != pos {
			return false
		}
		delete(queue.scheduledTicks, scheduledTickIndex{pos: tick.pos, typeName: tick.btype})
		return true
	})
}

func (queue *scheduledTickQueue) add(ticks []scheduledTick) {
	count := int64(len(ticks))
	for i, t := range ticks {
		index := scheduledTickIndex{pos: t.pos, typeName: t.btype}
		if _, exists := queue.scheduledTicks[index]; exists {
			continue
		}
		t.priority = normalizeScheduledTickPriority(t.priority)
		t.order = int64(i) - count
		if t.deferred {
			queue.reserveDeferredChunk(chunkPosFromBlockPos(t.pos))
		}
		queue.scheduledTicks[index] = t.t
		queue.push(t)
	}
}
