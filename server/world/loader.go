package world

import (
	"maps"
	"math"
	"sync"

	"github.com/go-gl/mathgl/mgl64"
)

var loaderOffsetCache sync.Map

// Loader implements the loading of the world. A loader can typically be moved around the world to load
// different parts of the world. An example usage is the player, which uses a loader to load chunks around it
// so that it can view them.
type Loader struct {
	r      int
	w      *World
	viewer Viewer

	mu        sync.RWMutex
	changeMu  sync.Mutex
	pos       ChunkPos
	loadQueue []ChunkPos
	loaded    map[ChunkPos]*Column
	pending   map[ChunkPos]struct{}

	activeRadius   int32
	activeRadiusSq int64

	closed   bool
	changing bool
}

// NewLoader creates a new loader using the chunk radius passed. Chunks beyond this radius from the position
// of the loader will never be loaded.
// The Viewer passed will handle the loading of chunks, including the viewing of entities that were loaded in
// those chunks.
func NewLoader(chunkRadius int, world *World, v Viewer) *Loader {
	l := &Loader{r: chunkRadius, loaded: make(map[ChunkPos]*Column), pending: make(map[ChunkPos]struct{}), viewer: v}
	l.world(world)
	return l
}

// World returns the World that the Loader is in.
func (l *Loader) World() *World {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.w
}

// ChangeWorld changes the World of the Loader. The currently loaded chunks are reset and any future loading
// is done from the new World.
func (l *Loader) ChangeWorld(tx *Tx, new *World) {
	l.changeMu.Lock()
	defer l.changeMu.Unlock()

	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return
	}
	old := l.w
	loaded := maps.Clone(l.loaded)
	l.changing = true
	clear(l.loaded)
	clear(l.pending)
	l.loadQueue = l.loadQueue[:0]
	l.mu.Unlock()

	removeLoaded := func(tx *Tx) {
		for pos := range loaded {
			tx.World().removeViewer(tx, pos, l)
		}
	}
	if tx.World() == old {
		removeLoaded(tx)
	} else {
		select {
		case <-old.exec(removeLoaded):
		case <-old.queueClosing:
		}
	}
	old.viewerMu.Lock()
	delete(old.viewers, l)
	old.viewerMu.Unlock()

	l.mu.Lock()
	l.world(new)
	l.changing = false
	l.mu.Unlock()
}

// ChangeRadius changes the maximum chunk radius of the Loader.
func (l *Loader) ChangeRadius(tx *Tx, new int) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.r = new
	l.evictUnused(tx)
	l.populateLoadQueue()
}

// Move moves the loader to the position passed. The position is translated to a chunk position to load
func (l *Loader) Move(tx *Tx, pos mgl64.Vec3) {
	l.mu.Lock()
	defer l.mu.Unlock()

	chunkPos := chunkPosFromVec3(pos)
	if chunkPos == l.pos {
		return
	}
	l.pos = chunkPos
	l.evictUnused(tx)
	l.populateLoadQueue()
}

// Load queues up to n chunks around the loader's centre, from the middle outwards, to be loaded in
// the background. The Viewer's ViewChunk is called for each chunk once ready, which may be after Load
// returns. Load does nothing for n <= 0.
func (l *Loader) Load(tx *Tx, n int) {
	l.mu.RLock()
	queueLen := len(l.loadQueue)
	l.mu.RUnlock()
	queued := 0
	for processed := 0; queued < n && processed < queueLen; processed++ {
		l.mu.Lock()
		if l.closed || l.changing || l.w == nil || l.w != tx.World() {
			l.mu.Unlock()
			return
		}
		if len(l.loadQueue) == 0 {
			l.mu.Unlock()
			break
		}
		pos := l.loadQueue[0]
		l.loadQueue = l.loadQueue[1:]
		if _, pending := l.pending[pos]; pending {
			l.loadQueue = append(l.loadQueue, pos)
			l.mu.Unlock()
			continue
		}
		w := l.w
		l.pending[pos] = struct{}{}
		l.loadQueue = append(l.loadQueue, pos)
		l.mu.Unlock()
		queued++

		if !w.loadChunkAsync(tx, pos, func(tx2 *Tx, col *Column) {
			l.viewChunk(tx2, pos, col)
		}) {
			l.mu.Lock()
			delete(l.pending, pos)
			l.queueLoad(pos)
			l.mu.Unlock()
		}
	}
}

// viewChunk passes a loaded chunk to the Loader's Viewer. If the chunk failed
// to load, it is queued to be loaded again.
func (l *Loader) viewChunk(tx *Tx, pos ChunkPos, c *Column) {
	l.mu.Lock()
	if l.closed || l.changing || l.viewer == nil || l.w == nil || l.w != tx.World() {
		l.mu.Unlock()
		return
	}
	delete(l.pending, pos)
	l.removeQueued(pos)
	if c == nil {
		l.queueLoad(pos)
		l.mu.Unlock()
		return
	}
	if _, ok := l.loaded[pos]; ok {
		l.mu.Unlock()
		return
	}
	if !l.withinLoadRadius(pos) {
		l.mu.Unlock()
		return
	}
	l.loaded[pos] = c
	w, viewer := l.w, l.viewer
	dim := w.Dimension()
	l.mu.Unlock()

	func() {
		defer func() {
			if r := recover(); r != nil {
				l.mu.Lock()
				if l.loaded[pos] == c {
					delete(l.loaded, pos)
					l.queueLoad(pos)
				}
				l.mu.Unlock()
				panic(r)
			}
		}()
		viewer.ViewChunk(pos, dim, c)
	}()

	l.mu.Lock()
	if l.closed || l.changing || l.viewer != viewer || l.w != w || l.loaded[pos] != c || !l.withinLoadRadius(pos) {
		l.mu.Unlock()
		return
	}
	w.addViewer(pos, c, l, viewer)
	l.mu.Unlock()

	w.viewChunkEntities(tx, c, viewer)
}

