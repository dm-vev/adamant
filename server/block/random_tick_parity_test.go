package block

import (
	"math/rand/v2"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

func randomTickWorld(t *testing.T) *world.World {
	t.Helper()
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Generator: world.NopGenerator{}, Provider: world.NopProvider{}}.New()
	t.Cleanup(func() { _ = w.Close() })
	return w
}

// TestCopperSlabsAndStairsOxidise checks WeatheringCopperSlabBlock and
// WeatheringCopperStairBlock: cut copper slabs and stairs oxidise as copper
// blocks do, and waxed ones never tick.
func TestCopperSlabsAndStairsOxidise(t *testing.T) {
	w := randomTickWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		slab := Slab{Block: Copper{Type: CutCopper()}}
		stairs := Stairs{Block: Copper{Type: CutCopper()}}
		tx.SetBlock(cube.Pos{0, 64, 0}, slab, nil)
		tx.SetBlock(cube.Pos{10, 64, 0}, stairs, nil)
		r := rand.New(rand.NewPCG(1, 2))
		for range 2000 {
			if s, ok := tx.Block(cube.Pos{0, 64, 0}).(Slab); ok {
				s.RandomTick(cube.Pos{0, 64, 0}, tx, r)
			}
			if s, ok := tx.Block(cube.Pos{10, 64, 0}).(Stairs); ok {
				s.RandomTick(cube.Pos{10, 64, 0}, tx, r)
			}
		}
		if got := tx.Block(cube.Pos{0, 64, 0}).(Slab).OxidationLevel(); got == UnoxidisedOxidation() {
			t.Fatal("a cut copper slab did not oxidise in 2000 random ticks")
		}
		if got := tx.Block(cube.Pos{10, 64, 0}).(Stairs).OxidationLevel(); got == UnoxidisedOxidation() {
			t.Fatal("cut copper stairs did not oxidise in 2000 random ticks")
		}
		if (Slab{Block: Copper{Type: CutCopper(), Waxed: true}}).RandomlyTicks() || (Slab{Block: Planks{}}).RandomlyTicks() {
			t.Fatal("a waxed copper or wooden slab ticks randomly")
		}
	})
}

// TestIceMeltsByBlockLight checks IceBlock.randomTick: ice melts into water
// where block light is above 10, and stays where it is darker.
func TestIceMeltsByBlockLight(t *testing.T) {
	w := randomTickWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		lit, dark := cube.Pos{0, 64, 0}, cube.Pos{20, 64, 0}
		tx.SetBlock(lit, Ice{}, nil)
		tx.SetBlock(dark, Ice{}, nil)
		tx.SetBlock(lit.Side(cube.FaceEast), Glowstone{}, nil)
		r := rand.New(rand.NewPCG(1, 2))
		Ice{}.RandomTick(lit, tx, r)
		Ice{}.RandomTick(dark, tx, r)
		if _, ok := tx.Block(lit).(Water); !ok {
			t.Fatalf("ice next to glowstone became %T, want water", tx.Block(lit))
		}
		if _, ok := tx.Block(dark).(Ice); !ok {
			t.Fatalf("ice in the dark became %T, want ice", tx.Block(dark))
		}
	})
}

// TestGrassFollowsJava checks SpreadingSnowyDirtBlock: grass under a block
// that lets no light through turns into dirt, while grass in the dark under
// open air stays, and lit grass spreads to the dirt around it.
func TestGrassFollowsJava(t *testing.T) {
	w := randomTickWorld(t)
	w.SetTime(6000)
	w.StopTime()
	<-w.Exec(func(tx *world.Tx) {
		covered := cube.Pos{0, 64, 0}
		tx.SetBlock(covered, Grass{}, nil)
		tx.SetBlock(covered.Side(cube.FaceUp), Stone{}, nil)
		r := rand.New(rand.NewPCG(1, 2))
		Grass{}.RandomTick(covered, tx, r)
		if _, ok := tx.Block(covered).(Dirt); !ok {
			t.Fatalf("grass under stone became %T, want dirt", tx.Block(covered))
		}

		for x := 10; x <= 14; x++ {
			for z := -2; z <= 2; z++ {
				tx.SetBlock(cube.Pos{x, 64, z}, Dirt{}, nil)
			}
		}
		tx.SetBlock(cube.Pos{12, 64, 0}, Grass{}, nil)
		for range 200 {
			Grass{}.RandomTick(cube.Pos{12, 64, 0}, tx, r)
		}
		spread := 0
		for x := 10; x <= 14; x++ {
			for z := -2; z <= 2; z++ {
				if _, ok := tx.Block(cube.Pos{x, 64, z}).(Grass); ok {
					spread++
				}
			}
		}
		if spread <= 1 {
			t.Fatal("lit grass did not spread to the dirt around it")
		}

		tx.SetBlock(cube.Pos{30, 64, 0}, Nylium{}, nil)
		tx.SetBlock(cube.Pos{30, 65, 0}, Stone{}, nil)
		Nylium{}.RandomTick(cube.Pos{30, 64, 0}, tx, r)
		if _, ok := tx.Block(cube.Pos{30, 64, 0}).(Netherrack); !ok {
			t.Fatalf("covered nylium became %T, want netherrack", tx.Block(cube.Pos{30, 64, 0}))
		}
	})
}

// TestVinesGrowFromTheirTip checks GrowingPlantHeadBlock.randomTick: the tip
// of cave vines grows down until it is 25 old, the rest does not.
func TestVinesGrowFromTheirTip(t *testing.T) {
	w := randomTickWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{0, 100, 0}, Stone{}, nil)
		tx.SetBlock(cube.Pos{0, 99, 0}, CaveVines{Age: 20}, nil)
		r := rand.New(rand.NewPCG(1, 2))
		for range 400 {
			for y := 99; y > 60; y-- {
				if v, ok := tx.Block(cube.Pos{0, y, 0}).(CaveVines); ok {
					v.RandomTick(cube.Pos{0, y, 0}, tx, r)
				}
			}
		}
		length := 0
		for y := 99; y > 60; y-- {
			if _, ok := tx.Block(cube.Pos{0, y, 0}).(CaveVines); ok {
				length++
			}
		}
		if length != 6 {
			t.Fatalf("cave vines of age 20 grew to %d blocks, want 6 as they stop at age 25", length)
		}
	})
}

// TestLeavesStayNearStrippedLogs checks the DISTANCE of LeavesBlock: leaves
// within six steps of any log, stripped ones too, do not decay, and those
// further away do.
func TestLeavesStayNearStrippedLogs(t *testing.T) {
	w := randomTickWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{0, 64, 0}, Log{Wood: OakWood(), Stripped: true}, nil)
		for x := 1; x <= 8; x++ {
			tx.SetBlock(cube.Pos{x, 64, 0}, Leaves{}, nil)
		}
		if !leavesNearLog(cube.Pos{6, 64, 0}, tx) {
			t.Fatal("a leaf six steps from a stripped log would decay")
		}
		if leavesNearLog(cube.Pos{7, 64, 0}, tx) {
			t.Fatal("a leaf seven steps from a log would stay")
		}
	})
}
