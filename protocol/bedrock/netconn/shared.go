package netconn

import (
	"sync"

	"github.com/dm-vev/adamant/protocol/bedrock/internal/versions"
	"github.com/dm-vev/adamant/net/bedrock"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/wire"
)

// sharedPacket holds one encoded wire packet for a class of wire: raw bytes
// (header + fields), pre-framed batch entry (length varint + raw), and the
// packet ID for metrics and incompressibility checks.
type sharedPacket struct {
	raw    []byte
	framed []byte
	id     uint32
}

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
	model []sharedPacket

	mu    sync.Mutex
	wires map[*versions.Protocol][]sharedPacket
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
	s.once.Do(func() { s.model = []sharedPacket{encodeSharedPacket(Unwrap(s.Packet))} })
	if len(s.model) == 0 {
		return nil
	}
	return s.model[0].raw
}

// encoded returns the packets of a protocol that carry the packet, each
// header first and pre-framed, encoded the first time a connection of its
// class of wire writes them.
func (s *Shared) encoded(p *versions.Protocol) []sharedPacket {
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
	var out []sharedPacket
	_ = p.Encode(pk, pk, func(pk proto.Packet) error {
		out = append(out, encodeSharedPacket(pk))
		return nil
	})
	if s.wires == nil {
		s.wires = make(map[*versions.Protocol][]sharedPacket, 1)
	}
	s.wires[p] = out
	return out
}

// encodeSharedPacket encodes pk with its header and pre-frames it for a batch body.
func encodeSharedPacket(pk proto.Packet) sharedPacket {
	id := pk.ID()
	w := wire.Writer{B: bedrock.AppendHeader(nil, bedrock.Header{ID: id})}
	pk.Encode(&w)
	return sharedPacket{
		raw:    w.B,
		framed: bedrock.AppendPacket(nil, w.B),
		id:     id,
	}
}
