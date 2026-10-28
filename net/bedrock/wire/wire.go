// Package wire encodes and decodes the field types of Bedrock packets:
// little-endian numbers, variable-length integers, strings and the small
// composite types built from them. A Writer appends to a slice without
// allocating; a Reader keeps the first error instead of panicking, and
// bounds what a hostile peer can make it allocate.
package wire

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

// Writer appends encoded fields to B.
type Writer struct {
	B []byte
}

func (w *Writer) Bool(v bool) {
	if v {
		w.B = append(w.B, 1)
	} else {
		w.B = append(w.B, 0)
	}
}

func (w *Writer) Uint8(v uint8)   { w.B = append(w.B, v) }
func (w *Writer) Int8(v int8)     { w.B = append(w.B, byte(v)) }
func (w *Writer) Uint16(v uint16) { w.B = binary.LittleEndian.AppendUint16(w.B, v) }
func (w *Writer) Int16(v int16)   { w.B = binary.LittleEndian.AppendUint16(w.B, uint16(v)) }
func (w *Writer) Uint32(v uint32) { w.B = binary.LittleEndian.AppendUint32(w.B, v) }
func (w *Writer) Int32(v int32)   { w.B = binary.LittleEndian.AppendUint32(w.B, uint32(v)) }
func (w *Writer) Uint64(v uint64) { w.B = binary.LittleEndian.AppendUint64(w.B, v) }
func (w *Writer) Int64(v int64)   { w.B = binary.LittleEndian.AppendUint64(w.B, uint64(v)) }

// Int32BE writes a big-endian int32, as a few login packets carry.
func (w *Writer) Int32BE(v int32) { w.B = binary.BigEndian.AppendUint32(w.B, uint32(v)) }

func (w *Writer) Float32(v float32) { w.Uint32(math.Float32bits(v)) }
func (w *Writer) Float64(v float64) { w.Uint64(math.Float64bits(v)) }

// Varuint32 and Varuint64 write unsigned variable-length integers, seven
// bits a byte.
func (w *Writer) Varuint32(v uint32) { w.B = binary.AppendUvarint(w.B, uint64(v)) }
func (w *Writer) Varuint64(v uint64) { w.B = binary.AppendUvarint(w.B, v) }

// Varint32 and Varint64 write signed variable-length integers, zigzag
// encoded.
func (w *Writer) Varint32(v int32) { w.B = binary.AppendVarint(w.B, int64(v)) }
func (w *Writer) Varint64(v int64) { w.B = binary.AppendVarint(w.B, v) }

// String writes a string prefixed with its length as a varuint32.
func (w *Writer) String(v string) {
	w.Varuint32(uint32(len(v)))
	w.B = append(w.B, v...)
}

// ByteSlice writes bytes prefixed with their length as a varuint32.
func (w *Writer) ByteSlice(v []byte) {
	w.Varuint32(uint32(len(v)))
	w.B = append(w.B, v...)
}

// Bytes writes bytes as they are, as the last field of a packet.
func (w *Writer) Bytes(v []byte) { w.B = append(w.B, v...) }

// ByteFloat writes an angle in degrees as a byte, 256 steps a turn.
func (w *Writer) ByteFloat(v float32) { w.B = append(w.B, byte(v/(360.0/256.0))) }

// UUID writes a UUID as two little-endian halves, the most significant
// first.
func (w *Writer) UUID(v [16]byte) {
	for i := 7; i >= 0; i-- {
		w.B = append(w.B, v[i])
	}
	for i := 15; i >= 8; i-- {
		w.B = append(w.B, v[i])
	}
}

// Errors of a Reader.
var (
	ErrShort    = errors.New("wire: data ends inside a field")
	ErrVarint   = errors.New("wire: malformed variable-length integer")
	ErrTooLong  = errors.New("wire: length over its limit")
	ErrTooLarge = errors.New("wire: value over its limit")
)

// DefaultMaxString bounds a string or byte slice read without a bound of
// its own.
const DefaultMaxString = 1 << 20

// Reader decodes fields from a slice. After the first error every read
// returns a zero value and Err returns that error.
type Reader struct {
	b   []byte
	off int
	err error
	// MaxString bounds the strings and byte slices read; zero means
	// DefaultMaxString.
	MaxString int
}

// NewReader returns a Reader of b.
func NewReader(b []byte) *Reader { return &Reader{b: b} }

// Err returns the first error of the reads.
func (r *Reader) Err() error { return r.err }

// Len returns the bytes left.
func (r *Reader) Len() int { return len(r.b) - r.off }

