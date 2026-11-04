package session

import (
	"reflect"
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
	"github.com/go-gl/mathgl/mgl64"
)

// TestViewNamedParticleDimension holds the dimension of a particle effect
// to the ID Bedrock gives it: 0 for the Overworld, which Player passes, as
// it passed 0 before dimensions were the game's, 1 for the Nether and 2 for
// the End.
func TestViewNamedParticleDimension(t *testing.T) {
	for _, c := range []struct {
		dim  world.Dimension
		want byte
	}{{world.Overworld, 0}, {world.Nether, 1}, {world.End, 2}} {
		s := newCaptureSession()
		s.ViewNamedParticle(mgl64.Vec3{1, 2, 3}, "minecraft:heart", c.dim)
		pks := s.taken()
		if len(pks) != 1 {
			t.Fatalf("%v: %d packets, want 1", c.dim, len(pks))
		}
		want := &mc.SpawnParticleEffectPacket{Dimension: c.want, EntityUniqueID: -1, Position: vec64To32(mgl64.Vec3{1, 2, 3}), ParticleName: "minecraft:heart_particle"}
		if !reflect.DeepEqual(pks[0], want) {
			t.Errorf("%v: %#v, want %#v", c.dim, pks[0], want)
		}
	}
}

// TestStopSoundNames holds the name StopSound sends to the one PlaySound
// sends for a sound.Custom of the name: a Java sound event ID mapped, other
// names passed through, nothing for a sound with no Bedrock counterpart,
// and the packet that stops every sound for all.
func TestStopSoundNames(t *testing.T) {
	for _, c := range []struct {
		name string
		all  bool
		want []proto.Packet
	}{
		{"block.pumpkin.carve", false, []proto.Packet{&mc.StopSoundPacket{SoundName: "pumpkin.carve"}}},
		{"random.click", false, []proto.Packet{&mc.StopSoundPacket{SoundName: "random.click"}}},
		{"custom.pack.sound", false, []proto.Packet{&mc.StopSoundPacket{SoundName: "custom.pack.sound"}}},
		{"block.growing_plant.crop", false, nil},
		{"", true, []proto.Packet{&mc.StopSoundPacket{StopAll: true}}},
		{"block.growing_plant.crop", true, []proto.Packet{&mc.StopSoundPacket{StopAll: true}}},
	} {
		s := newCaptureSession()
		s.StopSound(c.name, c.all)
		if got := s.taken(); !reflect.DeepEqual(got, c.want) {
			t.Errorf("StopSound(%q, %v) sent %#v, want %#v", c.name, c.all, got, c.want)
		}
	}
}
