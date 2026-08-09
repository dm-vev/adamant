package block

import (
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestExplosionAffectsEntityAtExactOrigin(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	defer w.Close()

	record := new(exactOriginExplosionRecord)
	<-w.Exec(func(tx *world.Tx) {
		pos := mgl64.Vec3{0.25, 4, 0.25}
		handle := world.EntitySpawnOpts{Position: pos}.New(exactOriginExplosionEntityType{}, exactOriginExplosionEntityConfig{record})
		tx.AddEntity(handle)
		ExplosionConfig{RandSource: rand.NewPCG(1, 2), ItemDropChance: -1}.Explode(tx, exactOriginExplosionSource{pos: pos, size: 0.1})
	})
	if record.calls != 1 || record.impact != 1 {
		t.Fatalf("exact-origin explosion calls/impact = %v/%v, want 1/1", record.calls, record.impact)
	}
}

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

type exactOriginExplosionSource struct {
	pos  mgl64.Vec3
	size float64
}

func (s exactOriginExplosionSource) Position() mgl64.Vec3 { return s.pos }
func (s exactOriginExplosionSource) Size() float64        { return s.size }

type exactOriginExplosionRecord struct {
	calls  int
	impact float64
}

type exactOriginExplosionEntityConfig struct{ record *exactOriginExplosionRecord }

func (c exactOriginExplosionEntityConfig) Apply(data *world.EntityData) { data.Data = c.record }

type exactOriginExplosionEntityType struct{}

func (exactOriginExplosionEntityType) Open(_ *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return &exactOriginExplosionEntity{handle: handle, data: data}
}
func (exactOriginExplosionEntityType) EncodeEntity() string {
	return "adamant:exact_origin_explosion_test"
}
func (exactOriginExplosionEntityType) BBox(world.Entity) cube.BBox {
	return cube.Box(-0.3, 0, -0.3, 0.3, 1.8, 0.3)
}
func (exactOriginExplosionEntityType) DecodeNBT(map[string]any, *world.EntityData) {}
func (exactOriginExplosionEntityType) EncodeNBT(*world.EntityData) map[string]any  { return nil }

type exactOriginExplosionEntity struct {
	handle *world.EntityHandle
	data   *world.EntityData
}

func (*exactOriginExplosionEntity) Close() error             { return nil }
func (e *exactOriginExplosionEntity) H() *world.EntityHandle { return e.handle }
func (e *exactOriginExplosionEntity) Position() mgl64.Vec3   { return e.data.Pos }
func (*exactOriginExplosionEntity) Rotation() cube.Rotation  { return cube.Rotation{} }
func (e *exactOriginExplosionEntity) Explode(_ world.ExplosionSource, impact float64) {
	record := e.data.Data.(*exactOriginExplosionRecord)
	record.calls++
	record.impact = impact
}
