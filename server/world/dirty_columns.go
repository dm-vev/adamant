package world

import "sync"

// dirtyColumns lists the positions of the columns of a world modified or
// touched since they were last saved, so that a capture walks them rather
// than every column loaded. A column is listed once, from its first change
// since its last save until a capture takes it (Column.listed). The list
// holds positions, not columns, so that a column unloaded meanwhile is
// freed: a capture skips a position whose column is not listed. Columns
// change on the owner, on region workers and, as they finish generating,
// on generation workers, hence the mutex: it is taken once a column a save,
// not once a change. The flags of a column (Column.modified, touched and
// listed) are atomic for the same reason: a worker finishing a column sets
// them while the owner may touch it, for an entity added to it.
type dirtyColumns struct {
	mu   sync.Mutex
	list []ChunkPos
}

// add lists pos.
func (d *dirtyColumns) add(pos ChunkPos) {
	d.mu.Lock()
	d.list = append(d.list, pos)
	d.mu.Unlock()
}

// take returns the positions listed and empties the list.
func (d *dirtyColumns) take() []ChunkPos {
	d.mu.Lock()
	defer d.mu.Unlock()
	list := d.list
	d.list = nil
	return list
}

// setModified marks c modified: a block, biome, entity or scheduled tick of
// it changed. The next save writes all of it.
func (c *Column) setModified() {
	c.modified.Store(true)
	c.enlist()
}

// touch marks c touched: what it keeps beside its blocks may have changed.
// The next save writes its entities, block entities, scheduled ticks and
// inhabited time. It costs a branch once c was touched.
func (c *Column) touch() {
	if !c.touched.Load() {
		c.touched.Store(true)
		c.enlist()
	}
}

// enlist lists c among the columns of its world to save, unless it is
// listed or was not placed in a world yet: placing it lists it then if it
// needs saving (World.placeColumn).
func (c *Column) enlist() {
	if c.dirty == nil || c.listed.Load() || !c.listed.CompareAndSwap(false, true) {
		return
	}
	c.dirty.add(c.pos)
}

// placeColumn places col at pos in the world, replacing the column there,
// and lists it if it already needs saving, as a column taken back from
// staging does.
func (w *World) placeColumn(pos ChunkPos, col *Column) {
	col.pos, col.dirty = pos, &w.dirty
	w.chunks[pos] = col
	if col.needsSave() {
		col.enlist()
	}
}

// touchEntity touches the column of the entity of s, which a transaction
// is about to use: whatever it does with it, the entity may change without
// marking its column modified. The column is looked up once an entity a
// capture, as touchedIn tells.
func (w *World) touchEntity(s *entityState) {
	if s.touchedIn == w.saveEpoch+1 {
		return
	}
	s.touchedIn = w.saveEpoch + 1
	if c := w.chunks[s.pos]; c != nil {
		c.touch()
	}
}
