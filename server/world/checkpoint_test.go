package world_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestCaptureAllIsOneInstant checks that CaptureAll captures its worlds at
// one instant: no world runs a transaction from the first capture to the
// last. A world whose queue is full is still captured, after the
// transactions queued before; a world that does not reach the barrier in
// time makes CaptureAll give up without capturing any; synchronous worlds
// are captured on the caller.
func TestCaptureAllIsOneInstant(t *testing.T) {
	pos, chunkPos := cube.Pos{1, 10, 1}, world.ChunkPos{0, 0}
	newWorld := func(t *testing.T, synchronous bool) *world.World {
		w := world.Config{Generator: world.NopGenerator{}, RandomTickSpeed: -1, Synchronous: synchronous}.New()
		t.Cleanup(func() { _ = w.Close() })
		<-w.Exec(func(tx *world.Tx) { tx.SetBlock(pos, block.Stone{}, nil) })
		return w
	}
	modified := func(w *world.World) (modified bool) {
		<-w.Exec(func(tx *world.Tx) { modified = tx.ColumnModified(chunkPos) })
		return
	}

	t.Run("one instant", func(t *testing.T) {
		a, b := newWorld(t, false), newWorld(t, false)
		// Each world runs a transaction counting an event every 50µs or so,
		// whose queue fills while it waits at the barrier.
		var events atomic.Int64
		stop := make(chan struct{})
		var wg sync.WaitGroup
		for _, w := range []*world.World{a, b} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					select {
					case <-stop:
						return
					default:
					}
					w.Do(func(*world.Tx) { events.Add(1) })
					time.Sleep(50 * time.Microsecond)
				}
			}()
		}
		defer func() {
			close(stop)
			wg.Wait()
		}()
		// Each world reads the count as its capture starts and again 20ms
		// later, which is long enough for the other to count hundreds of
		// events if it ran.
		extra := func(tx *world.Tx) any {
			first := events.Load()
			time.Sleep(20 * time.Millisecond)
			return [2]int64{first, events.Load()}
		}
		for i := range 3 {
			// Each capture changes the block: the same state again changes
			// nothing.
			var changed world.Block = block.Dirt{}
			if i%2 == 1 {
				changed = block.Stone{}
			}
			<-a.Exec(func(tx *world.Tx) { tx.SetBlock(pos, changed, nil) })
			<-b.Exec(func(tx *world.Tx) { tx.SetBlock(pos, changed, nil) })
			cps, err := world.CaptureAll(context.Background(), []*world.World{a, b, a}, extra)
			if err != nil {
				t.Fatalf("capture %d: %v", i, err)
			}
			if len(cps) != 2 {
				t.Fatalf("capture %d: %d checkpoints, want 2: one for each world", i, len(cps))
			}
			var seen []int64
			for _, cp := range cps {
				counts := cp.Extra.([2]int64)
				seen = append(seen, counts[0], counts[1])
				if cp.Columns() != 1 {
					t.Errorf("capture %d of %v: %d columns, want the one changed", i, cp.World().Dimension(), cp.Columns())
				}
				if err := cp.Write(); err != nil {
					t.Errorf("write capture %d: %v", i, err)
				}
			}
			for _, n := range seen[1:] {
				if n != seen[0] {
					t.Fatalf("capture %d: the worlds counted %v events as they captured, want the same count: one ran during the other's capture", i, seen)
				}
			}
			if modified(a) || modified(b) {
				t.Errorf("capture %d: a column captured is still marked modified", i)
			}
			// The worlds run again once released.
			deadline := time.Now().Add(5 * time.Second)
			for events.Load() < seen[0]+10 {
				if time.Now().After(deadline) {
					t.Fatalf("capture %d: the worlds counted no events after the capture", i)
				}
				time.Sleep(time.Millisecond)
			}
		}
	})

	t.Run("full queue", func(t *testing.T) {
		a, b := newWorld(t, false), newWorld(t, false)
		hold, held := make(chan struct{}), make(chan struct{})
		a.Do(func(*world.Tx) {
			close(held)
			<-hold
		})
		<-held
		// More than the queue holds: the rest overflow.
		const queued = 300
		ran := 0
		for range queued {
			a.Do(func(*world.Tx) { ran++ })
		}
		type result struct {
			cps []*world.Checkpoint
			err error
		}
		done := make(chan result, 1)
		go func() {
			cps, err := world.CaptureAll(context.Background(), []*world.World{a, b}, func(tx *world.Tx) any {
				if tx.World() == a {
					return ran
				}
				return nil
			})
			done <- result{cps, err}
		}()
		time.Sleep(50 * time.Millisecond)
		close(hold)
		res := <-done
		if res.err != nil {
			t.Fatalf("CaptureAll with a full queue: %v", res.err)
		}
		for _, cp := range res.cps {
			if cp.World() == a && cp.Extra.(int) != queued {
				t.Errorf("the world with a full queue captured after %d of the %d transactions queued before, want all", cp.Extra.(int), queued)
			}
			_ = cp.Write()
		}
		if len(res.cps) != 2 {
			t.Errorf("%d checkpoints, want 2", len(res.cps))
		}
	})

	t.Run("timeout", func(t *testing.T) {
		a, b := newWorld(t, false), newWorld(t, false)
		hold, held := make(chan struct{}), make(chan struct{})
		a.Do(func(*world.Tx) {
			close(held)
			<-hold
		})
		<-held
		var extras atomic.Int64
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		start := time.Now()
		cps, err := world.CaptureAll(ctx, []*world.World{a, b}, func(*world.Tx) any {
			extras.Add(1)
			return nil
		})
		if !errors.Is(err, context.DeadlineExceeded) || cps != nil {
			t.Fatalf("CaptureAll with a world held: %v, %d checkpoints; want the deadline exceeded and none", err, len(cps))
		}
		if took := time.Since(start); took > 3*time.Second {
			t.Errorf("CaptureAll gave up after %v, want about 200ms", took)
		}
		// The world that reached the barrier is released and was not
		// captured; neither is the other once it runs again.
		if !modified(b) {
			t.Error("the world that reached the barrier was captured")
		}
		close(hold)
		if !modified(a) {
			t.Error("the world held was captured after the timeout")
		}
		if n := extras.Load(); n != 0 {
			t.Errorf("extra ran %d times, want none", n)
		}
	})

	t.Run("synchronous", func(t *testing.T) {
		a, b := newWorld(t, true), newWorld(t, false)
		cps, err := world.CaptureAll(context.Background(), []*world.World{a, b}, func(tx *world.Tx) any { return tx.World() })
		if err != nil {
			t.Fatal(err)
		}
		if len(cps) != 2 {
			t.Fatalf("%d checkpoints, want 2", len(cps))
		}
		for _, cp := range cps {
			if cp.Extra != cp.World() || cp.Columns() != 1 {
				t.Errorf("checkpoint of %v: extra ran in %v, %d columns; want its world and 1", cp.World().Dimension(), cp.Extra, cp.Columns())
			}
			if err := cp.Write(); err != nil {
				t.Error(err)
			}
		}
		if modified(a) || modified(b) {
			t.Error("a column captured is still marked modified")
		}
	})
}

