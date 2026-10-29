package nbt

import (
	"bytes"
	"errors"
	"io"
	"runtime"
	"testing"
)

// stream hides the length of the data from the decoder, as a connection
// does.
type stream struct{ r io.Reader }

func (s stream) Read(p []byte) (int, error) { return s.r.Read(p) }

var encodings = []Encoding{LittleEndian, NetworkLittleEndian, BigEndian, NetworkBigEndian}

// decodeAll decodes data with every encoding into every kind of value,
// from data of a known length and from a stream.
func decodeAll(data []byte) {
	type sample struct {
		Name  string  `nbt:"name"`
		Ints  []int32 `nbt:"ints"`
		Bytes [4]byte `nbt:"bytes"`
		List  []any   `nbt:"list"`
	}
	for _, e := range encodings {
		var a any
		var m map[string]any
		var s sample
		_ = UnmarshalEncoding(data, &a, e)
		_ = UnmarshalEncoding(data, &m, e)
		_ = UnmarshalEncoding(data, &s, e)
		_ = NewDecoderWithEncoding(stream{bytes.NewReader(data)}, e).Decode(&a)
	}
}

// root writes the start of a root compound, which has no name in the
// network format of Java.
func root(w *offsetWriter, e Encoding) {
	_ = w.WriteByte(byte(tagStruct))
	if e != NetworkBigEndian {
		_ = e.WriteString(w, "")
	}
}

// allocated returns the bytes f allocates.
func allocated(f func()) uint64 {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	f()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// TestLengthsPastTheData decodes arrays and lists whose lengths no data
// follows: the decoder in gophertunnel overflowed n*4 into a negative
// length and panicked, and allocated what each length asked for.
func TestLengthsPastTheData(t *testing.T) {
	// The input fuzzing found: a list of ints of 0x30303030.
	decodeAll([]byte("\n\x00\x00\t\x01\x000\x030000"))
	for _, e := range encodings {
		for _, tag := range []byte{byte(tagByteArray), byte(tagInt32Array), byte(tagInt64Array), byte(tagSlice)} {
			var w bytes.Buffer
			ow := &offsetWriter{Writer: &w, WriteByte: w.WriteByte}
			root(ow, e)
			_ = ow.WriteByte(tag)
			_ = e.WriteString(ow, "x")
			if tag == byte(tagSlice) {
				_ = ow.WriteByte(byte(tagStruct))
			}
			_ = e.WriteInt32(ow, 0x7fffffff)
			data := append(w.Bytes(), make([]byte, 1000)...)
			if n := allocated(func() {
				var a any
				if err := UnmarshalEncoding(data, &a, e); err == nil {
					t.Errorf("tag %v of %T decoded", tag, e)
				}
				if err := NewDecoderWithEncoding(stream{bytes.NewReader(data)}, e).Decode(&a); err == nil {
					t.Errorf("tag %v of %T decoded from a stream", tag, e)
				}
			}); n > 4<<20 {
				t.Errorf("tag %v of %T: %d bytes allocated for 1000 bytes of data", tag, e, n)
			}
		}
	}
}

// TestListsNestedPastTheBound nests lists far deeper than the bound,
// which no tag read checked: the decoder recursed until the stack of the
// goroutine ran out.
func TestListsNestedPastTheBound(t *testing.T) {
	for _, e := range encodings {
		var w bytes.Buffer
		ow := &offsetWriter{Writer: &w, WriteByte: w.WriteByte}
		root(ow, e)
		_ = ow.WriteByte(byte(tagSlice))
		_ = e.WriteString(ow, "x")
		for range 1 << 20 {
			_ = ow.WriteByte(byte(tagSlice))
			_ = e.WriteInt32(ow, 1)
		}
		var a any
		if err := UnmarshalEncoding(w.Bytes(), &a, e); !errors.As(err, new(MaximumDepthReachedError)) {
			t.Fatalf("%T: %v, want the depth bound", e, err)
		}
	}
}

// TestManyListsOfNumbers decodes a compound of more lists of numbers than
// the bound of nesting: each left the depth one deeper in gophertunnel,
// which refused the 512th.
func TestManyListsOfNumbers(t *testing.T) {
	m := map[string]any{}
	for i := range 2000 {
		m[string(rune('a'+i%26))+string(rune(i))] = []int32{int32(i), 1}
	}
	for _, e := range encodings {
		b, err := MarshalEncoding(m, e)
		if err != nil {
			t.Fatal(err)
		}
		var back map[string]any
		if err := UnmarshalEncoding(b, &back, e); err != nil {
			t.Fatalf("%T: %v", e, err)
		}
		if len(back) != len(m) {
			t.Fatalf("%T: %d tags, want %d", e, len(back), len(m))
		}
	}
}

// TestValuesThatCannotHoldTheData decodes into values of types that
// cannot hold what the data has, which panicked in reflect: an error is
// returned instead.
func TestValuesThatCannotHoldTheData(t *testing.T) {
	type myInt int32
	type myByte byte
	type named string
	type Inner int32
	data, err := MarshalEncoding(map[string]any{"ints": []int32{1, 2}, "bytes": [2]byte{1, 2}, "m": map[string]any{"a": int32(1)}, "inner": int32(3)}, LittleEndian)
	if err != nil {
		t.Fatal(err)
	}
	var ints struct {
		Ints []myInt `nbt:"ints"`
	}
	var arr struct {
		Bytes [2]myByte `nbt:"bytes"`
	}
	var keys struct {
		M map[int]int32 `nbt:"m"`
	}
	var namedKeys struct {
		M map[named]int32 `nbt:"m"`
	}
	for _, v := range []any{&ints, &arr, &keys, &namedKeys} {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("%T: panic %v", v, r)
				}
			}()
			if err := UnmarshalEncoding(data, v, LittleEndian); err == nil {
				t.Fatalf("%T decoded", v)
			}
		}()
	}
	// A field of a type that is no struct embedded goes by its name.
	var embedded struct {
		Inner `nbt:"inner"`
		Ints  []int32        `nbt:"ints"`
		Bytes [2]byte        `nbt:"bytes"`
		M     map[string]any `nbt:"m"`
	}
	if err := UnmarshalEncoding(data, &embedded, LittleEndian); err != nil {
		t.Fatal(err)
	}
}

func FuzzDecode(f *testing.F) {
	for _, v := range []any{
		map[string]any{"a": []int32{1}, "b": [2]int64{1, 2}, "c": [3]byte{}, "d": []any{map[string]any{"e": "f"}}, "g": []any{[]any{int16(1)}}},
		map[string]any{"x": float32(1), "y": float64(2), "z": int64(3), "w": byte(4)},
	} {
		for _, e := range encodings {
			b, err := MarshalEncoding(v, e)
			if err != nil {
				f.Fatal(err)
			}
			f.Add(b)
		}
	}
	f.Fuzz(func(t *testing.T, data []byte) { decodeAll(data) })
}
