package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// Placed finishes an explicit copper transformation. Neighbour callbacks
// cannot do this: native API replacements with a different copper variant
// invalidate the pair, whereas waxing, scraping and oxidation change both.
func (d CopperDoor) Placed(pos cube.Pos, tx *world.Tx, previous world.Block) {
	if !d.pairTransform {
		return
	}
	old, ok := previous.(CopperDoor)
	if !ok || old.Top != d.Top || (old.Oxidation == d.Oxidation && old.Waxed == d.Waxed) {
		return
	}
	otherPos := pos.Side(cube.FaceUp)
	if d.Top {
		otherPos = pos.Side(cube.FaceDown)
	}
	other, ok := tx.Block(otherPos).(CopperDoor)
	if !ok || other.Top == d.Top || other.Oxidation != old.Oxidation || other.Waxed != old.Waxed {
		return
	}
	other.Oxidation, other.Waxed = d.Oxidation, d.Waxed
	tx.SetBlock(otherPos, other, nil)
}
