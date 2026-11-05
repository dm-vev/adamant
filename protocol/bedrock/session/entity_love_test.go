package session

import (
	"testing"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
	"github.com/go-gl/mathgl/mgl64"
)

// TestFedAnimalsShowHearts checks the hearts of an animal falling in love or
// breeding, for clients of both protocols: the LOVE_HEARTS event (21), with
// the in-love flag set in the metadata after it.
func TestFedAnimalsShowHearts(t *testing.T) {
	t.Parallel()
	if mc.ActorEventLoveHearts != 21 || v2193.ActorEventLoveHearts != 21 {
		t.Fatalf("LOVE_HEARTS = %d of 2225 and %d of 2193, want 21", mc.ActorEventLoveHearts, v2193.ActorEventLoveHearts)
	}
	h, err := entity.NewFromStructureNBT(map[string]any{"id": "minecraft:cow", "InLove": int32(600)}, world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 64, 0.5}, SpawnFinalized: true})
	if err != nil {
		t.Fatal(err)
	}
	w := world.Config{Entities: entity.DefaultRegistry, Synchronous: true}.New()
	defer w.Close()
	viewer := testSession("hearts")
	viewer.br = world.DefaultBlockRegistry
	<-w.Exec(func(tx *world.Tx) {
		cow := tx.AddEntity(h)
		viewer.ViewEntity(cow)
		added := viewer.testPacket().(*mc.AddActorPacket)
		if !actorFlag(added.EntityMetadata, mc.EntityDataFlagInLove) {
			t.Error("a cow in love is not shown with the in-love flag")
		}
		viewer.ViewEntityAction(cow, entity.LoveHeartsAction{})
		event, ok := viewer.testPacket().(*mc.ActorEventPacket)
		if !ok {
			t.Fatal("no ActorEvent for the hearts")
		}
		if event.EventType != mc.ActorEventLoveHearts || event.EntityRuntimeID != added.EntityRuntimeID {
			t.Errorf("hearts event = %d of %d, want %d of %d", event.EventType, event.EntityRuntimeID, mc.ActorEventLoveHearts, added.EntityRuntimeID)
		}
		if down := for2193(t, event).(*v2193.ActorEventPacket); down.EventType != event.EventType {
			t.Errorf("hearts event of 2193 = %d, want %d", down.EventType, event.EventType)
		}
	})
}
