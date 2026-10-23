package entity_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// aiTestWorld returns a world with a stone floor at y 64 from -16 to 16 on
// both axes, lit by night so zombies don't burn.
func aiTestWorld(t *testing.T) *world.World {
	t.Helper()
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: entity.DefaultRegistry}.New()
	t.Cleanup(func() { _ = w.Close() })
	w.SetTime(18000)
	w.StopTime()
	w.SetDifficulty(world.DifficultyNormal)
	<-w.Exec(func(tx *world.Tx) {
		for x := -16; x <= 16; x++ {
			for z := -16; z <= 16; z++ {
				tx.SetBlock(cube.Pos{x, 64, z}, block.Stone{}, nil)
			}
		}
	})
	return w
}

// stepWorld advances the world by n ticks.
func stepWorld(t *testing.T, w *world.World, n int) {
	t.Helper()
	for range n {
		if err := w.StepTicks(1); err != nil {
			t.Fatal(err)
		}
		w.AdvanceTick()
	}
}

func addAITestPlayer(w *world.World, pos mgl64.Vec3, mode world.GameMode) *world.EntityHandle {
	h := world.EntitySpawnOpts{}.New(player.Type, player.Config{Name: "AITest", Position: pos, GameMode: mode})
	<-w.Exec(func(tx *world.Tx) { tx.AddEntity(h) })
	return h
}

func addZombie(w *world.World, pos mgl64.Vec3) *world.EntityHandle {
	h := entity.NewZombie(world.EntitySpawnOpts{Position: pos})
	<-w.Exec(func(tx *world.Tx) { tx.AddEntity(h) })
	return h
}

func aiEntityPos(w *world.World, h *world.EntityHandle) (pos mgl64.Vec3) {
	<-w.Exec(func(tx *world.Tx) {
		if e, ok := h.Entity(tx); ok {
			pos = e.Position()
		}
	})
	return pos
}

// TestZombieClimbsStepsToReachItsTarget checks the zombie jumps onto blocks
// in its way, which mobs walking by velocity alone could not.
func TestZombieClimbsStepsToReachItsTarget(t *testing.T) {
	w := aiTestWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		for z := -16; z <= 16; z++ {
			for x := 3; x <= 16; x++ {
				tx.SetBlock(cube.Pos{x, 65, z}, block.Stone{}, nil)
			}
		}
	})
	addAITestPlayer(w, mgl64.Vec3{8.5, 66, .5}, world.GameModeSurvival)
	zombie := addZombie(w, mgl64.Vec3{.5, 65, .5})
	stepWorld(t, w, 200)
	if pos := aiEntityPos(w, zombie); pos[1] < 66 || pos[0] < 5 {
		t.Fatalf("zombie at %v did not climb the step towards the player", pos)
	}
}

// TestZombiePathsAroundFences checks the zombie walks around a fence it
// sees its target through rather than into it.
func TestZombiePathsAroundFences(t *testing.T) {
	w := aiTestWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		for z := -5; z <= 5; z++ {
			tx.SetBlock(cube.Pos{3, 65, z}, block.WoodFence{Wood: block.OakWood()}, nil)
		}
	})
	p := addAITestPlayer(w, mgl64.Vec3{7.5, 65, .5}, world.GameModeSurvival)
	zombie := addZombie(w, mgl64.Vec3{.5, 65, .5})
	lowest := 20.0
	for range 400 {
		stepWorld(t, w, 1)
		<-w.Exec(func(tx *world.Tx) {
			e, _ := p.Entity(tx)
			lowest = min(lowest, e.(*player.Player).Health())
		})
	}
	if pos := aiEntityPos(w, zombie); lowest == 20 {
		t.Fatalf("zombie at %v did not get around the fence to the player", pos)
	}
}

