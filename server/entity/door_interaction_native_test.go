package entity

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// The native lower-open and retained upper-open bits can disagree in either
// direction. Geometry, pathfinding and AI interaction must read the lower bit.
func TestDoorInteractionReadsLowerOpenBit(t *testing.T) {
	for _, copper := range []bool{false, true} {
		for _, lowerOpen := range []bool{false, true} {
			for _, upperOpen := range []bool{false, true} {
				t.Run(fmt.Sprintf("copper%t/lower%t/upper%t", copper, lowerOpen, upperOpen), func(t *testing.T) {
					w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
					defer w.Close()
					<-w.Exec(func(tx *world.Tx) {
						pos := cube.Pos{4, 65, 4}
						upper := pos.Side(cube.FaceUp)
						var lowerBlock, upperBlock world.Block = block.WoodDoor{Open: lowerOpen}, block.WoodDoor{Top: true, Open: upperOpen}
						if copper {
							lowerBlock, upperBlock = block.CopperDoor{Open: lowerOpen}, block.CopperDoor{Top: true, Open: upperOpen}
						}
						tx.SetBlock(pos.Side(cube.FaceDown), block.Stone{}, nil)
						tx.SetBlock(pos, lowerBlock, nil)
						tx.SetBlock(upper, upperBlock, nil)
						if value, ok := doorOpen(tx, upper); !ok || value != lowerOpen {
							t.Fatal("AI read the retained upper bit as the opening state")
						}
						if !reflect.DeepEqual(lowerBlock.Model().BBox(pos, tx), upperBlock.Model().BBox(upper, tx)) {
							t.Fatal("upper collision shape disagrees with lower opening")
						}
						if groundStepCost(tx, pos, GroundProfile{}) != groundStepCost(tx, upper, GroundProfile{}) {
							t.Fatal("door path cost depends on its unused upper bit")
						}
						setDoorOpen(tx, upper, !lowerOpen)
						if value, ok := doorOpen(tx, pos); !ok || value == lowerOpen {
							t.Fatal("AI upper-half interaction did not toggle the lower half")
						}
						if tx.Block(upper) != upperBlock {
							t.Fatal("AI interaction overwrote the retained upper permutation")
						}
					})
				})
			}
		}
	}
}
