package bedrock

import (
	"bytes"
	"compress/flate"
	"io"
	"math/rand/v2"
	"testing"

	"github.com/dm-vev/adamant/net/bedrock/wire"
)

func inflate(t testing.TB, b []byte) []byte {
	t.Helper()
	out, err := io.ReadAll(flate.NewReader(bytes.NewReader(b)))
	if err != nil {
		t.Fatalf("inflate: %v", err)
	}
	return out
}

// deflateInputs are inputs that take every path of fixedDeflate: no data,
// literals of both code lengths, matches of every length and of distances
// up to the window, and data that does not match.
func deflateInputs() [][]byte {
	rng := rand.New(rand.NewPCG(3, 4))
	random := func(n int) []byte {
		b := make([]byte, n)
		for i := range b {
			b[i] = byte(rng.Uint32())
		}
		return b
	}
	all := make([]byte, 256)
	for i := range all {
		all[i] = byte(i)
	}
	inputs := [][]byte{nil, {0}, {255}, all, bytes.Repeat([]byte{7}, 1000), bytes.Repeat([]byte("abcd"), 300), random(100), random(70000)}
	// Every match length, at distances of every code.
	for n := 3; n <= 300; n++ {
		chunk := random(n)
		for _, gap := range []int{0, 1, 5, 100, 1000, 20000, window - n} {
			inputs = append(inputs, append(append(append([]byte(nil), chunk...), random(gap)...), chunk...))
		}
	}
	// Past the window: no match may reach back that far.
	far := random(300)
	inputs = append(inputs, append(append(append([]byte(nil), far...), random(window)...), far...))
	return inputs
}

func TestFixedDeflate(t *testing.T) {
	for i, in := range deflateInputs() {
		out := fixedDeflate([]byte{0xaa}, in)
		if out[0] != 0xaa {
			t.Fatalf("input %d: prefix overwritten", i)
		}
		if got := inflate(t, out[1:]); !bytes.Equal(got, in) {
			t.Fatalf("input %d (%d bytes): inflated to %d different bytes", i, len(in), len(got))
		}
	}
}

// TestFixedDeflateMatches checks that repeated data shrinks: the matches
// are found and coded, not only literals.
func TestFixedDeflateMatches(t *testing.T) {
	in := bytes.Repeat([]byte("move actor absolute "), 100)
	if out := fixedDeflate(nil, in); len(out) > len(in)/20 {
		t.Fatalf("%d bytes deflated to %d", len(in), len(out))
	}
}

func FuzzFixedDeflate(f *testing.F) {
	f.Add([]byte("hello hello hello"))
	f.Fuzz(func(t *testing.T, in []byte) {
		if got := inflate(t, fixedDeflate(nil, in)); !bytes.Equal(got, in) {
			t.Fatalf("inflated to different bytes")
		}
	})
}

// tickBatch returns a body like those a server sends every tick to a
// player near mobs: their movement and motion, with some block updates
// and level events.
func tickBatch(rng *rand.Rand) []byte {
	var body []byte
	packet := func(id uint32, f func(w *wire.Writer)) {
		var w wire.Writer
		w.Varuint32(id)
		f(&w)
		body = AppendPacket(body, w.B)
	}
	for i := range 47 {
		x, y, z := float32(rng.IntN(100)-50)+rng.Float32(), 64+rng.Float32()*4, float32(rng.IntN(100)-50)+rng.Float32()
		packet(18, func(w *wire.Writer) {
			w.Varuint64(uint64(100 + i))
			w.Uint8(1)
			w.Float32(x)
			w.Float32(y)
			w.Float32(z)
			w.ByteFloat(rng.Float32() * 360)
			w.ByteFloat(rng.Float32() * 360)
			w.ByteFloat(rng.Float32() * 360)
		})
		packet(40, func(w *wire.Writer) {
			w.Varuint64(uint64(100 + i))
			w.Float32(rng.Float32()*0.2 - 0.1)
			w.Float32(-0.0784)
			w.Float32(rng.Float32()*0.2 - 0.1)
			w.Varuint64(0)
		})
	}
	for range 10 {
		packet(21, func(w *wire.Writer) {
			w.Varint32(int32(rng.IntN(200) - 100))
			w.Varuint32(uint32(60 + rng.IntN(8)))
			w.Varint32(int32(rng.IntN(200) - 100))
			w.Varuint32(uint32(rng.IntN(15000)))
			w.Varuint32(3)
			w.Varuint32(0)
		})
	}
	for range 4 {
		packet(25, func(w *wire.Writer) {
			w.Varint32(2001)
			w.Float32(float32(rng.IntN(200) - 100))
			w.Float32(65)
			w.Float32(float32(rng.IntN(200) - 100))
			w.Varint32(int32(rng.IntN(15000)))
		})
	}
	return body
}

func BenchmarkDeflate(b *testing.B) {
	rng := rand.New(rand.NewPCG(5, 6))
	batches := make([][]byte, 64)
	size := 0
	for i := range batches {
		batches[i] = tickBatch(rng)
		size += len(batches[i])
	}
	run := func(b *testing.B, compress func(dst, src []byte) []byte) {
		var out []byte
		total := 0
		b.SetBytes(int64(size / len(batches)))
		for i := 0; b.Loop(); i++ {
			out = compress(out[:0], batches[i%len(batches)])
			total += len(out)
		}
		b.ReportMetric(float64(total)/float64(b.N)/float64(size/len(batches)), "ratio")
	}
	b.Run("stdlib", func(b *testing.B) {
		run(b, func(dst, src []byte) []byte { out, _ := Flate{FixedBelow: -1}.Compress(dst, src); return out })
	})
	b.Run("fixed", func(b *testing.B) { run(b, fixedDeflate) })
}
