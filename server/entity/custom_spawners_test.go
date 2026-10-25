package entity_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/google/uuid"
)

// TestVillagesAreSieged checks VillageSiege: on a siege night zombies spawn
// one by one at the edge of the village a player is in.
func TestVillagesAreSieged(t *testing.T) {
	w, _ := naturalSpawnWorld(t, 18001)
	<-w.Exec(func(tx *world.Tx) {
		for i := range 6 {
			head := cube.Pos{-10 + 4*i, 64, -20}
			tx.SetBlock(head.Side(cube.FaceSouth), block.Bed{Facing: cube.North}, nil)
			tx.SetBlock(head, block.Bed{Facing: cube.North, Head: true}, nil)
			tx.ClaimPOI(head, uuid.New())
		}
	})
	entity.ForceVillageSiegeTonight(w)
	rec := &eventSpawnRecorder{}
	w.Handle(rec)
	for range 60 {
		w.AdvanceTick()
	}
	if rec.zombies == 0 {
		t.Fatal("no zombie besieged a village with a player in it on a siege night")
	}
}

// eventSpawnRecorder counts the zombies spawned for an event.
type eventSpawnRecorder struct {
	world.NopHandler
	zombies int
}

func (r *eventSpawnRecorder) HandleEntitySpawn(_ *world.Tx, e world.Entity) {
	if _, ok := e.(*entity.Zombie); ok && e.H().SpawnReason() == "event" {
		r.zombies++
	}
}
