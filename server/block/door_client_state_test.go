package block

import (
	"testing"

	"github.com/df-mc/dragonfly/server/world"
)

// Native packets retain the upper half's unused open bit. Ordinary toggles
// leave that bit unchanged; the lower half determines visible opening.
func TestUpperDoorHalfRetainsNativeOpenBit(t *testing.T) {
	br := world.DefaultBlockRegistry
	br.Finalize()
	hash := func(b world.Block) uint32 {
		h, _ := br.RuntimeIDToHash(br.BlockRuntimeID(b))
		return h
	}
	for _, door := range []world.Block{
		WoodDoor{Wood: OakWood(), Top: true, Open: true},
		IronDoor{Top: true, Open: true},
		CopperDoor{Top: true, Open: true},
	} {
		var closedTop, openBottom, closedBottom world.Block
		switch d := door.(type) {
		case WoodDoor:
			closedTop, openBottom, closedBottom = WoodDoor{Wood: d.Wood, Top: true}, WoodDoor{Wood: d.Wood, Open: true}, WoodDoor{Wood: d.Wood}
		case IronDoor:
			closedTop, openBottom, closedBottom = IronDoor{Top: true}, IronDoor{Open: true}, IronDoor{}
		case CopperDoor:
			closedTop, openBottom, closedBottom = CopperDoor{Top: true}, CopperDoor{Open: true}, CopperDoor{}
		}
		if hash(door) == hash(closedTop) {
			t.Errorf("%T: upper half lost its retained native open bit", door)
		}
		if rid, ok := br.HashToRuntimeID(hash(door)); !ok || rid != br.BlockRuntimeID(door) {
			t.Errorf("%T: upper network state does not round trip", door)
		}
		if hash(openBottom) == hash(closedBottom) {
			t.Errorf("%T: open lower half is sent closed", door)
		}
		if br.BlockRuntimeID(door) == br.BlockRuntimeID(closedTop) {
			t.Errorf("%T: server lost the open state of the upper half", door)
		}
	}
}
