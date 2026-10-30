package world

import (
	"sync"

	"github.com/df-mc/dragonfly/server/block/cube"
)

// BlockUpdate is a change of a block on a layer of its position.
type BlockUpdate struct {
	Pos   cube.Pos
	Block Block
	Layer int
}

// BlockUpdates are the changes of the blocks of a column that its viewers are
// shown together once the transaction that made them ends: the last change of
// each position and layer, in the order they were first changed. Every viewer
// of the column is shown the same BlockUpdates, one after the other on the
// goroutine of the world.
type BlockUpdates struct {
	Updates []BlockUpdate
	// Shared is left to the viewers: the first may keep there what it made
	// of the updates for the others to use.
	Shared any
}

type blockUpdateKey struct {
	pos   cube.Pos
	layer uint8
}

// pendingBlockUpdates are the changes of the blocks of a column its viewers
// are yet to be shown, with the index of the change of each position and
// layer among them.
type pendingBlockUpdates struct {
	updates []BlockUpdate
	index   map[blockUpdateKey]int
}

// blockUpdateQueue holds the columns with changes of blocks their viewers are
// yet to be shown. Regions ticking at once add to it.
type blockUpdateQueue struct {
	mu      sync.Mutex
	columns []*Column
}

// viewBlockUpdate notes a change of a block of a column for its viewers, who
// are shown it with the other changes of the column once the transaction
// ends, as Java's ChunkHolder sends the changes of a section together.
func (w *World) viewBlockUpdate(c *Column, pos cube.Pos, b Block, layer int) {
	if len(c.viewers) == 0 {
		return
	}
	if _, ordered := b.(UpdateNBTer); ordered {
		for v := range c.viewers {
			v.ViewBlockUpdate(pos, b, layer)
		}
		return
	}
	p := &c.pendingBlocks
	key := blockUpdateKey{pos: pos, layer: uint8(layer)}
	if i, ok := p.index[key]; ok {
		p.updates[i].Block = b
		return
	}
	if len(p.updates) == 0 {
		w.blockUpdates.mu.Lock()
		w.blockUpdates.columns = append(w.blockUpdates.columns, c)
		w.blockUpdates.mu.Unlock()
	}
	if p.index == nil {
		p.index = make(map[blockUpdateKey]int)
	}
	p.index[key] = len(p.updates)
	p.updates = append(p.updates, BlockUpdate{Pos: pos, Block: b, Layer: layer})
}

// flushBlockUpdates shows the viewers of columns the changes of their blocks
// noted since it last did.
func (w *World) flushBlockUpdates() {
	w.blockUpdates.mu.Lock()
	columns := w.blockUpdates.columns
	w.blockUpdates.columns = nil
	w.blockUpdates.mu.Unlock()
	for _, c := range columns {
		p := &c.pendingBlocks
		if len(p.updates) == 0 {
			continue
		}
		u := &BlockUpdates{Updates: p.updates}
		p.updates = nil
		clear(p.index)
		for v := range c.viewers {
			v.ViewBlockUpdates(u)
		}
	}
}
