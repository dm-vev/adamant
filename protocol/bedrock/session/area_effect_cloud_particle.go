package session

import (
	"github.com/df-mc/dragonfly/server/entity"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

// cloudParticleType returns the particle type the actor data of an area effect
// cloud names for the particle it shows, and false if the cloud shows the
// particles of its potion, for which the actor data names none.
func cloudParticleType(p entity.CloudParticle) (int32, bool) {
	switch p {
	case entity.CloudParticlePotion:
		return 0, false
	case entity.CloudParticleDragonBreath:
		return mc.ParticleTypeDragonBreath, true
	}
	// A cloud read from a world keeps the ParticleId stored for it, which the
	// world format numbers as the protocol does.
	return int32(p), true
}
