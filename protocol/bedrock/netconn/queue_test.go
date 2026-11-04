package netconn

import (
	"context"
	"math/rand/v2"
	"net"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/protocol/bedrock/internal/versions"
	"github.com/dm-vev/adamant/net/bedrock"
	ourclient "github.com/dm-vev/adamant/net/bedrock/client"
	ourlogin "github.com/dm-vev/adamant/net/bedrock/login"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
	"github.com/dm-vev/adamant/net/sctp"
)

// TestSendQueueOfAssociation takes the bytes an association buffered past
// those in flight, none once all are in flight with the headers of their
// chunks, with its window and round trip.
func TestSendQueueOfAssociation(t *testing.T) {
	for _, c := range []struct {
		buffered, flight, queued int
	}{
		{100 << 10, 30 << 10, 70 << 10},
		{1000, 1000 + 16, 0},
	} {
		queued, window, rtt := sendQueue(sctp.Stats{Buffered: c.buffered, Flight: c.flight, Cwnd: 50 << 10, RTT: time.Millisecond})
		if queued != c.queued || window != 50<<10 || rtt != time.Millisecond {
			t.Errorf("%d buffered, %d in flight: %d queued behind %d over %v", c.buffered, c.flight, queued, window, rtt)
		}
	}
}

// TestSendQueue reports the bytes SCTP queued for a client behind those in
// flight, with the window and round trip of its path, until the client
// took them; and nothing for a player of a proxy, which queues for its
// clients itself.
func TestSendQueue(t *testing.T) {
	if queued, window, rtt := (&Conn{}).SendQueue(); queued != 0 || window != 0 || rtt != 0 {
		t.Fatalf("a player of a proxy queues %d behind a window of %d over %v", queued, window, rtt)
	}
	l := listen(t, Config{})
	spawned := serve(t, l)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	conn, err := ourclient.Dial(ctx, "http://"+l.Addr().String(), ourclient.Config{Identity: ourlogin.Identity{DisplayName: "Bot"}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	srv := <-spawned

	// A Static goes out in one message encoded once: the client on the
	// loopback takes the bytes sent about as fast as they are compressed.
	letters := make([]byte, 512<<10)
	for i := range letters {
		letters[i] = byte('a' + rand.IntN(26))
	}
	text := &staticText{&mc.TextPacket{TextType: mc.TextTypeRaw, Message: string(letters)}}
	texts, queued, window, rtt := 0, 0, 0, time.Duration(0)
	for queued == 0 && texts < 16 {
		if err := srv.WritePacket(text); err != nil {
			t.Fatal(err)
		}
		texts++
		queued, window, rtt = srv.SendQueue()
	}
	if queued <= 0 || window < 4<<10 || rtt <= 0 {
		t.Fatalf("%d queued behind a window of %d over %v right after %d texts of 512 KiB", queued, window, rtt, texts)
	}
	for read := 0; read < texts; {
		pk, err := conn.ReadPacket()
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := pk.(*mc.TextPacket); ok {
			read++
		}
	}
	for queued, _, _ = srv.SendQueue(); queued != 0; queued, _, _ = srv.SendQueue() {
		if ctx.Err() != nil {
			t.Fatalf("%d bytes still queued once the client took everything", queued)
		}
		time.Sleep(time.Millisecond)
	}
}

// discard is a transport that takes every batch and sends nothing.
type discard struct{}

func (discard) ReadMessage() ([]byte, error)    { select {} }
func (discard) Send([]byte) error               { return nil }
func (discard) Close() error                    { return nil }
func (discard) SetReadDeadline(time.Time) error { return nil }
func (discard) SetReceiver(func(msg []byte))    {}
func (discard) OnClose(func(error))             {}
func (discard) RemoteAddr() net.Addr            { return &net.UDPAddr{} }

// BenchmarkWritePacket writes the packets of a tick of a client that sees
// others move, and flushes them, as a session does.
func BenchmarkWritePacket(b *testing.B) {
	l := &Listener{batches: bedrock.Config{Cache: bedrock.NewBatchCache()}}
	c := newConn(l, discard{}, nil)
	c.comp = bedrock.Flate{Level: 1}
	c.b.EnableCompression(c.comp)
	// A client of the default protocol, 2193, which the step from the
	// model converts: a move has the same layout in both.
	c.proto = versions.Default()
	move := &mc.MoveActorDeltaPacket{EntityRuntimeID: 7, PositionX: proto.Some[float32](1), PositionZ: proto.Some[float32](1)}
	b.ReportAllocs()
	for b.Loop() {
		for range 16 {
			_ = c.WritePacket(move)
		}
		_ = c.Flush()
	}
}
