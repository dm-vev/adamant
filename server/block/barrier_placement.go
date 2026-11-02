package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
)

// PlayerPlacementNotifier separates the placing player's immediate state
// confirmations from the ordinary world publication. Preparation only marks
// an outgoing snapshot; PlayerPlaced runs after a successful world mutation.
type PlayerPlacementNotifier interface {
	PlayerPlacementUpdate(*world.Tx) world.Block
	PlayerPlaced(cube.Pos, *world.Tx, func(world.BlockAction))
}

func (d WoodDoor) PlayerPlacementUpdate(tx *world.Tx) world.Block { return markBlockInteraction(d, tx) }
func (d CopperDoor) PlayerPlacementUpdate(tx *world.Tx) world.Block {
	return markBlockInteraction(d, tx)
}
func (d IronDoor) PlayerPlacementUpdate(tx *world.Tx) world.Block { return markBlockInteraction(d, tx) }
func (t WoodTrapdoor) PlayerPlacementUpdate(tx *world.Tx) world.Block {
	return markBlockInteraction(t, tx)
}
func (t CopperTrapdoor) PlayerPlacementUpdate(tx *world.Tx) world.Block {
	return markBlockInteraction(t, tx)
}
func (t IronTrapdoor) PlayerPlacementUpdate(tx *world.Tx) world.Block {
	return markBlockInteraction(t, tx)
}
func (f WoodFenceGate) PlayerPlacementUpdate(tx *world.Tx) world.Block {
	return markBlockInteraction(f, tx)
}

func (d WoodDoor) PlayerPlaced(pos cube.Pos, tx *world.Tx, notify func(world.BlockAction)) {
	barrierPlaced(d, pos, tx, notify)
}
func (d CopperDoor) PlayerPlaced(pos cube.Pos, tx *world.Tx, notify func(world.BlockAction)) {
	barrierPlaced(d, pos, tx, notify)
}
func (d IronDoor) PlayerPlaced(pos cube.Pos, tx *world.Tx, notify func(world.BlockAction)) {
	barrierPlaced(d, pos, tx, notify)
}
func (t WoodTrapdoor) PlayerPlaced(pos cube.Pos, tx *world.Tx, notify func(world.BlockAction)) {
	barrierPlaced(t, pos, tx, notify)
}
func (t CopperTrapdoor) PlayerPlaced(pos cube.Pos, tx *world.Tx, notify func(world.BlockAction)) {
	barrierPlaced(t, pos, tx, notify)
}
func (t IronTrapdoor) PlayerPlaced(pos cube.Pos, tx *world.Tx, notify func(world.BlockAction)) {
	barrierPlaced(t, pos, tx, notify)
}
func (f WoodFenceGate) PlayerPlaced(pos cube.Pos, tx *world.Tx, notify func(world.BlockAction)) {
	barrierPlaced(f, pos, tx, notify)
}

func barrierPlaced(b world.Block, pos cube.Pos, tx *world.Tx, notify func(world.BlockAction)) {
	_, otherFace, door := barrierOpen(b)
	confirmations := 2
	if door && otherFace == cube.FaceDown {
		confirmations = 1
	}
	notify(PlacementAction{Block: b, Confirmations: confirmations})
	if door && otherFace == cube.FaceDown {
		return
	}
	// DoorItem emits one sound for the complete pair. Ordinary block items
	// use their default permutation for sound data, rather than the newly
	// oriented placement permutation.
	soundBlock := b
	soundPos := pos.Vec3()
	switch b := b.(type) {
	case WoodTrapdoor:
		soundBlock = WoodTrapdoor{Wood: b.Wood, Facing: cube.East}
	case CopperTrapdoor:
		soundBlock = CopperTrapdoor{Oxidation: b.Oxidation, Waxed: b.Waxed, Facing: cube.East}
	case IronTrapdoor:
		soundBlock = IronTrapdoor{Facing: cube.East}
	case WoodFenceGate:
		soundBlock = WoodFenceGate{Wood: b.Wood, Facing: cube.South}
	}
	if !door {
		soundPos = pos.Vec3Centre()
	}
	hash, _ := tx.World().BlockRegistry().RuntimeIDToHash(tx.World().BlockRegistry().BlockRuntimeID(soundBlock))
	tx.Defer(func(tx *world.Tx) {
		anchor := pos
		if door {
			upperPos := pos.Side(cube.FaceUp)
			name, _ := b.EncodeBlock()
			upper := tx.Block(upperPos)
			upperName, _ := upper.EncodeBlock()
			if upperName == name {
				_, face, _ := barrierOpen(upper)
				if face == cube.FaceDown {
					anchor = upperPos
				}
			}
		}
		tx.PlaySound(soundPos, sound.NativeEvent{SoundType: "place", ExtraData: int32(hash), AfterBlock: &anchor})
	})
}
