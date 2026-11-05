package world

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/world/chunk"
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
