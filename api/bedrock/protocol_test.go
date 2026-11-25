package bedrock_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/df-mc/dragonfly/api"
	"github.com/df-mc/dragonfly/api/bedrock"
	bedrockadapter "github.com/df-mc/dragonfly/protocol/bedrock"
	"github.com/df-mc/dragonfly/protocol/bedrock/session"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
)

func TestMultiProtocolAndVirtualMenuWirePackets(t *testing.T) {
	t.Parallel()

	world.DefaultBlockRegistry.Finalize()
	w := world.Config{
		Log:             slog.New(slog.NewTextHandler(io.Discard, nil)),
		Generator:       world.NopGenerator{},
		Provider:        world.NopProvider{},
		RandomTickSpeed: -1,
		Blocks:          world.DefaultBlockRegistry,
		Entities:        entity.DefaultRegistry,
	}.New()
	t.Cleanup(func() { _ = w.Close() })

	conn := newMemConn()
	sess, p := spawnSessionPlayer(t, w, conn)

	protoNum, ok := bedrock.ProtocolVersion(p)
	if !ok || protoNum == 0 || bedrock.SessionProtocol(sess) != protoNum {
		t.Fatalf("ProtocolVersion = (%d, %v), SessionProtocol = %d", protoNum, ok, bedrock.SessionProtocol(sess))
	}
	ver, ok := bedrock.GameVersion(p)
	if !ok || ver == "" || bedrock.SessionGameVersion(sess) != ver {
		t.Fatalf("GameVersion = (%q, %v)", ver, ok)
	}

	// ForProtocols should NOT trigger on non-matching protocol, and SHOULD trigger on matching protocol.
	var wrongHits, rightHits, rangeHits atomic.Int32
	remWrong := bedrock.InterceptSession(sess, bedrock.ForProtocols(bedrock.FuncInterceptor{
		OnOut: func(_ *session.Session, pk proto.Packet) (proto.Packet, bool) {
			wrongHits.Add(1)
			return pk, true
		},
	}, 99999))
	remRight := bedrock.InterceptSession(sess, bedrock.ForProtocols(bedrock.FuncInterceptor{
		OnOut: func(_ *session.Session, pk proto.Packet) (proto.Packet, bool) {
			rightHits.Add(1)
			return pk, true
		},
	}, protoNum))
	remRange := bedrock.InterceptSession(sess, bedrock.ForProtocolRange(bedrock.FuncInterceptor{
		OnOut: func(_ *session.Session, pk proto.Packet) (proto.Packet, bool) {
			rangeHits.Add(1)
			return pk, true
		},
	}, 2168, 2225))
	defer remWrong()
	defer remRight()
	defer remRange()

	var seenProto int32
	sent := bedrock.SendPlayerVersionedPacket(p, func(pn int32) proto.Packet {
		seenProto = pn
		return &mc.TextPacket{TextType: mc.TextTypeRaw, Message: "versioned-hello"}
	})
	if !sent || seenProto != protoNum {
		t.Fatalf("SendPlayerVersionedPacket = (%v, proto=%d), want (true, %d)", sent, seenProto, protoNum)
	}
	waitForPacket(t, conn, func(pk *mc.TextPacket) bool {
		return pk.Message == "versioned-hello"
	})
	if wrongHits.Load() != 0 || rightHits.Load() == 0 || rangeHits.Load() == 0 {
		t.Fatalf("protocol filter hits: wrong=%d right=%d range=%d", wrongHits.Load(), rightHits.Load(), rangeHits.Load())
	}

	// Verify Virtual Double Chest sends paired BlockActorData with CustomName + ContainerOpen.
	doubleChest := api.NewMenu(api.MenuSpec{
		Title:    "Vault",
		Kind:     api.MenuDoubleChest,
		ReadOnly: true,
		Slots: map[int]api.MenuSlot{
			0: {Item: item.NewStack(item.Diamond{}, 1), Locked: true},
		},
	})
	if err := w.Do(func(tx *world.Tx) {
		doubleChest.Open(p, tx)
	}).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	actor := waitForPacket(t, conn, func(pk *mc.BlockActorDataPacket) bool {
		return pk.NBTData["id"] == "Chest" && pk.NBTData["CustomName"] == "Vault" && pk.NBTData["pairlead"] == byte(1)
	})
	if actor == nil {
		t.Fatal("expected paired virtual double chest BlockActorDataPacket with CustomName=Vault")
	}
	waitForPacket(t, conn, func(pk *mc.ContainerOpenPacket) bool {
		return pk.ContainerType == mc.ContainerTypeContainer
	})
	if err := w.Do(func(tx *world.Tx) {
		p.CloseMenu(false)
	}).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Verify LoadResourcePacksDir on an empty dir and WithResourcePacks helper.
	emptyDir := t.TempDir()
	packs, err := bedrock.LoadResourcePacksDir(emptyDir)
	if err != nil || len(packs) != 0 {
		t.Fatalf("LoadResourcePacksDir(empty) = (%d, %v)", len(packs), err)
	}
	if _, err := bedrock.LoadResourcePack(filepath.Join(emptyDir, "missing.zip")); err == nil && !os.IsNotExist(err) {
		t.Fatal("expected error loading missing resource pack")
	}
	cfg := bedrock.WithResourcePacks(bedrockadapter.Config{}, true, nil)
	if !cfg.ResourcesRequired || len(cfg.Resources) != 0 {
		t.Fatalf("WithResourcePacks = required:%v len:%d", cfg.ResourcesRequired, len(cfg.Resources))
	}
}
