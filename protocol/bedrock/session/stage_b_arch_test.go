package session

import (
	"bytes"
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/dm-vev/adamant/net/bedrock/proto"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
	"github.com/dm-vev/adamant/protocol/bedrock/netconn"
	"github.com/dm-vev/adamant/server/ecs"
	"github.com/dm-vev/adamant/server/entity"
	"github.com/dm-vev/adamant/server/item"
	"github.com/dm-vev/adamant/server/player"
	"github.com/dm-vev/adamant/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

type metaContributorEntity struct {
	movingEntity
	h     *world.EntityHandle
	patch world.ActorMetaPatch
}

func (e *metaContributorEntity) H() *world.EntityHandle                     { return e.h }
func (e *metaContributorEntity) ContributeMetadata(p *world.ActorMetaPatch) { *p = e.patch }

type metaContributorBehaviour struct {
	*entity.StationaryBehaviour
	patch world.ActorMetaPatch
}

func (b *metaContributorBehaviour) ContributeMetadata(p *world.ActorMetaPatch) { *p = b.patch }

type metaContributorBehConfig struct {
	patch world.ActorMetaPatch
}

func (c metaContributorBehConfig) Apply(data *world.EntityData) {
	data.Data = &metaContributorBehaviour{
		StationaryBehaviour: entity.StationaryBehaviourConfig{}.New(),
		patch:               c.patch,
	}
}

func TestMetaContributorAndECSMetaPatch(t *testing.T) {
	t.Parallel()
	w := metadataWorld(t)
	s := testSession("meta-patch")
	s.br = world.DefaultBlockRegistry

	<-w.Exec(func(tx *world.Tx) {
		// 1. MetadataContributor entity override.
		var cPatch world.ActorMetaPatch
		cPatch.SetNameTag("ContribMob")
		cPatch.SetAlwaysShowNameTag(true)
		cPatch.SetScoreTag("Score99")
		cPatch.SetScale(1.75)
		cPatch.SetWidth(1.1)
		cPatch.SetHeight(2.3)
		cPatch.SetVariant(5)
		cPatch.SetMarkVariant(8)
		cPatch.SetColor(12)
		cPatch.SetInvisible(true)
		cPatch.SetOnFire(true)
		cPatch.SetNoAI(true)
		cPatch.SetFlag(mc.EntityDataFlagSwimming)
		cPatch.ClearFlag(mc.EntityDataFlagClimb)
		cPatch.SetFlag(mc.EntityDataFlagCrawling)
		cPatch.ClearFlag(mc.EntityDataFlagAdmiring)
		cPatch.SetFloat("rage", 0.75)
		cPatch.SetInt("tier", 4)

		h := entity.NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 100, 0.5}})
		t.Cleanup(func() { _ = h.Close() })
		ce := &metaContributorEntity{h: h, patch: cPatch}

		m := s.parseEntityMetadata(ce)
		if m[mc.EntityDataKeyName] != "ContribMob" || m[mc.EntityDataKeyAlwaysShowNameTag] != byte(1) {
			t.Fatalf("nametag mismatch: name=%v always=%v", m[mc.EntityDataKeyName], m[mc.EntityDataKeyAlwaysShowNameTag])
		}
		if m[mc.EntityDataKeyScore] != "Score99" {
			t.Fatalf("score tag = %v, want Score99", m[mc.EntityDataKeyScore])
		}
		if m[mc.EntityDataKeyScale] != float32(1.75) || m[mc.EntityDataKeyWidth] != float32(1.1) || m[mc.EntityDataKeyHeight] != float32(2.3) {
			t.Fatalf("shape mismatch: scale=%v w=%v h=%v", m[mc.EntityDataKeyScale], m[mc.EntityDataKeyWidth], m[mc.EntityDataKeyHeight])
		}
		if m[mc.EntityDataKeyVariant] != int32(5) || m[mc.EntityDataKeyMarkVariant] != int32(8) || m[mc.EntityDataKeyColorIndex] != byte(12) {
			t.Fatalf("appearance mismatch: var=%v mark=%v color=%v", m[mc.EntityDataKeyVariant], m[mc.EntityDataKeyMarkVariant], m[mc.EntityDataKeyColorIndex])
		}
		for _, flag := range []uint8{mc.EntityDataFlagInvisible, mc.EntityDataFlagOnFire, mc.EntityDataFlagNoAI, mc.EntityDataFlagSwimming, mc.EntityDataFlagShowName, mc.EntityDataFlagAlwaysShowName} {
			if !m.Flag(mc.EntityDataKeyFlags, flag) {
				t.Fatalf("expected flag %d to be set", flag)
			}
		}
		if m.Flag(mc.EntityDataKeyFlags, mc.EntityDataFlagClimb) {
			t.Fatal("expected FlagClimb to be cleared")
		}
		if !m.Flag(mc.EntityDataKeyFlagsTwo, mc.EntityDataFlagCrawling-64) {
			t.Fatal("expected FlagCrawling in FlagsTwo to be set")
		}

		// ECS MetaPatchKey overrides MetadataContributor where both specify fields.
		var ecsOverride world.ActorMetaPatch
		ecsOverride.SetNameTag("ECSBoss")
		ecsOverride.SetInvisible(false)
		ecsOverride.SetInt("tier", 9)
		ecs.Set(h.Components(), world.MetaPatchKey, ecsOverride)

		m2 := s.parseEntityMetadata(ce)
		if m2[mc.EntityDataKeyName] != "ECSBoss" {
			t.Fatalf("ECS override NameTag = %v, want ECSBoss", m2[mc.EntityDataKeyName])
		}
		if m2.Flag(mc.EntityDataKeyFlags, mc.EntityDataFlagInvisible) {
			t.Fatal("expected ECS Invisible=false to override contributor Invisible=true")
		}
		if m2[mc.EntityDataKeyScale] != float32(1.75) {
			t.Fatalf("expected non-overridden contributor Scale=1.75 to be preserved, got %v", m2[mc.EntityDataKeyScale])
		}
		props := entityProperties(ce)
		if len(props.FloatProperties) != 1 || props.FloatProperties[0].Value != 0.75 {
			t.Fatalf("FloatProperties = %+v, want single value 0.75", props.FloatProperties)
		}
		if len(props.IntegerProperties) != 3 || props.IntegerProperties[2] != (mc.IntegerEntityProperty{Index: 2, Value: 9}) {
			t.Fatalf("IntegerProperties = %+v, want overridden tier at index 2 value 9", props.IntegerProperties)
		}

		// 2. Named actor properties (Pig climate_variant + Bee boolean property) via MetaPatchKey.
		pig := tx.AddEntity(entity.NewPig(world.EntitySpawnOpts{Position: mgl64.Vec3{2.5, 100, 0.5}}))
		var pigPatch world.ActorMetaPatch
		pigPatch.SetString("minecraft:climate_variant", "cold")
		ecs.Set(pig.H().Components(), world.MetaPatchKey, pigPatch)
		pigProps := entityProperties(pig)
		if len(pigProps.IntegerProperties) == 0 || pigProps.IntegerProperties[0].Value != 2 {
			t.Fatalf("pig climate_variant property = %+v, want index 0 value 2 (cold)", pigProps.IntegerProperties)
		}

		bee := tx.AddEntity(entity.NewBee(world.EntitySpawnOpts{Position: mgl64.Vec3{4.5, 100, 0.5}}))
		var beePatch world.ActorMetaPatch
		beePatch.SetInt("minecraft:has_nectar", 1)
		ecs.Set(bee.H().Components(), world.MetaPatchKey, beePatch)
		beeProps := entityProperties(bee)
		if len(beeProps.IntegerProperties) == 0 || beeProps.IntegerProperties[0].Value != 1 {
			t.Fatalf("bee has_nectar property = %+v, want index 0 value 1", beeProps.IntegerProperties)
		}

		// 3. Behaviour MetadataContributor on *entity.Ent.
		var behPatch world.ActorMetaPatch
		behPatch.SetScoreTag("BehScore")
		bh := world.EntitySpawnOpts{Position: mgl64.Vec3{6.5, 100, 0.5}}.New(entity.TextType, metaContributorBehConfig{patch: behPatch})
		behEnt := tx.AddEntity(bh)
		if got := s.parseEntityMetadata(behEnt)[mc.EntityDataKeyScore]; got != "BehScore" {
			t.Fatalf("behaviour MetadataContributor ScoreTag = %v, want BehScore", got)
		}

		// 4. ViewEntity (AddActorPacket) and ViewLayer AlwaysShowNameTag preserve MetaPatchKey NameTag.
		patchedCow := tx.AddEntity(entity.NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{10.5, 100, 0.5}}))
		var cowPatch world.ActorMetaPatch
		cowPatch.SetNameTag("PatchedCow")
		cowPatch.SetFloat("custom_rage", 0.5)
		ecs.Set(patchedCow.H().Components(), world.MetaPatchKey, cowPatch)
		vView := testSession("viewer-add")
		vView.br = world.DefaultBlockRegistry
		vView.viewLayer = world.NewViewLayer(vView)
		vView.viewLayerOwned = true
		vView.ViewAlwaysShowNameTag(patchedCow, false)
		takeTestPackets(vView)
		vView.ViewEntity(patchedCow)
		addActor := packetOf(vView.testPacket()).(*mc.AddActorPacket)
		if addActor.EntityMetadata[mc.EntityDataKeyName] != "PatchedCow" || addActor.EntityMetadata[mc.EntityDataKeyAlwaysShowNameTag] != byte(0) {
			t.Fatalf("AddActor metadata = name:%v always:%v, want PatchedCow/0", addActor.EntityMetadata[mc.EntityDataKeyName], addActor.EntityMetadata[mc.EntityDataKeyAlwaysShowNameTag])
		}
		if len(addActor.EntityProperties.FloatProperties) != 1 || addActor.EntityProperties.FloatProperties[0].Value != 0.5 {
			t.Fatalf("AddActor FloatProperties = %+v, want 0.5", addActor.EntityProperties.FloatProperties)
		}

		// 5. Shared state broadcast carries patched metadata & properties.
		var bc world.Broadcast
		v1, v2 := testSession("v1"), testSession("v2")
		v1.br, v2.br = world.DefaultBlockRegistry, world.DefaultBlockRegistry
		v1.ViewEntityStateShared(pig, &bc)
		v2.ViewEntityStateShared(pig, &bc)
		p1, p2 := v1.testPacket(), v2.testPacket()
		if p1 == nil || p1 != p2 {
			t.Fatalf("expected shared SetActorDataPacket across viewers, got %v vs %v", p1, p2)
		}
		sad := packetOf(p1).(*mc.SetActorDataPacket)
		if len(sad.EntityProperties.IntegerProperties) == 0 || sad.EntityProperties.IntegerProperties[0].Value != 2 {
			t.Fatalf("shared SetActorDataPacket properties = %+v, want climate_variant=2", sad.EntityProperties)
		}

		// 6. Standard unpatched entity metadata allocates no ECS patch and matches direct parse.
		plainCow := tx.AddEntity(entity.NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{12.5, 100, 0.5}}))
		if mayHaveMetaPatch(plainCow, metaCapsOf(plainCow), nil, metaCaps{}) {
			t.Fatal("standard unpatched cow should pass O(1) guard as false")
		}
	})
}

