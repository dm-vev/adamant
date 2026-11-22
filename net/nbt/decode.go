package nbt

import (
	"encoding/binary"
	"fmt"
	"io"
	"math/bits"
	"reflect"
	"sync"
	"unsafe"
)

// Decoder reads NBT objects from an NBT input stream.
type Decoder struct {
	// Encoding is the variant to use for decoding the NBT passed. By default, the variant is set to
	// NetworkLittleEndian, which is the variant used for network NBT.
	Encoding Encoding
	// AllowZero, when set to true, prevents an error from being returned if the
	// first byte read from an io.Reader is 0x00 (TAG_End). This kind of data is
	// technically invalid, but some implementations do this to represent an
	// empty NBT tree.
	AllowZero bool

	r        offsetReader
	depth    int
	encKind  encodingKind
	strCache *stringCache
}

type stringCache struct {
	keys      [64]string
	vals      [32]string
	dirtyKeys uint64
	dirtyVals uint32
}

func (c *stringCache) reset() {
	for m := c.dirtyKeys; m != 0; m &= m - 1 {
		c.keys[bits.TrailingZeros64(m)] = ""
	}
	c.dirtyKeys = 0
	for m := c.dirtyVals; m != 0; m &= m - 1 {
		c.vals[bits.TrailingZeros32(m)] = ""
	}
	c.dirtyVals = 0
}

type decodeState struct {
	dec   Decoder
	cache stringCache
}

var decodeStatePool = sync.Pool{
	New: func() any {
		s := &decodeState{}
		s.dec.strCache = &s.cache
		return s
	},
}

var stringCachePool = sync.Pool{
	New: func() any {
		return &stringCache{}
	},
}

var mapStringAnyType = reflect.TypeOf(map[string]any(nil))

// NewDecoder returns a new Decoder for the input stream reader passed.
func NewDecoder(r io.Reader) *Decoder {
	dec := &Decoder{Encoding: NetworkLittleEndian}
	initOffsetReader(&dec.r, r)
	return dec
}

// NewDecoderWithEncoding returns a new Decoder for the input stream reader passed with a specific encoding.
func NewDecoderWithEncoding(r io.Reader, encoding Encoding) *Decoder {
	dec := &Decoder{Encoding: encoding}
	initOffsetReader(&dec.r, r)
	return dec
}

// Decode reads the next NBT object from the input stream and stores it into the pointer to an object passed.
// See the Unmarshal docs for the conversion between NBT tags to Go types.
func (d *Decoder) Decode(v any) error {
	if v == nil {
		return NonPointerTypeError{ActualType: nil}
	}
	d.encKind = encodingKindOf(d.Encoding)
	switch dst := v.(type) {
	case *map[string]any:
		if dst != nil {
			return d.decodeRootMapAny(dst)
		}
	case *any:
		if dst != nil {
			return d.decodeRootAny(dst)
		}
	}
	val := reflect.ValueOf(v)
	if val.Kind() != reflect.Pointer {
		return NonPointerTypeError{ActualType: val.Type()}
	}
	t, tagName, err := d.tag()
	if err != nil {
		return err
	}
	if t == tagEnd && d.AllowZero {
		return nil
	}
	elem := val.Elem()
	if t == tagStruct && elem.Kind() == reflect.Struct {
		d.depth++
		if err := d.decodeStructPtr(elem.Addr().UnsafePointer(), getStructInfo(elem.Type()), tagName); err != nil {
			return err
		}
		d.depth--
		return nil
	}
	return d.unmarshalTag(elem, t, tagName)
}

