package entity_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func addVillager(t *testing.T, tx *world.Tx, pos mgl64.Vec3) *entity.Villager {
	t.Helper()
	h, err := entity.NewFromJavaNBT(map[string]any{"id": "minecraft:villager"}, world.EntitySpawnOpts{Position: pos, SpawnFinalized: true})
	if err != nil {
		t.Fatal(err)
	}
	return tx.AddEntity(h).(*entity.Villager)
}

func villager(tx *world.Tx, h *world.EntityHandle) *entity.Villager {
	e, _ := h.Entity(tx)
	return e.(*entity.Villager)
}

func TestVillagersRunFromZombies(t *testing.T) {
	w := traderTestWorld(t)
	var h *world.EntityHandle
	zombieAt := mgl64.Vec3{5.5, 64, .5}
	<-w.Exec(func(tx *world.Tx) {
		h = addVillager(t, tx, mgl64.Vec3{.5, 64, .5}).H()
		addMob(t, tx, "minecraft:zombie", zombieAt)
	})
	w.AdvanceTick()
	<-w.Exec(func(tx *world.Tx) {
		if !villager(tx, h).Panicking() {
			t.Fatal("villager does not panic with a zombie five blocks away")
		}
	})
	for range 60 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		if d := villager(tx, h).Position().Sub(zombieAt).Len(); d < 8 {
			t.Fatalf("villager is %.2f blocks from where the zombie appeared, want it to flee", d)
		}
	})
}

func TestHurtVillagersCalmDown(t *testing.T) {
	w := traderTestWorld(t)
	var h *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		h = addVillager(t, tx, mgl64.Vec3{.5, 64, .5}).H()
		villager(tx, h).Hurt(1, entity.FallDamageSource{})
	})
	w.AdvanceTick()
	<-w.Exec(func(tx *world.Tx) {
		if !villager(tx, h).Panicking() {
			t.Fatal("hurt villager does not panic")
		}
	})
	for range 45 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		if villager(tx, h).Panicking() {
			t.Fatal("villager still panics two seconds after a fall")
		}
	})
}

// panickingVillagerGolems hurts three villagers penned in a 5x5 yard every
// second for ten seconds and returns how many iron golems they summoned.
func panickingVillagerGolems(t *testing.T, slept bool) int {
	w := traderTestWorld(t)
	var villagers []*world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		for x := -3; x <= 3; x++ {
			for z := -3; z <= 3; z++ {
				if x == -3 || x == 3 || z == -3 || z == 3 {
					tx.SetBlock(cube.Pos{x, 64, z}, block.Stone{}, nil)
					tx.SetBlock(cube.Pos{x, 65, z}, block.Stone{}, nil)
				}
			}
		}
		for i := range 3 {
			v := addVillager(t, tx, mgl64.Vec3{float64(i*2) - 1.5, 64, .5})
			if slept {
				v.MarkSlept(tx.CurrentTick())
			}
			villagers = append(villagers, v.H())
		}
	})
	golems := 0
	for tick := range 200 {
		if tick%20 == 0 {
			<-w.Exec(func(tx *world.Tx) {
				for _, h := range villagers {
					villager(tx, h).Hurt(.5, entity.FallDamageSource{})
				}
			})
		}
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) { golems = countEntities(tx, "minecraft:iron_golem") })
	return golems
}

func TestPanickingVillagersSummonAnIronGolem(t *testing.T) {
	if n := panickingVillagerGolems(t, true); n != 1 {
		t.Fatalf("three panicking villagers that slept summoned %d iron golems, want one", n)
	}
	if n := panickingVillagerGolems(t, false); n != 0 {
		t.Fatalf("villagers that did not sleep summoned %d iron golems", n)
	}
}
