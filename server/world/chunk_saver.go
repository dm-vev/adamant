package world

import (
	"sync"

	"github.com/df-mc/dragonfly/server/world/chunk"
)

// chunkSaver writes saved columns through the provider on its own goroutine,
// in the order they were saved, as Java's chunk IO worker does, so a tick
// never waits for chunk encoding or the disk. A newer save of a position
// still waiting replaces the older one.
type chunkSaver struct {
	w *World

	mu    sync.Mutex
	cond  sync.Cond
	queue []*pendingColumn
	// waiting holds the queued save of each position.
	waiting map[ChunkPos]*pendingColumn
	// failed holds the saves that failed to write and were not saved again
	// since, for the world goroutine to take with takeFailed.
	failed map[ChunkPos]pendingColumn
	// writing is the position being written, valid while busy.
	writing ChunkPos
	busy    bool
	closed  bool
	done    chan struct{}
}

// pendingColumn is a column waiting to be written. A nil col was taken by
// the world goroutine, which wrote it itself. saved is the savedIn of the
// column saved.
type pendingColumn struct {
	pos   ChunkPos
	col   *chunk.Column
	saved uint64
}

func newChunkSaver(w *World) *chunkSaver {
	s := &chunkSaver{w: w, waiting: make(map[ChunkPos]*pendingColumn), failed: make(map[ChunkPos]pendingColumn), done: make(chan struct{})}
	s.cond.L = &s.mu
	go s.run()
	return s
}

// store queues col, the save numbered saved, to be written for pos. It
// replaces a save of pos that failed to write.
func (s *chunkSaver) store(pos ChunkPos, col *chunk.Column, saved uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.failed, pos)
	if p, ok := s.waiting[pos]; ok {
		p.col, p.saved = col, saved
		return
	}
	p := &pendingColumn{pos: pos, col: col, saved: saved}
	s.waiting[pos] = p
	s.queue = append(s.queue, p)
	s.cond.Broadcast()
}

// writeNow writes the save of pos still waiting, if any, or else the last
// one that failed to write, before the world goroutine reads the column back
// from the provider.
func (s *chunkSaver) writeNow(pos ChunkPos) {
	s.mu.Lock()
	p, ok := s.waiting[pos]
	if ok {
		delete(s.waiting, pos)
	}
	col := (*chunk.Column)(nil)
	if ok {
		col, p.col = p.col, nil
	}
	// An older save of pos being written must land before this one.
	for s.busy && s.writing == pos {
		s.cond.Wait()
	}
	if f, failed := s.failed[pos]; failed {
		delete(s.failed, pos)
		if col == nil {
			col = f.col
		}
	}
	s.mu.Unlock()
	if col != nil {
		_ = s.w.writeColumn(pos, col)
	}
}

// takeFailed returns the saves that failed to write and were not saved
// again since, and forgets them.
func (s *chunkSaver) takeFailed() map[ChunkPos]pendingColumn {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.failed) == 0 {
		return nil
	}
	failed := s.failed
	s.failed = make(map[ChunkPos]pendingColumn)
	return failed
}

// flush waits until every column saved so far was written.
func (s *chunkSaver) flush() {
	s.mu.Lock()
	for len(s.queue) != 0 || s.busy {
		s.cond.Wait()
	}
	s.mu.Unlock()
}

// close writes the columns still waiting and stops the saver.
func (s *chunkSaver) close() {
	s.mu.Lock()
	s.closed = true
	s.cond.Broadcast()
	s.mu.Unlock()
	<-s.done
}

func (s *chunkSaver) run() {
	defer close(s.done)
	for {
		s.mu.Lock()
		for len(s.queue) == 0 && !s.closed {
			s.cond.Wait()
		}
		if len(s.queue) == 0 {
			s.mu.Unlock()
			return
		}
		p := s.queue[0]
		s.queue[0] = nil
		s.queue = s.queue[1:]
		col, saved := p.col, p.saved
		if col != nil {
			delete(s.waiting, p.pos)
			s.writing, s.busy = p.pos, true
		}
		s.mu.Unlock()
		if col == nil {
			continue
		}
		err := s.w.writeColumn(p.pos, col)
		s.mu.Lock()
		if _, again := s.waiting[p.pos]; err != nil && !again {
			s.failed[p.pos] = pendingColumn{pos: p.pos, col: col, saved: saved}
		}
		s.busy = false
		s.cond.Broadcast()
		s.mu.Unlock()
	}
}
