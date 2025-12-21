package block

import (
	"fmt"
	"github.com/df-mc/dragonfly/server/world"
)

// Element is a chemistry element block.
type Element struct {
	solid

	Number int
}

// EncodeItem ...
func (e Element) EncodeItem() (name string, meta int16) {
	return fmt.Sprintf("minecraft:element_%d", e.Number), 0
}

// EncodeBlock ...
func (e Element) EncodeBlock() (string, map[string]any) {
	return fmt.Sprintf("minecraft:element_%d", e.Number), nil
}

// allElements returns all element blocks.
func allElements() (b []world.Block) {
	for i := 0; i <= 118; i++ {
		b = append(b, Element{Number: i})
	}
	return
}
