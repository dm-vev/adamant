package session_test

import (
	"testing"

	"github.com/df-mc/dragonfly/protocol/bedrock/session"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
	"github.com/go-gl/mathgl/mgl64"
)

// TestDispenserLaunchSounds checks the level events BDS 1.26.61.1 sends when a
// dispenser shoots a projectile: 1002 for most, 1008 for a fire charge, both
// with a pitch of 1.2 as fixed point in their data.
func TestDispenserLaunchSounds(t *testing.T) {
	pos := mgl64.Vec3{1.5, 64.5, 2.5}
	for name, c := range map[string]struct {
		sound world.Sound
		event int32
	}{
		"launch":   {sound.DispenserLaunch{}, mc.LevelEventSoundLaunch},
		"fireball": {sound.DispenserFireball{}, mc.LevelEventSoundGhastFireball},
	} {
		pks := session.SoundPackets(pos, c.sound)
		if len(pks) != 1 {
			t.Fatalf("%s: %d packets, want 1", name, len(pks))
		}
		ev, ok := pks[0].(*mc.LevelEventPacket)
		if !ok || ev.EventType != c.event || ev.EventData != 78642 || ev.Position != [3]float32{1.5, 64.5, 2.5} {
			t.Fatalf("%s: got %+v, want level event %d with data 78642 at %v", name, pks[0], c.event, pos)
		}
	}
}
