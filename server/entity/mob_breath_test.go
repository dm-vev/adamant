package entity_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
)

// TestMobsDrownUnderWater checks the air supply of LivingEntity.baseTick: a
// cow kept under water runs out of air after 15 seconds and drowns, while an
// undead husk breathes water.
func TestMobsDrownUnderWater(t *testing.T) {
	w := happyGhastWorld(t)
	var cow, husk *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		for x := -2; x <= 8; x++ {
			for z := -2; z <= 2; z++ {
				for y := 64; y <= 67; y++ {
					if y == 67 || x == -2 || x == 8 || z == -2 || z == 2 {
						tx.SetBlock(cube.Pos{x, y, z}, block.Stone{}, nil)
					} else {
						tx.SetBlock(cube.Pos{x, y, z}, block.Water{Still: true, Depth: 8}, nil)
					}
				}
			}
		}
		// Without its AI the cow does not float up to breathe.
		cow = spawnAnimal(t, tx, "minecraft:cow", .5, map[string]any{"NoAI": byte(1)})
		husk = spawnAnimal(t, tx, "minecraft:husk", 5.5, map[string]any{"PersistenceRequired": byte(1)})
	})
	for range 400 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		if l := livingOf(tx, cow); l != nil && l.Health() >= l.MaxHealth() {
			t.Fatal("cow under water for 20 seconds did not drown")
		}
		if l := livingOf(tx, husk); l == nil || l.Health() < l.MaxHealth() {
			t.Fatal("husk under water drowned")
		}
	})
}

func livingOf(tx *world.Tx, h *world.EntityHandle) entity.Living {
	e, ok := h.Entity(tx)
	if !ok {
		return nil
	}
	return e.(entity.Living)
}
