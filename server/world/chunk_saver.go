package world

import (
	"fmt"
	"slices"
	"sync"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/dm-vev/adamant/net/nbt"
)

// chunkSaver writes saved columns through the provider on its own goroutine,
// in the order they were saved, as Java's chunk IO worker does, so a tick
// never waits for chunk encoding or the disk. A newer save of a position
// still waiting replaces the older one.
//
// The columns a world unloads are not written at once: they are staged
// until the world is next captured, so that the disk holds the column as it
// was at a capture and not as it was at an unload, which would pair it with
// players and other columns of another instant. The saver encodes a staged
// column on its goroutine, as the provider would, and keeps the bytes. The
// capture then writes the columns staged before the columns it captured
// (commitStaged), and a column loaded again takes its staged copy back
// (takeStaged). When the staged columns take more than the limit of the
// world, the saver asks for a capture at once.
type chunkSaver struct {
	w *World

	mu    sync.Mutex
	cond  sync.Cond
	queue []*pendingColumn
	// waiting holds the queued write of each position.
	waiting map[ChunkPos]*pendingColumn
	// staged holds the column of each position unloaded since the world
	// was last captured. stagedBytes is the size of those encoded, and
	// stagedSeq numbers them in the order staged. full is set once the
	// saver asked for a capture as the staged columns exceeded the limit,
	// until they are committed.
	staged      map[ChunkPos]*stagedColumn
	stagedBytes int
	stagedSeq   uint64
	full        bool
	// encoding is the staged column being encoded, if any.
	encoding *stagedColumn
	// failed holds the saves that failed to write and were not saved again
	// since, for the world goroutine to take with takeFailed.
	failed map[ChunkPos]pendingColumn
	// panicked counts, by position, the writes in a row that panicked: a
	// save whose write panicked is tried once more, then dropped.
	panicked map[ChunkPos]int
	// committing holds the writes made into each commit not yet
	// committed, and inCommit the commit holding the last write of each
	// position, read back through it until it is committed (committed).
	committing map[Commit][]pendingColumn
	inCommit   map[ChunkPos]Commit
	// writing is the position being written, valid while busy.
	writing ChunkPos
	busy    bool
	closed  bool
	done    chan struct{}
}

// pendingColumn is a column waiting to be written, as a column or, if it
// was staged and encoded since, as its encoding. A pendingColumn with
// neither was taken by the world goroutine, which wrote it itself. saved is
// the savedIn of the column saved. commit, if not nil, is the commit of the
// checkpoint the column is written into, instead of the provider. A
// pendingColumn with stage set is no write but the encoding of that staged
// column.
type pendingColumn struct {
	pos    ChunkPos
	col    *chunk.Column
	enc    *encodedColumn
	saved  uint64
	commit Commit
	stage  *stagedColumn
}

// stagedColumn is a column unloaded since the world was last captured: the
// column handed over by the unload until the saver encoded it, then its
// encoding. seq orders the staged columns, saved is the savedIn of the
// column.
type stagedColumn struct {
	pos   ChunkPos
	seq   uint64
	saved uint64
	col   *chunk.Column
	enc   *encodedColumn
}

// encodedColumn is a column encoded as a provider stores it: its blocks and
// biomes with chunk.DiskEncoding, its entities and block entities as little
// endian NBT.
type encodedColumn struct {
	blocks         chunk.SerialisedData
	entities       []encodedEntity
	blockEntities  []encodedBlockEntity
	ticks          []chunk.SavedScheduledTick
	postProcessing [][]uint16
	inhabitedTime  uint64
	size           int
}

type encodedEntity struct {
	id   int64
	data []byte
}

type encodedBlockEntity struct {
	pos  cube.Pos
	data []byte
}

// encodeColumn encodes col. It only reads col.
func encodeColumn(col *chunk.Column) (*encodedColumn, error) {
	enc := &encodedColumn{
		blocks:         chunk.Encode(col.Chunk, chunk.DiskEncoding),
		ticks:          slices.Clone(col.ScheduledTicks),
		postProcessing: clonePostProcessing(col.PostProcessing),
		inhabitedTime:  col.InhabitedTime,
	}
	for _, sub := range enc.blocks.SubChunks {
		enc.size += len(sub)
	}
	enc.size += len(enc.blocks.Biomes) + 64*len(enc.ticks)
	for _, e := range col.Entities {
		b, err := nbt.MarshalEncoding(e.Data, nbt.LittleEndian)
		if err != nil {
			return nil, fmt.Errorf("encode entity %d: %w", e.ID, err)
		}
		enc.entities = append(enc.entities, encodedEntity{id: e.ID, data: b})
		enc.size += len(b)
	}
	for _, be := range col.BlockEntities {
		b, err := nbt.MarshalEncoding(be.Data, nbt.LittleEndian)
		if err != nil {
			return nil, fmt.Errorf("encode block entity at %v: %w", be.Pos, err)
		}
		enc.blockEntities = append(enc.blockEntities, encodedBlockEntity{pos: be.Pos, data: b})
		enc.size += len(b)
	}
	return enc, nil
}

