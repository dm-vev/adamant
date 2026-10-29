package session

import (
	"context"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

func TestMobDeathAnimationSurvivesRemoval(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	t.Cleanup(func() { _ = w.Close() })
	if err := w.Do(func(tx *world.Tx) {
		e := tx.AddEntity(entity.NewCow(world.EntitySpawnOpts{}))
		viewer := testSession("death-animation")
		viewer.entityRuntimeIDs[e.H()] = 2
		viewer.entities[2] = e.H()
		viewer.ViewEntityAction(e, entity.DeathAction{})
		viewer.HideEntity(e)
		viewer.HideEntity(e)
		pk, ok := (<-viewer.packets).(*v2193.ActorEventPacket)
		if !ok || pk.EventType != packet.ActorEventDeath || pk.EntityRuntimeID != 2 {
			t.Errorf("death packet = %#v, want actor 2 death event", pk)
		}
		select {
		case pk := <-viewer.packets:
			t.Errorf("packet after death interrupts native animation: %T", pk)
		default:
		}
		if _, ok := viewer.entityRuntimeIDs[e.H()]; ok {
			t.Error("removed mob retained its runtime ID")
		}
		if _, ok := viewer.entities[2]; ok {
			t.Error("removed mob retained its handle")
		}
		if len(viewer.deathAnimations) != 0 {
			t.Error("removed mob retained death-animation bookkeeping")
		}
	}).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestForcedHideStillRemovesDyingMob(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	t.Cleanup(func() { _ = w.Close() })
	if err := w.Do(func(tx *world.Tx) {
		e := tx.AddEntity(entity.NewCow(world.EntitySpawnOpts{}))
		viewer := testSession("forced-hide")
		viewer.hiddenEntities = make(map[uuid.UUID]struct{})
		viewer.entityRuntimeIDs[e.H()] = 2
		viewer.entities[2] = e.H()
		viewer.ViewEntityAction(e, entity.DeathAction{})
		<-viewer.packets
		viewer.StopShowingEntity(e)
		if pk, ok := (<-viewer.packets).(*v2193.RemoveActorPacket); !ok || pk.EntityUniqueID != e.H().UniqueID() {
			t.Errorf("forced-hide packet = %#v, want RemoveActor", pk)
		}
	}).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestSpecialDeathSequencesStillSendRemoveActor(t *testing.T) {
	for _, test := range []struct {
		name  string
		spawn func(world.EntitySpawnOpts) *world.EntityHandle
	}{
		{"wither", entity.NewWither},
		{"ender dragon", entity.NewEnderDragon},
		{"creaking", entity.NewCreaking},
	} {
		t.Run(test.name, func(t *testing.T) {
			w := world.Config{Synchronous: true}.New()
			t.Cleanup(func() { _ = w.Close() })
			h := test.spawn(world.EntitySpawnOpts{})
			t.Cleanup(func() { _ = h.Close() })
			if err := w.Do(func(tx *world.Tx) {
				e := h.WorldlessEntity(tx)
				viewer := testSession("special-death")
				viewer.entityRuntimeIDs[h] = 2
				viewer.entities[2] = h
				viewer.ViewEntityAction(e, entity.DeathAction{})
				<-viewer.packets
				viewer.HideEntity(e)
				if pk, ok := (<-viewer.packets).(*v2193.RemoveActorPacket); !ok || pk.EntityUniqueID != h.UniqueID() {
					t.Errorf("special-death removal = %#v, want RemoveActor", pk)
				}
			}).Wait(context.Background()); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestKilledMobSendsDeathWithoutImmediateRemoveActor(t *testing.T) {
	for _, test := range []struct {
		name  string
		spawn func(world.EntitySpawnOpts) *world.EntityHandle
	}{
		{"cow", entity.NewCow},
		{"zombie", entity.NewZombie},
		{"blaze", entity.NewBlaze},
	} {
		t.Run(test.name, func(t *testing.T) {
			w := world.Config{Synchronous: true, Entities: entity.DefaultRegistry}.New()
			t.Cleanup(func() { _ = w.Close() })
			viewer := testSession("lethal-hit")
			viewer.br = world.DefaultBlockRegistry
			viewer.packets = make(chan proto.Packet, 1024)
			var loader *world.Loader
			if err := w.Do(func(tx *world.Tx) {
				tx.SetBlock(cube.Pos{8, 63, 8}, block.Stone{}, nil)
				loader = world.NewLoader(1, w, deathAnimationViewer{session: viewer})
				loader.Move(tx, mgl64.Vec3{8.5, 64, 8.5})
				loader.Load(tx, 1)
			}).Wait(context.Background()); err != nil {
				t.Fatal(err)
			}
			var runtimeID uint64
			var uniqueID int64
			if err := w.Do(func(tx *world.Tx) {
				defer loader.Close(tx)
				h := test.spawn(world.EntitySpawnOpts{Position: mgl64.Vec3{8.5, 64, 8.5}})
				mob := tx.AddEntity(h).(entity.Living)
				runtimeID, uniqueID = viewer.entityRuntimeIDs[h], h.UniqueID()
				mob.Hurt(1000, entity.FallDamageSource{})
				if !mob.Dead() || !h.Closed() {
					t.Error("lethal hit did not finish server-side death")
				}
				if _, ok := viewer.entityRuntimeIDs[h]; ok {
					t.Error("lethal hit retained a targetable runtime ID")
				}
			}).Wait(context.Background()); err != nil {
				t.Fatal(err)
			}
			deaths := 0
			for len(viewer.packets) > 0 {
				switch pk := (<-viewer.packets).(type) {
				case *v2193.ActorEventPacket:
					if pk.EntityRuntimeID == runtimeID && pk.EventType == packet.ActorEventDeath {
						deaths++
					}
				case *v2193.RemoveActorPacket:
					if pk.EntityUniqueID == uniqueID {
						t.Error("lethal hit immediately removed the dying actor")
					}
				}
			}
			if runtimeID == 0 || deaths != 1 {
				t.Errorf("death events=%d for runtime ID %d, want one visible death", deaths, runtimeID)
			}
		})
	}
}

type deathAnimationViewer struct {
	world.NopViewer
	session *Session
}

func (v deathAnimationViewer) ViewEntity(e world.Entity) { v.session.ViewEntity(e) }
func (v deathAnimationViewer) HideEntity(e world.Entity) { v.session.HideEntity(e) }
func (v deathAnimationViewer) ViewEntityAction(e world.Entity, a world.EntityAction) {
	v.session.ViewEntityAction(e, a)
}

func TestMobRemovalWithoutDeathStillSendsRemoveActor(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	t.Cleanup(func() { _ = w.Close() })
	if err := w.Do(func(tx *world.Tx) {
		e := tx.AddEntity(entity.NewCow(world.EntitySpawnOpts{}))
		viewer := testSession("despawn")
		viewer.entityRuntimeIDs[e.H()] = 2
		viewer.entities[2] = e.H()
		viewer.HideEntity(e)
		pk, ok := (<-viewer.packets).(*v2193.RemoveActorPacket)
		if !ok || pk.EntityUniqueID != e.H().UniqueID() {
			t.Errorf("removal packet = %#v, want RemoveActor for %d", pk, e.H().UniqueID())
		}
	}).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}
