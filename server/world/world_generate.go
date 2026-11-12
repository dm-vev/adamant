package world

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// generation is the generation of chunks and the columns read ahead of their
// load.
type generation struct {
	generatorRunning sync.WaitGroup
	// prefetched holds the columns being read from the provider ahead of
	// their load, by position, and readSlots bounds how many are read at
	// once: see prefetchColumn. Only the owner touches the map.
	prefetched map[ChunkPos]*columnRead
	readSlots  chan struct{}
	// releaser is the generator if it releases its caches when generation
	// idles: releaseTimer fires releaseAfter after the last chunk was
	// generated, guarded by releaseMu. See armCacheRelease.
	releaser       ReleasingGenerator
	releaseMu      sync.Mutex
	releaseTimer   *time.Timer
	releaseAfter   time.Duration
	generatorQueue *generationQueue
	// generatorQueueSaturation counts how often more chunk generation tasks
	// waited than GeneratorQueueSize. We use this to rate-limit backlog
	// warnings so operators can tune the worker count.
	generatorQueueSaturation atomic.Uint64
	lastQueueSaturationLog   atomic.Uint64
}

type generationTask struct {
	pos     ChunkPos
	col     *Column
	context GenerationContext
}

// errGenerationPanicked is the loadErr of a column whose generation
// panicked. It holds what the generator made of it so far: it is served, so
// that nothing waits for it, but never saved, and is generated again once it
// unloaded.
var errGenerationPanicked = errors.New("generation panicked")

// generateChunkAsync schedules an asynchronous chunk generation task for the given position.
// The task joins the queue of the generator workers without waiting, so the
// world goroutine requesting chunks keeps ticking while the workers catch up.
// If shutdown is in progress, the column is immediately marked as ready, so
// Close() and c.waitReady() never block on a chunk that will not be generated.
func (w *World) generateChunkAsync(pos ChunkPos, col *Column) {
	task := generationTask{pos: pos, col: col, context: w.generationContext()}
	if w.closed.Load() {
		w.markUngenerated(col)
		return
	}
	if w.conf.Synchronous {
		w.runGenerationTask(task)
		return
	}
	waiting, ok := w.generatorQueue.push(task)
	if !ok {
		w.markUngenerated(col)
		return
	}
	if waiting > w.conf.GeneratorQueueSize {
		w.handleGeneratorBackpressure(waiting)
	}
}

// generatorWorker processes generation tasks from the generator queue until
// the queue is closed at shutdown.
func (w *World) generatorWorker() {
	defer w.running.Done()
	defer w.generatorRunning.Done()

	var cursor generationCursor
	for {
		task, ok := w.generatorQueue.popNear(&cursor)
		if !ok {
			return
		}
		w.runGenerationTask(task)
		w.armCacheRelease()
	}
}

