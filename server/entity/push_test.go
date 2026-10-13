package entity

import (
	"testing"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestOverlappingMobsArePushedApart(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry}.New()
	defer w.Close()
	<-w.Exec(func(tx *world.Tx) {
		a := tx.AddEntity(NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{0, 64, 0}})).(*Cow)
		b := tx.AddEntity(NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{0.3, 64, 0}})).(*Cow)
		drop := tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: mgl64.Vec3{0.1, 64, 0}}, item.NewStack(item.Stick{}, 1))).(*Ent)
		TickEntityCollisions(tx)
		if a.Velocity()[0] >= 0 || b.Velocity()[0] <= 0 {
			t.Fatalf("cows not pushed apart: a=%v b=%v", a.Velocity(), b.Velocity())
		}
		if drop.Velocity() != (mgl64.Vec3{}) {
			t.Fatalf("item entity was pushed: %v", drop.Velocity())
		}
	})
}

func TestCrammingDamagesPackedMobs(t *testing.T) {
	for _, limit := range []int{24, 0} {
		w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry}.New()
		<-w.Exec(func(tx *world.Tx) {
			if err := tx.World().SetGameRule("max_entity_cramming", limit); err != nil {
				t.Fatal(err)
			}
			cows := make([]*Cow, 0, 26)
			for i := range 26 {
				cows = append(cows, tx.AddEntity(NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{float64(i) * 0.001, 64, 0}})).(*Cow))
			}
			for range 40 {
				TickEntityCollisions(tx)
			}
			hurt := 0
			for _, c := range cows {
				if c.Health() < c.MaxHealth() {
					hurt++
				}
			}
			if limit > 0 && hurt == 0 {
				t.Fatal("26 packed cows took no cramming damage with max_entity_cramming 24")
			}
			if limit == 0 && hurt != 0 {
				t.Fatalf("%d cows took cramming damage with max_entity_cramming 0", hurt)
			}
		})
		_ = w.Close()
	}
}
