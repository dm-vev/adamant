package overworld

import (
	"cmp"
	"container/list"
	"maps"
	"slices"
	"sync"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/structure"
)

// originRecordCacheSize bounds the origin records kept for the chunks around
// them that are still to be decorated. A record is dropped once the nine
// chunks around its origin applied it; the bound covers chunks that are
// never generated. Generation spreading out from a player keeps the records
// of about three rings of chunks, some 600 at a view distance of 32 chunks:
// an evicted record is computed again. Generators tuned for more workers
// raise the bound with the wider front they generate.
const originRecordCacheSize = 1024

// originRecord holds what the features of one origin chunk wrote, step by
// step, when they ran after the origins that precede it.
type originRecord struct {
	steps []originStep
}

// originStep holds the writes and side outputs of the features of one
// decoration step of an origin, across the chunks around it.
type originStep struct {
	writes   []recordedWrite
	written  posIndex
	actors   map[cube.Pos]recordedActor
	ordered  []recordedActor
	ticks    []chunk.ScheduledTick
	markers  []cube.Pos
	entities []recordedEntity
}

// recordedWrite is the state a feature left at a position and the state it
// found there before it first wrote it.
type recordedWrite struct {
	pos           featurePos
	before, after world.NativeState
}

// recordedActor is a block actor a feature left at a block, recorded with the
// state of the block it belongs to.
type recordedActor struct {
	entry structure.Block
	state world.NativeState
}

// recordedEntity is an entity a feature spawned, created anew for the chunk
// it lands in.
type recordedEntity struct {
	pos   cube.Pos
	spawn func() *world.EntityHandle
}

func (s *originStep) write(p featurePos, before, after world.NativeState) {
	i := s.written.slot(p)
	if *i != 0 {
		s.writes[*i-1].after = after
		return
	}
	s.writes = append(s.writes, recordedWrite{pos: p, before: before, after: after})
	*i = uint32(len(s.writes))
}

func (s *originStep) actor(entry structure.Block, state world.NativeState) {
	if s.actors == nil {
		s.actors = make(map[cube.Pos]recordedActor)
	}
	entry.NBT = maps.Clone(entry.NBT)
	s.actors[cube.Pos{int(entry.Pos[0]), int(entry.Pos[1]), int(entry.Pos[2])}] = recordedActor{entry: entry, state: state}
}

// seal drops the lookup state used while recording and orders the actors by
// position, so a record applies the same way every time.
func (s *originStep) seal() {
	s.written.release()
	s.writes = slices.Clip(s.writes)
	for _, actor := range s.actors {
		s.ordered = append(s.ordered, actor)
	}
	slices.SortFunc(s.ordered, func(a, b recordedActor) int {
		return cmp.Or(cmp.Compare(a.entry.Pos[1], b.entry.Pos[1]), cmp.Compare(a.entry.Pos[2], b.entry.Pos[2]), cmp.Compare(a.entry.Pos[0], b.entry.Pos[0]))
	})
	s.actors = nil
}

// apply writes the step into the target chunk of r. A write applies only
// where the chunk still holds the state the feature found, so a feature that
// ran against a different neighbour never replaces the blocks of another.
func (s *originStep) apply(r *featureRegion) {
	for _, w := range s.writes {
		if r.inTarget(w.pos) && r.nativeState(w.pos) == w.before {
			r.setWithFlags(w.pos, w.after, 16)
		}
	}
	for _, actor := range s.ordered {
		p := featurePos{actor.entry.Pos[0], actor.entry.Pos[1], actor.entry.Pos[2]}
		if r.inTarget(p) && r.nativeState(p) == actor.state {
			entry := actor.entry
			entry.NBT = maps.Clone(entry.NBT)
			r.recordTemplateActor(entry)
		}
	}
	for _, tick := range s.ticks {
		if r.inTarget(featurePos{int32(tick.Pos[0]), int32(tick.Pos[1]), int32(tick.Pos[2])}) {
			r.reserveTick(tick)
		}
	}
	for _, pos := range s.markers {
		r.markPostProcessing(pos)
	}
	for _, e := range s.entities {
		r.addEntity(e.pos, e.spawn)
	}
}

// decorationOrigins returns the origin chunks whose decoration reaches the
// chunk at pos, in the order Java decorates them.
func decorationOrigins(pos world.ChunkPos) [9]world.ChunkPos {
	var origins [9]world.ChunkPos
	for i := range origins {
		origins[i] = world.ChunkPos{pos[0] + int32(i%3) - 1, pos[1] + int32(i/3) - 1}
	}
	return origins
}

// originPrecedes reports whether a decorates before b.
func originPrecedes(a, b world.ChunkPos) bool {
	return a[1] < b[1] || a[1] == b[1] && a[0] < b[0]
}

// applyContext writes the context record of origin into a recording region:
// like apply, a write lands only where the region still holds the state the
// feature found, and side outputs other than tick reservations are dropped
// as they are for an origin decorated only for context.
func (s *originStep) applyContext(r *featureRegion, origin world.ChunkPos) {
	r.SetOrigin(origin[0], origin[1])
	for _, w := range s.writes {
		if r.nativeState(w.pos) == w.before {
			r.setWithFlags(w.pos, w.after, 16)
		}
	}
	for _, actor := range s.ordered {
		p := featurePos{actor.entry.Pos[0], actor.entry.Pos[1], actor.entry.Pos[2]}
		if r.nativeState(p) == actor.state {
			entry := actor.entry
			entry.NBT = maps.Clone(entry.NBT)
			r.recordTemplateActor(entry)
		}
	}
	for _, tick := range s.ticks {
		r.reserveTick(tick)
	}
}

