package entity_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestSaddlingPlaysTheEquipSoundOfTheMob checks Pig.getEquipSound and
// Strider.getEquipSound: a pig or a strider that is saddled, by a player or by
// a dispenser, plays entity.pig.saddle or entity.strider.saddle, once; one that
// is already saddled plays nothing.
func TestSaddlingPlaysTheEquipSoundOfTheMob(t *testing.T) {
	t.Parallel()
	saddle := item.NewStack(item.Saddle{}, 1)
	for _, c := range []struct {
		name  string
		spawn func(world.EntitySpawnOpts) *world.EntityHandle
		sound string
		// saddled reports whether the mob wears a saddle.
		saddled func(world.Entity) bool
	}{
		{"pig", entity.NewPig, "entity.pig.saddle", func(e world.Entity) bool { return e.(*entity.Pig).Saddled() }},
		{"strider", entity.NewStrider, "entity.strider.saddle", func(e world.Entity) bool { return e.(*entity.Strider).Saddled() }},
	} {
		t.Run(c.name+" by a player", func(t *testing.T) {
			t.Parallel()
			w, _ := dispenserWorld(t)
			r := &soundNameRecorder{}
			w.Handle(r)
			<-w.Exec(func(tx *world.Tx) {
				p := addSurvivalPlayer(tx, mgl64.Vec3{.5, 64, .5})
				p.SetHeldItems(saddle.Grow(1), item.Stack{})
				mob := tx.AddEntity(c.spawn(world.EntitySpawnOpts{Position: mgl64.Vec3{2.5, 64, .5}}))
				if !p.UseItemOnEntity(mob) || !c.saddled(mob) {
					t.Fatal("the mob was not saddled")
				}
				if len(r.names) != 1 || r.names[0] != c.sound {
					t.Errorf("played %v, want %s", r.names, c.sound)
				}
				// A second saddle finds the slot full.
				r.names = nil
				p.UseItemOnEntity(mob)
				if len(r.names) != 0 {
					t.Errorf("played %v for a mob that wears a saddle, want nothing", r.names)
				}
			})
		})
		t.Run(c.name+" by a dispenser", func(t *testing.T) {
			t.Parallel()
			w, _ := dispenserWorld(t)
			r := &soundNameRecorder{}
			w.Handle(r)
			<-w.Exec(func(tx *world.Tx) {
				var mob world.Entity
				pos := dispenseOnce(tx, saddle, func(front cube.Pos) { mob = tx.AddEntity(c.spawn(world.EntitySpawnOpts{Position: front.Vec3Middle()})) })
				if !c.saddled(mob) || !slotOf(tx, pos).Empty() {
					t.Fatal("the mob was not saddled")
				}
				if len(r.names) != 1 || r.names[0] != c.sound {
					t.Errorf("played %v, want %s", r.names, c.sound)
				}
			})
		})
	}
}
