package entity_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
)

// TestMobsStepOnMagma checks Entity.applyEffectsFromBlocks and
// MagmaBlock.stepOn: magma burns a mob standing on it, but not one immune
// to fire.
func TestMobsStepOnMagma(t *testing.T) {
	w := happyGhastWorld(t)
	var zombie, piglin *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		for x := -3; x <= 9; x++ {
			for z := -3; z <= 3; z++ {
				tx.SetBlock(cube.Pos{x, 63, z}, block.Magma{}, nil)
				if x == -3 || x == 9 || z == -3 || z == 3 {
					tx.SetBlock(cube.Pos{x, 64, z}, block.Stone{}, nil)
					tx.SetBlock(cube.Pos{x, 65, z}, block.Stone{}, nil)
				}
			}
		}
		zombie = spawnAnimal(t, tx, "minecraft:husk", .5, map[string]any{"PersistenceRequired": byte(1)})
		piglin = spawnAnimal(t, tx, "minecraft:zombified_piglin", 6.5, map[string]any{"PersistenceRequired": byte(1)})
	})
	for range 40 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		z, _ := zombie.Entity(tx)
		p, _ := piglin.Entity(tx)
		if l := z.(entity.Living); l.Health() >= l.MaxHealth() {
			t.Fatal("husk standing on magma for two seconds was not burnt")
		}
		if l := p.(entity.Living); l.Health() < l.MaxHealth() {
			t.Fatalf("fire-immune zombified piglin on magma lost health: %v", l.Health())
		}
	})
}
