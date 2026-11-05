package block_test

import (
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/go-gl/mathgl/mgl64"
)

// itemEntities returns the items on the ground.
func (dt *dispenserTest) itemEntities() []world.Entity { return dt.entities("minecraft:item") }

// TestDispenserOptionalBehavioursKeepTheItem checks OptionalDispenseItemBehavior:
// bone meal on a block that cannot grow and flint and steel with nowhere to
// light keep the item, drop nothing and click as a failure.
func TestDispenserOptionalBehavioursKeepTheItem(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name  string
		stack item.Stack
	}{
		{"bone meal", item.NewStack(item.BoneMeal{}, 2)},
		{"flint and steel", item.NewStack(item.FlintAndSteel{}, 1)},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			dt := newDispenserTest(t, cube.FaceEast)
			dt.dispense(func(tx *world.Tx, front cube.Pos) { tx.SetBlock(front, block.Stone{}, nil) }, c.stack)
			if got := dt.slot(0); got.Count() != c.stack.Count() || got.Item() != c.stack.Item() {
				t.Fatalf("slot holds %v, want the %v it held", got, c.stack)
			}
			if items := dt.itemEntities(); len(items) != 0 {
				t.Fatalf("%d items dropped, want none", len(items))
			}
			if got := dt.recorder.soundNames(); !slices.Equal(got, []string{"block.click.fail"}) {
				t.Fatalf("sounds %v, want the click of a failure", got)
			}
			if got := dt.recorder.smokes(); !slices.Equal(got, []cube.Face{cube.FaceEast}) {
				t.Fatalf("smoke towards %v, want east once", got)
			}
		})
	}
}

// TestDispenserFlintAndSteelLightsTheBlockInFront checks that the fire goes
// into the block in front of the dispenser, not the one beyond it, and wears
// the item by one.
func TestDispenserFlintAndSteelLightsTheBlockInFront(t *testing.T) {
	t.Parallel()
	dt := newDispenserTest(t, cube.FaceEast)
	dt.dispense(nil, item.NewStack(item.FlintAndSteel{}, 1))
	<-dt.w.Exec(func(tx *world.Tx) {
		if _, ok := tx.Block(cube.Pos{1, 64, 0}).(block.Fire); !ok {
			t.Errorf("block in front is %T, want fire", tx.Block(cube.Pos{1, 64, 0}))
		}
		if _, ok := tx.Block(cube.Pos{2, 64, 0}).(block.Air); !ok {
			t.Errorf("block two ahead is %T, want air", tx.Block(cube.Pos{2, 64, 0}))
		}
	})
	if got := dt.slot(0); got.Durability() != got.MaxDurability()-1 {
		t.Fatalf("flint and steel has durability %d of %d, want one less", got.Durability(), got.MaxDurability())
	}
	if got := dt.recorder.soundNames(); !slices.Equal(got, []string{"block.click"}) {
		t.Fatalf("sounds %v, want the click of a success", got)
	}
}

// TestDispenserFlintAndSteelPrimesTNT checks that TNT in front is primed.
func TestDispenserFlintAndSteelPrimesTNT(t *testing.T) {
	t.Parallel()
	dt := newDispenserTest(t, cube.FaceEast)
	dt.dispense(func(tx *world.Tx, front cube.Pos) { tx.SetBlock(front, block.TNT{}, nil) }, item.NewStack(item.FlintAndSteel{}, 1))
	<-dt.w.Exec(func(tx *world.Tx) {
		if _, ok := tx.Block(cube.Pos{1, 64, 0}).(block.Air); !ok {
			t.Errorf("the TNT is still %T", tx.Block(cube.Pos{1, 64, 0}))
		}
	})
	if tnt := dt.entities("minecraft:tnt"); len(tnt) != 1 {
		t.Fatalf("%d primed TNT, want 1", len(tnt))
	}
}

// TestDispenserBoneMealGrowsTheCropInFront checks that bone meal grows a
// crop in front of the dispenser, uses one up and shows the growth particles
// at the crop.
func TestDispenserBoneMealGrowsTheCropInFront(t *testing.T) {
	t.Parallel()
	dt := newDispenserTest(t, cube.FaceEast)
	dt.dispense(func(tx *world.Tx, front cube.Pos) {
		tx.SetBlock(front.Side(cube.FaceDown), block.Farmland{Hydration: 7}, nil)
		tx.SetBlock(front, block.WheatSeeds{}, nil)
	}, item.NewStack(item.BoneMeal{}, 2))
	<-dt.w.Exec(func(tx *world.Tx) {
		crop, ok := tx.Block(cube.Pos{1, 64, 0}).(block.WheatSeeds)
		if !ok || crop.Growth == 0 {
			t.Errorf("crop in front is %v, want it grown", tx.Block(cube.Pos{1, 64, 0}))
		}
	})
	if got := dt.slot(0); got.Count() != 1 {
		t.Fatalf("slot holds %d bone meal, want 1", got.Count())
	}
	var bone []mgl64.Vec3
	for _, p := range dt.recorder.particles {
		if _, ok := p.p.(particle.BoneMeal); ok {
			bone = append(bone, p.pos)
		}
	}
	if want := []mgl64.Vec3{{1, 64, 0}}; !slices.Equal(bone, want) {
		t.Fatalf("growth particles at %v, want %v", bone, want)
	}
	if got := dt.recorder.soundNames(); !slices.Equal(got, []string{"block.click"}) {
		t.Fatalf("sounds %v, want the click of a success", got)
	}
}

