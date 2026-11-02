package nbt

import (
	"bytes"
	"errors"
	"fmt"
	"math/rand/v2"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/dm-vev/adamant/net/bedrock/wire"
)

// sample is a compound of every tag, as Read gives it.
func sample() map[string]any {
	return map[string]any{
		"byte":   uint8(253),
		"short":  int16(300),
		"int":    int32(-70000),
		"long":   int64(1) << 40,
		"float":  float32(1.5),
		"double": 2.25,
		"bytes":  [3]byte{1, 2, 3},
		"name":   "minecraft:chest",
		"list":   []int32{1, 2},
		"names":  []any{"a", "b"},
		"nested": map[string]any{"Items": []any{map[string]any{"Count": uint8(1)}}},
		"ints":   [2]int32{-1, 5},
		"longs":  [1]int64{1 << 50},
		"empty":  []any{},
	}
}

type facing int32

// TestGophertunnelValues writes values as a server builds them for
// gophertunnel: typed slices go as lists, named types by their kind, bool
// and int8 as bytes, maps of any value type as compounds.
func TestGophertunnelValues(t *testing.T) {
	in := map[string]any{
		"ints":   []int32{1, 2},
		"items":  []map[string]any{{"Count": uint8(1)}},
		"names":  []string{"a", "b"},
		"facing": facing(3),
		"flag":   true,
		"signed": int8(-1),
		"tags":   map[string]string{"k": "v"},
		"bytes":  []byte{7},
	}
	want := map[string]any{
		"ints":   []int32{1, 2},
		"items":  []any{map[string]any{"Count": uint8(1)}},
		"names":  []any{"a", "b"},
		"facing": int32(3),
		"flag":   uint8(1),
		"signed": uint8(255),
		"tags":   map[string]any{"k": "v"},
		"bytes":  []byte{7},
	}
	var w wire.Writer
	if err := Write(&w, in); err != nil {
		t.Fatal(err)
	}
	got, err := Read(wire.NewReader(w.B))
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("read back %#v, %v", got, err)
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

// TestLittleEndian checks the form of worlds and item data: fixed-width
// ints and an int16 string length.
func TestLittleEndian(t *testing.T) {
	var w wire.Writer
	if err := WriteEncoded(&w, map[string]any{"a": int32(-1)}, LittleEndian); err != nil {
		t.Fatal(err)
	}
	want := []byte{tagCompound, 0, 0, tagInt, 1, 0, 'a', 0xff, 0xff, 0xff, 0xff, tagEnd}
	if !bytes.Equal(w.B, want) {
		t.Fatalf("% x, want % x", w.B, want)
	}
	w.B = w.B[:0]
	if err := WriteEncoded(&w, sample(), LittleEndian); err != nil {
		t.Fatal(err)
	}
	got, err := ReadEncoded(wire.NewReader(w.B), LittleEndian)
	if err != nil || !reflect.DeepEqual(got, sample()) {
		t.Fatalf("read back %#v, %v", got, err)
	}
}

// TestArrayBound reads an int array of a length past maxArray: refused, as
// each length would make a Go type kept for good.
func TestArrayBound(t *testing.T) {
	var w wire.Writer
	w.Uint8(tagCompound)
	Network.writeString(&w, "")
	w.Uint8(tagIntArray)
	Network.writeString(&w, "a")
	Network.writeInt(&w, maxArray+1)
	for range maxArray + 1 {
		w.Uint8(0)
	}
	w.Uint8(tagEnd)
	if _, err := Read(wire.NewReader(w.B)); !errors.Is(err, wire.ErrTooLong) {
		t.Fatalf("read an array of %d: %v", maxArray+1, err)
	}
}

// arrays returns network NBT of a compound of arrays of a tag, of zeros, of
// the lengths from start to end, keyed by length in order.
func arrays(t byte, start, end int) []byte {
	var w wire.Writer
	w.Uint8(tagCompound)
	_ = Network.writeString(&w, "")
	for n := start; n < end; n++ {
		w.Uint8(t)
		_ = Network.writeString(&w, fmt.Sprintf("%05d", n))
		Network.writeInt(&w, int32(n))
		w.Bytes(make([]byte, n))
	}
	w.Uint8(tagEnd)
	return w.B
}

// TestArrayTypes reads arrays of many lengths. Each length of a Go array
// makes a type kept for good, 104 MiB of them for every length a peer may
// send, so past 256 elements an array reads into a slice of a type of its
// tag; shorter ones read into Go arrays, as callers take them. All write
// back as arrays.
func TestArrayTypes(t *testing.T) {
	for _, tag := range []byte{tagByteArray, tagIntArray, tagLongArray} {
		// Lengths no run of the process has read yet, whose types a
		// reflect.ArrayOf would make anew.
		start := 300 + rand.IntN(4096)
		data := arrays(tag, start, start+512)
		goArrays := 0
		_, n := held(func() any {
			m, err := Read(wire.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			for _, v := range m {
				if reflect.TypeOf(v).Kind() == reflect.Array {
					goArrays++
				}
			}
			return nil
		})
		if goArrays > 0 || n > 64<<10 {
			t.Errorf("tag %d: read 512 lengths into %d Go arrays, keeping %d bytes", tag, goArrays, n)
		}
		m, err := Read(wire.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		var w wire.Writer
		if err := Write(&w, m); err != nil || !bytes.Equal(w.B, data) {
			t.Fatalf("tag %d: arrays write back otherwise: %v", tag, err)
		}
	}
	// Fireworks read their colours as arrays of one byte or none.
	short := map[string]any{"none": [0]byte{}, "one": [1]byte{3}, "ints": [4]int32{1, 2, 3, 4}, "longs": [256]int64{}}
	var w wire.Writer
	if err := Write(&w, short); err != nil {
		t.Fatal(err)
	}
	if m, err := Read(wire.NewReader(w.B)); err != nil || !reflect.DeepEqual(m, short) {
		t.Fatalf("read short arrays back as %#v, %v", m, err)
	}
}

// root returns network NBT of a root compound of one tag "a", whose payload
// body writes.
func root(t byte, body func(w *wire.Writer)) []byte {
	var w wire.Writer
	w.Uint8(tagCompound)
	_ = Network.writeString(&w, "")
	w.Uint8(t)
	_ = Network.writeString(&w, "a")
	body(&w)
	w.Uint8(tagEnd)
	return w.B
}

// listOf writes a list of n elements of a type, each the bytes of elem.
func listOf(w *wire.Writer, t byte, n int, elem []byte) {
	w.Uint8(t)
	Network.writeInt(w, int32(n))
	for range n {
		w.Bytes(elem)
	}
}

// lists returns NBT of a list of n lists of 1024 elements of a type.
func lists(n int, t byte, elem []byte) []byte {
	return root(tagList, func(w *wire.Writer) {
		w.Uint8(tagList)
		Network.writeInt(w, int32(n))
		for range n {
			listOf(w, t, 1024, elem)
		}
	})
}

// held returns the bytes of memory what read returns holds.
func held(read func() any) (any, uint64) {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	v := read()
	runtime.GC()
	runtime.ReadMemStats(&after)
	runtime.KeepAlive(v)
	if after.HeapAlloc < before.HeapAlloc {
		return v, 0
	}
	return v, after.HeapAlloc - before.HeapAlloc
}

// TestAllocBound reads NBT that takes far more memory than its bytes: a
// byte of an empty compound takes a map of 48 bytes and a slot of 16 in
// its list. Within the MaxAlloc of the Reader it reads; past it it fails.
func TestAllocBound(t *testing.T) {
	data := lists(64, tagCompound, []byte{tagEnd})
	r := wire.NewReader(data)
	r.MaxAlloc = 1 << 20
	_, n := held(func() any {
		m, err := Read(r)
		if !errors.Is(err, wire.ErrAlloc) {
			t.Errorf("read %d bytes of empty compounds over a bound of %d: %v", len(data), r.MaxAlloc, err)
		}
		return m
	})
	if n > 2<<20 {
		t.Errorf("read %d bytes into %d held", len(data), n)
	}
	r = wire.NewReader(data)
	r.MaxAlloc = 16 << 20
	m, err := Read(r)
	if err != nil || len(m["a"].([]any)) != 64 {
		t.Fatalf("read within the bound: %v", err)
	}
}

// TestAllocCharged reads NBT of shapes a peer may send for the most memory
// a byte: the memory each holds must be about what is charged for it, so
// that a bound of half of it fails and a bound of thrice it does not.
func TestAllocCharged(t *testing.T) {
	keys := root(tagCompound, func(w *wire.Writer) {
		for i := range 1 << 15 {
			w.Uint8(tagCompound)
			_ = Network.writeString(w, string([]byte{byte(i), byte(i >> 8), 'k'}))
			w.Uint8(tagEnd)
		}
		w.Uint8(tagEnd)
	})
	// An array of 300 zeros, longer than a Go array is made for.
	var array wire.Writer
	Network.writeInt(&array, 300)
	array.Bytes(make([]byte, 300))
	for name, data := range map[string][]byte{
		"int arrays":        lists(2, tagIntArray, array.B),
		"long arrays":       lists(1, tagLongArray, array.B),
		"empty compounds":   lists(64, tagCompound, []byte{tagEnd}),
		"compounds of ints": lists(8, tagCompound, []byte{tagInt, 1, 'i', 0x80, 0x08, tagEnd}),
		"distinct keys":     keys,
		"empty lists":       lists(64, tagList, []byte{tagEnd, 0}),
		"empty strings":     lists(128, tagString, []byte{0}),
		"strings":           lists(64, tagString, []byte{2, 'a', 'b'}),
		"longs":             lists(256, tagLong, []byte{0}),
		"doubles":           lists(32, tagDouble, make([]byte, 8)),
		"floats":            lists(32, tagFloat, []byte{0, 0, 0x80, 0x3f}),
	} {
		_, n := held(func() any {
			m, err := Read(wire.NewReader(data))
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			return m
		})
		r := wire.NewReader(data)
		r.MaxAlloc = int(n / 2)
		if _, err := Read(r); !errors.Is(err, wire.ErrAlloc) {
			t.Errorf("%s: %d bytes held, read within a bound of half of them: %v", name, n, err)
		}
		r = wire.NewReader(data)
		r.MaxAlloc = int(n * 3)
		if _, err := Read(r); err != nil {
			t.Errorf("%s: %d bytes held, read failed within a bound of thrice them: %v", name, n, err)
		}
	}
}

// TestListBound reads NBT lists past the MaxList of the Reader, as a
// server bounds what clients send: they fail as other lists do, but for a
// list of bytes, which holds a byte a byte.
func TestListBound(t *testing.T) {
	for _, et := range []byte{tagInt, tagLong, tagString, tagCompound} {
		data := root(tagList, func(w *wire.Writer) { listOf(w, et, 1025, []byte{0}) })
		r := wire.NewReader(data)
		r.MaxList = 1024
		if _, err := Read(r); !errors.Is(err, wire.ErrTooLarge) {
			t.Errorf("list of 1025 of tag %d: %v", et, err)
		}
		r = wire.NewReader(data)
		if _, err := Read(r); err != nil {
			t.Errorf("list of 1025 of tag %d without a bound: %v", et, err)
		}
	}
	r := wire.NewReader(root(tagList, func(w *wire.Writer) { listOf(w, tagByte, 1025, []byte{7}) }))
	r.MaxList = 1024
	if m, err := Read(r); err != nil || len(m["a"].([]byte)) != 1025 {
		t.Fatalf("list of 1025 bytes: %v", err)
	}
}

// TestLongStrings writes strings as long as the game reads and longer: a
// longer one fails rather than going with a length that wrapped around in
// the 16 bits little-endian NBT gives it, which corrupted what followed.
func TestLongStrings(t *testing.T) {
	long, longer := strings.Repeat("a", 32767), strings.Repeat("a", 32768)
	for _, e := range []Encoding{Network, LittleEndian} {
		var w wire.Writer
		if err := WriteEncoded(&w, map[string]any{"s": long, long: int32(1)}, e); err != nil {
			t.Fatalf("encoding %d: %v", e, err)
		}
		m, err := ReadEncoded(wire.NewReader(w.B), e)
		if err != nil || m["s"] != long || m[long] != int32(1) {
			t.Fatalf("encoding %d: read back %v", e, err)
		}
		for name, v := range map[string]map[string]any{
			"value":      {"s": longer},
			"key":        {longer: int32(1)},
			"in a list":  {"l": []any{longer}},
			"in strings": {"l": []string{longer}},
			"of 65536":   {"s": strings.Repeat("a", 65536)},
		} {
			var w wire.Writer
			if err := WriteEncoded(&w, v, e); err == nil {
				t.Errorf("encoding %d: a string %s past the bound was written", e, name)
			}
		}
	}
}
