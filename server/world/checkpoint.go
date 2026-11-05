package world

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Checkpoint is a world captured for a save at one instant, in one of its
// transactions: its columns that changed since they were last saved, handed
// to the background saver (or written, by a synchronous world), and its
// settings and the data saved beside its columns, encoded. Write writes it,
// off the world's goroutine.
type Checkpoint struct {
	w *World
	// seq is the count of the capture (World.captures).
	seq      uint64
	columns  int
	settings *Settings
	data     []worldData
	tracking *positionTrackingCapture
	// staged holds the writes staged by the worlds of the provider since
	// the last capture, taken by this one, written before data. retry
	// holds those that failed to write into commit, staged again by
	// Committed.
	staged *stagedWrites
	retry  *stagedWrites
	// commit, if not nil, is the commit of the provider the checkpoint is
	// written into (CaptureAllInto).
	commit Commit

	// Extra is what the function passed to CaptureAll returned in the
	// transaction the world was captured in, such as the players in it.
	Extra any
}

// worldData is a part of a world saved beside its columns, encoded when the
// world was captured. write stores it through p, the provider of the world
// or a commit of it, and returns the error, as logged.
type worldData struct {
	write func(p Provider) error
}

// World returns the world captured.
func (cp *Checkpoint) World() *World { return cp.w }

// Columns returns the number of columns captured, those unloaded since the
// last capture included.
func (cp *Checkpoint) Columns() int { return cp.columns }

// Settings returns the settings of the world as they were captured, nil if
// the world is read-only and nothing was captured.
func (cp *Checkpoint) Settings() *Settings { return cp.settings }

// Write writes the checkpoint through the provider of its world, or into
// the commit it was captured for (CaptureAllInto), and waits for the
// columns captured to be written: it writes the data and maps the worlds of
// the provider staged since the last capture (Staging), gives the provider
// the settings captured and writes the data saved beside the columns, then
// waits for the columns and writes the position tracking data. It logs
// every error and returns them joined. The data of a capture older than one
// already written is not written: the newer holds it. Write waits for the
// background saver of the world: call it off any world owner. A checkpoint
// written into a commit is told the commit's outcome with Committed.
func (cp *Checkpoint) Write() error {
	return cp.write(true)
}

// Committed tells the checkpoint, written into a commit by Write, that the
// commit was committed, or failed to with err: if it failed, the columns
// and the data staged it held are saved again by the next capture, as
// failed writes are.
func (cp *Checkpoint) Committed(err error) {
	if cp.commit == nil {
		return
	}
	w := cp.w
	if w.saver != nil {
		w.saver.committed(cp.commit, err)
	}
	if cp.staged != nil {
		retry := cp.retry
		if err != nil {
			retry = cp.staged
		}
		w.providerUse.staging.finish(cp.staged, retry)
		cp.staged, cp.retry = nil, nil
	}
}

// write writes cp, waiting for its columns to be written first if flush is
// set.
func (cp *Checkpoint) write(flush bool) error {
	w := cp.w
	if cp.settings == nil {
		return nil
	}
	var errs []error
	fail := func(err error) {
		if err != nil {
			w.saveLog.Error(err.Error())
			errs = append(errs, err)
		}
	}
	to := w.conf.Provider
	if cp.commit != nil {
		to = cp.commit
	}
	if cp.staged != nil {
		retry, err := w.providerUse.staging.writeTo(to, cp.staged)
		fail(err)
		if cp.commit == nil {
			w.providerUse.staging.finish(cp.staged, retry)
			cp.staged = nil
		} else {
			cp.retry = retry
		}
	}
	w.dataMu.Lock()
	if cp.seq > w.dataWritten {
		w.dataWritten = cp.seq
		to.SaveSettings(cp.settings)
		if c, ok := to.(Commit); ok {
			c.SaveTick(w.conf.Dim, cp.settings.CurrentTick)
		}
		for _, d := range cp.data {
			fail(d.write(to))
		}
	}
	w.dataMu.Unlock()
	if flush && w.saver != nil {
		w.saver.flush()
	}
	if cp.tracking != nil {
		if err := w.providerUse.writePositionTracker(to, cp.tracking); err != nil {
			fail(fmt.Errorf("save position tracking data: %w", err))
		}
	}
	return errors.Join(errs...)
}

