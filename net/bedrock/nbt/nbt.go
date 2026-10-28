// Package nbt encodes and decodes NBT in the form Bedrock packets carry
// it: little-endian, with the int and long tags and every length as zigzag
// varints and strings prefixed with an unsigned varint. Values are plain
// Go values: map[string]any for a compound, []any for a list, and int8,
// int16, int32, int64, float32, float64, string, []byte, []int32 and
// []int64 for the other tags. Compounds are written with their keys in
// order, so that equal values encode to equal bytes.
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

// Write writes a compound as the root tag of NBT, with an empty name.
func Write(w *wire.Writer, v map[string]any) error {
	w.Uint8(tagCompound)
	w.String("")
	return writeCompound(w, v, 0)
}

// Read reads NBT whose root tag is a compound.
func Read(r *wire.Reader) (map[string]any, error) {
	if t := r.Uint8(); t != tagCompound {
		if r.Err() != nil {
			return nil, r.Err()
		}
		return nil, fmt.Errorf("nbt: root tag %d is not a compound", t)
	}
	_ = readString(r)
	v := readCompound(r, 0)
	return v, r.Err()
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

func writeCompound(w *wire.Writer, m map[string]any, depth int) error {
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
		writeString(w, k)
		if err := writePayload(w, t, m[k], depth); err != nil {
			return err
		}
	}
	w.Uint8(tagEnd)
	return nil
}

func writeString(w *wire.Writer, s string) {
	w.Varuint32(uint32(len(s)))
	w.Bytes([]byte(s))
}

func writePayload(w *wire.Writer, t byte, v any, depth int) error {
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
		w.Varint32(v.(int32))
	case tagLong:
		w.Varint64(v.(int64))
	case tagFloat:
		w.Float32(v.(float32))
	case tagDouble:
		w.Float64(v.(float64))
	case tagByteArray:
		b := v.([]byte)
		w.Varint32(int32(len(b)))
		w.Bytes(b)
	case tagString:
		writeString(w, v.(string))
	case tagList:
		return writeList(w, v.([]any), depth)
	case tagCompound:
		return writeCompound(w, v.(map[string]any), depth+1)
	case tagIntArray:
		s := v.([]int32)
		w.Varint32(int32(len(s)))
		for _, x := range s {
			w.Varint32(x)
		}
	case tagLongArray:
		s := v.([]int64)
		w.Varint32(int32(len(s)))
		for _, x := range s {
			w.Varint64(x)
		}
	}
	return nil
}

func writeList(w *wire.Writer, l []any, depth int) error {
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
	w.Varint32(int32(len(l)))
	for _, v := range l {
		if vt, _ := tagOf(v); vt != t {
			return errListType
		}
		if err := writePayload(w, t, v, depth+1); err != nil {
			return err
		}
	}
	return nil
}

func readString(r *wire.Reader) string {
	n := r.Varuint32()
	if n > maxString {
		r.Fail(wire.ErrTooLong)
		return ""
	}
	return string(r.Next(int(n)))
}

func readCompound(r *wire.Reader, depth int) map[string]any {
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
		k := readString(r)
		m[k] = readPayload(r, t, depth)
	}
	return m
}

// count reads the length of an array or list, which takes at least min
// bytes an element.
func count(r *wire.Reader, min int) int {
	n := r.Varint32()
	if n < 0 || int(n)*min > r.Len() {
		r.Fail(wire.ErrShort)
		return 0
	}
	return int(n)
}

func readPayload(r *wire.Reader, t byte, depth int) any {
	switch t {
	case tagByte:
		return r.Int8()
	case tagShort:
		return r.Int16()
	case tagInt:
		return r.Varint32()
	case tagLong:
		return r.Varint64()
	case tagFloat:
		return r.Float32()
	case tagDouble:
		return r.Float64()
	case tagByteArray:
		n := count(r, 1)
		return append([]byte(nil), r.Next(n)...)
	case tagString:
		return readString(r)
	case tagList:
		if depth+1 == maxDepth {
			r.Fail(errDepth)
			return nil
		}
		et := r.Uint8()
		n := count(r, 1)
		if et == tagEnd && n > 0 || et > tagLongArray {
			r.Fail(errTagType)
			return nil
		}
		l := make([]any, 0, n)
		for range n {
			if r.Err() != nil {
				return nil
			}
			l = append(l, readPayload(r, et, depth+1))
		}
		return l
	case tagCompound:
		return readCompound(r, depth+1)
	case tagIntArray:
		n := count(r, 1)
		s := make([]int32, 0, n)
		for range n {
			s = append(s, r.Varint32())
		}
		return s
	case tagLongArray:
		n := count(r, 1)
		s := make([]int64, 0, n)
		for range n {
			s = append(s, r.Varint64())
		}
		return s
	}
	r.Fail(errTagType)
	return nil
}
