package player

import (
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestGoldenAppleTopsUpAbsorption follows AbsorptionMobEffect.onEffectStarted,
// which LivingEntity.addEffect calls for every effect added: absorption is
// raised to four points, never stacked beyond.
func TestGoldenAppleTopsUpAbsorption(t *testing.T) {
	hungerTestPlayer(t, world.DifficultyNormal, func(tx *world.Tx, p *Player) {
		item.GoldenApple{}.Consume(tx, p)
		if p.Absorption() != 4 {
			t.Fatalf("absorption after one apple = %v, want 4", p.Absorption())
		}
		item.GoldenApple{}.Consume(tx, p)
		if p.Absorption() != 4 {
			t.Fatalf("absorption after two apples = %v, want 4", p.Absorption())
		}
		p.SetAbsorption(1)
		item.GoldenApple{}.Consume(tx, p)
		if p.Absorption() != 4 {
			t.Fatalf("absorption topped up from 1 = %v, want 4", p.Absorption())
		}
		item.EnchantedApple{}.Consume(tx, p)
		if e, _ := p.Effect(effect.Regeneration); p.Absorption() != 16 || e.Duration() != 20*time.Second {
			t.Fatalf("enchanted apple: absorption %v, regeneration %v; want 16, 20s", p.Absorption(), e.Duration())
		}
	})
}

// TestChorusFruitTeleportsOntoGround follows TeleportRandomlyConsumeEffect:
// the player lands on a block entities can teleport to within eight blocks,
// clear of blocks, and below the Nether's logical height of 128, at most a
// block above the floor.
func TestChorusFruitTeleportsOntoGround(t *testing.T) {
	for _, dim := range []world.Dimension{world.Overworld, world.Nether} {
		w := world.Config{Synchronous: true, Dim: dim}.New()
		t.Cleanup(func() { _ = w.Close() })
		<-w.Exec(func(tx *world.Tx) {
			floor := 100
			if dim == world.Nether {
				floor = 124
			}
			for x := -16; x <= 31; x++ {
				for z := -16; z <= 31; z++ {
					tx.SetBlock(cube.Pos{x, floor, z}, block.Stone{}, nil)
				}
			}
			start := mgl64.Vec3{8.5, float64(floor + 1), 8.5}
			p := tx.AddEntity(world.EntitySpawnOpts{Position: start}.New(Type, Config{Position: start, GameMode: world.GameModeSurvival})).(*Player)
			for range 50 {
				p.Teleport(start)
				item.ChorusFruit{}.Consume(tx, p)
				pos := p.Position()
				// randomTeleport keeps the fraction of the target height, so
				// the player may arrive up to a block above the floor.
				if pos[1] < float64(floor+1) || pos[1] >= float64(floor+2) || pos.Sub(start).Len() > 8*1.75 {
					t.Fatalf("%v: chorus fruit took the player to %v, want the floor at y %v near %v", dim, pos, floor+1, start)
				}
			}
		})
	}
}