// TestZombieAttacksSurvivalPlayersOnly checks the zombie hurts a survival
// player in reach and leaves a creative one alone.
func TestZombieAttacksSurvivalPlayersOnly(t *testing.T) {
	for _, tc := range []struct {
		mode world.GameMode
		hurt bool
	}{{world.GameModeSurvival, true}, {world.GameModeCreative, false}} {
		w := aiTestWorld(t)
		p := addAITestPlayer(w, mgl64.Vec3{4.5, 65, .5}, tc.mode)
		addZombie(w, mgl64.Vec3{.5, 65, .5})
		lowest := 20.0
		for range 200 {
			stepWorld(t, w, 1)
			<-w.Exec(func(tx *world.Tx) {
				e, _ := p.Entity(tx)
				lowest = min(lowest, e.(*player.Player).Health())
			})
		}
		if hurt := lowest < 20; hurt != tc.hurt {
			t.Errorf("%v player hurt = %v (lowest health %v), want %v", tc.mode, hurt, lowest, tc.hurt)
		}
	}
}

// TestIdleZombieStrollsNearPlayers checks the zombie walks around without a
// target while a player is near.
func TestIdleZombieStrollsNearPlayers(t *testing.T) {
	w := aiTestWorld(t)
	addAITestPlayer(w, mgl64.Vec3{-14.5, 65, -14.5}, world.GameModeCreative)
	zombie := addZombie(w, mgl64.Vec3{.5, 65, .5})
	start := aiEntityPos(w, zombie)
	moved := 0.0
	for range 60 {
		stepWorld(t, w, 20)
		moved = max(moved, aiEntityPos(w, zombie).Sub(start).Len())
	}
	if pos := aiEntityPos(w, zombie); moved < 1 {
		t.Fatalf("idle zombie stayed at %v", pos)
	}
}

func addHostile(w *world.World, id string, pos mgl64.Vec3) *world.EntityHandle {
	var h *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		t, _ := entity.DefaultRegistry.Lookup(id)
		conf := tx.World().EntityRegistry().Config()
		h = conf.SpawnMob(t.EncodeEntity(), world.EntitySpawnOpts{Position: pos, SpawnReason: "natural", SpawnSeed: 3})
		tx.AddEntity(h)
	})
	return h
}

// TestSkeletonShootsPlayersInRange checks the skeleton draws its bow and
// shoots a player it sees within 15 blocks.
func TestSkeletonShootsPlayersInRange(t *testing.T) {
	w := aiTestWorld(t)
	p := addAITestPlayer(w, mgl64.Vec3{10.5, 65, .5}, world.GameModeSurvival)
	skeleton := addHostile(w, "minecraft:skeleton", mgl64.Vec3{.5, 65, .5})
	lowest, drew, arrows := 20.0, false, 0
	for range 200 {
		stepWorld(t, w, 1)
		<-w.Exec(func(tx *world.Tx) {
			e, _ := p.Entity(tx)
			lowest = min(lowest, e.(*player.Player).Health())
			s, _ := skeleton.Entity(tx)
			drew = drew || s.(interface{ UsingItem() bool }).UsingItem()
			for other := range tx.Entities() {
				if other.H().Type().EncodeEntity() == "minecraft:arrow" {
					arrows++
				}
			}
		})
	}
	if !drew || arrows == 0 || lowest == 20 {
		t.Fatalf("skeleton drew its bow %v, arrows seen %d, lowest player health %v", drew, arrows, lowest)
	}
}

// TestWitherSkeletonWithersInMelee checks the wither skeleton fights with
// its sword and withers what it hits.
func TestWitherSkeletonWithersInMelee(t *testing.T) {
	w := aiTestWorld(t)
	p := addAITestPlayer(w, mgl64.Vec3{4.5, 65, .5}, world.GameModeSurvival)
	addHostile(w, "minecraft:wither_skeleton", mgl64.Vec3{.5, 65, .5})
	withered := false
	for range 200 {
		stepWorld(t, w, 1)
		<-w.Exec(func(tx *world.Tx) {
			e, _ := p.Entity(tx)
			for _, eff := range e.(*player.Player).Effects() {
				withered = withered || eff.Type() == effect.Wither
			}
		})
	}
	if !withered {
		t.Fatal("wither skeleton did not wither the player")
	}
}

