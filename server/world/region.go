package world

import (
	"maps"
	"runtime/debug"
	"sync"
	"sync/atomic"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/parallel"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/go-gl/mathgl/mgl64"
)

// Regions let the entities of parts of a world far apart tick at the same
// time, each part on a goroutine of its own, while the world owner waits, as
// Folia ticks the regions of a level.
//
// The columns whose entities tick actively are grouped by cells of
// regionCell chunks. Cells next to each other, diagonally included, join one
// region, and so do the cells any loader views, so that the columns of two
// regions lie more than regionCell chunks apart and no viewer is shown
// anything by two regions. A region owns the columns within regionReach
// chunks of its cells: the columns two regions own never meet, and a region
// only reads and writes the columns it owns. The entities in them are the
// region's: other regions do not find them. An entity of a region looking
// further than regionReach chunks away, which none does in vanilla, finds
// nothing there.
//
// During the phase the chunk map does not change. A region reads the owned
// columns that are loaded and settled and air in the others; it never
// loads, generates or completes a column. State of the world regions share,
// as the entity lookups, scheduled ticks, points of interest and redstone,
// is changed and read under one lock, held by one region at a time and only
// while a region does not wait for anything else. What a region cannot do
// at once, as moving an entity into a column not loaded yet or showing
// something to every viewer of the world, it leaves to the owner, which
// does it after the phase, region after region.
const (
	regionCell  = 16
	regionReach = 8
)

// regionCellPos is the position of a cell of regionCell by regionCell
// chunks.
type regionCellPos [2]int32

// regionCellOf returns the cell of the chunk at pos.
func regionCellOf(pos ChunkPos) regionCellPos {
	return regionCellPos{regionFloorDiv(pos[0], regionCell), regionFloorDiv(pos[1], regionCell)}
}

func regionFloorDiv(a, b int32) int32 {
	q := a / b
	if a%b != 0 && (a < 0) != (b < 0) {
		q--
	}
	return q
}

// region is a part of a world whose entities tick on one goroutine in a
// region phase.
type region struct {
	index int
	refs  []columnRef
	tx    *Tx
	// columns caches the owned settled columns the region read, and last the
	// one read last, at lastPos.
	columns map[ChunkPos]*Column
	lastPos ChunkPos
	last    *Column
	// void is the column of air the region reads where it has no column.
	void *Column
	// locked counts the lockShared calls of the region not unlocked yet.
	locked int
	// deferred is what the owner does for the region after the phase.
	deferred []func(tx *Tx)
	// javaRandom and lootSequences are the streams the region draws from in
	// place of the world's, seeded from them.
	javaRandom    *vanilla.Random
	lootSequences map[string]*vanilla.Random
	// locals are the values Tx.Local keeps for the worker ticking the
	// region.
	locals map[any]any
	// playersElsewhere is set if players that are not spectators are in
	// other regions or in none.
	playersElsewhere bool
}

// regionState is what a world keeps for its region phases.
type regionState struct {
	// active is set while a region phase runs.
	active atomic.Bool
	// mu guards the state of the world regions share during a phase.
	mu sync.Mutex
	// cells holds the region of every cell with a column ticking actively.
	// It is set before a phase and only read during it.
	cells map[regionCellPos]*region
	// warnedReach limits the warning of a region reaching too far to one.
	warnedReach atomic.Bool
	// phases counts the region phases run.
	phases atomic.Int64
}

// lockShared locks the state of the world regions share if tx belongs to a
// region, until the matching unlockShared. Calls may nest. A region holding
// the lock must not wait for anything another region might hold.
func (tx *Tx) lockShared() {
	if r := tx.region; r != nil {
		if r.locked == 0 {
			tx.w.regions.mu.Lock()
		}
		r.locked++
	}
}

// unlockShared undoes the last lockShared.
func (tx *Tx) unlockShared() {
	if r := tx.region; r != nil {
		if r.locked--; r.locked == 0 {
			tx.w.regions.mu.Unlock()
		}
	}
}

// PlayersElsewhere reports whether players that are not spectators are in
// the world of tx without Players yielding them: in a region phase, those of
// other regions, every one further than 256 blocks from the entities of the
// region. It is always false for a transaction of the world owner. Checks
// for the nearest player at any distance, as a mob despawning far from
// players, use it.
func (tx *Tx) PlayersElsewhere() bool {
	return tx.region != nil && tx.region.playersElsewhere
}

