package session_test

import (
	"log/slog"
	"math"
	"testing"

	"github.com/df-mc/dragonfly/protocol/bedrock/session"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/anticheat"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/login"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	v2193 "github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// TestTimerDefersInputsAhead has a client send 20 PlayerAuthInputs at once,
// a second of its ticks, to a player of a world that ticks when the test
// says: the game takes ten, the tolerance of Timer, in the first tick and
// one a tick after, and the session holds the rest, in their order, in its
// inbox. A deferred input moves the player where it says when it is
// handled.
func TestTimerDefersInputsAhead(t *testing.T) {
	log := slog.New(slog.DiscardHandler)
	w := world.Config{Log: log, Synchronous: true}.New()
	t.Cleanup(func() { _ = w.Close() })
	conn := &receiverConn{
		fuzzConn: &fuzzConn{identity: login.Identity{Identity: uuid.NewString(), DisplayName: "Timer"}, closed: make(chan struct{})},
		handle:   make(chan func(proto.Packet, int), 1),
	}
	sess := session.Config{Log: log, MaxChunkRadius: 1}.New(conn)
	t.Cleanup(sess.CloseConnection)
	pos := mgl64.Vec3{0.5, 64, 0.5}
	cfg := player.Config{Client: sess, Position: pos, UUID: uuid.MustParse(conn.identity.Identity), Name: conn.identity.DisplayName,
		AntiCheat: anticheat.Default(), Log: log, GameMode: world.GameModeCreative}
	h := world.EntitySpawnOpts{Position: pos, ID: cfg.UUID}.New(player.Type, cfg)
	sess.Bind(h, cfg.Skin)
	<-w.Exec(func(tx *world.Tx) { sess.Spawn(tx.AddEntity(h).(*player.Player), tx) })
	handle := <-conn.handle

	at := func(i int) mgl64.Vec3 { return pos.Add(mgl64.Vec3{0.1 * float64(i), 0, 0}) }
	for i := 1; i <= 20; i++ {
		eyes := at(i).Add(mgl64.Vec3{0, player.Type.NetworkOffset(), 0})
		handle(&v2193.PlayerAuthInputPacket{Position: [3]float32{float32(eyes[0]), float32(eyes[1]), float32(eyes[2])}, Tick: uint64(i)}, 200)
	}
	position := func() mgl64.Vec3 {
		var p mgl64.Vec3
		<-w.Exec(func(tx *world.Tx) {
			e, _ := h.Entity(tx)
			p = e.Position()
		})
		return p
	}
	near := func(a, b mgl64.Vec3) bool { return a.Sub(b).Len() < 1e-3 && math.Abs(a[1]-b[1]) < 1e-3 }
	w.AdvanceTick()
	if got := sess.NetStats().Inbox; got != 10 {
		t.Fatalf("after the first tick the inbox holds %d inputs, want 10", got)
	}
	if got := position(); !near(got, at(10)) {
		t.Fatalf("after the first tick the player is at %v, want %v", got, at(10))
	}
	w.AdvanceTick()
	if got := sess.NetStats().Inbox; got != 9 {
		t.Fatalf("after the second tick the inbox holds %d inputs, want 9", got)
	}
	if got := position(); !near(got, at(11)) {
		t.Fatalf("after the second tick the player is at %v, want %v: the deferred input moved it elsewhere", got, at(11))
	}
}
