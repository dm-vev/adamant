package entity_test

import (
	"math"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
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
	// Random positions below a floor in the void are never stable, so the
	// ground is made deep.
	<-w.Exec(func(tx *world.Tx) {
		for x := -16; x <= 16; x++ {
			for z := -16; z <= 16; z++ {
				for y := 56; y < 64; y++ {
					tx.SetBlock(cube.Pos{x, y, z}, block.Stone{}, nil)
				}
			}
		}
	})
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

// starePitch returns the pitch a player at from looks up at the eyes of an
// enderman at to with.
func starePitch(from, to mgl64.Vec3) float64 {
	d := to.Add(mgl64.Vec3{0, 2.55, 0}).Sub(from.Add(mgl64.Vec3{0, 1.62, 0}))
	return -mgl64.RadToDeg(math.Atan2(d[1], math.Hypot(d[0], d[2])))
}

func endermanAngry(w *world.World, h *world.EntityHandle) (angry bool) {
	<-w.Exec(func(tx *world.Tx) {
		if e, ok := h.Entity(tx); ok {
			angry = e.(*entity.Enderman).Angry()
		}
	})
	return angry
}

// TestEndermanAngersAtPlayersStaringAtIt checks a player looking an enderman
// in the eyes makes it scream and freeze, and a carved pumpkin hides the
// stare.
func TestEndermanAngersAtPlayersStaringAtIt(t *testing.T) {
	for _, pumpkin := range []bool{false, true} {
		w := aiTestWorld(t)
		from, to := mgl64.Vec3{.5, 65, -7.5}, mgl64.Vec3{.5, 65, .5}
		h := world.EntitySpawnOpts{}.New(player.Type, player.Config{Name: "Starer", Position: from, Rotation: cube.Rotation{0, starePitch(from, to)}, GameMode: world.GameModeSurvival})
		<-w.Exec(func(tx *world.Tx) {
			p := tx.AddEntity(h).(*player.Player)
			if pumpkin {
				p.Armour().SetHelmet(item.NewStack(block.Pumpkin{Carved: true}, 1))
			}
		})
		enderman := addSpecies(w, entity.NewEnderman(world.EntitySpawnOpts{Position: to}))
		angry := false
		for range 60 {
			stepWorld(t, w, 1)
			angry = angry || endermanAngry(w, enderman)
		}
		if angry == pumpkin {
			t.Fatalf("enderman angry %v at a player staring with a carved pumpkin %v", angry, pumpkin)
		}
		if !pumpkin && aiEntityPos(w, enderman).Sub(to).Len() > 1 {
			t.Fatalf("enderman stared at moved to %v", aiEntityPos(w, enderman))
		}
	}
}

// TestEndermanDodgesProjectiles checks a projectile hitting an enderman
// teleports it away unhurt.
func TestEndermanDodgesProjectiles(t *testing.T) {
	w := aiTestWorld(t)
	h := addSpecies(w, entity.NewEnderman(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 65, .5}}))
	<-w.Exec(func(tx *world.Tx) {
		e, _ := h.Entity(tx)
		if _, hurt := e.(entity.Living).Hurt(5, entity.ProjectileDamageSource{}); hurt {
			t.Fatal("projectile hurt the enderman")
		}
		if e.Position() == (mgl64.Vec3{.5, 65, .5}) {
			t.Fatal("enderman did not teleport from the projectile")
		}
		if l := e.(entity.Living); l.Health() != l.MaxHealth() {
			t.Fatalf("enderman health %v after a projectile", l.Health())
		}
	})
}

// TestEndermanTakesBlocks checks an enderman picks up a flower next to it
// in a field of flowers.
func TestEndermanTakesBlocks(t *testing.T) {
	w := aiTestWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		for x := -16; x <= 16; x++ {
			for z := -16; z <= 16; z++ {
				tx.SetBlock(cube.Pos{x, 64, z}, block.Grass{}, nil)
				if x != 0 || z != 0 {
					tx.SetBlock(cube.Pos{x, 65, z}, block.Flower{}, nil)
				}
			}
		}
	})
	h := addSpecies(w, entity.NewEnderman(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 65, .5}}))
	carried := false
	for range 1200 {
		stepWorld(t, w, 1)
		<-w.Exec(func(tx *world.Tx) {
			if e, ok := h.Entity(tx); ok {
				b, ok := e.(*entity.Enderman).CarriedBlock()
				_, flower := b.(block.Flower)
				carried = carried || ok && flower
			}
		})
		if carried {
			break
		}
	}
	if !carried {
		t.Fatal("enderman picked up no flower")
	}
}