// WholeWorld runs f with a transaction whose Players and Entities yield
// every entity of the world: at once if tx is a transaction of the world
// owner, or once the region phase ends if tx is one of a region. Telling
// every player of the world something, as a death message, uses it.
func (tx *Tx) WholeWorld(f func(tx *Tx)) {
	tx.deferToOwner(f)
}

// deferToOwner runs f with a transaction of the world owner: at once if tx
// is one, or after the region phase otherwise.
func (tx *Tx) deferToOwner(f func(tx *Tx)) {
	if r := tx.region; r != nil {
		r.deferred = append(r.deferred, f)
		return
	}
	f(tx)
}

// regionOwner returns the region owning the column at pos during a region
// phase, or nil if no region does.
func (w *World) regionOwner(pos ChunkPos) *region {
	cells := w.regions.cells
	x0, x1 := regionFloorDiv(pos[0]-regionReach, regionCell), regionFloorDiv(pos[0]+regionReach, regionCell)
	z0, z1 := regionFloorDiv(pos[1]-regionReach, regionCell), regionFloorDiv(pos[1]+regionReach, regionCell)
	for x := x0; x <= x1; x++ {
		for z := z0; z <= z1; z++ {
			if r, ok := cells[regionCellPos{x, z}]; ok {
				return r
			}
		}
	}
	return nil
}

// owns reports whether the region owns the column at pos.
func (r *region) owns(w *World, pos ChunkPos) bool {
	if _, ok := r.columns[pos]; ok {
		return true
	}
	return w.regionOwner(pos) == r
}

// loaded returns the owned column at pos if it is loaded, ready or not.
func (r *region) loaded(w *World, pos ChunkPos) (*Column, bool) {
	if c, ok := r.columns[pos]; ok {
		return c, true
	}
	c, ok := w.chunks[pos]
	if !ok {
		return nil, false
	}
	if w.regionOwner(pos) != r {
		r.reach(w)
		return nil, false
	}
	if c.settled {
		r.columns[pos] = c
	}
	return c, true
}

// column returns the owned column at pos if it is loaded and settled, or
// nil.
func (r *region) column(w *World, pos ChunkPos) *Column {
	if r.last != nil && r.lastPos == pos {
		return r.last
	}
	c, ok := r.columns[pos]
	if !ok {
		if c, ok = r.loaded(w, pos); !ok || !c.settled {
			return nil
		}
	}
	r.lastPos, r.last = pos, c
	return c
}

// columnOrVoid returns the owned column at pos if it is loaded and settled,
// or a column of air of the region's own otherwise.
func (r *region) columnOrVoid(w *World, pos ChunkPos) *Column {
	if c := r.column(w, pos); c != nil {
		return c
	}
	if r.void == nil {
		r.void = newColumn(chunk.New(w.conf.Blocks, w.Range()))
		r.void.fillLight(pos)
		r.void.markReady()
		r.void.poisIndexed, r.void.circuitsIndexed, r.void.settled = true, true, true
	}
	return r.void
}

// reach records that the region read a loaded column it does not own.
func (r *region) reach(w *World) {
	if !w.regions.warnedReach.Swap(true) {
		w.conf.Log.Debug("an entity of a region reached a column the region does not own", "region", r.index, "stack", string(debug.Stack()))
	}
}

// ownsEntity reports whether the entity of handle is in a column the region
// owns. The region must hold the shared lock.
func (r *region) ownsEntity(w *World, handle *EntityHandle) bool {
	s := handle.state
	return s != nil && r.owns(w, s.pos)
}

