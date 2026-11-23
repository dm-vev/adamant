package world

import (
	"fmt"
	"sync"
	"sync/atomic"
)

// TickPhase identifies when a registered world tick system runs within an
// active simulation tick.
type TickPhase uint8

const (
	// PhasePreEntities runs on each simulation tick before entities tick.
	PhasePreEntities TickPhase = iota
	// PhasePostEntities runs on each simulation tick immediately after entities tick.
	PhasePostEntities
	// PhasePostTick runs at the end of each simulation tick after blocks, fluids,
	// redstone, and post-block entity phases complete.
	PhasePostTick
)

type tickSystemEntry struct {
	id      uint64
	name    string
	fn      func(tx *Tx, tick int64)
	removed atomic.Bool
}

type tickSystemRegistry struct {
	mu     sync.Mutex
	nextID uint64
	phases [3]atomic.Pointer[[]*tickSystemEntry]
}

// AddSystem registers fn to run during phase on every active simulation tick of
// w, returning a function that removes the system. Panics recovered from fn are
// contained as subsystem faults so one failing system does not abort other
// systems or crash the world tick.
func (w *World) AddSystem(phase TickPhase, fn func(tx *Tx, tick int64)) func() {
	if phase > PhasePostTick {
		panic(fmt.Sprintf("world.AddSystem: invalid tick phase %d", phase))
	}
	if w == nil || fn == nil {
		return func() {}
	}
	w.systems.mu.Lock()
	w.systems.nextID++
	id := w.systems.nextID
	entry := &tickSystemEntry{
		id:   id,
		name: fmt.Sprintf("tick system %d", id),
		fn:   fn,
	}
	var cur []*tickSystemEntry
	if curPtr := w.systems.phases[phase].Load(); curPtr != nil {
		cur = *curPtr
	}
	next := make([]*tickSystemEntry, len(cur)+1)
	copy(next, cur)
	next[len(cur)] = entry
	w.systems.phases[phase].Store(&next)
	w.systems.mu.Unlock()

	return func() {
		if !entry.removed.CompareAndSwap(false, true) {
			return
		}
		w.systems.mu.Lock()
		defer w.systems.mu.Unlock()
		curPtr := w.systems.phases[phase].Load()
		if curPtr == nil || len(*curPtr) == 0 {
			return
		}
		filtered := make([]*tickSystemEntry, 0, len(*curPtr)-1)
		for _, item := range *curPtr {
			if item != entry && !item.removed.Load() {
				filtered = append(filtered, item)
			}
		}
		w.systems.phases[phase].Store(&filtered)
	}
}

func (w *World) runSystems(tx *Tx, phase TickPhase, tick int64) {
	ptr := w.systems.phases[phase].Load()
	if ptr == nil || len(*ptr) == 0 {
		return
	}
	for _, sys := range *ptr {
		if sys.removed.Load() {
			continue
		}
		w.runSystemSafe(tx, tick, sys)
	}
}

func (w *World) runSystemSafe(tx *Tx, tick int64, sys *tickSystemEntry) {
	defer func() {
		if r := recover(); r != nil {
			w.stepPanicked(tick, sys.name, r)
		}
	}()
	sys.fn(tx, tick)
}
