package entity_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
)

// TestZombiesCallReinforcements checks the reinforcements of
// Zombie.hurtServer: on hard a zombie certain to call reinforcements calls
// another zombie when a player hits it, and its chance then drops.
func TestZombiesCallReinforcements(t *testing.T) {
	w, p := naturalSpawnWorld(t, 18000)
	w.SetDifficulty(world.DifficultyHard)
	var z *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		z = spawnAnimal(t, tx, "minecraft:zombie", 4.5, map[string]any{"PersistenceRequired": byte(1), "attributes": []any{
			map[string]any{"id": "minecraft:spawn_reinforcements", "base": 1.0},
		}})
		pl, _ := p.Entity(tx)
		e, _ := z.Entity(tx)
		e.(entity.Living).Hurt(1, entity.AttackDamageSource{Attacker: pl})
		zombies := 0
		for e := range tx.Entities() {
			if _, ok := e.(*entity.Zombie); ok {
				zombies++
			}
		}
		if zombies != 2 {
			t.Fatalf("hurt zombie certain to call reinforcements left %d zombies, want 2", zombies)
		}
	})
}
