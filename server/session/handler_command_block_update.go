package session

import (
	"fmt"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// CommandBlockUpdateHandler applies command-block editor changes.
type CommandBlockUpdateHandler struct{}

func (CommandBlockUpdateHandler) Handle(p packet.Packet, s *Session, tx *world.Tx, c Controllable) error {
	pk := p.(*packet.CommandBlockUpdate)
	if pk.Mode > packet.CommandBlockChain {
		return fmt.Errorf("invalid command block mode %d", pk.Mode)
	}
	pos := cube.Pos{int(pk.Position[0]), int(pk.Position[1]), int(pk.Position[2])}
	if !c.GameMode().CreativeInventory() || !cmd.IsOperator(c.Name()) {
		return fmt.Errorf("only creative operators may edit command blocks")
	}
	if !pk.Block {
		handle, ok := s.entityFromRuntimeID(pk.MinecartEntityRuntimeID)
		if !ok {
			return fmt.Errorf("command minecart not found")
		}
		e, ok := handle.Entity(tx)
		if !ok || c.Position().Sub(e.Position()).Len() > 8 {
			return fmt.Errorf("command minecart is unavailable or out of reach")
		}
		updater, ok := e.(interface {
			UpdateCommandBlock(string, string, string, bool, uint32, bool)
		})
		if !ok {
			return fmt.Errorf("entity is not a command minecart")
		}
		updater.UpdateCommandBlock(pk.Command, pk.Name, pk.LastOutput, pk.ShouldTrackOutput, pk.TickDelay, pk.ExecuteOnFirstTick)
		return nil
	}
	if c.Position().Sub(pos.Vec3Centre()).Len() > 8 {
		return fmt.Errorf("command block is out of reach")
	}
	b, ok := tx.Block(pos).(block.CommandBlock)
	if !ok {
		return fmt.Errorf("no command block at requested position")
	}
	wasAuto := !b.NeedsRedstone
	b.Mode = block.CommandBlockMode(pk.Mode)
	b.Conditional, b.NeedsRedstone = pk.Conditional, pk.NeedsRedstone
	b.Command, b.CustomName, b.TrackOutput = pk.Command, pk.Name, pk.ShouldTrackOutput
	b.TickDelay, b.ExecuteOnFirstTick = int(pk.TickDelay), pk.ExecuteOnFirstTick
	if !b.TrackOutput {
		b.LastOutput = ""
	}
	b.Powered = tx.RedstonePower(pos) > 0
	tx.SetBlock(pos, b, nil)
	// Like CommandBlockEntity.setAutomatic, switching an unpowered impulse
	// block to always active runs it once on the next tick.
	if b.Mode == block.CommandBlockImpulse && !b.NeedsRedstone && !wasAuto && !b.Powered {
		tx.ScheduleBlockUpdate(pos, b, time.Second/20)
	}
	return nil
}
