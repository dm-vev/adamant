package world

import (
	"slices"

	"github.com/df-mc/dragonfly/server/block/cube"
)

// circuitPosOrder reproduces the list traversal of the native BlockPos hash
// maps, rather than relying on Go's randomized map iteration. Distinct keys in
// the same bucket are inserted before its first node; a new bucket appends.
// See CircuitSceneGraph::add (0x66ce1f0), update (0x66ce960), and the native
// rehash helper at 0x1cc1ec0. Erasing/clearing does not shrink the bucket count.
type circuitPosOrder struct {
	mask       uint64
	nodes      map[cube.Pos]*circuitPosNode
	buckets    map[uint64]*circuitPosNode
	head, tail *circuitPosNode
}

type circuitPosNode struct {
	pos        cube.Pos
	hash       uint64
	prev, next *circuitPosNode
}

func circuitPosHash(pos cube.Pos) uint64 {
	var hash uint64
	for _, coordinate := range pos {
		part := uint64(0xcbf29ce484222325)
		for shift := range 4 {
			part = (part ^ uint64(uint8(uint32(coordinate)>>(shift*8)))) * 0x100000001b3
		}
		hash ^= part + 0x9e3779b9 + (hash << 6) + (hash >> 2)
	}
	return hash
}

func (o *circuitPosOrder) insert(pos cube.Pos) {
	if o.nodes[pos] != nil {
		return
	}
	if o.nodes == nil {
		o.mask = 7
		o.nodes = make(map[cube.Pos]*circuitPosNode)
		o.buckets = make(map[uint64]*circuitPosNode)
	}
	if uint64(len(o.nodes)+1) > o.mask+1 {
		count := o.mask + 1
		if count < 512 {
			count *= 8
		} else {
			count *= 2
		}
		o.mask = count - 1
		clear(o.buckets)
		next := o.head
		o.head, o.tail = nil, nil
		for next != nil {
			node := next
			next = next.next
			o.link(node)
		}
	}
	node := &circuitPosNode{pos: pos, hash: circuitPosHash(pos)}
	o.nodes[pos] = node
	o.link(node)
}

func (o *circuitPosOrder) link(node *circuitPosNode) {
	bucket := node.hash & o.mask
	if first := o.buckets[bucket]; first != nil {
		node.prev, node.next = first.prev, first
		if first.prev == nil {
			o.head = node
		} else {
			first.prev.next = node
		}
		first.prev = node
	} else {
		node.prev, node.next = o.tail, nil
		if o.tail == nil {
			o.head = node
		} else {
			o.tail.next = node
		}
		o.tail = node
	}
	o.buckets[bucket] = node
}

func (o *circuitPosOrder) remove(pos cube.Pos) {
	node := o.nodes[pos]
	if node == nil {
		return
	}
	if node.prev == nil {
		o.head = node.next
	} else {
		node.prev.next = node.next
	}
	if node.next == nil {
		o.tail = node.prev
	} else {
		node.next.prev = node.prev
	}
	bucket := node.hash & o.mask
	if o.buckets[bucket] == node {
		if node.next != nil && node.next.hash&o.mask == bucket {
			o.buckets[bucket] = node.next
		} else {
			delete(o.buckets, bucket)
		}
	}
	delete(o.nodes, pos)
}

func (o *circuitPosOrder) clear() {
	clear(o.nodes)
	clear(o.buckets)
	o.head, o.tail = nil, nil
}

// circuitTraversal keeps the pending-add map and active chunk vectors separate.
// Static producers participate in registration even though they do not own a
// RedstoneCircuitState. Empty chunk entries survive ordinary block removal,
// as in CircuitSceneGraph::update; unloading discards world-owned state.
type circuitTraversal struct {
	pending, chunks circuitPosOrder
	kinds           map[cube.Pos]uint64
	entries         map[cube.Pos][]cube.Pos
}

func circuitChunkOrigin(pos cube.Pos) cube.Pos { return cube.Pos{pos[0] &^ 15, 0, pos[2] &^ 15} }

func (o *circuitTraversal) replace(pos cube.Pos, block Block) {
	_, capacitor := block.(RedstoneCircuitComponent)
	relevant := capacitor || isRedstoneRelevant(block)
	previous, existed := o.kinds[pos]
	var kind uint64
	if relevant {
		kind, _ = block.Hash()
		if existed && previous == kind {
			return
		}
	} else if !existed {
		return
	}
	if o.kinds == nil {
		o.kinds = make(map[cube.Pos]uint64)
		o.entries = make(map[cube.Pos][]cube.Pos)
	}
	chunk := circuitChunkOrigin(pos)
	o.entries[chunk] = slices.DeleteFunc(o.entries[chunk], func(p cube.Pos) bool { return p == pos })
	if !relevant {
		o.pending.remove(pos)
		delete(o.kinds, pos)
		return
	}
	o.kinds[pos] = kind
	o.pending.insert(pos)
}

func (o *circuitTraversal) flush() {
	for node := o.pending.head; node != nil; node = node.next {
		chunk := circuitChunkOrigin(node.pos)
		o.chunks.insert(chunk)
		o.entries[chunk] = append(o.entries[chunk], node.pos)
	}
	o.pending.clear()
}

func (o *circuitTraversal) positions(states map[cube.Pos]RedstoneCircuitState) []cube.Pos {
	positions := make([]cube.Pos, 0, len(states))
	for node := o.chunks.head; node != nil; node = node.next {
		for _, pos := range o.entries[node.pos] {
			if states[pos] != nil {
				positions = append(positions, pos)
			}
		}
	}
	return positions
}

func (o *circuitTraversal) removeChunk(chunk ChunkPos) {
	origin := cube.Pos{int(chunk[0]) * 16, 0, int(chunk[1]) * 16}
	for pos := range o.kinds {
		if circuitChunkOrigin(pos) == origin {
			o.pending.remove(pos)
			delete(o.kinds, pos)
		}
	}
	delete(o.entries, origin)
	o.chunks.remove(origin)
}
