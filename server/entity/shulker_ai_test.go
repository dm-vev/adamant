package entity_test

import (
	"math"
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

// shulkerAttacks ticks the shulker until its shell opens fully to attack and
// it fires its first bullet, reporting whether it did within ticks.
func shulkerAttacks(tx *world.Tx, h *world.EntityHandle, ticks int) bool {
	opened := -1
	for tick := range ticks {
		mob := tickShulker(tx, h, 1)
		if opened < 0 && mob.Peek() == 100 {
			opened = tick
		}
		if opened >= 0 && countEntities(tx, "minecraft:shulker_bullet") > 0 {
			return true
		}
	}
	return false
}

// TestShulkerStopsAttackingInvalidTarget checks ShulkerAttackGoal and
// TargetGoal.canContinueToUse: a shulker closes its shell once its target
// becomes invalid.
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
				if !shulkerAttacks(tx, shulker, 400) {
					t.Fatal("survival player did not provoke an open shell and a shot")
				}
				actor, _ := target.Entity(tx)
				tc.invalidate(tx, actor.(*player.Player))
				if mob := tickShulker(tx, shulker, 4); mob.Peek() == 100 {
					t.Fatal("shell stayed fully open after the target became invalid")
				}
				removeShulkerBullets(tx)
				for tick := range 120 {
					if mob := tickShulker(tx, shulker, 1); mob.Peek() == 100 {
						t.Fatalf("shell opened to attack at tick %d after the target became invalid", tick+1)
					}
				}
				if got := countEntities(tx, "minecraft:shulker_bullet"); got != 0 {
					t.Fatalf("idle shulker fired %d shots at an invalid target", got)
				}
			})
		})
	}
}

// TestShulkerRestartsAttackAfterIdle checks that a shulker attacks a player
// again once they are no longer in creative mode.
func TestShulkerRestartsAttackAfterIdle(t *testing.T) {
	w, shulker, target := shulkerAIWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		actor, _ := target.Entity(tx)
		p := actor.(*player.Player)
		for _, mode := range []world.GameMode{world.GameModeSurvival, world.GameModeAdventure} {
			p.SetGameMode(world.GameModeCreative)
			tickShulker(tx, shulker, 120)
			removeShulkerBullets(tx)
			if mob := tickShulker(tx, shulker, 60); mob.Peek() == 100 || countEntities(tx, "minecraft:shulker_bullet") != 0 {
				t.Fatal("creative observer kept the shulker attacking")
			}
			p.SetGameMode(mode)
			// LookAtPlayerGoal outranks ShulkerAttackGoal and may interrupt
			// it, so the first shot may take a few attempts.
			if !shulkerAttacks(tx, shulker, 400) {
				t.Fatal("shulker did not attack the player again")
			}
			removeShulkerBullets(tx)
		}
	})
}

// TestShulkerForgetsHiddenTarget checks the sight memory of TargetGoal: a
// shulker forgets a target hidden for three seconds and finds it again once
// it is visible.
func TestShulkerForgetsHiddenTarget(t *testing.T) {
	w, shulker, _ := shulkerAIWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		if !shulkerAttacks(tx, shulker, 400) {
			t.Fatal("survival player did not provoke an attack")
		}
		for y := 70; y < 73; y++ {
			tx.SetBlock(cube.Pos{4, y, 8}, block.Stone{}, nil)
		}
		// The sight memory of TargetGoal lasts three seconds.
		tickShulker(tx, shulker, 100)
		removeShulkerBullets(tx)
		for range 120 {
			if mob := tickShulker(tx, shulker, 1); mob.Peek() == 100 {
				t.Fatal("shulker reacquired the player through a solid wall")
			}
		}
		for y := 70; y < 73; y++ {
			tx.SetBlock(cube.Pos{4, y, 8}, block.Air{}, nil)
		}
		if !shulkerAttacks(tx, shulker, 400) {
			t.Fatal("shulker did not reacquire the visible player")
		}
	})
}

// TestShulkersTeleportWhenHurt checks Shulker.hurtServer: a shulker hurt
// below half its health may teleport to another spot it can stay at.
func TestShulkersTeleportWhenHurt(t *testing.T) {
	w := wolfTestWorld(t)
	var shulker *world.EntityHandle
	start := mgl64.Vec3{.5, 64, .5}
	<-w.Exec(func(tx *world.Tx) {
		mob := tx.AddEntity(entity.NewShulker(world.EntitySpawnOpts{Position: start}, entity.ShulkerBehaviourConfig{})).(*entity.Shulker)
		shulker = mob.H()
		// The closed shell takes the hit down to 6 of its 30 health.
		mob.Hurt(30, entity.GenericDamageSource{})
	})
	for range 80 {
		// Hits more than half a second apart all land.
		for range 11 {
			w.AdvanceTick()
		}
		moved := false
		<-w.Exec(func(tx *world.Tx) {
			actor, ok := shulker.Entity(tx)
			if !ok {
				t.Fatal("shulker died before it teleported")
			}
			s := actor.(*entity.Shulker)
			if p := s.Position(); p != start {
				if p[0]-math.Floor(p[0]) != .5 || p[2]-math.Floor(p[2]) != .5 || p[1] != math.Floor(p[1]) {
					t.Fatalf("shulker teleported off a block: %v", p)
				}
				moved = true
				return
			}
			s.Hurt(.1, entity.GenericDamageSource{})
		})
		if moved {
			return
		}
	}
	t.Fatal("shulker below half health never teleported")
}
