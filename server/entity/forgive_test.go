package entity

import (
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestForgiveDeadPlayerClearsAngerWithinRange(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry}.New()
	defer w.Close()
	<-w.Exec(func(tx *world.Tx) {
		victim := tx.AddEntity(NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{0, 64, 0}}))
		near := tx.AddEntity(NewZombifiedPiglin(world.EntitySpawnOpts{Position: mgl64.Vec3{20, 64, 0}})).(*ZombifiedPiglin)
		far := tx.AddEntity(NewZombifiedPiglin(world.EntitySpawnOpts{Position: mgl64.Vec3{40, 64, 0}})).(*ZombifiedPiglin)
		for _, z := range []*ZombifiedPiglin{near, far} {
			z.Hurt(1, AttackDamageSource{Attacker: victim})
			if z.z().target != victim.H().UUID().String() {
				t.Fatalf("zombified piglin did not target its attacker")
			}
		}
		if err := tx.World().SetGameRule("forgive_dead_players", false); err != nil {
			t.Fatal(err)
		}
		ForgiveDeadPlayer(tx, victim)
		if near.z().anger == 0 {
			t.Fatal("anger cleared with forgive_dead_players off")
		}
		if err := tx.World().SetGameRule("forgive_dead_players", true); err != nil {
			t.Fatal(err)
		}
		ForgiveDeadPlayer(tx, victim)
		if near.z().anger != 0 || near.z().target != "" {
			t.Fatalf("nearby piglin still angry: anger=%d target=%q", near.z().anger, near.z().target)
		}
		if far.z().anger == 0 {
			t.Fatal("piglin beyond 32 blocks was forgiven")
		}
	})
}
