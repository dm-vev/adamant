package c2193

import (
	"bytes"
	"math/rand/v2"
	"reflect"
	"slices"
	"testing"

	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2169"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/dm-vev/adamant/net/bedrock/wire"
)

type codec struct {
	name string
	new  func(id uint32) proto.Packet
}

var (
	from = codec{"v2169", v2169.New}
	to   = codec{"v2193", v2193.New}
)

func fill(rng *rand.Rand, v reflect.Value, depth int) {
	small := rng.IntN(2) == 0
	switch v.Kind() {
	case reflect.Bool:
		v.SetBool(rng.IntN(2) == 0)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if small {
			v.SetInt(int64(rng.IntN(12)))
		} else {
			v.SetInt(int64(rng.Uint64()) >> (64 - v.Type().Bits()))
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if small {
			v.SetUint(uint64(rng.IntN(12)))
		} else {
			v.SetUint(rng.Uint64() >> (64 - v.Type().Bits()))
		}
	case reflect.Float32, reflect.Float64:
		v.SetFloat(float64(rng.IntN(2000)-1000) / 8)
	case reflect.String:
		b := make([]byte, rng.IntN(5))
		for i := range b {
			b[i] = byte('a' + rng.IntN(26))
		}
		v.SetString(string(b))
	case reflect.Slice:
		if depth > 5 {
			return
		}
		n := rng.IntN(3)
		s := reflect.MakeSlice(v.Type(), n, n)
		for i := range n {
			fill(rng, s.Index(i), depth+1)
		}
		v.Set(s)
	case reflect.Array:
		for i := range v.Len() {
			fill(rng, v.Index(i), depth+1)
		}
	case reflect.Map:
		m := reflect.MakeMap(v.Type())
		if v.Type().Key().Kind() == reflect.String && rng.IntN(2) == 0 {
			m.SetMapIndex(reflect.ValueOf("k"), reflect.ValueOf(int32(rng.IntN(100))))
		}
		v.Set(m)
	case reflect.Struct:
		for i := range v.NumField() {
			if v.Type().Field(i).IsExported() {
				fill(rng, v.Field(i), depth+1)
			}
		}
	case reflect.Interface:
		if v.Type().NumMethod() == 0 {
			v.Set(reflect.ValueOf([]any{true, uint32(rng.IntN(9)), float32(rng.IntN(9)) / 2}[rng.IntN(3)]))
		} else if impls := unions[v.Type()]; len(impls) > 0 && depth < 6 {
			x := reflect.New(impls[rng.IntN(len(impls))])
			fill(rng, x.Elem(), depth+1)
			v.Set(x)
		}
	case reflect.Pointer:
		if depth > 5 {
			return
		}
		p := reflect.New(v.Type().Elem())
		fill(rng, p.Elem(), depth+1)
		v.Set(p)
	}
}

var unions = map[reflect.Type][]reflect.Type{
	reflect.TypeFor[v2169.StackRequestAction]():       {reflect.TypeFor[v2169.TakeStackRequestAction](), reflect.TypeFor[v2169.PlaceStackRequestAction]()},
	reflect.TypeFor[v2193.StackRequestAction]():       {reflect.TypeFor[v2193.TakeStackRequestAction](), reflect.TypeFor[v2193.PlaceStackRequestAction]()},
	reflect.TypeFor[v2169.InventoryTransactionData](): {reflect.TypeFor[v2169.NormalTransactionData](), reflect.TypeFor[v2169.UseItemTransactionData]()},
	reflect.TypeFor[v2193.InventoryTransactionData](): {reflect.TypeFor[v2193.NormalTransactionData](), reflect.TypeFor[v2193.UseItemTransactionData]()},
	reflect.TypeFor[v2169.Event]():                    {reflect.TypeFor[v2169.AchievementAwardedEvent]()},
	reflect.TypeFor[v2193.Event]():                    {reflect.TypeFor[v2193.AchievementAwardedEvent]()},
	reflect.TypeFor[v2169.ShapeData]():                {reflect.TypeFor[v2169.BoxShape](), reflect.TypeFor[v2169.TextShape]()},
	reflect.TypeFor[v2193.ShapeData]():                {reflect.TypeFor[v2193.BoxShape](), reflect.TypeFor[v2193.TextShape]()},
	reflect.TypeFor[v2169.ItemDescriptor]():           {reflect.TypeFor[v2169.DefaultItemDescriptor]()},
	reflect.TypeFor[v2193.ItemDescriptor]():           {reflect.TypeFor[v2193.DefaultItemDescriptor]()},
}

func encode(pk proto.Packet) (b []byte, ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	var w wire.Writer
	pk.Encode(&w)
	return w.B, true
}

func decode(c codec, id uint32, b []byte) (pk proto.Packet, ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	pk = c.new(id)
	r := wire.NewReader(b)
	pk.Decode(r)
	return pk, r.Err() == nil && r.Len() == 0
}

func TestSameLayoutsAgree(t *testing.T) {
	rng := rand.New(rand.NewPCG(2169, 2193))
	same := 0
	for id := range uint32(1024) {
		if !Same(id) {
			continue
		}
		same++
		for _, c := range []codec{from, to} {
			other := to
			if c.name == to.name {
				other = from
			}
			for range 12 {
				pk := c.new(id)
				fill(rng, reflect.ValueOf(pk).Elem(), 0)
				b, ok := encode(pk)
				if !ok {
					continue
				}
				pa, okA := decode(c, id, b)
				if !okA {
					continue
				}
				pb, okB := decode(other, id, b)
				if !okB {
					t.Errorf("packet %d: decoded with %s, not %s", id, c.name, other.name)
					continue
				}
				ea, _ := encode(pa)
				eb, _ := encode(pb)
				if !bytes.Equal(ea, eb) {
					t.Errorf("packet %d: re-encode mismatch between %s and %s", id, c.name, other.name)
				}
			}
		}
	}
	if same < 215 {
		t.Fatalf("Same holds for %d packets, want >= 215", same)
	}
}

func TestPacketListsAndCoverage(t *testing.T) {
	for id := range uint32(1024) {
		f, n := from.new(id) != nil, to.new(id) != nil
		switch {
		case slices.Contains(changed, id):
			if !f || !n || Same(id) {
				t.Errorf("packet %d is listed as changed", id)
			}
			if reviewed[id] == "" {
				t.Errorf("changed packet %d not in reviewed", id)
			}
		case slices.Contains(added, id):
			if f || !n {
				t.Errorf("packet %d is listed as added", id)
			}
			if noForm[id] == "" {
				t.Errorf("added packet %d not in noForm", id)
			}
		case slices.Contains(removed, id):
			if !f || n {
				t.Errorf("packet %d is listed as removed", id)
			}
		case f != n || f && !Same(id):
			t.Errorf("packet %d is in no list", id)
		}
	}
}

func TestFixupsRoundTrip(t *testing.T) {
	// DimensionData: MinimumY=-64, HeightRange=384 <-> Range=[320, -64]
	dimIn := &v2193.DimensionDataPacket{
		Definitions: []v2193.DimensionDefinition{
			{Name: "minecraft:overworld", MinimumY: -64, HeightRange: 384, Generator: 1, DimensionType: 0},
		},
	}
	dim2169 := Down(dimIn)[0].(*v2169.DimensionDataPacket)
	if dim2169.Definitions[0].Range != [2]int32{320, -64} {
		t.Fatalf("DimensionData down Range = %v, want [320 -64]", dim2169.Definitions[0].Range)
	}
	dimBack := Up(dim2169)[0].(*v2193.DimensionDataPacket)
	if dimBack.Definitions[0].MinimumY != -64 || dimBack.Definitions[0].HeightRange != 384 {
		t.Fatalf("DimensionData up = %+v", dimBack.Definitions[0])
	}

	// SubChunk: 16x16 HeightMap <-> flat 256 int8s
	var hm v2193.HeightMap
	for z := range 16 {
		for x := range 16 {
			hm[z][x] = int8(z*16 + x - 128)
		}
	}
	scIn := &v2193.SubChunkPacket{
		SubChunkEntries: []v2193.SubChunkEntry{
			{HeightMapType: 1, HeightMapData: proto.Some(hm)},
		},
	}
	sc2169 := Down(scIn)[0].(*v2169.SubChunkPacket)
	if !sc2169.SubChunkEntries[0].HeightMapData.Set || len(sc2169.SubChunkEntries[0].HeightMapData.Val) != 256 {
		t.Fatalf("SubChunk down heightmap len=%d", len(sc2169.SubChunkEntries[0].HeightMapData.Val))
	}
	b, ok := encode(sc2169)
	if !ok {
		t.Fatal("encode v2169 SubChunk failed")
	}
	dec, ok := decode(from, v2169.IDSubChunk, b)
	if !ok {
		t.Fatal("decode v2169 SubChunk failed")
	}
	scBack := Up(dec)[0].(*v2193.SubChunkPacket)
	if !scBack.SubChunkEntries[0].HeightMapData.Set || scBack.SubChunkEntries[0].HeightMapData.Val != hm {
		t.Fatalf("SubChunk heightmap roundtrip mismatch")
	}

	// PlayerAuthInput + PlayerInventoryAction + InputFlags
	paiIn := &v2169.PlayerAuthInputPacket{
		Tick:      10,
		InputData: v2169.InputFlags{1, 5},
		ItemInteractionData: proto.Some(v2169.PlayerInventoryAction{
			ActionType: 0,
			HotBarSlot: 2,
			Actions:    proto.Some([]v2169.InventoryAction{{SourceType: 0, InventorySlot: 3}}),
		}),
	}
	bPai, ok := encode(paiIn)
	if !ok {
		t.Fatal("encode v2169 PlayerAuthInput failed")
	}
	decPai, ok := decode(from, v2169.IDPlayerAuthInput, bPai)
	if !ok {
		t.Fatal("decode v2169 PlayerAuthInput failed")
	}
	pai2193 := Up(decPai)[0].(*v2193.PlayerAuthInputPacket)
	if pai2193.Tick != 10 || !slices.Equal(pai2193.InputData, []int32{1, 5}) || !pai2193.ItemInteractionData.Set || len(pai2193.ItemInteractionData.Val.Actions) != 1 {
		t.Fatalf("PlayerAuthInput up mismatch: %+v", pai2193)
	}

	// New 2193 packets are dropped going down to 2169.
	if out := Down(&v2193.RecordStartedPacket{}); len(out) != 0 {
		t.Fatalf("RecordStarted down = %v, want nil", out)
	}
	if out := Down(&v2193.SetPlayerFurnaceOptionsPacket{}); len(out) != 0 {
		t.Fatalf("SetPlayerFurnaceOptions down = %v, want nil", out)
	}
}
