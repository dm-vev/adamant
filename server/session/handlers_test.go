package session

import (
	"log/slog"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/netconn"
	"github.com/dm-vev/adamant/net/bedrock"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/dm-vev/adamant/net/bedrock/wire"
)

// TestHandledPacketsDecode decodes a packet of each ID a session handles as
// the server decodes what a client sends: none may be refused for its ID,
// which would drop every client that sends it.
func TestHandledPacketsDecode(t *testing.T) {
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
