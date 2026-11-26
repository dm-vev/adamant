package world

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dm-vev/adamant/server/world/chunk"
	"github.com/go-gl/mathgl/mgl64"
)

// slowProvider is a provider whose columns take delay to read, and that says
// it reads them concurrently. It counts the reads made on a world owner and
// off it, and the most at once.
type slowProvider struct {
	NopProvider
	delay time.Duration

	onOwner, offOwner    atomic.Int64
	inFlight, mostAtOnce atomic.Int64
	mu                   sync.Mutex
	positions            map[ChunkPos]int
}

func (*slowProvider) LoadsConcurrently() bool { return true }

func (p *slowProvider) LoadColumn(pos ChunkPos, _ Dimension) (*chunk.Column, error) {
	if ownerOf() != nil {
		p.onOwner.Add(1)
	} else {
		p.offOwner.Add(1)
	}
	n := p.inFlight.Add(1)
	for {
		most := p.mostAtOnce.Load()
		if n <= most || p.mostAtOnce.CompareAndSwap(most, n) {
			break
		}
	}
	p.mu.Lock()
	p.positions[pos]++
	p.mu.Unlock()
	time.Sleep(p.delay)
	p.inFlight.Add(-1)
	return &chunk.Column{Chunk: chunk.New(DefaultBlockRegistry, Overworld.Range())}, nil
}

// TestColumnsAreReadAheadOfTheOwner checks that a loader that asks for many
// columns of a provider that reads concurrently has them read on other
// goroutines than the owner's, several at once, each once, and that the
// owner does not spend the time of those reads in its transactions.
func TestColumnsAreReadAheadOfTheOwner(t *testing.T) {
	const delay = 10 * time.Millisecond
	p := &slowProvider{NopProvider: NopProvider{Set: defaultSettings()}, delay: delay, positions: map[ChunkPos]int{}}
	w := Config{Provider: p}.New()
	defer w.Close()
	// The loads of a tick are read by as many goroutines as a host of many
	// CPUs would give the world.
	w.readSlots = make(chan struct{}, 16)

	l := NewLoader(4, w, NopViewer{})
	var ownerTime time.Duration
	var transactions int
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		loaded := 0
		<-w.Exec(func(tx *Tx) {
			start := time.Now()
			l.Move(tx, mgl64.Vec3{8, 64, 8})
			l.Load(tx, 4)
			ownerTime += time.Since(start)
			transactions++
		})
		l.mu.RLock()
		loaded = len(l.loaded)
		l.mu.RUnlock()
		if loaded >= len(chunksAround(4)) {
			break
		}
		// The loads of a player come once a tick.
		time.Sleep(30 * time.Millisecond)
	}
	want := len(chunksAround(4))
	if got := len(p.positions); got != want {
		t.Fatalf("%d columns read, want %d", got, want)
	}
	for pos, n := range p.positions {
		if n != 1 {
			t.Errorf("column %v read %d times", pos, n)
		}
	}
	if on, off := p.onOwner.Load(), p.offOwner.Load(); on > off/4 {
		t.Errorf("%d columns read on the owner, %d off it: most should be read ahead", on, off)
	}
	if most := p.mostAtOnce.Load(); most < 2 {
		t.Errorf("columns were read %d at a time, want several", most)
	}
	// Read one at a time on the owner, the columns would take want*delay of it.
	if serial := time.Duration(want) * delay; ownerTime > serial/3 {
		t.Errorf("the owner spent %v in %d loads of %d columns that take %v to read in turn", ownerTime, transactions, want, serial)
	}
}

// TestLoaderDoesNotWaitForColumnsBeingRead checks that a loader of more
// columns than are read ahead at once leaves the columns still being read for
// a later tick instead of having the owner wait for the reads, or read them
// itself, and loads each once read.
func TestLoaderDoesNotWaitForColumnsBeingRead(t *testing.T) {
	const (
		delay  = 100 * time.Millisecond
		radius = 8
	)
	p := &slowProvider{NopProvider: NopProvider{Set: defaultSettings()}, delay: delay, positions: map[ChunkPos]int{}}
	w := Config{Provider: p}.New()
	defer w.Close()
	w.readSlots = make(chan struct{}, 64)
	if len(chunksAround(radius)) <= maxPrefetched {
		t.Fatalf("a loader of radius %d loads no more columns than are read ahead at once", radius)
	}
	l := NewLoader(radius, w, NopViewer{})
	var longest time.Duration
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		<-w.Exec(func(tx *Tx) {
			start := time.Now()
			l.Move(tx, mgl64.Vec3{8, 64, 8})
			l.Load(tx, 4)
			longest = max(longest, time.Since(start))
		})
		l.mu.RLock()
		loaded := len(l.loaded)
		l.mu.RUnlock()
		if loaded >= len(chunksAround(radius)) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got, want := len(l.loaded), len(chunksAround(radius)); got != want {
		t.Fatalf("%d columns loaded, want %d", got, want)
	}
	if longest >= delay/2 {
		t.Errorf("a load took %v of the owner with columns taking %v to read", longest, delay)
	}
	if on := p.onOwner.Load(); on != 0 {
		t.Errorf("%d columns read on the owner", on)
	}
	for pos, n := range p.positions {
		if n != 1 {
			t.Errorf("column %v read %d times", pos, n)
		}
	}
}

