package entity

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

func TestForgiveDeadPlayerClearsAngerWithinRange(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry}.New()
	defer w.Close()
	var victim, near, far *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		for x := -4; x <= 44; x++ {
			for z := -4; z <= 4; z++ {
				tx.SetBlock(cube.Pos{x, 63, z}, block.Stone{}, nil)
			}
		}
		victim = tx.AddEntity(NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{0, 64, 0}})).H()
		near = tx.AddEntity(NewZombifiedPiglin(world.EntitySpawnOpts{Position: mgl64.Vec3{20, 64, 0}})).H()
		far = tx.AddEntity(NewZombifiedPiglin(world.EntitySpawnOpts{Position: mgl64.Vec3{40, 64, 0}})).H()
		v, _ := victim.Entity(tx)
		for _, h := range []*world.EntityHandle{near, far} {
			z, _ := h.Entity(tx)
			z.(*ZombifiedPiglin).Hurt(1, AttackDamageSource{Attacker: v})
		}
	})
	// The piglins take their attacker as target and grow angry at it on
	// their next ticks.
	for range 4 {
		if err := w.StepTicks(1); err != nil {
			t.Fatal(err)
		}
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		v, _ := victim.Entity(tx)
		state := func(h *world.EntityHandle) *zombifiedPiglinState {
			e, _ := h.Entity(tx)
			return e.(*ZombifiedPiglin).z()
		}
		for _, h := range []*world.EntityHandle{near, far} {
			if state(h).angerTarget != v.H().UUID() {
				t.Fatalf("zombified piglin did not grow angry at its attacker")
			}
		}
		if err := tx.World().SetGameRule("forgive_dead_players", false); err != nil {
			t.Fatal(err)
		}
		ForgiveDeadPlayer(tx, v)
		if state(near).angerTicks == 0 {
			t.Fatal("anger cleared with forgive_dead_players off")
		}
		if err := tx.World().SetGameRule("forgive_dead_players", true); err != nil {
			t.Fatal(err)
		}
		ForgiveDeadPlayer(tx, v)
		if s := state(near); s.angerTicks != 0 || s.angerTarget != uuid.Nil {
			t.Fatalf("nearby piglin still angry: anger=%d target=%v", s.angerTicks, s.angerTarget)
		}
		if state(far).angerTicks == 0 {
			t.Fatal("piglin beyond 32 blocks was forgiven")
		}
	})
}
