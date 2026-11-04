package v2225

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	bedrockadapter "github.com/df-mc/dragonfly/protocol/bedrock"
	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/biome"
	"github.com/df-mc/dragonfly/server/world/generator"
	ourclient "github.com/dm-vev/adamant/net/bedrock/client"
	ourlogin "github.com/dm-vev/adamant/net/bedrock/login"
	"github.com/dm-vev/adamant/net/bedrock/proto/codec"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
	"github.com/dm-vev/adamant/net/nethernet"
	"github.com/go-gl/mathgl/mgl32"
	"github.com/google/uuid"
	"github.com/sandertv/gophertunnel/minecraft"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/login"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// gtNetwork dials NetherNet for a gophertunnel client.
type gtNetwork struct{ d nethernet.Dialer }

func (n gtNetwork) DialContext(ctx context.Context, address string) (net.Conn, error) {
	return n.d.DialContext(ctx, address)
}

func (gtNetwork) PingContext(context.Context, string) ([]byte, error) {
	return nil, errors.New("no ping")
}

func (gtNetwork) Listen(string) (minecraft.NetworkListener, error) {
	return nil, errors.New("no listen")
}

// boundProtocol signs the login of a gophertunnel client with the key of
// its transport, which the server matches.
type boundProtocol struct {
	minecraft.Protocol
	key *ecdsa.PrivateKey
}

func (p boundProtocol) ConvertFromLatest(pk packet.Packet, conn *minecraft.Conn) []packet.Packet {
	if l, ok := pk.(*packet.Login); ok {
		l.ConnectionRequest = login.EncodeOffline(conn.IdentityData(), conn.ClientData(), p.key, false)
	}
	return []packet.Packet{pk}
}

// gtDial joins a server as a gophertunnel client of master, of protocol
// 2225, and spawns.
func gtDial(ctx context.Context, t *testing.T, addr, name string) *minecraft.Conn {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	identity := login.IdentityData{DisplayName: name, Identity: uuid.NewString()}
	request := login.EncodeOffline(identity, login.ClientData{}, key, false)
	var cert struct{ Token string }
	n := binary.LittleEndian.Uint32(request[:4])
	if err := json.Unmarshal(request[4:4+n], &cert); err != nil {
		t.Fatal(err)
	}
	d := minecraft.Dialer{IdentityData: identity, Protocol: boundProtocol{Protocol: minecraft.DefaultProtocol, key: key}}
	conn, err := d.DialContextNetwork(ctx, gtNetwork{nethernet.Dialer{Key: key, Token: cert.Token}}, addr)
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.DoSpawnContext(ctx); err != nil {
		t.Fatal(err)
	}
	return conn
}

// freeAddr returns a local address no socket holds now.
func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().String()
}