// Chunk attempts to return a chunk at the given ChunkPos. If the chunk is not loaded, the second return value will
// be false.
func (l *Loader) Chunk(pos ChunkPos) (*Column, bool) {
	l.mu.RLock()
	c, ok := l.loaded[pos]
	l.mu.RUnlock()
	return c, ok
}

// Close closes the loader. It unloads all chunks currently loaded for the viewer, and hides all entities that
// are currently shown to it.
func (l *Loader) Close(tx *Tx) {
	l.changeMu.Lock()
	defer l.changeMu.Unlock()

	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return
	}
	w := l.w
	loaded := l.loaded
	l.loaded = map[ChunkPos]*Column{}
	clear(l.pending)
	l.closed = true
	l.mu.Unlock()

	removeLoaded := func(tx *Tx) {
		for pos := range loaded {
			w.removeViewer(tx, pos, l)
		}
	}
	if tx.World() == w {
		removeLoaded(tx)
	} else {
		select {
		case <-w.exec(removeLoaded):
		case <-w.queueClosing:
		}
	}
	w.viewerMu.Lock()
	delete(w.viewers, l)
	w.viewerMu.Unlock()

	l.mu.Lock()
	l.viewer = nil
	l.mu.Unlock()
}

// world sets the loader's world, adds them to the world's viewer list, then starts populating the load queue.
// This is only here to get rid of duplicated code, ChangeWorld should be used instead of this.
func (l *Loader) world(new *World) {
	l.w = new
	l.w.addWorldViewer(l)
	l.populateLoadQueue()
}

// evictUnused gets rid of chunks in the loaded map which are no longer within the chunk radius of the loader,
// and should therefore be removed.
func (l *Loader) evictUnused(tx *Tx) {
	for pos := range l.loaded {
		diffX, diffZ := float64(pos[0])-float64(l.pos[0]), float64(pos[1])-float64(l.pos[1])
		if math.Hypot(diffX, diffZ) > float64(l.r) {
			delete(l.loaded, pos)
			l.w.removeViewer(tx, pos, l)
		}
	}
}

// withinLoadRadius checks if a chunk position is within the Loader's radius.
func (l *Loader) withinLoadRadius(pos ChunkPos) bool {
	return chunkDistance(pos, l.pos) <= int32(l.r)
}

// chunkDistance returns the rounded distance between two chunk positions.
func chunkDistance(a, b ChunkPos) int32 {
	diffX, diffZ := float64(a[0])-float64(b[0]), float64(a[1])-float64(b[1])
	return int32(math.Round(math.Sqrt(diffX*diffX + diffZ*diffZ)))
}

// queueLoad adds pos back to the load queue, unless it is already loaded,
// queued, or no longer within the radius of the Loader.
func (l *Loader) queueLoad(pos ChunkPos) {
	if l.closed || l.w == nil || !l.withinLoadRadius(pos) {
		return
	}
	if _, ok := l.loaded[pos]; ok {
		return
	}
	if _, ok := l.pending[pos]; ok {
		return
	}
	for _, queued := range l.loadQueue {
		if queued == pos {
			return
		}
	}
	l.loadQueue = append(l.loadQueue, pos)
}

func (l *Loader) removeQueued(pos ChunkPos) {
	for i, queued := range l.loadQueue {
		if queued == pos {
			l.loadQueue = append(l.loadQueue[:i], l.loadQueue[i+1:]...)
			return
		}
	}
}

// populateLoadQueue populates the load queue of the loader. This method is called once to create the order in
// which chunks around the position the loader is now in should be loaded. Chunks are ordered to be loaded
// from the middle outwards.
func (l *Loader) populateLoadQueue() {
	l.loadQueue = l.loadQueue[:0]
	for _, offset := range loaderOffsets(l.r) {
		pos := ChunkPos{offset[0] + l.pos[0], offset[1] + l.pos[1]}
		if _, ok := l.loaded[pos]; ok {
			continue
		}
		if _, ok := l.pending[pos]; ok {
			continue
		}
		l.loadQueue = append(l.loadQueue, pos)
	}
}

func loaderOffsets(radius int) []ChunkPos {
	if offsets, ok := loaderOffsetCache.Load(radius); ok {
		return offsets.([]ChunkPos)
	}

	r := int32(radius)
	queue := make(map[int32][]ChunkPos, radius+1)
	for x := -r; x <= r; x++ {
		for z := -r; z <= r; z++ {
			distance := int32(math.Round(math.Hypot(float64(x), float64(z))))
			if distance > r {
				continue
			}
			queue[distance] = append(queue[distance], ChunkPos{x, z})
		}
	}

	offsets := make([]ChunkPos, 0, len(queue)*8)
	for i := int32(0); i <= r; i++ {
		offsets = append(offsets, queue[i]...)
	}
	actual, _ := loaderOffsetCache.LoadOrStore(radius, offsets)
	return actual.([]ChunkPos)
}

func (l *Loader) activeArea(simRadius int32) loaderActiveArea {
	l.mu.Lock()
	target := simRadius
	if target < 0 {
		target = 0
	}
	if lr := int32(l.r); lr >= 0 && lr < target {
		target = lr
	}
	if l.activeRadius != target {
		l.activeRadius = target
		l.activeRadiusSq = int64(target) * int64(target)
	}
	area := loaderActiveArea{pos: l.pos, radius: l.activeRadius, radiusSq: l.activeRadiusSq}
	l.mu.Unlock()
	return area
}