// runGenerationTask executes the chunk generation logic for a given task.
// It ensures that the associated column is always marked as ready, even if
// the generation panics or fails unexpectedly.
//
// This design guarantees that no waiting goroutine (e.g., loadChunk callers)
// will hang indefinitely due to an unmarked column.
func (w *World) runGenerationTask(task generationTask) {
	if task.col.Ready() || !task.col.startGeneration() {
		// The chunk was closed, or nothing wanted it any more, while its
		// task waited: nothing reads it.
		return
	}
	if task.col.Chunk == nil {
		task.col.Chunk = chunk.New(w.conf.Blocks, w.Range())
	}
	// Generation owns terrain, block entities and cached terrain payloads until
	// markReady publishes them. Entity/viewer membership remains owner-only,
	// including while generation is pending.
	defer func() {
		// Always recover from panics during generation to prevent worker
		// termination. The column holds what the generator made of it so
		// far: it is served, but never saved.
		if r := recover(); r != nil {
			f := newFault(FaultGeneration, r)
			f.Type, f.Pos = "generator", cube.Pos{int(task.pos[0]) << 4, 0, int(task.pos[1]) << 4}
			w.reportFault(f, quarantineKey{})
			task.col.loadErr = fmt.Errorf("%w: %v", errGenerationPanicked, r)
		}
		task.col.fillLight(task.pos)
		task.col.markReady()
		if !w.closed.Load() {
			w.Do(func(tx *Tx) {
				task.col.ensureLight(w, task.pos)
				w.finishGeneration(tx, task.col)
				w.reconcileMissingBlockStates(task.pos)
				w.finishChunkRequest(tx, task.pos, task.col)
			})
		}
	}()

	// The generation is timed and counted apart from the recover above,
	// which runs after this: a panic counts as a failure, not a time.
	start, generated := time.Now(), false
	w.stats.GenerationBusy.Add(1)
	defer func() {
		w.stats.GenerationBusy.Add(-1)
		if generated {
			w.stats.Generation.Observe(time.Since(start).Seconds())
		} else {
			w.stats.GenerationFailures.Inc()
		}
	}()

	// Perform the actual chunk generation.
	// The generator implementation is responsible for populating the chunk’s data.
	task.col.generation = w.conf.Generator.GenerateChunk(task.pos, task.col.Chunk, task.context)
	generated = true
	task.col.generation.pos = task.pos

	task.col.BlockEntities = w.generatedBlockEntities(task.pos, task.col.Chunk)
	if task.col.BlockEntities == nil {
		task.col.BlockEntities = map[cube.Pos]Block{}
	}
	task.col.invalidateNetworkBiomePayload()
	task.col.invalidateNetworkSubChunkPayloads()
	task.col.invalidateBlockEntityPayloads()
	task.col.modified, task.col.complete = true, true
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
		for i := range pal.Len() {
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
		for x := range byte(16) {
			for z := range byte(16) {
				for y := range byte(16) {
					rid := storage.At(x, y, z)
					if !w.conf.Blocks.NBTBlock(rid) {
						continue
					}
					worldPos := cube.Pos{baseX + int(x), subY + int(y), baseZ + int(z)}
					if _, ok := blockEntities[worldPos]; ok {
						continue
					}
					nbtB := DecodeNBT(w.conf.Blocks.BlockByRuntimeIDOrAir(rid).(NBTer), map[string]any{}, w.conf.Blocks).(Block)
					nbtB = restoreJavaBlockActor(c, worldPos, nbtB)
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

// focusGeneration orders the waiting generation tasks by the distance of their
// chunks to the loaders passed.
func (w *World) focusGeneration(loaders []*Loader) {
	focus := w.scratchFocus[:0]
	for _, l := range loaders {
		l.mu.RLock()
		focus = append(focus, l.pos)
		l.mu.RUnlock()
	}
	w.generatorQueue.setFocus(focus)
	w.scratchFocus = focus
}

// drainGenerationQueue closes the generator queue at shutdown and marks the
// columns of the tasks no worker took ready, so nothing waits for them.
func (w *World) drainGenerationQueue() {
	for _, task := range w.generatorQueue.close() {
		if !task.col.abandoned() {
			w.markUngenerated(task.col)
		}
	}
}

// markUngenerated marks a column ready that is not generated, as the world
// closes: it holds air, and is not complete, so that it is never saved.
func (w *World) markUngenerated(col *Column) {
	if col.Chunk == nil {
		col.Chunk = chunk.New(w.conf.Blocks, w.Range())
	}
	col.markReady()
}

// handleGeneratorBackpressure counts a chunk request made while more
// generation tasks waited than GeneratorQueueSize and emits a throttled
// warning, giving operators guidance on the worker count under heavy terrain
// generation load.
func (w *World) handleGeneratorBackpressure(waiting int) {
	count := w.generatorQueueSaturation.Add(1)
	now := uint64(time.Now().UnixNano())
	last := w.lastQueueSaturationLog.Load()

	if last != 0 && time.Duration(now-last) < time.Minute {
		return
	}
	if !w.lastQueueSaturationLog.CompareAndSwap(last, now) {
		return
	}

	w.genLog.Warn(
		"world generator backlog: chunk generation tasks are waiting for workers.",
		"waiting_tasks", waiting,
		"backlog_events", count,
		"backlog_threshold", w.conf.GeneratorQueueSize,
		"workers", w.conf.GeneratorWorkers,
	)
}

// finishGeneration transfers generated payloads to owner-thread world state.
// Clear the payload before adding entities: AddEntity may request this column.
func (w *World) finishGeneration(tx *Tx, c *Column) {
	if !c.Ready() {
		return
	}
	result := c.generation
	c.generation = GenerationResult{}
	if result.PostProcessing != nil {
		c.postProcessing = result.PostProcessing
	}
	w.decodeBlockEntities(c, result.BlockEntities)
	for _, update := range result.ScheduledTicks {
		w.scheduleGeneratedTick(update)
	}
	for _, e := range result.Entities {
		tx.AddEntity(e)
	}
	if result.SpawnOriginalMobs {
		if spawn := w.conf.Entities.Config().SpawnOriginalMobs; spawn != nil {
			spawn(tx, result.pos)
		}
	}
}
