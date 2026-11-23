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

func TestFixedDeflateBlockSyncFlushAllLengths(t *testing.T) {
	rng := rand.New(rand.NewPCG(19, 23))
	var block, stream []byte
	for n := 0; n <= 4096; n++ {
		src := make([]byte, n)
		for i := range src {
			if i%7 == 0 {
				src[i] = byte(rng.Uint32())
			} else {
				src[i] = byte(i * 11)
			}
		}
		block = fixedDeflateBlock(append(block[:0], 0x55), src, false)
		if block[0] != 0x55 {
			t.Fatalf("n=%d: prefix overwritten", n)
		}
		seg := block[1:]
		// Verify sync-flush trailer (0x00 0x00 0xFF 0xFF).
		if len(seg) < 6 || !bytes.Equal(seg[len(seg)-4:], []byte{0x00, 0x00, 0xff, 0xff}) {
			t.Fatalf("n=%d: missing sync-flush trailer: % x", n, seg)
		}
		stream = append(append(stream[:0], seg...), deflateFinalEmpty[:]...)

		if got := inflate(t, stream); !bytes.Equal(got, src) {
			t.Fatalf("n=%d: stdlib flate.NewReader mismatch: got %d want %d", n, len(got), len(src))
		}
		got2, err := (Flate{}).Decompress([]byte("HDR"), stream, n)
		if err != nil || !bytes.HasPrefix(got2, []byte("HDR")) || !bytes.Equal(got2[3:], src) {
			t.Fatalf("n=%d: Flate.Decompress mismatch: len=%d err=%v", n, len(got2), err)
		}
	}
}

func TestFixedDeflateBlockMultiSegment(t *testing.T) {
	rng := rand.New(rand.NewPCG(31, 37))
	segSizes := [][]int{
		{0, 0, 0},
		{1, 2, 3, 4, 5, 6, 7, 8, 9},
		{256, 512, 1024, 4096, 17},
		{1500, 0, 1500, 300, 29, 2000},
	}
	for tcIdx, sizes := range segSizes {
		var combined, want []byte
		for sIdx, sz := range sizes {
			seg := make([]byte, sz)
			for i := range seg {
				if sIdx%2 == 0 {
					seg[i] = byte('a' + (i % 13))
				} else {
					seg[i] = byte(rng.Uint32())
				}
			}
			want = append(want, seg...)
			combined = fixedDeflateBlock(combined, seg, false)
		}
		combined = append(combined, deflateFinalEmpty[:]...)

		if got := inflate(t, combined); !bytes.Equal(got, want) {
			t.Fatalf("tc %d: stdlib inflate mismatch len=%d want=%d", tcIdx, len(got), len(want))
		}
		got2, err := (Flate{}).Decompress(nil, combined, len(want))
		if err != nil || !bytes.Equal(got2, want) {
			t.Fatalf("tc %d: Flate.Decompress mismatch len=%d want=%d err=%v", tcIdx, len(got2), len(want), err)
		}
	}
}
