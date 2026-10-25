package entity_test

import (
	"math/rand/v2"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// spawnLightRecorder records the light of the blocks mobs spawn in.
type spawnLightRecorder struct {
	world.NopHandler
	spawns []spawnLight
}

type spawnLight struct {
	id         string
	pos        cube.Pos
	block, sky uint8
	below      world.Block
}

func (r *spawnLightRecorder) HandleEntitySpawn(tx *world.Tx, e world.Entity) {
	if _, living := e.(entity.Living); !living || e.H().Type().EncodeEntity() == "minecraft:player" {
		return
	}
	pos := cube.PosFromVec3(e.Position())
	r.spawns = append(r.spawns, spawnLight{id: e.H().Type().EncodeEntity(), pos: pos, block: tx.BlockLight(pos), sky: tx.SkyLight(pos), below: tx.Block(pos.Side(cube.FaceDown))})
}

// TestLitGroundKeepsMonstersAway checks Monster.isDarkEnoughToSpawn with the
// Overworld's block light limit of 0: at night monsters spawn only where no
// block light reaches, so glowstone set into the floor keeps them off the
// ground it lights, as torches do.
func TestLitGroundKeepsMonstersAway(t *testing.T) {
	w, _ := naturalSpawnWorld(t, 18000)
	<-w.Exec(func(tx *world.Tx) {
		for x := -56; x < 56; x += 6 {
			for z := -56; z < 56; z += 6 {
				if x < 0 {
					tx.SetBlock(cube.Pos{x, 63, z}, block.Glowstone{}, nil)
				}
			}
		}
	})
	rec := &spawnLightRecorder{}
	w.Handle(rec)
	for range 400 {
		w.AdvanceTick()
	}
	lit, dark := 0, 0
	for _, s := range rec.spawns {
		if s.block > 0 {
			t.Errorf("%s spawned at %v in block light %d", s.id, s.pos, s.block)
		}
		if s.pos[0] < 0 {
			lit++
		} else {
			dark++
		}
	}
	if dark == 0 {
		t.Fatal("no monster spawned on the dark half of the floor")
	}
	if lit != 0 {
		t.Fatalf("%d monsters spawned on the half lit by glowstone", lit)
	}
}

// TestAnimalsSpawnOnLitGrass checks the CREATURE category, which spawns once
// every 400 ticks: animals spawn by day on grass, in light above 8, by the
// rules of Animal.checkAnimalSpawnRules.
func TestAnimalsSpawnOnLitGrass(t *testing.T) {
	w := world.Config{Entities: entity.DefaultRegistry, Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	w.SetDifficulty(world.DifficultyNormal)
	w.SetTime(6000)
	w.StopTime()
	<-w.Exec(func(tx *world.Tx) {
		plains, _ := world.BiomeByName("plains")
		for x := -56; x < 56; x++ {
			for z := -56; z < 56; z++ {
				tx.SetBlock(cube.Pos{x, 63, z}, block.Grass{}, nil)
				tx.SetBiome(cube.Pos{x, 64, z}, plains)
			}
		}
		addSurvivalPlayer(tx, mgl64.Vec3{.5, 64, .5})
	})
	rec := &spawnLightRecorder{}
	w.Handle(rec)
	for range 1201 {
		w.AdvanceTick()
	}
	if len(rec.spawns) == 0 {
		t.Fatal("no animal spawned on plains grass by day in three spawn cycles")
	}
	for _, s := range rec.spawns {
		if _, grass := s.below.(block.Grass); !grass {
			t.Errorf("%s spawned at %v on %T, want grass", s.id, s.pos, s.below)
		}
		if max(s.block, s.sky) <= 8 {
			t.Errorf("%s spawned at %v in light %d", s.id, s.pos, max(s.block, s.sky))
		}
	}
}

// TestPeacefulNetherStillSpawnsPiglins checks that the monster category
// spawns on peaceful, as ServerChunkCache.spawnEnemies only follows the
// spawn_monsters rule: the spawn rules of zombified piglins, ghasts, magma
// cubes and endermen turn them away on peaceful, but piglins have none.
func TestPeacefulNetherStillSpawnsPiglins(t *testing.T) {
	w := world.Config{Dim: world.Nether, Entities: entity.DefaultRegistry, Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	w.SetDifficulty(world.DifficultyPeaceful)
	<-w.Exec(func(tx *world.Tx) {
		wastes, _ := world.BiomeByName("hell")
		for x := -56; x < 56; x++ {
			for z := -56; z < 56; z++ {
				tx.SetBlock(cube.Pos{x, 63, z}, block.Netherrack{}, nil)
				tx.SetBiome(cube.Pos{x, 64, z}, wastes)
			}
		}
		addSurvivalPlayer(tx, mgl64.Vec3{.5, 64, .5})
	})
	rec := &spawnLightRecorder{}
	w.Handle(rec)
	for range 600 {
		w.AdvanceTick()
	}
	piglins := 0
	for _, s := range rec.spawns {
		switch s.id {
		case "minecraft:piglin":
			piglins++
		default:
			t.Errorf("%s spawned at %v on peaceful", s.id, s.pos)
		}
	}
	if piglins == 0 {
		t.Fatal("no piglin spawned in the nether wastes on peaceful")
	}
}

// TestWaterMobsSpawnInTheSea checks the water categories: in an ocean, water
// mobs spawn only in water, fish and squids within 13 blocks below the sea
// level of 63, nautili between 25 and 5 below it, and glow squids deeper
// than 33 below it.
func TestWaterMobsSpawnInTheSea(t *testing.T) {
	w := world.Config{Entities: entity.DefaultRegistry, Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	w.SetDifficulty(world.DifficultyNormal)
	w.SetTime(6000)
	w.StopTime()
	<-w.Exec(func(tx *world.Tx) {
		ocean, _ := world.BiomeByName("ocean")
		for x := -45; x <= 44; x++ {
			for z := -45; z <= 44; z++ {
				tx.SetBlock(cube.Pos{x, 35, z}, block.Stone{}, nil)
				for y := 36; y <= 62; y++ {
					if x == -45 || x == 44 || z == -45 || z == 44 {
						// A wall that keeps the sea from running off.
						tx.SetBlock(cube.Pos{x, y, z}, block.Stone{}, nil)
						continue
					}
					tx.SetBlock(cube.Pos{x, y, z}, block.Water{Depth: 8, Still: true}, nil)
				}
				for y := 36; y <= 64; y += 4 {
					tx.SetBiome(cube.Pos{x, y, z}, ocean)
				}
			}
		}
		addSurvivalPlayer(tx, mgl64.Vec3{.5, 63, .5})
	})
	rec := &spawnLightRecorder{}
	w.Handle(rec)
	for range 400 {
		w.AdvanceTick()
	}
	if len(rec.spawns) == 0 {
		t.Fatal("no water mob spawned in an ocean in 20 seconds")
	}
	<-w.Exec(func(tx *world.Tx) {
		for _, s := range rec.spawns {
			if _, water := tx.Liquid(s.pos); !water {
				t.Errorf("%s spawned at %v out of water", s.id, s.pos)
			}
			// The depths the spawn rules of each kind allow.
			low, high := 63-13, 63
			switch s.id {
			case "minecraft:nautilus":
				low, high = 63-25, 63-5
			case "minecraft:glow_squid":
				low, high = -64, 63-33
			case "minecraft:drowned":
				low = -64
			}
			if s.pos[1] < low || s.pos[1] > high {
				t.Errorf("%s spawned at %v, outside y %d to %d", s.id, s.pos, low, high)
			}
		}
	})
}

// TestMonsterPopulationStaysCappedAndLeaves runs two minutes of night: the
// monsters around a player never outgrow the monster cap of 70 by more than
// a group, and once the player is gone far away every monster that may
// despawn does so, as Mob.checkDespawn removes mobs beyond 128 blocks.
func TestMonsterPopulationStaysCappedAndLeaves(t *testing.T) {
	w, p := naturalSpawnWorld(t, 18000)
	countMonsters := func(tx *world.Tx) (n int) {
		for e := range tx.Entities() {
			if _, ok := e.(entity.Living); ok && e.H().Type().EncodeEntity() != "minecraft:player" {
				n++
			}
		}
		return n
	}
	peak := 0
	for range 2400 {
		w.AdvanceTick()
		<-w.Exec(func(tx *world.Tx) { peak = max(peak, countMonsters(tx)) })
	}
	if peak == 0 {
		t.Fatal("no monster spawned in two minutes of night")
	}
	// The cap is checked before each group of up to four, and chicken
	// jockeys bring their chickens.
	if peak > 80 {
		t.Fatalf("%d mobs lived around one player at once, above the monster cap of 70", peak)
	}
	<-w.Exec(func(tx *world.Tx) {
		e, _ := p.Entity(tx)
		e.(interface{ Teleport(mgl64.Vec3) }).Teleport(mgl64.Vec3{1000.5, 64, .5})
	})
	for range 5 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		if n := countMonsters(tx); n != 0 {
			t.Fatalf("%d monsters stayed after the player went 1000 blocks away", n)
		}
	})
}

// TestNetherPortalsLetZombifiedPiglinsThrough checks NetherPortalBlock's
// random tick: in the Overworld near a player, a portal now and then lets a
// zombified piglin through onto the obsidian beneath it.
func TestNetherPortalsLetZombifiedPiglinsThrough(t *testing.T) {
	w := world.Config{Entities: entity.DefaultRegistry, Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	w.SetDifficulty(world.DifficultyNormal)
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{0, 63, 0}, block.Obsidian{}, nil)
		tx.SetBlock(cube.Pos{0, 64, 0}, block.Portal{Axis: cube.X}, nil)
		tx.SetBlock(cube.Pos{0, 65, 0}, block.Portal{Axis: cube.X}, nil)
		addSurvivalPlayer(tx, mgl64.Vec3{20.5, 64, .5})
		r := rand.New(rand.NewPCG(1, 2))
		for range 20000 {
			block.Portal{Axis: cube.X}.RandomTick(cube.Pos{0, 65, 0}, tx, r)
		}
		piglins := 0
		for e := range tx.Entities() {
			if e.H().Type().EncodeEntity() == "minecraft:zombie_pigman" || e.H().Type().EncodeEntity() == "minecraft:zombified_piglin" {
				piglins++
				if y := e.Position()[1]; y != 64 {
					t.Errorf("zombified piglin came through at y %v, want on the obsidian at 64", y)
				}
			}
		}
		if piglins == 0 {
			t.Fatal("no zombified piglin came through a portal in 20000 random ticks")
		}
	})
}