// Unmarshal decodes a slice of NBT data into a pointer to a Go values passed. Marshal will use the
// NetworkLittleEndian encoding by default. To use a specific encoding, use UnmarshalEncoding.
//
// The Go value passed must be a pointer to a value. Anything else will return an error before decoding.
// The following NBT tags are decoded in the Go value passed as such:
//
//	TAG_Byte: byte/uint8(/any) or bool
//	TAG_Short: int16(/any)
//	TAG_Int: int32(/any)
//	TAG_Long: int64(/any)
//	TAG_Float: float32(/any)
//	TAG_Double: float64(/any)
//	TAG_ByteArray: [...]byte(/any) (The value must be a byte array, not a slice)
//	TAG_String: string(/any)
//	TAG_List: []any(/any) (The value type of the slice may vary. Depending on the type of
//	          values in the List tag, it might be of the type of any of the other tags, such as []int64.
//
// TAG_Compound: struct{...}/map[string]any(/any)
// TAG_IntArray: [...]int32(/any) (The value must be an int32 array, not a slice)
// TAG_LongArray: [...]int64(/any) (The value must be an int64 array, not a slice)
//
// Unmarshal returns an error if the data is decoded into a struct and the struct does not have all fields
// that the matching TAG_Compound in the NBT has, in order to prevent the loss of data. For varying data, the
// data should be decoded into a map.
// Nil maps and slices are initialised and filled out automatically by Unmarshal.
//
// Unmarshal accepts struct fields with the 'nbt' struct tag. The 'nbt' struct tag allows setting the name of
// a field that some tag should be decoded in. Setting the struct tag to '-' means that field will never be
// filled by the decoding of the data passed.
func Unmarshal(data []byte, v any) error {
	return UnmarshalEncoding(data, v, NetworkLittleEndian)
}

// UnmarshalEncoding decodes a slice of NBT data into a pointer to a Go values passed using the NBT encoding
// passed. Its functionality is identical to that of Unmarshal, except that it allows a specific encoding.
func UnmarshalEncoding(data []byte, v any, encoding Encoding) error {
	s := decodeStatePool.Get().(*decodeState)
	s.dec.Encoding = encoding
	s.dec.AllowZero = false
	s.dec.depth = 0
	s.dec.r.off = 0
	s.dec.r.initSlice(data)
	err := s.dec.Decode(v)
	s.dec.Encoding = nil
	s.dec.r.rem = nil
	s.cache.reset()
	decodeStatePool.Put(s)
	return err
}

// These types are initialised once and re-used for each Unmarshal call.
var stringType = reflect.TypeOf("")
var byteType = reflect.TypeOf(byte(0))
var int32Type = reflect.TypeOf(int32(0))
var int64Type = reflect.TypeOf(int64(0))

func (d *Decoder) decodeRootMapAny(dst *map[string]any) error {
	t, tagName, err := d.tag()
	if err != nil {
		return err
	}
	if t == tagEnd && d.AllowZero {
		if *dst == nil {
			*dst = make(map[string]any)
		} else {
			clear(*dst)
		}
		return nil
	}
	if t != tagStruct {
		return d.unmarshalTag(reflect.ValueOf(dst).Elem(), t, tagName)
	}
	var borrowed *stringCache
	if d.strCache == nil {
		borrowed = stringCachePool.Get().(*stringCache)
		d.strCache = borrowed
	}
	d.depth++
	m, err := d.unmarshalCompoundAny()
	if borrowed != nil {
		borrowed.reset()
		d.strCache = nil
		stringCachePool.Put(borrowed)
	}
	if err != nil {
		return err
	}
	d.depth--
	*dst = m
	return nil
}

func (d *Decoder) decodeRootAny(dst *any) error {
	t, _, err := d.tag()
	if err != nil {
		return err
	}
	if t == tagEnd && d.AllowZero {
		return nil
	}
	var borrowed *stringCache
	if d.strCache == nil {
		borrowed = stringCachePool.Get().(*stringCache)
		d.strCache = borrowed
	}
	v, err := d.unmarshalAny(t)
	if borrowed != nil {
		borrowed.reset()
		d.strCache = nil
		stringCachePool.Put(borrowed)
	}
	if err != nil {
		return err
	}
	*dst = v
	return nil
}