// Fail records err as the error of the Reader if it has none.
func (r *Reader) Fail(err error) {
	if r.err == nil {
		r.err = err
	}
}

// Failf records a formatted error.
func (r *Reader) Failf(format string, args ...any) { r.Fail(fmt.Errorf(format, args...)) }

func (r *Reader) take(n int) []byte {
	if r.err != nil {
		return nil
	}
	if n < 0 || n > len(r.b)-r.off {
		r.err = ErrShort
		return nil
	}
	b := r.b[r.off : r.off+n : r.off+n]
	r.off += n
	return b
}

func (r *Reader) Bool() bool {
	b := r.take(1)
	return b != nil && b[0] != 0
}

func (r *Reader) Uint8() uint8 {
	if b := r.take(1); b != nil {
		return b[0]
	}
	return 0
}

func (r *Reader) Int8() int8 { return int8(r.Uint8()) }

func (r *Reader) Uint16() uint16 {
	if b := r.take(2); b != nil {
		return binary.LittleEndian.Uint16(b)
	}
	return 0
}

func (r *Reader) Int16() int16 { return int16(r.Uint16()) }

func (r *Reader) Uint32() uint32 {
	if b := r.take(4); b != nil {
		return binary.LittleEndian.Uint32(b)
	}
	return 0
}

func (r *Reader) Int32() int32 { return int32(r.Uint32()) }

func (r *Reader) Int32BE() int32 {
	if b := r.take(4); b != nil {
		return int32(binary.BigEndian.Uint32(b))
	}
	return 0
}

func (r *Reader) Uint64() uint64 {
	if b := r.take(8); b != nil {
		return binary.LittleEndian.Uint64(b)
	}
	return 0
}

func (r *Reader) Int64() int64       { return int64(r.Uint64()) }
func (r *Reader) Float32() float32   { return math.Float32frombits(r.Uint32()) }
func (r *Reader) Float64() float64   { return math.Float64frombits(r.Uint64()) }
func (r *Reader) ByteFloat() float32 { return float32(r.Uint8()) * (360.0 / 256.0) }

// uvarint reads a varint of at most max bytes.
func (r *Reader) uvarint(max int) uint64 {
	if r.err != nil {
		return 0
	}
	var v uint64
	for i := range max {
		if r.off == len(r.b) {
			r.err = ErrShort
			return 0
		}
		c := r.b[r.off]
		r.off++
		v |= uint64(c&0x7f) << (7 * i)
		if c&0x80 == 0 {
			return v
		}
	}
	r.err = ErrVarint
	return 0
}

func (r *Reader) Varuint32() uint32 {
	v := r.uvarint(5)
	if v > math.MaxUint32 {
		r.Fail(ErrVarint)
		return 0
	}
	return uint32(v)
}

func (r *Reader) Varuint64() uint64 { return r.uvarint(10) }

func (r *Reader) Varint32() int32 {
	u := r.Varuint32()
	return int32(u>>1) ^ -int32(u&1)
}

func (r *Reader) Varint64() int64 {
	u := r.Varuint64()
	return int64(u>>1) ^ -int64(u&1)
}

// length reads the varuint32 length of a string or byte slice.
func (r *Reader) length() int {
	n := int(r.Varuint32())
	max := r.MaxString
	if max == 0 {
		max = DefaultMaxString
	}
	if n > max {
		r.Fail(ErrTooLong)
		return 0
	}
	return n
}

func (r *Reader) String() string { return string(r.take(r.length())) }

// ByteSlice reads a byte slice that shares the data of the Reader.
func (r *Reader) ByteSlice() []byte { return r.take(r.length()) }

// Bytes reads the bytes left, sharing the data of the Reader.
func (r *Reader) Bytes() []byte { return r.take(r.Len()) }

// Next reads n bytes, sharing the data of the Reader.
func (r *Reader) Next(n int) []byte { return r.take(n) }

func (r *Reader) UUID() (v [16]byte) {
	b := r.take(16)
	if b == nil {
		return v
	}
	for i := range 8 {
		v[7-i] = b[i]
		v[15-i] = b[8+i]
	}
	return v
}

// Count reads the varuint32 length of a list and checks it against max
// and against the bytes left, a byte at least an element.
func (r *Reader) Count(max int) int {
	n := r.Varuint32()
	if r.err != nil {
		return 0
	}
	if int64(n) > int64(max) {
		r.Fail(ErrTooLarge)
		return 0
	}
	if int(n) > r.Len() {
		r.Fail(ErrShort)
		return 0
	}
	return int(n)
}
