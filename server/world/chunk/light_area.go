package chunk

import (
	"bytes"
	"math"
	"slices"
	"sync"

	"github.com/df-mc/dragonfly/server/block/cube"
)

// lightArea represents a square area of N*N chunks. It is used for light calculation specifically.
type lightArea struct {
	br           BlockRegistry
	lr           LightRegistry
	dry          []LightProperties
	airRID       uint32
	air          LightProperties
	baseX, baseZ int
	c            []*Chunk
	w            int
	r            cube.Range
	// changed has bit i set once propagation changed light in c[i].
	changed uint16
}

// lightQueue is a FIFO ring buffer used during light propagation.
type lightQueue struct {
	nodes []lightNode
	head  int
	tail  int
	size  int
}

// initialLightQueueCapacity is the starting size for light propagation queues. A lightNode is 48 bytes on
// 64-bit platforms, so 1024 entries cost about 48 KiB. This avoids the first grow/copy for busier lighting
// runs while keeping the queue transient and able to grow for larger chunks.
const initialLightQueueCapacity = 1024

// newLightQueue creates an empty queue sized to capacity (at least 1).
func newLightQueue(capacity int) *lightQueue {
	if capacity < 1 {
		capacity = 1
	}
	return &lightQueue{nodes: make([]lightNode, capacity)}
}

// push appends a node to the tail, growing storage if full.
func (q *lightQueue) push(n lightNode) {
	if q.size == len(q.nodes) {
		q.grow()
	}
	q.nodes[q.tail] = n
	q.tail = (q.tail + 1) % len(q.nodes)
	q.size++
}

// pop removes and returns the oldest queued node.
func (q *lightQueue) pop() (lightNode, bool) {
	if q.size == 0 {
		return lightNode{}, false
	}
	n := q.nodes[q.head]
	q.head = (q.head + 1) % len(q.nodes)
	q.size--
	return n, true
}

// empty returns true when no nodes are queued.
func (q *lightQueue) empty() bool {
	return q.size == 0
}

// grow expands the ring buffer and reorders elements to start at index 0.
func (q *lightQueue) grow() {
	nodes := make([]lightNode, len(q.nodes)<<1)
	if q.head < q.tail {
		copy(nodes, q.nodes[q.head:q.tail])
	} else {
		n := copy(nodes, q.nodes[q.head:])
		copy(nodes[n:], q.nodes[:q.tail])
	}
	q.head = 0
	q.tail = q.size
	q.nodes = nodes
}

// levelQueue holds light nodes by level and hands out the brightest first,
// as LightEngine's LeveledPriorityQueue does, so that a block is lit by the
// brightest light reaching it before that light spreads on, rather than lit
// again and again by brighter light arriving later.
type levelQueue struct {
	buckets [16]*lightQueue
	top     int
	size    int
}

// newLevelQueue creates an empty levelQueue.
func newLevelQueue() *levelQueue {
	q := &levelQueue{}
	for i := range q.buckets {
		q.buckets[i] = newLightQueue(64)
	}
	return q
}

// push adds a node to the bucket of its level.
func (q *levelQueue) push(n lightNode) {
	q.buckets[n.level&15].push(n)
	q.top = max(q.top, int(n.level&15))
	q.size++
}

// pop removes a node of the highest level queued.
func (q *levelQueue) pop() (lightNode, bool) {
	for q.size > 0 {
		if n, ok := q.buckets[q.top].pop(); ok {
			q.size--
			return n, true
		}
		q.top--
	}
	return lightNode{}, false
}

// empty returns true when no nodes are queued.
func (q *levelQueue) empty() bool {
	return q.size == 0
}

// LightArea creates a lightArea with the lower corner of the lightArea at baseX and baseZ. The length of the Chunk
// slice must be a square of a number, so 1, 4, 9 etc.
func LightArea(c []*Chunk, baseX, baseZ int) *lightArea {
	w := int(math.Sqrt(float64(len(c))))
	if len(c) != w*w {
		panic("area must have a square chunk area")
	}
	a := &lightArea{
		br:    c[0].br,
		c:     c,
		w:     w,
		baseX: baseX << 4,
		baseZ: baseZ << 4,
		r:     c[0].r,
	}
	a.lr, _ = a.br.(LightRegistry)
	if a.lr != nil {
		a.dry = a.lr.DryLightProperties()
	}
	a.airRID = a.br.AirRuntimeID()
	a.air = a.cell(0, a.airRID, a.airRID)
	return a
}

