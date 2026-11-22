//go:build !armbe && !arm64be && !ppc64 && !mips && !mips64 && !mips64p32 && !ppc && !sparc && !sparc64 && !s390 && !s390x

package nbt

import (
	"encoding/binary"
	"math"
	"math/bits"
	"unsafe"
)

type littleEndian struct{}

// WriteInt16 ...
func (littleEndian) WriteInt16(w *offsetWriter, x int16) error {
	if w.useRaw {
		w.raw = binary.LittleEndian.AppendUint16(w.raw, uint16(x))
		return nil
	}
	b := w.buf[:2]
	binary.LittleEndian.PutUint16(b, uint16(x))
	if err := w.writeBytes(b); err != nil {
		return FailedWriteError{Op: "WriteInt16", Off: w.off}
	}
	return nil
}

// WriteInt32 ...
func (littleEndian) WriteInt32(w *offsetWriter, x int32) error {
	if w.useRaw {
		w.raw = binary.LittleEndian.AppendUint32(w.raw, uint32(x))
		return nil
	}
	b := w.buf[:4]
	binary.LittleEndian.PutUint32(b, uint32(x))
	if err := w.writeBytes(b); err != nil {
		return FailedWriteError{Op: "WriteInt32", Off: w.off}
	}
	return nil
}

// WriteInt64 ...
func (littleEndian) WriteInt64(w *offsetWriter, x int64) error {
	if w.useRaw {
		w.raw = binary.LittleEndian.AppendUint64(w.raw, uint64(x))
		return nil
	}
	b := w.buf[:8]
	binary.LittleEndian.PutUint64(b, uint64(x))
	if err := w.writeBytes(b); err != nil {
		return FailedWriteError{Op: "WriteInt64", Off: w.off}
	}
	return nil
}

// WriteFloat32 ...
func (littleEndian) WriteFloat32(w *offsetWriter, x float32) error {
	if w.useRaw {
		w.raw = binary.LittleEndian.AppendUint32(w.raw, math.Float32bits(x))
		return nil
	}
	b := w.buf[:4]
	binary.LittleEndian.PutUint32(b, math.Float32bits(x))
	if err := w.writeBytes(b); err != nil {
		return FailedWriteError{Op: "WriteFloat32", Off: w.off}
	}
	return nil
}

// WriteFloat64 ...
func (littleEndian) WriteFloat64(w *offsetWriter, x float64) error {
	if w.useRaw {
		w.raw = binary.LittleEndian.AppendUint64(w.raw, math.Float64bits(x))
		return nil
	}
	b := w.buf[:8]
	binary.LittleEndian.PutUint64(b, math.Float64bits(x))
	if err := w.writeBytes(b); err != nil {
		return FailedWriteError{Op: "WriteFloat64", Off: w.off}
	}
	return nil
}

// WriteString ...
func (e littleEndian) WriteString(w *offsetWriter, x string) error {
	if len(x) > maxStringSize {
		return InvalidStringError{Off: w.offset(), N: uint(len(x)), Err: errStringTooLong}
	}
	if w.useRaw {
		w.raw = binary.LittleEndian.AppendUint16(w.raw, uint16(len(x)))
		w.raw = append(w.raw, x...)
		return nil
	}
	if err := e.WriteInt16(w, int16(uint16(len(x)))); err != nil {
		return FailedWriteError{Op: "WriteString", Off: w.off}
	}
	if err := w.writeStringBytes(x); err != nil {
		return FailedWriteError{Op: "WriteString", Off: w.off}
	}
	return nil
}

// Int16 ...
func (littleEndian) Int16(r *offsetReader) (int16, error) {
	b, err := r.readSmall(2)
	if err != nil {
		return 0, errOverrunInt16
	}
	return int16(binary.LittleEndian.Uint16(b)), nil
}

// Int32 ...
func (littleEndian) Int32(r *offsetReader) (int32, error) {
	b, err := r.readSmall(4)
	if err != nil {
		return 0, errOverrunInt32
	}
	return int32(binary.LittleEndian.Uint32(b)), nil
}

// Int64 ...
func (littleEndian) Int64(r *offsetReader) (int64, error) {
	b, err := r.readSmall(8)
	if err != nil {
		return 0, errOverrunFloat64
	}
	return int64(binary.LittleEndian.Uint64(b)), nil
}

// Float32 ...
func (littleEndian) Float32(r *offsetReader) (float32, error) {
	b, err := r.readSmall(4)
	if err != nil {
		return 0, errOverrunFloat32
	}
	return math.Float32frombits(binary.LittleEndian.Uint32(b)), nil
}

