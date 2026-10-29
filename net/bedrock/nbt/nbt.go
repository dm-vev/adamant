// Package nbt encodes and decodes NBT in the two forms Bedrock uses. Most
// packets carry the network form: little-endian, with the int and long
// tags and every length as zigzag varints and strings prefixed with an
// unsigned varint. The user data of items carries the little-endian form
// worlds are stored in, with fixed-width ints and lengths.
//
// Values are plain Go values, as gophertunnel's nbt package takes and gives
// them, so that the values a server builds for it go as they are: a map
// with string keys is a compound, any slice a list, a Go array of bytes,
// ints or longs an array tag, and uint8 (or bool or int8), int16, int32,
// int64, float32, float64 and string the other tags, named types by their
// kind. Read gives map[string]any, uint8, [N]byte, [N]int32 and [N]int64
// arrays, []byte, []int32 and []int64 for lists of bytes, ints and longs,
// and []any for other lists. Compounds are written with their keys in
// order, so that equal values encode to equal bytes.
package nbt

import (
	"errors"
	"fmt"
	"reflect"
	"slices"

	"github.com/dm-vev/adamant/net/bedrock/wire"
)

// Tag types.
const (
	tagEnd byte = iota
	tagByte
	tagShort
	tagInt
	tagLong
	tagFloat
	tagDouble
	tagByteArray
	tagString
	tagList
	tagCompound
	tagIntArray
	tagLongArray
)

const (
	// maxDepth bounds the nesting of compounds and lists read.
	maxDepth = 64
	// maxString bounds a string read, as the game bounds it.
	maxString = 32767
	// maxArray bounds an array read. Each length makes a Go array type that
	// is kept for good, so a peer must not choose from unbounded lengths.
	maxArray = 1 << 16
)

var (
	errDepth    = errors.New("nbt: nested too deep")
	errTagType  = errors.New("nbt: unknown tag type")
	errListType = errors.New("nbt: list of mixed types")
)

// Encoding is a form of NBT.
type Encoding uint8

const (
	// Network is the form of most packets: varint ints and lengths.
	Network Encoding = iota
	// LittleEndian is the form of worlds and of the user data of items:
	// fixed-width ints and lengths.
	LittleEndian
)

// Write writes a compound as the root tag of network NBT, with an empty
// name.
func Write(w *wire.Writer, v map[string]any) error { return WriteEncoded(w, v, Network) }

// Read reads network NBT whose root tag is a compound.
func Read(r *wire.Reader) (map[string]any, error) { return ReadEncoded(r, Network) }

// WriteEncoded writes a compound as the root tag of NBT in an encoding.
func WriteEncoded(w *wire.Writer, v map[string]any, e Encoding) error {
	w.Uint8(tagCompound)
	e.writeString(w, "")
	return e.writeCompound(w, v, 0)
}

// ReadEncoded reads NBT in an encoding whose root tag is a compound.
func ReadEncoded(r *wire.Reader, e Encoding) (map[string]any, error) {
	if t := r.Uint8(); t != tagCompound {
		if r.Err() != nil {
			return nil, r.Err()
		}
		return nil, fmt.Errorf("nbt: root tag %d is not a compound", t)
	}
	_ = e.readString(r)
	v := e.readCompound(r, 0)
	return v, r.Err()
}

// WriteList writes a list as the root tag of network NBT, with an empty
// name.
func WriteList(w *wire.Writer, l []any) error {
	w.Uint8(tagList)
	Network.writeString(w, "")
	return Network.writeList(w, l, 0)
}

// ReadList reads network NBT whose root tag is a list.
func ReadList(r *wire.Reader) ([]any, error) {
	if t := r.Uint8(); t != tagList {
		if r.Err() != nil {
			return nil, r.Err()
		}
		return nil, fmt.Errorf("nbt: root tag %d is not a list", t)
	}
	_ = Network.readString(r)
	l, _ := Network.readPayload(r, tagList, 0).([]any)
	return l, r.Err()
}

