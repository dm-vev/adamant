package entity_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func shulkerAIWorld(t *testing.T) (*world.World, *world.EntityHandle, *world.EntityHandle) {
	t.Helper()
	w := world.Config{Entities: entity.DefaultRegistry, Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { w.Close() })
	w.SetDifficulty(world.DifficultyNormal)
	var shulker, target *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{0, 69, 8}, block.Stone{}, nil)
		shulker = entity.NewShulker(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 70, 8.5}}, entity.ShulkerBehaviourConfig{})
		tx.AddEntity(shulker)
		target = world.EntitySpawnOpts{}.New(player.Type, player.Config{Name: "ShulkerTarget", Position: mgl64.Vec3{8.5, 70, 8.5}, GameMode: world.GameModeSurvival})
		tx.AddEntity(target)
	})
	return w, shulker, target
}

func tickShulker(tx *world.Tx, h *world.EntityHandle, ticks int) *entity.Shulker {
	actor, _ := h.Entity(tx)
	shulker := actor.(*entity.Shulker)
	for tick := range ticks {
		shulker.Tick(tx, int64(tick))
	}
	return shulker
}

func removeShulkerBullets(tx *world.Tx) {
	for actor := range tx.Entities() {
		if actor.H().Type().EncodeEntity() == "minecraft:shulker_bullet" {
			_ = actor.Close()
		}
	}
}

func TestShulkerStopsAttackingInvalidTarget(t *testing.T) {
	for _, tc := range []struct {
		name       string
		invalidate func(*world.Tx, *player.Player)
	}{
		{"creative", func(_ *world.Tx, p *player.Player) { p.SetGameMode(world.GameModeCreative) }},
		{"spectator", func(_ *world.Tx, p *player.Player) { p.SetGameMode(world.GameModeSpectator) }},
		{"out of range", func(_ *world.Tx, p *player.Player) { p.Teleport(mgl64.Vec3{32.5, 70, 8.5}) }},
		{"disconnected", func(_ *world.Tx, p *player.Player) { _ = p.Close() }},
		{"peaceful", func(tx *world.Tx, _ *player.Player) { tx.World().SetDifficulty(world.DifficultyPeaceful) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, shulker, target := shulkerAIWorld(t)
			<-w.Exec(func(tx *world.Tx) {
				if mob := tickShulker(tx, shulker, 20); mob.Peek() != 100 || countEntities(tx, "minecraft:shulker_bullet") != 1 {
					t.Fatal("survival player did not provoke an open shell and a shot")
				}
				removeShulkerBullets(tx)
				actor, _ := target.Entity(tx)
				tc.invalidate(tx, actor.(*player.Player))
				for tick := range 120 {
					if mob := tickShulker(tx, shulker, 1); mob.Peek() != 0 {
						t.Fatalf("shell remained open at tick %d after target became invalid: peek=%d", tick+1, mob.Peek())
					}
				}
				if got := countEntities(tx, "minecraft:shulker_bullet"); got != 0 {
					t.Fatalf("idle shulker fired %d shots at an invalid target", got)
				}
			})
		})
	}
}

func TestShulkerRestartsAttackAfterIdle(t *testing.T) {
	w, shulker, target := shulkerAIWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		actor, _ := target.Entity(tx)
		p := actor.(*player.Player)
		for _, mode := range []world.GameMode{world.GameModeSurvival, world.GameModeAdventure} {
			p.SetGameMode(world.GameModeCreative)
			if mob := tickShulker(tx, shulker, 120); mob.Peek() != 0 || countEntities(tx, "minecraft:shulker_bullet") != 0 {
				t.Fatal("creative observer kept the shulker attacking")
			}
			p.SetGameMode(mode)
			if mob := tickShulker(tx, shulker, 19); mob.Peek() != 100 || countEntities(tx, "minecraft:shulker_bullet") != 0 {
				t.Fatal("reacquired target did not open the shell with a fresh attack windup")
			}
			tickShulker(tx, shulker, 1)
			if got := countEntities(tx, "minecraft:shulker_bullet"); got != 1 {
				t.Fatalf("attack did not resume after 20 ticks: shots=%d", got)
			}
			removeShulkerBullets(tx)
		}
	})
}

func TestShulkerForgetsHiddenTarget(t *testing.T) {
	w, shulker, _ := shulkerAIWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		tickShulker(tx, shulker, 20)
		for y := 70; y < 73; y++ {
			tx.SetBlock(cube.Pos{4, y, 8}, block.Stone{}, nil)
		}
		if mob := tickShulker(tx, shulker, 60); mob.Peek() != 100 {
			t.Fatal("briefly hidden target was forgotten before the sight memory expired")
		}
		if mob := tickShulker(tx, shulker, 1); mob.Peek() != 0 {
			t.Fatalf("hidden target kept the shulker attacking after 60 ticks: peek=%d", mob.Peek())
		}
		removeShulkerBullets(tx)
		if mob := tickShulker(tx, shulker, 120); mob.Peek() != 0 || countEntities(tx, "minecraft:shulker_bullet") != 0 {
			t.Fatal("shulker reacquired the player through a solid wall")
		}
		for y := 70; y < 73; y++ {
			tx.SetBlock(cube.Pos{4, y, 8}, block.Air{}, nil)
		}
		tickShulker(tx, shulker, 20)
		if got := countEntities(tx, "minecraft:shulker_bullet"); got != 1 {
			t.Fatalf("shulker did not reacquire the visible player: shots=%d", got)
		}
	})
}
