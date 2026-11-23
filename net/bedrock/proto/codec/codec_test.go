package codec

import (
	"slices"
	"testing"

	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/convert/c2225"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2168"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2169"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2225"
	"github.com/dm-vev/adamant/net/bedrock/wire"
)

// TestCodecs checks the codecs are in order, the model the newest, and
// that every codec but the model's converts.
func TestCodecs(t *testing.T) {
	cs := All()
	if len(cs) != 4 || cs[0] != V2168 || cs[1] != V2169 || cs[2] != V2193 || cs[3] != V2225 || Model() != V2225 {
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

// TestDecode decodes packets of every supported protocol across single and
// multi-step chains into packets of the model (v2225).
func TestDecode(t *testing.T) {
	decode := func(c *Codec, pk proto.Packet) proto.Packet {
		t.Helper()
		var w wire.Writer
		pk.Encode(&w)
		r := wire.NewReader(w.B)
		pks := c.Decode(pk.ID(), r)
		if r.Err() != nil || r.Len() != 0 || len(pks) != 1 {
			t.Fatalf("%d %T: %v, %d left, %d packets", c.Number, pk, r.Err(), r.Len(), len(pks))
		}
		return pks[0]
	}
	// Same layout across all protocols.
	if text, ok := decode(V2168, &v2168.TextPacket{Message: "hi40"}).(*v2225.TextPacket); !ok || text.Message != "hi40" {
		t.Fatalf("Text of 2168 decodes as %T", text)
	}
	if text, ok := decode(V2169, &v2169.TextPacket{Message: "hi45"}).(*v2225.TextPacket); !ok || text.Message != "hi45" {
		t.Fatalf("Text of 2169 decodes as %T", text)
	}
	if text, ok := decode(V2193, &v2193.TextPacket{Message: "hi"}).(*v2225.TextPacket); !ok || text.Message != "hi" {
		t.Fatalf("Text of 2193 decodes as %T", text)
	}
	// Changed only in c2169 (SetScore): must still arrive as *v2225.SetScorePacket.
	if sc, ok := decode(V2168, &v2168.SetScorePacket{Entries: []v2168.ScoreboardEntry{{IdentityType: 0, EntryID: 9, ObjectiveName: "obj"}}}).(*v2225.SetScorePacket); !ok || len(sc.Entries) != 1 || sc.Entries[0].ObjectiveName != "obj" {
		t.Fatalf("SetScore of 2168 decodes as %#v", sc)
	}
	// Changed only in c2193 (MoveActorDelta): must arrive as *v2225.MoveActorDeltaPacket.
	if mv, ok := decode(V2168, &v2168.MoveActorDeltaPacket{EntityRuntimeID: 11, ForceMove: true}).(*v2225.MoveActorDeltaPacket); !ok || mv.EntityRuntimeID != 11 || !mv.ForceMove {
		t.Fatalf("MoveActorDelta of 2168 decodes as %#v", mv)
	}
	// Changed only in c2225 (Animate): must arrive as *v2225.AnimatePacket.
	if a, ok := decode(V2168, &v2168.AnimatePacket{ActionType: 1, EntityRuntimeID: 7}).(*v2225.AnimatePacket); !ok || a.EntityRuntimeID != 7 || a.Hand != 0 {
		t.Fatalf("Animate of 2168 decodes as %+v", a)
	}
	if a, ok := decode(V2193, &v2193.AnimatePacket{ActionType: 1, EntityRuntimeID: 7}).(*v2225.AnimatePacket); !ok || a.EntityRuntimeID != 7 || a.Hand != 0 {
		t.Fatalf("Animate of 2193 decodes as %+v", a)
	}
	// Changed in both c2193 and c2225 (PlayerAuthInput): must arrive as *v2225.PlayerAuthInputPacket.
	if pai, ok := decode(V2168, &v2168.PlayerAuthInputPacket{Tick: 99, InputData: v2168.InputFlags{1, 3}}).(*v2225.PlayerAuthInputPacket); !ok || pai.Tick != 99 || !slices.Equal(pai.InputData, []int32{1, 3}) {
		t.Fatalf("PlayerAuthInput of 2168 decodes as %+v", pai)
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
// without allocating, converts across 1-, 2-, and 3-step chains, and drops
// packets absent in older protocols.
func TestEncode(t *testing.T) {
	var got []proto.Packet
	write := func(pk proto.Packet) error {
		got = append(got, pk)
		return nil
	}
	text := &wrapped{&v2225.TextPacket{Message: "hi"}}
	for _, tc := range []struct {
		codec     *Codec
		pk, model proto.Packet
		check     func() bool
	}{
		{V2193, text, text.Packet, func() bool { return len(got) == 1 && got[0] == text }},
		{V2169, text, text.Packet, func() bool { return len(got) == 1 && got[0] == text }},
		{V2168, text, text.Packet, func() bool { return len(got) == 1 && got[0] == text }},
		{V2193, &v2225.AnimatePacket{Hand: 1}, nil, func() bool {
			_, ok := got[0].(*v2193.AnimatePacket)
			return len(got) == 1 && ok
		}},
		{V2168, &v2225.SetScorePacket{Entries: []v2225.ScoreboardEntry{{IdentityType: 0, EntryID: 5, ObjectiveName: "s"}}}, nil, func() bool {
			_, ok := got[0].(*v2168.SetScorePacket)
			return len(got) == 1 && ok
		}},
		{V2168, &v2225.MoveActorDeltaPacket{EntityRuntimeID: 3, Ticks: 2}, nil, func() bool {
			if len(got) != 1 {
				return false
			}
			var w wire.Writer
			got[0].Encode(&w)
			dec := v2168.New(v2168.IDMoveActorDelta)
			r := wire.NewReader(w.B)
			dec.Decode(r)
			return r.Err() == nil && r.Len() == 0 && dec.(*v2168.MoveActorDeltaPacket).EntityRuntimeID == 3
		}},
		{V2168, &v2225.DimensionDataPacket{Definitions: []v2225.DimensionDefinition{{Name: "minecraft:overworld", MinimumY: -64, HeightRange: 384}}}, nil, func() bool {
			if len(got) != 1 {
				return false
			}
			var w wire.Writer
			got[0].Encode(&w)
			dec := v2168.New(v2168.IDDimensionData)
			r := wire.NewReader(w.B)
			dec.Decode(r)
			return r.Err() == nil && r.Len() == 0 && dec.(*v2168.DimensionDataPacket).Definitions[0].Range == [2]int32{320, -64}
		}},
		{V2193, &v2225.ClientboundMatchmakingStatePacket{}, nil, func() bool { return len(got) == 0 }},
		{V2169, &v2225.RecordStartedPacket{}, nil, func() bool { return len(got) == 0 }},
		{V2168, &v2225.RecordStartedPacket{}, nil, func() bool { return len(got) == 0 }},
		{V2168, &v2225.LevelEventPacket{EventType: v2225.LevelEventParticleLegacyEvent | 105}, nil, func() bool { return len(got) == 0 }},
	} {
		got = nil
		model := tc.model
		if model == nil {
			model = tc.pk
		}
		if err := tc.codec.Encode(tc.pk, model, write); err != nil || !tc.check() {
			t.Errorf("%d %T: wrote %v, %v", tc.codec.Number, tc.pk, got, err)
		}
	}
	discard := func(proto.Packet) error { return nil }
	model := text.Packet
	for _, c := range []*Codec{V2168, V2169, V2193} {
		if n := testing.AllocsPerRun(100, func() { _ = c.Encode(text, model, discard) }); n != 0 {
			t.Errorf("%d: Encode of a packet of the same layout allocates %v times", c.Number, n)
		}
	}
	if !c2225.DownKeeps(model) {
		t.Fatal("c2225 does not keep a Text")
	}
}
