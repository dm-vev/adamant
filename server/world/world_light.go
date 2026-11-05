package world

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/parallel"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// fillLightAsync fills the light of a column just read from the provider on
// a goroutine of its own, marks it ready and then publishes it on the world
// owner, as a generated column is: until then, readers of its blocks wait
// for it. Filling the light of a column takes about as long as reading it,
// and players crossing terrain they saw before read many columns a tick.
func (w *World) fillLightAsync(pos ChunkPos, col *Column) {
	w.scheduleMu.Lock()
	if w.closed.Load() {
		w.scheduleMu.Unlock()
		col.fillLight(pos)
		col.markReady()
		return
	}
	w.generatorRunning.Add(1)
	w.scheduleMu.Unlock()
	go func() {
		defer w.generatorRunning.Done()
		defer func() {
			if r := recover(); r != nil {
				f := newFault(FaultGeneration, r)
				f.Type, f.Pos = "light", cube.Pos{int(pos[0]) << 4, 0, int(pos[1]) << 4}
				w.reportFault(f, quarantineKey{})
			}
			col.markReady()
			if w.closed.Load() {
				return
			}
			w.Do(func(tx *Tx) {
				if w.chunks[pos] != col {
					// The column closed meanwhile.
					return
				}
				col.ensureLight(w, pos)
				w.reconcileMissingBlockStates(pos)
				w.finishChunkRequest(tx, pos, col)
			})
		}()
		w.lightFills <- struct{}{}
		defer func() { <-w.lightFills }()
		col.fillLight(pos)
	}()
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

// spreadLight queues the light of the chunk at pos to be spread into all its
// neighbours if each of them is generated and lit. The spread is done with
// the others queued before light is next read.
func (w *World) spreadLight(pos ChunkPos) {
	if chunks, ok := w.spreadArea(pos); ok {
		w.pendingSpreads = append(w.pendingSpreads, pendingSpread{pos: pos, chunks: chunks})
	}
}

// spreadArea returns the chunks around pos, pos in the middle, if they are
// all generated and lit so that the light of pos can be spread into them.
func (w *World) spreadArea(pos ChunkPos) ([]*chunk.Chunk, bool) {
	c := make([]*chunk.Chunk, 0, 9)
	for z := int32(-1); z <= 1; z++ {
		for x := int32(-1); x <= 1; x++ {
			neighbourPos := ChunkPos{pos[0] + x, pos[1] + z}
			neighbour, ok := w.chunks[neighbourPos]
			if !ok {
				// Not all surrounding chunks existed: Stop spreading light.
				return nil, false
			}
			if !neighbour.Ready() || !neighbour.lightReady.Load() {
				// The neighbour chunk hasn't finished generating yet or its light hasn't been initialised
				// yet. We'll spread the light once all chunks involved are ready.
				return nil, false
			}
			c = append(c, neighbour.Chunk)
		}
	}
	return c, true
}

// pendingSpread is the spread of the light of the chunk at pos into the
// chunks around it, chunks holding all nine. A spread no longer deferred is
// done even if far from the columns ticking.
type pendingSpread struct {
	pos    ChunkPos
	chunks []*chunk.Chunk
	forced bool
}

// spreadFar reports whether the spread of the light around the chunk at pos
// may wait: no column within three chunks of it ticks its entities. Light is
// only read where things tick, so it is spread around a chunk once a column
// near it comes to tick, or light near it is read, as Java lights chunks off
// its main thread. Bedrock clients light chunks themselves.
func (w *World) spreadFar(pos ChunkPos) bool {
	for dx := int32(-3); dx <= 3; dx++ {
		for dz := int32(-3); dz <= 3; dz++ {
			if c, ok := w.chunks[ChunkPos{pos[0] + dx, pos[1] + dz}]; ok && w.entitiesTick(c) {
				return false
			}
		}
	}
	return true
}

// undeferSpreadsNear queues the spreads of light deferred around the chunks
// within three chunks of pos to be done with the others.
func (w *World) undeferSpreadsNear(pos ChunkPos) {
	for dx := int32(-3); dx <= 3; dx++ {
		for dz := int32(-3); dz <= 3; dz++ {
			p := ChunkPos{pos[0] + dx, pos[1] + dz}
			if sp, ok := w.deferredSpreads[p]; ok {
				sp.forced = true
				w.pendingSpreads = append(w.pendingSpreads, sp)
				delete(w.deferredSpreads, p)
			}
		}
	}
}

// flushSpreads does the pending light spreads and spreads again where they
// changed light, until spreading changes nothing: spreading only raises
// light, so the light ends up the same whatever order the chunks became
// ready in. In each round, spreads whose middle chunks lie at the same
// coordinates modulo 3 share no chunk, so each such class is spread on all
// cores while the owner waits.
func (w *World) flushSpreads() {
	if len(w.pendingSpreads) == 0 {
		return
	}
	queued := make(map[ChunkPos]struct{}, len(w.pendingSpreads))
	for _, sp := range w.pendingSpreads {
		if !w.spreadStillLoaded(sp) {
			continue
		}
		if !sp.forced && w.spreadFar(sp.pos) {
			if w.deferredSpreads == nil {
				w.deferredSpreads = make(map[ChunkPos]pendingSpread)
			}
			w.deferredSpreads[sp.pos] = sp
			continue
		}
		queued[sp.pos] = struct{}{}
	}
	w.pendingSpreads = nil
	changed := make(map[ChunkPos]struct{})
	for round := 0; len(queued) > 0; round++ {
		if round == maxSpreadRounds {
			w.genLog.Debug("light spreading did not settle", "rounds", round, "areas", len(queued))
			break
		}
		var classes [9][]pendingSpread
		for pos := range queued {
			if chunks, ok := w.spreadArea(pos); ok {
				// A spread deferred around pos is done with this one.
				delete(w.deferredSpreads, pos)
				class := (pos[0]%3+3)%3*3 + (pos[1]%3+3)%3
				classes[class] = append(classes[class], pendingSpread{pos: pos, chunks: chunks})
			}
		}
		clear(queued)
		clear(changed)
		for _, spreads := range classes {
			masks := make([]uint16, len(spreads))
			parallel.For(len(spreads), 1, func(i int) {
				sp := spreads[i]
				masks[i] = chunk.LightArea(sp.chunks, int(sp.pos[0])-1, int(sp.pos[1])-1).Spread()
			})
			for i, sp := range spreads {
				for bit := range 9 {
					if masks[i]&(1<<bit) != 0 {
						changed[ChunkPos{sp.pos[0] + int32(bit%3) - 1, sp.pos[1] + int32(bit/3) - 1}] = struct{}{}
					}
				}
			}
		}
		// Light that changed in a chunk may spread on into the chunks
		// around it through the areas holding it.
		for pos := range changed {
			for x := int32(-1); x <= 1; x++ {
				for z := int32(-1); z <= 1; z++ {
					queued[ChunkPos{pos[0] + x, pos[1] + z}] = struct{}{}
				}
			}
		}
	}
}

// maxSpreadRounds bounds the rounds of flushSpreads. Light settles within a
// few, as it only ever rises and reaches at most 15 blocks.
const maxSpreadRounds = 32

// spreadStillLoaded reports whether the chunks of a pending spread are all
// still the loaded ones at their positions.
func (w *World) spreadStillLoaded(sp pendingSpread) bool {
	i := 0
	for z := int32(-1); z <= 1; z++ {
		for x := int32(-1); x <= 1; x++ {
			if col, ok := w.chunks[ChunkPos{sp.pos[0] + x, sp.pos[1] + z}]; !ok || col.Chunk != sp.chunks[i] {
				return false
			}
			i++
		}
	}
	return true
}
