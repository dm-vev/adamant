package block_test

import (
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/potion"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
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

// TestDispenserWaxesCopper checks the behaviour of Items.HONEYCOMB: copper in
// front is waxed with the wax-on effect, the click is at the corner of the block
// as BDS sends it, and any other block leaves the honeycomb ejected.
func TestDispenserWaxesCopper(t *testing.T) {
	t.Parallel()
	dt := newDispenserTest(t, cube.FaceEast)
	dt.dispense(func(tx *world.Tx, front cube.Pos) { tx.SetBlock(front, block.Copper{Type: block.NormalCopper()}, nil) }, item.NewStack(item.Honeycomb{}, 2))
	<-dt.w.Exec(func(tx *world.Tx) {
		if c, ok := tx.Block(cube.Pos{1, 64, 0}).(block.Copper); !ok || !c.Waxed {
			t.Errorf("block in front is %v, want waxed copper", tx.Block(cube.Pos{1, 64, 0}))
		}
	})
	if got := dt.slot(0); got.Count() != 1 {
		t.Fatalf("slot holds %d honeycombs, want 1", got.Count())
	}
	var waxOn, click *recordedSound
	for i, s := range dt.recorder.sounds {
		switch v := s.s.(type) {
		case sound.SignWaxed:
			waxOn = &dt.recorder.sounds[i]
		case sound.Event:
			click = &dt.recorder.sounds[i]
			_ = v
		}
	}
	if waxOn == nil || waxOn.pos != (mgl64.Vec3{1.5, 64.5, 0.5}) {
		t.Errorf("wax-on sound %v, want it at the centre of the block", waxOn)
	}
	if click == nil || click.pos != (mgl64.Vec3{1, 64, 0}) {
		t.Errorf("click %v, want it at the corner of the block", click)
	}

	other := newDispenserTest(t, cube.FaceEast)
	other.dispense(func(tx *world.Tx, front cube.Pos) { tx.SetBlock(front, block.Stone{}, nil) }, item.NewStack(item.Honeycomb{}, 1))
	if items := other.itemEntities(); len(items) != 1 {
		t.Fatalf("%d items dropped for honeycomb on stone, want 1", len(items))
	}
}

// TestDispenserChargesRespawnAnchor checks the behaviour of Items.GLOWSTONE.
func TestDispenserChargesRespawnAnchor(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name          string
		set           func(tx *world.Tx, front cube.Pos)
		charge, left  int
		items, clicks int
		sounds        []string
	}{
		{"empty anchor", func(tx *world.Tx, front cube.Pos) { tx.SetBlock(front, block.RespawnAnchor{}, nil) }, 1, 1, 0, 1, []string{"other", "block.click"}},
		{"full anchor", func(tx *world.Tx, front cube.Pos) { tx.SetBlock(front, block.RespawnAnchor{Charge: 4}, nil) }, 4, 2, 0, 1, []string{"block.click.fail"}},
		{"no anchor", nil, -1, 1, 1, 1, []string{"block.click+block"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			dt := newDispenserTest(t, cube.FaceEast)
			dt.dispense(c.set, item.NewStack(block.Glowstone{}, 2))
			<-dt.w.Exec(func(tx *world.Tx) {
				anchor, ok := tx.Block(cube.Pos{1, 64, 0}).(block.RespawnAnchor)
				if c.charge >= 0 && (!ok || anchor.Charge != c.charge) {
					t.Errorf("anchor is %v, want charge %d", tx.Block(cube.Pos{1, 64, 0}), c.charge)
				}
			})
			if got := dt.slot(0); got.Count() != c.left {
				t.Errorf("slot holds %d glowstone, want %d", got.Count(), c.left)
			}
			if items := dt.itemEntities(); len(items) != c.items {
				t.Errorf("%d items dropped, want %d", len(items), c.items)
			}
			if got := dt.recorder.soundNames(); !slices.Equal(got, c.sounds) {
				t.Errorf("sounds %v, want %v", got, c.sounds)
			}
		})
	}
}

