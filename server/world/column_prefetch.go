package world

import (
	"fmt"
	"time"

	"github.com/df-mc/dragonfly/internal/faultcheck"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// concurrentLoader is a Provider whose LoadColumn may be called from several
// goroutines at once, and while columns are stored: mcdb's is, as LevelDB
// reads are. The world reads the columns its loaders are about to ask for
// ahead of its owner on goroutines of their own, and only for a provider that
// says so; one that does not is read from the owner, as it always was.
type concurrentLoader interface {
	LoadsConcurrently() bool
}

// A column read from the provider takes about as long as a tick has time for
// when a player arrives: LevelDB reads and decodes its blocks, its entities
// and its scheduled ticks, some 2.5 ms of the owner for each of the up to 16
// a tick a loader asks for. prefetchColumn reads the columns the loaders
// ask for next while the owner does something else, and loadChunk takes the
// result where it would have read the column itself.
const (
	// prefetchAhead is how many of the columns next in line a loader reads
	// ahead: its two next ticks of loads.
	prefetchAhead = 2 * 4 * generationRequestsPerLoad
	// maxPrefetched is the base bound on reads ahead not yet taken.
	maxPrefetched = 4 * prefetchAhead
	// maxPrefetchedCap caps dynamic scaling of the prefetch buffer so runaway
	// loader counts cannot grow prefetched reads unboundedly.
	maxPrefetchedCap = 256 * prefetchAhead
	// prefetchLife is how long a read ahead is trusted: one a loader never
	// came to ask for is dropped, so that a column is never loaded from what
	// the provider held long ago.
	prefetchLife = 10 * time.Second
	// prefetchPruneInterval bounds how often dropStalePrefetched scans
	// w.prefetched when the buffer is full.
	prefetchPruneInterval = 500 * time.Millisecond
)

// columnRead is a column being read from the provider ahead of its load.
type columnRead struct {
	done      chan struct{}
	col       *chunk.Column
	err       error
	at        time.Time
	from      Provider
	cancelled bool
}

// saverReadState describes how the background saver affects reading a column
// ahead of its load on the owner.
type saverReadState uint8

const (
	saverReadIdle saverReadState = iota
	saverReadStaged
	saverReadDefer
	saverReadCommit
	saverReadSync
)

// canPrefetch reports whether w reads columns ahead off the owner.
func (w *World) canPrefetch() bool {
	if w.conf.Synchronous || w.closed.Load() {
		return false
	}
	p, ok := w.conf.Provider.(concurrentLoader)
	return ok && p.LoadsConcurrently()
}

// viewerCount returns the number of loaders currently registered on w.
func (w *World) viewerCount() int {
	w.viewerMu.Lock()
	n := len(w.viewers)
	w.viewerMu.Unlock()
	return n
}

// prefetchLimit returns the soft capacity of w.prefetched, scaling with active
// loaders while preserving maxPrefetched as the minimum floor.
func (w *World) prefetchLimit() int {
	limit := w.viewerCount() * prefetchAhead
	if limit < maxPrefetched {
		return maxPrefetched
	}
	return min(limit, maxPrefetchedCap)
}

// prefetchAheadCount returns how many columns a single loader speculatively
// prefetches per tick, tapering per-loader depth when many loaders share the
// readSlots worker pool so head-of-line columns are not starved.
func (w *World) prefetchAheadCount() int {
	loaders := w.viewerCount()
	if loaders <= 4 {
		return prefetchAhead
	}
	return max(8, (prefetchAhead*4)/loaders)
}

// saverStateFor reports whether the saver holds pos in RAM staging, is
// actively encoding/writing it, has it in an open commit, or holds a failed
// write of it.
func (w *World) saverStateFor(pos ChunkPos) (saverReadState, Provider) {
	s := w.saver
	if s == nil {
		return saverReadIdle, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.encoding != nil && s.encoding.pos == pos {
		return saverReadDefer, nil
	}
	if _, ok := s.staged[pos]; ok {
		return saverReadStaged, nil
	}
	if _, ok := s.waiting[pos]; ok || (s.busy && s.writing == pos) {
		return saverReadDefer, nil
	}
	if _, ok := s.failed[pos]; ok {
		return saverReadSync, nil
	}
	if commit, ok := s.inCommit[pos]; ok {
		if commit != nil {
			return saverReadCommit, commit
		}
		return saverReadSync, nil
	}
	return saverReadIdle, nil
}

// prefetchColumn starts reading the column at pos from the provider on
// another goroutine, unless it is loaded, being read, or the saver holds
// something of it that cannot be read ahead. A column not loaded cannot be
// saved, so what is read of it stays what the provider holds until it is
// loaded and the read is taken. It must be called from the owner.
func (w *World) prefetchColumn(pos ChunkPos) {
	if !w.canPrefetch() {
		return
	}
	if _, loaded := w.chunks[pos]; loaded {
		return
	}
	state, commit := w.saverStateFor(pos)
	var from Provider
	switch state {
	case saverReadIdle:
	case saverReadCommit:
		from = commit
	default:
		return
	}
	w.startPrefetch(pos, from, false)
}

// startPrefetch launches an asynchronous LoadColumn for pos from from (or
// w.conf.Provider if from is nil). When demand is true (the column is at the
// front of a loader's queue), it permits a small demand slack above the
// speculative prefetch limit so one loader's lookahead cannot starve another.
func (w *World) startPrefetch(pos ChunkPos, from Provider, demand bool) bool {
	now := time.Now()
	if r, reading := w.prefetched[pos]; reading {
		if r.from == from && now.Sub(r.at) < prefetchLife {
			select {
			case <-r.done:
				if !r.cancelled {
					return true
				}
			default:
				return true
			}
		}
		delete(w.prefetched, pos)
	}
	limit := maxPrefetched
	if len(w.prefetched) >= limit {
		limit = w.prefetchLimit()
		if len(w.prefetched) >= limit {
			w.maybeDropStalePrefetched(now)
			if demand {
				limit += max(16, w.viewerCount())
			}
			if len(w.prefetched) >= limit {
				return false
			}
		}
	}
	w.scheduleMu.Lock()
	if w.closed.Load() {
		w.scheduleMu.Unlock()
		return false
	}
	w.generatorRunning.Add(1)
	w.scheduleMu.Unlock()

	r := &columnRead{done: make(chan struct{}), at: now, from: from}
	w.prefetched[pos] = r
	provider := w.conf.Provider
	if from != nil {
		provider = from
	}
	dim := w.conf.Dim
	go func() {
		defer w.generatorRunning.Done()
		defer close(r.done)
		defer func() {
			if v := recover(); v != nil {
				faultcheck.Report("world column load", v)
				r.col, r.err = nil, fmt.Errorf("load column %v (%v): panic: %v", pos, dim, v)
			}
		}()
		select {
		case w.readSlots <- struct{}{}:
			defer func() { <-w.readSlots }()
		case <-w.closeStarted:
			r.cancelled = true
			return
		}
		if w.closed.Load() {
			r.cancelled = true
			return
		}
		r.col, r.err = provider.LoadColumn(pos, dim)
	}()
	return true
}

// columnReading reports whether the column at pos must be deferred to a later
// tick rather than loaded synchronously on the owner: it starts reading the
// column ahead if needed, and for an async concurrentLoader only returns false
// once the read is already in memory (prefetched, staged, or failed-save
// recovery). It must be called from the owner.
func (w *World) columnReading(pos ChunkPos) bool {
	if !w.canPrefetch() {
		return false
	}
	state, commit := w.saverStateFor(pos)
	var from Provider
	switch state {
	case saverReadIdle:
	case saverReadCommit:
		from = commit
	case saverReadDefer:
		return true
	default:
		return false
	}
	now := time.Now()
	if r := w.prefetched[pos]; r != nil && r.from == from && now.Sub(r.at) < prefetchLife-time.Millisecond {
		select {
		case <-r.done:
			if !r.cancelled {
				return false
			}
			delete(w.prefetched, pos)
		default:
			return true
		}
	}
	// Not ready yet: start an async read if capacity permits, and always defer
	// so the owner never falls back to synchronous LevelDB I/O when saturated.
	w.startPrefetch(pos, from, true)
	return true
}

// takePrefetched takes the column read ahead at pos, waiting for the read to
// end, and reports whether there was one to take. It must be called from the
// owner, once the saver is known to hold nothing of pos.
func (w *World) takePrefetched(pos ChunkPos) (col *chunk.Column, taken bool, err error) {
	col, _, taken, err = w.takePrefetchedState(pos, nil)
	return col, taken, err
}

// takePrefetchedState takes a prefetched read for pos matching from (or any
// source when from is nil), reporting whether it came from an uncommitted
// commit (staged) and whether an entry was taken.
func (w *World) takePrefetchedState(pos ChunkPos, from Provider) (col *chunk.Column, staged, taken bool, err error) {
	r := w.prefetched[pos]
	if r == nil {
		return nil, false, false, nil
	}
	delete(w.prefetched, pos)
	if from != nil && r.from != from {
		return nil, false, false, nil
	}
	if time.Since(r.at) > prefetchLife {
		return nil, false, false, nil
	}
	<-r.done
	if r.cancelled {
		return nil, false, false, nil
	}
	return r.col, r.from != nil, true, r.err
}

// maybeDropStalePrefetched prunes stale reads at most once per
// prefetchPruneInterval so saturated ticks do not repeatedly scan the map.
func (w *World) maybeDropStalePrefetched(now time.Time) {
	if !w.prefetchPrunedAt.IsZero() && now.Sub(w.prefetchPrunedAt) < prefetchPruneInterval {
		return
	}
	w.prefetchPrunedAt = now
	w.dropStalePrefetchedAt(now)
}

// dropStalePrefetched forgets the reads ahead nobody took in time.
func (w *World) dropStalePrefetched() {
	w.dropStalePrefetchedAt(time.Now())
}

func (w *World) dropStalePrefetchedAt(now time.Time) {
	for pos, r := range w.prefetched {
		if now.Sub(r.at) > prefetchLife {
			delete(w.prefetched, pos)
		}
	}
}

// prefetchAhead has the world read the columns next in the queue of the
// loader before they are asked for.
func (l *Loader) prefetchAhead(w *World) {
	if !w.canPrefetch() {
		return
	}
	limit := w.prefetchAheadCount()
	var ahead [prefetchAhead]ChunkPos
	l.mu.RLock()
	next := append(ahead[:0], l.loadQueue[:min(len(l.loadQueue), limit)]...)
	same := l.w == w
	l.mu.RUnlock()
	if !same {
		return
	}
	for _, pos := range next {
		w.prefetchColumn(pos)
	}
}
