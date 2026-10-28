package interop

import (
	"fmt"
	"maps"
	"reflect"
	"testing"

	"github.com/dm-vev/adamant/net/bedrock/nbt"
	"github.com/dm-vev/adamant/net/bedrock/wire"
	gtnbt "github.com/sandertv/gophertunnel/minecraft/nbt"
)

// normalise maps the values of both decoders to one form: gophertunnel
// reads a byte as uint8 and arrays as Go arrays.
func normalise(v any) any {
	switch v := v.(type) {
	case uint8:
		return int8(v)
	case map[string]any:
		m := make(map[string]any, len(v))
		for k, x := range v {
			m[k] = normalise(x)
		}
		return m
	case []any:
		l := make([]any, len(v))
		for i, x := range v {
			l[i] = normalise(x)
		}
		return l
	}
	if rv := reflect.ValueOf(v); rv.Kind() == reflect.Array && rv.Type().Elem().Kind() == reflect.Uint8 {
		b := make([]byte, rv.Len())
		reflect.Copy(reflect.ValueOf(b), rv)
		return b
	}
	return v
}

// TestNBTMatchesGophertunnel decodes what each side encodes. Int arrays
// are left out: gophertunnel writes them with fixed-width ints, where the
// game and other servers use varints.
func TestNBTMatchesGophertunnel(t *testing.T) {
	value := map[string]any{
		"Name":   "minecraft:chest",
		"Count":  uint8(5),
		"Damage": int16(-2),
		"x":      int32(-123456),
		"time":   int64(-1) << 45,
		"f":      float32(0.25),
		"d":      1e-9,
		"data":   []byte{9, 8, 7},
		"list":   []any{map[string]any{"Slot": uint8(1)}, map[string]any{"Slot": uint8(2)}},
		"names":  []any{"a", "b"},
		"tag":    map[string]any{"display": map[string]any{"Name": "§aSword"}},
	}
	// gophertunnel writes a byte array from a Go array; a slice becomes a list.
	gtValue := maps.Clone(value)
	gtValue["data"] = [3]byte{9, 8, 7}
	theirs, err := gtnbt.MarshalEncoding(gtValue, gtnbt.NetworkLittleEndian)
	if err != nil {
		t.Fatal(err)
	}
	r := wire.NewReader(theirs)
	got, err := nbt.Read(r)
	if err != nil || r.Len() != 0 {
		t.Fatalf("read gophertunnel's NBT: %v, %d bytes left", err, r.Len())
	}
	if !reflect.DeepEqual(normalise(got), normalise(value)) {
		t.Fatalf("read %#v", got)
	}

	var w wire.Writer
	if err := nbt.Write(&w, value); err != nil {
		t.Fatal(err)
	}
	var back map[string]any
	if err := gtnbt.UnmarshalEncoding(w.B, &back, gtnbt.NetworkLittleEndian); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(normalise(back), normalise(value)) {
		t.Fatalf("gophertunnel read %s", fmt.Sprint(back))
	}
}
