package session

import (
	"bytes"
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
	"github.com/dm-vev/adamant/net/nbt"
)

// ViewTime ...
func (s *Session) ViewTime(time int) {
	s.writePacket(&mc.SetTimePacket{Time: int32(time)})
}

// ViewTimeCycle ...
func (s *Session) ViewTimeCycle(doDayLightCycle bool) {
	s.sendGameRules([]mc.GameRule{{Name: "dodaylightcycle", Value: doDayLightCycle}})
}

// ViewFallDamage controls whether fall damage is enabled for this viewer.
func (s *Session) ViewFallDamage(enabled bool) {
	s.sendGameRules([]mc.GameRule{{Name: "falldamage", Value: enabled}})
}

// ViewSleepingPlayers ...
func (s *Session) ViewSleepingPlayers(sleeping, max int) {
	buf := bytes.NewBuffer(nil)
	_ = nbt.NewEncoderWithEncoding(buf, nbt.NetworkLittleEndian).Encode(map[string]any{
		"ableToSleep":          int32(max),
		"overworldPlayerCount": int32(max),
		"sleepingPlayerCount":  int32(sleeping),
	})

	eventData := buf.Bytes()

	s.writePacket(&mc.LevelEventGenericPacket{
		EventID:             mc.LevelEventSleepingPlayers,
		SerialisedEventData: eventData[2 : len(eventData)-1],
	})
}

// OpenSign ...
func (s *Session) OpenSign(pos cube.Pos, frontSide bool) {
	blockPos := mc.BlockPos{int32(pos[0]), int32(pos[1]), int32(pos[2])}
	s.writePacket(&mc.OpenSignPacket{
		Position:  blockPos,
		FrontSide: frontSide,
	})
}

// ViewBlockAction ...
func (s *Session) ViewBlockAction(pos cube.Pos, a world.BlockAction) {
	blockPos := mc.BlockPos{int32(pos[0]), int32(pos[1]), int32(pos[2])}
	switch t := a.(type) {
	case block.BellRingAction:
		s.enqueueBellRingUpdate(pos, t.Bell)
	case block.BellUseRejectedAction:
		s.enqueueBellUseRejection(pos, t.Bell)
	case block.MovingBlockUpdateAction:
		s.enqueueBlockActorUpdate(pos, t.MovingBlock.PistonPos, clientActorNBT(t.MovingBlock.EncodeNBT(), pos))
	case world.PositionTrackingUpdateAction:
		s.writePacket(&mc.PositionTrackingDBServerBroadcastPacket{
			BroadcastAction: mc.PositionTrackingDBBroadcastActionUpdate,
			TrackingID:      t.Handle,
			Payload:         positionTrackingPayload(t.Handle, t.Position, t.Dimension, positionTrackingStatusTracked),
		})
	case world.PositionTrackingDestroyAction:
		s.writePacket(&mc.PositionTrackingDBServerBroadcastPacket{
			BroadcastAction: mc.PositionTrackingDBBroadcastActionDestroy,
			TrackingID:      t.Handle,
			Payload:         positionTrackingPayload(t.Handle, cube.Pos{}, 0, positionTrackingStatusMissing),
		})
	case block.OpenAction:
		s.writePacket(&mc.BlockEventPacket{
			Position:  blockPos,
			EventType: mc.BlockEventChangeChestState,
			EventData: 1,
		})
	case block.CloseAction:
		s.writePacket(&mc.BlockEventPacket{
			Position:  blockPos,
			EventType: mc.BlockEventChangeChestState,
		})
	case block.StartCrackAction:
		if t.BreakTime <= 0 {
			// An instant break has no cracking to animate, and encoding the crack speed would divide by zero.
			break
		}
		s.writePacket(&mc.LevelEventPacket{
			EventType: mc.LevelEventStartBlockCracking,
			Position:  vec64To32(pos.Vec3()),
			EventData: int32(65535 / (t.BreakTime.Seconds() * 20)),
		})
	case block.StopCrackAction:
		s.writePacket(&mc.LevelEventPacket{
			EventType: mc.LevelEventStopBlockCracking,
			Position:  vec64To32(pos.Vec3()),
			EventData: 0,
		})
	case block.ContinueCrackAction:
		if t.BreakTime <= 0 {
			break
		}
		s.writePacket(&mc.LevelEventPacket{
			EventType: mc.LevelEventUpdateBlockCracking,
			Position:  vec64To32(pos.Vec3()),
			EventData: int32(65535 / (t.BreakTime.Seconds() * 20)),
		})
	case block.DecoratedPotWobbleAction:
		nbt := clientActorNBT(t.DecoratedPot.EncodeNBT(), pos)
		nbt["animation"] = boolByte(t.Success) + 1
		s.writePacket(&mc.BlockActorDataPacket{
			Position: blockPos,
			NBTData:  nbt,
		})
	}
}

// ViewWorldSpawn ...
func (s *Session) ViewWorldSpawn(pos cube.Pos) {
	blockPos := mc.BlockPos{int32(pos[0]), int32(pos[1]), int32(pos[2])}
	s.writePacket(&mc.SetSpawnPositionPacket{
		SpawnType:     mc.SpawnTypeWorld,
		Position:      blockPos,
		Dimension:     mc.DimensionOverworld,
		SpawnPosition: blockPos,
	})
}

// SetPlayerSpawn updates the player's personal respawn location.
func (s *Session) SetPlayerSpawn(pos cube.Pos, dim world.Dimension) {
	if s == nil {
		return
	}
	id, _ := world.DimensionID(dim)
	blockPos := mc.BlockPos{int32(pos[0]), int32(pos[1]), int32(pos[2])}
	s.writePacket(&mc.SetSpawnPositionPacket{
		SpawnType:     mc.SpawnTypePlayer,
		Position:      blockPos,
		Dimension:     int32(id),
		SpawnPosition: blockPos,
	})
}

// ViewWeather ...
func (s *Session) ViewWeather(raining, thunder bool) {
	pk := &mc.LevelEventPacket{
		EventType: mc.LevelEventStopRaining,
	}
	if raining {
		pk.EventType, pk.EventData = mc.LevelEventStartRaining, int32(rand.IntN(50000)+10000)
	}
	s.writePacket(pk)

	pk = &mc.LevelEventPacket{
		EventType: mc.LevelEventStopThunderstorm,
	}
	if thunder {
		pk.EventType, pk.EventData = mc.LevelEventStartThunderstorm, int32(rand.IntN(50000)+10000)
	}
	s.writePacket(pk)
}
