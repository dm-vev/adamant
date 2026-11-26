package entity

import (
	"math"
	"sync"

	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/internal/parallel"
	"github.com/dm-vev/adamant/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// CrammingDamageSource is dealt to an entity squeezed together with more
// than max_entity_cramming-1 other pushable entities. It bypasses armour.
type CrammingDamageSource struct{}

func (CrammingDamageSource) ReducedByArmour() bool     { return false }
func (CrammingDamageSource) ReducedByResistance() bool { return true }
func (CrammingDamageSource) Fire() bool                { return false }
func (CrammingDamageSource) IgnoreTotem() bool         { return false }

const (
	pushScratchMaxEntities = 16384
	pushScratchMaxCells    = 16384
	pushParallelThreshold  = 256
)

const (
	pushFlagPlayer uint8 = 1 << iota
	pushFlagIsVehicle
	pushFlagIsPassenger
)

// pushSpatial holds the read-only spatial snapshot for one pushable entity.
// It contains no pointers so the garbage collector does not scan it.
type pushSpatial struct {
	pos     mgl64.Vec3
	box     cube.BBox
	cell    world.ChunkPos
	cellIdx int32
	next    int32
}

// pushCellSpan is the head and tail index into pushScratch.spatial for one
// chunk cell, preserving collection order without per-cell slice allocations.
type pushCellSpan struct {
	head int32
	tail int32
}

// pushMeta holds entity references and cached flags for one pushable entity.
type pushMeta struct {
	e          world.Entity
	l          Living
	gm         interface{ GameMode() world.GameMode }
	root       *world.EntityHandle
	vehicleGen uint32
	flags      uint8
}

// overlapSpan indexes one entity's broadphase overlaps inside a worker buffer.
type overlapSpan struct {
	worker int32
	start  int32
	count  int32
}

// pushScratch holds reusable per-tick buffers for TickEntityCollisions.
type pushScratch struct {
	spatial      []pushSpatial
	meta         []pushMeta
	cellSpans    []pushCellSpan
	cells        map[world.ChunkPos]int32
	others       []int32
	actOthers    []int32
	spans        []overlapSpan
	workerOthers [][]int32

	actTx          *world.Tx
	actIdx         int32
	actMaxCramming int
	actFn          func()
	mutGen         uint32
}

var pushScratchPool = sync.Pool{
	New: func() any {
		s := &pushScratch{
			cells: make(map[world.ChunkPos]int32, 64),
		}
		s.actFn = func() {
			s.pushEntities(s.actTx, s.actIdx, s.actMaxCramming)
		}
		return s
	},
}

// TickEntityCollisions ports LivingEntity.pushEntities for every living
// entity whose chunk ticks: each pushes the pushable entities overlapping its
// bounding box apart, and takes cramming damage when too many are packed
// together. Players push mobs but are not moved server-side, as the Bedrock
// client resolves its own collisions.
//
// Pushing only changes velocities, so overlaps are determined from the boxes at
// the start of the pass and then applied one entity after another in tick order.
// The flat serial sweep runs directly on the world owner without fanning out to
// worker goroutines, avoiding scheduling stalls under CPU contention.
func tickEntityCollisions(tx *world.Tx) {
	maxCramming := tx.World().GameRuleInt("max_entity_cramming")
	s := pushScratchPool.Get().(*pushScratch)
	defer s.release()

	s.mutGen = 1
	s.collect(tx)
	if len(s.spatial) < 2 {
		return
	}
	s.run(tx, maxCramming)
}

func (s *pushScratch) release() {
	oversized := cap(s.meta) > pushScratchMaxEntities || len(s.cellSpans) > pushScratchMaxCells
	clear(s.meta)
	s.meta = s.meta[:0]
	s.spatial = s.spatial[:0]
	s.cellSpans = s.cellSpans[:0]
	if len(s.cells) > 0 {
		clear(s.cells)
	}
	s.others = s.others[:0]
	s.actOthers = nil
	s.spans = s.spans[:0]
	for i := range s.workerOthers {
		s.workerOthers[i] = s.workerOthers[i][:0]
	}
	s.actTx = nil
	s.actIdx = 0
	s.actMaxCramming = 0
	if !oversized {
		pushScratchPool.Put(s)
	}
}

func (s *pushScratch) collect(tx *world.Tx) {
	var (
		lastCell    world.ChunkPos
		lastCellIdx int32 = -1
	)
	for e := range tx.TickingEntities() {
		l, ok := e.(Living)
		if !ok || l.Dead() {
			continue
		}
		var gm interface{ GameMode() world.GameMode }
		if m, ok := e.(interface{ GameMode() world.GameMode }); ok {
			if m.GameMode() == world.GameModeSpectator {
				continue
			}
			gm = m
		}
		t := e.H().Type()
		id := t.EncodeEntity()
		if id == "minecraft:armor_stand" {
			continue
		}
		var flags uint8
		if id == "minecraft:player" {
			flags = pushFlagPlayer
		}
		pos := e.Position()
		box := t.BBox(e).Translate(pos)
		cell := pushCell(pos)
		index := int32(len(s.spatial))

		var cellIdx int32
		if lastCellIdx >= 0 && cell == lastCell {
			cellIdx = lastCellIdx
			span := &s.cellSpans[cellIdx]
			s.spatial[span.tail].next = index
			span.tail = index
		} else if cid, exists := s.cells[cell]; exists {
			cellIdx = cid
			span := &s.cellSpans[cellIdx]
			s.spatial[span.tail].next = index
			span.tail = index
			lastCell, lastCellIdx = cell, cellIdx
		} else {
			cellIdx = int32(len(s.cellSpans))
			s.cells[cell] = cellIdx
			s.cellSpans = append(s.cellSpans, pushCellSpan{head: index, tail: index})
			lastCell, lastCellIdx = cell, cellIdx
		}

		s.spatial = append(s.spatial, pushSpatial{
			pos:     pos,
			box:     box,
			cell:    cell,
			cellIdx: cellIdx,
			next:    -1,
		})
		s.meta = append(s.meta, pushMeta{
			e:     e,
			l:     l,
			gm:    gm,
			flags: flags,
		})
	}
}

func (s *pushScratch) run(tx *world.Tx, maxCramming int) {
	if len(s.spatial) >= pushParallelThreshold {
		s.runParallel(tx, maxCramming)
		return
	}
	s.runSerial(tx, maxCramming)
}

func (s *pushScratch) runSerial(tx *world.Tx, maxCramming int) {
	s.actTx = tx
	s.actMaxCramming = maxCramming
	for i := range len(s.spatial) {
		m := &s.meta[i]
		if !s.stillPushable(m) {
			continue
		}
		others := s.appendOverlaps(i, s.others[:0])
		if len(others) == 0 {
			continue
		}
		if s.mutGen > 1 {
			others = s.filterPushable(others)
			if len(others) == 0 {
				continue
			}
		}
		s.others = others
		s.actOthers = others
		s.actIdx = int32(i)
		// Java pushes in the tick of the entity, LivingEntity.pushEntities:
		// a panic of it is the entity's.
		if !tx.ActOn(m.e, s.actFn) {
			s.mutGen++
		}
	}
}

func (s *pushScratch) computeOverlapsParallel() {
	n := len(s.spatial)
	if cap(s.spans) < n {
		s.spans = make([]overlapSpan, n)
	} else {
		s.spans = s.spans[:n]
	}
	workers := max(1, parallel.Workers())
	if len(s.workerOthers) < workers {
		grown := make([][]int32, workers)
		copy(grown, s.workerOthers)
		s.workerOthers = grown
	}
	for w := range workers {
		s.workerOthers[w] = s.workerOthers[w][:0]
	}
	parallel.ForWorkers(n, 16, func(worker, i int) {
		buf := s.workerOthers[worker]
		start := len(buf)
		buf = s.appendOverlaps(i, buf)
		s.workerOthers[worker] = buf
		s.spans[i] = overlapSpan{
			worker: int32(worker),
			start:  int32(start),
			count:  int32(len(buf) - start),
		}
	})
}

func (s *pushScratch) runParallel(tx *world.Tx, maxCramming int) {
	s.computeOverlapsParallel()
	s.actTx = tx
	s.actMaxCramming = maxCramming
	for i := range len(s.spatial) {
		sp := s.spans[i]
		if sp.count == 0 {
			continue
		}
		m := &s.meta[i]
		if !s.stillPushable(m) {
			continue
		}
		others := s.workerOthers[sp.worker][sp.start : sp.start+sp.count]
		if s.mutGen > 1 {
			others = s.filterPushable(others)
			if len(others) == 0 {
				continue
			}
		}
		s.actOthers = others
		s.actIdx = int32(i)
		if !tx.ActOn(m.e, s.actFn) {
			s.mutGen++
		}
	}
}

func (s *pushScratch) appendOverlaps(i int, dst []int32) []int32 {
	spatial := s.spatial
	sp := &spatial[i]
	box := sp.box
	near := box.Grow(2)
	low, high := pushCell(near.Min()), pushCell(near.Max())
	i32 := int32(i)

	if low == high {
		cid := sp.cellIdx
		if low != sp.cell {
			var ok bool
			cid, ok = s.cells[low]
			if !ok {
				return dst
			}
		}
		span := s.cellSpans[cid]
		if span.head == i32 && span.tail == i32 {
			return dst
		}
		for j := span.head; j >= 0; j = spatial[j].next {
			if j == i32 {
				continue
			}
			other := &spatial[j]
			if near.Vec3Within(other.pos) && other.box.IntersectsWith(box) {
				dst = append(dst, j)
			}
		}
		return dst
	}

	for x := low[0]; x <= high[0]; x++ {
		for z := low[1]; z <= high[1]; z++ {
			var cid int32
			if x == sp.cell[0] && z == sp.cell[1] {
				cid = sp.cellIdx
			} else {
				var ok bool
				cid, ok = s.cells[world.ChunkPos{x, z}]
				if !ok {
					continue
				}
			}
			for j := s.cellSpans[cid].head; j >= 0; j = spatial[j].next {
				if j == i32 {
					continue
				}
				other := &spatial[j]
				if near.Vec3Within(other.pos) && other.box.IntersectsWith(box) {
					dst = append(dst, j)
				}
			}
		}
	}
	return dst
}

func (s *pushScratch) stillPushable(m *pushMeta) bool {
	if s.mutGen == 1 {
		return true
	}
	if m.l.Dead() {
		return false
	}
	if m.gm != nil && m.gm.GameMode() == world.GameModeSpectator {
		return false
	}
	return true
}

func (s *pushScratch) filterPushable(others []int32) []int32 {
	n := 0
	for _, j := range others {
		if s.stillPushable(&s.meta[j]) {
			others[n] = j
			n++
		}
	}
	return others[:n]
}

// pushEntities ports the end of LivingEntity.pushEntities for the entity at
// index i overlapping s.actOthers: cramming damage beyond maxCramming, then pushes.
func (s *pushScratch) pushEntities(tx *world.Tx, i int32, maxCramming int) {
	a := &s.meta[i]
	others := s.actOthers
	if maxCramming > 0 && len(others) > maxCramming-1 && randInt(randomOf(tx, a.e), 4) == 0 {
		count := 0
		for _, j := range others {
			other := &s.meta[j]
			s.ensureVehicleInfo(tx, other)
			if other.flags&pushFlagIsPassenger == 0 {
				count++
			}
		}
		if count > maxCramming-1 {
			// Cramming damage may kill entities, change game mode, move them,
			// or dismount riders, so invalidate cached pass state first.
			s.mutGen++
			a.l.Hurt(6, CrammingDamageSource{})
		}
	}
	s.ensureVehicleInfo(tx, a)
	aRoot := a.root
	aCanPush := a.flags&(pushFlagIsVehicle|pushFlagPlayer|pushFlagIsPassenger) == 0
	var pa mgl64.Vec3
	if s.mutGen == 1 {
		pa = s.spatial[i].pos
	} else {
		pa = a.e.Position()
	}
	var (
		aVel      mgl64.Vec3
		aVelDirty bool
	)
	for _, j := range others {
		b := &s.meta[j]
		s.ensureVehicleInfo(tx, b)
		if aRoot == b.root {
			continue
		}
		var pb mgl64.Vec3
		if s.mutGen == 1 {
			pb = s.spatial[j].pos
		} else {
			pb = b.e.Position()
		}
		xa, za := pb[0]-pa[0], pb[2]-pa[2]
		dd := math.Max(math.Abs(xa), math.Abs(za))
		if dd < 0.01 {
			continue
		}
		dd = math.Sqrt(dd)
		xa, za = xa/dd, za/dd
		pow := min(1/dd, 1)
		xa, za = xa*pow*0.05, za*pow*0.05
		if aCanPush {
			if !aVelDirty {
				aVel = a.l.Velocity()
				aVelDirty = true
			}
			aVel = aVel.Add(mgl64.Vec3{-xa, 0, -za})
		}
		if b.flags&(pushFlagIsVehicle|pushFlagPlayer|pushFlagIsPassenger) == 0 {
			b.l.SetVelocity(b.l.Velocity().Add(mgl64.Vec3{xa, 0, za}))
		}
	}
	if aVelDirty {
		a.l.SetVelocity(aVel)
	}
}

func (s *pushScratch) ensureVehicleInfo(tx *world.Tx, m *pushMeta) {
	if m.vehicleGen == s.mutGen {
		return
	}
	m.vehicleGen = s.mutGen
	m.flags &^= pushFlagIsVehicle | pushFlagIsPassenger
	if isPassenger(m.e) {
		m.flags |= pushFlagIsPassenger
		m.root = rootVehicle(tx, m.e)
	} else {
		m.root = m.e.H()
	}
	if isVehicle(m.e) {
		m.flags |= pushFlagIsVehicle
	}
}

// pushCell returns the 4x4 sub-cell TickEntityCollisions files a position under.
func pushCell(pos mgl64.Vec3) world.ChunkPos {
	return world.ChunkPos{int32(math.Floor(pos[0])) >> 2, int32(math.Floor(pos[2])) >> 2}
}

// pushable ports Entity.isPushable: living entities that are alive, not
// spectating and not climbing, plus boats and minecarts.
func pushable(e world.Entity) bool {
	if isPushableVehicleID(e.H().Type().EncodeEntity()) {
		return true
	}
	if _, ent := e.(*Ent); ent {
		return false
	}
	l, ok := e.(Living)
	if !ok {
		return isPushableVehicleID(e.H().Type().EncodeEntity())
	}
	if l.Dead() {
		return false
	}
	if m, ok := e.(interface{ GameMode() world.GameMode }); ok && m.GameMode() == world.GameModeSpectator {
		return false
	}
	return e.H().Type().EncodeEntity() != "minecraft:armor_stand"
}

func isPushableVehicleID(id string) bool {
	switch id {
	case "minecraft:boat", "minecraft:chest_boat", "minecraft:minecart", "minecraft:chest_minecart",
		"minecraft:hopper_minecart", "minecraft:tnt_minecart", "minecraft:command_block_minecart":
		return true
	}
	return false
}

func isPassenger(e world.Entity) bool {
	r, ok := e.(Rider)
	return ok && r.Riding() != nil
}

func rootVehicle(tx *world.Tx, e world.Entity) *world.EntityHandle {
	h := e.H()
	for range 16 {
		r, ok := e.(Rider)
		if !ok || r.Riding() == nil {
			return h
		}
		next, ok := r.Riding().Entity(tx)
		if !ok {
			return r.Riding()
		}
		h, e = next.H(), next
	}
	return h
}

// isVehicle reports whether another entity is riding e.
func isVehicle(e world.Entity) bool {
	l, ok := e.(Linker)
	if !ok {
		return false
	}
	for _, link := range l.Links() {
		if link.Ridden == e.H() {
			return true
		}
	}
	return false
}
