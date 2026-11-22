package nbt

import (
	"math"
)

// Encoding is an encoding variant of NBT. In general, there are three different encodings of NBT, which are
// all the same except for the way basic types are written.
type Encoding interface {
	Int16(r *offsetReader) (int16, error)
	Int32(r *offsetReader) (int32, error)
	Int64(r *offsetReader) (int64, error)
	Float32(r *offsetReader) (float32, error)
	Float64(r *offsetReader) (float64, error)
	String(r *offsetReader) (string, error)
	Int32Slice(r *offsetReader) ([]int32, error)
	Int64Slice(r *offsetReader) ([]int64, error)

	WriteInt16(w *offsetWriter, x int16) error
	WriteInt32(w *offsetWriter, x int32) error
	WriteInt64(w *offsetWriter, x int64) error
	WriteFloat32(w *offsetWriter, x float32) error
	WriteFloat64(w *offsetWriter, x float64) error
	WriteString(w *offsetWriter, x string) error
}

// encodingKind identifies the concrete built-in Encoding to avoid interface dispatch and escape analysis overhead.
type encodingKind uint8

const (
	encOther encodingKind = iota
	encLittleEndian
	encNetworkLittleEndian
	encBigEndian
	encNetworkBigEndian
)

func encodingKindOf(e Encoding) encodingKind {
	switch e.(type) {
	case littleEndian:
		return encLittleEndian
	case networkLittleEndian:
		return encNetworkLittleEndian
	case bigEndian:
		return encBigEndian
	case networkBigEndian:
		return encNetworkBigEndian
	default:
		return encOther
	}
}

var (
	// NetworkLittleEndian is the variable sized integer implementation of NBT. It is otherwise the same as the
	// normal little endian NBT. The NetworkLittleEndian format limits the total bytes of NBT that may be read. If
	// the limit is hit, the reading operation will fail immediately. NetworkLittleEndian is generally used for NBT
	// sent over network in the Bedrock Edition protocol.
	NetworkLittleEndian networkLittleEndian

	// LittleEndian is the fixed size little endian implementation of NBT. It is the format typically used for
	// writing Minecraft (Bedrock Edition) world saves.
	LittleEndian littleEndian

	// NetworkBigEndian is a version of BigEndian introduced in 1.20.2 where the name of the root compound tag is
	// not written. Similarly to BigEndian, it is only used on Minecraft Java Edition and generally used for NBT
	// sent over the network.
	NetworkBigEndian networkBigEndian

	// BigEndian is the fixed size big endian implementation of NBT. It is the original implementation, and is
	// used only on Minecraft Java Edition.
	BigEndian bigEndian

	_ Encoding = NetworkLittleEndian
	_ Encoding = LittleEndian
	_ Encoding = NetworkBigEndian
	_ Encoding = BigEndian
)

const maxStringSize = math.MaxInt16

type networkLittleEndian struct{ littleEndian }

// WriteInt32 ...
func (networkLittleEndian) WriteInt32(w *offsetWriter, x int32) error {
	ux := uint32(x) << 1
	if x < 0 {
		ux = ^ux
	}
	if w.useRaw {
		if ux < 0x80 {
			w.raw = append(w.raw, byte(ux))
			return nil
		}
		for ux >= 0x80 {
			w.raw = append(w.raw, byte(ux)|0x80)
			ux >>= 7
		}
		w.raw = append(w.raw, byte(ux))
		return nil
	}
	if ux < 0x80 {
		if err := w.writeByte(byte(ux)); err != nil {
			return FailedWriteError{Op: "WriteInt32", Off: w.offset()}
		}
		return nil
	}
	i := 0
	for ux >= 0x80 {
		w.buf[i] = byte(ux) | 0x80
		ux >>= 7
		i++
	}
	w.buf[i] = byte(ux)
	if err := w.writeBytes(w.buf[:i+1]); err != nil {
		return FailedWriteError{Op: "WriteInt32", Off: w.offset()}
	}
	return nil
}

// WriteInt64 ...
func (networkLittleEndian) WriteInt64(w *offsetWriter, x int64) error {
	ux := uint64(x) << 1
	if x < 0 {
		ux = ^ux
	}
	if w.useRaw {
		if ux < 0x80 {
			w.raw = append(w.raw, byte(ux))
			return nil
		}
		for ux >= 0x80 {
			w.raw = append(w.raw, byte(ux)|0x80)
			ux >>= 7
		}
		w.raw = append(w.raw, byte(ux))
		return nil
	}
	if ux < 0x80 {
		if err := w.writeByte(byte(ux)); err != nil {
			return FailedWriteError{Op: "WriteInt64", Off: w.offset()}
		}
		return nil
	}
	i := 0
	for ux >= 0x80 {
		w.buf[i] = byte(ux) | 0x80
		ux >>= 7
		i++
	}
	w.buf[i] = byte(ux)
	if err := w.writeBytes(w.buf[:i+1]); err != nil {
		return FailedWriteError{Op: "WriteInt64", Off: w.offset()}
	}
	return nil
}

