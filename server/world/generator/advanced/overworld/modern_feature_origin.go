package overworld

import (
	"cmp"
	"container/list"
	"maps"
	"slices"
	"sync"

	"github.com/dm-vev/adamant/internal/faultcheck"
	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/world"
	"github.com/dm-vev/adamant/server/world/chunk"
	"github.com/dm-vev/adamant/server/world/generator/advanced/internal/structure"
)

// originRecordCacheSize bounds the origin records kept for the chunks around
// them that are still to be decorated. A record is dropped once the nine
// chunks around its origin applied it; the bound covers chunks that are
// never generated. Generation spreading out from a player keeps the records
// of about three rings of chunks, some 600 at a view distance of 32 chunks:
// an evicted record is computed again. Generators tuned for more workers
// raise the bound with the wider front they generate.
const (
	originRecordCacheSize = 1536
	// originRecordCacheMaxBytes bounds the estimated heap retained by
	// originRecordCache (64 MiB) so dense biomes or structures cannot push
	// generator heap into hundreds of megabytes during exploration.
	originRecordCacheMaxBytes int64 = 64 << 20

	originRecordBaseBytes = 64
	originStepBytes       = 184
	recordedWriteBytes    = 32
	recordedActorBytes    = 512
	scheduledTickBytes    = 48
	markerPosBytes        = 24
	recordedEntityBytes   = 96

	stepWritesInitCap      = 256
	maxFreeStepWrites      = 64
	maxPooledStepWritesCap = 32768
	maxFreeWriteSlabBytes  = 32 << 20
	maxFreeWriteSlabWrites = maxFreeWriteSlabBytes / recordedWriteBytes
)

var writeSlabCaps = [...]int{
	512, 1024, 2048, 4096, 8192, 16384, 32768, 65536, 131072,
}

// freeStepWrites keeps scratch recording slices for reuse across originStep
// recording passes. Unlike sync.Pool it survives GC cycles during generation
// and is drained by ReleaseGenerationCaches.
var freeStepWrites struct {
	sync.Mutex
	slices [][]recordedWrite
}

// freeWriteSlabs keeps sealed contiguous recordedWrite slabs bucketed by
// half-power-of-2 capacity for reuse once finished originRecords are evicted
// or retired with zero active readers.
var freeWriteSlabs struct {
	sync.Mutex
	buckets     [len(writeSlabCaps)][][]recordedWrite
	totalWrites int
}

func getStepWrites() []recordedWrite {
	freeStepWrites.Lock()
	if n := len(freeStepWrites.slices); n > 0 {
		s := freeStepWrites.slices[n-1]
		freeStepWrites.slices[n-1] = nil
		freeStepWrites.slices = freeStepWrites.slices[:n-1]
		freeStepWrites.Unlock()
		return s[:0]
	}
	freeStepWrites.Unlock()
	return make([]recordedWrite, 0, stepWritesInitCap)
}

func putStepWrites(s []recordedWrite) {
	if cap(s) == 0 || cap(s) > maxPooledStepWritesCap {
		return
	}
	s = s[:0]
	freeStepWrites.Lock()
	if len(freeStepWrites.slices) < maxFreeStepWrites {
		freeStepWrites.slices = append(freeStepWrites.slices, s)
	}
	freeStepWrites.Unlock()
}

func writeSlabBucketForLen(n int) int {
	for i, c := range writeSlabCaps {
		if c >= n {
			return i
		}
	}
	return -1
}

func writeSlabBucketExact(c int) int {
	for i, capSize := range writeSlabCaps {
		if capSize == c {
			return i
		}
	}
	return -1
}

func getWriteSlab(n int) []recordedWrite {
	if n <= 0 {
		return nil
	}
	b := writeSlabBucketForLen(n)
	if b < 0 {
		return make([]recordedWrite, n)
	}
	freeWriteSlabs.Lock()
	for idx := b; idx <= b+1 && idx < len(writeSlabCaps); idx++ {
		if k := len(freeWriteSlabs.buckets[idx]); k > 0 {
			slab := freeWriteSlabs.buckets[idx][k-1]
			freeWriteSlabs.buckets[idx][k-1] = nil
			freeWriteSlabs.buckets[idx] = freeWriteSlabs.buckets[idx][:k-1]
			freeWriteSlabs.totalWrites -= cap(slab)
			freeWriteSlabs.Unlock()
			return slab[:n]
		}
	}
	freeWriteSlabs.Unlock()
	return make([]recordedWrite, n, writeSlabCaps[b])
}

