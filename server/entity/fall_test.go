package entity_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// dropMob drops a mob from 10 blocks above a 9x9 floor of ground at y=63,
// whose centre is replaced by landing, and returns the health it lost.
func dropMob(t *testing.T, id string, landing world.Block, mobGriefing bool) (lost float64, landed world.Block) {
	t.Helper()
	w := newDeathTestWorld(t)
	w.SetGameRule("mob_griefing", mobGriefing)
	var h *world.EntityHandle
	var before float64
	<-w.Exec(func(tx *world.Tx) {
		for x := -4; x <= 4; x++ {
			for z := -4; z <= 4; z++ {
				tx.SetBlock(cube.Pos{x, 63, z}, block.Stone{}, nil)
			}
		}
		tx.SetBlock(cube.Pos{0, 63, 0}, landing, nil)
		top := 64.0
		if _, farmland := landing.(block.Farmland); farmland {
			top = 63 + 15.0/16
		}
		mob, err := entity.NewFromJavaNBT(map[string]any{"id": id}, world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, top + 10, 0.5}, SpawnFinalized: true})
		if err != nil {
			t.Fatal(err)
		}
		e := tx.AddEntity(mob)
		h, before = e.H(), e.(interface{ Health() float64 }).Health()
	})
	for range 60 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		e, ok := h.Entity(tx)
		if !ok {
			t.Fatalf("%s is gone", id)
		}
		lost, landed = before-e.(interface{ Health() float64 }).Health(), tx.Block(cube.Pos{0, 63, 0})
	})
	return lost, landed
}

func TestMobFallDamage(t *testing.T) {
	cases := []struct {
		name, id string
		landing  world.Block
		lost     float64
	}{
		// floor(10 - 3) for a cow, floor(10 - 5) for a fox and floor((10 - 3)
		// * 0.2) on hay.
		{"cow on stone", "minecraft:cow", block.Stone{}, 7},
		{"fox falls five blocks safely", "minecraft:fox", block.Stone{}, 5},
		{"goats take 10 less", "minecraft:goat", block.Stone{}, 0},
		{"chickens are immune", "minecraft:chicken", block.Stone{}, 0},
		{"hay bale takes a fifth of the damage", "minecraft:cow", block.HayBale{}, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if lost, _ := dropMob(t, c.id, c.landing, true); lost != c.lost {
				t.Fatalf("%s lost %v health, want %v", c.id, lost, c.lost)
			}
		})
	}
}

func TestMobsTrampleFarmlandWithMobGriefing(t *testing.T) {
	cases := []struct {
		name        string
		id          string
		mobGriefing bool
		trampled    bool
	}{
		{"cow", "minecraft:cow", true, true},
		{"cow without mob griefing", "minecraft:cow", false, false},
		{"chickens are too small", "minecraft:chicken", true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, landed := dropMob(t, c.id, block.Farmland{}, c.mobGriefing)
			if _, dirt := landed.(block.Dirt); dirt != c.trampled {
				t.Fatalf("farmland under a falling %s became %#v", c.id, landed)
			}
		})
	}
}
