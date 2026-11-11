package world

import (
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/go-gl/mathgl/mgl64"
)

// TestTransactionsLeaveNoLightToSpread generates chunks on workers and
// checks that every transaction of the world starts with the light of the
// chunks that became complete before it already spread, so that a tick never
// has to spread the light of every chunk generated since the last one.
func TestTransactionsLeaveNoLightToSpread(t *testing.T) {
	// The workers generate a chunk for each of the permits the loop below
	// hands out, at most four between two of its transactions, so chunks
	// keep becoming complete between transactions however fast the machine
	// runs either.
	permits := make(chan struct{}, 4)
	for range cap(permits) {
		permits <- struct{}{}
	}
	w := Config{Generator: permittedTestGenerator{permits}, GeneratorWorkers: 2, RandomTickSpeed: -1, Provider: NopProvider{}}.New()
	defer w.Close()
	defer close(permits)
	loader := NewLoader(6, w, NopViewer{})
	<-w.Exec(func(tx *Tx) { loader.Move(tx, mgl64.Vec3{8, 80, 8}) })
	deadline := time.Now().Add(time.Minute)
	checked, spread := 0, false
	for time.Now().Before(deadline) {
		for len(permits) < cap(permits) {
			permits <- struct{}{}
		}
		ready := 0
		<-w.Exec(func(tx *Tx) {
			if n := len(w.pendingSpreads); n > 0 {
				t.Errorf("transaction started with the light of %d areas still to spread", n)
			}
			checked++
			loader.Load(tx, 64)
			for _, c := range w.chunks {
				if c.Ready() {
					ready++
				}
			}
			spread = spread || ready > 9
		})
		if ready >= 100 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if !spread || checked < 20 {
		t.Fatalf("only %d transactions checked while chunks generated", checked)
	}
}

// stoneTestGenerator generates stone up to y 60 with glowstone here and
// there, whose light spreads into the chunks around. It takes a while for
// each chunk, so that chunks keep becoming complete between transactions.
type stoneTestGenerator struct{}

func (stoneTestGenerator) GenerateChunk(pos ChunkPos, c *chunk.Chunk, _ GenerationContext) GenerationResult {
	time.Sleep(2 * time.Millisecond)
	stone, _ := chunk.StateToRuntimeID("minecraft:stone", nil)
	glow, _ := chunk.StateToRuntimeID("minecraft:glowstone", nil)
	for x := range uint8(16) {
		for z := range uint8(16) {
			for y := int16(c.Range().Min()); y < 60; y++ {
				c.SetBlock(x, y, z, 0, stone)
			}
		}
	}
	c.SetBlock(0, 60, 0, 0, glow)
	c.SetBlock(15, 30, 15, 0, glow)
	return GenerationResult{}
}

func (stoneTestGenerator) DefaultSpawn(Dimension) cube.Pos { return cube.Pos{8, 61, 8} }

// permittedTestGenerator generates chunks as stoneTestGenerator does, each
// once it takes a permit, or at once when the permits are closed.
type permittedTestGenerator struct{ permits chan struct{} }

func (g permittedTestGenerator) GenerateChunk(pos ChunkPos, c *chunk.Chunk, ctx GenerationContext) GenerationResult {
	<-g.permits
	return stoneTestGenerator{}.GenerateChunk(pos, c, ctx)
}

func (permittedTestGenerator) DefaultSpawn(d Dimension) cube.Pos { return stoneTestGenerator{}.DefaultSpawn(d) }

// TestLightSpreadsWaitFarFromTickingColumns generates chunks further from a
// loader than its simulation distance and checks the light around those far
// from every column ticking waits to spread, is spread before light near it
// is read, and is spread once columns near it come to tick. Transactions run
// on the world's goroutine, so they report failures with Errorf.
func TestLightSpreadsWaitFarFromTickingColumns(t *testing.T) {
	w := Config{Generator: stoneTestGenerator{}, GeneratorWorkers: 2, RandomTickSpeed: -1, Provider: NopProvider{}}.New()
	defer w.Close()
	w.SetTickRange(1)
	loader := NewLoader(10, w, NopViewer{})
	<-w.Exec(func(tx *Tx) { loader.Move(tx, mgl64.Vec3{8, 80, 8}) })
	lit := func(pos ChunkPos, r int32) bool {
		for x := pos[0] - r; x <= pos[0]+r; x++ {
			for z := pos[1] - r; z <= pos[1]+r; z++ {
				if c, ok := w.chunks[ChunkPos{x, z}]; !ok || !c.Ready() || !c.lightReady.Load() {
					return false
				}
			}
		}
		return true
	}
	// checkWaiting checks no light waits to spread near a column ticking.
	checkWaiting := func() {
		for pos := range w.deferredSpreads {
			if !w.spreadFar(pos) {
				t.Errorf("light around %v waits to spread with a column ticking near it", pos)
				return
			}
		}
	}
	// checkLight checks the light of a chunk whose light around waited to
	// spread is the light computed from scratch once read.
	checkLight := func(tx *Tx, pos ChunkPos) {
		centre := cube.Pos{int(pos[0])<<4 + 8, 60, int(pos[1])<<4 + 8}
		fresh := tx.FreshLight(centre)
		for x := centre[0] - 8; x < centre[0]+8; x++ {
			for z := centre[2] - 8; z < centre[2]+8; z++ {
				for y := 20; y < 70; y++ {
					p := cube.Pos{x, y, z}
					sky, bl := fresh(p)
					if got := tx.SkyLight(p); got != sky {
						t.Errorf("skylight at %v = %d, want %d as computed from scratch", p, got, sky)
						return
					}
					if got := tx.BlockLight(p); got != bl {
						t.Errorf("block light at %v = %d, want %d as computed from scratch", p, got, bl)
						return
					}
				}
			}
		}
	}
	// waiting returns a chunk near the loader with all chunks within three
	// of it lit, whose light around waits to spread.
	waiting := func() (ChunkPos, bool) {
		for pos := range w.deferredSpreads {
			if max(abs32(pos[0]-loader.pos[0]), abs32(pos[1]-loader.pos[1])) <= 6 && lit(pos, 3) {
				return pos, true
			}
		}
		return ChunkPos{}, false
	}
	var far ChunkPos
	found := false
	for deadline := time.Now().Add(time.Minute); !found && !t.Failed() && time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		<-w.Exec(func(tx *Tx) {
			loader.Load(tx, 64)
			checkWaiting()
			far, found = waiting()
		})
	}
	if t.Failed() {
		return
	}
	if !found {
		t.Fatal("no light waited to spread far from the columns ticking")
	}
	<-w.Exec(func(tx *Tx) {
		checkLight(tx, far)
		if _, ok := w.deferredSpreads[far]; ok {
			t.Errorf("light around %v still waits to spread after light near it was read", far)
		}
	})
	if t.Failed() {
		return
	}

	// The columns around another such chunk come to tick, spreading the
	// light around them.
	var next ChunkPos
	<-w.Exec(func(tx *Tx) {
		if next, found = waiting(); found {
			loader.Move(tx, mgl64.Vec3{float64(next[0])*16 + 8, 80, float64(next[1])*16 + 8})
		}
	})
	if !found {
		t.Skip("no other light waited to spread near enough to move to")
	}
	time.Sleep(200 * time.Millisecond)
	<-w.Exec(func(tx *Tx) {
		if _, ok := w.deferredSpreads[next]; ok {
			t.Errorf("light around %v still waits to spread with the loader in its chunk", next)
			return
		}
		checkWaiting()
		checkLight(tx, next)
	})
}

