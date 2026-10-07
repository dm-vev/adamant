package block

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
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
