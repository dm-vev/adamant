package session

import (
	"context"
	"reflect"
	"testing"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/go-gl/mathgl/mgl64"
)

// TestOfferFlowerAnimation checks that an iron golem offering a flower is
// sent the AnimateEntity packet the golem played as a Bedrock animation
// before it was an action of the game, whether the session plays it alone
// or as a broadcast, and that a congested session drops it as it did.
func TestOfferFlowerAnimation(t *testing.T) {
	w := world.Config{Entities: entity.DefaultRegistry, Synchronous: true}.New()
	defer w.Close()
	if err := w.Do(func(tx *world.Tx) {
		golem := tx.AddEntity(entity.NewIronGolem(world.EntitySpawnOpts{Position: mgl64.Vec3{8.5, 65, 8.5}}))
		want := &mc.AnimateEntityPacket{Animation: "animation.iron_golem.offer_flower", EntityRuntimeIDs: []uint64{golem.H().RuntimeID()}}

		s := testSession("flower")
		s.ViewEntityAction(golem, entity.OfferFlowerAction{})
		if got := s.testPacket(); !reflect.DeepEqual(got, want) {
			t.Errorf("OfferFlowerAction sent %#v, want %#v", got, want)
		}
		old := testSession("animation")
		old.ViewEntityAnimationShared(golem, world.NewEntityAnimation("animation.iron_golem.offer_flower"), &world.Broadcast{})
		if got := old.testPacket(); !reflect.DeepEqual(got.(*sharedPacket).Packet, want) {
			t.Errorf("the animation of before sent %#v, want %#v", got, want)
		}

		congested := testSession("congested")
		congested.congested.Store(true)
		congested.ViewEntityAction(golem, entity.OfferFlowerAction{})
		if got := congested.testPacket(); got != nil {
			t.Errorf("a congested session was sent %#v", got)
		}
	}).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}