// computeOriginRecord records the features of origin after the context
// records of the origins around it that precede it, which hold what their
// structures and features write against the base terrain. Every origin is
// thus decorated once for its record and once for its context, rather than
// again for each origin it precedes.
func (f *ModernFeatures) computeOriginRecord(origin world.ChunkPos, registry chunk.BlockRegistry, r cube.Range, context world.GenerationContext) *originRecord {
	region := newRecordingRegion(f, registry, r)
	record := &originRecord{steps: make([]originStep, f.generationSteps())}
	// The context records are acquired first, so that in parallel mode those
	// this chunk owns are computed at once.
	var contexts [originSuccessors]*originRecordEntry
	var predecessors [originSuccessors]world.ChunkPos
	n := 0
	for _, o := range decorationOrigins(origin) {
		if o != origin && originPrecedes(o, origin) {
			predecessors[n], contexts[n] = o, f.contextRecord(o, registry, r, context)
			n++
		}
	}
	for i := range n {
		contexts[i].wait().steps[0].applyContext(region, predecessors[i])
		f.contexts.release(predecessors[i], originSuccessors)
	}
	f.decorateOrigin(region, origin, context, true, record)
	for i := range record.steps {
		record.steps[i].seal()
	}
	region.release()
	return record
}

// originSuccessors is the number of origins an origin precedes among the
// origins around them, each of which uses its context record once.
const originSuccessors = 4

// contextRecord returns the entry of the context record of origin, computing
// it if no other chunk is. Context records depend on nothing but the base
// terrain, so waiting for one never waits for an origin record.
func (f *ModernFeatures) contextRecord(origin world.ChunkPos, registry chunk.BlockRegistry, r cube.Range, context world.GenerationContext) *originRecordEntry {
	entry, owner := f.contexts.acquire(origin)
	if owner {
		f.fill(&f.contexts, entry, func() *originRecord {
			region := newRecordingRegion(f, registry, r)
			record := &originRecord{steps: make([]originStep, 1)}
			f.decorateContext(region, origin, context, &record.steps[0])
			region.release()
			record.steps[0].seal()
			return record
		})
	}
	return entry
}

// originRecord returns the entry of the record of origin, computing the
// record if no other chunk is: the record is ready once done is closed. In
// parallel mode a record this chunk owns is computed on its own goroutine.
func (f *ModernFeatures) originRecord(origin world.ChunkPos, registry chunk.BlockRegistry, r cube.Range, context world.GenerationContext) *originRecordEntry {
	entry, owner := f.origins.acquire(origin)
	if owner {
		f.fill(&f.origins, entry, func() *originRecord {
			return f.computeOriginRecord(origin, registry, r, context)
		})
	}
	return entry
}

// fill computes the record of an entry the caller owns, on its own goroutine
// in parallel mode. A panic computing it is passed on to every chunk that
// waits for the record, rather than leaving them waiting forever.
func (f *ModernFeatures) fill(cache *originRecordCache, entry *originRecordEntry, compute func() *originRecord) {
	if !f.parallel {
		cache.fill(entry, compute)
		return
	}
	go func() {
		// The waiters of the entry raise the panic again.
		defer func() { _ = recover() }()
		cache.fill(entry, compute)
	}()
}

// originRecordCache keeps recent origin records, computing each once even
// when several chunks around an origin are decorated concurrently.
type originRecordCache struct {
	mu      sync.Mutex
	entries map[world.ChunkPos]*originRecordEntry
	recent  list.List
	// capacity bounds the entries kept, originRecordCacheSize if zero.
	capacity int
}

type originRecordEntry struct {
	origin  world.ChunkPos
	done    chan struct{}
	record  *originRecord
	uses    int
	element *list.Element
	// failure is the panic computing the record, raised again for every
	// chunk that waits for it.
	failure any
}

// wait returns the record of the entry once it is computed, panicking if
// computing it did.
func (e *originRecordEntry) wait() *originRecord {
	<-e.done
	if e.failure != nil {
		panic(e.failure)
	}
	return e.record
}

// fill computes the record of an entry the caller owns and closes done. If
// compute panics, the entry is dropped and done closed before the panic goes
// on.
func (c *originRecordCache) fill(entry *originRecordEntry, compute func() *originRecord) {
	defer func() {
		if r := recover(); r != nil {
			entry.failure = r
			c.mu.Lock()
			if c.entries[entry.origin] == entry {
				c.remove(entry)
			}
			c.mu.Unlock()
			close(entry.done)
			panic(r)
		}
	}()
	entry.record = compute()
	close(entry.done)
}

// acquire returns the entry of origin. owner is true if the caller must
// compute its record and close done.
func (c *originRecordCache) acquire(origin world.ChunkPos) (entry *originRecordEntry, owner bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if entry, ok := c.entries[origin]; ok {
		c.recent.MoveToFront(entry.element)
		return entry, false
	}
	if c.entries == nil {
		c.entries = make(map[world.ChunkPos]*originRecordEntry)
	}
	entry = &originRecordEntry{origin: origin, done: make(chan struct{})}
	entry.element = c.recent.PushFront(entry)
	c.entries[origin] = entry
	capacity := c.capacity
	if capacity == 0 {
		capacity = originRecordCacheSize
	}
	if c.recent.Len() > capacity {
		c.remove(c.recent.Back().Value.(*originRecordEntry))
	}
	return entry, true
}

// release counts a use of the record of origin, dropping the record once it
// has been used by all the users it has.
func (c *originRecordCache) release(origin world.ChunkPos, users int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if entry, ok := c.entries[origin]; ok {
		if entry.uses++; entry.uses == users {
			c.remove(entry)
		}
	}
}

func (c *originRecordCache) remove(entry *originRecordEntry) {
	c.recent.Remove(entry.element)
	delete(c.entries, entry.origin)
}
