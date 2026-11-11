package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// Only tests use these helpers of redstone_torch.go, so they live with the tests.

// attachmentPowered reports whether the block the torch is attached to is powered.
func (t RedstoneTorch) attachmentPowered(pos cube.Pos, tx *world.Tx) bool {
	if tx == nil {
		return false
	}
	attached := pos.Side(t.facing())
	attachedBlock := tx.Block(attached)
	if source, ok := attachedBlock.(world.RedstonePowerSource); ok {
		for _, face := range cube.Faces() {
			if world.ClampRedstonePower(source.RedstonePower(attached, tx, face)) > 0 {
				return true
			}
		}
	}
	return world.RedstoneFullPowerConductor(attached, attachedBlock, tx) && tx.Redstone().ConductivePower(attached) > 0
}
