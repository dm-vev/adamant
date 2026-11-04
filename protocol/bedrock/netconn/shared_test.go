package netconn

import (
	"bytes"
	"testing"

	"github.com/df-mc/dragonfly/protocol/bedrock/internal/versions"
	"github.com/dm-vev/adamant/net/bedrock"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
	"github.com/dm-vev/adamant/net/bedrock/wire"
)

// TestSharedIsEncodedOncePerClass writes a Shared of a packet of the same
// layout in 2193 and the model, and one that 2225 changed: the first goes
// in the model's bytes to every protocol, encoded once; the second in the
// model's to the model and in 2193's, encoded once more, to 2193.
func TestSharedIsEncodedOncePerClass(t *testing.T) {
	text := &Shared{Packet: &mc.TextPacket{TextType: mc.TextTypeRaw, Message: "hi"}}
	model := text.encoded(versions.Model())
	if len(model) != 1 || &text.encoded(nil)[0][0] != &model[0][0] || &text.encoded(versions.Default())[0][0] != &model[0][0] || text.wires != nil {
		t.Fatal("a Text is not encoded once for every protocol")
	}
	var w wire.Writer
	text.Encode(&w)
	if _, fields, _ := bedrock.ReadHeader(model[0]); !bytes.Equal(w.B, fields) {
		t.Fatal("the Shared encodes otherwise than it writes")
	}

	animate := &Shared{Packet: &mc.AnimatePacket{ActionType: mc.AnimateActionSwingArm, EntityRuntimeID: 7, Hand: 1}}
	old := animate.encoded(versions.Default())
	if len(old) != 1 || &animate.encoded(versions.Default())[0][0] != &old[0][0] || len(animate.wires) != 1 {
		t.Fatal("an Animate is not encoded once for 2193")
	}
	h, fields, err := bedrock.ReadHeader(old[0])
	var down v2193.AnimatePacket
	r := wire.NewReader(fields)
	down.Decode(r)
	if err != nil || h.ID != mc.IDAnimate || r.Err() != nil || r.Len() != 0 || down.EntityRuntimeID != 7 {
		t.Fatalf("the Animate of 2193 is % x", old[0])
	}
	if now := animate.encoded(nil); bytes.Equal(now[0], old[0]) {
		t.Fatal("the model writes the Animate as 2193 does")
	}
}

// TestConnWritesShared writes a Shared through a connection of 2193.
func TestConnWritesShared(t *testing.T) {
	l := &Listener{batches: bedrock.Config{Cache: bedrock.NewBatchCache()}}
	c := newConn(l, discard{}, nil)
	c.comp = bedrock.None{}
	c.b.EnableCompression(c.comp)
	c.proto = versions.Default()
	pk := &Shared{Packet: &mc.AnimatePacket{ActionType: mc.AnimateActionSwingArm, EntityRuntimeID: 7}}
	if err := c.WritePacket(pk); err != nil {
		t.Fatal(err)
	}
	if pk.wires[versions.Default()] == nil {
		t.Fatal("the connection did not write the Animate of 2193")
	}
	if n := testing.AllocsPerRun(100, func() { _ = c.WritePacket(pk) }); n != 0 {
		t.Errorf("writing a Shared encoded allocates %v times", n)
	}
}
