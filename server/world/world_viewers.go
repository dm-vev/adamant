package world

import (
	"slices"
	"sync"

	"github.com/go-gl/mathgl/mgl64"
)

// viewerState is the viewers of the world and the loaders they view through.
type viewerState struct {
	viewerMu      sync.Mutex
	viewers       map[*Loader]Viewer
	viewerLoaders []*Loader
}

// addParticle spawns a Particle at a given position in the World. Viewers that
// are viewing the chunk will be shown the particle.
func (w *World) addParticle(tx *Tx, pos mgl64.Vec3, p Particle) {
	p.Spawn(w, pos)
	viewers := w.viewersOf(tx, pos)
	var b Broadcast
	for _, viewer := range viewers {
		if sv, ok := viewer.(SharedViewer); ok {
			sv.ViewParticleShared(pos, p, &b)
		} else {
			viewer.ViewParticle(pos, p)
		}
	}
	w.releaseViewers(viewers)
}

// playSound plays a sound at a specific position in the World. Viewers of that
// position will be able to hear the sound if they are close enough.
func (w *World) playSound(tx *Tx, pos mgl64.Vec3, s Sound) {
	ctx := tx.Event()
	if w.Handler().HandleSound(ctx, s, pos); ctx.Cancelled() {
		return
	}
	s.Play(w, pos)
	viewers := w.viewersOf(tx, pos)
	var b Broadcast
	for _, viewer := range viewers {
		if sv, ok := viewer.(SharedViewer); ok {
			sv.ViewSoundShared(pos, s, &b)
		} else {
			viewer.ViewSound(pos, s)
		}
	}
	w.releaseViewers(viewers)
}

// viewersOf returns all viewers viewing the position passed.
//
// The method deliberately borrows a slice from viewerSlicePool so the caller can iterate without allocating. The
// caller must eventually hand the slice back through releaseViewers to maintain the pool's effectiveness. We have to
// pay special attention to reusing buffers here because these lookups happen every time entities broadcast state or
// packets are fanned out to observers.
func (w *World) viewersOf(tx *Tx, pos mgl64.Vec3) []Viewer {
	c, ok := tx.loadedColumn(chunkPosFromVec3(pos))
	if !ok || len(c.viewers) == 0 {
		return nil
	}
	viewers := borrowViewers(len(c.viewers) + 1)
	for v := range c.viewers {
		viewers = append(viewers, v)
	}
	return viewers
}

// releaseViewers returns pooled viewer slices to viewerSlicePool. Forgetting to release will degrade the pool and
// reintroduce the very allocations this optimisation was meant to avoid.
func (w *World) releaseViewers(viewers []Viewer) {
	if viewers == nil {
		return
	}
	returnViewers(viewers)
}

// allViewers returns all viewers and loaders, regardless of where in the world
// they are viewing.
func (w *World) allViewers() ([]Viewer, []*Loader) {
	w.viewerMu.Lock()
	defer w.viewerMu.Unlock()

	viewers := borrowViewers(len(w.viewers))
	loaders := make([]*Loader, 0, len(w.viewers))
	for _, loader := range w.viewerLoaders {
		viewers = append(viewers, w.viewers[loader])
		loaders = append(loaders, loader)
	}
	return viewers, loaders
}

// addWorldViewer adds a viewer to the world. Should only be used while the
// viewer isn't viewing any chunks.
func (w *World) addWorldViewer(l *Loader) {
	w.viewerMu.Lock()
	if _, exists := w.viewers[l]; !exists {
		w.viewerLoaders = append(w.viewerLoaders, l)
	}
	w.viewers[l] = l.viewer
	w.viewerMu.Unlock()
}

func (w *World) removeWorldViewer(l *Loader) {
	w.viewerMu.Lock()
	delete(w.viewers, l)
	w.viewerLoaders = slices.DeleteFunc(w.viewerLoaders, func(loader *Loader) bool { return loader == l })
	w.viewerMu.Unlock()
}

