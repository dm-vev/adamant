package v2193

import (
	"testing"

	"github.com/dm-vev/adamant/net/bedrock/wire"
)

// FuzzDecode decodes hostile bytes as every packet: a decoder may fail but
// never panic, and what it decodes encodes again.
func FuzzDecode(f *testing.F) {
	f.Add(uint16(13), []byte{2, 4, 3, 'p', 'i', 'g'})
	f.Add(uint16(5), []byte{1, 0, 2, 'h', 'i', 0})
	f.Fuzz(func(t *testing.T, id uint16, b []byte) {
		pk := New(uint32(id) & 0x3ff)
		if pk == nil {
			return
		}
		r := wire.NewReader(b)
		pk.Decode(r)
		if r.Err() != nil {
			return
		}
		var w wire.Writer
		pk.Encode(&w)
		again := New(pk.ID())
		r = wire.NewReader(w.B)
		again.Decode(r)
		if r.Err() != nil {
			t.Fatalf("packet %d: what it decoded does not decode again: %v", pk.ID(), r.Err())
		}
	})
}

func TestEveryPacketDecodesEmpty(t *testing.T) {
	for id := uint32(0); id < 1024; id++ {
		if pk := New(id); pk != nil {
			r := wire.NewReader(nil)
			pk.Decode(r)
		}
	}
}