func (e Encoding) writeInt(w *wire.Writer, v int32) {
	if e == Network {
		w.Varint32(v)
	} else {
		w.Int32(v)
	}
}

func (e Encoding) readInt(r *wire.Reader) int32 {
	if e == Network {
		return r.Varint32()
	}
	return r.Int32()
}

func (e Encoding) writeLong(w *wire.Writer, v int64) {
	if e == Network {
		w.Varint64(v)
	} else {
		w.Int64(v)
	}
}

func (e Encoding) readLong(r *wire.Reader) int64 {
	if e == Network {
		return r.Varint64()
	}
	return r.Int64()
}

func (e Encoding) writeString(w *wire.Writer, s string) {
	if e == Network {
		w.Varuint32(uint32(len(s)))
	} else {
		w.Int16(int16(uint16(len(s))))
	}
	w.Bytes([]byte(s))
}

func (e Encoding) readString(r *wire.Reader) string {
	var n int
	if e == Network {
		n = int(r.Varuint32())
	} else {
		n = int(r.Int16())
	}
	if n < 0 || n > maxString {
		r.Fail(wire.ErrTooLong)
		return ""
	}
	return string(r.Next(n))
}

func tagOf(v any) (byte, bool) {
	switch v.(type) {
	case uint8, bool, int8:
		return tagByte, true
	case int16:
		return tagShort, true
	case int32:
		return tagInt, true
	case int64:
		return tagLong, true
	case float32:
		return tagFloat, true
	case float64:
		return tagDouble, true
	case string:
		return tagString, true
	case []any:
		return tagList, true
	case map[string]any:
		return tagCompound, true
	case nil:
		return 0, false
	}
	return tagOfType(reflect.TypeOf(v))
}

// tagOfType returns the tag of a type by its kind, as gophertunnel does.
func tagOfType(t reflect.Type) (byte, bool) {
	switch t.Kind() {
	case reflect.Uint8, reflect.Bool, reflect.Int8:
		return tagByte, true
	case reflect.Int16:
		return tagShort, true
	case reflect.Int32:
		return tagInt, true
	case reflect.Int64:
		return tagLong, true
	case reflect.Float32:
		return tagFloat, true
	case reflect.Float64:
		return tagDouble, true
	case reflect.String:
		return tagString, true
	case reflect.Array:
		switch t.Elem().Kind() {
		case reflect.Uint8:
			return tagByteArray, true
		case reflect.Int32:
			return tagIntArray, true
		case reflect.Int64:
			return tagLongArray, true
		}
	case reflect.Slice:
		return tagList, true
	case reflect.Map:
		if t.Key().Kind() == reflect.String {
			return tagCompound, true
		}
	}
	return 0, false
}

func (e Encoding) writeCompound(w *wire.Writer, m map[string]any, depth int) error {
	if depth == maxDepth {
		return errDepth
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		t, ok := tagOf(m[k])
		if !ok {
			return fmt.Errorf("nbt: %q holds a %T", k, m[k])
		}
		w.Uint8(t)
		e.writeString(w, k)
		if err := e.writePayload(w, t, m[k], depth); err != nil {
			return err
		}
	}
	w.Uint8(tagEnd)
	return nil
}

func (e Encoding) writePayload(w *wire.Writer, t byte, v any, depth int) error {
	switch v := v.(type) {
	case uint8:
		w.Uint8(v)
	case int8:
		w.Int8(v)
	case bool:
		w.Bool(v)
	case int16:
		w.Int16(v)
	case int32:
		e.writeInt(w, v)
	case int64:
		e.writeLong(w, v)
	case float32:
		w.Float32(v)
	case float64:
		w.Float64(v)
	case string:
		e.writeString(w, v)
	case []any:
		return e.writeList(w, v, depth)
	case map[string]any:
		return e.writeCompound(w, v, depth+1)
	default:
		return e.writeValue(w, t, reflect.ValueOf(v), depth)
	}
	return nil
}