// Float64 ...
func (littleEndian) Float64(r *offsetReader) (float64, error) {
	b, err := r.readSmall(8)
	if err != nil {
		return 0, errOverrunFloat64
	}
	return math.Float64frombits(binary.LittleEndian.Uint64(b)), nil
}

// String ...
func (e littleEndian) String(r *offsetReader) (string, error) {
	strLen, err := e.Int16(r)
	if err != nil {
		return "", BufferOverrunError{Op: "String"}
	}
	if strLen < 0 {
		return "", BufferOverrunError{Op: "String"}
	}
	s, err := r.readOwnedString(int(uint16(strLen)))
	if err != nil {
		return "", BufferOverrunError{Op: "String"}
	}
	return s, nil
}

// stringBytes reads a string into a temporary slice valid until the next read on r.
func (e littleEndian) stringBytes(r *offsetReader) ([]byte, error) {
	strLen, err := e.Int16(r)
	if err != nil {
		return nil, BufferOverrunError{Op: "String"}
	}
	if strLen < 0 {
		return nil, BufferOverrunError{Op: "String"}
	}
	b, err := r.readTempBytes(int(uint16(strLen)))
	if err != nil {
		return nil, BufferOverrunError{Op: "String"}
	}
	return b, nil
}

// Int32Slice ...
func (e littleEndian) Int32Slice(r *offsetReader) ([]int32, error) {
	n, err := e.Int32(r)
	if err != nil {
		return nil, BufferOverrunError{Op: "Int32Slice"}
	}
	if n < 0 {
		return nil, BufferOverrunError{Op: "Int32Slice"}
	}
	b, err := r.fixed(int64(n) * 4)
	if err != nil {
		return nil, BufferOverrunError{Op: "Int32Slice"}
	}
	if n == 0 {
		return []int32{}, nil
	}
	return unsafe.Slice((*int32)(unsafe.Pointer(&b[0])), n), nil
}

// Int64Slice ...
func (e littleEndian) Int64Slice(r *offsetReader) ([]int64, error) {
	n, err := e.Int32(r)
	if err != nil {
		return nil, BufferOverrunError{Op: "Int64Slice"}
	}
	if n < 0 {
		return nil, BufferOverrunError{Op: "Int64Slice"}
	}
	b, err := r.fixed(int64(n) * 8)
	if err != nil {
		return nil, BufferOverrunError{Op: "Int64Slice"}
	}
	if n == 0 {
		return []int64{}, nil
	}
	return unsafe.Slice((*int64)(unsafe.Pointer(&b[0])), n), nil
}

type bigEndian struct{}

// WriteInt16 ...
func (bigEndian) WriteInt16(w *offsetWriter, x int16) error {
	if w.useRaw {
		w.raw = binary.BigEndian.AppendUint16(w.raw, uint16(x))
		return nil
	}
	b := w.buf[:2]
	binary.BigEndian.PutUint16(b, uint16(x))
	if err := w.writeBytes(b); err != nil {
		return FailedWriteError{Op: "WriteInt16", Off: w.off}
	}
	return nil
}

// WriteInt32 ...
func (bigEndian) WriteInt32(w *offsetWriter, x int32) error {
	if w.useRaw {
		w.raw = binary.BigEndian.AppendUint32(w.raw, uint32(x))
		return nil
	}
	b := w.buf[:4]
	binary.BigEndian.PutUint32(b, uint32(x))
	if err := w.writeBytes(b); err != nil {
		return FailedWriteError{Op: "WriteInt32", Off: w.off}
	}
	return nil
}

// WriteInt64 ...
func (bigEndian) WriteInt64(w *offsetWriter, x int64) error {
	if w.useRaw {
		w.raw = binary.BigEndian.AppendUint64(w.raw, uint64(x))
		return nil
	}
	b := w.buf[:8]
	binary.BigEndian.PutUint64(b, uint64(x))
	if err := w.writeBytes(b); err != nil {
		return FailedWriteError{Op: "WriteInt64", Off: w.off}
	}
	return nil
}

// WriteFloat32 ...
func (bigEndian) WriteFloat32(w *offsetWriter, x float32) error {
	if w.useRaw {
		w.raw = binary.BigEndian.AppendUint32(w.raw, math.Float32bits(x))
		return nil
	}
	b := w.buf[:4]
	binary.BigEndian.PutUint32(b, math.Float32bits(x))
	if err := w.writeBytes(b); err != nil {
		return FailedWriteError{Op: "WriteFloat32", Off: w.off}
	}
	return nil
}