// regionsOf groups the columns ticking actively passed, kept in the order
// passed, into regions far enough apart to tick at once. The loaders passed
// join the cells they view.
func regionsOf(refs []columnRef, loaders []*Loader) [][]columnRef {
	cells := make(map[regionCellPos]int, len(refs)/4+1)
	var order []regionCellPos
	for _, ref := range refs {
		c := regionCellOf(ref.pos)
		if _, ok := cells[c]; !ok {
			cells[c] = len(order)
			order = append(order, c)
		}
	}
	parent := make([]int, len(order))
	for i := range parent {
		parent[i] = i
	}
	find := func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	union := func(i, j int) {
		if a, b := find(i), find(j); a != b {
			parent[max(a, b)] = min(a, b)
		}
	}
	for i, c := range order {
		for dx := int32(-1); dx <= 1; dx++ {
			for dz := int32(-1); dz <= 1; dz++ {
				if j, ok := cells[regionCellPos{c[0] + dx, c[1] + dz}]; ok {
					union(i, j)
				}
			}
		}
	}
	for _, l := range loaders {
		// The loader views the columns within its radius: the cells owning
		// any of them join.
		l.mu.RLock()
		pos, radius := l.pos, int32(l.r)
		l.mu.RUnlock()
		first := -1
		lo := regionCellOf(ChunkPos{pos[0] - radius - regionReach, pos[1] - radius - regionReach})
		hi := regionCellOf(ChunkPos{pos[0] + radius + regionReach, pos[1] + radius + regionReach})
		for x := lo[0]; x <= hi[0]; x++ {
			for z := lo[1]; z <= hi[1]; z++ {
				j, ok := cells[regionCellPos{x, z}]
				if !ok {
					continue
				}
				if first < 0 {
					first = j
				} else {
					union(first, j)
				}
			}
		}
	}
	groups := make(map[int]int)
	var out [][]columnRef
	for _, ref := range refs {
		root := find(cells[regionCellOf(ref.pos)])
		g, ok := groups[root]
		if !ok {
			g = len(out)
			groups[root] = g
			out = append(out, nil)
		}
		out[g] = append(out[g], ref)
	}
	return out
}

// tickRegions ticks the entities of the columns passed, which tick
// actively, region after region at once if they make more than one region,
// and reports whether they did. The owner then does what the regions left
// to it, region after region.
func (t ticker) tickRegions(tx *Tx, tick int64, refs []columnRef, loaders []*Loader) bool {
	groups := regionsOf(refs, loaders)
	if len(groups) < 2 {
		return false
	}
	w := tx.World()
	// Regions read settled columns only: the owner completes the columns
	// that became ready first.
	w.settleReadyColumns(tx)
	regions := make([]*region, len(groups))
	cells := make(map[regionCellPos]*region, len(groups)*2)
	for i, refs := range groups {
		r := &region{index: i, refs: refs, columns: make(map[ChunkPos]*Column, len(refs)*4)}
		r.tx = &Tx{w: w, region: r}
		regions[i] = r
		for _, ref := range refs {
			cells[regionCellOf(ref.pos)] = r
		}
	}
	w.regions.cells = cells
	for handle := range w.players {
		s := handle.state
		if s == nil {
			continue
		}
		if g, ok := s.ent.(interface{ GameMode() GameMode }); ok && g.GameMode() == GameModeSpectator {
			continue
		}
		owner := w.regionOwner(s.pos)
		for _, r := range regions {
			if r != owner {
				r.playersElsewhere = true
			}
		}
	}
	w.regions.phases.Add(1)
	w.regions.active.Store(true)
	func() {
		defer func() {
			w.regions.active.Store(false)
			w.regions.cells = nil
		}()
		w.growWorkerLocals()
		tickRegion := func(worker, i int) {
			r := regions[i]
			r.locals = w.workerLocals[worker]
			for _, ref := range r.refs {
				t.tickEntityColumn(r.tx, tick, ref.pos, ref.col, true)
			}
			if r.locked != 0 {
				panic("region finished holding the shared lock")
			}
		}
		if w.conf.Synchronous {
			// A synchronous world runs on the goroutine driving it, which
			// transactions its entities start run on at once: its regions
			// tick one after another.
			for i := range regions {
				tickRegion(0, i)
			}
			return
		}
		parallel.ForWorkers(len(regions), 1, tickRegion)
	}()
	for _, r := range regions {
		r.tx.closed = true
		for _, f := range r.deferred {
			f(tx)
		}
		tx.deferred = append(tx.deferred, r.tx.deferred...)
	}
	return true
}

// settleReadyColumns completes the columns that became ready since they
// were last settled.
func (w *World) settleReadyColumns(tx *Tx) {
	for pos, c := range w.unsettled {
		if cur, ok := w.chunks[pos]; !ok || cur != c || c.settled {
			delete(w.unsettled, pos)
			continue
		}
		if c.Ready() {
			tx.chunk(pos)
		}
	}
}

// markUnsettled records a column just put in the chunk map at pos that is
// not settled yet.
func (w *World) markUnsettled(pos ChunkPos, c *Column) {
	if w.conf.Regions && !c.settled {
		w.unsettled[pos] = c
	}
}

// loadedColumn returns the column at pos if it is loaded, ready or not,
// without loading it. In a region phase it only returns a column the region
// owns.
func (tx *Tx) loadedColumn(pos ChunkPos) (*Column, bool) {
	if r := tx.region; r != nil {
		return r.loaded(tx.w, pos)
	}
	c, ok := tx.w.chunks[pos]
	return c, ok
}

