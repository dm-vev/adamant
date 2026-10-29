package v2193

import (
	"bytes"
	"testing"

	"github.com/dm-vev/adamant/net/bedrock/wire"
)

// FuzzDecode decodes hostile bytes as every packet: a decoder may fail but
// never panic, and what it decodes encodes to bytes that come back the
// same through another round.
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
		if r.Err() != nil || r.Len() != 0 {
			t.Fatalf("packet %d: what it decoded does not decode again: %v, %d bytes left", pk.ID(), r.Err(), r.Len())
		}
		var w2 wire.Writer
		again.Encode(&w2)
		if !bytes.Equal(w2.B, w.B) {
			t.Fatalf("packet %d: encodes differently once decoded again\nfirst % x\nthen  % x", pk.ID(), w.B, w2.B)
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
