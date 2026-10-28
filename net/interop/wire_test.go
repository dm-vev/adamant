package interop

import (
	"bytes"
	"math/rand/v2"
	"testing"

	"github.com/dm-vev/adamant/net/bedrock/wire"
	"github.com/google/uuid"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// TestWireMatchesGophertunnel writes random values with both encoders and
// reads gophertunnel's bytes back with our reader.
func TestWireMatchesGophertunnel(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for range 2000 {
		var (
			b   = rng.Uint32()&1 == 1
			u8  = uint8(rng.Uint32())
			i16 = int16(rng.Uint32())
			u16 = uint16(rng.Uint32())
			i32 = int32(rng.Uint32()) >> (rng.Uint32() % 32)
			u32 = rng.Uint32() >> (rng.Uint32() % 32)
			i64 = int64(rng.Uint64()) >> (rng.Uint32() % 64)
			u64 = rng.Uint64() >> (rng.Uint32() % 64)
			f32 = float32(rng.NormFloat64() * 1000)
			f64 = rng.NormFloat64()
			s   = string(rune(rng.IntN(0x10000))) + "text"
			id  = uuid.New()
		)
		var gt bytes.Buffer
		w := protocol.NewWriter(&gt, 0)
		w.Bool(&b)
		w.Uint8(&u8)
		w.Int16(&i16)
		w.Uint16(&u16)
		w.Int32(&i32)
		w.BEInt32(&i32)
		w.Uint32(&u32)
		w.Int64(&i64)
		w.Uint64(&u64)
		w.Float32(&f32)
		w.Float64(&f64)
		w.Varint32(&i32)
		w.Varuint32(&u32)
		w.Varint64(&i64)
		w.Varuint64(&u64)
		w.String(&s)
		w.UUID(&id)

		var ours wire.Writer
		ours.Bool(b)
		ours.Uint8(u8)
		ours.Int16(i16)
		ours.Uint16(u16)
		ours.Int32(i32)
		ours.Int32BE(i32)
		ours.Uint32(u32)
		ours.Int64(i64)
		ours.Uint64(u64)
		ours.Float32(f32)
		ours.Float64(f64)
		ours.Varint32(i32)
		ours.Varuint32(u32)
		ours.Varint64(i64)
		ours.Varuint64(u64)
		ours.String(s)
		ours.UUID(id)
		if !bytes.Equal(gt.Bytes(), ours.B) {
			t.Fatalf("encodings differ:\ngophertunnel % x\nours         % x", gt.Bytes(), ours.B)
		}

		r := wire.NewReader(gt.Bytes())
		got := []any{r.Bool(), r.Uint8(), r.Int16(), r.Uint16(), r.Int32(), r.Int32BE(), r.Uint32(), r.Int64(), r.Uint64(),
			r.Float32(), r.Float64(), r.Varint32(), r.Varuint32(), r.Varint64(), r.Varuint64(), r.String(), uuid.UUID(r.UUID())}
		want := []any{b, u8, i16, u16, i32, i32, u32, i64, u64, f32, f64, i32, u32, i64, u64, s, id}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("field %d: read %v, want %v", i, got[i], want[i])
			}
		}
		if r.Err() != nil || r.Len() != 0 {
			t.Fatalf("err %v, %d bytes left", r.Err(), r.Len())
		}
	}
}
