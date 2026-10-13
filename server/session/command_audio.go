package session

import (
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// StopSound sends a Bedrock StopSound packet to the session.
func (s *Session) StopSound(name string, all bool) {
	if s == Nop {
		return
	}
	s.writePacket(&packet.StopSound{SoundName: name, StopAll: all})
}

// SpawnParticleIdentifier sends a named Bedrock particle effect to the session.
func (s *Session) SpawnParticleIdentifier(pos mgl64.Vec3, identifier string, dimension int32) {
	if s == Nop {
		return
	}
	s.writePacket(&packet.SpawnParticleEffect{Dimension: byte(dimension), EntityUniqueID: -1, Position: vec64To32(pos), ParticleName: identifier})
}
