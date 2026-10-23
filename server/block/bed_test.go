package block

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

func TestBedOccupancyFollowsPairedHalf(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	t.Cleanup(func() { _ = w.Close() })
	footPos, headPos := cube.Pos{0, 64, 0}, cube.Pos{0, 64, 1}
	foot := Bed{Facing: cube.South, Colour: item.ColourRed()}
	head := foot
	head.Head = true
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(footPos, foot, nil)
		tx.SetBlock(headPos, head, nil)
	})
	w.AdvanceTick()
	for _, occupied := range []bool{true, false} {
		head.Occupied = occupied
		<-w.Exec(func(tx *world.Tx) { tx.SetBlock(headPos, head, nil) })
		w.AdvanceTick()
		<-w.Exec(func(tx *world.Tx) {
			got := tx.Block(footPos).(Bed)
			if got.Occupied != occupied || got.Head || got.Colour != foot.Colour || got.Sleeper != nil {
				t.Fatalf("paired bed occupancy transition lost state: %#v", got)
			}
			if tx.Block(headPos).(Bed).Occupied != occupied {
				t.Fatal("foot update reversed head occupancy")
			}
		})
	}
	<-w.Exec(func(tx *world.Tx) {
		head.Occupied = true
		tx.SetBlock(headPos, head, nil)
		head.NeighbourUpdateTick(headPos, headPos.Side(cube.FaceUp), tx)
		if !tx.Block(headPos).(Bed).Occupied {
			t.Fatal("unrelated neighbour overwrote occupied head from stale foot")
		}
	})
}

// TestBedCanonicalStateKeepsItsColour checks the Java state of a bed names
// its colour, which Bedrock keeps in the block actor, so the bed is in the
// #beds tag.
func TestBedCanonicalStateKeepsItsColour(t *testing.T) {
	bed := Bed{Facing: cube.East, Colour: item.ColourLightBlue(), Head: true}
	state, err := world.CanonicalStateOf(bed)
	if err != nil {
		t.Fatal(err)
	}
	if state.Name != "minecraft:light_blue_bed" || state.Properties["facing"] != "east" || state.Properties["part"] != "head" {
		t.Fatalf("bed has Java state %v, want a light blue head facing east", state)
	}
	if in, err := gamedata.BlockTagContains("minecraft:beds", state.Name); err != nil || !in {
		t.Fatalf("%s is not in #beds: %v", state.Name, err)
	}
	back, err := bed.WithCanonicalState(state.Name, state.Properties)
	if err != nil || back.(Bed).Colour != bed.Colour || back.(Bed).Facing != bed.Facing || !back.(Bed).Head {
		t.Fatalf("bed state %v did not round trip: %v, %v", state, back, err)
	}
}
