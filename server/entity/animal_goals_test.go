package entity_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
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
		{"minecraft:chicken", block.WheatSeeds{}},
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

// TestBabiesFollowAdults checks FollowParentGoal: a calf more than three
// blocks from the nearest grown cow walks up to it.
func TestBabiesFollowAdults(t *testing.T) {
	w := traderTestWorld(t)
	var adult, calf *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) { adult = spawnAnimal(t, tx, "minecraft:cow", .5, nil) })
	// With no player around for five seconds the adult stops strolling.
	for range 110 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		pos := entityPos(tx, adult)
		calf = spawnAnimal(t, tx, "minecraft:cow", pos[0]+7, map[string]any{"Age": int32(-24000)})
	})
	for range 100 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		if d := horizontalDistance(entityPos(tx, calf), entityPos(tx, adult)); d > 3.5 {
			t.Fatalf("calf is %.2f blocks from the only adult cow, want it to follow to within 3 blocks", d)
		}
	})
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
