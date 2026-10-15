package overworld

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/biome"
)

// boneMealUntil applies bone meal to the block at pos until grown reports
// true, as a player does, and fails after 60 tries.
func boneMealUntil(t *testing.T, tx *world.Tx, pos cube.Pos, grown func() bool) {
	t.Helper()
	for range 60 {
		b, ok := tx.Block(pos).(item.BoneMealAffected)
		if !ok {
			break
		}
		if b.BoneMeal(pos, tx) == item.BoneMealResultNone {
			t.Fatalf("bone meal had no effect on %#v", tx.Block(pos))
		}
		if grown() {
			return
		}
	}
	if !grown() {
		t.Fatalf("%#v did not grow with bone meal", tx.Block(pos))
	}
}

func floor(tx *world.Tx, b world.Block, y, radius int) {
	for x := -radius; x <= radius; x++ {
		for z := -radius; z <= radius; z++ {
			tx.SetBlock(cube.Pos{x, y, z}, b, nil)
		}
	}
}

// TestPlantsGrowIntoFeatures checks plants grow into their Java features
// with bone meal: an oak sapling into a tree, dark oak only from four
// saplings, a mushroom into a huge mushroom, a fungus on its nylium into a
// huge fungus, and moss spreads a patch.
func TestPlantsGrowIntoFeatures(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	<-w.Exec(func(tx *world.Tx) {
		floor(tx, block.Dirt{}, 63, 8)
		sapling := cube.Pos{0, 64, 0}
		tx.SetBlock(sapling, block.TreeSapling{Type: "oak_sapling"}, nil)
		boneMealUntil(t, tx, sapling, func() bool { _, ok := tx.Block(sapling).(block.Log); return ok })
	})
	<-w.Exec(func(tx *world.Tx) {
		floor(tx, block.Air{}, 64, 8)
		floor(tx, block.Air{}, 65, 8)
		lone := cube.Pos{0, 64, 0}
		tx.SetBlock(lone, block.TreeSapling{Type: "dark_oak_sapling", Stage: 1}, nil)
		if tx.Block(lone).(item.BoneMealAffected).BoneMeal(lone, tx) != item.BoneMealResultNone {
			t.Fatal("bone meal took on a lone dark oak sapling")
		}
		for _, p := range []cube.Pos{{1, 64, 0}, {0, 64, 1}, {1, 64, 1}} {
			tx.SetBlock(p, block.TreeSapling{Type: "dark_oak_sapling", Stage: 1}, nil)
		}
		boneMealUntil(t, tx, lone, func() bool {
			logs := 0
			for _, p := range []cube.Pos{{0, 64, 0}, {1, 64, 0}, {0, 64, 1}, {1, 64, 1}} {
				if _, ok := tx.Block(p).(block.Log); ok {
					logs++
				}
			}
			return logs == 4
		})
	})
	<-w.Exec(func(tx *world.Tx) {
		mushroom := cube.Pos{40, 64, 0}
		tx.SetBlock(mushroom.Side(cube.FaceDown), block.Mycelium{}, nil)
		tx.SetBlock(mushroom, block.RedMushroom{}, nil)
		boneMealUntil(t, tx, mushroom, func() bool {
			_, ok := tx.Block(mushroom).(block.RedMushroom)
			return !ok
		})
		name, _ := tx.Block(mushroom).EncodeBlock()
		if name != "minecraft:mushroom_stem" {
			t.Fatalf("grown mushroom left %s at its base, want a stem", name)
		}

		fungus := cube.Pos{60, 64, 0}
		tx.SetBlock(fungus.Side(cube.FaceDown), block.Nylium{Warped: true}, nil)
		tx.SetBlock(fungus, block.Fungus{Warped: true}, nil)
		boneMealUntil(t, tx, fungus, func() bool {
			_, ok := tx.Block(fungus).(block.Fungus)
			return !ok
		})
		if name, _ := tx.Block(fungus).EncodeBlock(); name != "minecraft:warped_stem" {
			t.Fatalf("grown fungus left %s at its base, want a warped stem", name)
		}

		moss := cube.Pos{80, 63, 0}
		for x := -3; x <= 3; x++ {
			for z := -3; z <= 3; z++ {
				tx.SetBlock(moss.Add(cube.Pos{x, 0, z}), block.Stone{}, nil)
			}
		}
		tx.SetBlock(moss, block.MossBlock{}, nil)
		if tx.Block(moss).(item.BoneMealAffected).BoneMeal(moss, tx) != item.BoneMealResultArea {
			t.Fatal("bone meal on moss with air above did not spread")
		}
		spread := 0
		for x := -3; x <= 3; x++ {
			for z := -3; z <= 3; z++ {
				if _, ok := tx.Block(moss.Add(cube.Pos{x, 0, z})).(block.MossBlock); ok {
					spread++
				}
			}
		}
		if spread < 2 {
			t.Fatalf("moss patch covers %d blocks, want it to spread", spread)
		}
	})
}

// TestGrassBoneMealGrowsBiomePlants checks bone meal on grass as
// GrassBlock.performBonemeal: short grass grows across the grass around, and
// with repeated use the plains biome's flowers too.
func TestGrassBoneMealGrowsBiomePlants(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	<-w.Exec(func(tx *world.Tx) {
		for x := -8; x <= 8; x++ {
			for z := -8; z <= 8; z++ {
				for y := 60; y <= 70; y++ {
					tx.SetBiome(cube.Pos{x, y, z}, biome.Plains{})
				}
				tx.SetBlock(cube.Pos{x, 63, z}, block.Grass{}, nil)
			}
		}
		origin := cube.Pos{0, 63, 0}
		grass, flowers := 0, 0
		for range 5 {
			if tx.Block(origin).(item.BoneMealAffected).BoneMeal(origin, tx) != item.BoneMealResultArea {
				break
			}
			tx.SetBlock(origin.Side(cube.FaceUp), nil, nil)
		}
		for x := -8; x <= 8; x++ {
			for z := -8; z <= 8; z++ {
				switch tx.Block(cube.Pos{x, 64, z}).(type) {
				case block.ShortGrass, block.DoubleTallGrass:
					grass++
				case block.Flower:
					flowers++
				}
			}
		}
		if grass < 20 {
			t.Fatalf("bone meal grew %d grass plants, want grass spread around", grass)
		}
		if flowers == 0 {
			t.Fatal("bone meal grew no plains flowers")
		}
	})
}
