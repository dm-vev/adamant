package session

import (
	"bytes"
	"testing"

	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/entity"
	"github.com/dm-vev/adamant/server/text"
	"github.com/dm-vev/adamant/server/world"
	"github.com/dm-vev/adamant/server/world/particle"
	"github.com/dm-vev/adamant/server/world/sound"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
	"github.com/dm-vev/adamant/net/bedrock/wire"
	"github.com/go-gl/mathgl/mgl64"
)

// TestBroadcastsAreEncodedOnce shows a sound, a particle and an animation
// to the sessions of one broadcast: they are given the same packets, but for the
// player of a session, which knows itself by runtime ID 1.
func TestBroadcastsAreEncodedOnce(t *testing.T) {
	t.Parallel()
	a, b := testSession("a"), testSession("b")
	var sb world.Broadcast
	a.ViewSoundShared(mgl64.Vec3{1, 2, 3}, sound.Click{}, &sb)
	b.ViewSoundShared(mgl64.Vec3{1, 2, 3}, sound.Click{}, &sb)
	if pa, pb := a.testPacket(), b.testPacket(); pa == nil || pa != pb {
		t.Fatalf("sound packets %v and %v are not one", pa, pb)
	}
	var partb world.Broadcast
	a.ViewParticleShared(mgl64.Vec3{1, 2, 3}, particle.HugeExplosion{}, &partb)
	b.ViewParticleShared(mgl64.Vec3{1, 2, 3}, particle.HugeExplosion{}, &partb)
	if pa, pb := a.testPacket(), b.testPacket(); pa == nil || pa != pb {
		t.Fatalf("particle packets %v and %v are not one", pa, pb)
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

func encodeTestPacket(pk proto.Packet) []byte {
	w := &wire.Writer{}
	pk.Encode(w)
	return w.B
}

func takeTestPackets(s *Session) []proto.Packet {
	s.packetsMu.Lock()
	defer s.packetsMu.Unlock()
	out := append([]proto.Packet(nil), s.heldPackets...)
	clear(s.heldPackets)
	s.heldPackets = s.heldPackets[:0]
	return out
}

func TestViewEntityStateSharedReusesAndIsolates(t *testing.T) {
	t.Parallel()
	w := metadataWorld(t)
	newViewer := func(name string, withViewLayer bool) *Session {
		s := testSession(name)
		s.br = world.DefaultBlockRegistry
		if withViewLayer {
			s.viewLayer = world.NewViewLayer(s)
			s.viewLayerOwned = true
		}
		return s
	}
	a, b, c := newViewer("a", true), newViewer("b", true), newViewer("c", false)

	<-w.Exec(func(tx *world.Tx) {
		// 1. Regular mob: sessions with empty owned ViewLayer and nil ViewLayer share one packet.
		pig := tx.AddEntity(entity.NewPig(world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 100, 0.5}}))
		var bc world.Broadcast
		a.ViewEntityStateShared(pig, &bc)
		b.ViewEntityStateShared(pig, &bc)
		c.ViewEntityStateShared(pig, &bc)
		pa, pb, pc := a.testPacket(), b.testPacket(), c.testPacket()
		if pa == nil || pa != pb || pa != pc {
			t.Fatalf("pig shared packets differ: %p, %p, %p", pa, pb, pc)
		}

		// 2. Mount: SetActorDataPacket and UpdateAttributesPacket are both shared.
		horse := tx.AddEntity(entity.NewHorse(world.EntitySpawnOpts{Position: mgl64.Vec3{2.5, 100, 0.5}}))
		var mountBC world.Broadcast
		a.ViewEntityStateShared(horse, &mountBC)
		b.ViewEntityStateShared(horse, &mountBC)
		pksA, pksB := takeTestPackets(a), takeTestPackets(b)
		if len(pksA) != 2 || len(pksB) != 2 {
			t.Fatalf("mount packet counts = %d/%d, want 2/2", len(pksA), len(pksB))
		}
		if pksA[0] != pksB[0] || pksA[1] != pksB[1] {
			t.Fatalf("mount packets not shared: %p/%p vs %p/%p", pksA[0], pksA[1], pksB[0], pksB[1])
		}

		// 3. ViewLayer override: overridden viewer gets private metadata, others still share.
		a.ViewNameTag(pig, text.ParseLegacy("SecretPig"))
		takeTestPackets(a)
		var vlBC world.Broadcast
		a.ViewEntityStateShared(pig, &vlBC)
		b.ViewEntityStateShared(pig, &vlBC)
		c.ViewEntityStateShared(pig, &vlBC)
		pa, pb, pc = a.testPacket(), b.testPacket(), c.testPacket()
		if pb == nil || pb != pc {
			t.Fatalf("public viewers did not share after another viewer set ViewLayer: %p vs %p", pb, pc)
		}
		if pa == pb {
			t.Fatal("overridden viewer shared public packet")
		}
		if got := packetOf(pa).(*mc.SetActorDataPacket).EntityMetadata[mc.EntityDataKeyName]; got != "SecretPig" {
			t.Fatalf("overridden name tag = %v, want SecretPig", got)
		}

		// 4. Self entity uses selfEntityRuntimeID privately.
		selfEnt := movingEntity{h: a.ent}
		var selfBC world.Broadcast
		b.ViewEntityStateShared(selfEnt, &selfBC)
		a.ViewEntityStateShared(selfEnt, &selfBC)
		pb, pa = b.testPacket(), a.testPacket()
		if packetOf(pa).(*mc.SetActorDataPacket).EntityRuntimeID != selfEntityRuntimeID {
			t.Fatalf("self runtime ID = %d, want %d", packetOf(pa).(*mc.SetActorDataPacket).EntityRuntimeID, selfEntityRuntimeID)
		}
		if packetOf(pa) == packetOf(pb) {
			t.Fatal("self entity shared packet with other viewer")
		}

		// 5. Owned entity: owner gets selfEntityRuntimeID (1) for EntityDataKeyOwner privately.
		ownedEnt := ownedTestEntity{h: pig.H(), owner: a.ent}
		var ownBC world.Broadcast
		a.ViewEntityStateShared(ownedEnt, &ownBC)
		b.ViewEntityStateShared(ownedEnt, &ownBC)
		c.ViewEntityStateShared(ownedEnt, &ownBC)
		pa, pb, pc = a.testPacket(), b.testPacket(), c.testPacket()
		if pb == nil || pb != pc {
			t.Fatalf("non-owner viewers did not share owned entity packet: %p vs %p", pb, pc)
		}
		if got := packetOf(pa).(*mc.SetActorDataPacket).EntityMetadata[mc.EntityDataKeyOwner]; got != int64(selfEntityRuntimeID) {
			t.Fatalf("owner viewer key = %v, want %d", got, selfEntityRuntimeID)
		}
		if got := packetOf(pb).(*mc.SetActorDataPacket).EntityMetadata[mc.EntityDataKeyOwner]; got != a.ent.UniqueID() {
			t.Fatalf("other viewer owner key = %v, want %d", got, a.ent.UniqueID())
		}
	})
}

