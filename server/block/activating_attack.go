package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
)

// blockInteractionUpdate marks only the outgoing mutation snapshot. It is
// neither a block permutation property nor saved state.
type blockInteractionUpdate struct {
	active bool
	after  bool
	ready  <-chan struct{}
}

func (u blockInteractionUpdate) BlockInteractionUpdate() bool        { return u.active }
func (u blockInteractionUpdate) BlockUpdateAfter() (cube.Face, bool) { return cube.FaceDown, u.after }
func (u blockInteractionUpdate) BlockUpdateReady() <-chan struct{}   { return u.ready }

func (d WoodDoor) ActivateByAttack(pos cube.Pos, tx *world.Tx) bool {
	return activateBarrierByAttack(d, pos, tx)
}
func (d CopperDoor) ActivateByAttack(pos cube.Pos, tx *world.Tx) bool {
	return activateBarrierByAttack(d, pos, tx)
}
func (t WoodTrapdoor) ActivateByAttack(pos cube.Pos, tx *world.Tx) bool {
	return activateBarrierByAttack(t, pos, tx)
}
func (t CopperTrapdoor) ActivateByAttack(pos cube.Pos, tx *world.Tx) bool {
	return activateBarrierByAttack(t, pos, tx)
}
func (f WoodFenceGate) ActivateByAttack(pos cube.Pos, tx *world.Tx) bool {
	return activateBarrierByAttack(f, pos, tx)
}

func activateBarrierByAttack(b world.Block, pos cube.Pos, tx *world.Tx) bool {
	open, otherFace, door := barrierOpen(b)
	if power, _ := tx.Redstone().ConsumerPower(pos); power > 0 || door && otherFace == cube.FaceDown {
		return false
	}
	if door {
		return setDoorOpen(pos, b, !open, tx)
	}
	after := markBlockInteraction(barrierWithOpen(b, !open), tx)
	tx.SetBlock(pos, after, nil)
	afterPos := pos
	event := barrierToggleSound(tx, after, !open)
	event.AfterBlock = &afterPos
	tx.PlaySound(pos.Vec3Centre(), event)
	return true
}

// DoorBlock::_setOpen changes the lower open bit, republishes the unchanged
// upper permutation, and uses the old lower permutation for its sound data.
func setDoorOpen(pos cube.Pos, b world.Block, open bool, tx *world.Tx) bool {
	return setDoorOpenPublication(pos, b, open, tx, false)
}

func setDoorOpenPublication(pos cube.Pos, b world.Block, open bool, tx *world.Tx, circuit bool) bool {
	soundPos := pos
	_, otherFace, _ := barrierOpen(b)
	if otherFace == cube.FaceDown {
		pos = pos.Side(cube.FaceDown)
		lower := tx.Block(pos)
		name, _ := b.EncodeBlock()
		lowerName, _ := lower.EncodeBlock()
		if name != lowerName {
			return false
		}
		b = lower
	}
	wasOpen, _, _ := barrierOpen(b)
	if wasOpen == open {
		return false
	}
	update := blockInteractionUpdate{active: true, ready: tx.Defer(func(*world.Tx) {}).Done()}
	tx.SetBlock(pos, markInteraction(barrierWithOpen(b, open), update), nil)
	afterPos := pos
	otherPos := pos.Side(cube.FaceUp)
	other := tx.Block(otherPos)
	name, _ := b.EncodeBlock()
	otherName, _ := other.EncodeBlock()
	if name == otherName {
		tx.SetBlock(otherPos, markInteraction(other, update), nil)
		afterPos = otherPos
	}
	event := barrierToggleSound(tx, b, open)
	if !circuit {
		event.AfterBlock = &afterPos
	}
	tx.PlaySound(soundPos.Vec3Centre(), event)
	return true
}

func barrierToggleSound(tx *world.Tx, b world.Block, open bool) sound.NativeEvent {
	name := "trapdoor."
	if _, _, door := barrierOpen(b); door {
		name = "door."
	} else if _, gate := b.(WoodFenceGate); gate {
		name = "fence_gate."
	}
	if open {
		name += "open"
	} else {
		name += "close"
	}
	registry := tx.World().BlockRegistry()
	hash, _ := registry.RuntimeIDToHash(registry.BlockRuntimeID(b))
	return sound.NativeEvent{SoundType: name, ExtraData: int32(hash)}
}

func (c Candle) ActivateByAttack(pos cube.Pos, tx *world.Tx) bool {
	if !c.Lit {
		return false
	}
	c.Lit = false
	tx.SetBlock(pos, markBlockInteraction(c, tx), nil)
	playCandleAttackExtinguish(pos, tx)
	return true
}
func (c CandleCake) ActivateByAttack(pos cube.Pos, tx *world.Tx) bool {
	if !c.Lit {
		return false
	}
	c.Lit = false
	tx.SetBlock(pos, markBlockInteraction(c, tx), nil)
	playCandleAttackExtinguish(pos, tx)
	return true
}
func playCandleAttackExtinguish(pos cube.Pos, tx *world.Tx) {
	tx.PlaySound(pos.Vec3(), sound.NativeEvent{SoundType: "extinguish.candle", ExtraData: -1, AfterBlock: &pos})
}

func markBlockInteraction(b world.Block, tx *world.Tx) world.Block {
	return markInteraction(b, blockInteractionUpdate{active: true, ready: tx.Defer(func(*world.Tx) {}).Done()})
}

func markInteraction(b world.Block, update blockInteractionUpdate) world.Block {
	switch b := b.(type) {
	case WoodDoor:
		b.blockInteractionUpdate = update
		return b
	case CopperDoor:
		b.blockInteractionUpdate = update
		return b
	case IronDoor:
		b.blockInteractionUpdate = update
		return b
	case WoodTrapdoor:
		b.blockInteractionUpdate = update
		return b
	case CopperTrapdoor:
		b.blockInteractionUpdate = update
		return b
	case IronTrapdoor:
		b.blockInteractionUpdate = update
		return b
	case WoodFenceGate:
		b.blockInteractionUpdate = update
		return b
	case Candle:
		b.blockInteractionUpdate = update
		return b
	case CandleCake:
		b.blockInteractionUpdate = update
		return b
	}
	panic("block has no activating-attack update")
}