func TestEntityBroadcastViewerExtendedShared(t *testing.T) {
	t.Parallel()
	w := metadataWorld(t)

	newViewer := func(name string) *Session {
		s := testSession(name)
		s.br = world.DefaultBlockRegistry
		s.viewLayer = world.NewViewLayer(s)
		s.viewLayerOwned = true
		return s
	}
	v1, v2, direct := newViewer("v1"), newViewer("v2"), newViewer("direct")

	<-w.Exec(func(tx *world.Tx) {
		pos := mgl64.Vec3{8.5, 64, 8.5}
		p := tx.AddEntity(world.EntitySpawnOpts{Position: pos}.New(player.Type, player.Config{Position: pos})).(*player.Player)
		p.SetHeldItems(item.NewStack(item.Sword{Tier: item.ToolTierDiamond}, 1), item.NewStack(item.Shield{}, 1))
		p.Armour().SetHelmet(item.NewStack(item.Helmet{Tier: item.ArmourTierDiamond{}}, 1))

		selfViewer := testSessionOf("self", p.H(), uuid.NewString())
		selfViewer.br = world.DefaultBlockRegistry

		// 1. ViewEntityVelocityShared.
		var velBC world.Broadcast
		vel := mgl64.Vec3{0.25, 0.5, -0.25}
		v1.ViewEntityVelocityShared(p, vel, &velBC)
		v2.ViewEntityVelocityShared(p, vel, &velBC)
		selfViewer.ViewEntityVelocityShared(p, vel, &velBC)
		direct.ViewEntityVelocity(p, vel)

		pv1, pv2, pSelf, pDirect := v1.testPacket(), v2.testPacket(), selfViewer.testPacket(), direct.testPacket()
		if pv1 == nil || pv1 != pv2 {
			t.Fatalf("velocity not shared across v1/v2: %p vs %p", pv1, pv2)
		}
		if !bytes.Equal(encodeTestPacket(packetOf(pv1)), encodeTestPacket(packetOf(pDirect))) {
			t.Fatal("velocity shared bytes differ from direct ViewEntityVelocity")
		}
		if packetOf(pSelf).(*mc.SetActorMotionPacket).EntityRuntimeID != selfEntityRuntimeID {
			t.Fatalf("self velocity runtime ID = %d, want %d", packetOf(pSelf).(*mc.SetActorMotionPacket).EntityRuntimeID, selfEntityRuntimeID)
		}

		// Congested viewer drops non-self velocity.
		v1.congested.Store(true)
		v1.ViewEntityVelocityShared(p, vel, &velBC)
		if got := v1.testPacket(); got != nil {
			t.Fatalf("congested viewer should drop velocity, got %T", got)
		}
		v1.congested.Store(false)

		// 2. ViewEntityItemsShared (including justShown empty skipping).
		var itemBC world.Broadcast
		v1.justShown = map[*world.EntityHandle]struct{}{p.H(): {}}
		v1.ViewEntityItemsShared(p, &itemBC)
		v2.ViewEntityItemsShared(p, &itemBC)
		selfViewer.ViewEntityItemsShared(p, &itemBC)
		direct.ViewEntityItems(p)

		items1, items2, itemsSelf, itemsDirect := takeTestPackets(v1), takeTestPackets(v2), takeTestPackets(selfViewer), takeTestPackets(direct)
		if len(items1) != 2 || len(items2) != 2 || len(itemsDirect) != 2 || len(itemsSelf) != 0 {
			t.Fatalf("unexpected item packet counts v1=%d v2=%d direct=%d self=%d", len(items1), len(items2), len(itemsDirect), len(itemsSelf))
		}
		if items1[0] != items2[0] || items1[1] != items2[1] {
			t.Fatal("item packets not shared between v1 and v2")
		}
		for i := range itemsDirect {
			if !bytes.Equal(encodeTestPacket(packetOf(items1[i])), encodeTestPacket(packetOf(itemsDirect[i]))) {
				t.Fatalf("item packet %d bytes differ from direct", i)
			}
		}

		// Empty hand with justShown skips packet.
		bare := tx.AddEntity(world.EntitySpawnOpts{Position: pos}.New(player.Type, player.Config{Position: pos})).(*player.Player)
		v1.justShown[bare.H()] = struct{}{}
		var emptyItemBC world.Broadcast
		v1.ViewEntityItemsShared(bare, &emptyItemBC)
		v2.ViewEntityItemsShared(bare, &emptyItemBC)
		if got := takeTestPackets(v1); len(got) != 0 {
			t.Fatalf("justShown bare player sent %d item packets, want 0", len(got))
		}
		if got := takeTestPackets(v2); len(got) != 2 {
			t.Fatalf("non-justShown bare player sent %d item packets, want 2", len(got))
		}

		// 3. ViewEntityArmourShared + ViewLayer EnforceInvisible override.
		delete(v1.justShown, p.H())
		var armourBC world.Broadcast
		v1.ViewVisibility(p, world.EnforceInvisible())
		takeTestPackets(v1) // Drain ViewVisibility's own refresh packet if any.
		v1.ViewEntityArmourShared(p, &armourBC)
		v2.ViewEntityArmourShared(p, &armourBC)
		direct.ViewEntityArmour(p)
		pa1, pa2, paDir := v1.testPacket(), v2.testPacket(), direct.testPacket()
		if pa2 == nil || !bytes.Equal(encodeTestPacket(packetOf(pa2)), encodeTestPacket(packetOf(paDir))) {
			t.Fatal("armour shared bytes differ from direct")
		}
		if pa1 == nil || pa1 == pa2 {
			t.Fatalf("ViewLayer EnforceInvisible viewer got %v, must not share real armour packet", pa1)
		}
		invisibleArmour := packetOf(pa1).(*mc.MobArmourEquipmentPacket)
		if invisibleArmour.Helmet.Stack.NetworkID != 0 {
			t.Fatalf("invisible armour helmet NetworkID = %d, want 0", invisibleArmour.Helmet.Stack.NetworkID)
		}

		// Empty armour with justShown skips packet and clears justShown.
		var emptyArmourBC world.Broadcast
		v1.ViewEntityArmourShared(bare, &emptyArmourBC)
		v2.ViewEntityArmourShared(bare, &emptyArmourBC)
		if got := takeTestPackets(v1); len(got) != 0 {
			t.Fatalf("justShown bare player sent %d armour packets, want 0", len(got))
		}
		if got := takeTestPackets(v2); len(got) != 1 {
			t.Fatalf("non-justShown bare player sent %d armour packets, want 1", len(got))
		}
		if _, still := v1.justShown[bare.H()]; still {
			t.Fatal("expected ViewEntityArmourShared to clear justShown entry")
		}

		// 4. ViewEntityActionShared: HurtAction (with health), DeathAction, PickedUpAction, OfferFlowerAction.
		cow := tx.AddEntity(entity.NewCow(world.EntitySpawnOpts{Position: pos}))
		var hurtBC world.Broadcast
		v1.ViewEntityActionShared(cow, entity.HurtAction{}, &hurtBC)
		v2.ViewEntityActionShared(cow, entity.HurtAction{}, &hurtBC)
		direct.ViewEntityAction(cow, entity.HurtAction{})
		h1, h2, hDir := takeTestPackets(v1), takeTestPackets(v2), takeTestPackets(direct)
		if len(h1) != 2 || len(h2) != 2 || len(hDir) != 2 {
			t.Fatalf("hurt packet counts = (%d,%d,%d), want all 2", len(h1), len(h2), len(hDir))
		}
		if h1[0] != h2[0] || h1[1] != h2[1] {
			t.Fatal("hurt packets not shared")
		}

		// DeathAction: only viewers seeing cow record death animation and receive packet.
		v1.entityRuntimeIDs[cow.H()] = cow.H().RuntimeID()
		var deathBC world.Broadcast
		v2.ViewEntityActionShared(cow, entity.DeathAction{}, &deathBC) // v2 does not see cow
		v1.ViewEntityActionShared(cow, entity.DeathAction{}, &deathBC) // v1 sees cow
		if got := takeTestPackets(v2); len(got) != 0 {
			t.Fatalf("non-viewing session received %d death packets, want 0", len(got))
		}
		if got := takeTestPackets(v1); len(got) != 1 {
			t.Fatalf("viewing session received %d death packets, want 1", len(got))
		}
		if _, remembered := v1.deathAnimations[cow.H().RuntimeID()]; !remembered {
			t.Fatal("expected viewing session to record death animation")
		}

		// PickedUpAction: collector gets self runtime ID 1 privately while others share.
		var pickupBC world.Broadcast
		act := entity.PickedUpAction{Collector: p}
		selfViewer.ViewEntityActionShared(cow, act, &pickupBC)
		v1.ViewEntityActionShared(cow, act, &pickupBC)
		v2.ViewEntityActionShared(cow, act, &pickupBC)
		pkSelf := packetOf(selfViewer.testPacket()).(*mc.TakeItemActorPacket)
		pk1, pk2 := v1.testPacket(), v2.testPacket()
		if pkSelf.TakerEntityRuntimeID != selfEntityRuntimeID {
			t.Fatalf("collector TakerEntityRuntimeID = %d, want %d", pkSelf.TakerEntityRuntimeID, selfEntityRuntimeID)
		}
		if pk1 == nil || pk1 != pk2 {
			t.Fatalf("non-collector viewers did not share TakeItemActorPacket: %p vs %p", pk1, pk2)
		}
		if packetOf(pk1).(*mc.TakeItemActorPacket).TakerEntityRuntimeID != p.H().RuntimeID() {
			t.Fatalf("shared TakerEntityRuntimeID = %d, want %d", packetOf(pk1).(*mc.TakeItemActorPacket).TakerEntityRuntimeID, p.H().RuntimeID())
		}

		// OfferFlowerAction: congested viewer drops cosmetic while non-congested shares.
		var flowerBC world.Broadcast
		v1.congested.Store(true)
		v1.ViewEntityActionShared(cow, entity.OfferFlowerAction{}, &flowerBC)
		v2.ViewEntityActionShared(cow, entity.OfferFlowerAction{}, &flowerBC)
		if v1.testPacket() != nil {
			t.Fatal("congested viewer should drop OfferFlowerAction")
		}
		if v2.testPacket() == nil {
			t.Fatal("non-congested viewer should receive OfferFlowerAction")
		}
		v1.congested.Store(false)

		// CriticalHitAction and SwingArmAction share across non-self viewers and match direct wire bytes.
		for _, a := range []world.EntityAction{
			entity.CriticalHitAction{Count: 12},
			entity.LoveHeartsAction{},
			entity.SwingArmAction{},
		} {
			var actBC world.Broadcast
			v1.ViewEntityActionShared(p, a, &actBC)
			v2.ViewEntityActionShared(p, a, &actBC)
			direct.ViewEntityAction(p, a)
			ap1, ap2, apDir := v1.testPacket(), v2.testPacket(), direct.testPacket()
			if ap1 == nil || ap1 != ap2 {
				t.Fatalf("%T not shared across non-self viewers", a)
			}
			if !bytes.Equal(encodeTestPacket(packetOf(ap1)), encodeTestPacket(packetOf(apDir))) {
				t.Fatalf("%T shared bytes differ from direct", a)
			}
		}

		// Legacy protocol viewer falls back cleanly for items & armour.
		oldViewer := newViewer("old")
		oldViewer.conf.Protocol = oldProtocol()
		var oldItemsBC, oldArmourBC world.Broadcast
		oldViewer.ViewEntityItemsShared(p, &oldItemsBC)
		oldViewer.ViewEntityArmourShared(p, &oldArmourBC)
		if got := takeTestPackets(oldViewer); len(got) != 3 {
			t.Fatalf("old protocol fallback emitted %d packets, want 3", len(got))
		}
	})
}

