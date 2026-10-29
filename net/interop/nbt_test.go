package interop

import (
	"reflect"
	"testing"

	"github.com/dm-vev/adamant/net/bedrock/nbt"
	"github.com/dm-vev/adamant/net/bedrock/wire"
	gtnbt "github.com/sandertv/gophertunnel/minecraft/nbt"
)

// nbtValue holds a value of each tag as a server builds it for
// gophertunnel. Int and long arrays are left out: gophertunnel writes their
// elements as fixed-width ints, where the game and other servers write
// varints.
func nbtValue() map[string]any {
	return map[string]any{
		"Name":   "minecraft:chest",
		"Count":  uint8(5),
		"Damage": int16(-2),
		"x":      int32(-123456),
		"time":   int64(-1) << 45,
		"f":      float32(0.25),
		"d":      1e-9,
		"data":   [3]byte{9, 8, 7},
		"bytes":  []byte{1, 2},
		"ints":   []int32{-1, 7},
		"longs":  []int64{1 << 40},
		"list":   []any{map[string]any{"Slot": uint8(1)}, map[string]any{"Slot": uint8(2)}},
		"names":  []any{"a", "b"},
		"tag":    map[string]any{"display": map[string]any{"Name": "§aSword"}},
	}
}

// TestNBTMatchesGophertunnel decodes what each side encodes: both read the
// same Go values back.
func TestNBTMatchesGophertunnel(t *testing.T) {
	theirs, err := gtnbt.MarshalEncoding(nbtValue(), gtnbt.NetworkLittleEndian)
	if err != nil {
		t.Fatal(err)
	}
	r := wire.NewReader(theirs)
	got, err := nbt.Read(r)
	if err != nil || r.Len() != 0 {
		t.Fatalf("read gophertunnel's NBT: %v, %d bytes left", err, r.Len())
	}
	if !reflect.DeepEqual(got, nbtValue()) {
		t.Fatalf("read %#v", got)
	}

	var w wire.Writer
	if err := nbt.Write(&w, nbtValue()); err != nil {
		t.Fatal(err)
	}
	var back map[string]any
	if err := gtnbt.UnmarshalEncoding(w.B, &back, gtnbt.NetworkLittleEndian); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back, nbtValue()) {
		t.Fatalf("gophertunnel read %#v", back)
	}
}
