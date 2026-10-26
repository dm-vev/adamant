package block

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

func boneMealWorld(t *testing.T, f func(tx *world.Tx)) {
	t.Helper()
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	<-w.Exec(f)
}

// TestBoneMealGrowsJavaBonemealables checks the blocks Java's
// BonemealableBlock covers that took no bone meal.
func TestBoneMealGrowsJavaBonemealables(t *testing.T) {
	boneMealWorld(t, func(tx *world.Tx) {
		// Kelp grows at its top into water, whatever the clicked block's age.
		for y := 60; y <= 66; y++ {
			tx.SetBlock(cube.Pos{0, y, 0}, Water{Depth: 8, Still: true}, nil)
		}
		tx.SetBlock(cube.Pos{0, 59, 0}, Stone{}, nil)
		tx.SetBlock(cube.Pos{0, 60, 0}, Kelp{Age: 3}, nil)
		tx.SetBlock(cube.Pos{0, 61, 0}, Kelp{Age: 25}, nil)
		if r := (Kelp{Age: 3}).BoneMeal(cube.Pos{0, 60, 0}, tx); r == item.BoneMealResultNone {
			t.Fatal("kelp took no bone meal")
		}
		if k, ok := tx.Block(cube.Pos{0, 62, 0}).(Kelp); !ok || k.Age != 25 {
			t.Fatalf("kelp grew %v above its head, want kelp of age 25", tx.Block(cube.Pos{0, 62, 0}))
		}

		tx.SetBlock(cube.Pos{4, 70, 0}, CaveVines{}, nil)
		(CaveVines{}).BoneMeal(cube.Pos{4, 70, 0}, tx)
		if v := tx.Block(cube.Pos{4, 70, 0}).(CaveVines); !v.HasBerries() {
			t.Fatal("cave vines grew no glow berry")
		}

		tx.SetBlock(cube.Pos{8, 70, 0}, DirtWithRoots{}, nil)
		(DirtWithRoots{}).BoneMeal(cube.Pos{8, 70, 0}, tx)
		if _, ok := tx.Block(cube.Pos{8, 69, 0}).(HangingRoots); !ok {
			t.Fatal("rooted dirt grew no hanging roots")
		}

		tx.SetBlock(cube.Pos{12, 70, 0}, Leaves{Type: MangroveLeaves()}, nil)
		(Leaves{Type: MangroveLeaves()}).BoneMeal(cube.Pos{12, 70, 0}, tx)
		if s, ok := tx.Block(cube.Pos{12, 69, 0}).(TreeSapling); !ok || !s.Hanging || s.Type != "mangrove_propagule" {
			t.Fatalf("mangrove leaves grew %v below, want a hanging propagule", tx.Block(cube.Pos{12, 69, 0}))
		}

		tx.SetBlock(cube.Pos{16, 70, 0}, ShortDryGrass{}, nil)
		(ShortDryGrass{}).BoneMeal(cube.Pos{16, 70, 0}, tx)
		if _, ok := tx.Block(cube.Pos{16, 70, 0}).(TallDryGrass); !ok {
			t.Fatal("short dry grass did not grow tall")
		}

		tx.SetBlock(cube.Pos{20, 70, 0}, Netherrack{}, nil)
		tx.SetBlock(cube.Pos{21, 70, 0}, Nylium{Warped: true}, nil)
		(Netherrack{}).BoneMeal(cube.Pos{20, 70, 0}, tx)
		if n, ok := tx.Block(cube.Pos{20, 70, 0}).(Nylium); !ok || !n.Warped {
			t.Fatalf("netherrack beside warped nylium became %v", tx.Block(cube.Pos{20, 70, 0}))
		}

		tx.SetBlock(cube.Pos{24, 59, 0}, Stone{}, nil)
		tx.SetBlock(cube.Pos{24, 60, 0}, SeaGrass{Type: SeaGrassDefault()}, nil)
		tx.SetBlock(cube.Pos{24, 61, 0}, Water{Depth: 8, Still: true}, nil)
		(SeaGrass{Type: SeaGrassDefault()}).BoneMeal(cube.Pos{24, 60, 0}, tx)
		if s, ok := tx.Block(cube.Pos{24, 61, 0}).(SeaGrass); !ok || s.Type != SeaGrassDoubleTop() {
			t.Fatal("seagrass did not grow tall")
		}

		tx.SetBlock(cube.Pos{28, 70, 0}, PaleHangingMoss{}, nil)
		tx.SetBlock(cube.Pos{28, 69, 0}, PaleHangingMoss{Tip: true}, nil)
		(PaleHangingMoss{}).BoneMeal(cube.Pos{28, 70, 0}, tx)
		if m, ok := tx.Block(cube.Pos{28, 68, 0}).(PaleHangingMoss); !ok || !m.Tip {
			t.Fatal("pale hanging moss did not grow below its tip")
		}

		// Twisting vines grow at least as far as their count allows into air.
		tx.SetBlock(cube.Pos{32, 70, 0}, TwistingVines{Age: 2}, nil)
		if r := (TwistingVines{Age: 2}).BoneMeal(cube.Pos{32, 70, 0}, tx); r == item.BoneMealResultNone {
			t.Fatal("twisting vines took no bone meal")
		}
	})
}

