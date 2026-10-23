package player

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestZombifiedPiglinIsNeutralUntilProvoked(t *testing.T) {
	for _, universal := range []bool{false, true} {
		w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: entity.DefaultRegistry}.New()
		var attackerH, bystanderH, piglinH *world.EntityHandle
		<-w.Exec(func(tx *world.Tx) {
			tx.World().SetDifficulty(world.DifficultyNormal)
			if err := tx.World().SetGameRule("universal_anger", universal); err != nil {
				t.Fatal(err)
			}
			for x := -4; x <= 8; x++ {
				for z := -4; z <= 4; z++ {
					tx.SetBlock(cube.Pos{x, 63, z}, block.Stone{}, nil)
				}
			}
			attackerH = tx.AddEntity(world.EntitySpawnOpts{}.New(Type, Config{Name: "Attacker", Position: mgl64.Vec3{1.5, 64, 0.5}, GameMode: world.GameModeSurvival})).H()
			bystanderH = tx.AddEntity(world.EntitySpawnOpts{}.New(Type, Config{Name: "Bystander", Position: mgl64.Vec3{0.5, 64, 2.5}, GameMode: world.GameModeSurvival})).H()
			piglinH = tx.AddEntity(entity.NewZombifiedPiglin(world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 64, 0.5}})).H()
		})
		exec := func(f func(attacker, bystander *Player, z *entity.ZombifiedPiglin)) {
			<-w.Exec(func(tx *world.Tx) {
				a, _ := attackerH.Entity(tx)
				b, _ := bystanderH.Entity(tx)
				z, _ := piglinH.Entity(tx)
				f(a.(*Player), b.(*Player), z.(*entity.ZombifiedPiglin))
			})
		}
		hurt := false
		run := func(n int) {
			for range n {
				if err := w.StepTicks(1); err != nil {
					t.Fatal(err)
				}
				w.AdvanceTick()
				exec(func(a, b *Player, _ *entity.ZombifiedPiglin) {
					hurt = hurt || a.Health() < a.MaxHealth() || b.Health() < b.MaxHealth()
				})
			}
		}
		run(100)
		exec(func(_, bystander *Player, z *entity.ZombifiedPiglin) {
			if hurt {
				t.Fatalf("universal_anger=%v: unprovoked piglin attacked a player", universal)
			}
			if z.IsPreventingPlayerRest(bystander) {
				t.Fatalf("universal_anger=%v: unprovoked piglin prevents rest", universal)
			}
		})
		exec(func(attacker, _ *Player, z *entity.ZombifiedPiglin) {
			z.Hurt(1, entity.AttackDamageSource{Attacker: attacker})
		})
		// The goals of the piglin react to the hit on its next ticks.
		run(2)
		exec(func(attacker, bystander *Player, z *entity.ZombifiedPiglin) {
			if !z.IsPreventingPlayerRest(attacker) {
				t.Fatalf("universal_anger=%v: piglin not angry at its attacker", universal)
			}
			if got := z.IsPreventingPlayerRest(bystander); got != universal {
				t.Fatalf("universal_anger=%v: angry at bystander = %v", universal, got)
			}
		})
		run(60)
		if !hurt {
			t.Fatalf("universal_anger=%v: provoked piglin never attacked", universal)
		}
		_ = w.Close()
	}
}