// writeValue writes a value of another type by its kind.
func (e Encoding) writeValue(w *wire.Writer, t byte, v reflect.Value, depth int) error {
	if v.Kind() == reflect.Interface {
		v = v.Elem()
	}
	switch t {
	case tagByte:
		switch v.Kind() {
		case reflect.Bool:
			w.Bool(v.Bool())
		case reflect.Uint8:
			w.Uint8(uint8(v.Uint()))
		default:
			w.Int8(int8(v.Int()))
		}
	case tagShort:
		w.Int16(int16(v.Int()))
	case tagInt:
		e.writeInt(w, int32(v.Int()))
	case tagLong:
		e.writeLong(w, v.Int())
	case tagFloat:
		w.Float32(float32(v.Float()))
	case tagDouble:
		w.Float64(v.Float())
	case tagString:
		e.writeString(w, v.String())
	case tagByteArray:
		e.writeInt(w, int32(v.Len()))
		for i := range v.Len() {
			w.Uint8(uint8(v.Index(i).Uint()))
		}
	case tagIntArray:
		e.writeInt(w, int32(v.Len()))
		for i := range v.Len() {
			e.writeInt(w, int32(v.Index(i).Int()))
		}
	case tagLongArray:
		e.writeInt(w, int32(v.Len()))
		for i := range v.Len() {
			e.writeLong(w, v.Index(i).Int())
		}
	case tagList:
		return e.writeListValue(w, v, depth)
	case tagCompound:
		return e.writeMapValue(w, v, depth+1)
	}
	return nil
}

// writeListValue writes a slice of any type as a list: of the tag of its
// elements, or of its first element for a slice of interfaces.
func (e Encoding) writeListValue(w *wire.Writer, v reflect.Value, depth int) error {
	if depth+1 == maxDepth {
		return errDepth
	}
	elem := v.Type().Elem()
	dynamic := elem.Kind() == reflect.Interface
	t := tagEnd
	if dynamic && v.Len() > 0 {
		var ok bool
		if t, ok = tagOf(v.Index(0).Interface()); !ok {
			return fmt.Errorf("nbt: list holds a %T", v.Index(0).Interface())
		}
	} else if !dynamic {
		var ok bool
		if t, ok = tagOfType(elem); !ok {
			return fmt.Errorf("nbt: list of %s", elem)
		}
	}
	w.Uint8(t)
	e.writeInt(w, int32(v.Len()))
	for i := range v.Len() {
		x := v.Index(i)
		if dynamic {
			if xt, _ := tagOf(x.Interface()); xt != t {
				return errListType
			}
			if err := e.writePayload(w, t, x.Interface(), depth+1); err != nil {
				return err
			}
			continue
		}
		if err := e.writeValue(w, t, x, depth+1); err != nil {
			return err
		}
	}
	return nil
}

// writeMapValue writes a map with string keys of any type as a compound.
func (e Encoding) writeMapValue(w *wire.Writer, v reflect.Value, depth int) error {
	if depth == maxDepth {
		return errDepth
	}
	keys := make([]string, 0, v.Len())
	for _, k := range v.MapKeys() {
		keys = append(keys, k.String())
	}
	slices.Sort(keys)
	for _, k := range keys {
		x := v.MapIndex(reflect.ValueOf(k).Convert(v.Type().Key()))
		t, ok := tagOf(x.Interface())
		if !ok {
			return fmt.Errorf("nbt: %q holds a %T", k, x.Interface())
		}
		w.Uint8(t)
		e.writeString(w, k)
		if err := e.writePayload(w, t, x.Interface(), depth); err != nil {
			return err
		}
	}
	w.Uint8(tagEnd)
	return nil
}