// decode decodes the column encoded, as a provider reads back the column
// it stored.
func (enc *encodedColumn) decode(br BlockRegistry, r cube.Range) (*chunk.Column, error) {
	c, err := chunk.DiskDecode(br, enc.blocks, r)
	if err != nil {
		return nil, fmt.Errorf("decode chunk data: %w", err)
	}
	col := &chunk.Column{
		Chunk:          c,
		Entities:       make([]chunk.Entity, 0, len(enc.entities)),
		BlockEntities:  make([]chunk.BlockEntity, 0, len(enc.blockEntities)),
		ScheduledTicks: slices.Clone(enc.ticks),
		PostProcessing: clonePostProcessing(enc.postProcessing),
		InhabitedTime:  enc.inhabitedTime,
	}
	for _, e := range enc.entities {
		ent := chunk.Entity{ID: e.id, Data: make(map[string]any)}
		if err := nbt.UnmarshalEncoding(e.data, &ent.Data, nbt.LittleEndian); err != nil {
			return nil, fmt.Errorf("decode entity %d: %w", e.id, err)
		}
		col.Entities = append(col.Entities, ent)
	}
	for _, be := range enc.blockEntities {
		b := chunk.BlockEntity{Pos: be.pos, Data: make(map[string]any)}
		if err := nbt.UnmarshalEncoding(be.data, &b.Data, nbt.LittleEndian); err != nil {
			return nil, fmt.Errorf("decode block entity at %v: %w", be.pos, err)
		}
		col.BlockEntities = append(col.BlockEntities, b)
	}
	return col, nil
}

func newChunkSaver(w *World) *chunkSaver {
	s := &chunkSaver{
		w:          w,
		waiting:    make(map[ChunkPos]*pendingColumn),
		staged:     make(map[ChunkPos]*stagedColumn),
		failed:     make(map[ChunkPos]pendingColumn),
		panicked:   make(map[ChunkPos]int),
		committing: make(map[Commit][]pendingColumn),
		inCommit:   make(map[ChunkPos]Commit),
		done:       make(chan struct{}),
	}
	s.cond.L = &s.mu
	go s.run()
	return s
}

// store queues col, the save numbered saved, to be written for pos, into
// commit if not nil. It replaces a save of pos that failed to write.
func (s *chunkSaver) store(pos ChunkPos, col *chunk.Column, saved uint64, commit Commit) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.storeLocked(pendingColumn{pos: pos, col: col, saved: saved, commit: commit})
}

// storeLocked queues the write of p, replacing a write of its position
// still waiting or that failed. A write into a commit replacing one
// waiting goes into that commit; a write into none replacing one into a
// commit goes into the commit too, which then holds a column newer than
// its checkpoint rather than none. The caller holds mu.
func (s *chunkSaver) storeLocked(p pendingColumn) {
	delete(s.failed, p.pos)
	if w, ok := s.waiting[p.pos]; ok {
		w.col, w.enc, w.saved = p.col, p.enc, p.saved
		if p.commit != nil {
			w.commit = p.commit
		}
		return
	}
	q := &pendingColumn{pos: p.pos, col: p.col, enc: p.enc, saved: p.saved, commit: p.commit}
	s.waiting[p.pos] = q
	s.queue = append(s.queue, q)
	s.cond.Broadcast()
}

// restore stages again the save p of a column unloaded, which failed to
// write, for the next capture to write, unless the column was staged again
// since.
func (s *chunkSaver) restore(p pendingColumn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.staged[p.pos]; ok {
		return
	}
	s.stagedSeq++
	sc := &stagedColumn{pos: p.pos, seq: s.stagedSeq, saved: p.saved, col: p.col, enc: p.enc}
	if sc.enc != nil {
		s.stagedBytes += sc.enc.size
	}
	s.staged[p.pos] = sc
	if sc.enc == nil {
		s.queue = append(s.queue, &pendingColumn{pos: p.pos, stage: sc})
		s.cond.Broadcast()
	}
}

