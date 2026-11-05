package session

import (
	"github.com/df-mc/dragonfly/server/world"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
	"github.com/go-gl/mathgl/mgl64"
)

// StopShowingEntity stops showing a world.Entity to the Session. It will be completely invisible until a call to
// StartShowingEntity is made.
func (s *Session) StopShowingEntity(e world.Entity) {
	s.entityMutex.Lock()
	_, ok := s.hiddenEntities[e.H().UUID()]
	if !ok {
		s.hiddenEntities[e.H().UUID()] = struct{}{}
		s.hidesEntities.Store(true)
	}
	s.entityMutex.Unlock()

	if !ok {
		s.HideEntity(e)
	}
}

// StartShowingEntity starts showing a world.Entity to the Session that was previously hidden using StopShowingEntity.
func (s *Session) StartShowingEntity(e world.Entity) {
	s.entityMutex.Lock()
	_, ok := s.hiddenEntities[e.H().UUID()]
	if ok {
		delete(s.hiddenEntities, e.H().UUID())
		s.hidesEntities.Store(len(s.hiddenEntities) > 0)
	}
	s.entityMutex.Unlock()

	if ok {
		e.H().ResendMove()
		s.ViewEntity(e)
		s.ViewEntityState(e)
		s.ViewEntityItems(e)
		s.ViewEntityArmour(e)
	}
}

// SendRespawn spawns the Controllable entity of the session client-side in the world, provided it has died.
func (s *Session) SendRespawn(pos mgl64.Vec3, c Controllable) {
	s.resetInventory("respawn")
	s.writePacket(&mc.RespawnPacket{
		Position:        vec64To32(entityNetworkPosition(c, pos)),
		State:           mc.RespawnStateReadyToSpawn,
		EntityRuntimeID: selfEntityRuntimeID,
	})
}

// SendPlayerSpawn updates the player's spawn point on the client-side. There is currently little reason
// to do so other than to prevent the client-side "Respawn point set" message when sleeping in a bed.
func (s *Session) SendPlayerSpawn(pos mgl64.Vec3) {
	blockPos := mc.BlockPos{int32(pos[0]), int32(pos[1]), int32(pos[2])}
	s.writePacket(&mc.SetSpawnPositionPacket{
		SpawnType:     mc.SpawnTypePlayer,
		Position:      blockPos,
		Dimension:     mc.DimensionOverworld,
		SpawnPosition: blockPos,
	})
}

// sendBiomes sends all the vanilla biomes to the session.
func (s *Session) sendBiomes() {
	definitions, stringList := s.tables().biomes()
	s.writePacket(&mc.BiomeDefinitionListPacket{
		BiomeDefinitions: definitions,
		StringList:       stringList,
	})
}

// valueOrDefault returns the value passed if it is not the zero value of the type T, otherwise it returns
// the default value provided.
func valueOrDefault[T comparable](v, def T) T {
	var zero T
	if v == zero {
		return def
	}
	return v
}

// The following functions use the go:linkname directive in order to make sure the item.byID and item.toID
// functions do not need to be exported.
