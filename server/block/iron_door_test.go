package block

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

func TestIronDoorStatesRoundTrip(t *testing.T) {
	for _, b := range append(allIronDoors(), allIronTrapdoors()...) {
		name, props := b.EncodeBlock()
		got, ok := world.BlockByName(name, props)
		if !ok || got != b {
			t.Fatalf("%s %v decoded to %#v", name, props, got)
		}
	}
}

func TestIronDoorOpensOnlyWithRedstone(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	<-w.Exec(func(tx *world.Tx) {
		base := cube.Pos{0, 64, 0}
		tx.SetBlock(base.Side(cube.FaceDown), Stone{}, nil)
		tx.SetBlock(base, IronDoor{Facing: cube.North}, nil)
		tx.SetBlock(base.Side(cube.FaceUp), IronDoor{Facing: cube.North, Top: true}, nil)
		if _, ok := any(IronDoor{}).(Activatable); ok {
			t.Fatal("iron doors must not be activatable by hand")
		}
		if _, ok := any(IronTrapdoor{}).(Activatable); ok {
			t.Fatal("iron trapdoors must not be activatable by hand")
		}

		lever := base.Side(cube.FaceWest)
		tx.SetBlock(lever.Side(cube.FaceDown), Stone{}, nil)
		tx.SetBlock(lever, RedstoneBlock{}, nil)
		for range 4 {
			w.AdvanceTick()
		}
		bottom, top := tx.Block(base).(IronDoor), tx.Block(base.Side(cube.FaceUp)).(IronDoor)
		if !bottom.Open || !top.Open {
			t.Fatalf("powered iron door open bottom=%v top=%v", bottom.Open, top.Open)
		}
		tx.SetBlock(lever, nil, nil)
		for range 4 {
			w.AdvanceTick()
		}
		if tx.Block(base).(IronDoor).Open || tx.Block(base.Side(cube.FaceUp)).(IronDoor).Open {
			t.Fatal("iron door stayed open after power was removed")
		}
	})
}
