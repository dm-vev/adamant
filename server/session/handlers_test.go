package session

import (
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
