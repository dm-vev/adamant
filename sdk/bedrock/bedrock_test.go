package bedrock_test

import (
	"context"
	"image"
	"image/color"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	bedrockadapter "github.com/df-mc/dragonfly/protocol/bedrock"
	"github.com/df-mc/dragonfly/protocol/bedrock/netconn"
	"github.com/df-mc/dragonfly/protocol/bedrock/session"
	"github.com/df-mc/dragonfly/sdk/bedrock"
	"github.com/df-mc/dragonfly/sdk/event"
	"github.com/df-mc/dragonfly/sdk/menu"
	"github.com/df-mc/dragonfly/sdk/mob"
	"github.com/df-mc/dragonfly/sdk/plugin"
	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/client/clienttest"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/skin"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/login"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

type memConn struct {
	identity   login.Identity
	packets    chan proto.Packet
	readErrors chan error
	closed     chan struct{}
	once       sync.Once
	mu         sync.Mutex
	writes     []proto.Packet
	notify     chan proto.Packet
}

func newMemConn() *memConn {
	return &memConn{
		identity: login.Identity{
			Identity:    uuid.NewString(),
			DisplayName: "BedrockSDK",
			XUID:        "2535400000000999",
		},
		packets:    make(chan proto.Packet, 16),
		readErrors: make(chan error, 1),
		closed:     make(chan struct{}),
		notify:     make(chan proto.Packet, 64),
	}
}

func (c *memConn) Close() error {
	c.once.Do(func() { close(c.closed) })
	return nil
}

func (c *memConn) IdentityData() login.Identity { return c.identity }
func (*memConn) ClientData() login.ClientData   { return login.ClientData{} }
func (*memConn) ClientCacheEnabled() bool       { return false }
func (*memConn) ChunkRadius() int               { return 1 }
func (*memConn) Latency() time.Duration         { return 0 }
func (*memConn) Flush() error                   { return nil }
func (*memConn) RemoteAddr() net.Addr           { return &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 19132} }

func (c *memConn) ReadPacket() (proto.Packet, error) {
	select {
	case pk := <-c.packets:
		return pk, nil
	case err := <-c.readErrors:
		return nil, err
	case <-c.closed:
		return nil, io.EOF
	}
}

func (c *memConn) WritePacket(pk proto.Packet) error {
	c.mu.Lock()
	c.writes = append(c.writes, pk)
	c.mu.Unlock()
	select {
	case c.notify <- pk:
	default:
	}
	return nil
}

func (*memConn) StartGameContext(context.Context, netconn.GameData) error { return nil }

func (c *memConn) written() []proto.Packet {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]proto.Packet, len(c.writes))
	copy(out, c.writes)
	return out
}

func waitForPacket[P proto.Packet](t *testing.T, c *memConn, pred func(P) bool) P {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		for _, pk := range c.written() {
			if p, ok := pk.(P); ok && pred(p) {
				return p
			}
		}
		select {
		case pk := <-c.notify:
			if p, ok := pk.(P); ok && pred(p) {
				return p
			}
		case <-deadline:
			var zero P
			t.Fatalf("timed out waiting for packet %T", zero)
			return zero
		}
	}
}

func spawnSessionPlayer(t *testing.T, w *world.World, conn *memConn) (*session.Session, *player.Player) {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	sess := session.Config{Log: log, MaxChunkRadius: 1}.New(conn)
	t.Cleanup(func() {
		sess.CloseConnection()
		_ = conn.Close()
	})

	pos := mgl64.Vec3{0.5, 64, 0.5}
	cfg := player.Config{
		Client:   sess,
		UUID:     uuid.MustParse(conn.identity.Identity),
		Name:     conn.identity.DisplayName,
		Position: pos,
		Skin:     skin.New(64, 64),
	}
	h := world.EntitySpawnOpts{Position: pos, ID: cfg.UUID}.New(player.Type, cfg)
	sess.Bind(h, cfg.Skin)

	var p *player.Player
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := w.Do(func(tx *world.Tx) {
		p = tx.AddEntity(h).(*player.Player)
		sess.Spawn(p, tx)
	}).Wait(ctx); err != nil {
		t.Fatalf("spawn session player: %v", err)
	}
	return sess, p
}

