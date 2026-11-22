package nbt

import (
	"encoding/binary"
	"io"
	"math"
	"reflect"
	"sync"
)

// Encoder writes NBT objects to an NBT output stream.
type Encoder struct {
	// Encoding is the variant to use for encoding the objects passed. By default, the variant is set to
	// NetworkLittleEndian, which is the variant used for network NBT.
	Encoding Encoding

	w       offsetWriter
	depth   int
	encKind encodingKind
}

// NewEncoder returns a new encoder for the output stream writer passed.
func NewEncoder(w io.Writer) *Encoder {
	enc := &Encoder{Encoding: NetworkLittleEndian}
	initOffsetWriter(&enc.w, w)
	return enc
}

// NewEncoderWithEncoding returns a new encoder for the output stream writer passed using a specific encoding.
// It is identical to calling NewEncoder and setting the Encoding field manually.
func NewEncoderWithEncoding(w io.Writer, encoding Encoding) *Encoder {
	enc := &Encoder{Encoding: encoding}
	initOffsetWriter(&enc.w, w)
	return enc
}

// Encode encodes an object to NBT and writes it to the NBT output stream of the encoder. See the Marshal
// docs for the conversion from Go types to NBT tags and special struct tags.
func (e *Encoder) Encode(v any) error {
	e.encKind = encodingKindOf(e.Encoding)
	return e.marshalAny(v, "")
}

// Marshal encodes an object to its NBT representation and returns it as a byte slice. It uses the
// NetworkLittleEndian NBT encoding. To use a specific encoding, use MarshalEncoding.
//
// The Go value passed must be one of the values below, and structs, maps and slices may only have types that
// are found in the list below.
//
// The following Go types are converted to tags as such:
//
//	byte/uint8: TAG_Byte
//	bool: TAG_Byte
//	int16: TAG_Short
//	int32: TAG_Int
//	int64: TAG_Long
//	float32: TAG_Float
//	float64: TAG_Double
//	[...]byte: TAG_ByteArray
//	[...]int32: TAG_IntArray
//	[...]int64: TAG_LongArray
//	string: TAG_String
//	[]<type>: TAG_List
//	struct{...}: TAG_Compound
//	map[string]<type/any>: TAG_Compound
//
// Marshal accepts struct fields with the 'nbt' struct tag. The 'nbt' struct tag allows setting the name of
// a field that some tag should be decoded in. Setting the struct tag to '-' means that field will never be
// filled by the decoding of the data passed. Suffixing the 'nbt' struct tag with ',omitempty' will prevent
// the field from being encoded if it is equal to its default value.
func Marshal(v any) ([]byte, error) {
	return MarshalEncoding(v, NetworkLittleEndian)
}

const maxPooledEncodeCap = 64 << 10

type encodeState struct {
	enc Encoder
}

// encodeStatePool holds reusable Encoder + byte buffer pairs for MarshalEncoding.
var encodeStatePool = sync.Pool{
	New: func() any {
		s := &encodeState{}
		s.enc.w.raw = make([]byte, 0, 256)
		s.enc.w.useRaw = true
		return s
	},
}

// MarshalEncoding encodes an object to its NBT representation and returns it as a byte slice. Its
// functionality is identical to that of Marshal, except it accepts any NBT encoding.
func MarshalEncoding(v any, encoding Encoding) ([]byte, error) {
	s := encodeStatePool.Get().(*encodeState)
	s.enc.Encoding = encoding
	s.enc.depth = 0
	s.enc.w.off = 0
	s.enc.w.raw = s.enc.w.raw[:0]
	err := s.enc.Encode(v)
	data := append([]byte(nil), s.enc.w.raw...)
	if cap(s.enc.w.raw) <= maxPooledEncodeCap {
		s.enc.Encoding = nil
		s.enc.w.raw = s.enc.w.raw[:0]
		encodeStatePool.Put(s)
	}
	return data, err
}

func (e *Encoder) writeInt16(x int16) error {
	switch e.encKind {
	case encLittleEndian, encNetworkLittleEndian:
		return littleEndian{}.WriteInt16(&e.w, x)
	case encBigEndian, encNetworkBigEndian:
		return bigEndian{}.WriteInt16(&e.w, x)
	default:
		return e.Encoding.WriteInt16(&e.w, x)
	}
}