func (e Encoding) writeList(w *wire.Writer, l []any, depth int) error {
	if depth+1 == maxDepth {
		return errDepth
	}
	t := tagEnd
	if len(l) > 0 {
		var ok bool
		if t, ok = tagOf(l[0]); !ok {
			return fmt.Errorf("nbt: list holds a %T", l[0])
		}
	}
	w.Uint8(t)
	e.writeInt(w, int32(len(l)))
	for _, v := range l {
		if vt, _ := tagOf(v); vt != t {
			return errListType
		}
		if err := e.writePayload(w, t, v, depth+1); err != nil {
			return err
		}
	}
	return nil
}

func (e Encoding) readCompound(r *wire.Reader, depth int) map[string]any {
	if depth == maxDepth {
		r.Fail(errDepth)
		return nil
	}
	m := make(map[string]any)
	for r.Err() == nil {
		t := r.Uint8()
		if t == tagEnd || r.Err() != nil {
			break
		}
		k := e.readString(r)
		m[k] = e.readPayload(r, t, depth)
	}
	return m
}

var (
	byteType  = reflect.TypeFor[byte]()
	int32Type = reflect.TypeFor[int32]()
	int64Type = reflect.TypeFor[int64]()
)

// newArray returns a settable Go array of n elements, or an invalid value,
// failing r, for an array past maxArray.
func newArray(r *wire.Reader, n int, elem reflect.Type) reflect.Value {
	if n > maxArray {
		r.Fail(wire.ErrTooLong)
		return reflect.Value{}
	}
	return reflect.New(reflect.ArrayOf(n, elem)).Elem()
}

// count reads the length of an array or list, which takes at least min
// bytes an element.
func (e Encoding) count(r *wire.Reader, min int) int {
	n := e.readInt(r)
	if n < 0 || int(n)*min > r.Len() {
		r.Fail(wire.ErrShort)
		return 0
	}
	return int(n)
}

func (e Encoding) readPayload(r *wire.Reader, t byte, depth int) any {
	switch t {
	case tagByte:
		return r.Uint8()
	case tagShort:
		return r.Int16()
	case tagInt:
		return e.readInt(r)
	case tagLong:
		return e.readLong(r)
	case tagFloat:
		return r.Float32()
	case tagDouble:
		return r.Float64()
	case tagByteArray:
		n := e.count(r, 1)
		a := newArray(r, n, byteType)
		if !a.IsValid() {
			return nil
		}
		reflect.Copy(a, reflect.ValueOf(r.Next(n)))
		return a.Interface()
	case tagString:
		return e.readString(r)
	case tagList:
		if depth+1 == maxDepth {
			r.Fail(errDepth)
			return nil
		}
		et := r.Uint8()
		n := e.count(r, 1)
		if et == tagEnd && n > 0 || et > tagLongArray {
			r.Fail(errTagType)
			return nil
		}
		// Lists of bytes, ints and longs read into slices of them.
		switch et {
		case tagByte:
			return append([]byte{}, r.Next(n)...)
		case tagInt:
			s := make([]int32, n)
			for i := range s {
				s[i] = e.readInt(r)
			}
			return s
		case tagLong:
			s := make([]int64, n)
			for i := range s {
				s[i] = e.readLong(r)
			}
			return s
		}
		l := make([]any, 0, n)
		for range n {
			if r.Err() != nil {
				return nil
			}
			l = append(l, e.readPayload(r, et, depth+1))
		}
		return l
	case tagCompound:
		return e.readCompound(r, depth+1)
	case tagIntArray:
		n := e.count(r, 1)
		a := newArray(r, n, int32Type)
		if !a.IsValid() {
			return nil
		}
		for i := range n {
			a.Index(i).SetInt(int64(e.readInt(r)))
		}
		return a.Interface()
	case tagLongArray:
		n := e.count(r, 1)
		a := newArray(r, n, int64Type)
		if !a.IsValid() {
			return nil
		}
		for i := range n {
			a.Index(i).SetInt(e.readLong(r))
		}
		return a.Interface()
	}
	r.Fail(errTagType)
	return nil
}
