package entity

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// stateViewRecorder records what entity state updates show.
type stateViewRecorder struct {
	world.NopViewer
	aggressiveVindicator, chargingPillager, bare bool
}

func (r *stateViewRecorder) ViewEntityState(e world.Entity) {
	switch e := e.(type) {
	case *Vindicator:
		r.aggressiveVindicator = r.aggressiveVindicator || e.Aggressive()
	case *Pillager:
		r.chargingPillager = r.chargingPillager || e.UsingItem() || e.CrossbowCharged()
	case *Ent:
		r.bare = true
	}
}

// TestIllagerStateReachesViewers checks that the state updates of attacking
// illagers reach viewers as their species. Updates used to show the bare
// entity, so sessions dropped the flags of the species: a vindicator never
// showed it was angry and raised its axe, nor a pillager its crossbow charge.
func TestIllagerStateReachesViewers(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry}.New()
	defer w.Close()
	w.SetDifficulty(world.DifficultyNormal)
	w.SetTime(6000)
	w.StopTime()
	recorder := &stateViewRecorder{}
	<-w.Exec(func(tx *world.Tx) {
		for x := -24; x <= 24; x++ {
			for z := -24; z <= 24; z++ {
				tx.SetBlock(cube.Pos{x, 64, z}, block.Stone{}, nil)
			}
		}
		loader := world.NewLoader(3, w, recorder)
		loader.Move(tx, mgl64.Vec3{0, 65, 0})
		loader.Load(tx, 49)
		tx.AddEntity(NewVillager(world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 65, 0.5}}))
		tx.AddEntity(NewVindicator(world.EntitySpawnOpts{Position: mgl64.Vec3{6.5, 65, 0.5}}))
		pillager := tx.AddEntity(NewPillager(world.EntitySpawnOpts{Position: mgl64.Vec3{-8.5, 65, 0.5}})).(*Pillager)
		pillager.state().main = item.NewStack(item.Crossbow{}, 1)
	})
	for range 200 {
		w.AdvanceTick()
	}
	if !recorder.aggressiveVindicator {
		t.Error("viewers were never shown an aggressive vindicator")
	}
	if !recorder.chargingPillager {
		t.Error("viewers were never shown a pillager charging its crossbow")
	}
	if recorder.bare {
		t.Error("a state update showed the bare entity instead of its species")
	}
}
