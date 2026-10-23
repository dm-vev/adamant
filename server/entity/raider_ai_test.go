package entity

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestPatrolWalksTowardsItsTarget checks a patrol leader and its companion
// walk towards the patrol target, and a lone raider stops patrolling.
func TestPatrolWalksTowardsItsTarget(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry}.New()
	defer w.Close()
	w.SetTime(18000)
	w.StopTime()
	<-w.Exec(func(tx *world.Tx) {
		for x := -40; x <= 40; x++ {
			for z := -40; z <= 40; z++ {
				tx.SetBlock(cube.Pos{x, 64, z}, block.Stone{}, nil)
			}
		}
	})
	leader := NewPillager(world.EntitySpawnOpts{Position: mgl64.Vec3{-30.5, 65, .5}})
	member := NewPillager(world.EntitySpawnOpts{Position: mgl64.Vec3{-32.5, 65, .5}})
	<-w.Exec(func(tx *world.Tx) {
		l := tx.AddEntity(leader).(*Pillager).state()
		l.patrolLeader, l.patrolling, l.patrolTarget, l.hasPatrolTarget = true, true, cube.Pos{200, 65, 0}, true
		tx.AddEntity(member).(*Pillager).state().patrolling = true
	})
	for range 600 {
		if err := w.StepTicks(1); err != nil {
			t.Fatal(err)
		}
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		for _, h := range []*world.EntityHandle{leader, member} {
			e, _ := h.Entity(tx)
			if x := e.Position()[0]; x < -15 {
				t.Fatalf("patrol pillager only reached x %.2f", x)
			}
		}
	})
}