// startServer starts a server of a flat world on addr whose config allows
// 1.26.50 and 1.26.60, its primary 1.26.50, as config.toml would, and
// returns the players it accepts.
func startServer(t *testing.T, addr string) (*server.Server, <-chan *player.Player) {
	t.Helper()
	uc := bedrockadapter.DefaultConfig()
	uc.Network.Address = addr
	uc.Network.IdentityFile = filepath.Join(t.TempDir(), "identity.pem")
	uc.Network.Protocols = []string{"1.26.50", "1.26.60"}
	uc.Network.PrimaryProtocol = "1.26.50"
	uc.Server.AuthEnabled = false
	bc, err := uc.Config(slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	srv := server.Config{
		Log:            slog.New(slog.DiscardHandler),
		Listeners:      []func(server.Config, server.Host) (server.Listener, error){bc.Listener},
		Name:           "Interop",
		DisableNether:  true,
		DisableEnd:     true,
		MaxChunkRadius: 2,
		WorldProvider:  world.NopProvider{Set: &world.Settings{Name: "Interop", Spawn: cube.Pos{0, -60, 0}, DefaultGameMode: world.GameModeSurvival, TickRange: 6}},
		Generator: func(world.Dimension, int64) world.Generator {
			return generator.NewFlat(biome.Plains{}, []world.Block{block.Grass{}, block.Dirt{}, block.Dirt{}, block.Bedrock{}})
		},
	}.New()
	// Players spawn on the spawn itself, not at random around it, so that
	// the two clients stand within reach of each other.
	if err := srv.World().SetGameRule("respawn_radius", int64(0)); err != nil {
		t.Fatal(err)
	}
	srv.Listen()
	t.Cleanup(func() { _ = srv.Close() })
	players := make(chan *player.Player, 4)
	go func() {
		for p := range srv.Accept() {
			players <- p
		}
	}()
	return srv, players
}

// TestClientsOfTwoProtocolsPlayTogether joins one server, which allows 1.26.50
// and 1.26.60, its primary 1.26.50, with a client of 2193 (the client of the
// net module) and a client of 2225 (gophertunnel master): each sees the
// other, hears the other's chat, and the client of 2225 opens a chest and
// strikes the player of the client of 2193, who is shown it hurt.
func TestClientsOfTwoProtocolsPlayTogether(t *testing.T) {
	addr := freeAddr(t)
	_, players := startServer(t, addr)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	old, err := ourclient.Dial(ctx, "http://"+addr, ourclient.Config{Identity: ourlogin.Identity{DisplayName: "Older"}, Protocol: codec.V2193, ChunkRadius: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	stop := context.AfterFunc(ctx, func() { _ = old.Close() })
	defer stop()
	oldPlayer := <-players
	newer := gtDial(ctx, t, addr, "Newer")
	defer newer.Close()
	newPlayer := <-players
	if g := newer.GameData(); g.BaseGameVersion != "1.26.60" {
		t.Errorf("the client of 2225 is told the version %s", g.BaseGameVersion)
	}

	// A chest by the player of 2225, set by the game.
	var chest cube.Pos
	h := newPlayer.H()
	<-h.World().Exec(func(tx *world.Tx) {
		e, _ := h.Entity(tx)
		chest = cube.PosFromVec3(e.Position()).Add(cube.Pos{1, 0, 0})
		tx.SetBlock(chest, block.NewChest(), nil)
	})

	// The client of 2225 waits to see the player of 2193 and then talks,
	// opens the chest and strikes.
	newerSaw, newerHeard, opened := make(chan uint64, 1), make(chan struct{}), make(chan struct{})
	go func() {
		var heard, open bool
		for {
			pk, err := newer.ReadPacket()
			if err != nil {
				return
			}
			switch pk := pk.(type) {
			case *packet.AddPlayer:
				if pk.Username == "Older" {
					newerSaw <- pk.EntityRuntimeID
				}
			case *packet.Text:
				if !heard && strings.Contains(pk.Message+strings.Join(pk.Parameters, " "), "from 1.26.50") {
					heard = true
					close(newerHeard)
				}
			case *packet.ContainerOpen:
				if !open && pk.ContainerPosition == (protocol.BlockPos{int32(chest[0]), int32(chest[1]), int32(chest[2])}) {
					open = true
					close(opened)
				}
			}
		}
	}()

	var oldSawNewer, oldHeard, hurt bool
	var target uint64
	deadline := time.Now().Add(40 * time.Second)
	for (!oldSawNewer || !oldHeard || !hurt) && time.Now().Before(deadline) {
		select {
		case target = <-newerSaw:
			_ = old.WritePacket(&mc.TextPacket{TextType: mc.TextTypeChat, SourceName: "Older", Message: "hello from 1.26.50"})
			_ = old.Flush()
			go play(newer, target, chest)
		default:
		}
		pk, err := old.ReadPacket()
		if errors.As(err, new(*ourclient.DecodeError)) {
			t.Fatalf("a packet to the client of 2193 does not decode: %v", err)
		}
		if err != nil {
			t.Fatal(err)
		}
		switch pk := pk.(type) {
		case *mc.AddPlayerPacket:
			oldSawNewer = oldSawNewer || pk.Username == "Newer"
		case *mc.TextPacket:
			oldHeard = oldHeard || strings.Contains(pk.Message+strings.Join(pk.Parameters, " "), "from 1.26.60")
		case *mc.ActorEventPacket:
			hurt = hurt || pk.EventType == mc.ActorEventHurt && pk.EntityRuntimeID == old.StartGame().EntityRuntimeID
		case *mc.UpdateAttributesPacket:
			for _, a := range pk.Attributes {
				hurt = hurt || a.Name == "minecraft:health" && a.Value < a.Max && pk.EntityRuntimeID == old.StartGame().EntityRuntimeID
			}
		}
	}
	if !oldSawNewer || !oldHeard || !hurt {
		t.Fatalf("the client of 2193 saw the other %v, heard it %v, was struck %v", oldSawNewer, oldHeard, hurt)
	}
	for name, ch := range map[string]chan struct{}{"heard the client of 2193": newerHeard, "opened the chest": opened} {
		select {
		case <-ch:
		case <-time.After(10 * time.Second):
			t.Errorf("the client of 2225 never %s", name)
		}
	}
	oh := oldPlayer.H()
	<-oh.World().Exec(func(tx *world.Tx) {
		if e, ok := oh.Entity(tx); ok {
			if p := e.(*player.Player); p.Health() >= p.MaxHealth() {
				t.Error("the player of 2193 was not hurt")
			}
		}
	})
}

// play has the client of 2225 step two blocks south of the spawn they
// share, facing back north to the player of 2193 a runtime ID names, talk,
// open the chest and strike that player a few times.
func play(c *minecraft.Conn, target uint64, chest cube.Pos) {
	pos := c.GameData().PlayerPosition
	for tick := uint64(1); tick <= 20; tick++ {
		pos[2] += 0.1
		_ = c.WritePacket(&packet.PlayerAuthInput{
			Position: pos, Delta: mgl32.Vec3{0, 0, 0.1}, Yaw: 180, HeadYaw: 180,
			InputMode: packet.InputModeMouse, PlayMode: packet.PlayModeNormal, Tick: tick,
		})
		_ = c.Flush()
		time.Sleep(50 * time.Millisecond)
	}
	_ = c.WritePacket(&packet.Text{TextType: packet.TextTypeChat, SourceName: "Newer", Message: "hello from 1.26.60"})
	_ = c.WritePacket(&packet.InventoryTransaction{TransactionData: &protocol.UseItemTransactionData{
		ActionType: protocol.UseItemActionClickBlock, TriggerType: protocol.TriggerTypePlayerInput,
		BlockPosition: protocol.BlockPos{int32(chest[0]), int32(chest[1]), int32(chest[2])}, BlockFace: int32(cube.FaceWest),
		Position: pos, ClickedPosition: mgl32.Vec3{0, 0.5, 0.5}, ClientPrediction: protocol.ClientPredictionSuccess,
	}})
	_ = c.Flush()
	for range 3 {
		time.Sleep(600 * time.Millisecond)
		_ = c.WritePacket(&packet.InventoryTransaction{TransactionData: &protocol.UseItemOnEntityTransactionData{
			TargetEntityRuntimeID: target, ActionType: protocol.UseItemOnEntityActionAttack, Position: pos,
		}})
		_ = c.Flush()
	}
}