// stage stages col, the column at pos as it unloaded, the save numbered
// saved, until the world is next captured. The saver owns col from then.
func (s *chunkSaver) stage(pos ChunkPos, col *chunk.Column, saved uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if old, ok := s.staged[pos]; ok && old.enc != nil {
		s.stagedBytes -= old.enc.size
	}
	s.stagedSeq++
	sc := &stagedColumn{pos: pos, seq: s.stagedSeq, saved: saved, col: col}
	s.staged[pos] = sc
	s.queue = append(s.queue, &pendingColumn{pos: pos, stage: sc})
	s.cond.Broadcast()
}

// commitStaged queues the columns staged to be written, into commit if not
// nil, in the order they were staged, before any column saved after: the
// world calls it when it is captured, so that the columns unloaded since
// the last capture are written with this one. It waits for a staged column
// being encoded.
func (s *chunkSaver) commitStaged(commit Commit) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	for s.encoding != nil {
		s.cond.Wait()
	}
	staged := make([]*stagedColumn, 0, len(s.staged))
	for _, sc := range s.staged {
		staged = append(staged, sc)
	}
	slices.SortFunc(staged, func(a, b *stagedColumn) int { return compareSeq(a.seq, b.seq) })
	for _, sc := range staged {
		s.storeLocked(pendingColumn{pos: sc.pos, col: sc.col, enc: sc.enc, saved: sc.saved, commit: commit})
	}
	clear(s.staged)
	s.stagedBytes, s.full = 0, false
	return len(staged)
}