// readyColumn returns the column at pos if it is loaded and ready, without
// loading it. In a region phase it only returns a settled column the region
// owns.
func (tx *Tx) readyColumn(pos ChunkPos) (*Column, bool) {
	if r := tx.region; r != nil {
		c := r.column(tx.w, pos)
		return c, c != nil
	}
	c, ok := tx.w.chunks[pos]
	return c, ok && c.Ready()
}

// blockLoaded returns the block at pos if its column is loaded and ready,
// as World.blockLoaded does, reading only owned columns in a region phase.
func (tx *Tx) blockLoaded(pos cube.Pos) (Block, bool) {
	if tx.region == nil {
		return tx.w.blockLoaded(pos)
	}
	w := tx.w
	if pos.OutOfBounds(w.ra) {
		return w.conf.Blocks.Air(), false
	}
	c, ok := tx.readyColumn(chunkPosFromBlockPos(pos))
	if !ok {
		return w.conf.Blocks.Air(), false
	}
	return w.loadedBlockIn(c, pos), true
}

// entityInWorld reports whether the entity of handle is in the world of tx
// and, in a region phase, in a column the region owns.
func (tx *Tx) entityInWorld(handle *EntityHandle) bool {
	if handle == nil {
		return false
	}
	if r := tx.region; r != nil {
		tx.lockShared()
		defer tx.unlockShared()
		return handle.w == tx.w && r.ownsEntity(tx.w, handle)
	}
	return handle.w == tx.w && handle.state != nil
}

// inRegionPhase reports whether a region phase of the world runs.
func (w *World) inRegionPhase() bool {
	return w.regions.active.Load()
}

// regionWrites reports whether the region of tx may change the block at pos
// at once: its column is the region's and settled.
func (tx *Tx) regionWrites(pos cube.Pos) bool {
	return tx.region.column(tx.w, chunkPosFromBlockPos(pos)) != nil
}

// lightReadyColumn returns the column at pos if it is loaded, ready and lit,
// as World.loadedChunk does, in a region phase only if the region owns it.
func (tx *Tx) lightReadyColumn(pos ChunkPos) (*Column, bool) {
	if r := tx.region; r != nil {
		c := r.column(tx.w, pos)
		return c, c != nil && c.lightReady.Load()
	}
	return tx.w.loadedChunk(pos)
}

// ridCache caches a value for each block runtime ID. Regions read it at
// once: it is replaced rather than changed when a value is added.
type ridCache[T any] struct {
	values atomic.Pointer[map[uint32]T]
	mu     sync.Mutex
}

// load returns the value cached for rid.
func (c *ridCache[T]) load(rid uint32) (T, bool) {
	if values := c.values.Load(); values != nil {
		v, ok := (*values)[rid]
		return v, ok
	}
	var zero T
	return zero, false
}

// store caches v for rid.
func (c *ridCache[T]) store(rid uint32, v T) {
	c.mu.Lock()
	defer c.mu.Unlock()
	next := make(map[uint32]T, 1)
	if values := c.values.Load(); values != nil {
		next = maps.Clone(*values)
	}
	next[rid] = v
	c.values.Store(&next)
}

// Local returns the value kept under key for the goroutine running tx,
// calling newValue to make it the first time. The world owner keeps one set
// of values, and each worker of the parallel phases of the world one of its
// own, kept from one phase to the next, so that a value is never used by two
// goroutines at once: caches the owner keeps for the world, such as Java's
// PathTypeCache, stay safe when regions tick at once.
func (tx *Tx) Local(key any, newValue func() any) any {
	if tx.lastLocal.value != nil && tx.lastLocal.key == key {
		return tx.lastLocal.value
	}
	locals := tx.w.workerLocals[0]
	if r := tx.region; r != nil {
		locals = r.locals
	}
	return tx.lastLocal.find(locals, key, newValue)
}

// Local returns the value kept under key for the worker reading through v,
// as Tx.Local does for a transaction.
func (v *ReadView) Local(key any, newValue func() any) any {
	if v.lastLocal.value != nil && v.lastLocal.key == key {
		return v.lastLocal.value
	}
	return v.lastLocal.find(v.locals, key, newValue)
}

// lastLocal remembers the value Local returned last, as callers often ask
// for the same one many times in a row.
type lastLocal struct {
	key, value any
}

