package entity_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestMobsDespawnFarFromPlayers checks Mob.checkDespawn: a monster more than
// 128 blocks from every player leaves the world, unless a name tag made it
// persistent, and an animal stays however far it is.
func TestMobsDespawnFarFromPlayers(t *testing.T) {
	w := happyGhastWorld(t)
	w.SetDifficulty(world.DifficultyNormal)
	var plain, named, cow *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		plain = spawnAnimal(t, tx, "minecraft:husk", .5, nil)
		named = spawnAnimal(t, tx, "minecraft:husk", 4.5, nil)
		cow = spawnAnimal(t, tx, "minecraft:cow", 8.5, nil)
		p := addSurvivalPlayer(tx, mgl64.Vec3{.5, 64, 140.5})
		tx.SetBlock(cube.Pos{0, 63, 140}, block.Stone{}, nil)
		tag := item.NewStack(item.NameTag{}, 1).WithCustomName("Husky")
		p.SetHeldItems(tag, item.Stack{})
		e, _ := named.Entity(tx)
		if !(item.NameTag{}).UseOnEntity(e, tx, p, &item.UseContext{}) {
			t.Fatal("name tag did not name the husk")
		}
	})
	for range 3 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		if _, ok := plain.Entity(tx); ok {
			t.Fatal("husk 140 blocks from the only player did not despawn")
		}
		e, ok := named.Entity(tx)
		if !ok {
			t.Fatal("named husk despawned")
		}
		if e.(*entity.Husk).NameTag() != "Husky" {
			t.Fatalf("named husk is called %q, want Husky", e.(*entity.Husk).NameTag())
		}
		if _, ok := cow.Entity(tx); !ok {
			t.Fatal("cow far from players despawned")
		}
	})
}
