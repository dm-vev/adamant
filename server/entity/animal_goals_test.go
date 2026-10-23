package entity_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func entityPos(tx *world.Tx, h *world.EntityHandle) mgl64.Vec3 {
	e, _ := h.Entity(tx)
	return e.Position()
}

func horizontalDistance(a, b mgl64.Vec3) float64 {
	a[1], b[1] = 0, 0
	return a.Sub(b).Len()
}

// TestAnimalsFollowPlayersHoldingTheirFood checks TemptGoal: a player
// holding an animal's food lures it from ten blocks away until it stands
// 2.5 blocks away.
func TestAnimalsFollowPlayersHoldingTheirFood(t *testing.T) {
	cases := []struct {
		id   string
		food world.Item
	}{
		{"minecraft:cow", item.Wheat{}},
		{"minecraft:mooshroom", item.Wheat{}},
		{"minecraft:sheep", item.Wheat{}},
		{"minecraft:pig", block.Carrot{}},
		{"minecraft:pig", item.CarrotOnAStick{}},
		{"minecraft:chicken", block.WheatSeeds{}},
		{"minecraft:horse", item.GoldenCarrot{}},
		{"minecraft:llama", block.HayBale{}},
		{"minecraft:rabbit", block.Carrot{}},
		{"minecraft:turtle", block.SeaGrass{}},
		{"minecraft:panda", block.Bamboo{}},
	}
	for _, c := range cases {
		t.Run(c.id, func(t *testing.T) {
			w := traderTestWorld(t)
			var h *world.EntityHandle
			playerPos := mgl64.Vec3{9.5, 64, .5}
			<-w.Exec(func(tx *world.Tx) {
				h = spawnAnimal(t, tx, c.id, .5, nil)
				addSurvivalPlayer(tx, playerPos).SetHeldItems(item.NewStack(c.food, 1), item.Stack{})
			})
			closest := 100.0
			for range 200 {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) { closest = min(closest, horizontalDistance(entityPos(tx, h), playerPos)) })
			}
			if closest > 3.5 {
				t.Fatalf("%s came no closer than %.2f blocks to a player holding its food", c.id, closest)
			}
			if closest < 1.5 {
				t.Fatalf("%s walked %.2f blocks up to the player, want it to stop 2.5 blocks away", c.id, closest)
			}
		})
	}
}

// TestAnimalsPathAroundWallsToTheirFood checks animals find a path around a
// wall to a player holding their food rather than walking into the wall.
func TestAnimalsPathAroundWallsToTheirFood(t *testing.T) {
	foods := map[string]world.Item{"minecraft:cow": item.Wheat{}, "minecraft:sheep": item.Wheat{}, "minecraft:chicken": block.WheatSeeds{}}
	for id, food := range foods {
		t.Run(id, func(t *testing.T) {
			w := traderTestWorld(t)
			var h *world.EntityHandle
			playerPos := mgl64.Vec3{8.5, 64, .5}
			<-w.Exec(func(tx *world.Tx) {
				for z := -3; z <= 3; z++ {
					tx.SetBlock(cube.Pos{4, 64, z}, block.Stone{}, nil)
					tx.SetBlock(cube.Pos{4, 65, z}, block.Stone{}, nil)
				}
				h = spawnAnimal(t, tx, id, .5, nil)
				addSurvivalPlayer(tx, playerPos).SetHeldItems(item.NewStack(food, 1), item.Stack{})
			})
			closest := 100.0
			for range 300 {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) { closest = min(closest, horizontalDistance(entityPos(tx, h), playerPos)) })
			}
			if closest > 3.5 {
				t.Fatalf("%s came no closer than %.2f blocks to a player holding its food behind a wall", id, closest)
			}
		})
	}
}

// TestChickensFallSlowly checks Chicken.aiStep: a falling chicken flaps its
// wings, which slows its fall to about a block a second.
func TestChickensFallSlowly(t *testing.T) {
	w := traderTestWorld(t)
	var h *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		h = tx.AddEntity(entity.NewChicken(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 74, .5}}, entity.ChickenBehaviourConfig{})).H()
	})
	for range 10 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		if fell := 74 - entityPos(tx, h)[1]; fell < .5 || fell > 1.5 {
			t.Fatalf("chicken fell %.2f blocks in half a second, want about one", fell)
		}
	})
}

