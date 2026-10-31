package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

func (d WoodDoor) Filled(pos cube.Pos, tx *world.Tx, _ world.Block)   { doorFilled(d, pos, tx) }
func (d CopperDoor) Filled(pos cube.Pos, tx *world.Tx, _ world.Block) { doorFilled(d, pos, tx) }
func (d IronDoor) Filled(pos cube.Pos, tx *world.Tx, _ world.Block)   { doorFilled(d, pos, tx) }

func (d WoodDoor) CommandFillUpdate(tx *world.Tx) world.Block     { return markBlockInteraction(d, tx) }
func (d CopperDoor) CommandFillUpdate(tx *world.Tx) world.Block   { return markBlockInteraction(d, tx) }
func (d IronDoor) CommandFillUpdate(tx *world.Tx) world.Block     { return markBlockInteraction(d, tx) }
func (t WoodTrapdoor) CommandFillUpdate(tx *world.Tx) world.Block { return markBlockInteraction(t, tx) }
func (t CopperTrapdoor) CommandFillUpdate(tx *world.Tx) world.Block {
	return markBlockInteraction(t, tx)
}
func (t IronTrapdoor) CommandFillUpdate(tx *world.Tx) world.Block { return markBlockInteraction(t, tx) }
func (f WoodFenceGate) CommandFillUpdate(tx *world.Tx) world.Block {
	return markBlockInteraction(f, tx)
}

// Command placement copies the lower permutation's open bit to its partner.
// Ordinary native toggles do not execute this command callback, retaining
// that bit thereafter. Both behaviours are retained in the BDS pulse corpus.
func doorFilled(b world.Block, pos cube.Pos, tx *world.Tx) {
	open, face, _ := barrierOpen(b)
	otherPos := pos.Side(face)
	other := tx.Block(otherPos)
	name, _ := b.EncodeBlock()
	otherName, _ := other.EncodeBlock()
	if name != otherName {
		return
	}
	otherOpen, _, _ := barrierOpen(other)
	if open == otherOpen {
		return
	}
	ready := b.(interface{ BlockUpdateReady() <-chan struct{} }).BlockUpdateReady()
	if face == cube.FaceDown {
		tx.SetBlock(pos, markInteraction(barrierWithOpen(b, otherOpen), blockInteractionUpdate{after: true, ready: ready}), nil)
	} else {
		tx.SetBlock(otherPos, markInteraction(barrierWithOpen(other, open), blockInteractionUpdate{after: true, ready: ready}), nil)
	}
}
