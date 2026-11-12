package world

import (
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/df-mc/dragonfly/server/internal/parallel"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// saveState is what the world keeps for saving its columns and handing
// transfers out.
type saveState struct {
	autoSaveDisabled atomic.Bool
	// saver writes saved columns in the background; it is nil for
	// synchronous and read-only worlds, which write them at once.
	saver *chunkSaver
	// saves counts the columns saved, on the world goroutine. A column saved
	// takes the count as its savedIn, which tells a failed write of that
	// save from a later save of the column.
	saves uint64
	// dirty lists the columns modified or touched since they were last
	// saved, which a capture walks. saveEpoch counts the captures that
	// walked them, on the world goroutine: an entity touches its column
	// once an epoch (World.touchEntity).
	dirty     dirtyColumns
	saveEpoch uint64
	// captures counts the captures of the world for a save, on the world
	// goroutine. dataWritten is the count of the last capture whose data
	// was written, under dataMu: the data of a capture is written off the
	// world goroutine, and that of an older one never after a newer one.
	captures    uint64
	dataMu      sync.Mutex
	dataWritten uint64
	// holds is the number of HoldTransfers holding transfers out of the
	// world, and transfersOut the number under way, both under the mutex of
	// transfers.
	holds        int
	transfersOut int
}

// Save saves the World to the provider and waits until it reached the disk:
// it captures the world in a transaction, then writes what it captured. It
// saves this world alone, outside the checkpoints of a server, which save
// all its worlds and players at one instant (CaptureAll). Called from a
// world owner or a region worker, it logs ErrOwnerWait and returns without
// saving: use Tx.SaveWorld from a transaction.
func (w *World) Save() {
	if w == nil {
		return
	}
	if !w.conf.Synchronous && checkOwnerWait("World.Save") != nil {
		return
	}
	var cp *Checkpoint
	<-w.exec(func(tx *Tx) { cp = w.capture(tx, false, nil) })
	if cp != nil {
		_ = cp.Write()
	}
}

// saveChunk saves a chunk and its entities to disk after compacting the chunk.
func (w *World) saveChunk(tx *Tx, pos ChunkPos, c *Column) {
	w.persistChunk(tx, pos, c, true)
}

// saveChunks saves the columns that changed since they were last saved,
// into commit if not nil, and returns how many it saved. It walks the
// columns listed as they changed (dirtyColumns), not every column loaded,
// but for ChunkSavingAll, which saves every column. A column modified is
// saved whole: copying its blocks only touches that column, so it is done
// on all cores while the owner waits, and the saver compacts the copy. A
// column only touched is saved without its blocks, which the provider
// keeps as they are, if it is an ActorStorer. The entities, block entities
// and scheduled ticks of each are then encoded and handed to the saver on
// the owner. A synchronous world compacts its columns and writes them at
// once.
func (w *World) saveChunks(tx *Tx, commit Commit) int {
	type persisted struct {
		pos  ChunkPos
		c    *Column
		full bool
	}
	to := w.conf.Provider
	if commit != nil {
		to = commit
	}
	_, actors := to.(ActorStorer)
	var columns []persisted
	add := func(pos ChunkPos, c *Column) {
		w.finishGeneration(tx, c)
		if w.storesColumn(pos, c) {
			columns = append(columns, persisted{pos: pos, c: c, full: c.modified || !actors || !c.complete})
		}
	}
	all := w.conf.SaveChunks == ChunkSavingAll
	var waiting map[ChunkPos]bool
	for _, pos := range w.dirty.take() {
		c, ok := w.chunks[pos]
		switch {
		case !ok || !c.listed:
			// Unloaded, and saved as it unloaded, or listed twice: a
			// column replaced lists its position again.
		case !c.Ready():
			// Generating or being lit: listed until it is ready, once.
			if !waiting[pos] {
				if waiting == nil {
					waiting = map[ChunkPos]bool{}
				}
				waiting[pos] = true
				w.dirty.add(pos)
			}
		default:
			c.listed = false
			if !all {
				add(pos, c)
			}
		}
	}
	if all {
		for pos, c := range w.chunks {
			if c.Ready() {
				add(pos, c)
			}
		}
	}
	var full []int
	for i, p := range columns {
		if p.full {
			full = append(full, i)
		}
	}
	clones := make([]*chunk.Chunk, len(columns))
	parallel.For(len(full), 4, func(j int) {
		i := full[j]
		c := columns[i].c
		if w.saver == nil {
			c.compact()
			return
		}
		clones[i] = c.Chunk.CloneBlocks() //nolint:staticcheck // QF1008: the chunk of the column is cloned, not the column.
	})
	cols := make([]*Column, len(columns))
	for i, p := range columns {
		cols[i] = p.c
	}
	entities := w.encodeEntities(cols)
	for i, p := range columns {
		col := w.columnWith(p.c, p.pos, entities[i])
		saved := w.markSaved(p.c, p.full)
		if !p.full {
			col.Chunk, col.PostProcessing = nil, nil
		}
		if w.saver == nil {
			if w.writeColumn(to, p.pos, col) != nil {
				w.restoreFailedSave(p.c, p.full)
			}
			continue
		}
		if p.full {
			col.Chunk = clones[i]
			col.PostProcessing = clonePostProcessing(col.PostProcessing)
		}
		w.saver.store(p.pos, col, saved, commit)
	}
	w.saveEpoch++
	return len(columns)
}

// markSaved clears the modified and touched flags of c, which is being
// saved, whole if full, and returns the number of the save, for the saver
// to report a failed write with.
func (w *World) markSaved(c *Column, full bool) uint64 {
	w.saves++
	c.modified, c.touched, c.savedIn = false, false, w.saves
	if full {
		c.blocksSavedIn = w.saves
	}
	return w.saves
}

// restoreFailedSave marks c, whose save failed to write, to be saved
// again: modified if the save wrote its blocks, touched if not.
func (w *World) restoreFailedSave(c *Column, full bool) {
	if full {
		c.setModified()
	} else {
		c.touch()
	}
}

// restoreFailedSaves takes the saves the background saver failed to write.
// A column still loaded is saved again by the next save: whole if the
// blocks of the save that failed were not saved since, without them if only
// what it keeps beside them was not; a column unloaded since is written
// again as it was saved.
func (w *World) restoreFailedSaves() {
	if w.saver == nil {
		return
	}
	for pos, p := range w.saver.takeFailed() {
		if c, ok := w.chunks[pos]; ok {
			switch {
			case p.hasBlocks() && c.blocksSavedIn == p.blocksSaved:
				c.setModified()
			case c.savedIn == p.saved:
				c.touch()
			}
			continue
		}
		w.saver.restore(p)
	}
}

// persistChunk saves the column at pos. A live column stays loaded and keeps
// changing, so the background saver gets a copy of its blocks; a closing
// column is handed over as it is, and staged by the saver until the world
// is next captured (chunkSaver.stage). The saver compacts either.
func (w *World) persistChunk(tx *Tx, pos ChunkPos, c *Column, live bool) {
	// Column generation runs in background workers. Saving is performed on the world transaction goroutine and must
	// not access columns that are still being generated.
	if !c.Ready() {
		return
	}
	w.finishGeneration(tx, c)
	if w.conf.ReadOnly || !w.storesColumn(pos, c) {
		return
	}
	col := w.columnTo(c, pos)
	saved := w.markSaved(c, true)
	if w.saver == nil {
		c.compact()
		if w.writeColumn(w.conf.Provider, pos, col) != nil {
			c.setModified()
		}
		return
	}
	if !live {
		w.saver.stage(pos, col, saved)
		return
	}
	col.Chunk = c.Chunk.CloneBlocks() //nolint:staticcheck // QF1008: the chunk of the column is cloned, not the column.
	col.PostProcessing = clonePostProcessing(col.PostProcessing)
	w.saver.store(pos, col, saved, nil)
}

// failedLoad reports whether c stands in for a column that failed to load,
// or was cut short by a panic of the generator, which is never saved, and
// logs that it is not saved the first time.
func (w *World) failedLoad(pos ChunkPos, c *Column) bool {
	if c.loadErr == nil {
		return false
	}
	if !c.loadErrLogged {
		c.loadErrLogged = true
		msg := "Not saving chunk: it failed to load, and the empty chunk in its place would overwrite it."
		if errors.Is(c.loadErr, errGenerationPanicked) {
			msg = "Not saving chunk: its generation panicked. It is generated again once it unloaded."
		}
		w.saveLog.Warn(msg, "X", pos[0], "Z", pos[1], "error", c.loadErr.Error())
	}
	return true
}

// writeColumn stores col through p, the provider of the world or a commit
// of it, logging the error it fails with. A column without blocks is stored
// through ActorStorer, leaving the blocks p holds.
func (w *World) writeColumn(p Provider, pos ChunkPos, col *chunk.Column) error {
	var err error
	if col.Chunk == nil {
		storer, ok := p.(ActorStorer)
		if !ok {
			err = fmt.Errorf("store the actors of column %v: provider %T stores whole columns only", pos, p)
		} else {
			err = storer.StoreColumnActors(pos, w.conf.Dim, col)
		}
	} else {
		err = p.StoreColumn(pos, w.conf.Dim, col)
	}
	if err != nil {
		w.saveLog.Error("save chunk: "+err.Error(), "X", pos[0], "Z", pos[1])
		return err
	}
	w.stats.ChunksWritten.Inc()
	return nil
}

// clonePostProcessing copies the post-processing marks of a column.
func clonePostProcessing(sections [][]uint16) [][]uint16 {
	if sections == nil {
		return nil
	}
	clone := make([][]uint16, len(sections))
	for i, section := range sections {
		clone[i] = slices.Clone(section)
	}
	return clone
}

// closeChunk saves a chunk and its entities to disk after compacting the chunk.
// Afterwards, scheduled updates from that chunk are removed and all entities
// in it are closed.
func (w *World) closeChunk(tx *Tx, pos ChunkPos, c *Column) {
	if c.fillingLight {
		c.waitReady()
	}
	w.finishGeneration(tx, c)
	// An entity moved by a teleport stays listed in its old chunk until its
	// next tick. It moves to the chunk it is in now rather than closing with
	// the chunk it left: a teleported player would otherwise be kicked.
	for _, e := range slices.Clone(c.Entities) {
		state, ok := w.entities[e]
		if !ok || state.pos != pos {
			continue
		}
		if now := chunkPosFromVec3(e.data.Pos); now != pos {
			w.moveEntityChunk(tx, e, state, now, entityChunkRef{pos: pos, col: c})
		}
	}
	for _, e := range slices.Clone(c.Entities) {
		if _, ok := e.Entity(tx); ok {
			continue
		}
		if c.removeEntity(e) {
			if state, ok := w.entities[e]; ok {
				w.removePendingRider(e)
				w.forgetEntity(e, state)
				delete(w.entities, e)
				w.untrackPlayer(e)
				delete(w.entitiesByUUID, e.id)
				delete(w.entitiesByUniqueID, e.UniqueID())
				w.entityCount.Add(-1)
			}
		}
	}
	// A failed write of the column marks it modified again, so that
	// closing it saves it.
	w.restoreFailedSaves()
	w.persistChunk(tx, pos, c, false)
	if len(c.faultyActors) != 0 || len(w.blockFaults.m) != 0 {
		w.forgetChunk(pos)
	}
	delete(w.unsettled, pos)
	w.scheduledBlockTicks.removeChunk(pos)
	w.scheduledFluidTicks.removeChunk(pos)
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
		if state, ok := w.entities[e]; ok {
			w.removePendingRider(e)
			w.forgetEntity(e, state)
			delete(w.entities, e)
			w.untrackPlayer(e)
			delete(w.entitiesByUUID, e.id)
			delete(w.entitiesByUniqueID, e.UniqueID())
			w.entityCount.Add(-1)
		}
		e.state = nil
		e.unsetAndLockWorld()
		_ = e.Close()
	}
	c.resetEntities()
	if w.lastChunkPos == pos {
		w.lastChunk = nil
	}
	if _, ok := w.chunks[pos]; ok {
		delete(w.chunks, pos)
		delete(w.pendingLight, pos)
		w.chunkCount.Add(-1)
		w.stats.ChunksUnloaded.Inc()
		w.columnLeft(pos)
	}
}

// columnLeft is called once the column at pos left or was replaced. It tells
// the columns within two chunks of it that the chunks around them may no
// longer all be ready, and drops the light spreads deferred into it: the
// columns around spread their light into it again once it is lit.
func (w *World) columnLeft(pos ChunkPos) {
	for dx := int32(-2); dx <= 2; dx++ {
		for dz := int32(-2); dz <= 2; dz++ {
			p := ChunkPos{pos[0] + dx, pos[1] + dz}
			near := dx >= -1 && dx <= 1 && dz >= -1 && dz <= 1
			if c, ok := w.chunks[p]; ok {
				c.entityTickingNeighbours = false
				if near {
					c.blockTickingNeighbours = false
				}
			}
			if near && len(w.deferredSpreads) != 0 {
				delete(w.deferredSpreads, p)
			}
		}
	}
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
			if !w.autoSaveDisabled.Load() {
				w.Save()
			}
		case <-w.closing:
			w.running.Done()
			return
		}
	}
}