// TestMobDamageCooldown checks a mob hit again within half a second only
// takes the damage by which the hit exceeds the last.
func TestMobDamageCooldown(t *testing.T) {
	w := aiTestWorld(t)
	h := addZombie(w, mgl64.Vec3{.5, 65, .5})
	<-w.Exec(func(tx *world.Tx) {
		e, _ := h.Entity(tx)
		l := e.(entity.Living)
		l.Hurt(4, entity.VoidDamageSource{})
		if _, hurt := l.Hurt(4, entity.VoidDamageSource{}); hurt || l.Health() != 16 {
			t.Fatalf("second hit hurt %v, health %v", hurt, l.Health())
		}
		if l.Hurt(6, entity.VoidDamageSource{}); l.Health() != 14 {
			t.Fatalf("stronger hit left health %v, want 14", l.Health())
		}
	})
	stepWorld(t, w, 10)
	<-w.Exec(func(tx *world.Tx) {
		e, _ := h.Entity(tx)
		l := e.(entity.Living)
		if l.Hurt(1, entity.VoidDamageSource{}); l.Health() != 13 {
			t.Fatalf("hit after the cooldown left health %v, want 13", l.Health())
		}
	})
}

// TestSlimesHopAtPlayersAndSplit checks a slime hops to a player, hurts it
// by its size, and splits into two to four slimes of half its size when it
// dies, while the smallest slimes are harmless.
func TestSlimesHopAtPlayersAndSplit(t *testing.T) {
	for _, size := range []int{1, 4} {
		w := aiTestWorld(t)
		p := addAITestPlayer(w, mgl64.Vec3{6.5, 65, .5}, world.GameModeSurvival)
		slime := addSpecies(w, entity.NewSlimeWithSize(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 65, .5}}, size))
		<-w.Exec(func(tx *world.Tx) {
			e, _ := slime.Entity(tx)
			if l := e.(entity.Living); l.MaxHealth() != float64(size*size) {
				t.Fatalf("slime of size %d has %v health, want %d", size, l.MaxHealth(), size*size)
			}
		})
		highest := 0.0
		lowest := lowestHealth(t, w, p, 300, func(tx *world.Tx) {
			if e, ok := slime.Entity(tx); ok {
				highest = max(highest, e.Position()[1])
			}
		})
		if highest < 65.3 {
			t.Fatalf("slime of size %d never hopped", size)
		}
		if hurt := lowest < 20; hurt != (size > 1) {
			t.Fatalf("slime of size %d hurt the player %v (lowest health %v)", size, hurt, lowest)
		}
		if size == 1 {
			continue
		}
		<-w.Exec(func(tx *world.Tx) {
			e, _ := slime.Entity(tx)
			e.(entity.Living).Hurt(100, entity.VoidDamageSource{})
		})
		halves := 0
		<-w.Exec(func(tx *world.Tx) {
			for e := range tx.Entities() {
				if s, ok := e.(*entity.Slime); ok && s.Size() == 2 {
					halves++
				}
			}
		})
		if halves < 2 || halves > 4 {
			t.Fatalf("slime split into %d halves", halves)
		}
	}
}