// TestDispenserTurnsDirtIntoMud checks the behaviour of Items.POTION: a water
// bottle turns the dirt in front into mud and leaves a glass bottle in its slot;
// other potions are ejected.
func TestDispenserTurnsDirtIntoMud(t *testing.T) {
	t.Parallel()
	dt := newDispenserTest(t, cube.FaceEast)
	dt.dispense(func(tx *world.Tx, front cube.Pos) { tx.SetBlock(front, block.Dirt{}, nil) }, item.NewStack(item.Potion{Type: potion.Water()}, 1))
	<-dt.w.Exec(func(tx *world.Tx) {
		if _, ok := tx.Block(cube.Pos{1, 64, 0}).(block.Mud); !ok {
			t.Errorf("block in front is %v, want mud", tx.Block(cube.Pos{1, 64, 0}))
		}
	})
	if _, ok := dt.slot(0).Item().(item.GlassBottle); !ok {
		t.Fatalf("slot holds %v, want a glass bottle", dt.slot(0))
	}
	if items := dt.itemEntities(); len(items) != 0 {
		t.Fatalf("%d items dropped, want none", len(items))
	}

	other := newDispenserTest(t, cube.FaceEast)
	other.dispense(func(tx *world.Tx, front cube.Pos) { tx.SetBlock(front, block.Dirt{}, nil) }, item.NewStack(item.Potion{Type: potion.Swiftness()}, 1))
	<-other.w.Exec(func(tx *world.Tx) {
		if _, ok := tx.Block(cube.Pos{1, 64, 0}).(block.Dirt); !ok {
			t.Errorf("block in front is %v, want dirt kept", tx.Block(cube.Pos{1, 64, 0}))
		}
	})
	if items := other.itemEntities(); len(items) != 1 {
		t.Fatalf("%d items dropped for a swiftness potion, want 1", len(items))
	}
}

// TestDispenserPlacesShulkerBoxes checks ShulkerBoxDispenseBehavior: the box is
// placed in the air in front facing the way of the dispenser if there is air
// below it, and up otherwise; it is kept when it cannot be placed. BDS plays
// no click for it.
func TestDispenserPlacesShulkerBoxes(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name   string
		set    func(tx *world.Tx, front cube.Pos)
		placed bool
		facing cube.Face
	}{
		{"floor below", nil, true, cube.FaceUp},
		{"air below", func(tx *world.Tx, front cube.Pos) { tx.SetBlock(front.Side(cube.FaceDown), nil, nil) }, true, cube.FaceEast},
		{"blocked", func(tx *world.Tx, front cube.Pos) {
			tx.SetBlock(front, block.Stone{}, nil)
			tx.SetBlock(front.Side(cube.FaceUp), block.Stone{}, nil)
		}, false, cube.FaceUp},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			dt := newDispenserTest(t, cube.FaceEast)
			dt.dispense(c.set, item.NewStack(block.NewShulkerBox(), 1))
			<-dt.w.Exec(func(tx *world.Tx) {
				box, ok := tx.Block(cube.Pos{1, 64, 0}).(block.ShulkerBox)
				if ok != c.placed || ok && box.Facing != c.facing {
					t.Errorf("block in front is %v, want a shulker box placed %v facing %v", tx.Block(cube.Pos{1, 64, 0}), c.placed, c.facing)
				}
			})
			if kept := !dt.slot(0).Empty(); kept == c.placed {
				t.Errorf("shulker box kept in the dispenser: %v, placed: %v", kept, c.placed)
			}
			if got := dt.recorder.soundNames(); len(got) != 0 {
				t.Errorf("sounds %v, want none", got)
			}
		})
	}
}