// TestSkeletonAvoidsWolves checks the skeleton walks away from a wolf near
// it.
func TestSkeletonAvoidsWolves(t *testing.T) {
	w := aiTestWorld(t)
	addAITestPlayer(w, mgl64.Vec3{-14.5, 65, -14.5}, world.GameModeCreative)
	skeleton := addHostile(w, "minecraft:skeleton", mgl64.Vec3{.5, 65, .5})
	wolfAt := mgl64.Vec3{3.5, 65, .5}
	<-w.Exec(func(tx *world.Tx) {
		tx.AddEntity(entity.NewWolf(world.EntitySpawnOpts{Position: wolfAt}, entity.WolfBehaviourConfig{}))
	})
	farthest := 0.0
	for range 200 {
		stepWorld(t, w, 1)
		farthest = max(farthest, aiEntityPos(w, skeleton).Sub(wolfAt).Len())
	}
	if farthest < 6 {
		t.Fatalf("skeleton got at most %.2f blocks from the wolf", farthest)
	}
}

func addSpecies(w *world.World, h *world.EntityHandle) *world.EntityHandle {
	<-w.Exec(func(tx *world.Tx) { tx.AddEntity(h) })
	return h
}

func lowestHealth(t *testing.T, w *world.World, p *world.EntityHandle, ticks int, each func(tx *world.Tx)) float64 {
	t.Helper()
	lowest := 20.0
	for range ticks {
		stepWorld(t, w, 1)
		<-w.Exec(func(tx *world.Tx) {
			if e, ok := p.Entity(tx); ok {
				lowest = min(lowest, e.(*player.Player).Health())
			}
			if each != nil {
				each(tx)
			}
		})
	}
	return lowest
}

// TestCreeperSwellsAndExplodesNearPlayers checks the creeper walks up to a
// player, swells with its fuse lit and explodes.
func TestCreeperSwellsAndExplodesNearPlayers(t *testing.T) {
	w := aiTestWorld(t)
	p := addAITestPlayer(w, mgl64.Vec3{6.5, 65, .5}, world.GameModeSurvival)
	creeper := addSpecies(w, entity.NewCreeper(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 65, .5}}))
	lit := false
	lowest := lowestHealth(t, w, p, 300, func(tx *world.Tx) {
		if e, ok := creeper.Entity(tx); ok {
			lit = lit || e.(interface{ Ignited() bool }).Ignited()
		}
	})
	if !lit || lowest == 20 || !creeper.Closed() {
		t.Fatalf("creeper lit its fuse %v, lowest player health %v, creeper gone %v", lit, lowest, creeper.Closed())
	}
}

// TestSpiderClimbsWallsAtNight checks the spider climbs a wall two blocks
// high to a player on top of it, too high to jump.
func TestSpiderClimbsWallsAtNight(t *testing.T) {
	w := aiTestWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		for x := 2; x <= 16; x++ {
			for z := -16; z <= 16; z++ {
				tx.SetBlock(cube.Pos{x, 65, z}, block.Stone{}, nil)
				tx.SetBlock(cube.Pos{x, 66, z}, block.Stone{}, nil)
			}
		}
	})
	p := addAITestPlayer(w, mgl64.Vec3{8.5, 67, .5}, world.GameModeSurvival)
	spider := addSpecies(w, entity.NewSpider(world.EntitySpawnOpts{Position: mgl64.Vec3{-6.5, 65, .5}}))
	highest := 0.0
	lowest := lowestHealth(t, w, p, 400, func(tx *world.Tx) {
		if e, ok := spider.Entity(tx); ok {
			highest = max(highest, e.Position()[1])
		}
	})
	if highest < 67 || lowest == 20 {
		t.Fatalf("spider climbed to y %.2f, lowest player health %v", highest, lowest)
	}
}