func (e *Encoder) writeInt32(x int32) error {
	switch e.encKind {
	case encLittleEndian:
		return littleEndian{}.WriteInt32(&e.w, x)
	case encNetworkLittleEndian:
		return networkLittleEndian{}.WriteInt32(&e.w, x)
	case encBigEndian, encNetworkBigEndian:
		return bigEndian{}.WriteInt32(&e.w, x)
	default:
		return e.Encoding.WriteInt32(&e.w, x)
	}
}

func (e *Encoder) writeInt64(x int64) error {
	switch e.encKind {
	case encLittleEndian:
		return littleEndian{}.WriteInt64(&e.w, x)
	case encNetworkLittleEndian:
		return networkLittleEndian{}.WriteInt64(&e.w, x)
	case encBigEndian, encNetworkBigEndian:
		return bigEndian{}.WriteInt64(&e.w, x)
	default:
		return e.Encoding.WriteInt64(&e.w, x)
	}
}

func (e *Encoder) writeFloat32(x float32) error {
	switch e.encKind {
	case encLittleEndian, encNetworkLittleEndian:
		return littleEndian{}.WriteFloat32(&e.w, x)
	case encBigEndian, encNetworkBigEndian:
		return bigEndian{}.WriteFloat32(&e.w, x)
	default:
		return e.Encoding.WriteFloat32(&e.w, x)
	}
}

func (e *Encoder) writeFloat64(x float64) error {
	switch e.encKind {
	case encLittleEndian, encNetworkLittleEndian:
		return littleEndian{}.WriteFloat64(&e.w, x)
	case encBigEndian, encNetworkBigEndian:
		return bigEndian{}.WriteFloat64(&e.w, x)
	default:
		return e.Encoding.WriteFloat64(&e.w, x)
	}
}

func (e *Encoder) writeString(x string) error {
	switch e.encKind {
	case encLittleEndian:
		return littleEndian{}.WriteString(&e.w, x)
	case encNetworkLittleEndian:
		return networkLittleEndian{}.WriteString(&e.w, x)
	case encBigEndian, encNetworkBigEndian:
		return bigEndian{}.WriteString(&e.w, x)
	default:
		return e.Encoding.WriteString(&e.w, x)
	}
}

// writeTag writes a single tag to the output held by the Encoder. The tag type and the name are written.
func (e *Encoder) writeTag(t tagType, tagName string) error {
	if e.depth >= maximumNestingDepth {
		return MaximumDepthReachedError{}
	}
	if e.encKind == encNetworkBigEndian && t == tagStruct && e.depth == 0 {
		// As of Minecraft Java 1.20.2, the name of the root compound tag is not written over the network.
		return e.w.writeByte(byte(t))
	}
	if e.w.useRaw {
		return e.writeTagRaw(t, tagName)
	}
	if e.w.out != nil {
		return e.writeTagBuffer(t, tagName)
	}
	if err := e.w.writeByte(byte(t)); err != nil {
		return err
	}
	return e.writeString(tagName)
}

func (e *Encoder) writeTagRaw(t tagType, tagName string) error {
	if len(tagName) > maxStringSize {
		e.w.raw = append(e.w.raw, byte(t))
		return InvalidStringError{Off: e.w.offset(), N: uint(len(tagName)), Err: errStringTooLong}
	}
	switch e.encKind {
	case encLittleEndian:
		n := uint16(len(tagName))
		e.w.raw = append(e.w.raw, byte(t), byte(n), byte(n>>8))
		e.w.raw = append(e.w.raw, tagName...)
		return nil
	case encBigEndian, encNetworkBigEndian:
		n := uint16(len(tagName))
		e.w.raw = append(e.w.raw, byte(t), byte(n>>8), byte(n))
		e.w.raw = append(e.w.raw, tagName...)
		return nil
	case encNetworkLittleEndian:
		ux := uint32(len(tagName))
		if ux < 0x80 {
			e.w.raw = append(e.w.raw, byte(t), byte(ux))
			e.w.raw = append(e.w.raw, tagName...)
			return nil
		}
		e.w.raw = append(e.w.raw, byte(t))
		for ux >= 0x80 {
			e.w.raw = append(e.w.raw, byte(ux)|0x80)
			ux >>= 7
		}
		e.w.raw = append(e.w.raw, byte(ux))
		e.w.raw = append(e.w.raw, tagName...)
		return nil
	default:
		if err := e.w.writeByte(byte(t)); err != nil {
			return err
		}
		return e.writeString(tagName)
	}
}

