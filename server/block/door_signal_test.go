package block

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// TestDoorsOnlyFollowSignalChanges checks DoorBlock and TrapDoorBlock: a door
// or trapdoor opened by hand stays open through neighbour updates, and power
// switching on opens it and switching off closes it.
func TestDoorsOnlyFollowSignalChanges(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	door, trapdoor := cube.Pos{0, 64, 0}, cube.Pos{4, 64, 0}
	ticks := func() {
		for range 3 {
			w.AdvanceTick()
		}
	}
	open := func(tx *world.Tx) (bool, bool, bool) {
		return tx.Block(door).(WoodDoor).Open, tx.Block(door.Side(cube.FaceUp)).(WoodDoor).Open, tx.Block(trapdoor).(WoodTrapdoor).Open
	}
	runWorld(w, func(tx *world.Tx) {
		for x := -1; x <= 6; x++ {
			tx.SetBlock(cube.Pos{x, 63, 0}, Stone{}, nil)
		}
		tx.SetBlock(door, WoodDoor{Wood: OakWood()}, nil)
		tx.SetBlock(door.Side(cube.FaceUp), WoodDoor{Wood: OakWood(), Top: true}, nil)
		tx.SetBlock(trapdoor, WoodTrapdoor{Wood: OakWood()}, nil)
	})
	ticks()
	runWorld(w, func(tx *world.Tx) {
		tx.Block(door).(WoodDoor).Activate(door, cube.FaceNorth, tx, nil, nil)
		tx.Block(trapdoor).(WoodTrapdoor).Activate(trapdoor, cube.FaceNorth, tx, nil, nil)
	})
	ticks()
	runWorld(w, func(tx *world.Tx) {
		tx.SetBlock(door.Side(cube.FaceEast), Stone{}, nil)
		tx.SetBlock(trapdoor.Side(cube.FaceEast), Stone{}, nil)
	})
	ticks()
	runWorld(w, func(tx *world.Tx) {
		if lower, upper, trap := open(tx); !lower || !upper || !trap {
			t.Fatalf("hand-opened door %v/%v and trapdoor %v closed without a signal", lower, upper, trap)
		}
		tx.Block(door).(WoodDoor).Activate(door, cube.FaceNorth, tx, nil, nil)
		tx.Block(trapdoor).(WoodTrapdoor).Activate(trapdoor, cube.FaceNorth, tx, nil, nil)
	})
	ticks()
	runWorld(w, func(tx *world.Tx) {
		tx.SetBlock(door.Side(cube.FaceWest), RedstoneBlock{}, nil)
		tx.SetBlock(trapdoor.Side(cube.FaceWest), RedstoneBlock{}, nil)
	})
	ticks()
	runWorld(w, func(tx *world.Tx) {
		if lower, upper, trap := open(tx); !lower || !upper || !trap {
			t.Fatalf("powering left door %v/%v and trapdoor %v closed", lower, upper, trap)
		}
		tx.SetBlock(door.Side(cube.FaceWest), Air{}, nil)
		tx.SetBlock(trapdoor.Side(cube.FaceWest), Air{}, nil)
	})
	ticks()
	runWorld(w, func(tx *world.Tx) {
		if lower, upper, trap := open(tx); lower || upper || trap {
			t.Fatalf("unpowering left door %v/%v and trapdoor %v open", lower, upper, trap)
		}
	})
}
