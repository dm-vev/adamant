package block

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

func TestConcurrentExplosionsLockSharedRandSource(t *testing.T) {
	source := &concurrentExplosionSource{}
	w1 := world.Config{Synchronous: true}.New()
	w2 := world.Config{Synchronous: true}.New()
	t.Cleanup(func() {
		_ = w1.Close()
		_ = w2.Close()
	})

	start := make(chan struct{})
	var wg sync.WaitGroup
	for _, w := range []*world.World{w1, w2} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			<-w.Exec(func(tx *world.Tx) {
				ExplosionConfig{RandSource: source, ItemDropChance: -1}.Explode(tx, world.BlockExplosionSource{
					Pos: cube.Pos{}, ExplosionSize: 0.1,
				})
			})
		}()
	}
	close(start)
	wg.Wait()
	if source.overlap.Load() {
		t.Fatal("shared rand.Source was used concurrently")
	}
}

type concurrentExplosionSource struct {
	calls   atomic.Int32
	active  atomic.Int32
	overlap atomic.Bool
}

func (s *concurrentExplosionSource) Uint64() uint64 {
	if s.active.Add(1) != 1 {
		s.overlap.Store(true)
	}
	call := s.calls.Add(1)
	if call <= 2 {
		time.Sleep(20 * time.Millisecond)
	}
	s.active.Add(-1)
	return uint64(call)
}
