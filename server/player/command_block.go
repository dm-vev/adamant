package player

import (
	"fmt"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// commandBlockReach is how far the feet of a player may be from the centre
// of a command block, or from a command block minecart, that it edits.
const commandBlockReach = 8

// checkCommandBlockEdit checks that the player may make edit.
func (p *Player) checkCommandBlockEdit(edit block.CommandBlockEdit) error {
	if edit.Mode > block.CommandBlockChain {
		return fmt.Errorf("invalid command block mode %d", edit.Mode)
	}
	if !p.CanUseGameMasterBlocks() {
		return fmt.Errorf("only creative operators may edit command blocks")
	}
	if !edit.ValidText() {
		return fmt.Errorf("command block text over %d bytes", block.MaxCommandBlockText)
	}
	return nil
}

// EditCommandBlock applies edit to the command block at pos, as the player
// sets it in the editor of the block. It fails if the player may not use
// game master blocks, if a text of the edit is over
// block.MaxCommandBlockText bytes, if the feet of the player are over 8
// blocks from the centre of the block, or if no command block is at pos.
//
// The block is powered as the redstone around it powers it. Switching an
// unpowered impulse block to always active runs it once on the next tick,
// as CommandBlockEntity.setAutomatic does.
func (p *Player) EditCommandBlock(pos cube.Pos, edit block.CommandBlockEdit) error {
	if err := p.checkCommandBlockEdit(edit); err != nil {
		return err
	}
	if p.Position().Sub(pos.Vec3Centre()).Len() > commandBlockReach {
		return fmt.Errorf("command block is out of reach")
	}
	b, ok := p.tx.Block(pos).(block.CommandBlock)
	if !ok {
		return fmt.Errorf("no command block at requested position")
	}
	wasAuto := !b.NeedsRedstone
	b = b.Edited(edit)
	b.Powered = p.tx.Redstone().Power(pos) > 0
	p.tx.SetBlock(pos, b, nil)
	if b.Mode == block.CommandBlockImpulse && !b.NeedsRedstone && !wasAuto && !b.Powered {
		p.tx.ScheduleBlockUpdate(pos, b, time.Second/20)
	}
	return nil
}

// EditCommandMinecart applies the command, name, output, tracking, delay and
// first tick of edit to the command block minecart e, as the player sets
// them in its editor. It fails as EditCommandBlock does, with e as the block,
// and if e is not a command block minecart.
func (p *Player) EditCommandMinecart(e world.Entity, edit block.CommandBlockEdit) error {
	if err := p.checkCommandBlockEdit(edit); err != nil {
		return err
	}
	if p.Position().Sub(e.Position()).Len() > commandBlockReach {
		return fmt.Errorf("command minecart is unavailable or out of reach")
	}
	updater, ok := e.(interface {
		UpdateCommandBlock(string, string, string, bool, uint32, bool)
	})
	if !ok {
		return fmt.Errorf("entity is not a command minecart")
	}
	updater.UpdateCommandBlock(edit.Command, edit.Name, edit.LastOutput, edit.TrackOutput, uint32(edit.TickDelay), edit.ExecuteOnFirstTick)
	return nil
}
