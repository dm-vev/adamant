package bedrock

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math/rand/v2"
	"testing"
)

func TestSnappyAlgorithmAndRoundTrip(t *testing.T) {
	var s Snappy
	if s.Algorithm() != AlgorithmSnappy || s.Algorithm() != 1 {
		t.Fatalf("Algorithm() = %d, want %d", s.Algorithm(), AlgorithmSnappy)
	}
	for i, in := range deflateInputs() {
		c, err := s.Compress([]byte{0xaa}, in)
		if err != nil || c[0] != 0xaa {
			t.Fatalf("input %d: compress err=%v prefix=%x", i, err, c[:1])
		}
		got, err := s.Decompress([]byte("HDR"), c[1:], len(in))
		if err != nil || !bytes.HasPrefix(got, []byte("HDR")) || !bytes.Equal(got[3:], in) {
			t.Fatalf("input %d (%d bytes): decompress len=%d err=%v", i, len(in), len(got), err)
		}
	}
}

func TestSnappyAllLengthsAndLarge(t *testing.T) {
	var s Snappy
	rng := rand.New(rand.NewPCG(7, 8))
	var comp, decomp []byte
	for n := 0; n <= 4096; n++ {
		in := make([]byte, n)
		for i := range in {
			if i%5 == 0 {
				in[i] = byte(rng.Uint32())
			} else {
				in[i] = byte(i * 13)
			}
		}
		var err error
		comp, err = s.Compress(comp[:0], in)
		if err != nil {
			t.Fatalf("n=%d compress: %v", n, err)
		}
		decomp, err = s.Decompress(decomp[:0], comp, n)
		if err != nil || !bytes.Equal(decomp, in) {
			t.Fatalf("n=%d round-trip: len=%d err=%v", n, len(decomp), err)
		}
	}
	for _, n := range []int{65535, 65536, 65537, 131072, 200000} {
		in := bytes.Repeat([]byte("bedrock-snappy-block-test!"), n/26+1)[:n]
		c, err := s.Compress(nil, in)
		if err != nil {
			t.Fatalf("large %d compress: %v", n, err)
		}
		got, err := s.Decompress(nil, c, n)
		if err != nil || !bytes.Equal(got, in) {
			t.Fatalf("large %d round-trip: len=%d err=%v", n, len(got), err)
		}
	}
}

func TestSnappyZeroAllocCompress(t *testing.T) {
	var s Snappy
	rng := rand.New(rand.NewPCG(9, 10))
	in := tickBatch(rng)
	dst := make([]byte, 0, 32+len(in)+len(in)/6)
	// Warm the pooled hash table.
	if _, err := s.Compress(dst[:0], in); err != nil {
		t.Fatal(err)
	}
	if allocs := testing.AllocsPerRun(200, func() {
		var err error
		dst, err = s.Compress(dst[:0], in)
		if err != nil {
			t.Fatal(err)
		}
	}); allocs != 0 {
		t.Fatalf("Snappy.Compress allocated %v times in steady state", allocs)
	}
}

