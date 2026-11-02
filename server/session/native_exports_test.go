package session

import (
	"encoding/json"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl32"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// External integration tests import the actual player package. Keep explicit
// flushing and authenticated-input replay available only in the test build.
func FlushNativeTestBlockUpdates(s *Session) { s.flushBlockUpdates() }

func NativeTestPhaseMarker(s *Session, value any) {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	s.writePacket(&packet.Text{Message: string(data)})
}

func CloseNativeTestSession(s *Session) {
	sessions.Remove(s, nil)
	s.CloseConnection()
}

func ReplayNativeTestBlockUse(s *Session, tx *world.Tx, c Controllable, pos cube.Pos, declaredCount ...uint16) error {
	held, _ := c.HeldItems()
	instance := instanceFromItem(s.br, held)
	if len(declaredCount) != 0 {
		instance.Stack.Count = declaredCount[0]
	}
	pk := &packet.PlayerAuthInput{InputData: protocol.NewInputFlags(packet.InputFlagCount)}
	pk.InputData.Set(packet.InputFlagPerformItemInteraction)
	pk.ItemInteractionData = protocol.Option(protocol.UseItemTransactionData{
		ActionType: protocol.UseItemActionClickBlock, TriggerType: protocol.TriggerTypePlayerInput,
		BlockPosition: protocol.BlockPos{int32(pos[0]), int32(pos[1]), int32(pos[2])},
		BlockFace:     1, ClickedPosition: mgl32.Vec3{.5, 1, .5},
		Position: vec64To32(entityNetworkPosition(c, c.Position())), BlockRuntimeID: networkBlockID(s.br, tx.Block(pos)),
		ClientPrediction: protocol.ClientPredictionSuccess, HeldItem: instance,
	})
	return (PlayerAuthInputHandler{}).handleActions(pk, s, tx, c)
}
