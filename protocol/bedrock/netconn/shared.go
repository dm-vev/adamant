package netconn

import (
	"sync"

	"github.com/df-mc/dragonfly/protocol/bedrock/internal/versions"
	"github.com/dm-vev/adamant/net/bedrock"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/wire"
)

// Shared is a packet of the model that goes alike to every client it is
// written to, such as a move many clients see: the first connection of
// each class of wire to write it encodes it, and the others of the class
// write those bytes. A class of wire is the protocols a packet goes to in
// the same bytes: the model and every protocol that takes the packet as it
// is, as Keeps tells, are one; each protocol that converts it is one of its
// own. A Shared must not be changed once written.
type Shared struct {
	proto.Packet

	once  sync.Once
	model [][]byte

	mu    sync.Mutex
	wires map[*versions.Protocol][][]byte
}

// Unwrap returns the packet, which a connection that converts packets
// converts.
func (s *Shared) Unwrap() proto.Packet { return s.Packet }

// Encode appends the fields of the packet as the model encodes them,
// encoded once: for what writes the packet without a connection of
// netconn.
func (s *Shared) Encode(w *wire.Writer) {
	raw := s.modelBytes()
	_, fields, _ := bedrock.ReadHeader(raw)
	w.B = append(w.B, fields...)
}

// modelBytes returns the packet as the model encodes it, header first.
func (s *Shared) modelBytes() []byte {
	s.once.Do(func() { s.model = [][]byte{encodeWithHeader(Unwrap(s.Packet))} })
	return s.model[0]
}

// encoded returns the packets of a protocol that carry the packet, each
// header first, encoded the first time a connection of its class of wire
// writes them.
func (s *Shared) encoded(p *versions.Protocol) [][]byte {
	pk := Unwrap(s.Packet)
	if p == nil || !p.Converts() || p.Keeps != nil && p.Keeps(pk) {
		s.modelBytes()
		return s.model
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if out, ok := s.wires[p]; ok {
		return out
	}
	var out [][]byte
	_ = p.Encode(pk, pk, func(pk proto.Packet) error {
		out = append(out, encodeWithHeader(pk))
		return nil
	})
	if s.wires == nil {
		s.wires = make(map[*versions.Protocol][][]byte, 1)
	}
	s.wires[p] = out
	return out
}

// encodeWithHeader returns a packet encoded with its header.
func encodeWithHeader(pk proto.Packet) []byte {
	w := wire.Writer{B: bedrock.AppendHeader(nil, bedrock.Header{ID: pk.ID()})}
	pk.Encode(&w)
	return w.B
}