func (d *Decoder) readInt16() (int16, error) {
	switch d.encKind {
	case encLittleEndian, encNetworkLittleEndian:
		return littleEndian{}.Int16(&d.r)
	case encBigEndian, encNetworkBigEndian:
		return bigEndian{}.Int16(&d.r)
	default:
		return d.Encoding.Int16(&d.r)
	}
}

func (d *Decoder) readInt32() (int32, error) {
	switch d.encKind {
	case encLittleEndian:
		return littleEndian{}.Int32(&d.r)
	case encNetworkLittleEndian:
		return networkLittleEndian{}.Int32(&d.r)
	case encBigEndian, encNetworkBigEndian:
		return bigEndian{}.Int32(&d.r)
	default:
		return d.Encoding.Int32(&d.r)
	}
}

func (d *Decoder) readInt64() (int64, error) {
	switch d.encKind {
	case encLittleEndian:
		return littleEndian{}.Int64(&d.r)
	case encNetworkLittleEndian:
		return networkLittleEndian{}.Int64(&d.r)
	case encBigEndian, encNetworkBigEndian:
		return bigEndian{}.Int64(&d.r)
	default:
		return d.Encoding.Int64(&d.r)
	}
}

func (d *Decoder) readFloat32() (float32, error) {
	switch d.encKind {
	case encLittleEndian, encNetworkLittleEndian:
		return littleEndian{}.Float32(&d.r)
	case encBigEndian, encNetworkBigEndian:
		return bigEndian{}.Float32(&d.r)
	default:
		return d.Encoding.Float32(&d.r)
	}
}

func (d *Decoder) readFloat64() (float64, error) {
	switch d.encKind {
	case encLittleEndian, encNetworkLittleEndian:
		return littleEndian{}.Float64(&d.r)
	case encBigEndian, encNetworkBigEndian:
		return bigEndian{}.Float64(&d.r)
	default:
		return d.Encoding.Float64(&d.r)
	}
}

func (d *Decoder) readString() (string, error) {
	switch d.encKind {
	case encLittleEndian:
		return littleEndian{}.String(&d.r)
	case encNetworkLittleEndian:
		return networkLittleEndian{}.String(&d.r)
	case encBigEndian, encNetworkBigEndian:
		return bigEndian{}.String(&d.r)
	default:
		return d.Encoding.String(&d.r)
	}
}

func (d *Decoder) readStringBytes() ([]byte, error) {
	switch d.encKind {
	case encLittleEndian:
		return littleEndian{}.stringBytes(&d.r)
	case encNetworkLittleEndian:
		return networkLittleEndian{}.stringBytes(&d.r)
	case encBigEndian, encNetworkBigEndian:
		return bigEndian{}.stringBytes(&d.r)
	default:
		s, err := d.Encoding.String(&d.r)
		if err != nil {
			return nil, err
		}
		return []byte(s), nil
	}
}

func (d *Decoder) readInternedKey() (string, error) {
	b, err := d.readStringBytes()
	if err != nil {
		return "", err
	}
	n := len(b)
	if n == 0 {
		return "", nil
	}
	if n == 1 {
		return string(b), nil
	}
	if d.depth > 1 {
		if sc := d.strCache; sc != nil && n <= 32 {
			h := (uint32(b[0]) | uint32(b[n-1])<<8 | uint32(b[n>>1])<<16 | uint32(n)<<24) * 0x9e3779b1
			idx := h >> 26
			if cached := sc.keys[idx]; len(cached) == n && cached == string(b) {
				return cached, nil
			}
			s := string(b)
			sc.keys[idx] = s
			sc.dirtyKeys |= uint64(1) << idx
			return s, nil
		}
	}
	return string(b), nil
}

