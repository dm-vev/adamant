package world

import (
	"fmt"
	"time"

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
	// maxPrefetched bounds the reads ahead not yet taken.
	maxPrefetched = 4 * prefetchAhead
	// prefetchLife is how long a read ahead is trusted: one a loader never
	// came to ask for is dropped, so that a column is never loaded from what
	// the provider held long ago.
	prefetchLife = 10 * time.Second
)

// columnRead is a column being read from the provider ahead of its load.
type columnRead struct {
	done chan struct{}
	col  *chunk.Column
	err  error
	at   time.Time
}

// prefetchColumn starts reading the column at pos from the provider on
// another goroutine, unless it is loaded, being read, or the saver holds
// something of it that the provider does not yet: loadChunk then reads it
// from the saver or through its commit, as it does for any column. A column
// not loaded cannot be saved, so what is read of it stays what the provider
// holds until it is loaded and the read is taken. It must be called from the
// owner.
func (w *World) prefetchColumn(pos ChunkPos) {
	if w.conf.Synchronous || w.closed.Load() {
		return
	}
	if p, ok := w.conf.Provider.(concurrentLoader); !ok || !p.LoadsConcurrently() {
		return
	}
	if _, loaded := w.chunks[pos]; loaded {
		return
	}
	if r, reading := w.prefetched[pos]; reading && time.Since(r.at) < prefetchLife {
		return
	}
	if len(w.prefetched) >= maxPrefetched {
		w.dropStalePrefetched()
		if len(w.prefetched) >= maxPrefetched {
			return
		}
	}
	if w.saver != nil && !w.saver.idle(pos) {
		return
	}
	w.scheduleMu.Lock()
	if w.closed.Load() {
		w.scheduleMu.Unlock()
		return
	}
	w.generatorRunning.Add(1)
	w.scheduleMu.Unlock()

	r := &columnRead{done: make(chan struct{}), at: time.Now()}
	w.prefetched[pos] = r
	provider, dim := w.conf.Provider, w.conf.Dim
	go func() {
		defer w.generatorRunning.Done()
		defer close(r.done)
		defer func() {
			if v := recover(); v != nil {
				r.col, r.err = nil, fmt.Errorf("load column %v (%v): panic: %v", pos, dim, v)
			}
		}()
		w.readSlots <- struct{}{}
		defer func() { <-w.readSlots }()
		r.col, r.err = provider.LoadColumn(pos, dim)
	}()
}

// columnReading reports whether the column at pos is being read ahead and
// the read has not ended, starting to read it ahead if it may be. A loader
// leaves such a column for a later tick instead of having the owner wait for
// the read: the owner keeps ticking while the provider reads, as Java's main
// thread does while its IO worker reads chunks. It must be called from the
// owner.
func (w *World) columnReading(pos ChunkPos) bool {
	w.prefetchColumn(pos)
	r := w.prefetched[pos]
	if r == nil || time.Since(r.at) > prefetchLife {
		return false
	}
	select {
	case <-r.done:
		return false
	default:
		return true
	}
}

// takePrefetched takes the column read ahead at pos, waiting for the read to
// end, and reports whether there was one to take. It must be called from the
// owner, once the saver is known to hold nothing of pos.
func (w *World) takePrefetched(pos ChunkPos) (col *chunk.Column, taken bool, err error) {
	r := w.prefetched[pos]
	if r == nil {
		return nil, false, nil
	}
	delete(w.prefetched, pos)
	if time.Since(r.at) > prefetchLife {
		return nil, false, nil
	}
	<-r.done
	return r.col, true, r.err
}

// dropStalePrefetched forgets the reads ahead nobody took in time.
func (w *World) dropStalePrefetched() {
	for pos, r := range w.prefetched {
		if time.Since(r.at) > prefetchLife {
			delete(w.prefetched, pos)
		}
	}
}

// prefetchAhead has the world read the columns next in the queue of the
// loader before they are asked for.
func (l *Loader) prefetchAhead(w *World) {
	var ahead [prefetchAhead]ChunkPos
	l.mu.RLock()
	next := append(ahead[:0], l.loadQueue[:min(len(l.loadQueue), len(ahead))]...)
	same := l.w == w
	l.mu.RUnlock()
	if !same {
		return
	}
	for _, pos := range next {
		w.prefetchColumn(pos)
	}
}
