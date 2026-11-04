package session

import (
	"testing"

	"github.com/df-mc/dragonfly/server/entity"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

// TestAreaEffectCloudParticleMetadata checks the particle the actor data of
// area effect clouds names: none for the particles of the potion, the dragon's
// breath particle type for dragon's breath, and the stored ParticleId for any
// other cloud read from a world.
func TestAreaEffectCloudParticleMetadata(t *testing.T) {
	s := testSession("cloud-particle")
	for _, c := range []struct {
		particle entity.CloudParticle
		want     any
	}{
		{entity.CloudParticlePotion, nil},
		{entity.CloudParticleDragonBreath, int32(47)},
		{entity.CloudParticle(68), int32(68)},
	} {
		m := mc.NewMetadata()
		s.addSpecificMetadata(entity.AreaEffectCloudBehaviourConfig{Particle: c.particle}.New(), m)
		got, ok := m[mc.EntityDataKeyDataParticle]
		if c.want == nil {
			if ok {
				t.Errorf("particle %d: actor data names particle %v, want none", c.particle, got)
			}
			continue
		}
		if got != c.want {
			t.Errorf("particle %d: actor data names particle %#v, want %#v", c.particle, got, c.want)
		}
	}
}