func (d *Decoder) readInternedVal() (string, error) {
	b, err := d.readStringBytes()
	if err != nil {
		return "", err
	}
	n := len(b)
	if n == 0 {
		return "", nil
	}
	if n == 1 {
		return string(b), nil
	}
	if sc := d.strCache; sc != nil && n <= 32 {
		h := (uint32(b[0]) | uint32(b[n-1])<<8 | uint32(b[n>>1])<<16 | uint32(n)<<24) * 0x9e3779b1
		idx := h >> 27
		if cached := sc.vals[idx]; len(cached) == n && cached == string(b) {
			return cached, nil
		}
		s := string(b)
		sc.vals[idx] = s
		sc.dirtyVals |= uint32(1) << idx
		return s, nil
	}
	return string(b), nil
}

func (d *Decoder) readInt32Slice() ([]int32, error) {
	switch d.encKind {
	case encLittleEndian:
		return littleEndian{}.Int32Slice(&d.r)
	case encNetworkLittleEndian:
		return networkLittleEndian{}.Int32Slice(&d.r)
	case encBigEndian, encNetworkBigEndian:
		return bigEndian{}.Int32Slice(&d.r)
	default:
		return d.Encoding.Int32Slice(&d.r)
	}
}

func (d *Decoder) readInt64Slice() ([]int64, error) {
	switch d.encKind {
	case encLittleEndian:
		return littleEndian{}.Int64Slice(&d.r)
	case encNetworkLittleEndian:
		return networkLittleEndian{}.Int64Slice(&d.r)
	case encBigEndian, encNetworkBigEndian:
		return bigEndian{}.Int64Slice(&d.r)
	default:
		return d.Encoding.Int64Slice(&d.r)
	}
}

// unmarshalCompoundAny decodes a TAG_Compound directly into a freshly allocated map[string]any without reflection.
func (d *Decoder) unmarshalCompoundAny() (map[string]any, error) {
	m := make(map[string]any)
	for {
		nestedTagType, nestedTagName, err := d.tagInterned()
		if err != nil {
			return nil, err
		}
		if !nestedTagType.IsValid() {
			return nil, UnknownTagError{Off: d.r.offset(), Op: "Map", TagType: nestedTagType}
		}
		if nestedTagType == tagEnd {
			return m, nil
		}
		val, err := d.unmarshalAny(nestedTagType)
		if err != nil {
			return nil, err
		}
		m[nestedTagName] = val
	}
}

// unmarshalAny decodes a tag of type t directly into a Go any value without reflection (except for rare array lengths).
func (d *Decoder) unmarshalAny(t tagType) (any, error) {
	switch t {
	case tagByte:
		v, err := d.r.readByte()
		if err != nil {
			return nil, BufferOverrunError{Op: "Byte"}
		}
		return v, nil
	case tagInt16:
		v, err := d.readInt16()
		if err != nil {
			return nil, err
		}
		return boxInt16(v), nil
	case tagInt32:
		v, err := d.readInt32()
		if err != nil {
			return nil, err
		}
		return boxInt32(v), nil
	case tagInt64:
		v, err := d.readInt64()
		if err != nil {
			return nil, err
		}
		return boxInt64(v), nil
	case tagFloat32:
		v, err := d.readFloat32()
		if err != nil {
			return nil, err
		}
		return boxFloat32(v), nil
	case tagFloat64:
		v, err := d.readFloat64()
		if err != nil {
			return nil, err
		}
		return boxFloat64(v), nil
	case tagString:
		return d.readInternedVal()
	case tagSlice:
		return d.unmarshalSliceAny()
	case tagStruct:
		d.depth++
		m, err := d.unmarshalCompoundAny()
		if err != nil {
			return nil, err
		}
		d.depth--
		return m, nil
	case tagByteArray:
		return d.unmarshalByteArrayAny()
	case tagInt32Array:
		return d.unmarshalInt32ArrayAny()
	case tagInt64Array:
		return d.unmarshalInt64ArrayAny()
	case tagEnd:
		return nil, UnexpectedTagError{Off: d.r.offset(), TagType: tagEnd}
	default:
		return nil, UnknownTagError{Off: d.r.offset(), TagType: t, Op: "Match"}
	}
}