type funcInterceptor struct {
	in  func(s *Session, pk proto.Packet) (proto.Packet, bool)
	out func(s *Session, pk proto.Packet) (proto.Packet, bool)
}

func (f funcInterceptor) OnIncoming(s *Session, pk proto.Packet) (proto.Packet, bool) {
	if f.in != nil {
		return f.in(s, pk)
	}
	return pk, true
}

func (f funcInterceptor) OnOutgoing(s *Session, pk proto.Packet) (proto.Packet, bool) {
	if f.out != nil {
		return f.out(s, pk)
	}
	return pk, true
}

func TestPacketInterceptorsAndSendRawPacket(t *testing.T) {
	t.Parallel()
	w := metadataWorld(t)

	// 1. Incoming rewrite and cancel, plus Config.Interceptors.
	var logIn []uint32
	var dropChat atomic.Bool
	globalIC := funcInterceptor{
		in: func(_ *Session, pk proto.Packet) (proto.Packet, bool) {
			logIn = append(logIn, pk.ID())
			if dropChat.Load() && pk.ID() == mc.IDText {
				return nil, false
			}
			if r, ok := pk.(*mc.RequestChunkRadiusPacket); ok {
				cp := *r
				cp.ChunkRadius = 4
				return &cp, true
			}
			return pk, true
		},
		out: func(_ *Session, pk proto.Packet) (proto.Packet, bool) {
			if tpk, ok := pk.(*mc.SetTimePacket); ok && tpk.Time == 999 {
				return nil, false
			}
			if tpk, ok := pk.(*mc.SetTimePacket); ok {
				cp := *tpk
				cp.Time = tpk.Time + 10
				return &cp, true
			}
			return pk, true
		},
	}

	s := Config{
		Log:            slog.New(slog.DiscardHandler),
		MaxChunkRadius: 16,
		Interceptors:   []PacketInterceptor{globalIC},
	}.New(benchmarkConn{})
	s.BeginBatch()
	drainPackets(s) // Clear initial Config.New packets.

	// Zero-interceptor session fast path check.
	plain := testSession("plain")
	if plain.hasInInterceptors.Load() || plain.hasOutInterceptors.Load() {
		t.Fatal("expected plain session without interceptors to have false fast-path flags")
	}

	if err := w.Do(func(tx *world.Tx) {
		pos := mgl64.Vec3{0.5, 64, 0.5}
		p := tx.AddEntity(world.EntitySpawnOpts{Position: pos}.New(player.Type, player.Config{Position: pos})).(*player.Player)

		// Rewrite incoming RequestChunkRadiusPacket 12 -> 4.
		if err := s.HandlePacket(&mc.RequestChunkRadiusPacket{ChunkRadius: 12}, tx, p); err != nil {
			t.Fatalf("HandlePacket RequestChunkRadius: %v", err)
		}
		if got := s.ChunkRadius(); got != 4 {
			t.Fatalf("ChunkRadius = %d, want rewritten 4", got)
		}
		if cr, ok := s.testPacket().(*mc.ChunkRadiusUpdatedPacket); !ok || cr.ChunkRadius != 4 {
			t.Fatalf("ChunkRadiusUpdatedPacket = %#v, want radius 4", cr)
		}

		// Drop incoming TextPacket before handler runs (no error, ignored).
		dropChat.Store(true)
		if err := s.HandlePacket(&mc.TextPacket{SourceName: "mismatch", Message: "dropped"}, tx, p); err != nil {
			t.Fatalf("expected dropped TextPacket to return nil err, got %v", err)
		}
		dropChat.Store(false)
		if len(logIn) != 2 {
			t.Fatalf("incoming interceptor count = %d, want 2", len(logIn))
		}
	}).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Outgoing rewrite & cancel + SendRawPacket.
	s.SendRawPacket(&mc.SetTimePacket{Time: 999})
	if got := s.testPacket(); got != nil {
		t.Fatalf("expected cancelled packet to be dropped, got %T", got)
	}
	s.SendRawPacket(&mc.SetTimePacket{Time: 50})
	gotTime, ok := s.testPacket().(*mc.SetTimePacket)
	if !ok || gotTime.Time != 60 {
		t.Fatalf("SendRawPacket rewritten SetTimePacket = %#v, want Time=60", gotTime)
	}

	// 2. Shared packet unwrapping & isolation: intercepted session must not corrupt shared packet for other viewers.
	sClean := testSession("clean")
	sHooked := testSession("hooked")
	var sawUnwrapped bool
	remove := sHooked.AddInterceptor(funcInterceptor{
		out: func(_ *Session, pk proto.Packet) (proto.Packet, bool) {
			mot, isMot := pk.(*mc.SetActorMotionPacket)
			if !isMot {
				t.Errorf("expected unwrapped *mc.SetActorMotionPacket, got %T", pk)
				return pk, true
			}
			sawUnwrapped = true
			cp := *mot
			cp.Velocity[1] = 42
			return &cp, true
		},
	})

	// Direct SendRawPacket with *netconn.Shared as well as ViewEntityVelocityShared.
	sharedDirect := &netconn.Shared{Packet: &mc.SetActorMotionPacket{EntityRuntimeID: 77, Velocity: [3]float32{1, 2, 3}}}
	sHooked.SendRawPacket(sharedDirect)
	sClean.SendRawPacket(sharedDirect)
	if !sawUnwrapped {
		t.Fatal("outgoing interceptor did not observe unwrapped packet")
	}
	gotHooked := sHooked.testPacket().(*mc.SetActorMotionPacket)
	gotClean := packetOf(sClean.testPacket()).(*mc.SetActorMotionPacket)
	if gotHooked.Velocity[1] != 42 || gotClean.Velocity[1] != 2 {
		t.Fatalf("shared packet isolation failed: hooked=%v clean=%v", gotHooked.Velocity[1], gotClean.Velocity[1])
	}

	// Verify ViewEntityVelocityShared isolates hooked session even with in-place mutation.
	inPlace := testSession("inplace")
	remInPlace := inPlace.AddInterceptor(funcInterceptor{
		out: func(_ *Session, pk proto.Packet) (proto.Packet, bool) {
			if mot, isMot := pk.(*mc.SetActorMotionPacket); isMot {
				mot.Velocity[1] = 88
			}
			return pk, true
		},
	})
	<-w.Exec(func(tx *world.Tx) {
		liveCow := tx.AddEntity(entity.NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 64, 0.5}}))
		var velBC world.Broadcast
		inPlace.ViewEntityVelocityShared(liveCow, mgl64.Vec3{0, 5, 0}, &velBC)
		sClean.ViewEntityVelocityShared(liveCow, mgl64.Vec3{0, 5, 0}, &velBC)
		pHooked := inPlace.testPacket().(*mc.SetActorMotionPacket)
		pClean := packetOf(sClean.testPacket()).(*mc.SetActorMotionPacket)
		if pHooked.Velocity[1] != 88 || pClean.Velocity[1] != 5 {
			t.Fatalf("broadcast isolation failed: hooked=%v clean=%v", pHooked.Velocity[1], pClean.Velocity[1])
		}
	})
	remInPlace()

	// 3. Idempotent removal restores fast path.
	remove()
	remove()
	if sHooked.hasInInterceptors.Load() || sHooked.hasOutInterceptors.Load() {
		t.Fatal("expected removing all interceptors to clear fast-path flags")
	}
	sHooked.SendRawPacket(&mc.SetTimePacket{Time: 123})
	if pk := sHooked.testPacket().(*mc.SetTimePacket); pk.Time != 123 {
		t.Fatalf("after remove got Time=%d, want 123", pk.Time)
	}

	// 4. Nil / nop safety.
	nop.SendRawPacket(&mc.SetTimePacket{Time: 1})
	remNop := nop.AddInterceptor(globalIC)
	remNop()
	remNil := sHooked.AddInterceptor(nil)
	remNil()
	sHooked.SendRawPacket(nil)

	// 5. Concurrent AddInterceptor / remove / SendRawPacket under -race.
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 50 {
				rem := sHooked.AddInterceptor(funcInterceptor{})
				sHooked.SendRawPacket(&mc.SetTimePacket{Time: 7})
				rem()
			}
		}()
	}
	wg.Wait()
	drainPackets(sHooked)
}
