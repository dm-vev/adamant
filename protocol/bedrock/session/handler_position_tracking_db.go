package session

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

const (
	positionTrackingPayloadVersion = 1
	positionTrackingStatusTracked  = 0
	positionTrackingStatusMissing  = 2
)

// PositionTrackingDBHandler handles client queries for lodestone compass targets.
type PositionTrackingDBHandler struct{}

// Handle responds with the tracked position, or marks the target as unavailable.
func (*PositionTrackingDBHandler) Handle(p proto.Packet, s *Session, tx *world.Tx, c Controllable) error {
	pk, ok := p.(*v2193.PositionTrackingDBClientRequestPacket)
	if !ok {
		return fmt.Errorf("expected *v2193.PositionTrackingDBClientRequestPacket, got %T", p)
	}
	if pk.RequestAction != v2193.PositionTrackingDBRequestActionQuery {
		return fmt.Errorf("unknown position tracking request action %d", pk.RequestAction)
	}
	pos, dim, found := cube.Pos{}, 0, false
	if holdsTrackingCompass(c, pk.TrackingID) {
		pos, dim, found = tx.World().TrackedPosition(pk.TrackingID)
	}
	action, status := byte(v2193.PositionTrackingDBBroadcastActionUpdate), byte(positionTrackingStatusTracked)
	if !found {
		action, status = v2193.PositionTrackingDBBroadcastActionNotFound, positionTrackingStatusMissing
	}
	s.writePacket(&v2193.PositionTrackingDBServerBroadcastPacket{
		BroadcastAction: action,
		TrackingID:      pk.TrackingID,
		Payload:         positionTrackingPayload(pk.TrackingID, pos, dim, status),
	})
	return nil
}

func holdsTrackingCompass(c Controllable, handle int32) bool {
	hasHandle := func(stack item.Stack) bool {
		compass, ok := stack.Item().(item.Compass)
		return ok && compass.TrackingHandle == handle
	}
	mainHand, offHand := c.HeldItems()
	if hasHandle(mainHand) || hasHandle(offHand) {
		return true
	}
	_, ok := c.Inventory().FirstFunc(hasHandle)
	return ok
}

func positionTrackingPayload(handle int32, pos cube.Pos, dim int, status byte) map[string]any {
	return map[string]any{
		"version": byte(positionTrackingPayloadVersion),
		"dim":     int32(dim),
		"id":      fmt.Sprintf("0x%08x", handle),
		"pos":     []int32{int32(pos[0]), int32(pos[1]), int32(pos[2])},
		"status":  status,
	}
}