func (d *Decoder) unmarshalByteArrayAny() (any, error) {
	length, err := d.readInt32()
	if err != nil {
		return nil, err
	}
	if length == 16 && d.r.hasBytes(16) {
		var a [16]byte
		_ = d.r.readInto(a[:])
		return a, nil
	}
	if d.r.hasBytes(int64(length)) {
		arr := reflect.New(reflect.ArrayOf(int(length), byteType))
		_ = d.r.readInto(unsafe.Slice((*byte)(arr.UnsafePointer()), int(length)))
		return arr.Elem().Interface(), nil
	}
	b, err := d.r.fixed(int64(length))
	if err != nil {
		return nil, BufferOverrunError{Op: "ByteArray"}
	}
	value := reflect.New(reflect.ArrayOf(int(length), byteType)).Elem()
	reflect.Copy(value, reflect.ValueOf(b))
	return value.Interface(), nil
}

func (d *Decoder) unmarshalInt32ArrayAny() (any, error) {
	n, err := d.readInt32()
	if err != nil || n < 0 {
		return nil, BufferOverrunError{Op: "Int32Slice"}
	}
	if n == 4 {
		var a [4]int32
		for i := range a {
			x, err := d.readInt32()
			if err != nil {
				return nil, BufferOverrunError{Op: "Int32Slice"}
			}
			a[i] = x
		}
		return a, nil
	}
	if d.encKind == encNetworkLittleEndian {
		c, err := d.r.capacity(int64(n))
		if err != nil {
			return nil, BufferOverrunError{Op: "Int32Slice"}
		}
		s := make([]int32, 0, c)
		for range n {
			x, err := d.readInt32()
			if err != nil {
				return nil, BufferOverrunError{Op: "Int32Slice"}
			}
			s = append(s, x)
		}
		value := reflect.New(reflect.ArrayOf(len(s), int32Type)).Elem()
		reflect.Copy(value, reflect.ValueOf(s))
		return value.Interface(), nil
	}
	if d.r.hasBytes(int64(n) * 4) {
		arr := reflect.New(reflect.ArrayOf(int(n), int32Type))
		dst := unsafe.Slice((*int32)(arr.UnsafePointer()), int(n))
		for i := range dst {
			x, err := d.readInt32()
			if err != nil {
				return nil, BufferOverrunError{Op: "Int32Slice"}
			}
			dst[i] = x
		}
		return arr.Elem().Interface(), nil
	}
	b, err := d.r.fixed(int64(n) * 4)
	if err != nil {
		return nil, BufferOverrunError{Op: "Int32Slice"}
	}
	if n == 0 {
		return [0]int32{}, nil
	}
	arr := reflect.New(reflect.ArrayOf(int(n), int32Type))
	dst := unsafe.Slice((*int32)(arr.UnsafePointer()), int(n))
	if d.encKind == encBigEndian || d.encKind == encNetworkBigEndian {
		for i := range dst {
			dst[i] = int32(binary.BigEndian.Uint32(b[i*4:]))
		}
	} else {
		for i := range dst {
			dst[i] = int32(binary.LittleEndian.Uint32(b[i*4:]))
		}
	}
	return arr.Elem().Interface(), nil
}