// WriteString ...
func (networkLittleEndian) WriteString(w *offsetWriter, x string) error {
	if len(x) > maxStringSize {
		return InvalidStringError{Off: w.offset(), N: uint(len(x)), Err: errStringTooLong}
	}
	ux := uint32(len(x))
	if w.useRaw {
		if ux < 0x80 {
			w.raw = append(w.raw, byte(ux))
			w.raw = append(w.raw, x...)
			return nil
		}
		for ux >= 0x80 {
			w.raw = append(w.raw, byte(ux)|0x80)
			ux >>= 7
		}
		w.raw = append(w.raw, byte(ux))
		w.raw = append(w.raw, x...)
		return nil
	}
	if ux < 0x80 {
		if err := w.writeByte(byte(ux)); err != nil {
			return FailedWriteError{Op: "WriteString", Off: w.offset()}
		}
	} else {
		i := 0
		for ux >= 0x80 {
			w.buf[i] = byte(ux) | 0x80
			ux >>= 7
			i++
		}
		w.buf[i] = byte(ux)
		if err := w.writeBytes(w.buf[:i+1]); err != nil {
			return FailedWriteError{Op: "WriteString", Off: w.offset()}
		}
	}
	if err := w.writeStringBytes(x); err != nil {
		return FailedWriteError{Op: "WriteString", Off: w.offset()}
	}
	return nil
}

var (
	errOverrunInt16   error = BufferOverrunError{Op: "Int16"}
	errOverrunInt32   error = BufferOverrunError{Op: "Int32"}
	errOverrunInt64   error = BufferOverrunError{Op: "Int64"}
	errOverrunFloat32 error = BufferOverrunError{Op: "Float32"}
	errOverrunFloat64 error = BufferOverrunError{Op: "Float64"}
)

// Int32 ...
func (networkLittleEndian) Int32(r *offsetReader) (int32, error) {
	if r.useData {
		rem := r.rem
		if len(rem) == 0 {
			return 0, errOverrunInt32
		}
		b := rem[0]
		if b < 0x80 {
			r.rem = rem[1:]
			ux := uint32(b)
			x := int32(ux >> 1)
			if ux&1 != 0 {
				x = ^x
			}
			return x, nil
		}
		var ux uint32
		pos := 0
		for i := uint(0); i < 35; i += 7 {
			if pos >= len(rem) {
				r.rem = rem[pos:]
				return 0, errOverrunInt32
			}
			b = rem[pos]
			pos++
			ux |= uint32(b&0x7f) << i
			if b&0x80 == 0 {
				r.rem = rem[pos:]
				x := int32(ux >> 1)
				if ux&1 != 0 {
					x = ^x
				}
				return x, nil
			}
		}
		r.rem = rem[pos:]
		return 0, InvalidVarintError{N: 5, Off: r.offset()}
	}
	var ux uint32
	for i := uint(0); i < 35; i += 7 {
		b, err := r.readByte()
		if err != nil {
			return 0, errOverrunInt32
		}
		ux |= uint32(b&0x7f) << i
		if b&0x80 == 0 {
			x := int32(ux >> 1)
			if ux&1 != 0 {
				x = ^x
			}
			return x, nil
		}
	}
	return 0, InvalidVarintError{N: 5, Off: r.offset()}
}

// Int64 ...
func (networkLittleEndian) Int64(r *offsetReader) (int64, error) {
	if r.useData {
		rem := r.rem
		if len(rem) == 0 {
			return 0, errOverrunInt64
		}
		b := rem[0]
		if b < 0x80 {
			r.rem = rem[1:]
			ux := uint64(b)
			x := int64(ux >> 1)
			if ux&1 != 0 {
				x = ^x
			}
			return x, nil
		}
		var ux uint64
		pos := 0
		for i := uint(0); i < 70; i += 7 {
			if pos >= len(rem) {
				r.rem = rem[pos:]
				return 0, errOverrunInt64
			}
			b = rem[pos]
			pos++
			ux |= uint64(b&0x7f) << i
			if b&0x80 == 0 {
				r.rem = rem[pos:]
				x := int64(ux >> 1)
				if ux&1 != 0 {
					x = ^x
				}
				return x, nil
			}
		}
		r.rem = rem[pos:]
		return 0, InvalidVarintError{N: 10, Off: r.offset()}
	}
	var ux uint64
	for i := uint(0); i < 70; i += 7 {
		b, err := r.readByte()
		if err != nil {
			return 0, errOverrunInt64
		}
		ux |= uint64(b&0x7f) << i
		if b&0x80 == 0 {
			x := int64(ux >> 1)
			if ux&1 != 0 {
				x = ^x
			}
			return x, nil
		}
	}
	return 0, InvalidVarintError{N: 10, Off: r.offset()}
}