func putWriteSlab(slab []recordedWrite) {
	c := cap(slab)
	b := writeSlabBucketExact(c)
	if b < 0 {
		return
	}
	slab = slab[:0]
	freeWriteSlabs.Lock()
	if freeWriteSlabs.totalWrites+c <= maxFreeWriteSlabWrites {
		freeWriteSlabs.buckets[b] = append(freeWriteSlabs.buckets[b], slab)
		freeWriteSlabs.totalWrites += c
	}
	freeWriteSlabs.Unlock()
}

func clearOriginWritePools() {
	freeStepWrites.Lock()
	freeStepWrites.slices = nil
	freeStepWrites.Unlock()
	freeWriteSlabs.Lock()
	freeWriteSlabs.buckets = [len(writeSlabCaps)][][]recordedWrite{}
	freeWriteSlabs.totalWrites = 0
	freeWriteSlabs.Unlock()
}

// originRecord holds what the features of one origin chunk wrote, step by
// step, when they ran after the origins that precede it.
type originRecord struct {
	steps []originStep
	// slab backs every step's writes sub-slice once sealed so one allocation
	// serves the whole record and can be recycled when no readers remain.
	slab []recordedWrite
}

// originStep holds the writes and side outputs of the features of one
// decoration step of an origin, across the chunks around it. structWrites holds
// non-BeyondChunk structure block writes inside the origin chunk itself so
// higher-color successor origins see them without double-decorating.
type originStep struct {
	writes       []recordedWrite
	structWrites []recordedWrite
	written      posIndex
	actors       map[cube.Pos]recordedActor
	ordered      []recordedActor
	ticks        []chunk.ScheduledTick
	markers      []cube.Pos
	entities     []recordedEntity
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
	if s.writes == nil {
		s.writes = getStepWrites()
	}
	s.writes = append(s.writes, recordedWrite{pos: p, before: before, after: after})
	*i = uint32(len(s.writes))
}

func (s *originStep) writeStruct(p featurePos, before, after world.NativeState) {
	if s.structWrites == nil {
		s.structWrites = getStepWrites()
	}
	s.structWrites = append(s.structWrites, recordedWrite{pos: p, before: before, after: after})
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
	if len(s.actors) > 0 {
		s.ordered = make([]recordedActor, 0, len(s.actors))
		for _, actor := range s.actors {
			s.ordered = append(s.ordered, actor)
		}
		slices.SortFunc(s.ordered, func(a, b recordedActor) int {
			return cmp.Or(cmp.Compare(a.entry.Pos[1], b.entry.Pos[1]), cmp.Compare(a.entry.Pos[2], b.entry.Pos[2]), cmp.Compare(a.entry.Pos[0], b.entry.Pos[0]))
		})
		s.actors = nil
	}
}

// release returns any scratch index pages and unsealed write slice to their pools.
func (s *originStep) release() {
	s.written.release()
	if s.writes != nil {
		putStepWrites(s.writes)
		s.writes = nil
	}
	if s.structWrites != nil {
		putStepWrites(s.structWrites)
		s.structWrites = nil
	}
}

func packStepWrites(dst *[]recordedWrite, slab []recordedWrite, off int) int {
	if n := len(*dst); n > 0 {
		scratch := *dst
		packed := slab[off : off+n : off+n]
		copy(packed, scratch)
		*dst = packed
		putStepWrites(scratch)
		return off + n
	}
	if *dst != nil {
		putStepWrites(*dst)
		*dst = nil
	}
	return off
}

// seal packs every step's writes into one contiguous slab, returns the scratch
// slices to freeStepWrites, and finalises each step's lookup and actor state.
func (r *originRecord) seal() {
	totalWrites := 0
	for i := range r.steps {
		totalWrites += len(r.steps[i].writes) + len(r.steps[i].structWrites)
	}
	var slab []recordedWrite
	if totalWrites > 0 {
		slab = getWriteSlab(totalWrites)
		r.slab = slab
	}
	off := 0
	for i := range r.steps {
		s := &r.steps[i]
		off = packStepWrites(&s.writes, slab, off)
		off = packStepWrites(&s.structWrites, slab, off)
		s.seal()
	}
}

