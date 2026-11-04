package block

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// TestDiscPressIsAPlainBlock checks the disc press of 1.26.60 is a block of
// each of its four states, and an item, that drops itself.
func TestDiscPressIsAPlainBlock(t *testing.T) {
	t.Parallel()
	for _, d := range cube.Directions() {
		b, ok := world.BlockByName("minecraft:disc_press", map[string]any{"minecraft:cardinal_direction": d.String()})
		if !ok || b != (DiscPress{Facing: d}) {
			t.Errorf("disc press facing %v is %#v, %v", d, b, ok)
		}
	}
	if it, ok := world.ItemByName("minecraft:disc_press", 0); !ok || it != (DiscPress{}) {
		t.Fatalf("disc press item %#v, %v", it, ok)
	}
	if drops := (DiscPress{Facing: cube.East}).BreakInfo().Drops(nil, nil); len(drops) != 1 || drops[0].Item() != (DiscPress{}) {
		t.Errorf("a disc press drops %v", drops)
	}
}