// Fill executes the light 'filling' stage, where the lightArea is filled with light coming only from the
// individual chunks within the lightArea itself, without light crossing chunk borders.
func (a *lightArea) Fill() {
	a.initialiseLightSlices()
	queue := newLevelQueue()
	a.insertBlockLightNodes(queue)
	a.insertSkyLightNodes(queue)

	for !queue.empty() {
		a.propagate(queue)
	}
}

// Spread executes the light 'spreading' stage, where the lightArea has light spread from every Chunk into the
// neighbouring chunks. The neighbouring chunks must have passed the light 'filling' stage before this
// function is called for an lightArea that includes them. Spread returns the chunks whose light it changed as a
// mask: bit i is set if it changed the light of the chunk at index i of those the lightArea was made of.
func (a *lightArea) Spread() uint16 {
	a.changed = 0
	queue := newLevelQueue()
	a.insertLightSpreadingNodes(queue, BlockLight)
	a.insertLightSpreadingNodes(queue, SkyLight)

	for !queue.empty() {
		a.propagate(queue)
	}
	return a.changed
}

// light returns the light at a cube.Pos with the light type l.
func (a *lightArea) light(pos cube.Pos, l light) uint8 {
	return l.light(a.sub(pos), uint8(pos[0]&0xf), uint8(pos[1]&0xf), uint8(pos[2]&0xf))
}

// light sets the light at a cube.Pos with the light type l.
func (a *lightArea) setLight(pos cube.Pos, l light, v uint8) {
	l.setLight(a.sub(pos), uint8(pos[0]&0xf), uint8(pos[1]&0xf), uint8(pos[2]&0xf), v)
}

// iterSubChunks iterates over all blocks of the lightArea on a per-SubChunk basis. A filter function may be passed to
// specify if a SubChunk should be iterated over. If it returns false, it will not be iterated over.
func (a *lightArea) iterSubChunks(filter func(sub *SubChunk) bool, f func(pos cube.Pos)) {
	for cx := 0; cx < a.w; cx++ {
		for cz := 0; cz < a.w; cz++ {
			baseX, baseZ, c := a.baseX+(cx<<4), a.baseZ+(cz<<4), a.c[a.chunkIndex(cx, cz)]

			for index, sub := range c.sub {
				if !filter(sub) {
					continue
				}
				baseY := int(c.SubY(int16(index)))
				a.iterSubChunk(func(x, y, z int) {
					f(cube.Pos{x + baseX, y + baseY, z + baseZ})
				})
			}
		}
	}
}

// iterEdges iterates over all chunk edges within the lightArea and calls the function f with the cube.Pos at either
// side of the edge.
func (a *lightArea) iterEdges(filter func(a, b *SubChunk) bool, f func(a, b cube.Pos)) {
	minY, maxY := a.r[0]>>4, a.r[1]>>4
	// First iterate over chunk X, Y and Z, so we can filter out a complete 16x16 sheet of blocks if the
	// filter function returns false.
	for cu := 1; cu < a.w; cu++ {
		u := cu << 4
		for cv := 0; cv < a.w; cv++ {
			v := cv << 4
			for cy := minY; cy < maxY; cy++ {
				baseY := cy << 4

				xa, za := cube.Pos{a.baseX + u, baseY, a.baseZ + v}, cube.Pos{a.baseX + v, baseY, a.baseZ + u}
				xb, zb := xa.Side(cube.FaceWest), za.Side(cube.FaceNorth)

				addX, addZ := filter(a.sub(xa), a.sub(xb)), filter(a.sub(za), a.sub(zb))
				if !addX && !addZ {
					continue
				}
				// The order of these loops allows us to take care of block spreading over both the X and Z axis by
				// just swapping around the axes.
				for addV := 0; addV < 16; addV++ {
					for y := 0; y < 16; y++ {
						// Finally, iterate over the 16x16 sheet and actually do the per-block checks.
						if addX {
							f(xa.Add(cube.Pos{0, y, addV}), xb.Add(cube.Pos{0, y, addV}))
						}
						if addZ {
							f(za.Add(cube.Pos{addV, y}), zb.Add(cube.Pos{addV, y}))
						}
					}
				}
			}
		}
	}
}

