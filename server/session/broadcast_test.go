package session

import (
	"testing"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// TestBroadcastsAreEncodedOnce shows a sound and an animation to the
// sessions of one broadcast: they are given the same packets, but for the
// player of a session, which knows itself by runtime ID 1.
func TestBroadcastsAreEncodedOnce(t *testing.T) {
	a, b := testSession("a"), testSession("b")
	var sb world.Broadcast
	a.ViewSoundShared(mgl64.Vec3{1, 2, 3}, sound.Click{}, &sb)
	b.ViewSoundShared(mgl64.Vec3{1, 2, 3}, sound.Click{}, &sb)
	if pa, pb := a.testPacket(), b.testPacket(); pa == nil || pa != pb {
		t.Fatalf("sound packets %v and %v are not one", pa, pb)
	}
	e := movingEntity{h: entity.NewText("mob", mgl64.Vec3{})}
	var ab world.Broadcast
	a.ViewEntityAnimationShared(e, world.NewEntityAnimation("animation.test"), &ab)
	b.ViewEntityAnimationShared(e, world.NewEntityAnimation("animation.test"), &ab)
	if pa, pb := a.testPacket(), b.testPacket(); pa == nil || pa != pb {
		t.Fatalf("animation packets %v and %v are not one", pa, pb)
	}
	var own world.Broadcast
	b.ViewEntityAnimationShared(movingEntity{h: a.ent}, world.NewEntityAnimation("animation.test"), &own)
	a.ViewEntityAnimationShared(movingEntity{h: a.ent}, world.NewEntityAnimation("animation.test"), &own)
	pb, pa := b.testPacket(), a.testPacket()
	if self := packetOf(pa); self == nil || self.(interface{ ID() uint32 }).ID() != 158 {
		t.Fatalf("own animation %T", pa)
	}
	if packetOf(pa) == packetOf(pb) {
		t.Fatal("the session of the player shares the packet of the others")
	}
}