func (d *Decoder) unmarshalInt64ArrayAny() (any, error) {
	n, err := d.readInt32()
	if err != nil || n < 0 {
		return nil, BufferOverrunError{Op: "Int64Slice"}
	}
	if n == 2 {
		var a [2]int64
		for i := range a {
			x, err := d.readInt64()
			if err != nil {
				return nil, BufferOverrunError{Op: "Int64Slice"}
			}
			a[i] = x
		}
		return a, nil
	}
	if d.encKind == encNetworkLittleEndian {
		c, err := d.r.capacity(int64(n))
		if err != nil {
			return nil, BufferOverrunError{Op: "Int64Slice"}
		}
		s := make([]int64, 0, c)
		for range n {
			x, err := d.readInt64()
			if err != nil {
				return nil, BufferOverrunError{Op: "Int64Slice"}
			}
			s = append(s, x)
		}
		value := reflect.New(reflect.ArrayOf(len(s), int64Type)).Elem()
		reflect.Copy(value, reflect.ValueOf(s))
		return value.Interface(), nil
	}
	if d.r.hasBytes(int64(n) * 8) {
		arr := reflect.New(reflect.ArrayOf(int(n), int64Type))
		dst := unsafe.Slice((*int64)(arr.UnsafePointer()), int(n))
		for i := range dst {
			x, err := d.readInt64()
			if err != nil {
				return nil, BufferOverrunError{Op: "Int64Slice"}
			}
			dst[i] = x
		}
		return arr.Elem().Interface(), nil
	}
	b, err := d.r.fixed(int64(n) * 8)
	if err != nil {
		return nil, BufferOverrunError{Op: "Int64Slice"}
	}
	if n == 0 {
		return [0]int64{}, nil
	}
	arr := reflect.New(reflect.ArrayOf(int(n), int64Type))
	dst := unsafe.Slice((*int64)(arr.UnsafePointer()), int(n))
	if d.encKind == encBigEndian || d.encKind == encNetworkBigEndian {
		for i := range dst {
			dst[i] = int64(binary.BigEndian.Uint64(b[i*8:]))
		}
	} else {
		for i := range dst {
			dst[i] = int64(binary.LittleEndian.Uint64(b[i*8:]))
		}
	}
	return arr.Elem().Interface(), nil
}

func (d *Decoder) unmarshalSliceAny() (any, error) {
	if d.depth >= maximumNestingDepth {
		return nil, MaximumDepthReachedError{}
	}
	d.depth++
	listTypeByte, err := d.r.readByte()
	if err != nil {
		return nil, BufferOverrunError{Op: "Slice"}
	}
	listType := tagType(listTypeByte)
	if !listType.IsValid() {
		return nil, UnknownTagError{Off: d.r.offset(), TagType: listType, Op: "Slice"}
	}
	switch listType {
	case tagByte:
		length, err := d.readInt32()
		if err != nil || length < 0 {
			return nil, BufferOverrunError{Op: "ByteSlice"}
		}
		if length == 0 {
			d.depth--
			return []any{}, nil
		}
		b, err := d.r.fixed(int64(length))
		if err != nil {
			return nil, BufferOverrunError{Op: "ByteSlice"}
		}
		d.depth--
		return b, nil
	case tagInt32:
		s, err := d.readInt32Slice()
		if err != nil {
			return nil, BufferOverrunError{Op: "Int32Slice"}
		}
		d.depth--
		return s, nil
	case tagInt64:
		s, err := d.readInt64Slice()
		if err != nil {
			return nil, BufferOverrunError{Op: "Int64Slice"}
		}
		d.depth--
		return s, nil
	default:
		s, err := d.unmarshalGenericSliceAny(listType)
		if err != nil {
			return nil, err
		}
		d.depth--
		return s, nil
	}
}

func (d *Decoder) unmarshalGenericSliceAny(listType tagType) ([]any, error) {
	length, err := d.readInt32()
	if err != nil {
		return nil, err
	}
	n, err := d.r.capacity(int64(length))
	if err != nil {
		return nil, BufferOverrunError{Op: "Slice"}
	}
	s := make([]any, 0, n)
	switch listType {
	case tagStruct:
		for range length {
			d.depth++
			m, err := d.unmarshalCompoundAny()
			if err != nil {
				return nil, err
			}
			d.depth--
			s = append(s, m)
		}
	case tagFloat32:
		for range length {
			v, err := d.readFloat32()
			if err != nil {
				return nil, err
			}
			s = append(s, boxFloat32(v))
		}
	case tagString:
		for range length {
			v, err := d.readInternedVal()
			if err != nil {
				return nil, err
			}
			s = append(s, v)
		}
	case tagInt16:
		for range length {
			v, err := d.readInt16()
			if err != nil {
				return nil, err
			}
			s = append(s, boxInt16(v))
		}
	case tagFloat64:
		for range length {
			v, err := d.readFloat64()
			if err != nil {
				return nil, err
			}
			s = append(s, boxFloat64(v))
		}
	default:
		for range length {
			v, err := d.unmarshalAny(listType)
			if err != nil {
				return nil, err
			}
			s = append(s, v)
		}
	}
	return s, nil
}