// String ...
func (e networkLittleEndian) String(r *offsetReader) (string, error) {
	length, err := e.stringLength(r)
	if err != nil {
		return "", err
	}
	if length > maxStringSize {
		return "", InvalidStringError{N: uint(length), Off: r.offset(), Err: errStringTooLong}
	}
	s, err := r.readOwnedString(int(length))
	if err != nil {
		return "", BufferOverrunError{Op: "String"}
	}
	return s, nil
}

// stringBytes reads a string into a temporary byte slice valid until the next read on r.
func (e networkLittleEndian) stringBytes(r *offsetReader) ([]byte, error) {
	length, err := e.stringLength(r)
	if err != nil {
		return nil, err
	}
	if length > maxStringSize {
		return nil, InvalidStringError{N: uint(length), Off: r.offset(), Err: errStringTooLong}
	}
	b, err := r.readTempBytes(int(length))
	if err != nil {
		return nil, BufferOverrunError{Op: "String"}
	}
	return b, nil
}

// stringLength reads the length of a string as a varuint32.
func (networkLittleEndian) stringLength(r *offsetReader) (uint32, error) {
	if r.useData {
		rem := r.rem
		if len(rem) == 0 {
			return 0, BufferOverrunError{Op: "StringLength"}
		}
		b := rem[0]
		if b < 0x80 {
			r.rem = rem[1:]
			return uint32(b), nil
		}
		var ux uint32
		pos := 0
		for i := uint(0); i < 35; i += 7 {
			if pos >= len(rem) {
				r.rem = rem[pos:]
				return 0, BufferOverrunError{Op: "StringLength"}
			}
			b = rem[pos]
			pos++
			ux |= uint32(b&0x7f) << i
			if b&0x80 == 0 {
				r.rem = rem[pos:]
				return ux, nil
			}
		}
		r.rem = rem[pos:]
		return 0, InvalidVarintError{N: 5, Off: r.offset()}
	}
	var ux uint32
	for i := uint(0); i < 35; i += 7 {
		b, err := r.readByte()
		if err != nil {
			return 0, BufferOverrunError{Op: "StringLength"}
		}
		ux |= uint32(b&0x7f) << i
		if b&0x80 == 0 {
			return ux, nil
		}
	}
	return 0, InvalidVarintError{N: 5, Off: r.offset()}
}

// Int32Slice ...
func (e networkLittleEndian) Int32Slice(r *offsetReader) ([]int32, error) {
	n, err := e.Int32(r)
	if err != nil {
		return nil, BufferOverrunError{Op: "Int32Slice"}
	}
	if n < 0 {
		return nil, BufferOverrunError{Op: "Int32Slice"}
	}
	c, err := r.capacity(int64(n))
	if err != nil {
		return nil, BufferOverrunError{Op: "Int32Slice"}
	}
	m := make([]int32, 0, c)
	for range n {
		x, err := e.Int32(r)
		if err != nil {
			return nil, BufferOverrunError{Op: "Int32Slice"}
		}
		m = append(m, x)
	}
	return m, nil
}

// Int64Slice ...
func (e networkLittleEndian) Int64Slice(r *offsetReader) ([]int64, error) {
	n, err := e.Int32(r)
	if err != nil {
		return nil, BufferOverrunError{Op: "Int64Slice"}
	}
	if n < 0 {
		return nil, BufferOverrunError{Op: "Int64Slice"}
	}
	c, err := r.capacity(int64(n))
	if err != nil {
		return nil, BufferOverrunError{Op: "Int64Slice"}
	}
	m := make([]int64, 0, c)
	for range n {
		x, err := e.Int64(r)
		if err != nil {
			return nil, BufferOverrunError{Op: "Int64Slice"}
		}
		m = append(m, x)
	}
	return m, nil
}

type networkBigEndian struct{ bigEndian }
