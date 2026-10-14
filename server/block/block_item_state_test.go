package block

import (
	"fmt"
	"testing"

	"github.com/df-mc/dragonfly/server/world"
)

// TestBlockItemsAreRegisteredStates checks every block registered as an item
// is one of its block's registered states. Sending a stack to a client looks
// the item's block up by state, so any other value panics the world as soon
// as a player is given the item.
func TestBlockItemsAreRegisteredStates(t *testing.T) {
	world.DefaultBlockRegistry.Finalize()
	for _, it := range world.Items() {
		b, ok := it.(world.Block)
		if !ok {
			continue
		}
		name, meta := it.EncodeItem()
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("item %s:%d (%T) is no registered block state: %v", name, meta, it, fmt.Sprint(r))
				}
			}()
			world.BlockRuntimeID(b)
		}()
	}
}