// releaseScratch returns scratch slices and index pages of an unsealed record
// when recording aborts due to a panic.
func (r *originRecord) releaseScratch() {
	for i := range r.steps {
		r.steps[i].release()
	}
}

// recycle returns the sealed write slab to freeWriteSlabs once no cache entry
// or active reader references this record.
func (r *originRecord) recycle() {
	if r == nil {
		return
	}
	if r.slab != nil {
		putWriteSlab(r.slab)
		r.slab = nil
	}
	for i := range r.steps {
		r.steps[i].writes = nil
		r.steps[i].structWrites = nil
	}
}

func recycleOriginRecords(records []*originRecord) {
	for _, r := range records {
		r.recycle()
	}
}

// apply writes the step into the target chunk of r. A write applies only
// where the chunk still holds the state the feature found, so a feature that
// ran against a different neighbour never replaces the blocks of another, and
// kelp only where its floor holds.
func (s *originStep) apply(r *featureRegion) {
	for _, w := range s.writes {
		if r.inTarget(w.pos) && r.nativeState(w.pos) == w.before && r.kelpFloorHolds(w.pos, w.after) {
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
// chunk at pos, in row-major Java order (used by DecorateInOneRegion).
func decorationOrigins(pos world.ChunkPos) [9]world.ChunkPos {
	var origins [9]world.ChunkPos
	for i := range origins {
		origins[i] = world.ChunkPos{pos[0] + int32(i%3) - 1, pos[1] + int32(i/3) - 1}
	}
	return origins
}

// originColor assigns a 4-color parity in [0..3] to each origin chunk so no two
// distinct origins of the same color can write into each other's home chunk.
// Dependency depth across the infinite grid is strictly bounded at 3.
func originColor(pos world.ChunkPos) int {
	return int(pos[0]&1) | (int(pos[1]&1) << 1)
}

// originColorUsers is the total number of callers acquiring an originRecord of
// each color: 9 target chunks in Decorate plus 8/6/2/0 higher-color successors.
var originColorUsers = [4]int{17, 15, 11, 9}

func originUsers(pos world.ChunkPos) int {
	return originColorUsers[originColor(pos)]
}

// originPrecedes reports whether a decorates before b in 4-color DAG order.
func originPrecedes(a, b world.ChunkPos) bool {
	ca, cb := originColor(a), originColor(b)
	if ca != cb {
		return ca < cb
	}
	return a[1] < b[1] || (a[1] == b[1] && a[0] < b[0])
}

// coloredDecorationOrigins returns the 9 origins around pos sorted in
// 4-color DAG order so parallel/single-pass decoration is deterministic.
func coloredDecorationOrigins(pos world.ChunkPos) [9]world.ChunkPos {
	origins := decorationOrigins(pos)
	for i := 1; i < len(origins); i++ {
		v := origins[i]
		j := i
		for j > 0 && originPrecedes(v, origins[j-1]) {
			origins[j] = origins[j-1]
			j--
		}
		origins[j] = v
	}
	return origins
}

func applyPredecessorWrites(r *featureRegion, origin world.ChunkPos, writes []recordedWrite) {
	for _, w := range writes {
		if absFeatureInt(w.pos.x>>4-origin[0]) <= 1 && absFeatureInt(w.pos.z>>4-origin[1]) <= 1 {
			if w.before != w.after && r.nativeState(w.pos) == w.before && r.kelpFloorHolds(w.pos, w.after) {
				r.setWithFlagsPrev(w.pos, w.after, w.before, true, 16)
			}
		}
	}
}

// applyPredecessor writes a lower-color predecessor origin's step into origin's
// recording region for positions falling within origin's 3x3 write area.
func (s *originStep) applyPredecessor(r *featureRegion, origin world.ChunkPos) {
	applyPredecessorWrites(r, origin, s.structWrites)
	applyPredecessorWrites(r, origin, s.writes)
	for _, actor := range s.ordered {
		p := featurePos{actor.entry.Pos[0], actor.entry.Pos[1], actor.entry.Pos[2]}
		if absFeatureInt(p.x>>4-origin[0]) <= 1 && absFeatureInt(p.z>>4-origin[1]) <= 1 && r.nativeState(p) == actor.state {
			entry := actor.entry
			entry.NBT = maps.Clone(entry.NBT)
			r.recordTemplateActor(entry)
		}
	}
	for _, tick := range s.ticks {
		if absFeatureInt(int32(tick.Pos[0])>>4-origin[0]) <= 1 && absFeatureInt(int32(tick.Pos[2])>>4-origin[1]) <= 1 {
			r.reserveTick(tick)
		}
	}
}

const maxOriginPredecessors = 8

// computeOriginRecord decorates origin ONCE against its lower-color neighbour
// origin records (bounded DAG depth <= 3), eliminating redundant context passes.
func (f *ModernFeatures) computeOriginRecord(origin world.ChunkPos, registry chunk.BlockRegistry, r cube.Range, context world.GenerationContext) *originRecord {
	region := newRecordingRegion(f, registry, r)
	record := &originRecord{steps: make([]originStep, f.generationSteps())}
	sealed := false
	defer func() {
		if !sealed {
			record.releaseScratch()
			region.release()
		}
	}()
	c := originColor(origin)
	if c != 0 {
		var predecessors [maxOriginPredecessors]world.ChunkPos
		n := 0
		for _, o := range coloredDecorationOrigins(origin) {
			if originColor(o) < c {
				predecessors[n] = o
				n++
			}
		}
		region.SetOrigin(origin[0], origin[1])
		f.applyOriginPredecessors(region, origin, predecessors[:n], registry, r, context)
	}
	f.decorateOrigin(region, origin, context, true, record)
	record.seal()
	sealed = true
	region.release()
	return record
}

func (f *ModernFeatures) applyOriginPredecessors(region *featureRegion, origin world.ChunkPos, predecessors []world.ChunkPos, registry chunk.BlockRegistry, r cube.Range, context world.GenerationContext) {
	if !f.parallel {
		for _, p := range predecessors {
			entry := f.originRecord(p, registry, r, context)
			released := false
			func() {
				defer func() {
					if !released {
						f.origins.abandon(entry)
					}
				}()
				rec := entry.wait()
				for i := range rec.steps {
					rec.steps[i].applyPredecessor(region, origin)
				}
				f.origins.release(entry, originUsers(p))
				released = true
			}()
		}
		return
	}
	var entries [maxOriginPredecessors]*originRecordEntry
	for i, p := range predecessors {
		entries[i] = f.originRecord(p, registry, r, context)
	}
	released := 0
	defer func() {
		for i := released; i < len(predecessors); i++ {
			if entries[i] != nil {
				f.origins.abandon(entries[i])
			}
		}
	}()
	for i, p := range predecessors {
		rec := entries[i].wait()
		for j := range rec.steps {
			rec.steps[j].applyPredecessor(region, origin)
		}
		f.origins.release(entries[i], originUsers(p))
		released = i + 1
	}
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
		defer func() {
			if r := recover(); r != nil {
				faultcheck.Report("overworld feature origin prefetch", r)
			}
		}()
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
	// maxBytes bounds the estimated heap kept, originRecordCacheMaxBytes if zero.
	maxBytes int64
	bytes    int64
}

type originRecordEntry struct {
	origin  world.ChunkPos
	done    chan struct{}
	record  *originRecord
	uses    int
	active  int
	ready   bool
	removed bool
	bytes   int64
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
				_ = c.removeLocked(entry)
			}
			c.mu.Unlock()
			close(entry.done)
			panic(r)
		}
	}()
	record := compute()
	size := estimateOriginRecordBytes(record)
	var buf [8]*originRecord
	recycled := buf[:0]
	c.mu.Lock()
	entry.record = record
	entry.ready = true
	if c.entries[entry.origin] == entry {
		entry.bytes = size
		c.bytes += size
		recycled = c.evictLocked(recycled)
	} else if rec := c.detachLocked(entry); rec != nil {
		recycled = append(recycled, rec)
	}
	c.mu.Unlock()
	close(entry.done)
	recycleOriginRecords(recycled)
}