func (e *Encoder) writeTagBuffer(t tagType, tagName string) error {
	if len(tagName) > maxStringSize {
		_ = e.w.out.WriteByte(byte(t))
		e.w.off++
		return InvalidStringError{Off: e.w.off, N: uint(len(tagName)), Err: errStringTooLong}
	}
	var hdrLen int
	switch e.encKind {
	case encLittleEndian:
		n := uint16(len(tagName))
		e.w.buf[0] = byte(t)
		e.w.buf[1] = byte(n)
		e.w.buf[2] = byte(n >> 8)
		hdrLen = 3
	case encBigEndian, encNetworkBigEndian:
		n := uint16(len(tagName))
		e.w.buf[0] = byte(t)
		e.w.buf[1] = byte(n >> 8)
		e.w.buf[2] = byte(n)
		hdrLen = 3
	case encNetworkLittleEndian:
		e.w.buf[0] = byte(t)
		ux := uint32(len(tagName))
		hdrLen = 1
		for ux >= 0x80 {
			e.w.buf[hdrLen] = byte(ux) | 0x80
			ux >>= 7
			hdrLen++
		}
		e.w.buf[hdrLen] = byte(ux)
		hdrLen++
	default:
		if err := e.w.writeByte(byte(t)); err != nil {
			return err
		}
		return e.writeString(tagName)
	}
	if hdrLen+len(tagName) <= len(e.w.buf) {
		copy(e.w.buf[hdrLen:], tagName)
		n, _ := e.w.out.Write(e.w.buf[:hdrLen+len(tagName)])
		e.w.off += int64(n)
		return nil
	}
	n, _ := e.w.out.Write(e.w.buf[:hdrLen])
	m, _ := e.w.out.WriteString(tagName)
	e.w.off += int64(n + m)
	return nil
}

func (e *Encoder) marshalAny(v any, tagName string) error {
	switch val := v.(type) {
	case byte:
		if err := e.writeTag(tagByte, tagName); err != nil {
			return err
		}
		return e.w.writeByte(val)
	case bool:
		if err := e.writeTag(tagByte, tagName); err != nil {
			return err
		}
		return e.writeBool(val)
	case int16:
		if err := e.writeTag(tagInt16, tagName); err != nil {
			return err
		}
		return e.writeInt16(val)
	case int32:
		if err := e.writeTag(tagInt32, tagName); err != nil {
			return err
		}
		return e.writeInt32(val)
	case int64:
		if err := e.writeTag(tagInt64, tagName); err != nil {
			return err
		}
		return e.writeInt64(val)
	case float32:
		if err := e.writeTag(tagFloat32, tagName); err != nil {
			return err
		}
		return e.writeFloat32(val)
	case float64:
		if err := e.writeTag(tagFloat64, tagName); err != nil {
			return err
		}
		return e.writeFloat64(val)
	case string:
		if err := e.writeTag(tagString, tagName); err != nil {
			return err
		}
		return e.writeString(val)
	case map[string]any:
		if err := e.writeTag(tagStruct, tagName); err != nil {
			return err
		}
		return e.encodeMapAny(val)
	case []any:
		if err := e.writeTag(tagSlice, tagName); err != nil {
			return err
		}
		return e.encodeSliceAny(val, tagName)
	case []float32:
		if err := e.writeTag(tagSlice, tagName); err != nil {
			return err
		}
		return e.encodeFloat32Slice(val)
	case []string:
		if err := e.writeTag(tagSlice, tagName); err != nil {
			return err
		}
		return e.encodeStringSlice(val)
	case []int32:
		if err := e.writeTag(tagSlice, tagName); err != nil {
			return err
		}
		return e.encodeInt32Slice(val)
	case *map[string]any:
		if val == nil {
			return e.marshalReflect(reflect.ValueOf(v), tagName)
		}
		if err := e.writeTag(tagStruct, tagName); err != nil {
			return err
		}
		return e.encodeMapAny(*val)
	default:
		return e.marshalAnySliceOrReflect(v, tagName)
	}
}

