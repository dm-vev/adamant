package entity

import "github.com/df-mc/dragonfly/server/world"

// CarryBlock records a block state seized by an enderman. The state is encoded
// as native block name/properties so it survives save and entity transfer.
func (e *Enderman) CarryBlock(block world.Block) bool {
	if block == nil {
		return false
	}
	name, properties := block.EncodeBlock()
	e.s().carried = map[string]any{"Name": name, "Properties": properties}
	return true
}

// CarriedBlock returns the currently held native block-state compound.
func (e *Enderman) CarriedBlock() map[string]any {
	if e.s().carried == nil {
		return nil
	}
	return CloneNativeMobNBT(map[string]any{"CarriedBlock": e.s().carried})["CarriedBlock"].(map[string]any)
}

// DropCarriedBlock clears the block held by the enderman and returns its state.
func (e *Enderman) DropCarriedBlock() map[string]any {
	state := e.CarriedBlock()
	e.s().carried = nil
	return state
}
