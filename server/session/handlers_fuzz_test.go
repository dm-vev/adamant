package session_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"runtime/debug"
	"sync"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/dm-vev/adamant/net/bedrock/wire"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
	"github.com/sandertv/gophertunnel/minecraft"
	"github.com/sandertv/gophertunnel/minecraft/protocol/login"
)

// fuzzConn is the connection of a client whose packets a fuzz test hands
// to the session itself. It encodes what the session writes, as a real
// connection does, and keeps the first packet that did not encode.
type fuzzConn struct {
	identity login.IdentityData
	closed   chan struct{}
	once     sync.Once

	mu       sync.Mutex
	unencoded string
}

func (c *fuzzConn) Close() error                     { c.once.Do(func() { close(c.closed) }); return nil }
func (c *fuzzConn) IdentityData() login.IdentityData { return c.identity }
func (*fuzzConn) ClientData() login.ClientData       { return login.ClientData{} }
func (*fuzzConn) ClientCacheEnabled() bool           { return true }
func (*fuzzConn) ChunkRadius() int                   { return 1 }
func (*fuzzConn) Latency() time.Duration             { return 0 }
func (*fuzzConn) Flush() error                       { return nil }
func (*fuzzConn) RemoteAddr() net.Addr               { return &net.UDPAddr{} }
func (*fuzzConn) StartGameContext(context.Context, minecraft.GameData) error {
	return nil
}

func (c *fuzzConn) ReadPacket() (proto.Packet, error) {
	<-c.closed
	return nil, io.EOF
}

func (c *fuzzConn) WritePacket(pk proto.Packet) error {
	defer func() {
		if r := recover(); r != nil {
			c.mu.Lock()
			if c.unencoded == "" {
				c.unencoded = fmt.Sprintf("%T: %v\n%s", pk, r, debug.Stack())
			}
			c.mu.Unlock()
		}
	}()
	var w wire.Writer
	pk.Encode(&w)
	return nil
}

// FuzzHandlePacket decodes hostile bytes as a packet of any kind a client
// may send and hands each one that decodes to the handler of a player in
// a world: no packet may make a handler panic, nor make the session write
// a packet that does not encode.
func FuzzHandlePacket(f *testing.F) {
	if dir := os.Getenv("FUZZ_CRASH_DIR"); dir != "" {
		cf, _ := os.Create(fmt.Sprintf("%s/crash-%d.txt", dir, os.Getpid()))
		_ = debug.SetCrashOutput(cf, debug.CrashOptions{})
	}
	log := slog.New(slog.DiscardHandler)
	w := world.Config{Log: log}.New()
	f.Cleanup(func() { _ = w.Close() })
	conn := &fuzzConn{identity: login.IdentityData{Identity: uuid.NewString(), DisplayName: "Fuzz", XUID: "2535400000000001"}, closed: make(chan struct{})}
	sess := session.Config{Log: log, MaxChunkRadius: 1}.New(conn)
	f.Cleanup(sess.CloseConnection)
	spawn := func() *world.EntityHandle {
		pos := mgl64.Vec3{0.5, 64, 0.5}
		cfg := player.Config{Session: sess, Position: pos, UUID: uuid.MustParse(conn.identity.Identity), Name: conn.identity.DisplayName}
		h := world.EntitySpawnOpts{Position: pos, ID: cfg.UUID}.New(player.Type, cfg)
		sess.SetHandle(h, cfg.Skin)
		<-w.Exec(func(tx *world.Tx) { sess.Spawn(tx.AddEntity(h).(*player.Player), tx) })
		return h
	}
	h := spawn()

	// Every packet of no fields set seeds the fuzzer, but those that have
	// no wire form then, as a transaction of no data.
	for id := uint32(0); id < 1024; id++ {
		if pk := v2193.New(id); pk != nil {
			func() {
				defer func() { _ = recover() }()
				var wr wire.Writer
				pk.Encode(&wr)
				f.Add(uint16(id), wr.B)
			}()
		}
	}
	f.Fuzz(func(t *testing.T, id uint16, b []byte) {
		pk := v2193.New(uint32(id) & 0x3ff)
		if pk == nil {
			return
		}
		r := wire.NewReader(b)
		r.MaxList = 1024
		pk.Decode(r)
		if r.Err() != nil {
			return
		}
		var panicked string
		<-w.Exec(func(tx *world.Tx) {
			e, ok := h.Entity(tx)
			if !ok {
				return
			}
			defer func() {
				if r := recover(); r != nil {
					panicked = fmt.Sprintf("%v\n%s", r, debug.Stack())
				}
			}()
			_ = sess.HandlePacket(pk, tx, e.(*player.Player))
		})
		if panicked != "" {
			t.Fatalf("%T %+v panicked: %s", pk, pk, panicked)
		}
		conn.mu.Lock()
		unencoded := conn.unencoded
		conn.mu.Unlock()
		if unencoded != "" {
			t.Fatalf("after %T %+v, the session wrote a packet that does not encode: %s", pk, pk, unencoded)
		}
	})
}
