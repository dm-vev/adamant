package entity_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestLlamasSpitOnceAtTheirAttacker checks LlamaHurtByTargetGoal with
// RangedAttackGoal: a hurt llama spits at its attacker and then calms down.
func TestLlamasSpitOnceAtTheirAttacker(t *testing.T) {
	w := traderTestWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		h := spawnAnimal(t, tx, "minecraft:llama", .5, nil)
		p := addSurvivalPlayer(tx, mgl64.Vec3{6.5, 64, .5})
		e, _ := h.Entity(tx)
		e.(entity.Living).Hurt(1, entity.AttackDamageSource{Attacker: p})
	})
	spits := 0
	for range 200 {
		w.AdvanceTick()
		<-w.Exec(func(tx *world.Tx) { spits = max(spits, countEntities(tx, "minecraft:llama_spit")) })
	}
	if spits != 1 {
		t.Fatalf("hurt llama spat %d times at its attacker in ten seconds, want once", spits)
	}
}

// TestLlamasFollowALeashedLlama checks LlamaFollowCaravanGoal: a free llama
// near a llama on a lead walks up behind it.
func TestLlamasFollowALeashedLlama(t *testing.T) {
	w := traderTestWorld(t)
	var lead, follower *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		p := addSurvivalPlayer(tx, mgl64.Vec3{-1.5, 64, .5})
		lead = spawnAnimal(t, tx, "minecraft:llama", .5, nil)
		e, _ := lead.Entity(tx)
		if !e.(entity.Leashable).AttachLeash(p, tx) {
			t.Fatal("could not leash the llama")
		}
		follower = spawnAnimal(t, tx, "minecraft:llama", 8.5, nil)
	})
	// The leashed llama strolls on, so the follower is judged by how close
	// it came.
	closest := 100.0
	for range 100 {
		w.AdvanceTick()
		<-w.Exec(func(tx *world.Tx) {
			closest = min(closest, horizontalDistance(entityPos(tx, follower), entityPos(tx, lead)))
		})
	}
	if closest > 3.5 {
		t.Fatalf("free llama stayed %.2f blocks from the leashed llama, want it to join its caravan two blocks behind", closest)
	}
}