func (e *Encoder) marshalAnySliceOrReflect(v any, tagName string) error {
	switch val := v.(type) {
	case []map[string]any:
		if err := e.writeTag(tagSlice, tagName); err != nil {
			return err
		}
		return e.encodeSliceMapAny(val)
	case []byte:
		if err := e.writeTag(tagSlice, tagName); err != nil {
			return err
		}
		return e.encodeByteSlice(val)
	case []bool:
		if err := e.writeTag(tagSlice, tagName); err != nil {
			return err
		}
		return e.encodeBoolSlice(val)
	case []int16:
		if err := e.writeTag(tagSlice, tagName); err != nil {
			return err
		}
		return e.encodeInt16Slice(val)
	case []int64:
		if err := e.writeTag(tagSlice, tagName); err != nil {
			return err
		}
		return e.encodeInt64Slice(val)
	case []float64:
		if err := e.writeTag(tagSlice, tagName); err != nil {
			return err
		}
		return e.encodeFloat64Slice(val)
	default:
		return e.marshalReflect(reflect.ValueOf(v), tagName)
	}
}

func (e *Encoder) writeBool(b bool) error {
	if b {
		return e.w.writeByte(1)
	}
	return e.w.writeByte(0)
}

func (e *Encoder) encodeMapAny(m map[string]any) error {
	e.depth++
	for k, v := range m {
		if err := e.marshalAny(v, k); err != nil {
			return err
		}
	}
	e.depth--
	return e.w.writeByte(byte(tagEnd))
}

func (e *Encoder) writeSliceHeader(listType tagType, length int) error {
	if err := e.w.writeByte(byte(listType)); err != nil {
		return FailedWriteError{Off: e.w.offset(), Op: "WriteSlice", Err: err}
	}
	return e.writeInt32(int32(length))
}

func tagFromAny(v any) tagType {
	switch v.(type) {
	case byte, bool:
		return tagByte
	case int16:
		return tagInt16
	case int32:
		return tagInt32
	case int64:
		return tagInt64
	case float32:
		return tagFloat32
	case float64:
		return tagFloat64
	case string:
		return tagString
	case map[string]any:
		return tagStruct
	case []any, []map[string]any, []byte, []bool, []int16, []int32, []int64, []float32, []float64, []string:
		return tagSlice
	default:
		return tagFromType(reflect.TypeOf(v))
	}
}

func (e *Encoder) encodeSliceAny(s []any, tagName string) error {
	e.depth++
	listType := tagEnd
	if len(s) > 0 {
		if s[0] == nil {
			return e.encodeReflect(reflect.ValueOf(s), tagName)
		}
		listType = tagFromAny(s[0])
		if listType == math.MaxUint8 {
			return IncompatibleTypeError{Type: sliceAnyType, ValueName: tagName}
		}
	}
	if err := e.writeSliceHeader(listType, len(s)); err != nil {
		return err
	}
	switch listType {
	case tagStruct:
		for _, elem := range s {
			if m, ok := elem.(map[string]any); ok {
				if err := e.encodeMapAny(m); err != nil {
					return err
				}
			} else if err := e.encodeAny(elem, ""); err != nil {
				return err
			}
		}
	case tagFloat32:
		for _, elem := range s {
			if f, ok := elem.(float32); ok {
				if err := e.writeFloat32(f); err != nil {
					return err
				}
			} else if err := e.encodeAny(elem, ""); err != nil {
				return err
			}
		}
	case tagString:
		for _, elem := range s {
			if str, ok := elem.(string); ok {
				if err := e.writeString(str); err != nil {
					return err
				}
			} else if err := e.encodeAny(elem, ""); err != nil {
				return err
			}
		}
	default:
		for _, elem := range s {
			if err := e.encodeAny(elem, ""); err != nil {
				return err
			}
		}
	}
	e.depth--
	return nil
}