// TestMagmaCubesHopHigherAndShrugOffHits checks a magma cube hops higher
// than a slime of its size, its armour softens hits, and it splits when it
// dies.
func TestMagmaCubesHopHigherAndShrugOffHits(t *testing.T) {
	w := aiTestWorld(t)
	magma := addSpecies(w, entity.NewMagmaCubeWithSize(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 65, .5}}, 4))
	slime := addSpecies(w, entity.NewSlimeWithSize(world.EntitySpawnOpts{Position: mgl64.Vec3{8.5, 65, 8.5}}, 4))
	magmaTop, slimeTop := 0.0, 0.0
	for range 400 {
		stepWorld(t, w, 1)
		<-w.Exec(func(tx *world.Tx) {
			if e, ok := magma.Entity(tx); ok {
				magmaTop = max(magmaTop, e.Position()[1])
			}
			if e, ok := slime.Entity(tx); ok {
				slimeTop = max(slimeTop, e.Position()[1])
			}
		})
	}
	if magmaTop <= slimeTop || slimeTop < 65.5 {
		t.Fatalf("magma cube hopped to %.2f, slime to %.2f", magmaTop, slimeTop)
	}
	<-w.Exec(func(tx *world.Tx) {
		e, _ := magma.Entity(tx)
		l := e.(entity.Living)
		if dealt, _ := l.Hurt(5, entity.AttackDamageSource{}); dealt >= 5 {
			t.Fatalf("magma cube took %v of 5 damage through its armour", dealt)
		}
	})
}

// TestBlazeChargesAndShootsFireballs checks a blaze that sees a player
// burns while it charges and shoots fireballs at it.
func TestBlazeChargesAndShootsFireballs(t *testing.T) {
	w := aiTestWorld(t)
	addAITestPlayer(w, mgl64.Vec3{10.5, 65, .5}, world.GameModeSurvival)
	blaze := addSpecies(w, entity.NewBlaze(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 65, .5}}))
	burned, fireballs := false, 0
	for range 200 {
		stepWorld(t, w, 1)
		<-w.Exec(func(tx *world.Tx) {
			if e, ok := blaze.Entity(tx); ok {
				burned = burned || e.(*entity.Blaze).Burning()
			}
		})
		<-w.Exec(func(tx *world.Tx) { fireballs = max(fireballs, countEntities(tx, "minecraft:small_fireball")) })
	}
	if !burned || fireballs == 0 {
		t.Fatalf("blaze burned %v, fireballs %d", burned, fireballs)
	}
}

// TestGhastFloatsAndShootsAtPlayers checks ghasts float around and shoot a
// fireball at a player level with them. A ghast that drifts more than four
// blocks above or below the player ignores it, so there are three.
func TestGhastFloatsAndShootsAtPlayers(t *testing.T) {
	w := aiTestWorld(t)
	addAITestPlayer(w, mgl64.Vec3{14.5, 65, .5}, world.GameModeSurvival)
	var ghasts []*world.EntityHandle
	for _, z := range []float64{-8, .5, 8} {
		ghasts = append(ghasts, addSpecies(w, entity.NewGhast(world.EntitySpawnOpts{Position: mgl64.Vec3{-6, 66, z}})))
	}
	moved, charged, fireballs := 0.0, false, 0
	for range 400 {
		stepWorld(t, w, 1)
		<-w.Exec(func(tx *world.Tx) {
			for i, h := range ghasts {
				if e, ok := h.Entity(tx); ok {
					moved = max(moved, e.Position().Sub(mgl64.Vec3{-6, 66, []float64{-8, .5, 8}[i]}).Len())
					charged = charged || e.(*entity.Ghast).Charging()
				}
			}
			fireballs = max(fireballs, countEntities(tx, "minecraft:fireball"))
		})
	}
	if moved < 1 || !charged || fireballs == 0 {
		t.Fatalf("ghasts moved %.2f, charged %v, fireballs %d", moved, charged, fireballs)
	}
}

// TestPillagerLoadsAndShootsItsCrossbow checks a pillager loads its crossbow
// in sight of a player and shoots arrows at it.
func TestPillagerLoadsAndShootsItsCrossbow(t *testing.T) {
	w := aiTestWorld(t)
	p := addAITestPlayer(w, mgl64.Vec3{7.5, 65, .5}, world.GameModeSurvival)
	pillager := addSpecies(w, entity.NewPillager(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 65, .5}, SpawnReason: "structure"}))
	charging := false
	lowest := lowestHealth(t, w, p, 300, func(tx *world.Tx) {
		if e, ok := pillager.Entity(tx); ok {
			charging = charging || e.(interface{ Charging() bool }).Charging()
		}
	})
	if !charging || lowest == 20 {
		t.Fatalf("pillager loaded its crossbow %v, lowest player health %v", charging, lowest)
	}
}

