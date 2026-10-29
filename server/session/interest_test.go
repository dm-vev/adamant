package session

import (
	"context"
	"testing"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/go-gl/mathgl/mgl64"
)

// TestEntitiesShownWithinRange shows a session entities by the ranges of
// their types: an item beyond 6 chunks of the player stays unseen, and its
// moves are not sent, until the player comes near; one left behind past
// its range and the margin is hidden.
func TestEntitiesShownWithinRange(t *testing.T) {
	w := world.Config{Entities: entity.DefaultRegistry, Synchronous: true}.New()
	t.Cleanup(func() { _ = w.Close() })
	s := testSession("interest")
	s.br = world.DefaultBlockRegistry
	if err := w.Do(func(tx *world.Tx) {
		near := tx.AddEntity(entity.NewItem(world.EntitySpawnOpts{Position: mgl64.Vec3{10, 64, 0}}, item.NewStack(item.Apple{}, 1)))
		far := tx.AddEntity(entity.NewItem(world.EntitySpawnOpts{Position: mgl64.Vec3{200, 64, 0}}, item.NewStack(item.Apple{}, 1)))
		farMob := tx.AddEntity(entity.NewText("far text", mgl64.Vec3{120, 64, 0}))

		s.updateInterest(tx, mgl64.Vec3{0, 64, 0})
		s.ViewEntity(near)
		if _, ok := (s.testPacket()).(*v2193.AddItemActorPacket); !ok {
			t.Fatal("an item within range was not shown")
		}
		s.ViewEntity(far)
		s.ViewEntity(farMob)
		// The text is within the default range of 8 chunks.
		if _, ok := (s.testPacket()).(*v2193.AddActorPacket); !ok {
			t.Fatal("an entity within the default range was not shown")
		}
		if pk := s.testPacket(); pk != nil {
			t.Fatalf("%T sent for an item out of range", pk)
		}
		s.ViewEntityMove(far, &world.EntityMove{Pos: mgl64.Vec3{201, 64, 0}, Changed: world.MoveX, Ticks: 1})
		if pk := s.testPacket(); pk != nil {
			t.Fatalf("a move of an item out of range was sent: %T", pk)
		}

		// The player goes to x=150: the far item comes within range, the
		// near one is left 140 blocks behind, past 96 and the margin.
		for range interestInterval {
			s.updateInterest(tx, mgl64.Vec3{150, 64, 0})
		}
		var removed, added bool
		for pk := s.testPacket(); pk != nil; pk = s.testPacket() {
			switch p := pk.(type) {
			case *v2193.RemoveActorPacket:
				removed = true
			case *v2193.AddItemActorPacket:
				added = p.EntityRuntimeID == far.H().RuntimeID()
			}
		}
		if !removed || !added {
			t.Fatalf("removed the near item %v, added the far one %v", removed, added)
		}
		// The world forgetting an entity kept out of range drops it.
		s.HideEntity(near)
		if s.hidesFar.Load() {
			t.Fatal("an entity the world hid is still kept out of range")
		}
	}).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}