// capture captures the world for a save in tx. It saves every column that
// changed since its last save, or closes every column loaded if closing, as
// the world does when it closes, after the columns unloaded and staged since
// the last capture. It then takes a copy of the settings and encodes the
// data of the world saved beside its columns. The columns go to the
// background saver at once, in the order of the world's other saves, so
// that a column unloaded after the capture is never written before it; the
// rest is left to the Checkpoint returned. Closing captures no position
// tracking data, which the provider saves as it closes. A read-only world
// captures nothing. The columns of a capture for a commit, if not nil, are
// written into it.
func (w *World) capture(tx *Tx, closing bool, commit Commit) *Checkpoint {
	cp := &Checkpoint{w: w, commit: commit}
	if w.conf.ReadOnly {
		return cp
	}
	w.captures++
	cp.seq = w.captures
	w.saveLog.Debug("Saving chunks in memory to disk...")
	w.restoreFailedSaves()
	if closing {
		for pos, c := range w.chunks {
			w.closeChunk(tx, pos, c)
		}
	}
	// The columns unloaded since the last capture are written with this
	// one, before the columns it captures.
	if w.saver != nil {
		cp.columns = w.saver.commitStaged(commit)
	}
	if !closing {
		cp.columns += w.saveChunks(tx, commit)
	}
	w.saveLog.Debug("Updating level.dat values...")
	cp.settings = w.set.Snapshot()
	cp.staged = w.providerUse.staging.take()
	cp.data = w.captureData()
	if !closing {
		tracking, err := w.providerUse.capturePositionTracker(w.conf.Provider)
		if err != nil {
			w.saveLog.Error("save position tracking data: " + err.Error())
		}
		cp.tracking = tracking
	}
	return cp
}

// captureData encodes the parts of the world saved beside its columns, in
// the order they are written: village points of interest, raids, the dragon
// fight, loot random sequences, the border, the state of the generator and
// the scoreboard. A part the provider has no store for is left out, but for
// the state of the generator and the scoreboard, whose writes fail.
func (w *World) captureData() []worldData {
	var data []worldData
	_, stores := w.conf.Provider.(worldDataProvider)
	add := func(what, namespace string, encode func() ([]byte, error)) {
		if !stores {
			return
		}
		b, err := encode()
		if err != nil {
			w.saveLog.Error("encode " + what + ": " + err.Error())
			return
		}
		data = append(data, worldData{write: func(p Provider) error {
			if err := saveWorldData(p, namespace, b); err != nil {
				return fmt.Errorf("save %s: %w", what, err)
			}
			return nil
		}})
	}
	add("village POI data", w.villageDataNamespace(), w.encodeVillagePOIs)
	add("raid data", w.raidDataNamespace(), w.encodeRaids)
	if w.dragonFight != nil {
		if _, ok := w.conf.Provider.(DragonFightProvider); ok {
			fight := w.captureDragonFight()
			data = append(data, worldData{write: func(p Provider) error {
				provider, ok := p.(DragonFightProvider)
				if !ok {
					return fmt.Errorf("save End dragon fight: provider %T stores none", p)
				}
				if err := provider.SaveDragonFightData(fight); err != nil {
					return fmt.Errorf("save End dragon fight: %w", err)
				}
				return nil
			}})
		}
	}
	add("loot random sequences", w.lootRandomSequenceNamespace(), w.encodeLootRandomSequences)
	add("world border", w.borderNamespace(), w.encodeBorder)

	generation, err := encodeGenerationState(w.conf.Provider, w.conf.Generator, w.conf.Dim)
	switch {
	case err != nil:
		data = append(data, worldData{write: func(Provider) error { return fmt.Errorf("save generation state: %w", err) }})
	case generation != nil:
		data = append(data, worldData{write: func(p Provider) error {
			if err := generation(p); err != nil {
				return fmt.Errorf("save generation state: %w", err)
			}
			return nil
		}})
	}

	if scoreboard, err := w.encodeScoreboard(); err != nil {
		w.saveLog.Error("encode scoreboard: " + err.Error())
	} else {
		data = append(data, worldData{write: func(p Provider) error {
			if err := saveWorldData(p, scoreboardNamespace, scoreboard); err != nil {
				return fmt.Errorf("save scoreboard: %w", err)
			}
			return nil
		}})
	}
	return data
}

// CaptureTimeout is how long CaptureAll waits for the worlds to reach its
// barrier if the context it is passed has no deadline.
const CaptureTimeout = 5 * time.Second