// WriteFloat64 ...
func (bigEndian) WriteFloat64(w *offsetWriter, x float64) error {
	if w.useRaw {
		w.raw = binary.BigEndian.AppendUint64(w.raw, math.Float64bits(x))
		return nil
	}
	b := w.buf[:8]
	binary.BigEndian.PutUint64(b, math.Float64bits(x))
	if err := w.writeBytes(b); err != nil {
		return FailedWriteError{Op: "WriteFloat64", Off: w.off}
	}
	return nil
}

// WriteString ...
func (e bigEndian) WriteString(w *offsetWriter, x string) error {
	if len(x) > maxStringSize {
		return InvalidStringError{Off: w.offset(), N: uint(len(x)), Err: errStringTooLong}
	}
	if w.useRaw {
		w.raw = binary.BigEndian.AppendUint16(w.raw, uint16(len(x)))
		w.raw = append(w.raw, x...)
		return nil
	}
	if err := e.WriteInt16(w, int16(uint16(len(x)))); err != nil {
		return FailedWriteError{Op: "WriteString", Off: w.off}
	}
	if err := w.writeStringBytes(x); err != nil {
		return FailedWriteError{Op: "WriteString", Off: w.off}
	}
	return nil
}

// Int16 ...
func (bigEndian) Int16(r *offsetReader) (int16, error) {
	b, err := r.readSmall(2)
	if err != nil {
		return 0, errOverrunInt16
	}
	return int16(binary.BigEndian.Uint16(b)), nil
}

// Int32 ...
func (bigEndian) Int32(r *offsetReader) (int32, error) {
	b, err := r.readSmall(4)
	if err != nil {
		return 0, errOverrunInt32
	}
	return int32(binary.BigEndian.Uint32(b)), nil
}

// Int64 ...
func (bigEndian) Int64(r *offsetReader) (int64, error) {
	b, err := r.readSmall(8)
	if err != nil {
		return 0, errOverrunFloat64
	}
	return int64(binary.BigEndian.Uint64(b)), nil
}

// Float32 ...
func (bigEndian) Float32(r *offsetReader) (float32, error) {
	b, err := r.readSmall(4)
	if err != nil {
		return 0, errOverrunFloat32
	}
	return math.Float32frombits(binary.BigEndian.Uint32(b)), nil
}

// Float64 ...
func (bigEndian) Float64(r *offsetReader) (float64, error) {
	b, err := r.readSmall(8)
	if err != nil {
		return 0, errOverrunFloat64
	}
	return math.Float64frombits(binary.BigEndian.Uint64(b)), nil
}

// String ...
func (e bigEndian) String(r *offsetReader) (string, error) {
	strLen, err := e.Int16(r)
	if err != nil {
		return "", BufferOverrunError{Op: "String"}
	}
	s, err := r.readOwnedString(int(uint16(strLen)))
	if err != nil {
		return "", BufferOverrunError{Op: "String"}
	}
	return s, nil
}

// stringBytes reads a string into a temporary slice valid until the next read on r.
func (e bigEndian) stringBytes(r *offsetReader) ([]byte, error) {
	strLen, err := e.Int16(r)
	if err != nil {
		return nil, BufferOverrunError{Op: "String"}
	}
	b, err := r.readTempBytes(int(uint16(strLen)))
	if err != nil {
		return nil, BufferOverrunError{Op: "String"}
	}
	return b, nil
}

// Int32Slice ...
func (e bigEndian) Int32Slice(r *offsetReader) ([]int32, error) {
	n, err := e.Int32(r)
	if err != nil {
		return nil, BufferOverrunError{Op: "Int32Slice"}
	}
	b, err := r.fixed(int64(n) * 4)
	if err != nil {
		return nil, BufferOverrunError{Op: "Int32Slice"}
	}
	if n == 0 {
		return []int32{}, nil
	}
	s := unsafe.Slice((*uint32)(unsafe.Pointer(&b[0])), n)
	for i := range s {
		s[i] = bits.ReverseBytes32(s[i])
	}
	return unsafe.Slice((*int32)(unsafe.Pointer(&b[0])), n), nil
}

// Int64Slice ...
func (e bigEndian) Int64Slice(r *offsetReader) ([]int64, error) {
	n, err := e.Int32(r)
	if err != nil {
		return nil, BufferOverrunError{Op: "Int64Slice"}
	}
	b, err := r.fixed(int64(n) * 8)
	if err != nil {
		return nil, BufferOverrunError{Op: "Int64Slice"}
	}
	if n == 0 {
		return []int64{}, nil
	}
	s := unsafe.Slice((*uint64)(unsafe.Pointer(&b[0])), n)
	for i := range s {
		s[i] = bits.ReverseBytes64(s[i])
	}
	return unsafe.Slice((*int64)(unsafe.Pointer(&b[0])), n), nil
}