// TestSpidersLeavePlayersAloneInDaylight checks a spider in the light picks
// no target.
func TestSpidersLeavePlayersAloneInDaylight(t *testing.T) {
	w := aiTestWorld(t)
	w.SetTime(6000)
	p := addAITestPlayer(w, mgl64.Vec3{3.5, 65, .5}, world.GameModeSurvival)
	addSpecies(w, entity.NewSpider(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 65, .5}}))
	if lowest := lowestHealth(t, w, p, 200, nil); lowest != 20 {
		t.Fatalf("spider attacked in daylight, lowest player health %v", lowest)
	}
}

// TestCaveSpiderPoisons checks the cave spider poisons what it bites on
// normal difficulty.
func TestCaveSpiderPoisons(t *testing.T) {
	w := aiTestWorld(t)
	p := addAITestPlayer(w, mgl64.Vec3{3.5, 65, .5}, world.GameModeSurvival)
	addSpecies(w, entity.NewCaveSpider(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 65, .5}}))
	poisoned := false
	lowestHealth(t, w, p, 200, func(tx *world.Tx) {
		e, _ := p.Entity(tx)
		for _, eff := range e.(*player.Player).Effects() {
			poisoned = poisoned || eff.Type() == effect.Poison
		}
	})
	if !poisoned {
		t.Fatal("cave spider did not poison the player")
	}
}

// TestHurtSilverfishWakesInfestedBlocks checks a silverfish hurt by a player
// breaks the infested blocks around it, releasing more silverfish.
func TestHurtSilverfishWakesInfestedBlocks(t *testing.T) {
	w := aiTestWorld(t)
	// Planks around the infested blocks keep released silverfish from
	// hiding again straight away.
	<-w.Exec(func(tx *world.Tx) {
		for x := -4; x <= 4; x++ {
			for z := 3; z <= 5; z++ {
				tx.SetBlock(cube.Pos{x, 64, z}, block.Planks{}, nil)
			}
		}
		for x := -3; x <= 3; x++ {
			tx.SetBlock(cube.Pos{x, 64, 4}, block.InfestedStone{}, nil)
		}
	})
	p := addAITestPlayer(w, mgl64.Vec3{.5, 65, -3.5}, world.GameModeSurvival)
	fish := addSpecies(w, entity.NewSilverfish(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 65, .5}}))
	<-w.Exec(func(tx *world.Tx) {
		pl, _ := p.Entity(tx)
		e, _ := fish.Entity(tx)
		e.(entity.Living).Hurt(1, entity.AttackDamageSource{Attacker: pl.(*player.Player)})
	})
	stepWorld(t, w, 40)
	count := 0
	<-w.Exec(func(tx *world.Tx) {
		for e := range tx.Entities() {
			if e.H().Type() == entity.SilverfishType {
				count++
			}
		}
	})
	if count < 2 {
		t.Fatalf("hurt silverfish woke no friends, %d silverfish", count)
	}
}

// TestIdleSilverfishHidesInStone checks a silverfish with nothing to attack
// hides in the stone next to it.
func TestIdleSilverfishHidesInStone(t *testing.T) {
	w := aiTestWorld(t)
	fish := addSpecies(w, entity.NewSilverfish(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 65, .5}}))
	stepWorld(t, w, 600)
	infested := 0
	<-w.Exec(func(tx *world.Tx) {
		for x := -16; x <= 16; x++ {
			for z := -16; z <= 16; z++ {
				if _, ok := tx.Block(cube.Pos{x, 64, z}).(block.InfestedStone); ok {
					infested++
				}
			}
		}
	})
	if !fish.Closed() || infested != 1 {
		t.Fatalf("silverfish gone %v, infested blocks %d", fish.Closed(), infested)
	}
}
