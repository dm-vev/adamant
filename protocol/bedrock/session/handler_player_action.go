package session

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

// PlayerActionHandler handles the PlayerAction packet.
type PlayerActionHandler struct{}

// Handle ...
func (*PlayerActionHandler) Handle(p proto.Packet, s *Session, _ *world.Tx, c Controllable) error {
	pk := p.(*v2193.PlayerActionPacket)

	return handlePlayerAction(pk.ActionType, pk.BlockFace, pk.BlockPosition, pk.EntityRuntimeID, s, c)
}

// handlePlayerAction handles an action performed by a player, found in the PlayerAction and PlayerAuthInput packets.
func handlePlayerAction(action int32, face int32, pos v2193.BlockPos, entityRuntimeID uint64, s *Session, c Controllable) error {
	if entityRuntimeID != selfEntityRuntimeID {
		return errSelfRuntimeID
	}
	switch action {
	case v2193.PlayerActionStartSleeping, v2193.PlayerActionRespawn, v2193.PlayerActionDimensionChangeDone:
		// Don't do anything for these actions.
	case v2193.PlayerActionStopSleeping:
		c.Wake()
	case v2193.PlayerActionStartBreak, v2193.PlayerActionContinueDestroyBlock:
		blockFace, err := parseBlockFace(face)
		if err != nil {
			return err
		}
		s.swingingArm.Store(true)
		defer s.swingingArm.Store(false)

		s.breakingPos = cube.Pos{int(pos[0]), int(pos[1]), int(pos[2])}
		c.StartBreaking(s.breakingPos, blockFace)
	case v2193.PlayerActionAbortBreak:
		c.AbortBreaking()
	case v2193.PlayerActionPredictDestroyBlock, v2193.PlayerActionStopBreak:
		s.swingingArm.Store(true)
		defer s.swingingArm.Store(false)
		c.FinishBreaking()
	case v2193.PlayerActionCrackBreak:
		// Don't do anything for this action. It is no longer used. Block
		// cracking is done fully server-side.
	case v2193.PlayerActionStartItemUseOn:
		// TODO: Properly utilize these actions.
	case v2193.PlayerActionStopItemUseOn:
		c.ReleaseItem()
	case v2193.PlayerActionInteractWithBlock:
		// Item interaction data carries the actual block interaction.
	case v2193.PlayerActionCreativePlayerDestroyBlock:
		// Don't do anything for this action.
	case v2193.PlayerActionMissedSwing:
		s.swingingArm.Store(true)
		defer s.swingingArm.Store(false)
		c.PunchAir()
	default:
		return fmt.Errorf("unhandled ActionType %v", action)
	}
	return nil
}