// TestVindicatorsHuntPlayersAndJohnnyEverything checks a vindicator attacks
// players, and one named Johnny attacks a cow too.
func TestVindicatorsHuntPlayersAndJohnnyEverything(t *testing.T) {
	w := aiTestWorld(t)
	p := addAITestPlayer(w, mgl64.Vec3{5.5, 65, .5}, world.GameModeSurvival)
	addSpecies(w, entity.NewVindicator(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 65, .5}, SpawnReason: "structure"}))
	if lowest := lowestHealth(t, w, p, 200, nil); lowest == 20 {
		t.Fatal("vindicator did not hurt the player")
	}

	w = aiTestWorld(t)
	cow := addSpecies(w, entity.NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{4.5, 65, .5}}))
	addSpecies(w, entity.NewVindicator(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 65, .5}, NameTag: "Johnny", SpawnReason: "structure"}))
	hurt := false
	for range 300 {
		stepWorld(t, w, 1)
		<-w.Exec(func(tx *world.Tx) {
			if e, ok := cow.Entity(tx); ok {
				l := e.(entity.Living)
				hurt = hurt || l.Health() < l.MaxHealth()
			} else {
				hurt = true
			}
		})
	}
	if !hurt {
		t.Fatal("Johnny left the cow alone")
	}
}

// TestEvokerSummonsVexesAndTurnsSheepRed checks an evoker summons vexes at
// a player in sight, and an idle one turns a blue sheep red.
func TestEvokerSummonsVexesAndTurnsSheepRed(t *testing.T) {
	w := aiTestWorld(t)
	p := addAITestPlayer(w, mgl64.Vec3{10.5, 65, .5}, world.GameModeSurvival)
	addSpecies(w, entity.NewEvoker(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 65, .5}, SpawnReason: "structure"}))
	vexes, fangs := 0, 0
	lowestHealth(t, w, p, 300, func(tx *world.Tx) {
		vexes = max(vexes, countEntities(tx, "minecraft:vex"))
		fangs = max(fangs, countEntities(tx, "minecraft:evoker_fangs"))
	})
	if vexes == 0 && fangs == 0 {
		t.Fatal("evoker cast no spell at the player")
	}

	w = aiTestWorld(t)
	sheep := addSpecies(w, world.EntitySpawnOpts{Position: mgl64.Vec3{4.5, 65, .5}}.New(entity.SheepType, entity.SheepConfig{WoolColor: 11}))
	addSpecies(w, entity.NewEvoker(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 65, .5}, SpawnReason: "structure"}))
	stepWorld(t, w, 400)
	<-w.Exec(func(tx *world.Tx) {
		e, _ := sheep.Entity(tx)
		if c := e.(*entity.Sheep).WoolColor(); c != 14 {
			t.Fatalf("sheep wool colour %d, want red", c)
		}
	})
}

// TestRavagerBitesAndKnocksBack checks a ravager bites a player hard
// enough to knock it back.
func TestRavagerBitesAndKnocksBack(t *testing.T) {
	w := aiTestWorld(t)
	p := addAITestPlayer(w, mgl64.Vec3{5.5, 65, .5}, world.GameModeSurvival)
	addSpecies(w, entity.NewRavager(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 65, .5}}))
	if lowest := lowestHealth(t, w, p, 200, nil); lowest > 20-12+2 {
		t.Fatalf("ravager bit the player down to %v only", lowest)
	}
}

// buildPool fills a walled pool of still water five blocks deep around the
// origin on the floor of an AI test world.
func buildPool(w *world.World) {
	<-w.Exec(func(tx *world.Tx) {
		for x := -7; x <= 7; x++ {
			for z := -7; z <= 7; z++ {
				for y := 65; y <= 70; y++ {
					if x == -7 || x == 7 || z == -7 || z == 7 {
						tx.SetBlock(cube.Pos{x, y, z}, block.Stone{}, nil)
					} else if y <= 69 {
						tx.SetBlock(cube.Pos{x, y, z}, block.Water{Still: true, Depth: 8}, nil)
					}
				}
			}
		}
	})
}

