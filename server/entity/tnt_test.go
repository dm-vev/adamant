package entity_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	_ "github.com/df-mc/dragonfly/server/world/biome"
	"github.com/go-gl/mathgl/mgl64"
)

func TestTNTFuseDecrementsBeforeExpiry(t *testing.T) {
	for _, ticks := range []int{0, 1, 80} {
		t.Run(fmt.Sprint(ticks), func(t *testing.T) {
			w := world.Config{Synchronous: true, Entities: entity.DefaultRegistry}.New()
			t.Cleanup(func() { _ = w.Close() })
			w.SetSpawnMobs(false)
			glass := cube.Pos{0, 63, 0}
			var handle *world.EntityHandle
			<-w.Exec(func(tx *world.Tx) {
				tx.SetBlock(glass, block.Glass{}, nil)
				handle = entity.NewTNT(world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 64, 0.5}, Velocity: mgl64.Vec3{1e-9, 0, 0}}, time.Duration(ticks)*time.Second/20, nil)
				tx.AddEntity(handle)
			})
			for elapsed := 1; elapsed < ticks; elapsed++ {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					e, ok := tx.EntityByUUID(handle.UUID())
					if !ok {
						t.Fatalf("TNT disappeared after %d/%d ticks", elapsed, ticks)
					}
					fuse := e.(*entity.TNT).Behaviour().(interface{ Fuse() time.Duration }).Fuse()
					if want := time.Duration(ticks-elapsed) * time.Second / 20; fuse != want {
						t.Fatalf("fuse after tick %d = %v, want %v", elapsed, fuse, want)
					}
				})
			}
			w.AdvanceTick()
			<-w.Exec(func(tx *world.Tx) {
				if _, ok := tx.EntityByUUID(handle.UUID()); ok {
					t.Fatalf("TNT survived expiry tick %d", max(ticks, 1))
				}
				if _, ok := tx.Block(glass).(block.Glass); ok {
					t.Fatal("TNT was removed without exploding")
				}
			})
		})
	}
}