// CaptureAll captures the worlds passed at one instant, for a checkpoint
// that saves them together. Each world runs one transaction that waits at a
// barrier until every world is in its own, then captures itself, as
// World.Save does, and runs extra, if not nil, whose result is the Extra of
// its Checkpoint. It then waits until every world captured, so that no world
// changes between the first capture and the last. Nothing is written: the
// columns go to the saver of each world, and the rest is written by Write.
//
// CaptureAll gives up when ctx ends, or after CaptureTimeout if ctx has no
// deadline, before any world captured: it releases the worlds that reached
// the barrier and cancels the transactions of the others, so that a timeout
// captures nothing. A world closed is left out. If a capture panics, the
// panic is logged, the world is left out and the error returned names it,
// beside the checkpoints of the others.
//
// The barrier deadlocks only if an owner waits for another, which R5 and
// ErrOwnerWait forbid: a world reaches it after its current tick and the
// transactions queued before, and waits for nothing but the barrier.
// Synchronous worlds cannot wait for each other: they are captured one after
// another on the calling goroutine, while the others wait at the barrier.
// Called from a world owner or a region worker with a world that is not
// synchronous, CaptureAll returns ErrOwnerWait without capturing.
func CaptureAll(ctx context.Context, worlds []*World, extra func(tx *Tx) any) ([]*Checkpoint, error) {
	return CaptureAllInto(ctx, worlds, nil, extra)
}

// CaptureAllInto captures the worlds passed at one instant as CaptureAll
// does, for a checkpoint written into commit, a Commit of their provider,
// all together: the Checkpoints returned write their columns, data and
// settings into it. The checkpoint is committed once they were written, and
// each is then told the outcome with Committed. A nil commit captures as
// CaptureAll does.
func CaptureAllInto(ctx context.Context, worlds []*World, commit Commit, extra func(tx *Tx) any) ([]*Checkpoint, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, CaptureTimeout)
		defer cancel()
	}
	var distinct []*World
	seen := make(map[*World]bool, len(worlds))
	waits := false
	for _, w := range worlds {
		if w == nil || seen[w] {
			continue
		}
		seen[w] = true
		distinct = append(distinct, w)
		waits = waits || !w.conf.Synchronous
	}
	if waits {
		if err := checkOwnerWait("CaptureAll"); err != nil {
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	n := len(distinct)
	cps := make([]*Checkpoint, n)
	ran := make([]bool, n)
	capture := func(i int, tx *Tx) {
		w := distinct[i]
		cp := w.capture(tx, false, commit)
		if extra != nil {
			cp.Extra = extra(tx)
		}
		cps[i] = cp
	}
	// Exactly one of start and abort is closed, by this goroutine.
	start, abort, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	arrived, left, captured := make(chan int, n), make(chan int, n), make(chan int, n)
	var tasks []*Task
	waiting := 0
	for i, w := range distinct {
		if w.conf.Synchronous {
			continue
		}
		task := w.Do(func(tx *Tx) {
			arrived <- i
			select {
			case <-start:
			case <-abort:
				return
			}
			defer func() {
				captured <- i
				<-release
			}()
			capture(i, tx)
		})
		tasks = append(tasks, task)
		waiting++
		go func() {
			<-task.Done()
			if errors.Is(task.Err(), ErrWorldClosed) {
				left <- i
			}
		}()
	}
	reached := 0
	for ; waiting > 0; waiting-- {
		select {
		case i := <-arrived:
			ran[i] = true
			reached++
		case <-left:
		case <-ctx.Done():
			close(abort)
			for _, task := range tasks {
				task.Cancel()
			}
			return nil, fmt.Errorf("world: capture: %d of %d worlds reached the barrier: %w", reached, len(tasks), ctx.Err())
		}
	}
	for i, w := range distinct {
		if !w.conf.Synchronous {
			continue
		}
		task := w.Do(func(tx *Tx) { capture(i, tx) })
		ran[i] = !errors.Is(task.Err(), ErrWorldClosed)
	}
	close(start)
	for ; reached > 0; reached-- {
		<-captured
	}
	close(release)

	out := make([]*Checkpoint, 0, n)
	var errs []error
	for i, cp := range cps {
		switch {
		case cp != nil:
			out = append(out, cp)
		case ran[i]:
			errs = append(errs, fmt.Errorf("world: capture of %v panicked", distinct[i].Dimension()))
		}
	}
	return out, errors.Join(errs...)
}