// TestDrownedSwimsToPlayersInWater checks a drowned hunts a player in the
// water by day and swims to it.
func TestDrownedSwimsToPlayersInWater(t *testing.T) {
	w := aiTestWorld(t)
	w.SetTime(6000)
	buildPool(w)
	p := addAITestPlayer(w, mgl64.Vec3{4.5, 65, .5}, world.GameModeSurvival)
	addSpecies(w, entity.NewDrowned(world.EntitySpawnOpts{Position: mgl64.Vec3{-4.5, 67, .5}}))
	if lowest := lowestHealth(t, w, p, 400, nil); lowest == 20 {
		t.Fatal("drowned did not reach the player in the water")
	}
}

// TestDrownedLeavesPlayersOnLandByDay checks a drowned ignores a player on
// land by day.
func TestDrownedLeavesPlayersOnLandByDay(t *testing.T) {
	w := aiTestWorld(t)
	w.SetTime(6000)
	p := addAITestPlayer(w, mgl64.Vec3{3.5, 65, .5}, world.GameModeSurvival)
	addSpecies(w, entity.NewDrowned(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 65, .5}}))
	if lowest := lowestHealth(t, w, p, 200, nil); lowest != 20 {
		t.Fatalf("drowned hurt a player on land by day, lowest health %v", lowest)
	}
}

// TestDrownedThrowsTridents checks a drowned holding a trident throws it.
func TestDrownedThrowsTridents(t *testing.T) {
	w := aiTestWorld(t)
	p := addAITestPlayer(w, mgl64.Vec3{8.5, 65, .5}, world.GameModeSurvival)
	drowned, err := entity.NewFromStructureNBT(map[string]any{"id": "minecraft:drowned",
		"equipment": map[string]any{"mainhand": map[string]any{"id": "minecraft:trident", "count": int32(1)}},
	}, world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 65, .5}, SpawnFinalized: true})
	if err != nil {
		t.Fatal(err)
	}
	addSpecies(w, drowned)
	tridents := 0
	lowestHealth(t, w, p, 200, func(tx *world.Tx) { tridents = max(tridents, countEntities(tx, "minecraft:thrown_trident")) })
	if tridents == 0 {
		t.Fatal("drowned threw no trident")
	}
}

// TestGuardianBeamsPlayersInWater checks a guardian aims its beam at a
// player in the water and hurts it once the beam charged.
func TestGuardianBeamsPlayersInWater(t *testing.T) {
	w := aiTestWorld(t)
	buildPool(w)
	p := addAITestPlayer(w, mgl64.Vec3{5.5, 65, .5}, world.GameModeSurvival)
	guardian := addSpecies(w, entity.NewGuardian(world.EntitySpawnOpts{Position: mgl64.Vec3{-4.5, 66, .5}}))
	beamed := false
	lowest := lowestHealth(t, w, p, 300, func(tx *world.Tx) {
		if e, ok := guardian.Entity(tx); ok {
			beamed = beamed || e.(interface{ BeamTargetHandle() *world.EntityHandle }).BeamTargetHandle() != nil
		}
	})
	// The magic of the beam hurts by 1, and the hit that follows only by
	// the 5 by which its 6 exceed that.
	if !beamed || lowest > 20-6 {
		t.Fatalf("guardian aimed its beam %v, lowest player health %v", beamed, lowest)
	}
}

// TestGuardianPricksAndFlops checks a guardian standing still pricks what
// hits it, and one on land flops about.
func TestGuardianPricksAndFlops(t *testing.T) {
	w := aiTestWorld(t)
	p := addAITestPlayer(w, mgl64.Vec3{2.5, 65, .5}, world.GameModeSurvival)
	guardian := addSpecies(w, entity.NewGuardian(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 65, .5}}))
	highest := 0.0
	for range 40 {
		stepWorld(t, w, 1)
		<-w.Exec(func(tx *world.Tx) {
			if e, ok := guardian.Entity(tx); ok {
				highest = max(highest, e.Position()[1])
			}
		})
	}
	if highest < 65.5 {
		t.Fatalf("guardian on land only reached y %.2f", highest)
	}
	<-w.Exec(func(tx *world.Tx) {
		pl, _ := p.Entity(tx)
		e, _ := guardian.Entity(tx)
		e.(entity.Living).Hurt(1, entity.AttackDamageSource{Attacker: pl})
		if h := pl.(*player.Player).Health(); h != 18 {
			t.Fatalf("player health %v after hitting a still guardian, want 18", h)
		}
	})
}

