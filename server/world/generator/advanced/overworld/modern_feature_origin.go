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
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/javastructure"
)

// originRecordCacheSize bounds the origin records kept for the chunks around
// them that are still to be decorated. A record is dropped once the nine
// chunks around its origin applied it; the bound covers chunks that are
// never generated. Generation spreading out from a player keeps the records
// of about three rings of chunks, some 600 at a view distance of 32 chunks:
// an evicted record is computed again, decorating five origins.
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
	written  map[featurePos]int
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
	entry javastructure.Block
	state world.NativeState
}

// recordedEntity is an entity a feature spawned, created anew for the chunk
// it lands in.
type recordedEntity struct {
	pos   cube.Pos
	spawn func() *world.EntityHandle
}

func (s *originStep) write(p featurePos, before, after world.NativeState) {
	if i, ok := s.written[p]; ok {
		s.writes[i].after = after
		return
	}
	if s.written == nil {
		s.written = make(map[featurePos]int)
	}
	s.written[p] = len(s.writes)
	s.writes = append(s.writes, recordedWrite{pos: p, before: before, after: after})
}

func (s *originStep) actor(entry javastructure.Block, state world.NativeState) {
	if s.actors == nil {
		s.actors = make(map[cube.Pos]recordedActor)
	}
	entry.NBT = maps.Clone(entry.NBT)
	s.actors[cube.Pos{int(entry.Pos[0]), int(entry.Pos[1]), int(entry.Pos[2])}] = recordedActor{entry: entry, state: state}
}

// seal drops the lookup state used while recording and orders the actors by
// position, so a record applies the same way every time.
func (s *originStep) seal() {
	s.written = nil
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
			r.reserveJavaTick(tick)
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

// computeOriginRecord records the features of origin after the origins
// around it that precede it, whose features and the structures of all of
// them form the context.
func (f *ModernFeatures) computeOriginRecord(origin world.ChunkPos, registry chunk.BlockRegistry, r cube.Range, context world.GenerationContext) *originRecord {
	region := newRecordingRegion(f, registry, r)
	record := &originRecord{steps: make([]originStep, f.generationSteps())}
	for _, o := range decorationOrigins(origin) {
		if o == origin {
			f.decorateOrigin(region, o, context, true, record)
			break
		}
		if originPrecedes(o, origin) {
			f.decorateOrigin(region, o, context, true, nil)
		}
	}
	for i := range record.steps {
		record.steps[i].seal()
	}
	return record
}

// originRecord returns the entry of the record of origin, computing the
// record if no other chunk is: the record is ready once done is closed.
func (f *ModernFeatures) originRecord(origin world.ChunkPos, registry chunk.BlockRegistry, r cube.Range, context world.GenerationContext) *originRecordEntry {
	entry, owner := f.origins.acquire(origin)
	if owner {
		entry.record = f.computeOriginRecord(origin, registry, r, context)
		close(entry.done)
	}
	return entry
}

// originRecordCache keeps recent origin records, computing each once even
// when several chunks around an origin are decorated concurrently.
type originRecordCache struct {
	mu      sync.Mutex
	entries map[world.ChunkPos]*originRecordEntry
	recent  list.List
}

type originRecordEntry struct {
	origin  world.ChunkPos
	done    chan struct{}
	record  *originRecord
	uses    int
	element *list.Element
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
	if c.recent.Len() > originRecordCacheSize {
		c.remove(c.recent.Back().Value.(*originRecordEntry))
	}
	return entry, true
}

// release counts a chunk that applied the record of origin, dropping the
// record once all nine chunks around its origin have.
func (c *originRecordCache) release(origin world.ChunkPos) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if entry, ok := c.entries[origin]; ok {
		if entry.uses++; entry.uses == len(decorationOrigins(origin)) {
			c.remove(entry)
		}
	}
}

func (c *originRecordCache) remove(entry *originRecordEntry) {
	c.recent.Remove(entry.element)
	delete(c.entries, entry.origin)
}