func TestSessionOfAndWithInterceptors(t *testing.T) {
	t.Parallel()

	if _, ok := bedrock.SessionOf(nil); ok {
		t.Fatal("SessionOf(nil) = true, want false")
	}

	world.DefaultBlockRegistry.Finalize()
	w := world.Config{
		Log:             slog.New(slog.NewTextHandler(io.Discard, nil)),
		Synchronous:     true,
		RandomTickSpeed: -1,
		Generator:       world.NopGenerator{},
		Provider:        world.NopProvider{},
		Blocks:          world.DefaultBlockRegistry,
		Entities:        entity.DefaultRegistry,
	}.New()
	t.Cleanup(func() { _ = w.Close() })

	<-w.Exec(func(tx *world.Tx) {
		h := world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 64, 0.5}}.New(player.Type, player.Config{
			Client: clienttest.New(),
			UUID:   uuid.New(),
			Name:   "NonBedrock",
			Skin:   skin.New(64, 64),
		})
		sim := tx.AddEntity(h).(*player.Player)
		if _, ok := bedrock.SessionOf(sim); ok {
			t.Fatal("SessionOf(clienttest.Recorder) = true, want false")
		}
		if _, ok := bedrock.InterceptPlayer(sim, bedrock.FuncInterceptor{}); ok {
			t.Fatal("InterceptPlayer(clienttest.Recorder) = true, want false")
		}
		if bedrock.SendPlayer(sim, &mc.TextPacket{Message: "ignored"}) {
			t.Fatal("SendPlayer(clienttest.Recorder) = true, want false")
		}
	})

	ic1 := bedrock.FuncInterceptor{}
	ic2 := bedrock.FuncInterceptor{}
	base := bedrockadapter.Config{Interceptors: []session.PacketInterceptor{ic1}}
	updated := bedrock.WithInterceptors(base, nil, ic2)
	if len(base.Interceptors) != 1 || len(updated.Interceptors) != 2 {
		t.Fatalf("WithInterceptors base=%d updated=%d", len(base.Interceptors), len(updated.Interceptors))
	}
}