func TestSnappyDecompressBounds(t *testing.T) {
	var s Snappy
	payload := bytes.Repeat([]byte("snappy-limit-payload-"), 20)
	valid, err := s.Compress(nil, payload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Decompress(nil, valid, len(payload)-1); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("limit n-1: got %v, want ErrTooLarge", err)
	}
	if _, err := s.Decompress(nil, valid, -1); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("limit -1: got %v, want ErrTooLarge", err)
	}
	empty, _ := s.Compress(nil, nil)
	if got, err := s.Decompress([]byte("P:"), empty, 0); err != nil || string(got) != "P:" {
		t.Fatalf("empty limit 0: got=%q err=%v", got, err)
	}
	one, _ := s.Compress(nil, []byte{'x'})
	if _, err := s.Decompress(nil, one, 0); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("1B limit 0: got %v, want ErrTooLarge", err)
	}

	// Copy4 valid packet check: 4 literal bytes + copy4(len=4, offset=4).
	copy4Valid := []byte{8, 3<<2 | 0, 'a', 'b', 'c', 'd', 3<<2 | 3, 4, 0, 0, 0}
	if got, err := s.Decompress(nil, copy4Valid, 8); err != nil || string(got) != "abcdabcd" {
		t.Fatalf("valid copy4: got=%q err=%v", got, err)
	}

	corrupt := map[string][]byte{
		"empty_src":         nil,
		"truncated_varint":  {0x80},
		"overflow_varint":   {0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x02},
		"u32_overflow":      {0x80, 0x80, 0x80, 0x80, 0x10},
		"truncated_body":    {0x10, 0x00, 'a'},
		"huge_len_short":    append(binary.AppendUvarint(nil, 1<<20), 0x00, 'a'),
		"trunc_lit_len60":   {2, 60 << 2},
		"trunc_lit_len63":   {2, 63 << 2, 1, 0, 0},
		"trunc_lit_payload": {4, 3<<2 | 0, 'a', 'b'},
		"trunc_copy1":       {4, 1, 0, 'a', snappyTagCopy1},
		"trunc_copy2":       {4, 1, 0, 'a', snappyTagCopy2, 1},
		"trunc_copy4":       {4, 1, 0, 'a', snappyTagCopy4, 1, 0, 0},
		"copy1_zero_off":    {5, 0, 'a', snappyTagCopy1, 0},
		"copy1_oob_off":     {5, 0, 'a', snappyTagCopy1, 2},
		"copy2_zero_off":    {5, 0, 'a', 3<<2 | snappyTagCopy2, 0, 0},
		"copy2_oob_off":     {5, 0, 'a', 3<<2 | snappyTagCopy2, 5, 0},
		"copy4_zero_off":    {5, 0, 'a', 3<<2 | snappyTagCopy4, 0, 0, 0, 0},
		"copy4_oob_off":     {5, 0, 'a', 3<<2 | snappyTagCopy4, 9, 0, 0, 0},
		"copy1_past_end":    {3, 0, 'a', snappyTagCopy1, 1},
		"underfilled":       {5, 0, 'a'},
		"trailing_garbage":  {1, 0, 'a', 0, 'b'},
	}
	for name, bad := range corrupt {
		if _, err := s.Decompress(nil, bad, 2<<20); err == nil {
			t.Errorf("%s: expected corrupt error, got nil", name)
		}
	}
}

func TestSnappyConnRoundTrip(t *testing.T) {
	a, b := pipes()
	cache := NewBatchCache()
	ca, cb := NewConn(a, Config{Cache: cache}), NewConn(b, Config{})
	ca.EnableCompression(Snappy{})
	cb.EnableCompression(Snappy{})

	small := packet(9, []byte("hello"))
	large := packet(10, bytes.Repeat([]byte("snappy batch payload "), 60))
	_ = ca.WritePacket(small)
	_ = ca.Flush()
	_ = ca.WritePacket(small)
	_ = ca.WritePacket(large)
	_ = ca.Flush()
	if a.sent[0][0] != byte(AlgorithmNone&0xff) || a.sent[1][0] != byte(AlgorithmSnappy) {
		t.Fatalf("prefixes %#x, %#x", a.sent[0][0], a.sent[1][0])
	}
	for _, want := range [][]byte{small, small, large} {
		if got, err := cb.ReadPacket(); err != nil || !bytes.Equal(got, want) {
			t.Fatalf("read %d bytes err=%v, want %d", len(got), err, len(want))
		}
	}
}

func FuzzSnappy(f *testing.F) {
	f.Add([]byte("hello snappy hello snappy"))
	f.Fuzz(func(t *testing.T, in []byte) {
		var s Snappy
		c, err := s.Compress(nil, in)
		if err != nil {
			t.Fatal(err)
		}
		got, err := s.Decompress(nil, c, len(in))
		if err != nil || !bytes.Equal(got, in) {
			t.Fatalf("round-trip failed: len=%d want=%d err=%v", len(got), len(in), err)
		}
		// Ensure Decompress never panics on arbitrary bytes.
		_, _ = s.Decompress(nil, in, 64<<10)
	})
}
