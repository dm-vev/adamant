package wire

import (
	"bytes"
	"errors"
	"math"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	var w Writer
	w.Bool(true)
	w.Uint8(200)
	w.Int8(-5)
	w.Uint16(65000)
	w.Int16(-30000)
	w.Uint32(4e9)
	w.Int32(math.MinInt32)
	w.Int32BE(2193)
	w.Uint64(math.MaxUint64)
	w.Int64(math.MinInt64)
	w.Float32(-1.5)
	w.Float64(math.Pi)
	w.Varuint32(math.MaxUint32)
	w.Varuint64(math.MaxUint64)
	w.Varint32(math.MinInt32)
	w.Varint32(-1)
	w.Varint64(math.MaxInt64)
	w.String("héllo")
	w.ByteSlice([]byte{1, 2, 3})
	w.ByteFloat(90)
	id := [16]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
	w.UUID(id)
	w.Bytes([]byte("rest"))

	r := NewReader(w.B)
	checks := []struct {
		got, want any
	}{
		{r.Bool(), true}, {r.Uint8(), uint8(200)}, {r.Int8(), int8(-5)}, {r.Uint16(), uint16(65000)},
		{r.Int16(), int16(-30000)}, {r.Uint32(), uint32(4e9)}, {r.Int32(), int32(math.MinInt32)},
		{r.Int32BE(), int32(2193)}, {r.Uint64(), uint64(math.MaxUint64)}, {r.Int64(), int64(math.MinInt64)},
		{r.Float32(), float32(-1.5)}, {r.Float64(), math.Pi}, {r.Varuint32(), uint32(math.MaxUint32)},
		{r.Varuint64(), uint64(math.MaxUint64)}, {r.Varint32(), int32(math.MinInt32)}, {r.Varint32(), int32(-1)},
		{r.Varint64(), int64(math.MaxInt64)}, {r.String(), "héllo"}, {string(r.ByteSlice()), "\x01\x02\x03"},
		{r.ByteFloat(), float32(90)}, {r.UUID(), id}, {string(r.Bytes()), "rest"},
	}
	for i, c := range checks {
		if c.got != c.want {
			t.Fatalf("field %d: %v, want %v", i, c.got, c.want)
		}
	}
	if r.Err() != nil || r.Len() != 0 {
		t.Fatalf("err %v, %d bytes left", r.Err(), r.Len())
	}
}

// TestVarintBytes checks the encodings against known bytes: the zigzag of
// -1 is 1, of 1 is 2, and 300 takes two bytes.
func TestVarintBytes(t *testing.T) {
	for _, tc := range []struct {
		write func(*Writer)
		want  []byte
	}{
		{func(w *Writer) { w.Varint32(-1) }, []byte{1}},
		{func(w *Writer) { w.Varint32(1) }, []byte{2}},
		{func(w *Writer) { w.Varuint32(300) }, []byte{0xac, 0x02}},
		{func(w *Writer) { w.Varuint32(math.MaxUint32) }, []byte{0xff, 0xff, 0xff, 0xff, 0x0f}},
		{func(w *Writer) { w.UUID([16]byte{15: 1}) }, []byte{0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0}},
	} {
		var w Writer
		tc.write(&w)
		if !bytes.Equal(w.B, tc.want) {
			t.Fatalf("% x, want % x", w.B, tc.want)
		}
	}
}

// TestByteFloatWraps writes angles out of a turn, which wrap around.
func TestByteFloatWraps(t *testing.T) {
	for v, want := range map[float32]byte{0: 0, 90: 64, 359: 255, 360: 0, -10: 249, -90: 192, 720 + 45: 32} {
		var w Writer
		w.ByteFloat(v)
		if w.B[0] != want {
			t.Errorf("%v degrees: byte %d, want %d", v, w.B[0], want)
		}
	}
}

func TestReaderErrors(t *testing.T) {
	for name, tc := range map[string]struct {
		data []byte
		read func(*Reader)
		want error
	}{
		"short int":         {[]byte{1, 2}, func(r *Reader) { r.Int32() }, ErrShort},
		"long varint":       {[]byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x01}, func(r *Reader) { r.Varuint32() }, ErrVarint},
		"varint over 32":    {[]byte{0xff, 0xff, 0xff, 0xff, 0x1f}, func(r *Reader) { r.Varuint32() }, ErrVarint},
		"string past end":   {[]byte{5, 'a'}, func(r *Reader) { _ = r.String() }, ErrShort},
		"string over limit": {[]byte{0x80, 0x80, 0x80, 0x01}, func(r *Reader) { _ = r.String() }, ErrTooLong},
		"count over max":    {[]byte{10, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, func(r *Reader) { r.Count(5) }, ErrTooLarge},
		"count past end":    {[]byte{10, 0}, func(r *Reader) { r.Count(100) }, ErrShort},
		"count over list":   {[]byte{3, 0, 0, 0}, func(r *Reader) { r.MaxList = 2; r.Count(5) }, ErrTooLarge},
		"list over bound":   {[]byte{0, 0, 0, 0}, func(r *Reader) { r.MaxList = 2; r.List(3) }, ErrTooLarge},
		"list past end":     {[]byte{0, 0}, func(r *Reader) { r.List(3) }, ErrShort},
	} {
		r := NewReader(tc.data)
		tc.read(r)
		if !errors.Is(r.Err(), tc.want) {
			t.Fatalf("%s: %v, want %v", name, r.Err(), tc.want)
		}
		// Reads after an error return zero values.
		if r.Uint8() != 0 || r.String() != "" {
			t.Fatalf("%s: read a value after the error", name)
		}
	}
}

func FuzzReader(f *testing.F) {
	f.Add([]byte{5, 'h', 'e', 'l', 'l', 'o', 0xac, 0x02})
	f.Fuzz(func(t *testing.T, b []byte) {
		r := NewReader(b)
		for r.Err() == nil && r.Len() > 0 {
			_ = r.String()
			_ = r.Varint64()
			_ = r.UUID()
			r.Count(1 << 10)
		}
	})
}

func BenchmarkWriteMovement(b *testing.B) {
	w := Writer{B: make([]byte, 0, 64)}
	for range b.N {
		w.B = w.B[:0]
		w.Varuint64(12345)
		w.Uint8(3)
		w.Float32(1)
		w.Float32(64)
		w.Float32(-3)
		w.ByteFloat(90)
		w.ByteFloat(45)
		w.ByteFloat(45)
	}
}