// chunksAround returns the positions of the chunks a loader of the radius
// loads around the origin.
func chunksAround(radius int) []ChunkPos {
	var out []ChunkPos
	for x := -radius; x <= radius; x++ {
		for z := -radius; z <= radius; z++ {
			if chunkDistance(ChunkPos{int32(x), int32(z)}, ChunkPos{}) <= int32(radius) {
				out = append(out, ChunkPos{int32(x), int32(z)})
			}
		}
	}
	return out
}

// TestReadAheadSkipsColumnsTheSaverHolds checks that a column the saver holds
// something of is not read ahead from the provider, which does not hold it
// yet: the owner reads it through the saver as it always did.
func TestReadAheadSkipsColumnsTheSaverHolds(t *testing.T) {
	t.Parallel()
	pos := func(i int32) ChunkPos { return ChunkPos{i, i} }
	s := &chunkSaver{
		waiting:  map[ChunkPos]*pendingColumn{pos(1): {}},
		staged:   map[ChunkPos]*stagedColumn{pos(2): {}},
		failed:   map[ChunkPos]pendingColumn{pos(3): {}},
		inCommit: map[ChunkPos]Commit{pos(4): nil},
		writing:  pos(5),
		busy:     true,
		encoding: &stagedColumn{pos: pos(6)},
	}
	for i := int32(1); i <= 6; i++ {
		if s.idle(pos(i)) {
			t.Errorf("the saver is idle at %v, which it holds something of", pos(i))
		}
	}
	if !s.idle(pos(7)) {
		t.Error("the saver is not idle at a column it holds nothing of")
	}
}

// TestReadAheadIsDroppedWhenStale checks that a read ahead that nobody took
// within its life is not used, so that a column is never loaded from what the
// provider held long ago.
func TestReadAheadIsDroppedWhenStale(t *testing.T) {
	t.Parallel()
	p := &slowProvider{NopProvider: NopProvider{Set: defaultSettings()}, positions: map[ChunkPos]int{}}
	w := Config{Provider: p}.New()
	defer w.Close()
	<-w.Exec(func(*Tx) {
		w.prefetchColumn(ChunkPos{9, 9})
		r := w.prefetched[ChunkPos{9, 9}]
		if r == nil {
			t.Fatal("no column read ahead")
		}
		<-r.done
		r.at = time.Now().Add(-2 * prefetchLife)
		if _, taken, _ := w.takePrefetched(ChunkPos{9, 9}); taken {
			t.Error("a read ahead older than its life was taken")
		}
		if len(w.prefetched) != 0 {
			t.Error("a stale read ahead was kept")
		}
	})
}

type slowCommit struct {
	*slowProvider
}

func (*slowCommit) SaveSettings(*Settings)    {}
func (*slowCommit) SaveTick(Dimension, int64) {}
func (*slowCommit) Commit(uint64) error       { return nil }
func (*slowCommit) Discard()                  {}

