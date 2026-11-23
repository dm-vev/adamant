package bedrockadapter

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/protocol/bedrock/internal/versions"
	"github.com/df-mc/dragonfly/protocol/bedrock/session"
	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/biome"
	"github.com/df-mc/dragonfly/server/world/generator"
	"github.com/dm-vev/adamant/net/bedrock/client"
	"github.com/dm-vev/adamant/net/bedrock/login"
	"github.com/dm-vev/adamant/net/bedrock/proto/codec"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
	"github.com/go-gl/mathgl/mgl32"
)

// playServer starts a server of a flat world whose NetherNet listener
// allows 2193 and 2225, its primary 2193, and returns its address and the
// players it accepts.
func playServer(t *testing.T) (string, <-chan *player.Player) {
	t.Helper()
	c := testNetherNetConfig()
	c.Network.Address = "127.0.0.1:0"
	c.Network.IdentityFile = filepath.Join(t.TempDir(), "identity.pem")
	c.Protocols = []*versions.Protocol{oldProtocol(), versions.Model()}
	c.Primary = oldProtocol()
	addr := make(chan string, 1)
	srv := server.Config{
		Log: slog.New(slog.DiscardHandler),
		Listeners: []func(server.Config, server.Host) (server.Listener, error){func(conf server.Config, host server.Host) (server.Listener, error) {
			l, err := c.netherNetListener(conf, host)
			if err != nil {
				return nil, err
			}
			addr <- l.(netherNetListener).Addr().String()
			return session.Listen(l, session.ListenConfig{Log: conf.Log, Blocks: conf.Blocks, Primary: c.Primary}), nil
		}},
		Name:           "Protocols",
		DisableNether:  true,
		DisableEnd:     true,
		MaxChunkRadius: 2,
		WorldProvider:  world.NopProvider{Set: &world.Settings{Name: "Protocols", Spawn: cube.Pos{0, -60, 0}, DefaultGameMode: world.GameModeSurvival, TickRange: 6}},
		Generator: func(world.Dimension, int64) world.Generator {
			return generator.NewFlat(biome.Plains{}, []world.Block{block.Grass{}, block.Dirt{}, block.Dirt{}, block.Bedrock{}})
		},
	}.New()
	srv.Listen()
	t.Cleanup(func() { _ = srv.Close() })
	players := make(chan *player.Player, 4)
	go func() {
		for p := range srv.Accept() {
			players <- p
		}
	}()
	select {
	case a := <-addr:
		return a, players
	case <-time.After(20 * time.Second):
		t.Fatal("the server did not listen")
		return "", nil
	}
}

