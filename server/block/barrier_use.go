package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// RejectedUseUpdates confirms the unchanged door halves to the player whose
// empty-hand interaction was rejected. These are outgoing snapshots only:
// there is no world mutation, neighbour notification or sound.
func (d IronDoor) RejectedUseUpdates(pos cube.Pos, tx *world.Tx, publish func(cube.Pos, world.Block)) {
	update := blockInteractionUpdate{active: true, explicitOrder: true, ready: tx.Defer(func(*world.Tx) {}).Done()}
	publish(pos, markInteraction(d, update))
	face := cube.FaceUp
	if d.Top {
		face = cube.FaceDown
	}
	otherPos := pos.Side(face)
	if other, ok := tx.Block(otherPos).(IronDoor); ok && other.Top != d.Top {
		update.after, update.afterFace = true, face.Opposite()
		publish(otherPos, markInteraction(other, update))
	}
}

// RejectedUseUpdates confirms the unchanged iron trapdoor without activating it.
func (t IronTrapdoor) RejectedUseUpdates(pos cube.Pos, tx *world.Tx, publish func(cube.Pos, world.Block)) {
	publish(pos, markBlockInteraction(t, tx))
}