func TestViewEntityStateSharedMatchesDirectForAllRegistryEntities(t *testing.T) {
	t.Parallel()
	w := metadataWorld(t)
	direct := testSession("direct")
	direct.br = world.DefaultBlockRegistry
	shared1 := testSession("shared-1")
	shared1.br = world.DefaultBlockRegistry
	shared1.viewLayer = world.NewViewLayer(shared1)
	shared1.viewLayerOwned = true
	shared2 := testSession("shared-2")
	shared2.br = world.DefaultBlockRegistry
	shared2.viewLayer = world.NewViewLayer(shared2)
	shared2.viewLayerOwned = true

	<-w.Exec(func(tx *world.Tx) {
		ents, err := registryEntities(tx)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range ents {
			if metadataPanics(direct, e) {
				t.Errorf("%s panicked in parseEntityMetadata", e.H().Type().EncodeEntity())
				continue
			}
			direct.ViewEntityState(e)
			var bc world.Broadcast
			shared1.ViewEntityStateShared(e, &bc)
			shared2.ViewEntityStateShared(e, &bc)

			wantPks := takeTestPackets(direct)
			got1 := takeTestPackets(shared1)
			got2 := takeTestPackets(shared2)
			if len(got1) != len(wantPks) || len(got2) != len(wantPks) {
				t.Fatalf("%s: packet counts direct=%d shared1=%d shared2=%d", e.H().Type().EncodeEntity(), len(wantPks), len(got1), len(got2))
			}
			for i := range wantPks {
				if got1[i] != got2[i] {
					t.Errorf("%s packet %d: shared viewers received distinct packets", e.H().Type().EncodeEntity(), i)
				}
				wantBytes := encodeTestPacket(packetOf(wantPks[i]))
				gotBytes := encodeTestPacket(packetOf(got1[i]))
				if !bytes.Equal(wantBytes, gotBytes) {
					t.Errorf("%s packet %d wire bytes differ between ViewEntityState and ViewEntityStateShared", e.H().Type().EncodeEntity(), i)
				}
			}
		}
	})
}

type nonComparableEntityType struct {
	tags []string
}

func (nonComparableEntityType) Open(*world.Tx, *world.EntityHandle, *world.EntityData) world.Entity {
	return nil
}
func (nonComparableEntityType) EncodeEntity() string { return "test:non_comparable" }
func (nonComparableEntityType) BBox(world.Entity) cube.BBox {
	return cube.Box(-0.25, 0, -0.25, 0.25, 0.5, 0.25)
}
func (nonComparableEntityType) DecodeNBT(map[string]any, *world.EntityData) {}
func (nonComparableEntityType) EncodeNBT(*world.EntityData) map[string]any  { return nil }

func TestNonComparableEntityTypeMetadataSafe(t *testing.T) {
	t.Parallel()
	h := world.EntitySpawnOpts{}.New(nonComparableEntityType{tags: []string{"a"}}, entity.StationaryBehaviourConfig{})
	t.Cleanup(func() { _ = h.Close() })
	e := movingEntity{h: h}
	a, b := testSession("nc-a"), testSession("nc-b")
	a.br, b.br = world.DefaultBlockRegistry, world.DefaultBlockRegistry
	for range 3 {
		m := a.parseEntityMetadata(e)
		if m[mc.EntityDataKeyWidth] != float32(0.5) || m[mc.EntityDataKeyHeight] != float32(0.5) {
			t.Fatalf("unexpected bbox metadata: width=%v height=%v", m[mc.EntityDataKeyWidth], m[mc.EntityDataKeyHeight])
		}
		var bc world.Broadcast
		a.ViewEntityStateShared(e, &bc)
		b.ViewEntityStateShared(e, &bc)
		pa, pb := a.testPacket(), b.testPacket()
		if pa == nil || pa != pb {
			t.Fatalf("non-comparable entity type broadcast not shared: %p vs %p", pa, pb)
		}
	}
}

type ownedTestEntity struct {
	movingEntity
	h     *world.EntityHandle
	owner *world.EntityHandle
}

func (e ownedTestEntity) H() *world.EntityHandle     { return e.h }
func (e ownedTestEntity) Owner() *world.EntityHandle { return e.owner }
