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
			attacker := tx.AddEntity(world.EntitySpawnOpts{}.New(Type, Config{Name: "Attacker", Position: mgl64.Vec3{1.5, 64, 0.5}, GameMode: world.GameModeSurvival})).(*Player)
			bystander := tx.AddEntity(world.EntitySpawnOpts{}.New(Type, Config{Name: "Bystander", Position: mgl64.Vec3{0.5, 64, 2.5}, GameMode: world.GameModeSurvival})).(*Player)
			z := tx.AddEntity(entity.NewZombifiedPiglin(world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 64, 0.5}})).(*entity.ZombifiedPiglin)
			tick := int64(0)
			run := func(n int) {
				for range n {
					tick++
					z.Tick(tx, tick)
				}
			}
			run(100)
			if attacker.Health() != attacker.MaxHealth() || bystander.Health() != bystander.MaxHealth() {
				t.Fatalf("universal_anger=%v: unprovoked piglin attacked a player", universal)
			}
			if z.IsPreventingPlayerRest(bystander) {
				t.Fatalf("universal_anger=%v: unprovoked piglin prevents rest", universal)
			}
			z.Hurt(1, entity.AttackDamageSource{Attacker: attacker})
			if !z.IsPreventingPlayerRest(attacker) {
				t.Fatalf("universal_anger=%v: piglin not angry at its attacker", universal)
			}
			if got := z.IsPreventingPlayerRest(bystander); got != universal {
				t.Fatalf("universal_anger=%v: angry at bystander = %v", universal, got)
			}
			run(60)
			if attacker.Health() == attacker.MaxHealth() && bystander.Health() == bystander.MaxHealth() {
				t.Fatalf("universal_anger=%v: provoked piglin never attacked", universal)
			}
		})
		_ = w.Close()
	}
}
