package session

import (
	"bytes"
	"encoding/hex"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

func TestBlockActorDataNBTBytes(t *testing.T) {
	pk := &packet.BlockActorData{
		Position: protocol.BlockPos{12, 64, -7},
		NBTData:  block.EnchantingTable{}.EncodeNBT(),
	}
	got := marshalPacketBytes(t, pk)
	want, err := hex.DecodeString("381880010d0a00080269640c456e6368616e745461626c6500")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("BlockActorData bytes differ\ngot:  %x\nwant: %x", got, want)
	}
}

func marshalPacketBytes(t *testing.T, pk packet.Packet) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := (&packet.Header{PacketID: pk.ID()}).Write(&buf); err != nil {
		t.Fatal(err)
	}
	pk.Marshal(protocol.NewWriter(&buf, 0))
	return buf.Bytes()
}