// iterSubChunk iterates over the coordinates of a SubChunk (0-15 on all axes) and calls the function f for each of
// those coordinates.
func (a *lightArea) iterSubChunk(f func(x, y, z int)) {
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			for z := 0; z < 16; z++ {
				f(x, y, z)
			}
		}
	}
}

// props returns the light properties of the blocks at pos.
func (a *lightArea) props(pos cube.Pos) LightProperties {
	x, y, z, sub := uint8(pos[0]&0xf), uint8(pos[1]&0xf), uint8(pos[2]&0xf), a.sub(pos)
	switch len(sub.storages) {
	case 0:
		return a.air
	case 1:
		return a.cell(0, sub.storages[0].At(x, y, z), a.airRID)
	default:
		return a.cell(0, sub.storages[0].At(x, y, z), sub.storages[1].At(x, y, z))
	}
}

// cell returns the light properties of a cell with the blocks passed, from
// the LightRegistry if the registry is one, or else from the light values of
// the blocks.
func (a *lightArea) cell(state, fg, bg uint32) LightProperties {
	if state == 0 && bg == a.airRID && fg < uint32(len(a.dry)) {
		return a.dry[fg]
	}
	if a.lr != nil {
		return a.lr.LightProperties(state, fg, bg)
	}
	return LightProperties{
		Emission:  max(a.br.LightBlock(fg), a.br.LightBlock(bg)),
		Dampening: max(a.br.FilteringBlock(fg), a.br.FilteringBlock(bg)),
	}
}

var (
	fullLight    = bytes.Repeat([]byte{0xff}, 2048)
	fullLightPtr = &fullLight[0]
	noLight      = make([]uint8, 2048)
	noLightPtr   = &noLight[0]
)

// initialiseLightSlices initialises all light slices in the sub chunks of all chunks either with full light if there is
// no sub chunk with any blocks above it, or with empty light if there is. The sub chunks with empty light are then
// ready to be properly calculated.
func (a *lightArea) initialiseLightSlices() {
	for _, c := range a.c {
		index := len(c.sub) - 1
		for index >= 0 {
			sub := c.sub[index]
			if !sub.Empty() {
				// We've hit the topmost empty SubChunk.
				break
			}
			sub.skyLight = fullLight
			sub.blockLight = noLight
			index--
		}
		for index >= 0 {
			// Fill up the rest of the sub chunks with empty light. We will do light calculation for these sub chunks
			// later on.
			c.sub[index].skyLight = noLight
			c.sub[index].blockLight = noLight
			index--
		}
	}
}

// sub returns the SubChunk corresponding to a cube.Pos.
func (a *lightArea) sub(pos cube.Pos) *SubChunk {
	return a.chunk(pos).SubChunk(int16(pos[1]))
}

// chunk returns the Chunk corresponding to a cube.Pos.
func (a *lightArea) chunk(pos cube.Pos) *Chunk {
	x, z := pos[0]-a.baseX, pos[2]-a.baseZ
	return a.c[a.chunkIndex(x>>4, z>>4)]
}

// chunkIndex finds the index in the chunk slice of an lightArea for a Chunk at a specific x and z.
func (a *lightArea) chunkIndex(x, z int) int {
	return x + (z * a.w)
}