// find returns the value under key in locals, making it with newValue if
// there is none, and remembers it.
func (l *lastLocal) find(locals map[any]any, key any, newValue func() any) any {
	v, ok := locals[key]
	if !ok {
		v = newValue()
		locals[key] = v
	}
	l.key, l.value = key, v
	return v
}

// growWorkerLocals makes sure workerLocals has the values of every worker
// of a parallel phase. The first are the owner's: the first worker of a
// phase runs on the owner.
func (w *World) growWorkerLocals() {
	for len(w.workerLocals) < max(1, parallel.Workers()) {
		w.workerLocals = append(w.workerLocals, make(map[any]any))
	}
}

// regionHandler is the Handler of a world as regions call it: each call
// holds the shared lock, so that a Handler is never called by two regions
// at once.
type regionHandler struct {
	h Handler
}

func (r regionHandler) HandleLiquidFlow(ctx *Context, from, into cube.Pos, liquid Liquid, replaced Block) {
	ctx.lockShared()
	defer ctx.unlockShared()
	r.h.HandleLiquidFlow(ctx, from, into, liquid, replaced)
}

func (r regionHandler) HandleLiquidDecay(ctx *Context, pos cube.Pos, before, after Liquid) {
	ctx.lockShared()
	defer ctx.unlockShared()
	r.h.HandleLiquidDecay(ctx, pos, before, after)
}

func (r regionHandler) HandleLiquidHarden(ctx *Context, hardenedPos cube.Pos, liquidHardened, otherLiquid, newBlock Block) {
	ctx.lockShared()
	defer ctx.unlockShared()
	r.h.HandleLiquidHarden(ctx, hardenedPos, liquidHardened, otherLiquid, newBlock)
}

func (r regionHandler) HandleSound(ctx *Context, s Sound, pos mgl64.Vec3) {
	ctx.lockShared()
	defer ctx.unlockShared()
	r.h.HandleSound(ctx, s, pos)
}

func (r regionHandler) HandleFireSpread(ctx *Context, from, to cube.Pos) {
	ctx.lockShared()
	defer ctx.unlockShared()
	r.h.HandleFireSpread(ctx, from, to)
}

func (r regionHandler) HandleBlockBurn(ctx *Context, pos cube.Pos) {
	ctx.lockShared()
	defer ctx.unlockShared()
	r.h.HandleBlockBurn(ctx, pos)
}

func (r regionHandler) HandleCropTrample(ctx *Context, pos cube.Pos) {
	ctx.lockShared()
	defer ctx.unlockShared()
	r.h.HandleCropTrample(ctx, pos)
}

func (r regionHandler) HandleLeavesDecay(ctx *Context, pos cube.Pos) {
	ctx.lockShared()
	defer ctx.unlockShared()
	r.h.HandleLeavesDecay(ctx, pos)
}

func (r regionHandler) HandlePortalCreate(ctx *Context, portalType Dimension, positions []cube.Pos) {
	ctx.lockShared()
	defer ctx.unlockShared()
	r.h.HandlePortalCreate(ctx, portalType, positions)
}

func (r regionHandler) HandlePortalActivate(ctx *Context, portalType Dimension, positions []cube.Pos) {
	ctx.lockShared()
	defer ctx.unlockShared()
	r.h.HandlePortalActivate(ctx, portalType, positions)
}

func (r regionHandler) HandleEntitySpawn(tx *Tx, e Entity) {
	tx.lockShared()
	defer tx.unlockShared()
	r.h.HandleEntitySpawn(tx, e)
}

func (r regionHandler) HandleEntityDespawn(tx *Tx, e Entity) {
	tx.lockShared()
	defer tx.unlockShared()
	r.h.HandleEntityDespawn(tx, e)
}

func (r regionHandler) HandleExplosion(ctx *Context, src ExplosionSource, entities *[]Entity, blocks *[]cube.Pos, itemDropChance *float64, spawnFire *bool) {
	ctx.lockShared()
	defer ctx.unlockShared()
	r.h.HandleExplosion(ctx, src, entities, blocks, itemDropChance, spawnFire)
}

func (r regionHandler) HandleRedstoneUpdate(ctx *Context, update RedstoneUpdate) {
	ctx.lockShared()
	defer ctx.unlockShared()
	r.h.HandleRedstoneUpdate(ctx, update)
}

func (r regionHandler) HandleClose(tx *Tx) {
	tx.lockShared()
	defer tx.unlockShared()
	r.h.HandleClose(tx)
}
