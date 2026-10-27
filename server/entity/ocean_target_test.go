package entity

import (
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestOceanNearestTargetWithinRange checks zombie nautili find the nearest
// player or axolotl within their range, and nothing further.
func TestOceanNearestTargetWithinRange(t *testing.T) {
	w := world.Config{Synchronous: true, Entities: DefaultRegistry}.New()
	t.Cleanup(func() { _ = w.Close() })
	<-w.Exec(func(tx *world.Tx) {
		from := mgl64.Vec3{8, 40, 8}
		near := tx.AddEntity(NewAxolotl(world.EntitySpawnOpts{Position: from.Add(mgl64.Vec3{5, 0, 0})}))
		tx.AddEntity(NewAxolotl(world.EntitySpawnOpts{Position: from.Add(mgl64.Vec3{0, 0, 9})}))
		tx.AddEntity(NewAxolotl(world.EntitySpawnOpts{Position: from.Add(mgl64.Vec3{-20, 0, 0})}))
		if got := oceanNearestTarget(tx, from, 16); got == nil || got.H() != near.H() {
			t.Fatalf("nearest target %v, want the axolotl 5 blocks away", got)
		}
		if got := oceanNearestTarget(tx, from.Add(mgl64.Vec3{-60, 0, 0}), 16); got != nil {
			t.Fatalf("target %v found with none within 16 blocks", got)
		}
	})
}