// TestReadingLightFarOutSpreadsItNearby generates chunks far from the
// columns ticking, whose light around waits to spread, reads light in one of
// them, as a chunk spawning its first mobs does, and checks the light read
// is the light computed from scratch while the light around chunks further
// out still waits: the spreads the read undeferred do not run on across the
// whole view.
func TestReadingLightFarOutSpreadsItNearby(t *testing.T) {
	w := Config{Generator: stoneTestGenerator{}, GeneratorWorkers: 2, RandomTickSpeed: -1, Provider: NopProvider{}}.New()
	defer w.Close()
	w.SetTickRange(1)
	loader := NewLoader(14, w, NopViewer{})
	<-w.Exec(func(tx *Tx) { loader.Move(tx, mgl64.Vec3{8, 80, 8}) })
	// far is a chunk nine chunks out whose light around waits to spread,
	// with the light around chunks five and more further out waiting too.
	far, found := ChunkPos{}, false
	for deadline := time.Now().Add(time.Minute); !found && time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		<-w.Exec(func(tx *Tx) {
			loader.Load(tx, 128)
			for pos := range w.deferredSpreads {
				if max(abs32(pos[0]), abs32(pos[1])) != 9 {
					continue
				}
				beyond := 0
				for other := range w.deferredSpreads {
					if max(abs32(other[0]-pos[0]), abs32(other[1]-pos[1])) > lightReadRadius+1 {
						beyond++
					}
				}
				if _, ok := w.deferredSpreads[ChunkPos{pos[0] + 1, pos[1]}]; ok && beyond >= 50 {
					far, found = pos, true
					return
				}
			}
		})
	}
	if !found {
		t.Fatal("no light waited to spread far from the columns ticking")
	}
	<-w.Exec(func(tx *Tx) {
		var beyond []ChunkPos
		for pos := range w.deferredSpreads {
			if max(abs32(pos[0]-far[0]), abs32(pos[1]-far[1])) > lightReadRadius+1 {
				beyond = append(beyond, pos)
			}
		}
		centre := cube.Pos{int(far[0])<<4 + 8, 60, int(far[1])<<4 + 8}
		fresh := tx.FreshLight(centre)
		for x := centre[0] - 8; x < centre[0]+8; x++ {
			for z := centre[2] - 8; z < centre[2]+8; z++ {
				for y := 20; y < 70; y++ {
					p := cube.Pos{x, y, z}
					sky, bl := fresh(p)
					if got := tx.SkyLight(p); got != sky {
						t.Errorf("skylight at %v = %d, want %d as computed from scratch", p, got, sky)
						return
					}
					if got := tx.BlockLight(p); got != bl {
						t.Errorf("block light at %v = %d, want %d as computed from scratch", p, got, bl)
						return
					}
				}
			}
		}
		spread := 0
		for _, pos := range beyond {
			if _, ok := w.deferredSpreads[pos]; !ok {
				spread++
			}
		}
		if spread != 0 {
			t.Errorf("reading light at %v spread the light around %d of the %d chunks waiting more than %d chunks from it", far, spread, len(beyond), lightReadRadius+1)
		}
	})
}