func (e *Encoder) encodeAny(v any, tagName string) error {
	switch val := v.(type) {
	case map[string]any:
		return e.encodeMapAny(val)
	case float32:
		return e.writeFloat32(val)
	case string:
		return e.writeString(val)
	case int32:
		return e.writeInt32(val)
	case int16:
		return e.writeInt16(val)
	case int64:
		return e.writeInt64(val)
	case byte:
		return e.w.writeByte(val)
	case bool:
		return e.writeBool(val)
	case float64:
		return e.writeFloat64(val)
	case []any:
		return e.encodeSliceAny(val, tagName)
	case []map[string]any:
		return e.encodeSliceMapAny(val)
	case []byte:
		return e.encodeByteSlice(val)
	case []bool:
		return e.encodeBoolSlice(val)
	case []int16:
		return e.encodeInt16Slice(val)
	case []int32:
		return e.encodeInt32Slice(val)
	case []int64:
		return e.encodeInt64Slice(val)
	case []float32:
		return e.encodeFloat32Slice(val)
	case []float64:
		return e.encodeFloat64Slice(val)
	case []string:
		return e.encodeStringSlice(val)
	default:
		return e.encodeReflect(reflect.ValueOf(v), tagName)
	}
}

func (e *Encoder) encodeSliceMapAny(s []map[string]any) error {
	e.depth++
	if err := e.writeSliceHeader(tagStruct, len(s)); err != nil {
		return err
	}
	for _, elem := range s {
		if err := e.encodeMapAny(elem); err != nil {
			return err
		}
	}
	e.depth--
	return nil
}

func (e *Encoder) encodeByteSlice(s []byte) error {
	e.depth++
	if err := e.writeSliceHeader(tagByte, len(s)); err != nil {
		return err
	}
	if len(s) > 0 {
		if err := e.w.writeBytes(s); err != nil {
			return err
		}
	}
	e.depth--
	return nil
}

func (e *Encoder) encodeBoolSlice(s []bool) error {
	e.depth++
	if err := e.writeSliceHeader(tagByte, len(s)); err != nil {
		return err
	}
	for _, b := range s {
		if err := e.writeBool(b); err != nil {
			return err
		}
	}
	e.depth--
	return nil
}

func (e *Encoder) encodeInt16Slice(s []int16) error {
	e.depth++
	if err := e.writeSliceHeader(tagInt16, len(s)); err != nil {
		return err
	}
	for _, v := range s {
		if err := e.writeInt16(v); err != nil {
			return err
		}
	}
	e.depth--
	return nil
}

func (e *Encoder) encodeInt32Slice(s []int32) error {
	e.depth++
	if err := e.writeSliceHeader(tagInt32, len(s)); err != nil {
		return err
	}
	if e.w.useRaw && e.encKind == encLittleEndian {
		for _, v := range s {
			e.w.raw = binary.LittleEndian.AppendUint32(e.w.raw, uint32(v))
		}
		e.depth--
		return nil
	}
	for _, v := range s {
		if err := e.writeInt32(v); err != nil {
			return err
		}
	}
	e.depth--
	return nil
}

func (e *Encoder) encodeInt64Slice(s []int64) error {
	e.depth++
	if err := e.writeSliceHeader(tagInt64, len(s)); err != nil {
		return err
	}
	for _, v := range s {
		if err := e.writeInt64(v); err != nil {
			return err
		}
	}
	e.depth--
	return nil
}

func (e *Encoder) encodeFloat32Slice(s []float32) error {
	e.depth++
	if err := e.writeSliceHeader(tagFloat32, len(s)); err != nil {
		return err
	}
	if e.w.useRaw && (e.encKind == encLittleEndian || e.encKind == encNetworkLittleEndian) {
		for _, v := range s {
			e.w.raw = binary.LittleEndian.AppendUint32(e.w.raw, math.Float32bits(v))
		}
		e.depth--
		return nil
	}
	for _, v := range s {
		if err := e.writeFloat32(v); err != nil {
			return err
		}
	}
	e.depth--
	return nil
}

func (e *Encoder) encodeFloat64Slice(s []float64) error {
	e.depth++
	if err := e.writeSliceHeader(tagFloat64, len(s)); err != nil {
		return err
	}
	for _, v := range s {
		if err := e.writeFloat64(v); err != nil {
			return err
		}
	}
	e.depth--
	return nil
}

func (e *Encoder) encodeStringSlice(s []string) error {
	e.depth++
	if err := e.writeSliceHeader(tagString, len(s)); err != nil {
		return err
	}
	for _, v := range s {
		if err := e.writeString(v); err != nil {
			return err
		}
	}
	e.depth--
	return nil
}