// TestBabiesFollowAdults checks FollowParentGoal: a baby more than three
// blocks from the nearest grown animal of its kind walks up to it.
func TestBabiesFollowAdults(t *testing.T) {
	for _, id := range []string{"minecraft:cow", "minecraft:polar_bear"} {
		t.Run(id, func(t *testing.T) {
			w := traderTestWorld(t)
			var adult, baby *world.EntityHandle
			<-w.Exec(func(tx *world.Tx) { adult = spawnAnimal(t, tx, id, .5, nil) })
			// With no player around for five seconds a cow stops strolling.
			for range 110 {
				w.AdvanceTick()
			}
			<-w.Exec(func(tx *world.Tx) {
				pos := entityPos(tx, adult)
				baby = spawnAnimal(t, tx, id, pos[0]+7, map[string]any{"Age": int32(-24000)})
			})
			closest := 100.0
			for range 100 {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					closest = min(closest, horizontalDistance(entityPos(tx, baby), entityPos(tx, adult)))
				})
			}
			if closest > 3.5 {
				t.Fatalf("baby came no closer than %.2f blocks to the only adult, want it to follow to within 3 blocks", closest)
			}
		})
	}
}

// TestSheepEatGrassToRegrowWool checks EatBlockGoal: a sheared lamb grazes
// the grass block it stands on, which turns to dirt, regrows its wool and
// lets it grow up a minute sooner.
func TestSheepEatGrassToRegrowWool(t *testing.T) {
	w := traderTestWorld(t)
	var h *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		h = spawnAnimal(t, tx, "minecraft:sheep", .5, map[string]any{"Age": int32(-24000), "Sheared": byte(1)})
	})
	grazed := false
	for tick := 0; tick < 600 && !grazed; tick++ {
		w.AdvanceTick()
		<-w.Exec(func(tx *world.Tx) {
			e, _ := h.Entity(tx)
			grazed = !e.(interface{ Sheared() bool }).Sheared()
		})
	}
	if !grazed {
		t.Fatal("sheared lamb on grass did not regrow its wool in 30 seconds")
	}
	<-w.Exec(func(tx *world.Tx) {
		dirt := 0
		for x := -20; x <= 20; x++ {
			for z := -20; z <= 20; z++ {
				if _, ok := tx.Block(cube.Pos{x, 63, z}).(block.Dirt); ok {
					dirt++
				}
			}
		}
		if dirt != 1 {
			t.Fatalf("grazing left %d dirt blocks, want the grass block the lamb ate", dirt)
		}
	})
}

// TestRabbitsFleeFromPlayers checks RabbitAvoidEntityGoal: a rabbit runs away
// from a survival player without carrots that comes within eight blocks.
func TestRabbitsFleeFromPlayers(t *testing.T) {
	w := traderTestWorld(t)
	var h *world.EntityHandle
	playerPos := mgl64.Vec3{5.5, 64, .5}
	<-w.Exec(func(tx *world.Tx) {
		h = spawnAnimal(t, tx, "minecraft:rabbit", .5, nil)
		addSurvivalPlayer(tx, playerPos)
	})
	for range 60 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		if d := horizontalDistance(entityPos(tx, h), playerPos); d < 8 {
			t.Fatalf("rabbit stayed %.2f blocks from a player, want it to flee beyond eight blocks", d)
		}
	})
}

// TestBeesFollowFlowers checks the bee's TemptGoal: a calm bee flies up to a
// player holding a flower.
func TestBeesFollowFlowers(t *testing.T) {
	w := traderTestWorld(t)
	var h *world.EntityHandle
	eye := mgl64.Vec3{9.5, 64 + 1.62, .5}
	<-w.Exec(func(tx *world.Tx) {
		h = spawnAnimal(t, tx, "minecraft:bee", .5, nil)
		addSurvivalPlayer(tx, mgl64.Vec3{9.5, 64, .5}).SetHeldItems(item.NewStack(block.Flower{}, 1), item.Stack{})
	})
	closest := 100.0
	for range 200 {
		w.AdvanceTick()
		<-w.Exec(func(tx *world.Tx) { closest = min(closest, entityPos(tx, h).Sub(eye).Len()) })
	}
	if closest > 3.5 {
		t.Fatalf("bee came no closer than %.2f blocks to a player holding a flower", closest)
	}
}

// TestWorriedPandasFleeFromPlayers checks PandaAvoidGoal: a worried panda
// runs from a player within eight blocks.
func TestWorriedPandasFleeFromPlayers(t *testing.T) {
	w := traderTestWorld(t)
	var worried *world.EntityHandle
	playerPos := mgl64.Vec3{4.5, 64, .5}
	<-w.Exec(func(tx *world.Tx) {
		worried = spawnAnimal(t, tx, "minecraft:panda", .5, map[string]any{"Variant": int32(2), "MainGene": int32(2), "HiddenGene": int32(2)})
		addSurvivalPlayer(tx, playerPos)
	})
	for range 100 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		if d := horizontalDistance(entityPos(tx, worried), playerPos); d < 8 {
			t.Fatalf("worried panda stayed %.2f blocks from a player, want it to flee beyond eight blocks", d)
		}
	})
}