func TestPacketInterceptionScopedMultiProtocolAndVirtualMenu(t *testing.T) {
	t.Parallel()

	courier := mob.Define("sdk_bedrock:courier",
		mob.NetworkType("minecraft:zombie"),
		mob.OnMeta(func(_ world.Entity, patch *world.ActorMetaPatch) {
			patch.SetNameTag("Courier")
		}),
	)

	world.DefaultBlockRegistry.Finalize()
	entReg := mob.Register(entity.DefaultRegistry, courier)
	w := world.Config{
		Log:             slog.New(slog.NewTextHandler(io.Discard, nil)),
		Generator:       world.NopGenerator{},
		Provider:        world.NopProvider{},
		RandomTickSpeed: -1,
		Blocks:          world.DefaultBlockRegistry,
		Entities:        entReg,
	}.New()
	t.Cleanup(func() { _ = w.Close() })

	conn := newMemConn()
	sess, p := spawnSessionPlayer(t, w, conn)

	var protoNum int32
	if err := w.Do(func(_ *world.Tx) {
		gotSess, ok := bedrock.SessionOf(p)
		if !ok || gotSess != sess {
			t.Fatalf("SessionOf = (%v, %v)", gotSess, ok)
		}
		var protoOK bool
		protoNum, protoOK = bedrock.Protocol(p)
		if !protoOK || protoNum == 0 || bedrock.SessionProtocol(sess) != protoNum {
			t.Fatalf("Protocol = (%d, %v)", protoNum, protoOK)
		}
		ver, verOK := bedrock.GameVersion(p)
		if !verOK || ver == "" || bedrock.SessionGameVersion(sess) != ver {
			t.Fatalf("GameVersion = (%q, %v)", ver, verOK)
		}
	}).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Verify InterceptScoped auto-removes when plugin disables.
	srv := server.Config{
		Log:             slog.New(slog.NewTextHandler(io.Discard, nil)),
		WorldProvider:   world.NopProvider{},
		PlayerProvider:  player.NopProvider{},
		Generator:       func(world.Dimension, int64) world.Generator { return world.NopGenerator{} },
		RandomTickSpeed: -1,
		SaveInterval:    -1,
	}.New()
	srv.Listen()
	t.Cleanup(func() { _ = srv.Close() })

	app := plugin.New(srv)
	var scopedHits atomic.Int32
	if err := w.Do(func(_ *world.Tx) {
		app.Use(plugin.Func("sniffer", func(ctx *plugin.Context) {
			_, ok := bedrock.InterceptScoped(ctx, p, bedrock.FuncInterceptor{
				OnOut: func(_ *session.Session, pk proto.Packet) (proto.Packet, bool) {
					if txt, isText := pk.(*mc.TextPacket); isText && txt.Message == "ping-scoped" {
						scopedHits.Add(1)
					}
					return pk, true
				},
			})
			if !ok {
				t.Fatal("InterceptScoped returned false")
			}
		}, nil))
		bedrock.SendPlayer(p, &mc.TextPacket{TextType: mc.TextTypeRaw, Message: "ping-scoped"})
	}).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitForPacket(t, conn, func(pk *mc.TextPacket) bool { return pk.Message == "ping-scoped" })
	if scopedHits.Load() != 1 {
		t.Fatalf("scopedHits = %d, want 1", scopedHits.Load())
	}
	app.Disable()
	bedrock.Send(sess, &mc.TextPacket{TextType: mc.TextTypeRaw, Message: "ping-scoped"})
	bedrock.Send(sess, &mc.TextPacket{TextType: mc.TextTypeRaw, Message: "ping-after"})
	waitForPacket(t, conn, func(pk *mc.TextPacket) bool { return pk.Message == "ping-after" })
	if scopedHits.Load() != 1 {
		t.Fatalf("scopedHits after disable = %d, want 1", scopedHits.Load())
	}

	// Verify incoming chat rewrite + drop via InterceptPlayer.
	chatMessages := make(chan string, 4)
	var remove func()
	if err := w.Do(func(tx *world.Tx) {
		event.OnPlayer[player.ChatEvent](nil, p, event.Normal, func(ev *player.ChatEvent) {
			chatMessages <- ev.Message
			ev.Cancel()
		})
		var ok bool
		remove, ok = bedrock.InterceptPlayer(p, bedrock.FuncInterceptor{
			OnIn: func(_ *session.Session, pk proto.Packet) (proto.Packet, bool) {
				txt, isText := pk.(*mc.TextPacket)
				if !isText {
					return pk, true
				}
				if txt.Message == "drop-in" {
					return nil, false
				}
				if txt.Message == "raw-in" {
					cp := *txt
					cp.Message = "rewritten-in"
					return &cp, true
				}
				return pk, true
			},
		})
		if !ok {
			t.Fatal("InterceptPlayer failed")
		}
	}).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	conn.packets <- &mc.TextPacket{TextType: mc.TextTypeChat, SourceName: conn.identity.DisplayName, Message: "drop-in", XUID: conn.identity.XUID}
	conn.packets <- &mc.TextPacket{TextType: mc.TextTypeChat, SourceName: conn.identity.DisplayName, Message: "raw-in", XUID: conn.identity.XUID}
	select {
	case msg := <-chatMessages:
		if msg != "rewritten-in" {
			t.Fatalf("chatMessages = %q, want rewritten-in", msg)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for rewritten chat packet")
	}
	remove()

	// Verify ForProtocols / ForProtocolRange / SendPlayerVersioned.
	var wrongHits, rightHits, rangeHits atomic.Int32
	remWrong := bedrock.InterceptSession(sess, bedrock.ForProtocols(bedrock.FuncInterceptor{
		OnOut: func(_ *session.Session, pk proto.Packet) (proto.Packet, bool) { wrongHits.Add(1); return pk, true },
	}, 99999))
	remRight := bedrock.InterceptSession(sess, bedrock.ForProtocols(bedrock.FuncInterceptor{
		OnOut: func(_ *session.Session, pk proto.Packet) (proto.Packet, bool) { rightHits.Add(1); return pk, true },
	}, protoNum))
	remRange := bedrock.InterceptSession(sess, bedrock.ForProtocolRange(bedrock.FuncInterceptor{
		OnOut: func(_ *session.Session, pk proto.Packet) (proto.Packet, bool) { rangeHits.Add(1); return pk, true },
	}, 2168, 2225))
	defer remWrong()
	defer remRight()
	defer remRange()

	var seenProto int32
	if err := w.Do(func(_ *world.Tx) {
		if !bedrock.SendPlayerVersioned(p, func(pn int32) proto.Packet {
			seenProto = pn
			return &mc.TextPacket{TextType: mc.TextTypeRaw, Message: "versioned-hello"}
		}) || seenProto != protoNum {
			t.Fatalf("SendPlayerVersioned proto=%d want=%d", seenProto, protoNum)
		}
	}).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitForPacket(t, conn, func(pk *mc.TextPacket) bool { return pk.Message == "versioned-hello" })
	if wrongHits.Load() != 0 || rightHits.Load() == 0 || rangeHits.Load() == 0 {
		t.Fatalf("protocol filter hits: wrong=%d right=%d range=%d", wrongHits.Load(), rightHits.Load(), rangeHits.Load())
	}

	// Verify custom mob ActorMetaPatch wire packet.
	if err := w.Do(func(tx *world.Tx) {
		e := courier.SpawnIn(tx, world.EntitySpawnOpts{Position: mgl64.Vec3{2.5, 64, 0.5}})
		sess.ViewEntity(e)
	}).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	addActor := waitForPacket(t, conn, func(pk *mc.AddActorPacket) bool { return pk.EntityType == "minecraft:zombie" })
	if got := addActor.EntityMetadata[mc.EntityDataKeyName]; got != "Courier" {
		t.Fatalf("AddActorPacket NameTag = %#v, want Courier", got)
	}

	// Verify virtual DoubleChest sends paired BlockActorData + ContainerOpen.
	vault := menu.DoubleChest("Vault",
		menu.ReadOnly(),
		menu.Slot(0, item.NewStack(item.Diamond{}, 1), menu.Locked()),
	)
	if err := w.Do(func(tx *world.Tx) {
		vault.Open(p, tx)
	}).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitForPacket(t, conn, func(pk *mc.BlockActorDataPacket) bool {
		return pk.NBTData["id"] == "Chest" && pk.NBTData["CustomName"] == "Vault" && pk.NBTData["pairlead"] == byte(1)
	})
	waitForPacket(t, conn, func(pk *mc.ContainerOpenPacket) bool {
		return pk.ContainerType == mc.ContainerTypeContainer
	})
	if err := w.Do(func(tx *world.Tx) {
		p.CloseMenu(false)
	}).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Verify resource pack helpers.
	emptyDir := t.TempDir()
	packs, err := bedrock.LoadPacksDir(emptyDir)
	if err != nil || len(packs) != 0 {
		t.Fatalf("LoadPacksDir(empty) = (%d, %v)", len(packs), err)
	}
	if _, err := bedrock.LoadPack(filepath.Join(emptyDir, "missing.zip")); err == nil && !os.IsNotExist(err) {
		t.Fatal("expected error loading missing pack")
	}
	cfg := bedrock.WithPacks(bedrockadapter.Config{}, true, nil)
	if !cfg.ResourcesRequired || len(cfg.Resources) != 0 {
		t.Fatalf("WithPacks required=%v len=%d", cfg.ResourcesRequired, len(cfg.Resources))
	}

	// Verify Bedrock Camera, Fog, Transfer, AnimateEntity wire packets.
	pos := mgl64.Vec3{10, 80, 10}
	bedrock.CameraFadeSession(sess, 0.5, 1.0, 0.5, color.RGBA{R: 0, G: 0, B: 0, A: 255})
	waitForPacket(t, conn, func(pk *mc.CameraInstructionPacket) bool { return pk.Fade.Set })

	bedrock.CameraSetSession(sess, bedrock.CameraSetOpts{
		Preset: 1,
		Ease:   &bedrock.CameraEase{Type: bedrock.EaseInOutCubic, Duration: 1.5},
		Pos:    &pos,
	})
	waitForPacket(t, conn, func(pk *mc.CameraInstructionPacket) bool { return pk.Set.Set && pk.Set.Val.Preset == 1 })

	bedrock.CameraClearSession(sess)
	waitForPacket(t, conn, func(pk *mc.CameraInstructionPacket) bool { return pk.Clear.Set && pk.Clear.Val })

	bedrock.CameraShakeSession(sess, 0.5, 2.0, bedrock.ShakeRotational)
	waitForPacket(t, conn, func(pk *mc.CameraShakePacket) bool { return pk.Intensity == 0.5 && pk.Action == 0 })

	bedrock.SetSessionFog(sess, "minecraft:fog_crimson_forest")
	waitForPacket(t, conn, func(pk *mc.PlayerFogPacket) bool {
		return len(pk.Stack) == 1 && pk.Stack[0] == "minecraft:fog_crimson_forest"
	})

	bedrock.TransferSession(sess, "play.example.com", 19132, false)
	waitForPacket(t, conn, func(pk *mc.TransferPacket) bool {
		return pk.Address == "play.example.com" && pk.Port == 19132
	})

	bedrock.AnimateEntity(sess, bedrock.AnimateOpts{Animation: "animation.humanoid.wave"}, 1)
	waitForPacket(t, conn, func(pk *mc.AnimateEntityPacket) bool {
		return pk.Animation == "animation.humanoid.wave" && len(pk.EntityRuntimeIDs) == 1
	})

	// Verify runtime Resource Pack builder compiles an in-memory *resource.Pack.
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	pack, err := bedrock.NewPackBuilder("AdamantPack", "Generated at runtime").
		Version(1, 2, 3).
		AddItemTexture("ruby_sword", img).
		AddBlockTexture("glow_tile", img).
		AddSound("custom.ding", "custom/ding.ogg", []byte("OggS")).
		Build()
	if err != nil {
		t.Fatalf("PackBuilder.Build: %v", err)
	}
	if pack.Name() != "AdamantPack" || !pack.HasTextures() {
		t.Fatalf("pack name=%q hasTextures=%v", pack.Name(), pack.HasTextures())
	}
	if _, err := pack.ReadFile("textures/items/ruby_sword.png"); err != nil {
		t.Fatalf("missing item texture in built pack: %v", err)
	}
	if _, err := pack.ReadFile("sounds/sound_definitions.json"); err != nil {
		t.Fatalf("missing sound_definitions.json in built pack: %v", err)
	}
}