// TestDispenserRemaindersGoIntoTheDispenser checks DefaultDispenseItemBehavior
// .consumeWithRemainder: the bucket a bucket makes goes into a free slot of the
// dispenser when the stack has items left, and is dropped, with the smoke and
// the click of an ejected item, only if the dispenser is full.
func TestDispenserRemaindersGoIntoTheDispenser(t *testing.T) {
	t.Parallel()
	water := func(tx *world.Tx, front cube.Pos) { tx.SetBlock(front, block.Water{Depth: 8, Still: true}, nil) }
	t.Run("fits", func(t *testing.T) {
		t.Parallel()
		dt := newDispenserTest(t, cube.FaceEast)
		dt.dispense(water, item.NewStack(item.Bucket{}, 2))
		if got := dt.slot(0); got.Count() != 1 || got.Item() != (item.Bucket{}) {
			t.Fatalf("slot 0 holds %v, want one empty bucket", got)
		}
		if b, ok := dt.slot(1).Item().(item.Bucket); !ok || dt.slot(1).Count() != 1 || b.Content.LiquidType() != "water" {
			t.Fatalf("slot 1 holds %v, want a water bucket", dt.slot(1))
		}
		if items := dt.itemEntities(); len(items) != 0 {
			t.Fatalf("%d items dropped, want none", len(items))
		}
		if got := dt.recorder.smokes(); len(got) != 1 {
			t.Fatalf("%d smokes, want 1", len(got))
		}
	})
	t.Run("full", func(t *testing.T) {
		t.Parallel()
		dt := newDispenserTest(t, cube.FaceEast)
		// Every slot holds full stacks of empty buckets, whichever the
		// dispenser picks, and none takes the water bucket.
		var stacks []item.Stack
		for range 9 {
			stacks = append(stacks, item.NewStack(item.Bucket{}, 16))
		}
		dt.dispense(water, stacks...)
		used := 0
		for i := range 9 {
			if dt.slot(i).Count() == 15 {
				used++
			}
		}
		if used != 1 {
			t.Fatalf("%d slots lost a bucket, want 1", used)
		}
		if items := dt.itemEntities(); len(items) != 1 {
			t.Fatalf("%d items dropped, want the water bucket", len(items))
		}
		if got := dt.recorder.smokes(); len(got) != 2 {
			t.Fatalf("%d smokes, want 2: one for the dispense, one for the ejection", len(got))
		}
	})
	t.Run("glass bottle", func(t *testing.T) {
		t.Parallel()
		dt := newDispenserTest(t, cube.FaceEast)
		dt.dispense(func(tx *world.Tx, front cube.Pos) {
			tx.SetBlock(front, block.Water{Depth: 1}, nil)
		}, item.NewStack(item.GlassBottle{}, 2))
		if got := dt.slot(0); got.Count() != 1 {
			t.Fatalf("slot 0 holds %v, want one glass bottle", got)
		}
		if got := dt.slot(1); got.Count() != 1 || got.Item().(interface{ EncodeItem() (string, int16) }) == nil {
			t.Fatalf("slot 1 holds %v, want a water bottle", got)
		}
		if name, _ := dt.slot(1).Item().EncodeItem(); name != "minecraft:potion" {
			t.Fatalf("slot 1 holds %s, want a water bottle", name)
		}
	})
}

// TestDispenserHarnessFindsMobsByTheirBox checks that the harness finds a mob
// whose bounding box reaches into the block in front, though its position is
// outside of the block, such as a happy ghast standing on the floor beside it:
// Level.getEntitiesOfClass intersects the bounding box of the mob with that of
// the block.
func TestDispenserHarnessFindsMobsByTheirBox(t *testing.T) {
	t.Parallel()
	dt := newDispenserTest(t, cube.FaceEast)
	dt.dispense(func(tx *world.Tx, front cube.Pos) {
		tx.AddEntity(entity.NewHappyGhast(world.EntitySpawnOpts{Position: front.Vec3().Add(mgl64.Vec3{1.5, 0, 0.5})}))
	}, item.NewStack(item.Harness{Colour: item.ColourWhite()}, 1))
	if got := dt.slot(0); !got.Empty() {
		t.Fatalf("slot holds %v, want the harness used", got)
	}
	if items := dt.itemEntities(); len(items) != 0 {
		t.Fatalf("%d items dropped, want none", len(items))
	}
}
