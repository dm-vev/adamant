package player

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
)

// EditStructureBlock applies edit to the structure block at pos, as the
// player sets it in the editor of the block, and, if trigger is set, saves
// its structure to the world save or loads it, as its mode says. It fails if
// the player may not use game master blocks, if the block is out of its
// reach or if the mode of the edit is not one of a structure block, and does
// nothing if no structure block is at pos.
func (p *Player) EditStructureBlock(pos cube.Pos, edit block.StructureBlockEdit, trigger bool) error {
	if !p.CanUseGameMasterBlocks() {
		return fmt.Errorf("only creative operators may edit structure blocks")
	}
	if !p.CanReach(pos.Vec3Middle()) {
		return fmt.Errorf("structure block at %v is not within reach", pos)
	}
	if edit.Mode > block.StructureBlockExport {
		return fmt.Errorf("invalid structure block mode %d", edit.Mode)
	}
	b, ok := p.tx.Block(pos).(block.StructureBlock)
	if !ok {
		p.log.Debug("edit structure block: no structure block at position", "pos", pos.String())
		return nil
	}
	b = b.Edited(edit)
	p.tx.SetBlock(pos, b, nil)
	if !trigger {
		return nil
	}
	switch b.Mode {
	case block.StructureBlockSave:
		// The save button of the editor always writes to the world save.
		b.Save(pos, p.tx, true)
	case block.StructureBlockLoad:
		b.Load(pos, p.tx)
	}
	return nil
}
