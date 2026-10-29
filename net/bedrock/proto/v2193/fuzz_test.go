package v2193

import (
	"bytes"
	"errors"
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

// TestNestingIsBounded decodes data store values, which hold values of
// their own type, nested to the bound and past it.
func TestNestingIsBounded(t *testing.T) {
	nested := func(lists int) []byte {
		var w wire.Writer
		w.Varuint32(1) // One update,
		w.Varuint32(1) // a change of a property.
		w.String("store")
		w.String("property")
		w.Uint32(0)
		for range lists {
			w.Int32(5) // A list of one value,
			w.Varuint32(1)
		}
		w.Int32(0) // ending in no value.
		return w.B
	}
	for _, c := range []struct {
		lists int
		ok    bool
	}{{wire.MaxDepth - 1, true}, {wire.MaxDepth, false}, {wire.MaxDepth * 100, false}} {
		b := nested(c.lists)
		var pk ClientBoundDataStorePacket
		r := wire.NewReader(b)
		pk.Decode(r)
		if c.ok {
			var w wire.Writer
			pk.Encode(&w)
			if r.Err() != nil || r.Len() != 0 || !bytes.Equal(w.B, b) {
				t.Errorf("%d nested lists: %v, %d bytes left", c.lists, r.Err(), r.Len())
			}
		} else if !errors.Is(r.Err(), wire.ErrDepth) {
			t.Errorf("%d nested lists: error %v, want %v", c.lists, r.Err(), wire.ErrDepth)
		}
	}
}

type blockPos [3]int32
type vec3 [3]float32

// TestMetadataOfNamedTypes writes metadata holding a position and a vector
// of named types, as a server built on gophertunnel holds them.
func TestMetadataOfNamedTypes(t *testing.T) {
	m := Metadata{1: blockPos{1, -2, 3}, 2: vec3{0.5, 1, 2}}
	var w wire.Writer
	writeMetadata(&w, &m)
	var back Metadata
	r := wire.NewReader(w.B)
	readMetadata(r, &back)
	if r.Err() != nil || back[1] != [3]int32{1, -2, 3} || back[2] != [3]float32{0.5, 1, 2} {
		t.Fatalf("read back %v, %v", back, r.Err())
	}
}