// tag reads a tag from the decoder, and its name if the tag type is not a TAG_End.
func (d *Decoder) tag() (t tagType, tagName string, err error) {
	if d.depth >= maximumNestingDepth {
		return 0, "", MaximumDepthReachedError{}
	}
	if d.r.offset() >= maximumNetworkOffset && d.encKind == encNetworkLittleEndian {
		return 0, "", MaximumBytesReadError{}
	}
	tagTypeByte, err := d.r.readByte()
	if err != nil {
		return 0, "", BufferOverrunError{Op: "ReadTag"}
	}
	t = tagType(tagTypeByte)
	if d.encKind == encNetworkBigEndian && t == tagStruct && d.depth == 0 {
		// As of Minecraft Java 1.20.2, the name of the root compound tag is not written over the network.
		return t, "", nil
	}
	if t != tagEnd {
		// Only read a tag name if the tag's type is not TAG_End.
		tagName, err = d.readString()
	}
	return t, tagName, err
}

// tagInterned reads a tag and interns short tag names in d.strCache to deduplicate repeated compound keys.
func (d *Decoder) tagInterned() (t tagType, tagName string, err error) {
	if d.depth >= maximumNestingDepth {
		return 0, "", MaximumDepthReachedError{}
	}
	if d.r.offset() >= maximumNetworkOffset && d.encKind == encNetworkLittleEndian {
		return 0, "", MaximumBytesReadError{}
	}
	tagTypeByte, err := d.r.readByte()
	if err != nil {
		return 0, "", BufferOverrunError{Op: "ReadTag"}
	}
	t = tagType(tagTypeByte)
	if d.encKind == encNetworkBigEndian && t == tagStruct && d.depth == 0 {
		return t, "", nil
	}
	if t != tagEnd {
		tagName, err = d.readInternedKey()
	}
	return t, tagName, err
}

// tagBytes reads a tag and its name into a temporary byte slice valid until the next read on d.r.
func (d *Decoder) tagBytes() (t tagType, tagName []byte, err error) {
	if d.depth >= maximumNestingDepth {
		return 0, nil, MaximumDepthReachedError{}
	}
	if d.r.offset() >= maximumNetworkOffset && d.encKind == encNetworkLittleEndian {
		return 0, nil, MaximumBytesReadError{}
	}
	tagTypeByte, err := d.r.readByte()
	if err != nil {
		return 0, nil, BufferOverrunError{Op: "ReadTag"}
	}
	t = tagType(tagTypeByte)
	if d.encKind == encNetworkBigEndian && t == tagStruct && d.depth == 0 {
		return t, nil, nil
	}
	if t != tagEnd {
		tagName, err = d.readStringBytes()
	}
	return t, tagName, err
}

// isAny checks if a reflect.Value has the type `any` or `interface{}`.
func isAny(v reflect.Value) bool {
	return v.Kind() == reflect.Interface && v.NumMethod() == 0
}

// wrapListInvalidType formats InvalidTypeError inside a list element to match the legacy error message.
func wrapListInvalidType(err error, off int64, elemType reflect.Type, tagName string, i int, listType tagType) error {
	if e, ok := err.(InvalidTypeError); ok {
		return InvalidTypeError{
			Off:       off,
			FieldType: elemType,
			Field:     fmt.Sprintf("%v[%v].%v", tagName, i, e.Field),
			TagType:   listType,
		}
	}
	return err
}