// TestDispenserSummonsGolem checks the behaviour of Blocks.CARVED_PUMPKIN: a
// pumpkin is placed in the air in front only if the base of a golem is below it
// (so the pumpkin completes the pattern and the golem appears), and is kept
// otherwise.
func TestDispenserSummonsGolem(t *testing.T) {
	t.Parallel()
	ironGolem := func(tx *world.Tx, front cube.Pos) {
		for _, p := range []cube.Pos{front.Side(cube.FaceDown), front.Side(cube.FaceDown).Add(cube.Pos{0, 0, 1}), front.Side(cube.FaceDown).Add(cube.Pos{0, 0, -1}), front.Add(cube.Pos{0, -2, 0})} {
			tx.SetBlock(p, block.Iron{}, nil)
		}
	}
	dt := newDispenserTest(t, cube.FaceEast)
	dt.dispense(ironGolem, item.NewStack(block.Pumpkin{Carved: true}, 1))
	if golems := dt.entities("minecraft:iron_golem"); len(golems) != 1 {
		t.Fatalf("%d iron golems, want 1", len(golems))
	}
	<-dt.w.Exec(func(tx *world.Tx) {
		if _, ok := tx.Block(cube.Pos{1, 64, 0}).(block.Air); !ok {
			t.Errorf("block in front is %v, want the pumpkin used up by the golem", tx.Block(cube.Pos{1, 64, 0}))
		}
	})
	if !dt.slot(0).Empty() {
		t.Fatalf("slot holds %v, want the pumpkin used", dt.slot(0))
	}

	none := newDispenserTest(t, cube.FaceEast)
	none.dispense(nil, item.NewStack(block.Pumpkin{Carved: true}, 1))
	<-none.w.Exec(func(tx *world.Tx) {
		if _, ok := tx.Block(cube.Pos{1, 64, 0}).(block.Air); !ok {
			t.Errorf("block in front is %v, want no pumpkin placed", tx.Block(cube.Pos{1, 64, 0}))
		}
	})
	if got := none.slot(0); got.Count() != 1 {
		t.Fatalf("slot holds %v, want the pumpkin kept", got)
	}
	if got := none.recorder.soundNames(); !slices.Equal(got, []string{"block.click.fail"}) {
		t.Fatalf("sounds %v, want the failure click", got)
	}
}

// TestDispenserSummonsWither checks the behaviour of Items.WITHER_SKELETON_SKULL:
// the skull is placed in the air in front over a T of soul sand, and with a skull
// on each side the wither appears; not in peaceful.
func TestDispenserSummonsWither(t *testing.T) {
	t.Parallel()
	pattern := func(tx *world.Tx, front cube.Pos) {
		below := front.Side(cube.FaceDown)
		for _, p := range []cube.Pos{below, below.Add(cube.Pos{0, 0, 1}), below.Add(cube.Pos{0, 0, -1}), below.Side(cube.FaceDown)} {
			tx.SetBlock(p, block.SoulSand{}, nil)
		}
		for _, p := range []cube.Pos{front.Add(cube.Pos{0, 0, 1}), front.Add(cube.Pos{0, 0, -1})} {
			tx.SetBlock(p, block.Skull{Type: block.WitherSkeletonSkull()}, nil)
		}
	}
	skull := item.NewStack(block.Skull{Type: block.WitherSkeletonSkull()}, 1)
	dt := newDispenserTest(t, cube.FaceEast)
	dt.dispense(pattern, skull)
	if withers := dt.entities("minecraft:wither"); len(withers) != 1 {
		t.Fatalf("%d withers, want 1", len(withers))
	}
	if !dt.slot(0).Empty() {
		t.Fatalf("slot holds %v, want the skull used", dt.slot(0))
	}

	peaceful := newDispenserTest(t, cube.FaceEast)
	<-peaceful.w.Exec(func(tx *world.Tx) { peaceful.w.SetDifficulty(world.DifficultyPeaceful) })
	peaceful.dispense(pattern, skull)
	if withers := peaceful.entities("minecraft:wither"); len(withers) != 0 {
		t.Fatalf("%d withers in peaceful, want none", len(withers))
	}
	if got := peaceful.slot(0); got.Count() != 1 {
		t.Fatalf("slot holds %v, want the skull kept", got)
	}
}

// TestDispenserKeepsTNTWithoutExplosions checks that the gamerule tnt_explodes
// off makes the TNT behaviour fail: the dispenser keeps the TNT and clicks.
func TestDispenserKeepsTNTWithoutExplosions(t *testing.T) {
	t.Parallel()
	dt := newDispenserTest(t, cube.FaceEast)
	if err := dt.w.SetGameRule("tnt_explodes", false); err != nil {
		t.Fatal(err)
	}
	dt.dispense(nil, item.NewStack(block.TNT{}, 1))
	if tnt := dt.entities("minecraft:tnt"); len(tnt) != 0 {
		t.Fatalf("%d primed TNT, want none", len(tnt))
	}
	if got := dt.slot(0); got.Count() != 1 {
		t.Fatalf("slot holds %v, want the TNT kept", got)
	}
	if got := dt.recorder.soundNames(); !slices.Equal(got, []string{"block.click.fail"}) {
		t.Fatalf("sounds %v, want the failure click", got)
	}
}
