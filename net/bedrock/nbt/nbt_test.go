package nbt

import (
	"bytes"
	"errors"
	"reflect"
	"testing"

	"github.com/dm-vev/adamant/net/bedrock/wire"
)

func sample() map[string]any {
	return map[string]any{
		"byte":   int8(-3),
		"short":  int16(300),
		"int":    int32(-70000),
		"long":   int64(1) << 40,
		"float":  float32(1.5),
		"double": 2.25,
		"bytes":  []byte{1, 2, 3},
		"name":   "minecraft:chest",
		"list":   []any{int32(1), int32(2)},
		"nested": map[string]any{"Items": []any{map[string]any{"Count": int8(1)}}},
		"ints":   []int32{-1, 5},
		"longs":  []int64{1 << 50},
		"empty":  []any{},
	}
}

func TestRoundTrip(t *testing.T) {
	var w wire.Writer
	if err := Write(&w, sample()); err != nil {
		t.Fatal(err)
	}
	r := wire.NewReader(w.B)
	got, err := Read(r)
	if err != nil || r.Len() != 0 {
		t.Fatalf("read: %v, %d bytes left", err, r.Len())
	}
	if !reflect.DeepEqual(got, sample()) {
		t.Fatalf("read back %#v", got)
	}
	// Keys go in order: the same value always gives the same bytes.
	var again wire.Writer
	_ = Write(&again, sample())
	if !bytes.Equal(w.B, again.B) {
		t.Fatal("encoding is not deterministic")
	}
}

// TestKnownBytes checks a small compound against the bytes of the format:
// zigzag varint ints, varuint string lengths.
func TestKnownBytes(t *testing.T) {
	var w wire.Writer
	_ = Write(&w, map[string]any{"a": int32(-1), "b": "xy"})
	want := []byte{
		tagCompound, 0, // root, empty name
		tagInt, 1, 'a', 1, // -1 zigzags to 1
		tagString, 1, 'b', 2, 'x', 'y',
		tagEnd,
	}
	if !bytes.Equal(w.B, want) {
		t.Fatalf("% x, want % x", w.B, want)
	}
}

func TestLimits(t *testing.T) {
	deep := map[string]any{}
	cur := deep
	for range maxDepth + 1 {
		next := map[string]any{}
		cur["x"] = next
		cur = next
	}
	var w wire.Writer
	if err := Write(&w, deep); !errors.Is(err, errDepth) {
		t.Fatalf("write too deep: %v", err)
	}
	// The bytes of a compound nested past the limit.
	var b []byte
	b = append(b, tagCompound, 0)
	for range maxDepth + 1 {
		b = append(b, tagCompound, 1, 'x')
	}
	if _, err := Read(wire.NewReader(b)); !errors.Is(err, errDepth) {
		t.Fatalf("read too deep: %v", err)
	}
	for name, data := range map[string][]byte{
		"array past end":  {tagCompound, 0, tagByteArray, 1, 'a', 0x80, 0x80, 0x04},
		"negative length": {tagCompound, 0, tagList, 1, 'a', tagInt, 1},
		"long string":     {tagCompound, 0, tagString, 0xff, 0xff, 0x03},
		"unknown tag":     {tagCompound, 0, 99, 1, 'a'},
		"list of ends":    {tagCompound, 0, tagList, 1, 'a', tagEnd, 2},
	} {
		if _, err := Read(wire.NewReader(data)); err == nil {
			t.Fatalf("%s: read without an error", name)
		}
	}
	if err := Write(&w, map[string]any{"x": struct{}{}}); err == nil {
		t.Fatal("wrote a value of no tag")
	}
	if err := Write(&w, map[string]any{"x": []any{int32(1), "two"}}); !errors.Is(err, errListType) {
		t.Fatalf("list of mixed types: %v", err)
	}
}

func FuzzRead(f *testing.F) {
	var w wire.Writer
	_ = Write(&w, sample())
	f.Add(w.B)
	f.Fuzz(func(t *testing.T, b []byte) {
		v, err := Read(wire.NewReader(b))
		if err != nil {
			return
		}
		var out wire.Writer
		if err := Write(&out, v); err != nil {
			t.Fatalf("value read does not write back: %v", err)
		}
	})
}
