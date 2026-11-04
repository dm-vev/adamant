package session

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
)

// PlayerActionHandler handles the PlayerAction packet.
type PlayerActionHandler struct{}

// Handle ...
func (*PlayerActionHandler) Handle(p proto.Packet, s *Session, _ *world.Tx, c Controllable) error {
	pk := p.(*mc.PlayerActionPacket)

	return handlePlayerAction(pk.ActionType, pk.BlockFace, pk.BlockPosition, pk.EntityRuntimeID, s, c)
}

// handlePlayerAction handles an action performed by a player, found in the PlayerAction and PlayerAuthInput packets.
func handlePlayerAction(action int32, face int32, pos mc.BlockPos, entityRuntimeID uint64, s *Session, c Controllable) error {
	if entityRuntimeID != selfEntityRuntimeID {
		return errSelfRuntimeID
	}
	switch action {
	case mc.PlayerActionStartSleeping, mc.PlayerActionRespawn, mc.PlayerActionDimensionChangeDone:
		// Don't do anything for these actions.
	case mc.PlayerActionStopSleeping:
		c.Wake()
	case mc.PlayerActionStartBreak, mc.PlayerActionContinueDestroyBlock:
		blockFace, err := parseBlockFace(face)
		if err != nil {
			return err
		}
		s.swingingArm.Store(true)
		defer s.swingingArm.Store(false)

		s.breakingPos = cube.Pos{int(pos[0]), int(pos[1]), int(pos[2])}
		c.StartBreaking(s.breakingPos, blockFace)
	case mc.PlayerActionAbortBreak:
		c.AbortBreaking()
	case mc.PlayerActionPredictDestroyBlock, mc.PlayerActionStopBreak:
		s.swingingArm.Store(true)
		defer s.swingingArm.Store(false)
		c.FinishBreaking()
	case mc.PlayerActionCrackBreak:
		// Don't do anything for this action. It is no longer used. Block
		// cracking is done fully server-side.
	case mc.PlayerActionStartItemUseOn:
		// TODO: Properly utilize these actions.
	case mc.PlayerActionStopItemUseOn:
		c.ReleaseItem()
	case mc.PlayerActionInteractWithBlock:
		// Item interaction data carries the actual block interaction.
	case mc.PlayerActionCreativePlayerDestroyBlock:
		// Don't do anything for this action.
	case mc.PlayerActionMissedSwing:
		s.swingingArm.Store(true)
		defer s.swingingArm.Store(false)
		c.PunchAir()
	default:
		return fmt.Errorf("unhandled ActionType %v", action)
	}
	return nil
}
