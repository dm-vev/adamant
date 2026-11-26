package entity_test

import (
	"testing"

	"github.com/dm-vev/adamant/server/block"
	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/entity"
	"github.com/dm-vev/adamant/server/item"
	"github.com/dm-vev/adamant/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestMobsDespawnFarFromPlayers checks Mob.checkDespawn: a monster more than
// 128 blocks from every player leaves the world, unless a name tag made it
// persistent, and an animal stays however far it is.
func TestMobsDespawnFarFromPlayers(t *testing.T) {
	t.Parallel()
	w := happyGhastWorld(t)
	w.SetDifficulty(world.DifficultyNormal)
	var plain, named, cow *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		plain = spawnAnimal(t, tx, "minecraft:husk", .5, nil)
		named = spawnAnimal(t, tx, "minecraft:husk", 4.5, nil)
		cow = spawnAnimal(t, tx, "minecraft:cow", 8.5, nil)
		p := addSurvivalPlayer(tx, mgl64.Vec3{.5, 64, 140.5})
		tx.SetBlock(cube.Pos{0, 63, 140}, block.Stone{}, nil)
		tag := item.NewStack(item.NameTag{}, 1).WithCustomName("Husky")
		p.SetHeldItems(tag, item.Stack{})
		e, _ := named.Entity(tx)
		if !(item.NameTag{}).UseOnEntity(e, tx, p, &item.UseContext{}) {
			t.Fatal("name tag did not name the husk")
		}
	})
	for range 3 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		if _, ok := plain.Entity(tx); ok {
			t.Fatal("husk 140 blocks from the only player did not despawn")
		}
		e, ok := named.Entity(tx)
		if !ok {
			t.Fatal("named husk despawned")
		}
		if e.(*entity.Husk).NameTag() != "Husky" {
			t.Fatalf("named husk is called %q, want Husky", e.(*entity.Husk).NameTag())
		}
		if _, ok := cow.Entity(tx); !ok {
			t.Fatal("cow far from players despawned")
		}
	})
}

func TestMobDespawnEdgeCases(t *testing.T) {
	t.Parallel()
	// 1. Only a spectator player in the world: far hostile mob must NOT despawn.
	wSpec := happyGhastWorld(t)
	wSpec.SetDifficulty(world.DifficultyNormal)
	var huskSpec *world.EntityHandle
	<-wSpec.Exec(func(tx *world.Tx) {
		huskSpec = spawnAnimal(t, tx, "minecraft:husk", .5, nil)
		p := addSurvivalPlayer(tx, mgl64.Vec3{.5, 64, 150.5})
		p.SetGameMode(world.GameModeSpectator)
		tx.SetBlock(cube.Pos{0, 63, 150}, block.Stone{}, nil)
	})
	for range 3 {
		wSpec.AdvanceTick()
	}
	<-wSpec.Exec(func(tx *world.Tx) {
		if _, ok := huskSpec.Entity(tx); !ok {
			t.Fatal("husk despawned when only spectator player existed")
		}
	})

	// 2. Spectator close (10 blocks) + survival far (140 blocks): hostile mob MUST despawn.
	//    Survival close (20 blocks) + survival far (140 blocks): hostile mob MUST stay.
	wMix := happyGhastWorld(t)
	wMix.SetDifficulty(world.DifficultyNormal)
	var huskFar, huskNear, codFar *world.EntityHandle
	<-wMix.Exec(func(tx *world.Tx) {
		huskFar = spawnAnimal(t, tx, "minecraft:husk", .5, nil)
		spec := addSurvivalPlayer(tx, mgl64.Vec3{.5, 64, 10.5})
		spec.SetGameMode(world.GameModeSpectator)
		addSurvivalPlayer(tx, mgl64.Vec3{.5, 64, 140.5})
		tx.SetBlock(cube.Pos{0, 63, 140}, block.Stone{}, nil)

		// Cod (water ambient, despawn distance 64) at 70 blocks from survival player at z=140.5 -> despawns!
		tx.SetBlock(cube.Pos{0, 63, 70}, block.Stone{}, nil)
		codFar = entity.NewCod(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 64, 70.5}})
		tx.AddEntity(codFar)
		// Husk at 20 blocks from survival player at z=140.5 -> stays!
		tx.SetBlock(cube.Pos{0, 63, 120}, block.Stone{}, nil)
		huskNear = entity.NewHusk(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 64, 120.5}})
		tx.AddEntity(huskNear)
	})
	for range 3 {
		wMix.AdvanceTick()
	}
	<-wMix.Exec(func(tx *world.Tx) {
		if _, ok := huskFar.Entity(tx); ok {
			t.Fatal("husk 140 blocks from survival player (with spectator nearby) did not despawn")
		}
		if _, ok := codFar.Entity(tx); ok {
			t.Fatal("cod 70 blocks (>64) from survival player did not despawn")
		}
		if _, ok := huskNear.Entity(tx); !ok {
			t.Fatal("husk 20 blocks from survival player despawned")
		}
	})
}

func BenchmarkMobTickTown100(b *testing.B) {
	w := world.Config{Synchronous: true, Entities: entity.DefaultRegistry}.New()
	b.Cleanup(func() { _ = w.Close() })
	w.SetDifficulty(world.DifficultyNormal)
	var mobs []*world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		for x := -60; x <= 60; x += 16 {
			for z := -60; z <= 60; z += 16 {
				tx.SetBlock(cube.Pos{x, 63, z}, block.Stone{}, nil)
			}
		}
		for i := range 100 {
			x := float64((i%10)*10) - 45 + .5
			z := float64((i/10)*10) - 45 + .5
			addSurvivalPlayer(tx, mgl64.Vec3{x, 64, z})
		}
		for i := range 20 {
			x := float64((i%5)*16) - 32 + .5
			z := float64((i/5)*16) - 24 + .5
			h := entity.NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{x, 64, z}})
			tx.AddEntity(h)
			mobs = append(mobs, h)
		}
	})
	// Warm up a couple ticks.
	for range 3 {
		w.AdvanceTick()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		w.AdvanceTick()
	}
	if len(mobs) == 0 {
		b.Fatal("no mobs")
	}
}
