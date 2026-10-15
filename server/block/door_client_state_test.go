package block

import (
	"testing"

	"github.com/df-mc/dragonfly/server/world"
)

// TestUpperDoorHalfSentClosed checks the network state of door halves as BDS
// sends them: the upper half never carries open_bit, while the lower half
// does and the server keeps the open state on both.
func TestUpperDoorHalfSentClosed(t *testing.T) {
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
		if hash(door) != hash(closedTop) {
			t.Errorf("%T: open upper half is sent open", door)
		}
		if hash(openBottom) == hash(closedBottom) {
			t.Errorf("%T: open lower half is sent closed", door)
		}
		if br.BlockRuntimeID(door) == br.BlockRuntimeID(closedTop) {
			t.Errorf("%T: server lost the open state of the upper half", door)
		}
	}
}
