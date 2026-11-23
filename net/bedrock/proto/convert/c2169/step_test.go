package c2169

import (
	"bytes"
	"math/rand/v2"
	"reflect"
	"slices"
	"testing"

	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2168"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2169"
	"github.com/dm-vev/adamant/net/bedrock/wire"
)

type codec struct {
	name string
	new  func(id uint32) proto.Packet
}

var (
	from = codec{"v2168", v2168.New}
	to   = codec{"v2169", v2169.New}
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
	reflect.TypeFor[v2168.StackRequestAction]():       {reflect.TypeFor[v2168.TakeStackRequestAction](), reflect.TypeFor[v2168.PlaceStackRequestAction]()},
	reflect.TypeFor[v2169.StackRequestAction]():       {reflect.TypeFor[v2169.TakeStackRequestAction](), reflect.TypeFor[v2169.PlaceStackRequestAction]()},
	reflect.TypeFor[v2168.InventoryTransactionData](): {reflect.TypeFor[v2168.NormalTransactionData](), reflect.TypeFor[v2168.UseItemTransactionData]()},
	reflect.TypeFor[v2169.InventoryTransactionData](): {reflect.TypeFor[v2169.NormalTransactionData](), reflect.TypeFor[v2169.UseItemTransactionData]()},
	reflect.TypeFor[v2168.Event]():                    {reflect.TypeFor[v2168.AchievementAwardedEvent]()},
	reflect.TypeFor[v2169.Event]():                    {reflect.TypeFor[v2169.AchievementAwardedEvent]()},
	reflect.TypeFor[v2168.ShapeData]():                {reflect.TypeFor[v2168.BoxShape](), reflect.TypeFor[v2168.TextShape]()},
	reflect.TypeFor[v2169.ShapeData]():                {reflect.TypeFor[v2169.BoxShape](), reflect.TypeFor[v2169.TextShape]()},
	reflect.TypeFor[v2168.ItemDescriptor]():           {reflect.TypeFor[v2168.DefaultItemDescriptor]()},
	reflect.TypeFor[v2169.ItemDescriptor]():           {reflect.TypeFor[v2169.DefaultItemDescriptor]()},
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
	rng := rand.New(rand.NewPCG(2168, 2169))
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
	if same < 230 {
		t.Fatalf("Same holds for %d packets, want >= 230", same)
	}
}

func TestPacketListsAndCoverage(t *testing.T) {
	if !slices.Equal(changed, []uint32{108}) || len(added) != 0 || len(removed) != 0 {
		t.Fatalf("changed=%v added=%v removed=%v", changed, added, removed)
	}
	for _, id := range changed {
		if reviewed[id] == "" {
			t.Errorf("changed packet %d not in reviewed", id)
		}
	}
}

func TestSetScoreRoundTrip(t *testing.T) {
	in := &v2169.SetScorePacket{
		Entries: []v2169.ScoreboardEntry{
			{IdentityType: 0, EntryID: 42, ObjectiveName: "sidebar"},
			{IdentityType: 0, EntryID: 43, ObjectiveName: ""},
			{IdentityType: 3, EntryID: 44, ObjectiveName: "sidebar", Score: 7, DisplayName: "hi"},
		},
	}
	down := Down(in)
	if len(down) != 1 {
		t.Fatalf("Down len=%d", len(down))
	}
	b2168, ok := encode(down[0])
	if !ok {
		t.Fatal("encode v2168 failed")
	}
	dec2168, ok := decode(from, v2168.IDSetScore, b2168)
	if !ok {
		t.Fatal("decode v2168 failed")
	}
	// Verify v2168 bytes do NOT decode identically as v2169 because of the extra bool.
	if _, ok := decode(to, v2169.IDSetScore, b2168); ok {
		t.Fatal("expected v2168 SetScore remove bytes to differ from v2169")
	}
	up := Up(dec2168)
	if len(up) != 1 {
		t.Fatalf("Up len=%d", len(up))
	}
	got := up[0].(*v2169.SetScorePacket)
	if !reflect.DeepEqual(got, in) {
		t.Fatalf("roundtrip mismatch: got %+v, want %+v", got, in)
	}
}