// BenchmarkCheckpointCapture measures how long CaptureAll holds a world
// with 1,000 columns changed since the last save and 2,000 item entities:
// what a checkpoint stalls the world for. The columns are written by the
// saver of the world, through a provider that stores nothing, outside the
// time measured.
func BenchmarkCheckpointCapture(b *testing.B) {
	const columns, side = 1000, 32
	w := world.Config{Generator: world.NopGenerator{}, RandomTickSpeed: -1, ChunkUnloadInterval: time.Hour}.New()
	defer w.Close()
	positions := make([]cube.Pos, 0, columns)
	for i := range columns {
		positions = append(positions, cube.Pos{(i%side)*16 + 1, 10, (i/side)*16 + 1})
	}
	<-w.Exec(func(tx *world.Tx) {
		for _, pos := range positions {
			for j := range 2 {
				at := pos.Vec3Middle().Add(mgl64.Vec3{float64(j), 1, 0})
				tx.AddEntity(entity.NewItem(world.EntitySpawnOpts{Position: at}, item.NewStack(item.Diamond{}, 1)))
			}
		}
	})
	blocks := []world.Block{block.Stone{}, block.Dirt{}}
	b.ResetTimer()
	for i := range b.N {
		b.StopTimer()
		<-w.Exec(func(tx *world.Tx) {
			for _, pos := range positions {
				tx.SetBlock(pos, blocks[i%2], nil)
			}
		})
		b.StartTimer()
		cps, err := world.CaptureAll(context.Background(), []*world.World{w}, nil)
		b.StopTimer()
		if err != nil {
			b.Fatal(err)
		}
		if cps[0].Columns() != columns {
			b.Fatalf("captured %d columns, want %d", cps[0].Columns(), columns)
		}
		_ = cps[0].Write()
		b.StartTimer()
	}
}
