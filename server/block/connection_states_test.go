package block

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// connectionStatesFloor lays a floor of stone below y 64 from -4 to 8 on
// both axes.
func connectionStatesFloor(tx *world.Tx) {
	for x := -4; x <= 8; x++ {
		for z := -4; z <= 8; z++ {
			tx.SetBlock(cube.Pos{x, 63, z}, Stone{}, nil)
		}
	}
}

// wire returns redstone wire of no power connected as the sides say, in
// the order north, east, south, west, each n, s or u.
func wire(sides string) RedstoneWire {
	conn := func(c byte) WireConnection {
		switch c {
		case 's':
			return SideWireConnection()
		case 'u':
			return UpWireConnection()
		}
		return NoWireConnection()
	}
	return RedstoneWire{North: conn(sides[0]), East: conn(sides[1]), South: conn(sides[2]), West: conn(sides[3])}
}

// TestRedstoneWireConnectsAsJava places redstone wire as a player does,
// the scene of testdata/connection_states.commands of the session: a line
// with a corner and a step up onto a block, and wire alone. Each connects
// as Java's RedstoneWireBlock computes it, which is how BDS 1.26.61 left
// the same scene saved by 1.26.52 once it loaded it.
func TestRedstoneWireConnectsAsJava(t *testing.T) {
	t.Parallel()
	w := world.Config{Synchronous: true}.New()
	defer w.Close()

	runWorld(w, func(tx *world.Tx) {
		connectionStatesFloor(tx)
		for _, pos := range []cube.Pos{{2, 64, 0}, {3, 64, 0}, {4, 64, 0}, {4, 64, 1}} {
			tx.SetBlock(pos, redstoneWirePlaced(tx, pos), nil)
		}
		tx.SetBlock(cube.Pos{5, 64, 0}, Stone{}, nil)
		for _, pos := range []cube.Pos{{5, 65, 0}, {2, 64, 6}} {
			tx.SetBlock(pos, redstoneWirePlaced(tx, pos), nil)
		}
	})
	w.AdvanceTick()
	runWorld(w, func(tx *world.Tx) {
		for pos, want := range map[cube.Pos]RedstoneWire{
			{2, 64, 0}: wire("nsns"),
			{3, 64, 0}: wire("nsns"),
			// The corner, which climbs the block to the east.
			{4, 64, 0}: wire("nuss"),
			{4, 64, 1}: wire("snsn"),
			// The wire on the block descends to the west and runs on east.
			{5, 65, 0}: wire("nsns"),
			// Wire alone runs to all four sides.
			{2, 64, 6}: wire("ssss"),
		} {
			if got := tx.Block(pos); got != want {
				t.Errorf("wire at %v = %#v, want %#v", pos, got, want)
			}
		}
	})
}

// TestRedstoneWireShapesTheWireItClimbsTo places wire on a block beside
// wire one block lower, after both: the lower wire, at a corner of the
// upper one and no neighbour of it, turns up the block, as Java's
// updateIndirectNeighbourShapes shapes it. Breaking the upper wire turns it
// back.
func TestRedstoneWireShapesTheWireItClimbsTo(t *testing.T) {
	t.Parallel()
	w := world.Config{Synchronous: true}.New()
	defer w.Close()

	low, block, high := cube.Pos{0, 64, 0}, cube.Pos{1, 64, 0}, cube.Pos{1, 65, 0}
	runWorld(w, func(tx *world.Tx) {
		connectionStatesFloor(tx)
		tx.SetBlock(low, redstoneWirePlaced(tx, low), nil)
		tx.SetBlock(block, Stone{}, nil)
	})
	w.AdvanceTick()
	runWorld(w, func(tx *world.Tx) {
		if got := tx.Block(low); got != wire("ssss") {
			t.Fatalf("wire alone = %#v, want it running to all four sides", got)
		}
		tx.SetBlock(high, redstoneWirePlaced(tx, high), nil)
	})
	w.AdvanceTick()
	runWorld(w, func(tx *world.Tx) {
		if got := tx.Block(low); got != wire("nuns") {
			t.Errorf("lower wire = %#v, want it climbing east", got)
		}
		if got := tx.Block(high); got != wire("nsns") {
			t.Errorf("upper wire = %#v, want it descending west", got)
		}
		tx.SetBlock(high, nil, nil)
	})
	w.AdvanceTick()
	runWorld(w, func(tx *world.Tx) {
		if got := tx.Block(low); got != wire("ssss") {
			t.Errorf("lower wire once the upper went = %#v, want it running to all four sides", got)
		}
	})
}

// TestFireBurnsOnTheSidesOfBlocksThatBurn starts fire beside logs, as
// Java's FireBlock places it: fire on a floor of stone burns on no side;
// fire with no floor burns on the sides of the logs, and follows them as
// they go.
func TestFireBurnsOnTheSidesOfBlocksThatBurn(t *testing.T) {
	t.Parallel()
	w := world.Config{Synchronous: true}.New()
	defer w.Close()

	floor, floating := cube.Pos{1, 64, 4}, cube.Pos{1, 70, 4}
	runWorld(w, func(tx *world.Tx) {
		connectionStatesFloor(tx)
		tx.SetBlock(floor.Side(cube.FaceWest), Log{Wood: OakWood()}, nil)
		tx.SetBlock(floating.Side(cube.FaceWest), Log{Wood: OakWood()}, nil)
		tx.SetBlock(floating.Side(cube.FaceNorth), Log{Wood: OakWood()}, nil)
		Fire{}.Start(tx, floor)
		Fire{}.Start(tx, floating)
		if got := tx.Block(floor); got != (Fire{}) {
			t.Errorf("fire on stone = %#v, want it burning on no side", got)
		}
		if got := tx.Block(floating); got != (Fire{West: true, North: true}) {
			t.Errorf("fire with no floor = %#v, want it burning west and north", got)
		}
		tx.SetBlock(floating.Side(cube.FaceNorth), nil, nil)
	})
	w.AdvanceTick()
	runWorld(w, func(tx *world.Tx) {
		if got, ok := tx.Block(floating).(Fire); !ok || !got.West || got.North {
			t.Errorf("fire once the log to the north went = %#v, want it burning west alone", tx.Block(floating))
		}
	})
}

// TestChorusPlantConnectsAsJava grows the chorus plant of the scene of the
// capture of BDS: a stem on end stone and a branch. Each connects to the
// plants beside it and the stem to the end stone below, as Java's
// ChorusPlantBlock does, and as BDS 1.26.61 sends them.
func TestChorusPlantConnectsAsJava(t *testing.T) {
	t.Parallel()
	w := world.Config{Dim: world.End, Synchronous: true}.New()
	defer w.Close()

	stem, top, branch := cube.Pos{4, 64, 4}, cube.Pos{4, 65, 4}, cube.Pos{5, 65, 4}
	runWorld(w, func(tx *world.Tx) {
		connectionStatesFloor(tx)
		tx.SetBlock(stem.Side(cube.FaceDown), EndStone{}, nil)
		for _, pos := range []cube.Pos{stem, top, branch} {
			tx.SetBlock(pos, chorusPlantAt(tx, pos), nil)
		}
	})
	w.AdvanceTick()
	runWorld(w, func(tx *world.Tx) {
		for pos, want := range map[cube.Pos]ChorusPlant{
			stem:   {Down: true, Up: true},
			top:    {Down: true, East: true},
			branch: {West: true},
		} {
			if got := tx.Block(pos); got != want {
				t.Errorf("chorus plant at %v = %#v, want %#v", pos, got, want)
			}
		}
	})
}
