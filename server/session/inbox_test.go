package session_test

import (
	"log/slog"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/login"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	v2193 "github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// receiverConn hands the packets of a test to its session as a connection
// of the server does: on a goroutine of its own, with the memory each
// takes.
type receiverConn struct {
	*fuzzConn
	handle chan func(pk proto.Packet, cost int)
}

func (c *receiverConn) Receive(handle func(pk proto.Packet, cost int), _ func(err error)) {
	c.handle <- handle
}

// TestInboxMemoryOfTicks has a client send packets of the most memory a
// packet may take, 4 MiB, to a player of a world that ticks only when the
// test says: as many as the inbox may hold, 32 MiB, wait for a tick, which
// handles them and gives their memory back, so as many fit again; one more
// before the next tick cuts the connection.
func TestInboxMemoryOfTicks(t *testing.T) {
	log := slog.New(slog.DiscardHandler)
	w := world.Config{Log: log, Synchronous: true}.New()
	t.Cleanup(func() { _ = w.Close() })
	conn := &receiverConn{
		fuzzConn: &fuzzConn{identity: login.Identity{Identity: uuid.NewString(), DisplayName: "Inbox"}, closed: make(chan struct{})},
		handle:   make(chan func(proto.Packet, int), 1),
	}
	sess := session.Config{Log: log, MaxChunkRadius: 1}.New(conn)
	t.Cleanup(sess.CloseConnection)
	pos := mgl64.Vec3{0.5, 64, 0.5}
	cfg := player.Config{Client: sess, Position: pos, UUID: uuid.MustParse(conn.identity.Identity), Name: conn.identity.DisplayName}
	h := world.EntitySpawnOpts{Position: pos, ID: cfg.UUID}.New(player.Type, cfg)
	sess.SetHandle(h, cfg.Skin)
	<-w.Exec(func(tx *world.Tx) { sess.Spawn(tx.AddEntity(h).(*player.Player), tx) })
	handle := <-conn.handle

	send := func(n int) {
		for range n {
			handle(&v2193.AnimatePacket{}, 4<<20)
		}
	}
	send(8)
	if got := sess.NetStats().Inbox; got != 8 {
		t.Fatalf("the inbox holds %d packets, want 8", got)
	}
	w.AdvanceTick()
	if got := sess.NetStats().Inbox; got != 0 {
		t.Fatalf("a tick left %d packets in the inbox", got)
	}
	send(8)
	if got := sess.NetStats().Inbox; got != 8 {
		t.Fatalf("after a tick the inbox took %d packets of 8 within the memory it may hold", got)
	}
	send(1)
	select {
	case <-conn.closed:
	case <-time.After(5 * time.Second):
		t.Fatal("a client past the memory the inbox may hold was not cut")
	}
}
