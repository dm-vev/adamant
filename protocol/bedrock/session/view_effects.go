package session

import (
	"strings"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
	"github.com/go-gl/mathgl/mgl64"
)

// ViewItemCooldown ...
func (s *Session) ViewItemCooldown(item world.Item, duration time.Duration) {
	name, _ := item.EncodeItem()
	category := name
	if _, suffix, ok := strings.Cut(name, ":"); ok && suffix != "" {
		category = suffix
	}
	s.writePacket(&mc.ClientStartItemCooldownPacket{
		Category: category,
		Duration: int32(duration.Milliseconds() / 50),
	})
}

// ViewParticle ...
func (s *Session) ViewParticle(pos mgl64.Vec3, p world.Particle) {
	if s.dropCosmetic() {
		return
	}
	s.particlePackets(pos, p, s.writePacket)
}

// ViewParticleShared ...
func (s *Session) ViewParticleShared(pos mgl64.Vec3, p world.Particle, b *world.Broadcast) {
	if s.dropCosmetic() {
		return
	}
	// The packets of a particle name blocks by the registry of the session,
	// that of the world every session of it shares.
	s.writeBroadcast(b, func(out func(proto.Packet)) { s.particlePackets(pos, p, out) })
}

// playSound plays a world.Sound at a position, disabling relative volume if set to true.
func (s *Session) playSound(pos mgl64.Vec3, t world.Sound, disableRelative bool) {
	out := s.writePacket
	if bell, ok := t.(sound.BellRing); ok && bell.AfterBlock {
		out = func(pk proto.Packet) { s.enqueuePacketAfterBlock(cube.PosFromVec3(pos), pk) }
	}
	s.soundPackets(pos, t, disableRelative, out)
}

// PlaySound plays a world.Sound to the client. The volume is not dependent on the distance to the source if it is a
// sound of the LevelSoundEvent packet.
func (s *Session) PlaySound(t world.Sound, pos mgl64.Vec3) {
	if s == Nop {
		return
	}
	s.playSound(pos, t, true)
}

// ViewSound ...
func (s *Session) ViewSound(pos mgl64.Vec3, soundType world.Sound) {
	if s.dropCosmetic() {
		return
	}
	s.playSound(pos, soundType, false)
}

// ViewSoundShared ...
func (s *Session) ViewSoundShared(pos mgl64.Vec3, t world.Sound, b *world.Broadcast) {
	if s.dropCosmetic() {
		return
	}
	if bell, ok := t.(sound.BellRing); ok && bell.AfterBlock {
		s.playSound(pos, t, false)
		return
	}
	s.writeBroadcast(b, func(out func(proto.Packet)) { s.soundPackets(pos, t, false, out) })
}
