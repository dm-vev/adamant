// Package proto holds what the generated packet codecs of every protocol
// version share: the Packet interface and the small value types of the
// schema.
package proto

import "github.com/dm-vev/adamant/net/bedrock/wire"

// Packet is a packet of one protocol version with its codec.
type Packet interface {
	// ID returns the ID of the packet.
	ID() uint32
	// Encode appends the fields of the packet.
	Encode(w *wire.Writer)
	// Decode reads the fields of the packet; errors stay in the Reader.
	Decode(r *wire.Reader)
}

// Optional is a value that may be absent: a bool precedes it on the wire.
type Optional[T any] struct {
	Set bool
	Val T
}

// Some returns an Optional holding v.
func Some[T any](v T) Optional[T] { return Optional[T]{Set: true, Val: v} }

// Value returns the value of the Optional and whether it is set.
func (o Optional[T]) Value() (T, bool) { return o.Val, o.Set }

// Color is a colour as red, green, blue and alpha bytes.
type Color [4]uint8
