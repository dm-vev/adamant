package entity

import (
	"testing"

	"github.com/df-mc/dragonfly/server/world"
)

// TestAreaEffectCloudParticleNBTRoundTrip checks that the world format stores
// the particle of a cloud as its ParticleId, leaves it out for the particles of
// the potion, and keeps a ParticleId the game has no name for.
func TestAreaEffectCloudParticleNBTRoundTrip(t *testing.T) {
	for _, c := range []struct {
		particle CloudParticle
		stored   any
	}{
		{CloudParticlePotion, nil},
		{CloudParticleDragonBreath, int32(47)},
		{CloudParticle(68), int32(68)},
	} {
		m := AreaEffectCloudType.EncodeNBT(&world.EntityData{Data: AreaEffectCloudBehaviourConfig{Particle: c.particle}.New()})
		if got := m["ParticleId"]; got != c.stored {
			t.Errorf("particle %d: stored ParticleId %#v, want %#v", c.particle, got, c.stored)
		}
		decoded := new(world.EntityData)
		AreaEffectCloudType.DecodeNBT(m, decoded)
		if got := decoded.Data.(*AreaEffectCloudBehaviour).Particle(); got != c.particle {
			t.Errorf("particle %d: decoded particle %d", c.particle, got)
		}
	}
}
