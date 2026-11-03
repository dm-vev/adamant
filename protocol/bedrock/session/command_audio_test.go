package session

import (
	"reflect"
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/go-gl/mathgl/mgl64"
)

// TestSpawnParticleIdentifierDimension holds the dimension of a particle
// effect to the ID Bedrock gives it: 0 for the Overworld, which Player
// passes, as it passed 0 before dimensions were the game's, 1 for the
// Nether and 2 for the End.
func TestSpawnParticleIdentifierDimension(t *testing.T) {
	for _, c := range []struct {
		dim  world.Dimension
		want byte
	}{{world.Overworld, 0}, {world.Nether, 1}, {world.End, 2}} {
		s := newCaptureSession()
		s.SpawnParticleIdentifier(mgl64.Vec3{1, 2, 3}, "minecraft:heart_particle", c.dim)
		pks := s.taken()
		if len(pks) != 1 {
			t.Fatalf("%v: %d packets, want 1", c.dim, len(pks))
		}
		want := &v2193.SpawnParticleEffectPacket{Dimension: c.want, EntityUniqueID: -1, Position: vec64To32(mgl64.Vec3{1, 2, 3}), ParticleName: "minecraft:heart_particle"}
		if !reflect.DeepEqual(pks[0], want) {
			t.Errorf("%v: %#v, want %#v", c.dim, pks[0], want)
		}
	}
}