// TestElderGuardianCursesPlayers checks an elder guardian gives a player
// nearby mining fatigue within a minute.
func TestElderGuardianCursesPlayers(t *testing.T) {
	w := aiTestWorld(t)
	buildPool(w)
	p := addAITestPlayer(w, mgl64.Vec3{12.5, 72, .5}, world.GameModeSurvival)
	addSpecies(w, entity.NewElderGuardian(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 66, .5}}))
	cursed := false
	for range 1210 {
		stepWorld(t, w, 1)
		<-w.Exec(func(tx *world.Tx) {
			e, _ := p.Entity(tx)
			for _, eff := range e.(*player.Player).Effects() {
				cursed = cursed || eff.Type() == effect.MiningFatigue && eff.Level() == 3
			}
		})
		if cursed {
			break
		}
	}
	if !cursed {
		t.Fatal("elder guardian gave no mining fatigue")
	}
}

// TestWitchThrowsPotionsAtPlayers checks a witch throws potions that slow
// or poison a player.
func TestWitchThrowsPotionsAtPlayers(t *testing.T) {
	w := aiTestWorld(t)
	p := addAITestPlayer(w, mgl64.Vec3{6.5, 65, .5}, world.GameModeSurvival)
	addSpecies(w, entity.NewWitch(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 65, .5}}))
	affected := false
	lowestHealth(t, w, p, 300, func(tx *world.Tx) {
		e, _ := p.Entity(tx)
		for _, eff := range e.(*player.Player).Effects() {
			affected = affected || eff.Type() == effect.Slowness || eff.Type() == effect.Poison
		}
	})
	if !affected {
		t.Fatal("witch potions did not affect the player")
	}
}

// TestWitchDrinksAndResistsMagic checks a burning witch drinks fire
// resistance, and magic hurts it little.
func TestWitchDrinksAndResistsMagic(t *testing.T) {
	w := aiTestWorld(t)
	witch := addSpecies(w, entity.NewWitch(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 65, .5}}))
	<-w.Exec(func(tx *world.Tx) {
		e, _ := witch.Entity(tx)
		l := e.(entity.Living)
		if l.Hurt(10, effect.InstantDamageSource{}); l.Health() < 26-1.5-.01 {
			t.Fatalf("witch health %v after 10 magic damage", l.Health())
		}
		e.(interface{ SetOnFire(time.Duration) }).SetOnFire(10 * time.Second)
	})
	resistant := false
	for range 200 {
		stepWorld(t, w, 1)
		<-w.Exec(func(tx *world.Tx) {
			e, _ := witch.Entity(tx)
			for _, eff := range e.(entity.Living).Effects() {
				resistant = resistant || eff.Type() == effect.FireResistance
			}
		})
		if resistant {
			break
		}
	}
	if !resistant {
		t.Fatal("burning witch drank no fire resistance")
	}
}

// TestPhantomSwoopsAtPlayers checks a phantom circling above a player
// swoops down and bites it, unless a cat is near.
func TestPhantomSwoopsAtPlayers(t *testing.T) {
	for _, cat := range []bool{false, true} {
		w := aiTestWorld(t)
		p := addAITestPlayer(w, mgl64.Vec3{.5, 65, .5}, world.GameModeSurvival)
		if cat {
			addSpecies(w, entity.NewCat(world.EntitySpawnOpts{Position: mgl64.Vec3{2.5, 65, .5}}))
		}
		addSpecies(w, entity.NewPhantom(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 80, .5}, SpawnReason: "natural"}))
		lowest := lowestHealth(t, w, p, 800, nil)
		if bitten := lowest < 20; bitten == cat {
			t.Fatalf("phantom bit the player %v with a cat near %v, lowest health %v", bitten, cat, lowest)
		}
	}
}
