package session

import (
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/go-gl/mathgl/mgl64"
)

// StopSound sends a Bedrock StopSound packet to the session.
func (s *Session) StopSound(name string, all bool) {
	if s == Nop {
		return
	}
	s.writePacket(&v2193.StopSoundPacket{SoundName: name, StopAll: all})
}

// SpawnParticleIdentifier sends the named Bedrock particle effect at pos in
// the dimension dim to the session.
func (s *Session) SpawnParticleIdentifier(pos mgl64.Vec3, identifier string, dim world.Dimension) {
	if s == Nop {
		return
	}
	id, _ := world.DimensionID(dim)
	s.writePacket(&v2193.SpawnParticleEffectPacket{Dimension: byte(id), EntityUniqueID: -1, Position: vec64To32(pos), ParticleName: identifier})
}