// initWorldViewer sends initial state without holding loader or viewer locks:
// viewers may re-enter the loader from any of these callbacks.
func (w *World) initWorldViewer(viewer Viewer) {
	viewer.ViewTime(w.Time())
	viewer.ViewTimeCycle(w.TimeCycle())
	w.set.Lock()
	raining, thundering := w.set.Raining, w.set.Raining && w.set.Thundering
	w.set.Unlock()
	viewer.ViewWeather(raining, thundering)
	viewer.ViewWorldSpawn(w.Spawn())
}

// addViewer adds a viewer to the World at a given position. Any events that
// happen in the chunk at that position, such as block and entity changes, will
// be sent to the viewer.
func (w *World) addViewer(pos ChunkPos, c *Column, loader *Loader, viewer Viewer) {
	if viewer != nil {
		c.viewers[viewer] = struct{}{}
	}
	c.loaders = append(c.loaders, loader)

	w.addActiveColumn(pos, c)
}

func (w *World) viewChunkEntities(tx *Tx, c *Column, viewer Viewer) {
	for _, entity := range c.Entities {
		if ent, ok := entity.Entity(tx); ok {
			showEntity(ent, viewer)
		}
	}
}

// rollbackViewer removes a partially published chunk without closing it, so it may be retried.
func (w *World) rollbackViewer(tx *Tx, pos ChunkPos, c *Column, loader *Loader, viewer Viewer) {
	w.removeViewerFrom(tx, pos, c, loader, viewer, false)
}

func (w *World) removeViewerFrom(tx *Tx, pos ChunkPos, expected *Column, loader *Loader, viewer Viewer, closeUnused bool) {
	if w == nil {
		return
	}
	c, ok := w.chunks[pos]
	if !ok || expected != nil && c != expected {
		return
	}
	if i := slices.Index(c.loaders, loader); i != -1 {
		c.loaders = slices.Delete(c.loaders, i, i+1)
	}

	if len(c.loaders) == 0 {
		w.removeActiveColumn(pos)
	}

	// Hide all entities in the chunk from the viewer.
	delete(c.viewers, viewer)
	if viewer != nil {
		for _, entity := range c.Entities {
			if ent, ok := entity.Entity(tx); ok {
				viewer.HideEntity(ent)
			}
		}
	}

	if closeUnused && len(c.viewers) == 0 && len(c.loaders) == 0 && !w.retainedByForcedChunk(pos) {
		w.closeChunk(tx, pos, c)
	}
}

// showEntity shows an Entity to a viewer of the world. It makes sure
// everything of the Entity, including the items held, is shown.
func showEntity(e Entity, viewer Viewer) {
	// The viewer has the entity where it is now, which the parts the moves
	// leave out need not match.
	e.H().data.Moves.showAll()
	viewer.ViewEntity(e)
	viewer.ViewEntityItems(e)
	viewer.ViewEntityArmour(e)
}

// viewerSlicePool recycles temporary []Viewer buffers created while broadcasting world state to reduce GC churn.
// The default capacity is intentionally small: most columns have just a handful of viewers, yet larger slices are
// returned to the pool so hot paths can still reuse previously grown allocations instead of re-allocating.
//
// The pool holds *[]Viewer: a slice put in a sync.Pool as it is would be boxed, an allocation on every release.
// viewerHolderPool keeps the emptied holders while their slices are lent out.
var (
	viewerSlicePool = sync.Pool{
		New: func() any {
			viewers := make([]Viewer, 0, 8)
			return &viewers
		},
	}
	viewerHolderPool sync.Pool
)

// borrowViewers lends an empty pooled slice with room for n viewers. It is
// handed back with returnViewers.
func borrowViewers(n int) []Viewer {
	holder := viewerSlicePool.Get().(*[]Viewer)
	viewers := *holder
	*holder = nil
	viewerHolderPool.Put(holder)
	if cap(viewers) < n {
		returnViewers(viewers)
		return make([]Viewer, 0, n)
	}
	return viewers[:0]
}

// returnViewers hands a slice lent by borrowViewers back to viewerSlicePool.
func returnViewers(viewers []Viewer) {
	holder, _ := viewerHolderPool.Get().(*[]Viewer)
	if holder == nil {
		holder = new([]Viewer)
	}
	*holder = viewers[:0]
	viewerSlicePool.Put(holder)
}
