// Package bedrock carries Minecraft: Bedrock Edition packets over a message
// transport such as a NetherNet connection: packets go in batches, each
// batch compressed or not on its own, with the limits a server keeps
// against a hostile peer. It knows the framing of packets, not their
// fields: codecs plug in above it.
package bedrock

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// Compression algorithms a batch names in its first byte once compression
// is on, as NetworkSettings numbers them.
const (
	AlgorithmFlate  uint16 = 0
	AlgorithmSnappy uint16 = 1
	AlgorithmNone   uint16 = 0xffff
)

var (
	errPacketLength = errors.New("bedrock: packet length past the end of the batch")
	errEmptyPacket  = errors.New("bedrock: empty packet")
	errTooMany      = errors.New("bedrock: too many packets in a batch")
)

// AppendPacket appends a packet to the body of a batch: its length, then
// the packet, its header and fields.
func AppendPacket(body, packet []byte) []byte {
	return append(binary.AppendUvarint(body, uint64(len(packet))), packet...)
}

// Packets calls f with each packet of the body of a batch, at most max
// packets, each valid until f returns. It stops at the first error.
func Packets(body []byte, max int, f func(packet []byte) error) error {
	for n := 0; len(body) > 0; n++ {
		if n == max {
			return errTooMany
		}
		size, k := binary.Uvarint(body)
		if k <= 0 || size > uint64(len(body)-k) {
			return errPacketLength
		}
		if size == 0 {
			return errEmptyPacket
		}
		if err := f(body[k : k+int(size)]); err != nil {
			return err
		}
		body = body[k+int(size):]
	}
	return nil
}

// Header is the header of a packet: its ID and the sub-clients, split
// screen players, it goes from and to.
type Header struct {
	ID                   uint32
	SenderSub, TargetSub uint8
}

// AppendHeader appends the header of a packet.
func AppendHeader(b []byte, h Header) []byte {
	return binary.AppendUvarint(b, uint64(h.ID&0x3ff|uint32(h.SenderSub&3)<<10|uint32(h.TargetSub&3)<<12))
}

// ReadHeader reads the header at the start of a packet and returns the
// fields after it.
func ReadHeader(packet []byte) (Header, []byte, error) {
	v, k := binary.Uvarint(packet)
	if k <= 0 || v > 0xffffffff {
		return Header{}, nil, fmt.Errorf("bedrock: malformed packet header")
	}
	return Header{ID: uint32(v) & 0x3ff, SenderSub: uint8(v>>10) & 3, TargetSub: uint8(v>>12) & 3}, packet[k:], nil
}
