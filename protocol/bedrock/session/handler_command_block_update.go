package session

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

// CommandBlockUpdateHandler applies command-block editor changes.
type CommandBlockUpdateHandler struct{}

func (CommandBlockUpdateHandler) Handle(p proto.Packet, s *Session, tx *world.Tx, c Controllable) error {
	pk := p.(*mc.CommandBlockUpdatePacket)
	if pk.Mode > mc.CommandBlockChain {
		return fmt.Errorf("invalid command block mode %d", pk.Mode)
	}
	edit := block.CommandBlockEdit{
		Mode:               block.CommandBlockMode(pk.Mode),
		Conditional:        pk.Conditional,
		NeedsRedstone:      pk.NeedsRedstone,
		Command:            pk.Command,
		Name:               pk.Name,
		LastOutput:         pk.LastOutput,
		TrackOutput:        pk.ShouldTrackOutput,
		TickDelay:          int(pk.TickDelay),
		ExecuteOnFirstTick: pk.ExecuteOnFirstTick,
	}
	// The player checks these again. They come first, before the minecart
	// is looked up, as the refusals did.
	if !c.CanUseGameMasterBlocks() {
		return fmt.Errorf("only creative operators may edit command blocks")
	}
	if !edit.ValidText() {
		return fmt.Errorf("command block text over %d bytes", block.MaxCommandBlockText)
	}
	if !pk.Block {
		handle, ok := s.entityFromRuntimeID(pk.MinecartEntityRuntimeID)
		if !ok {
			return fmt.Errorf("command minecart not found")
		}
		e, ok := handle.Entity(tx)
		if !ok {
			return fmt.Errorf("command minecart is unavailable or out of reach")
		}
		return c.EditCommandMinecart(e, edit)
	}
	return c.EditCommandBlock(cube.Pos{int(pk.Position[0]), int(pk.Position[1]), int(pk.Position[2])}, edit)
}