// acquire returns the entry of origin. owner is true if the caller must
// compute its record and close done.
func (c *originRecordCache) acquire(origin world.ChunkPos) (entry *originRecordEntry, owner bool) {
	var buf [8]*originRecord
	recycled := buf[:0]
	c.mu.Lock()
	if entry, ok := c.entries[origin]; ok {
		entry.active++
		c.recent.MoveToFront(entry.element)
		c.mu.Unlock()
		return entry, false
	}
	if c.entries == nil {
		c.entries = make(map[world.ChunkPos]*originRecordEntry)
	}
	entry = &originRecordEntry{origin: origin, done: make(chan struct{}), active: 1}
	entry.element = c.recent.PushFront(entry)
	c.entries[origin] = entry
	recycled = c.evictLocked(recycled)
	c.mu.Unlock()
	recycleOriginRecords(recycled)
	return entry, true
}

// release counts a use of the record of entry, dropping the record once it has
// been used by all the users it has and evicting idle entries if over budget.
func (c *originRecordCache) release(entry *originRecordEntry, users int) {
	var buf [8]*originRecord
	recycled := buf[:0]
	c.mu.Lock()
	if entry.active > 0 {
		entry.active--
	}
	if c.entries[entry.origin] == entry {
		if entry.uses++; entry.uses >= users {
			if rec := c.removeLocked(entry); rec != nil {
				recycled = append(recycled, rec)
			}
			c.mu.Unlock()
			recycleOriginRecords(recycled)
			return
		}
	} else if rec := c.detachLocked(entry); rec != nil {
		recycled = append(recycled, rec)
	}
	recycled = c.evictLocked(recycled)
	c.mu.Unlock()
	recycleOriginRecords(recycled)
}

