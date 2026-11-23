package bedrock_test

import (
	"context"
	"io"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/api"
	"github.com/df-mc/dragonfly/api/bedrock"
	bedrockadapter "github.com/df-mc/dragonfly/protocol/bedrock"
	"github.com/df-mc/dragonfly/protocol/bedrock/netconn"
	"github.com/df-mc/dragonfly/protocol/bedrock/session"
	"github.com/df-mc/dragonfly/server/client/clienttest"
	"github.com/df-mc/dragonfly/server/entity"
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
		if bedrock.SendPlayerRawPacket(sim, &mc.TextPacket{Message: "ignored"}) {
			t.Fatal("SendPlayerRawPacket(clienttest.Recorder) = true, want false")
		}
	})

	ic1 := bedrock.FuncInterceptor{}
	ic2 := bedrock.FuncInterceptor{}
	base := bedrockadapter.Config{Interceptors: []session.PacketInterceptor{ic1}}
	updated := bedrock.WithInterceptors(base, nil, ic2)
	if len(base.Interceptors) != 1 || len(updated.Interceptors) != 2 {
		t.Fatalf("WithInterceptors mutated base or failed append: base=%d updated=%d",
			len(base.Interceptors), len(updated.Interceptors))
	}
}

func rewriteDropTextInterceptor() bedrock.PacketInterceptor {
	rewrite := func(dropVal, rawVal, outVal string) func(*session.Session, proto.Packet) (proto.Packet, bool) {
		return func(_ *session.Session, pk proto.Packet) (proto.Packet, bool) {
			txt, isText := pk.(*mc.TextPacket)
			if !isText {
				return pk, true
			}
			if txt.Message == dropVal {
				return nil, false
			}
			if txt.Message == rawVal {
				cp := *txt
				cp.Message = outVal
				return &cp, true
			}
			return pk, true
		}
	}
	return bedrock.FuncInterceptor{
		OnIn:  rewrite("drop-in", "raw-in", "rewritten-in"),
		OnOut: rewrite("drop-out", "raw-out", "rewritten-out"),
	}
}

func assertCustomActorMetaPacket(t *testing.T, w *world.World, sess *session.Session, conn *memConn) {
	t.Helper()
	if err := w.Do(func(tx *world.Tx) {
		e := api.SpawnCustomEntity(tx, api.CustomEntitySpec{
			Name:        "sdk_bedrock:courier",
			NetworkType: "minecraft:zombie",
			OnMeta: func(_ world.Entity, patch *world.ActorMetaPatch) {
				patch.SetNameTag("Courier")
			},
		}, world.EntitySpawnOpts{Position: mgl64.Vec3{2.5, 64, 0.5}})
		sess.ViewEntity(e)
	}).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}

	addActor := waitForPacket(t, conn, func(pk *mc.AddActorPacket) bool {
		return pk.EntityType == "minecraft:zombie"
	})
	if got := addActor.EntityMetadata[mc.EntityDataKeyName]; got != "Courier" {
		t.Fatalf("AddActorPacket NameTag metadata = %#v, want Courier", got)
	}
}

func TestPacketInterceptionAndRawInjection(t *testing.T) {
	t.Parallel()

	customType := api.NewCustomEntityType(api.CustomEntitySpec{
		Name:        "sdk_bedrock:courier",
		NetworkType: "minecraft:zombie",
		OnMeta: func(_ world.Entity, patch *world.ActorMetaPatch) {
			patch.SetNameTag("Courier")
		},
	})
	world.DefaultBlockRegistry.Finalize()
	entReg := api.RegisterEntityTypes(entity.DefaultRegistry, customType)
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

	gotSess, ok := bedrock.SessionOf(p)
	if !ok || gotSess != sess {
		t.Fatalf("SessionOf(bedrock player) = (%v, %v), want (%v, true)", gotSess, ok, sess)
	}

	chatMessages := make(chan string, 4)
	if err := w.Do(func(tx *world.Tx) {
		api.OnPlayer[player.ChatEvent](p, api.PriorityNormal, func(ev *player.ChatEvent) {
			chatMessages <- ev.Message
			ev.Cancel()
		})
	}).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}

	remove, ok := bedrock.InterceptPlayer(p, rewriteDropTextInterceptor())
	if !ok {
		t.Fatal("InterceptPlayer failed")
	}

	// Verify incoming drop + rewrite.
	conn.packets <- &mc.TextPacket{TextType: mc.TextTypeChat, SourceName: conn.identity.DisplayName, Message: "drop-in", XUID: conn.identity.XUID}
	conn.packets <- &mc.TextPacket{TextType: mc.TextTypeChat, SourceName: conn.identity.DisplayName, Message: "raw-in", XUID: conn.identity.XUID}
	select {
	case msg := <-chatMessages:
		if msg != "rewritten-in" {
			t.Fatalf("chatMessages received %q, want rewritten-in", msg)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for rewritten incoming TextPacket")
	}

	// Verify SendRawPacket + SendPlayerRawPacket pass through OnOut.
	bedrock.SendRawPacket(sess, &mc.TextPacket{TextType: mc.TextTypeRaw, Message: "drop-out"})
	if !bedrock.SendPlayerRawPacket(p, &mc.TextPacket{TextType: mc.TextTypeRaw, Message: "raw-out"}) {
		t.Fatal("SendPlayerRawPacket returned false")
	}
	waitForPacket(t, conn, func(pk *mc.TextPacket) bool {
		return pk.Message == "rewritten-out"
	})
	for _, pk := range conn.written() {
		if txt, isText := pk.(*mc.TextPacket); isText && txt.Message == "drop-out" {
			t.Fatal("OnOut failed to drop drop-out packet")
		}
	}

	// Remove interceptor (and test idempotent second call) -> raw packet passes untouched.
	remove()
	remove()
	bedrock.SendRawPacket(sess, &mc.TextPacket{TextType: mc.TextTypeRaw, Message: "raw-out"})
	waitForPacket(t, conn, func(pk *mc.TextPacket) bool {
		return pk.Message == "raw-out"
	})

	assertCustomActorMetaPacket(t, w, sess, conn)
}
