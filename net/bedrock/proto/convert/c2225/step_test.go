package c2225

import (
	"bytes"
	"math/rand/v2"
	"reflect"
	"slices"
	"testing"

	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2225"
	"github.com/dm-vev/adamant/net/bedrock/wire"
)

// codec is the codecs of a protocol.
type codec struct {
	name string
	new  func(id uint32) proto.Packet
}

var (
	from = codec{"v2193", v2193.New}
	to   = codec{"v2225", v2225.New}
)

// fill sets a value to random contents: small numbers often, so that enum
// branches are taken, short strings and lists, values of the interfaces of
// the codecs. A value the codecs refuse to encode is left to encode to
// refuse.
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
			// The values of game rules, abilities and settings.
			v.Set(reflect.ValueOf([]any{true, uint32(rng.IntN(9)), float32(rng.IntN(9)) / 2}[rng.IntN(3)]))
		} else if impls := implementations(v.Type()); len(impls) > 0 && depth < 6 {
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

// implementations returns the kinds of a union of the codecs, an
// interface every generated type implements: those the codecs decode into
// it, and those unions lists.
func implementations(t reflect.Type) []reflect.Type {
	return append(slices.Clone(decodedKinds()[t]), unions[t]...)
}

// unions lists the kinds of the unions the codecs choose by a number deep
// in a packet, which decodedKinds does not reach.
var unions = map[reflect.Type][]reflect.Type{
	reflect.TypeFor[v2193.StackRequestAction]():       {reflect.TypeFor[v2193.TakeStackRequestAction](), reflect.TypeFor[v2193.PlaceStackRequestAction](), reflect.TypeFor[v2193.SwapStackRequestAction](), reflect.TypeFor[v2193.DropStackRequestAction](), reflect.TypeFor[v2193.DestroyStackRequestAction](), reflect.TypeFor[v2193.ConsumeStackRequestAction](), reflect.TypeFor[v2193.CreateStackRequestAction](), reflect.TypeFor[v2193.LabTableCombineStackRequestAction](), reflect.TypeFor[v2193.BeaconPaymentStackRequestAction](), reflect.TypeFor[v2193.MineBlockStackRequestAction](), reflect.TypeFor[v2193.CraftRecipeStackRequestAction](), reflect.TypeFor[v2193.AutoCraftRecipeStackRequestAction](), reflect.TypeFor[v2193.CraftCreativeStackRequestAction](), reflect.TypeFor[v2193.CraftRecipeOptionalStackRequestAction](), reflect.TypeFor[v2193.CraftGrindstoneRecipeStackRequestAction](), reflect.TypeFor[v2193.CraftLoomRecipeStackRequestAction](), reflect.TypeFor[v2193.CraftNonImplementedStackRequestAction](), reflect.TypeFor[v2193.CraftResultsDeprecatedStackRequestAction]()},
	reflect.TypeFor[v2225.StackRequestAction]():       {reflect.TypeFor[v2225.TakeStackRequestAction](), reflect.TypeFor[v2225.PlaceStackRequestAction](), reflect.TypeFor[v2225.SwapStackRequestAction](), reflect.TypeFor[v2225.DropStackRequestAction](), reflect.TypeFor[v2225.DestroyStackRequestAction](), reflect.TypeFor[v2225.ConsumeStackRequestAction](), reflect.TypeFor[v2225.CreateStackRequestAction](), reflect.TypeFor[v2225.LabTableCombineStackRequestAction](), reflect.TypeFor[v2225.BeaconPaymentStackRequestAction](), reflect.TypeFor[v2225.MineBlockStackRequestAction](), reflect.TypeFor[v2225.CraftRecipeStackRequestAction](), reflect.TypeFor[v2225.AutoCraftRecipeStackRequestAction](), reflect.TypeFor[v2225.CraftCreativeStackRequestAction](), reflect.TypeFor[v2225.CraftRecipeOptionalStackRequestAction](), reflect.TypeFor[v2225.CraftGrindstoneRecipeStackRequestAction](), reflect.TypeFor[v2225.CraftLoomRecipeStackRequestAction](), reflect.TypeFor[v2225.CraftNonImplementedStackRequestAction](), reflect.TypeFor[v2225.CraftResultsDeprecatedStackRequestAction](), reflect.TypeFor[v2225.CraftReservedStackRequestAction]()},
	reflect.TypeFor[v2193.InventoryTransactionData](): {reflect.TypeFor[v2193.NormalTransactionData](), reflect.TypeFor[v2193.MismatchTransactionData](), reflect.TypeFor[v2193.UseItemTransactionData](), reflect.TypeFor[v2193.UseItemOnEntityTransactionData](), reflect.TypeFor[v2193.ReleaseItemTransactionData]()},
	reflect.TypeFor[v2225.InventoryTransactionData](): {reflect.TypeFor[v2225.NormalTransactionData](), reflect.TypeFor[v2225.MismatchTransactionData](), reflect.TypeFor[v2225.UseItemTransactionData](), reflect.TypeFor[v2225.UseItemOnEntityTransactionData](), reflect.TypeFor[v2225.ReleaseItemTransactionData]()},
}

// kinds holds the kinds decodedKinds found of each union.
var kinds map[reflect.Type][]reflect.Type

// decodedKinds returns the kinds of the unions packets hold that they
// decode from short bytes of each first byte and kind: the codecs choose
// the kind of a union by a number before its value.
func decodedKinds() map[reflect.Type][]reflect.Type {
	if kinds != nil {
		return kinds
	}
	kinds = map[reflect.Type][]reflect.Type{}
	for _, c := range []codec{from, to} {
		for id := range uint32(1024) {
			if c.new(id) == nil {
				continue
			}
			for kind := range 64 {
				for _, prefix := range [][]byte{{}, {0}, {0, 0}} {
					b := append(append(slices.Clone(prefix), byte(kind<<1), byte(kind)), make([]byte, 32)...)
					if pk, _ := decode(c, id, b); pk != nil {
						collect(reflect.ValueOf(pk).Elem(), 0)
					}
				}
			}
		}
	}
	return kinds
}

// collect adds the kinds of the unions a value holds to kinds.
func collect(v reflect.Value, depth int) {
	if depth > 4 || !v.IsValid() {
		return
	}
	switch v.Kind() {
	case reflect.Interface:
		if v.IsNil() {
			return
		}
		if e := v.Elem(); e.Kind() == reflect.Pointer && v.Type().NumMethod() > 0 && !slices.Contains(kinds[v.Type()], e.Type().Elem()) {
			kinds[v.Type()] = append(kinds[v.Type()], e.Type().Elem())
		}
		collect(v.Elem(), depth+1)
	case reflect.Pointer:
		if !v.IsNil() {
			collect(v.Elem(), depth+1)
		}
	case reflect.Struct:
		for i := range v.NumField() {
			if v.Type().Field(i).IsExported() {
				collect(v.Field(i), depth+1)
			}
		}
	case reflect.Slice, reflect.Array:
		for i := range min(v.Len(), 4) {
			collect(v.Index(i), depth+1)
		}
	}
}

// encode encodes a packet, false if its codec refuses its contents.
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

// decode decodes the fields of a packet of an ID with a codec, false if
// they do not decode or bytes are left.
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

// agree checks that bytes of a packet of an ID that decode with one codec
// decode with the other and encode back as the first encodes them. It
// reports whether the bytes decoded with the first.
func agree(t *testing.T, id uint32, b []byte, a, other codec) bool {
	t.Helper()
	pa, ok := decode(a, id, b)
	if !ok {
		return false
	}
	pb, ok := decode(other, id, b)
	if !ok {
		t.Errorf("packet %d: bytes % x decode with %s, not with %s", id, head(b), a.name, other.name)
		return true
	}
	ea, okA := encode(pa)
	eb, okB := encode(pb)
	if okA != okB || !bytes.Equal(ea, eb) {
		t.Errorf("packet %d: % x encodes back as % x with %s and % x with %s", id, head(b), head(ea), a.name, head(eb), other.name)
	}
	return true
}

func head(b []byte) []byte {
	if len(b) > 48 {
		return b[:48]
	}
	return b
}

// samples returns bytes of packets of an ID: those of its empty packet and
// of random packets of both codecs, with some cut short or with a byte
// changed.
func samples(rng *rand.Rand, id uint32) [][]byte {
	var out [][]byte
	for _, c := range []codec{from, to} {
		if b, ok := encode(c.new(id)); ok {
			out = append(out, b)
		}
		for range 40 {
			pk := c.new(id)
			fill(rng, reflect.ValueOf(pk).Elem(), 0)
			if b, ok := encode(pk); ok {
				out = append(out, b)
			}
		}
	}
	for _, b := range slices.Clone(out) {
		if len(b) > 1 {
			out = append(out, b[:rng.IntN(len(b))])
			changed := slices.Clone(b)
			changed[rng.IntN(len(changed))] ^= byte(1 + rng.IntN(255))
			out = append(out, changed)
		}
	}
	return out
}

// TestSameLayoutsAgree proves Same for every ID it holds for: what either
// codec encodes of random packets, or of their bytes cut or changed,
// decodes with both or neither, and encodes back the same with both.
func TestSameLayoutsAgree(t *testing.T) {
	rng := rand.New(rand.NewPCG(2193, 2225))
	var same, decoded int
	for id := range uint32(1024) {
		if !Same(id) {
			continue
		}
		same++
		if from.new(id) == nil || to.new(id) == nil {
			t.Fatalf("Same(%d) of a packet one protocol has not", id)
		}
		n := 0
		for _, b := range samples(rng, id) {
			if agree(t, id, b, from, to) {
				n++
			}
			agree(t, id, b, to, from)
		}
		if n == 0 {
			t.Errorf("packet %d: no sample decoded", id)
		}
		decoded += n
	}
	if same < 200 {
		t.Fatalf("Same holds for %d packets, want the 219 of the 236 of 2193 that did not change", same)
	}
	t.Logf("%d packets of the same layout, %d samples decoded", same, decoded)
}

// TestPacketLists checks the lists of the IDs of the packets that changed,
// that 2225 added and that 2193 removed against Same and the codecs.
func TestPacketLists(t *testing.T) {
	for id := range uint32(1024) {
		f, n := from.new(id) != nil, to.new(id) != nil
		switch {
		case slices.Contains(changed, id):
			if !f || !n || Same(id) {
				t.Errorf("packet %d is listed as changed", id)
			}
		case slices.Contains(added, id):
			if f || !n {
				t.Errorf("packet %d is listed as added", id)
			}
		case slices.Contains(removed, id):
			if !f || n {
				t.Errorf("packet %d is listed as removed", id)
			}
		case f != n || f && !Same(id):
			t.Errorf("packet %d is in no list", id)
		}
	}
	want := []uint32{11, 12, 13, 30, 44, 58, 63, 90, 93, 137, 144, 147, 164, 180, 315, 345, 348}
	if !slices.Equal(changed, want) {
		t.Errorf("changed %v, want %v: the 19 packets of the design and PlayerAuthInput, which holds a stack request, but PlayerVideoCapture, CameraInstruction and CameraSpline, which v2193 coded wrongly", changed, want)
	}
}

// FuzzSameLayoutsAgree is TestSameLayoutsAgree of hostile bytes.
func FuzzSameLayoutsAgree(f *testing.F) {
	rng := rand.New(rand.NewPCG(1, 2))
	for _, id := range []uint32{1, 9, 39, 40, 49, 50, 111, 148} {
		for _, b := range samples(rng, id)[:4] {
			f.Add(uint16(id), b)
		}
	}
	f.Fuzz(func(t *testing.T, id uint16, b []byte) {
		if !Same(uint32(id)) {
			return
		}
		agree(t, uint32(id), b, from, to)
		agree(t, uint32(id), b, to, from)
	})
}

// TestChangedLayoutsDisagree is the control of TestSameLayoutsAgree: the
// samples of a packet whose layout changed show it, decoding with one
// codec and not the other or encoding back otherwise, for all but the
// changes random contents rarely reach.
func TestChangedLayoutsDisagree(t *testing.T) {
	rng := rand.New(rand.NewPCG(2193, 2225))
	var quiet []uint32
	for _, id := range changed {
		shown := false
		for _, b := range samples(rng, id) {
			for _, c := range [][2]codec{{from, to}, {to, from}} {
				pa, ok := decode(c[0], id, b)
				if !ok {
					continue
				}
				pb, ok := decode(c[1], id, b)
				ea, _ := encode(pa)
				eb, _ := encode(pb)
				if !ok || !bytes.Equal(ea, eb) {
					shown = true
				}
			}
		}
		if !shown {
			quiet = append(quiet, id)
		}
	}
	if len(quiet) > 2 {
		t.Errorf("the samples of packets %v that changed show no change", quiet)
	}
	t.Logf("changes no sample shows: %v", quiet)
}