// TestPrefetchSaturatedNeverReadsOnOwner checks that an async concurrentLoader
// never falls back to synchronous LoadColumn on the world owner when many
// loaders or a saturated prefetch map exhaust prefetch capacity.
func TestPrefetchSaturatedNeverReadsOnOwner(t *testing.T) {
	const (
		delay   = 15 * time.Millisecond
		loaders = 8
		radius  = 3
	)
	p := &slowProvider{NopProvider: NopProvider{Set: defaultSettings()}, delay: delay, positions: map[ChunkPos]int{}}
	w := Config{Provider: p}.New()
	defer w.Close()
	w.readSlots = make(chan struct{}, 16)

	ls := make([]*Loader, loaders)
	for i := range ls {
		ls[i] = NewLoader(radius, w, NopViewer{})
	}
	// Pre-fill w.prefetched beyond prefetchLimit + demand slack with dummy
	// in-flight entries so the very first Load ticks hit hard saturation.
	dummyDone := make(chan struct{})
	defer close(dummyDone)
	var dummyKeys []ChunkPos
	<-w.Exec(func(tx *Tx) {
		for i, l := range ls {
			l.Move(tx, mgl64.Vec3{float64(i*200 + 8), 64, 8})
		}
		capFill := w.prefetchLimit() + max(16, w.viewerCount()) + 8
		for i := range capFill {
			pos := ChunkPos{int32(-10000 - i), -10000}
			w.prefetched[pos] = &columnRead{done: dummyDone, at: time.Now()}
			dummyKeys = append(dummyKeys, pos)
		}
		// While completely saturated, Load must defer and never read on owner.
		for _, l := range ls {
			l.Load(tx, 4)
		}
	})
	if on := p.onOwner.Load(); on != 0 {
		t.Fatalf("%d columns read on owner while prefetch was hard-saturated, want 0", on)
	}
	// Release dummy saturation entries and verify all loaders finish off-owner.
	<-w.Exec(func(*Tx) {
		for _, pos := range dummyKeys {
			delete(w.prefetched, pos)
		}
	})

	wantPerLoader := len(chunksAround(radius))
	var longest time.Duration
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		allDone := true
		<-w.Exec(func(tx *Tx) {
			start := time.Now()
			for _, l := range ls {
				l.Load(tx, 4)
			}
			longest = max(longest, time.Since(start))
		})
		for _, l := range ls {
			l.mu.RLock()
			if len(l.loaded) < wantPerLoader {
				allDone = false
			}
			l.mu.RUnlock()
		}
		if allDone {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	for i, l := range ls {
		l.mu.RLock()
		got := len(l.loaded)
		l.mu.RUnlock()
		if got != wantPerLoader {
			t.Fatalf("loader %d loaded %d columns, want %d", i, got, wantPerLoader)
		}
	}
	if on := p.onOwner.Load(); on != 0 {
		t.Errorf("%d columns read on the owner under %d loaders, want 0", on, loaders)
	}
	if longest >= delay {
		t.Errorf("longest owner load took %v with delay %v", longest, delay)
	}
}

// TestPrefetchSaverBusyDefersAndCommitPrefetches checks that a loader defers
// when the saver is writing/encoding a column and prefetches off-owner when the
// column is held in an uncommitted Commit.
func TestPrefetchSaverBusyDefersAndCommitPrefetches(t *testing.T) {
	const delay = 15 * time.Millisecond
	p := &slowProvider{NopProvider: NopProvider{Set: defaultSettings()}, delay: delay, positions: map[ChunkPos]int{}}
	w := Config{Provider: p}.New()
	defer w.Close()

	pos := ChunkPos{0, 0}
	l := NewLoader(0, w, NopViewer{})
	// Hold pos in saver.waiting: Loader.Load must defer and not read on owner.
	w.saver.mu.Lock()
	w.saver.waiting[pos] = &pendingColumn{pos: pos}
	w.saver.mu.Unlock()
	<-w.Exec(func(tx *Tx) {
		l.Move(tx, mgl64.Vec3{8, 64, 8})
		l.Load(tx, 1)
	})
	if on := p.onOwner.Load(); on != 0 {
		t.Fatalf("%d owner reads while saver held waiting column, want 0", on)
	}
	l.mu.RLock()
	loaded := len(l.loaded)
	l.mu.RUnlock()
	if loaded != 0 {
		t.Fatalf("loaded %d columns while saver held waiting column, want 0", loaded)
	}

	// Move pos to saver.inCommit with a slowCommit provider: loader must
	// prefetch it off-owner from the commit and mark the reloaded column modified.
	cp := &slowProvider{NopProvider: NopProvider{Set: defaultSettings()}, delay: delay, positions: map[ChunkPos]int{}}
	commit := &slowCommit{slowProvider: cp}
	w.saver.mu.Lock()
	delete(w.saver.waiting, pos)
	w.saver.inCommit[pos] = commit
	w.saver.mu.Unlock()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		<-w.Exec(func(tx *Tx) {
			l.Load(tx, 1)
		})
		l.mu.RLock()
		loaded = len(l.loaded)
		l.mu.RUnlock()
		if loaded == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if loaded != 1 {
		t.Fatal("column in saver.inCommit did not load")
	}
	if on := cp.onOwner.Load(); on != 0 {
		t.Errorf("%d commit reads ran on the owner, want 0", on)
	}
	if off := cp.offOwner.Load(); off != 1 {
		t.Errorf("%d commit reads ran off the owner, want 1", off)
	}
	if on := p.onOwner.Load(); on != 0 {
		t.Errorf("%d provider reads ran on the owner, want 0", on)
	}
	var modified bool
	<-w.Exec(func(*Tx) {
		if c, ok := w.chunks[pos]; ok {
			modified = c.modified.Load()
		}
	})
	if !modified {
		t.Error("column reloaded from uncommitted commit was not marked modified")
	}
}

// TestPrefetchCloseCancelsQueuedReads checks that World.Close does not block
// waiting for prefetch goroutines queued on a full readSlots semaphore.
func TestPrefetchCloseCancelsQueuedReads(t *testing.T) {
	p := &slowProvider{NopProvider: NopProvider{Set: defaultSettings()}, delay: 5 * time.Second, positions: map[ChunkPos]int{}}
	w := Config{Provider: p}.New()
	w.readSlots = make(chan struct{}, 1)
	w.readSlots <- struct{}{} // Occupy the single slot so prefetches block in select.

	<-w.Exec(func(*Tx) {
		for i := range 8 {
			w.prefetchColumn(ChunkPos{int32(i), 0})
		}
	})
	closed := make(chan error, 1)
	go func() {
		closed <- w.Close()
	}()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatalf("World.Close: %v", err)
		}
	case <-time.After(3 * time.Second):
		<-w.readSlots
		t.Fatal("World.Close blocked on queued prefetch goroutines")
	}
	<-w.readSlots
}
