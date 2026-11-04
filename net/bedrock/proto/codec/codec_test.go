package codec

import (
	"testing"

	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/convert/c2225"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2225"
	"github.com/dm-vev/adamant/net/bedrock/wire"
)

// TestCodecs checks the codecs are in order, the model the newest, and
// that every codec but the model's converts.
func TestCodecs(t *testing.T) {
	cs := All()
	if len(cs) != 2 || cs[0] != V2193 || cs[1] != V2225 || Model() != V2225 {
		t.Fatalf("codecs %v, model %v", cs, Model())
	}
	for i, c := range cs {
		if i > 0 && cs[i-1].Number >= c.Number {
			t.Errorf("%d comes before %d", cs[i-1].Number, c.Number)
		}
		if got, ok := ByNumber(c.Number); !ok || got != c {
			t.Errorf("ByNumber(%d) = %v", c.Number, got)
		}
		if (c == Model()) == c.Converts() {
			t.Errorf("%d: Converts() = %v", c.Number, c.Converts())
		}
		if c.New == nil || c.FromClient == nil || c.SetShieldID == nil {
			t.Errorf("%d lacks codecs", c.Number)
		}
	}
	if _, ok := ByNumber(2250); ok {
		t.Error("a codec of 2250")
	}
}

// TestDecode decodes a packet of 2193 of the same layout as the model's,
// and one 2225 changed through the step.
func TestDecode(t *testing.T) {
	decode := func(c *Codec, pk proto.Packet) proto.Packet {
		t.Helper()
		var w wire.Writer
		pk.Encode(&w)
		r := wire.NewReader(w.B)
		pks := c.Decode(pk.ID(), r)
		if r.Err() != nil || r.Len() != 0 || len(pks) != 1 {
			t.Fatalf("%T: %v, %d left, %d packets", pk, r.Err(), r.Len(), len(pks))
		}
		return pks[0]
	}
	if text, ok := decode(V2193, &v2193.TextPacket{Message: "hi"}).(*v2225.TextPacket); !ok || text.Message != "hi" {
		t.Fatalf("Text of 2193 decodes as %T", text)
	}
	if a, ok := decode(V2193, &v2193.AnimatePacket{ActionType: 1, EntityRuntimeID: 7}).(*v2225.AnimatePacket); !ok || a.EntityRuntimeID != 7 || a.Hand != 0 {
		t.Fatalf("Animate of 2193 decodes as %+v", a)
	}
	if _, ok := decode(V2225, &v2225.AnimatePacket{Hand: 1}).(*v2225.AnimatePacket); !ok {
		t.Fatal("Animate of the model does not decode as the model")
	}
	if pks := V2193.Decode(1000, wire.NewReader(nil)); pks != nil {
		t.Fatalf("an unknown ID decodes to %v", pks)
	}
}

// wrapped is a packet of the model in a wrapper, which goes as it is if
// the packet does.
type wrapped struct{ proto.Packet }

// TestEncode writes a packet of the same layout as it is, wrapper and all,
// without allocating, one 2225 changed converted, and one 2193 has no form
// of not at all.
func TestEncode(t *testing.T) {
	var got []proto.Packet
	write := func(pk proto.Packet) error {
		got = append(got, pk)
		return nil
	}
	text := &wrapped{&v2225.TextPacket{Message: "hi"}}
	for _, c := range []struct {
		pk, model proto.Packet
		check     func() bool
	}{
		{text, text.Packet, func() bool { return len(got) == 1 && got[0] == text }},
		{&v2225.AnimatePacket{Hand: 1}, nil, func() bool {
			_, ok := got[0].(*v2193.AnimatePacket)
			return len(got) == 1 && ok
		}},
		{&v2225.ClientboundMatchmakingStatePacket{}, nil, func() bool { return len(got) == 0 }},
	} {
		got = nil
		model := c.model
		if model == nil {
			model = c.pk
		}
		if err := V2193.Encode(c.pk, model, write); err != nil || !c.check() {
			t.Errorf("%T: wrote %v, %v", c.pk, got, err)
		}
	}
	discard := func(proto.Packet) error { return nil }
	model := text.Packet
	if n := testing.AllocsPerRun(100, func() { _ = V2193.Encode(text, model, discard) }); n != 0 {
		t.Errorf("Encode of a packet of the same layout allocates %v times", n)
	}
	if !c2225.DownKeeps(model) {
		t.Fatal("c2225 does not keep a Text")
	}
}