// TestClientOf2225Plays joins a server that allows 2193 and 2225, its
// primary 2193, as a client of 2225: it spawns, is sent the chunks around
// it, walks, and reads its own chat as the server shows it. The server
// tells it the version of 2225 and has the player where it walked to.
func TestClientOf2225Plays(t *testing.T) {
	addr, players := playServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, err := client.Dial(ctx, addr, client.Config{Identity: login.Identity{DisplayName: "Newer"}, Protocol: codec.V2225, ChunkRadius: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	var p *player.Player
	select {
	case p = <-players:
	case <-ctx.Done():
		t.Fatal("the server did not accept the player")
	}
	if game := conn.StartGame(); game.GameVersion != "1.26.60" || game.BaseGameVersion != "1.26.60" {
		t.Errorf("StartGame of %s on %s, want 1.26.60", game.GameVersion, game.BaseGameVersion)
	}

	start := conn.StartGame().PlayerPosition
	go func() {
		pos := start
		for tick := uint64(1); tick <= 20 && ctx.Err() == nil; tick++ {
			time.Sleep(50 * time.Millisecond)
			pos[0] += 0.1
			_ = conn.WritePacket(&mc.PlayerAuthInputPacket{Position: pos, Delta: mgl32.Vec3{0.1, 0, 0}, InputMode: 1, PlayMode: 0, Tick: tick})
			_ = conn.Flush()
		}
		_ = conn.WritePacket(&mc.TextPacket{TextType: mc.TextTypeChat, SourceName: "Newer", Message: "hello from 1.26.60", XUID: ""})
		_ = conn.Flush()
	}()
	chunks, heard := 0, false
	for !heard || chunks == 0 {
		pk, err := conn.ReadPacket()
		var decodeErr *client.DecodeError
		if errors.As(err, &decodeErr) {
			t.Fatalf("a packet of the server does not decode as 2225: %v", err)
		}
		if err != nil {
			t.Fatalf("after %d chunks: %v", chunks, err)
		}
		switch pk := pk.(type) {
		case *mc.LevelChunkPacket:
			chunks++
		case *mc.TextPacket:
			heard = heard || strings.Contains(pk.Message, "hello from 1.26.60") || slicesContains(pk.Parameters, "hello from 1.26.60")
		}
	}
	var pos mgl32.Vec3
	h := p.H()
	<-h.World().Exec(func(tx *world.Tx) {
		if e, ok := h.Entity(tx); ok {
			v := e.Position()
			pos = mgl32.Vec3{float32(v[0]), float32(v[1]), float32(v[2])}
		}
	})
	if pos[0] <= start[0]+0.5 {
		t.Errorf("the player is at %v, it walked from %v", pos, start)
	}
}

func slicesContains(l []string, s string) bool {
	for _, x := range l {
		if strings.Contains(x, s) {
			return true
		}
	}
	return false
}

// TestAllFourProtocolsPlayTogether starts a server with the default
// multiprotocol setup (2168, 2169, 2193, 2225; primary 2225) and connects
// four real NetherNet clients speaking each protocol simultaneously: every
// client spawns, receives chunks without decode errors, walks, and chats.
func TestAllFourProtocolsPlayTogether(t *testing.T) {
	c := testNetherNetConfig()
	c.Network.Address = "127.0.0.1:0"
	c.Network.IdentityFile = filepath.Join(t.TempDir(), "identity.pem")
	c.Protocols = versions.Configurable()
	c.Primary = versions.NewestPrimary()
	addrCh := make(chan string, 1)
	srv := server.Config{
		Log: slog.New(slog.DiscardHandler),
		Listeners: []func(server.Config, server.Host) (server.Listener, error){func(conf server.Config, host server.Host) (server.Listener, error) {
			l, err := c.netherNetListener(conf, host)
			if err != nil {
				return nil, err
			}
			addrCh <- l.(netherNetListener).Addr().String()
			return session.Listen(l, session.ListenConfig{Log: conf.Log, Blocks: conf.Blocks, Primary: c.Primary, Allowed: c.Protocols}), nil
		}},
		Name:           "AllProtocols",
		DisableNether:  true,
		DisableEnd:     true,
		MaxChunkRadius: 2,
		WorldProvider:  world.NopProvider{Set: &world.Settings{Name: "AllProtocols", Spawn: cube.Pos{0, -60, 0}, DefaultGameMode: world.GameModeSurvival, TickRange: 6}},
		Generator: func(world.Dimension, int64) world.Generator {
			return generator.NewFlat(biome.Plains{}, []world.Block{block.Grass{}, block.Dirt{}, block.Dirt{}, block.Bedrock{}})
		},
	}.New()
	srv.Listen()
	t.Cleanup(func() { _ = srv.Close() })
	players := make(chan *player.Player, 8)
	go func() {
		for p := range srv.Accept() {
			players <- p
		}
	}()
	addr := <-addrCh

	clients := []struct {
		name  string
		proto *codec.Codec
		ver   string
	}{
		{"C2168", codec.V2168, "1.26.40"},
		{"C2169", codec.V2169, "1.26.45"},
		{"C2193", codec.V2193, "1.26.50"},
		{"C2225", codec.V2225, "1.26.60"},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	for _, tc := range clients {
		conn, err := client.Dial(ctx, addr, client.Config{
			Identity:    login.Identity{DisplayName: tc.name},
			Protocol:    tc.proto,
			ChunkRadius: 2,
		})
		if err != nil {
			t.Fatalf("%s (%d) dial: %v", tc.name, tc.proto.Number, err)
		}
		defer conn.Close()
		var p *player.Player
		select {
		case p = <-players:
		case <-ctx.Done():
			t.Fatalf("%s (%d): server did not accept player", tc.name, tc.proto.Number)
		}
		if game := conn.StartGame(); game.GameVersion != tc.ver || game.BaseGameVersion != tc.ver {
			t.Errorf("%s: StartGame %s on %s, want %s", tc.name, game.GameVersion, game.BaseGameVersion, tc.ver)
		}
		start := conn.StartGame().PlayerPosition
		msg := "hello from " + tc.ver
		go func(c *client.Conn, start mgl32.Vec3, name, msg string) {
			pos := start
			for tick := uint64(1); tick <= 15 && ctx.Err() == nil; tick++ {
				time.Sleep(50 * time.Millisecond)
				pos[0] += 0.1
				_ = c.WritePacket(&mc.PlayerAuthInputPacket{Position: pos, Delta: mgl32.Vec3{0.1, 0, 0}, InputMode: 1, PlayMode: 0, Tick: tick})
				_ = c.Flush()
			}
			_ = c.WritePacket(&mc.TextPacket{TextType: mc.TextTypeChat, SourceName: name, Message: msg})
			_ = c.Flush()
		}(conn, start, tc.name, msg)

		chunks, heard := 0, false
		for !heard || chunks == 0 {
			pk, err := conn.ReadPacket()
			var decodeErr *client.DecodeError
			if errors.As(err, &decodeErr) {
				t.Fatalf("%s (%d): server packet failed to decode: %v", tc.name, tc.proto.Number, err)
			}
			if err != nil {
				t.Fatalf("%s (%d) after %d chunks: %v", tc.name, tc.proto.Number, chunks, err)
			}
			switch pk := pk.(type) {
			case *mc.LevelChunkPacket:
				chunks++
			case *mc.TextPacket:
				heard = heard || strings.Contains(pk.Message, msg) || slicesContains(pk.Parameters, msg)
			}
		}
		var pos mgl32.Vec3
		h := p.H()
		<-h.World().Exec(func(tx *world.Tx) {
			if e, ok := h.Entity(tx); ok {
				v := e.Position()
				pos = mgl32.Vec3{float32(v[0]), float32(v[1]), float32(v[2])}
			}
		})
		if pos[0] <= start[0]+0.3 {
			t.Errorf("%s (%d): player at %v, walked from %v", tc.name, tc.proto.Number, pos, start)
		}
	}
}
