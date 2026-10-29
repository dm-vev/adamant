// Package nbt encodes and decodes NBT in the two forms Bedrock uses. Most
// packets carry the network form: little-endian, with the int and long
// tags and every length as zigzag varints and strings prefixed with an
// unsigned varint. The user data of items carries the little-endian form
// worlds are stored in, with fixed-width ints and lengths.
//
// Values are plain Go values: map[string]any for a compound, []any for a
// list, and int8, int16, int32, int64, float32, float64, string, []byte,
// []int32 and []int64 for the other tags. Compounds are written with their
// keys in order, so that equal values encode to equal bytes.
package nbt

import (
	"errors"
	"fmt"
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
	case int8, uint8, bool:
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
	case []byte:
		return tagByteArray, true
	case string:
		return tagString, true
	case []any:
		return tagList, true
	case map[string]any:
		return tagCompound, true
	case []int32:
		return tagIntArray, true
	case []int64:
		return tagLongArray, true
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
	switch t {
	case tagByte:
		switch v := v.(type) {
		case int8:
			w.Int8(v)
		case uint8:
			w.Uint8(v)
		case bool:
			w.Bool(v)
		}
	case tagShort:
		w.Int16(v.(int16))
	case tagInt:
		e.writeInt(w, v.(int32))
	case tagLong:
		e.writeLong(w, v.(int64))
	case tagFloat:
		w.Float32(v.(float32))
	case tagDouble:
		w.Float64(v.(float64))
	case tagByteArray:
		b := v.([]byte)
		e.writeInt(w, int32(len(b)))
		w.Bytes(b)
	case tagString:
		e.writeString(w, v.(string))
	case tagList:
		return e.writeList(w, v.([]any), depth)
	case tagCompound:
		return e.writeCompound(w, v.(map[string]any), depth+1)
	case tagIntArray:
		s := v.([]int32)
		e.writeInt(w, int32(len(s)))
		for _, x := range s {
			e.writeInt(w, x)
		}
	case tagLongArray:
		s := v.([]int64)
		e.writeInt(w, int32(len(s)))
		for _, x := range s {
			e.writeLong(w, x)
		}
	}
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
		return r.Int8()
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
		return append([]byte(nil), r.Next(n)...)
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
		s := make([]int32, 0, n)
		for range n {
			s = append(s, e.readInt(r))
		}
		return s
	case tagLongArray:
		n := e.count(r, 1)
		s := make([]int64, 0, n)
		for range n {
			s = append(s, e.readLong(r))
		}
		return s
	}
	r.Fail(errTagType)
	return nil
}
