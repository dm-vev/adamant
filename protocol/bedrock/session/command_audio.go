package session

import (
	"github.com/df-mc/dragonfly/server/world"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/go-gl/mathgl/mgl64"
)

// StopSound sends a Bedrock StopSound packet to the session, for the sound
// the name is played by: a Java sound event ID is mapped as PlaySound maps
// those of sound.Custom, and one with no Bedrock counterpart, which plays
// nothing, sends nothing unless all is set.
func (s *Session) StopSound(name string, all bool) {
	if s == Nop {
		return
	}
	name, ok := bedrockSoundName(name)
	if !ok && !all {
		return
	}
	s.writePacket(&mc.StopSoundPacket{SoundName: name, StopAll: all})
}

// ViewNamedParticle sends the Bedrock particle effect of the name, a
// particle type of Java Edition or a name passed on as it is, at pos in the
// dimension dim to the session.
func (s *Session) ViewNamedParticle(pos mgl64.Vec3, name string, dim world.Dimension) {
	if s == Nop {
		return
	}
	id, _ := world.DimensionID(dim)
	s.writePacket(&mc.SpawnParticleEffectPacket{Dimension: byte(id), EntityUniqueID: -1, Position: vec64To32(pos), ParticleName: bedrockParticleName(name)})
}
