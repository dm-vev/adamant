package entity_test

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// ridePig spawns a saddled pig on stone and seats a survival player on it
// holding the item passed, looking along the yaw given.
func ridePig(t *testing.T, held world.Item, yaw float64) (*world.World, *world.EntityHandle, *world.EntityHandle) {
	t.Helper()
	w := traderTestWorld(t)
	var pig, rider *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		// The pig walks up to 30 blocks towards positive x.
		for x := -20; x <= 50; x++ {
			for z := -20; z <= 20; z++ {
				tx.SetBlock(cube.Pos{x, 63, z}, block.Stone{}, nil)
			}
		}
		pig = spawnAnimal(t, tx, "minecraft:pig", .5, map[string]any{"Saddle": byte(1)})
		p := addSurvivalPlayer(tx, mgl64.Vec3{.5, 64, -1.5})
		p.SetHeldItems(item.NewStack(held, 1), item.Stack{})
		p.Move(mgl64.Vec3{}, yaw-p.Rotation().Yaw(), -p.Rotation().Pitch())
		e, _ := pig.Entity(tx)
		if !e.(*entity.Pig).Interact(tx, p, &item.UseContext{}) {
			t.Fatal("player could not ride the saddled pig")
		}
		rider = p.H()
	})
	return w, pig, rider
}

// TestPigsAreSteeredWithCarrotsOnSticks checks Pig.tickRidden and
// getRiddenSpeed: a pig walks the way the player riding it looks while the
// player holds a carrot on a stick, at 0.225 times its speed.
func TestPigsAreSteeredWithCarrotsOnSticks(t *testing.T) {
	// A yaw of -90 looks towards positive x.
	w, pig, _ := ridePig(t, item.CarrotOnAStick{}, -90)
	start := aiEntityPos(w, pig)
	stepWorld(t, w, 40)
	moved := aiEntityPos(w, pig).Sub(start)
	// The pig accelerates to 0.25*0.225/(1-0.6*0.91) blocks a tick.
	if want := .25 * .225 / (1 - .6*.91) * 40 * .8; moved[0] < want || moved[0] > want*1.4 || math.Abs(moved[2]) > .5 {
		t.Fatalf("steered pig moved %.2f, want about %.2f blocks along positive x", moved, want/.8)
	}
	<-w.Exec(func(tx *world.Tx) {
		e, _ := pig.Entity(tx)
		if yaw := e.Rotation().Yaw(); math.Abs(yaw+90) > 1e-6 {
			t.Fatalf("steered pig looks along yaw %.2f, want the rider's -90", yaw)
		}
	})
}

// TestPigsWanderWithoutCarrotsOnSticks checks Pig.getControllingPassenger:
// without a carrot on a stick the player riding a pig does not steer it.
func TestPigsWanderWithoutCarrotsOnSticks(t *testing.T) {
	w, pig, rider := ridePig(t, item.Stick{}, -90)
	<-w.Exec(func(tx *world.Tx) {
		e, _ := pig.Entity(tx)
		r, _ := rider.Entity(tx)
		if e.(*entity.Pig).ControlsVehicle(r) {
			t.Fatal("player without a carrot on a stick steers the pig")
		}
	})
	start := aiEntityPos(w, pig)
	stepWorld(t, w, 40)
	if moved := aiEntityPos(w, pig).Sub(start); moved[0] > 3 {
		t.Fatalf("pig ridden without a carrot on a stick walked %.2f blocks the way its rider looks", moved[0])
	}
}

// TestCarrotsOnSticksBoostPigs checks FoodOnAStickItem.use and
// ItemBasedSteering: using a carrot on a stick boosts the pig, which runs
// faster, once until the boost ends, and costs the carrot 7 durability. A
// boost of 140 to 980 ticks speeds the pig up along a sine, so between ticks
// 70 and 110 it runs at least 1.2 times as fast.
func TestCarrotsOnSticksBoostPigs(t *testing.T) {
	w, pig, rider := ridePig(t, item.CarrotOnAStick{}, -90)
	stepWorld(t, w, 20)
	use := func() (durability int) {
		<-w.Exec(func(tx *world.Tx) {
			r, _ := rider.Entity(tx)
			p := r.(*player.Player)
			p.UseItem()
			main, _ := p.HeldItems()
			durability = main.Durability()
		})
		return durability
	}
	if d := use(); d != 25-7 {
		t.Fatalf("carrot on a stick has %d durability after boosting, want %d", d, 25-7)
	}
	if d := use(); d != 25-7 {
		t.Fatalf("boosting a boosted pig took durability, %d left", d)
	}
	stepWorld(t, w, 70)
	start := aiEntityPos(w, pig)
	stepWorld(t, w, 40)
	if moved, want := aiEntityPos(w, pig).Sub(start), .25*.225/(1-.6*.91)*40*1.15; moved[0] < want {
		t.Fatalf("boosted pig moved %.2f blocks in two seconds, want at least %.2f", moved[0], want)
	}
}

// TestCarrotsOnSticksBreakIntoFishingRods checks the carrot on a stick turns
// back into a fishing rod once it is used up.
func TestCarrotsOnSticksBreakIntoFishingRods(t *testing.T) {
	w, _, rider := ridePig(t, item.CarrotOnAStick{}, -90)
	<-w.Exec(func(tx *world.Tx) {
		r, _ := rider.Entity(tx)
		p := r.(*player.Player)
		p.SetHeldItems(item.NewStack(item.CarrotOnAStick{}, 1).Damage(20), item.Stack{})
		p.UseItem()
		if main, _ := p.HeldItems(); main.Item() != (item.FishingRod{}) {
			t.Fatalf("worn out carrot on a stick turned into %v, want a fishing rod", main.Item())
		}
	})
}