// abandon releases an active reference without counting a completed use, e.g.
// when a caller aborts due to a panic in a sibling origin.
func (c *originRecordCache) abandon(entry *originRecordEntry) {
	var buf [8]*originRecord
	recycled := buf[:0]
	c.mu.Lock()
	if entry.active > 0 {
		entry.active--
	}
	if rec := c.detachLocked(entry); rec != nil {
		recycled = append(recycled, rec)
	}
	recycled = c.evictLocked(recycled)
	c.mu.Unlock()
	recycleOriginRecords(recycled)
}

func (c *originRecordCache) maxCapacity() int {
	if c.capacity > 0 {
		return c.capacity
	}
	return originRecordCacheSize
}

func (c *originRecordCache) byteBudget() int64 {
	if c.maxBytes > 0 {
		return c.maxBytes
	}
	return originRecordCacheMaxBytes
}

func (c *originRecordCache) overBudgetLocked() bool {
	return c.recent.Len() > c.maxCapacity() || c.bytes > c.byteBudget()
}

func (c *originRecordCache) evictLocked(recycled []*originRecord) []*originRecord {
	if !c.overBudgetLocked() {
		return recycled
	}
	for el := c.recent.Back(); el != nil && c.overBudgetLocked(); {
		prev := el.Prev()
		entry := el.Value.(*originRecordEntry)
		if entry.ready && entry.active == 0 {
			if rec := c.removeLocked(entry); rec != nil {
				recycled = append(recycled, rec)
			}
		}
		el = prev
	}
	return recycled
}

func (c *originRecordCache) detachLocked(entry *originRecordEntry) *originRecord {
	if !entry.removed || !entry.ready || entry.active != 0 || entry.record == nil {
		return nil
	}
	rec := entry.record
	entry.record = nil
	return rec
}

func (c *originRecordCache) removeLocked(entry *originRecordEntry) *originRecord {
	if entry.removed {
		return c.detachLocked(entry)
	}
	if entry.element != nil {
		c.recent.Remove(entry.element)
		entry.element = nil
	}
	delete(c.entries, entry.origin)
	c.bytes -= entry.bytes
	entry.bytes = 0
	entry.removed = true
	return c.detachLocked(entry)
}

func estimateOriginRecordBytes(r *originRecord) int64 {
	if r == nil {
		return 0
	}
	n := int64(originRecordBaseBytes) + int64(cap(r.steps))*originStepBytes + int64(cap(r.slab))*recordedWriteBytes
	for i := range r.steps {
		s := &r.steps[i]
		if r.slab == nil {
			n += int64(cap(s.writes)+cap(s.structWrites)) * recordedWriteBytes
		}
		n += int64(cap(s.ordered)) * recordedActorBytes
		n += int64(cap(s.ticks)) * scheduledTickBytes
		n += int64(cap(s.markers)) * markerPosBytes
		n += int64(cap(s.entities)) * recordedEntityBytes
	}
	return n
}
