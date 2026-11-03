package session

import (
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/protocol/bedrock/netconn"
	"github.com/dm-vev/adamant/net/bedrock"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/dm-vev/adamant/net/bedrock/wire"
)

// TestHandledPacketsDecode decodes a packet of each ID a session handles as
// the server decodes what a client sends: none may be refused for its ID,
// which would drop every client that sends it.
func TestHandledPacketsDecode(t *testing.T) {
	t.Parallel()
	s := &Session{}
	s.registerHandlers()
	// Packets whose zero value has no encoding.
	samples := map[uint32]proto.Packet{
		v2193.IDInventoryTransaction: &v2193.InventoryTransactionPacket{TransactionData: &v2193.NormalTransactionData{}},
		v2193.IDRequestAbility:       &v2193.RequestAbilityPacket{Value: true},
	}
	if len(s.handlers) == 0 {
		t.Fatal("no packet handled")
	}
	for id := range s.handlers {
		pk := samples[id]
		if pk == nil {
			pk = v2193.New(id)
		}
		w := wire.Writer{B: bedrock.AppendHeader(nil, bedrock.Header{ID: id})}
		pk.Encode(&w)
		got, err := netconn.DecodePacket(w.B)
		if err != nil || got.ID() != id {
			t.Errorf("%T: %v", pk, err)
		}
	}
}

// countedError counts the times its message is built.
type countedError struct{ n *int }

func (e countedError) Error() string {
	*e.n++
	return "closed"
}

// TestConnClosedLogsLazily closes the connection of a session with an
// error of a client, which may be as long as a packet: its message is
// built only if the debug log is on.
func TestConnClosedLogsLazily(t *testing.T) {
	t.Parallel()
	var out strings.Builder
	n := 0
	s := &Session{conf: Config{Log: slog.New(slog.NewTextHandler(&out, nil))}}
	s.connClosed(countedError{&n})
	if n != 0 {
		t.Fatalf("the message was built %d times with the debug log off", n)
	}
	s = &Session{conf: Config{Log: slog.New(slog.NewTextHandler(&out, &slog.HandlerOptions{Level: slog.LevelDebug}))}}
	s.connClosed(countedError{&n})
	if n == 0 || !strings.Contains(out.String(), "closed") {
		t.Fatalf("the close was not logged with the debug log on: %q", out.String())
	}
	if !s.inboxClosed {
		t.Fatal("the inbox stayed open")
	}
}

// closingConn tells when the session closed it.
type closingConn struct {
	testConn
	closed chan struct{}
}

func (c closingConn) Close() error {
	close(c.closed)
	return nil
}

// inboxSession returns a session of a connection that tells when it is
// closed.
func inboxSession() (*Session, chan struct{}) {
	closed := make(chan struct{})
	return &Session{conn: closingConn{closed: closed}, conf: Config{Log: slog.New(slog.DiscardHandler)}}, closed
}

// inboxState returns the packets in the inbox of a session, the memory they take
// and whether it is closed.
func (s *Session) inboxState() (n, cost int, closed bool) {
	s.inboxMu.Lock()
	defer s.inboxMu.Unlock()
	return len(s.inbox), s.inboxCost, s.inboxClosed
}

// TestInboxMemoryBound has a client send packets of the most memory a
// packet may take, 4 MiB, while the world of its player does not tick: the
// connection is cut before its inbox holds more than 32 MiB, where the
// count of packets alone let it hold 2048 of them, 8 GiB. Ordinary traffic
// and a burst of the largest items a client sends fit, and the memory of
// the packets the world takes is given back.
func TestInboxMemoryBound(t *testing.T) {
	t.Parallel()
	s, closed := inboxSession()
	for range 64 {
		s.receive(&v2193.BlockActorDataPacket{}, 4<<20)
	}
	if n, cost, cut := s.inboxState(); !cut || n != 8 || cost != 32<<20 {
		t.Fatalf("holds %d packets of %d bytes, cut %v", n, cost, cut)
	}
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("the connection was not closed")
	}

	// Moves for a while, then seven InventoryTransactions of three copies
	// of a shulker box of 27 full books, the largest item a client sends,
	// each 4.24 MiB to hold as netconn decodes it.
	const shulkers = 4441926
	s, _ = inboxSession()
	for range maxInbox - 7 {
		s.receive(&v2193.PlayerAuthInputPacket{}, 200)
	}
	for range 7 {
		s.receive(&v2193.InventoryTransactionPacket{}, shulkers)
	}
	if n, cost, cut := s.inboxState(); cut || n != maxInbox {
		t.Fatalf("ordinary traffic: %d packets of %d bytes, cut %v", n, cost, cut)
	}
	packets, _ := s.takeInbox()
	if n, cost, _ := s.inboxState(); n != 0 || cost != 0 || len(packets) != maxInbox {
		t.Fatalf("took %d packets, left %d of %d bytes", len(packets), n, cost)
	}
	s.requeue(packets[maxInbox-7:])
	if n, cost, _ := s.inboxState(); n != 7 || cost != 7*shulkers {
		t.Fatalf("put back 7 packets: %d packets of %d bytes", n, cost)
	}
}