// Relight updates the light of the lightArea after the blocks at the
// positions passed changed in the light they emit or filter. Light that no
// longer reaches is removed and light is spread to where it now reaches, so
// that the result is the light Fill and Spread would compute. The positions
// must lie in the centre chunk of the lightArea.
func (a *lightArea) Relight(changed []cube.Pos) {
	// A block changed many times since the light was last brought up to
	// date is relit once.
	if len(changed) > 1 {
		seen := make(map[cube.Pos]struct{}, len(changed))
		unique := changed[:0:0]
		for _, pos := range changed {
			if _, ok := seen[pos]; !ok {
				seen[pos] = struct{}{}
				unique = append(unique, pos)
			}
		}
		changed = unique
	}
	a.relight(changed, BlockLight, func(pos cube.Pos) uint8 {
		return a.props(pos).Emission
	})
	heights := make(map[[2]int]int, 1)
	height := func(x, z int) int {
		h, ok := heights[[2]int{x, z}]
		if !ok {
			c := a.chunk(cube.Pos{x, 0, z})
			highestY := c.Range().Min()
			for index := range c.sub {
				if !c.sub[index].Empty() {
					highestY = int(c.SubY(int16(index))) + 15
				}
			}
			h = a.skySource(x, z, highestY)
			heights[[2]int{x, z}] = h
		}
		return h
	}
	sky := func(pos cube.Pos) uint8 {
		if pos[1] >= height(pos[0], pos[2]) {
			return 15
		}
		return 0
	}
	// Skylight of 15 fills every block from the height map up. Blocks of the
	// changed columns that became or stopped being part of that are changed
	// as well: no other block holds skylight of 15.
	skyChanged := slices.Clone(changed)
	columns := make(map[[2]int]struct{}, 1)
	for _, pos := range changed {
		if _, scanned := columns[[2]int{pos[0], pos[2]}]; scanned {
			continue
		}
		columns[[2]int{pos[0], pos[2]}] = struct{}{}
		h := height(pos[0], pos[2])
		for y := max(h, a.r.Min()); y <= a.r.Max() && a.light(cube.Pos{pos[0], y, pos[2]}, SkyLight) < 15; y++ {
			skyChanged = append(skyChanged, cube.Pos{pos[0], y, pos[2]})
		}
		for y := min(h, a.r.Max()+1) - 1; y >= a.r.Min() && a.light(cube.Pos{pos[0], y, pos[2]}, SkyLight) == 15; y-- {
			skyChanged = append(skyChanged, cube.Pos{pos[0], y, pos[2]})
		}
	}
	a.relight(skyChanged, SkyLight, sky)
}

// relight removes the light of type lt from the positions changed and the
// blocks they lit, then spreads light back from the sources found and the
// lit blocks around what was removed.
func (a *lightArea) relight(changed []cube.Pos, lt light, source func(pos cube.Pos) uint8) {
	removal, refill, spread := relightQueues.Get().(*lightQueue), relightQueues.Get().(*lightQueue), relightLevelQueues.Get().(*levelQueue)
	defer relightQueues.Put(removal)
	defer relightQueues.Put(refill)
	defer relightLevelQueues.Put(spread)
	for _, pos := range changed {
		if !a.inside(pos) {
			continue
		}
		removal.push(node(pos, a.light(pos, lt), lt))
		a.setLight(pos, lt, 0)
	}
	for !removal.empty() {
		n, _ := removal.pop()
		if level := source(n.pos); level > 0 {
			spread.push(node(n.pos, level, lt))
		}
		for _, face := range cube.Faces() {
			pos := n.pos.Side(face)
			if !a.inside(pos) {
				continue
			}
			level := a.light(pos, lt)
			if level == 0 {
				continue
			}
			if level < n.level {
				// The block may have been lit by the one removed.
				a.setLight(pos, lt, 0)
				removal.push(node(pos, level, lt))
			} else {
				refill.push(node(pos, level, lt))
			}
		}
	}
	for !refill.empty() {
		n, _ := refill.pop()
		a.spreadFrom(spread, n.pos, lt, a.light(n.pos, lt))
	}
	for !spread.empty() {
		a.propagate(spread)
	}
}

// relightQueues holds the queues relight empties, so that relighting after
// every block change does not allocate them anew.
var relightQueues = sync.Pool{New: func() any { return newLightQueue(initialLightQueueCapacity) }}

// relightLevelQueues holds the level queues relight empties.
var relightLevelQueues = sync.Pool{New: func() any { return newLevelQueue() }}

// inside reports if pos lies within the lightArea.
func (a *lightArea) inside(pos cube.Pos) bool {
	return pos[1] >= a.r.Min() && pos[1] <= a.r.Max() && pos[0] >= a.baseX && pos[2] >= a.baseZ && pos[0] < a.baseX+a.w*16 && pos[2] < a.baseZ+a.w*16
}