// TestBoneMealSkipsNonJavaBonemealables checks flowers and sugar cane, which
// are no BonemealableBlock in Java, take no bone meal.
func TestBoneMealSkipsNonJavaBonemealables(t *testing.T) {
	for _, b := range []world.Block{Flower{}, SugarCane{}} {
		if _, ok := b.(item.BoneMealAffected); ok {
			t.Errorf("%T takes bone meal", b)
		}
	}
}

// TestBoneMealGrowsSeagrassUnderWater follows BoneMealItem.growWaterPlant:
// bone meal on the sea floor grows seagrass in the water around it.
func TestBoneMealGrowsSeagrassUnderWater(t *testing.T) {
	boneMealWorld(t, func(tx *world.Tx) {
		for x := -8; x <= 8; x++ {
			for z := -8; z <= 8; z++ {
				tx.SetBlock(cube.Pos{x, 59, z}, Stone{}, nil)
				tx.SetBlock(cube.Pos{x, 60, z}, Water{Depth: 8, Still: true}, nil)
				tx.SetBlock(cube.Pos{x, 61, z}, Water{Depth: 8, Still: true}, nil)
			}
		}
		if !(Water{Depth: 8, Still: true}).GrowWaterPlants(cube.Pos{0, 60, 0}, cube.FaceUp, tx) {
			t.Fatal("water source grew nothing")
		}
		if _, ok := tx.Block(cube.Pos{0, 60, 0}).(SeaGrass); !ok {
			t.Fatalf("no seagrass where the bone meal was used: %v", tx.Block(cube.Pos{0, 60, 0}))
		}
	})
}

// TestPaleMossCarpetFollowsJava follows MossyCarpetBlock: a carpet placed by
// a wall grows a low side against it, and bone meal grows a topper above
// with the sides it can attach.
func TestPaleMossCarpetFollowsJava(t *testing.T) {
	boneMealWorld(t, func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{0, 59, 0}, Stone{}, nil)
		tx.SetBlock(cube.Pos{1, 60, 0}, Stone{}, nil)
		tx.SetBlock(cube.Pos{1, 61, 0}, Stone{}, nil)
		carpet := PaleMossCarpet{}.updated(cube.Pos{0, 60, 0}, tx, true)
		if carpet.EastConnection != ShortWallConnection() || carpet.NorthConnection != NoWallConnection() {
			t.Fatalf("carpet by an east wall has sides %v", carpet)
		}
		tx.SetBlock(cube.Pos{0, 60, 0}, carpet, nil)
		carpet.BoneMeal(cube.Pos{0, 60, 0}, tx)
		topper, ok := tx.Block(cube.Pos{0, 61, 0}).(PaleMossCarpet)
		if !ok || !topper.Upper || topper.EastConnection != ShortWallConnection() {
			t.Fatalf("bone meal grew %v above the carpet, want a topper against the east wall", tx.Block(cube.Pos{0, 61, 0}))
		}
	})
}
