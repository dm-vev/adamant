package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/world"
)

// DoorItem first replaces a build-over cell (including snow below eight
// layers). Otherwise it moves above the clicked cell and requires its top face.
func doorPlacementPosition(tx *world.Tx, pos cube.Pos, face cube.Face, b world.Block) (cube.Pos, bool) {
	pos, face, ok := firstReplaceable(tx, pos, face, b)
	if !ok || face != cube.FaceUp || !replaceableWith(tx, pos.Side(cube.FaceUp), b) || !doorSupportedBy(pos.Side(cube.FaceDown), tx) {
		return pos, false
	}
	return pos, true
}

func (d WoodDoor) Filled(pos cube.Pos, tx *world.Tx, _ world.Block)   { doorFilled(d, pos, tx) }
func (d CopperDoor) Filled(pos cube.Pos, tx *world.Tx, _ world.Block) { doorFilled(d, pos, tx) }
func (d IronDoor) Filled(pos cube.Pos, tx *world.Tx, _ world.Block)   { doorFilled(d, pos, tx) }

func (d WoodDoor) CanFillAtPos(pos cube.Pos, tx *world.Tx) bool   { return doorCanFill(pos, d.Top, tx) }
func (d CopperDoor) CanFillAtPos(pos cube.Pos, tx *world.Tx) bool { return doorCanFill(pos, d.Top, tx) }
func (d IronDoor) CanFillAtPos(pos cube.Pos, tx *world.Tx) bool   { return doorCanFill(pos, d.Top, tx) }

func doorCanFill(pos cube.Pos, top bool, tx *world.Tx) bool {
	_, belowDoor := tx.Block(pos.Side(cube.FaceDown)).Model().(model.Door)
	return !top && !belowDoor
}

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

// Native command placement clones the lower permutation into its upper half.
// Ordinary native toggles do not execute this command callback, retaining
// that bit thereafter. Both behaviours are retained in the BDS pulse corpus.
func doorFilled(b world.Block, pos cube.Pos, tx *world.Tx) {
	_, face, _ := barrierOpen(b)
	if face == cube.FaceDown {
		return
	}
	var upper world.Block
	switch d := b.(type) {
	case WoodDoor:
		d.Top = true
		upper = d
	case CopperDoor:
		d.Top = true
		upper = d
	case IronDoor:
		d.Top = true
		upper = d
	}
	otherPos := pos.Side(cube.FaceUp)
	registry := tx.World().BlockRegistry()
	if registry.BlockRuntimeID(tx.Block(otherPos)) == registry.BlockRuntimeID(upper) {
		return
	}
	ready := b.(interface{ BlockUpdateReady() <-chan struct{} }).BlockUpdateReady()
	tx.SetBlock(otherPos, markInteraction(upper, blockInteractionUpdate{after: true, ready: ready}), &world.SetOpts{DisableBlockUpdates: true})
}