func compareSeq(a, b uint64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// takeStaged takes the staged column of pos out of staging, decoded as the
// provider would read it back, for the world goroutine loading pos again.
// It reports false if pos has no column staged: a write of pos still
// waiting, or the last that failed, is then written first, for the world
// to read the column back from the Provider returned: the commit that holds
// it, if a commit not yet committed does, or nil for the world's provider.
func (s *chunkSaver) takeStaged(pos ChunkPos) (*chunk.Column, bool, Provider, error) {
	s.mu.Lock()
	// The saver encodes a staged column it owns: it lands first.
	for s.encoding != nil && s.encoding.pos == pos {
		s.cond.Wait()
	}
	sc, ok := s.staged[pos]
	if ok {
		delete(s.staged, pos)
		if sc.enc != nil {
			s.stagedBytes -= sc.enc.size
		}
	}
	s.mu.Unlock()
	if !ok {
		var from Provider
		if c := s.writeNow(pos); c != nil {
			from = c
		}
		return nil, false, from, nil
	}
	enc := sc.enc
	if enc == nil {
		var err error
		if enc, err = encodeColumn(sc.col); err != nil {
			return nil, true, nil, fmt.Errorf("load staged column: %w", err)
		}
	}
	col, err := enc.decode(s.w.conf.Blocks, s.w.Range())
	if err != nil {
		return nil, true, nil, fmt.Errorf("load staged column: %w", err)
	}
	return col, true, nil, nil
}

// writeNow writes the save of pos still waiting, if any, or else the last
// one that failed to write, before the world goroutine reads the column back
// from the provider. It returns the commit not yet committed that holds the
// last write of pos, which the column is to be read back through.
func (s *chunkSaver) writeNow(pos ChunkPos) Commit {
	s.mu.Lock()
	p, ok := s.waiting[pos]
	var now pendingColumn
	if ok {
		delete(s.waiting, pos)
		now = *p
		p.col, p.enc = nil, nil
	}
	// An older save of pos being written must land before this one.
	for s.busy && s.writing == pos {
		s.cond.Wait()
	}
	if f, failed := s.failed[pos]; failed {
		delete(s.failed, pos)
		if !ok {
			now, ok = f, true
		}
	}
	s.mu.Unlock()
	if ok {
		_, _ = s.write(now)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inCommit[pos]
}

// saveAttempts is how many writes of a save in a row may panic before the
// saver drops it.
const saveAttempts = 2

// write writes the column of p through the provider, or into its commit,
// decoding it first if it is encoded. A panic of it, of decoding the column
// or of the provider, is reported as a fault of saving and returned as an
// error, so that the saver goes on with the next column and flush does not
// wait for it forever.
func (s *chunkSaver) write(p pendingColumn) (panicked bool, err error) {
	defer func() {
		if r := recover(); r != nil {
			f := newFault(FaultSave, r)
			f.Type, f.Pos = "chunk writer", cube.Pos{int(p.pos[0]) << 4, 0, int(p.pos[1]) << 4}
			s.w.reportFault(f, quarantineKey{})
			panicked, err = true, fmt.Errorf("save chunk: panic: %v", r)
		}
	}()
	col := p.col
	if p.enc != nil {
		var decodeErr error
		if col, decodeErr = p.enc.decode(s.w.conf.Blocks, s.w.Range()); decodeErr != nil {
			s.w.conf.Log.Error("save chunk: "+decodeErr.Error(), "X", p.pos[0], "Z", p.pos[1])
			return false, decodeErr
		}
	}
	if p.commit == nil {
		return false, s.w.writeColumn(s.w.conf.Provider, p.pos, col)
	}
	err = s.w.writeColumn(p.commit, p.pos, col)
	if err == nil {
		s.mu.Lock()
		s.committing[p.commit] = append(s.committing[p.commit], pendingColumn{pos: p.pos, col: col, saved: p.saved})
		s.inCommit[p.pos] = p.commit
		s.mu.Unlock()
	}
	return false, err
}

// committed tells the saver that commit was committed, or failed to, with
// err: the columns written into it are then read back from the provider,
// and, if it failed, are failed saves, written again by the next capture.
func (s *chunkSaver) committed(commit Commit, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.committing[commit] {
		if s.inCommit[p.pos] == commit {
			delete(s.inCommit, p.pos)
		}
		if _, again := s.waiting[p.pos]; err != nil && !again {
			s.failed[p.pos] = p
		}
	}
	delete(s.committing, commit)
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

// close writes the columns still waiting or staged and stops the saver.
func (s *chunkSaver) close() {
	s.commitStaged(nil)
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
		if sc := p.stage; sc != nil {
			if s.staged[sc.pos] != sc || sc.enc != nil {
				// Taken back or committed already.
				s.cond.Broadcast()
				s.mu.Unlock()
				continue
			}
			s.encoding = sc
			s.mu.Unlock()
			s.encodeStaged(sc)
			continue
		}
		col, enc, saved, commit := p.col, p.enc, p.saved, p.commit
		if col != nil || enc != nil {
			delete(s.waiting, p.pos)
			s.writing, s.busy = p.pos, true
		}
		s.cond.Broadcast()
		s.mu.Unlock()
		if col == nil && enc == nil {
			continue
		}
		panicked, err := s.write(pendingColumn{pos: p.pos, col: col, enc: enc, saved: saved, commit: commit})
		s.mu.Lock()
		dropped := false
		if panicked {
			s.panicked[p.pos]++
		} else if err == nil {
			delete(s.panicked, p.pos)
		}
		if _, again := s.waiting[p.pos]; err != nil && !again {
			if dropped = s.panicked[p.pos] >= saveAttempts; dropped {
				delete(s.panicked, p.pos)
			} else {
				// Taken by the next save, which writes the column again.
				s.failed[p.pos] = pendingColumn{pos: p.pos, col: col, enc: enc, saved: saved}
			}
		}
		s.busy = false
		s.cond.Broadcast()
		s.mu.Unlock()
		if dropped {
			s.w.conf.Log.Error("save chunk: dropped the save of the chunk, whose writes panicked twice in a row", "X", p.pos[0], "Z", p.pos[1])
		}
	}
}

// encodeStaged encodes sc, the column s.encoding names. A column that
// fails to encode stays staged as it is, to be written as it is.
func (s *chunkSaver) encodeStaged(sc *stagedColumn) {
	enc, err := encodeColumn(sc.col)
	if err != nil {
		s.w.conf.Log.Error("stage chunk: "+err.Error(), "X", sc.pos[0], "Z", sc.pos[1])
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.encoding = nil
	s.cond.Broadcast()
	if err != nil {
		return
	}
	sc.enc, sc.col = enc, nil
	s.stagedBytes += enc.size
	if limit := s.w.conf.StagingLimit; limit > 0 && s.stagedBytes > limit && !s.full {
		s.full = true
		go s.w.stagingFull(s.stagedBytes)
	}
}

// DefaultStagingLimit is the StagingLimit of a Config that sets none: 256
// MiB of encoded columns, some 16,000 terrain columns.
const DefaultStagingLimit = 256 << 20

// stagingFull handles the columns staged exceeding the StagingLimit, with
// the bytes they take: Config.StagingFull is called, or else the world
// saves itself. It runs on a goroutine of its own.
func (w *World) stagingFull(bytes int) {
	if f := w.conf.StagingFull; f != nil {
		f(w, bytes)
		return
	}
	if w.closed.Load() {
		return
	}
	w.conf.Log.Warn("Saving the world early: the chunks unloaded since it was saved exceed the staging limit.", "bytes", bytes, "limit", w.conf.StagingLimit)
	w.Save()
}
