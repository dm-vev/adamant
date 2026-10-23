package mc112

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
)

const (
	nbtTagEnd       = 0
	nbtTagByte      = 1
	nbtTagShort     = 2
	nbtTagInt       = 3
	nbtTagLong      = 4
	nbtTagFloat     = 5
	nbtTagDouble    = 6
	nbtTagByteArray = 7
	nbtTagString    = 8
	nbtTagList      = 9
	nbtTagCompound  = 10
	nbtTagIntArray  = 11
	nbtTagLongArray = 12
)

type nbtReader struct {
	r *bytes.Reader
}

func newNBTReader(b []byte) *nbtReader {
	return &nbtReader{r: bytes.NewReader(b)}
}

func (r *nbtReader) readByte() (byte, error) {
	b, err := r.r.ReadByte()
	if err != nil {
		return 0, err
	}
	return b, nil
}

func (r *nbtReader) readInt16() (int16, error) {
	var b [2]byte
	if _, err := io.ReadFull(r.r, b[:]); err != nil {
		return 0, err
	}
	return int16(binary.BigEndian.Uint16(b[:])), nil
}

func (r *nbtReader) readInt32() (int32, error) {
	var b [4]byte
	if _, err := io.ReadFull(r.r, b[:]); err != nil {
		return 0, err
	}
	return int32(binary.BigEndian.Uint32(b[:])), nil
}

func (r *nbtReader) readInt64() (int64, error) {
	var b [8]byte
	if _, err := io.ReadFull(r.r, b[:]); err != nil {
		return 0, err
	}
	return int64(binary.BigEndian.Uint64(b[:])), nil
}

func (r *nbtReader) readFloat32() (float32, error) {
	v, err := r.readInt32()
	if err != nil {
		return 0, err
	}
	return math.Float32frombits(uint32(v)), nil
}

func (r *nbtReader) readFloat64() (float64, error) {
	v, err := r.readInt64()
	if err != nil {
		return 0, err
	}
	return math.Float64frombits(uint64(v)), nil
}

func (r *nbtReader) readString() (string, error) {
	n, err := r.readInt16()
	if err != nil {
		return "", err
	}
	if n < 0 {
		return "", fmt.Errorf("nbt: negative string length %d", n)
	}
	if n == 0 {
		return "", nil
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(r.r, b); err != nil {
		return "", err
	}
	return string(b), nil
}

func (r *nbtReader) readByteArray() ([]byte, error) {
	n, err := r.readInt32()
	if err != nil {
		return nil, err
	}
	if n < 0 {
		return nil, fmt.Errorf("nbt: negative byte array length %d", n)
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(r.r, b); err != nil {
		return nil, err
	}
	return b, nil
}

func (r *nbtReader) readInt32Array() ([]int32, error) {
	n, err := r.readInt32()
	if err != nil {
		return nil, err
	}
	if n < 0 {
		return nil, fmt.Errorf("nbt: negative int array length %d", n)
	}
	a := make([]int32, n)
	for i := int32(0); i < n; i++ {
		v, err := r.readInt32()
		if err != nil {
			return nil, err
		}
		a[i] = v
	}
	return a, nil
}

func (r *nbtReader) readInt64Array() ([]int64, error) {
	n, err := r.readInt32()
	if err != nil {
		return nil, err
	}
	if n < 0 {
		return nil, fmt.Errorf("nbt: negative long array length %d", n)
	}
	a := make([]int64, n)
	for i := int32(0); i < n; i++ {
		v, err := r.readInt64()
		if err != nil {
			return nil, err
		}
		a[i] = v
	}
	return a, nil
}

func (r *nbtReader) readTagName() (string, error) {
	return r.readString()
}

func (r *nbtReader) readPayload(tagType byte, depth int) (any, error) {
	if depth > 512 {
		return nil, fmt.Errorf("nbt: maximum nesting depth exceeded")
	}

	switch tagType {
	case nbtTagEnd:
		return nil, nil
	case nbtTagByte:
		v, err := r.readByte()
		return int8(v), err
	case nbtTagShort:
		v, err := r.readInt16()
		return v, err
	case nbtTagInt:
		v, err := r.readInt32()
		return v, err
	case nbtTagLong:
		v, err := r.readInt64()
		return v, err
	case nbtTagFloat:
		v, err := r.readFloat32()
		return v, err
	case nbtTagDouble:
		v, err := r.readFloat64()
		return v, err
	case nbtTagByteArray:
		return r.readByteArray()
	case nbtTagString:
		return r.readString()
	case nbtTagList:
		elemType, err := r.readByte()
		if err != nil {
			return nil, err
		}
		length, err := r.readInt32()
		if err != nil {
			return nil, err
		}
		if length < 0 {
			return nil, fmt.Errorf("nbt: negative list length %d", length)
		}

		switch elemType {
		case nbtTagEnd:
			// An empty list may use TAG_End as its element type.
			if length != 0 {
				return nil, fmt.Errorf("nbt: non-empty list with end element type")
			}
			return []any{}, nil
		case nbtTagInt:
			a := make([]int32, length)
			for i := int32(0); i < length; i++ {
				v, err := r.readInt32()
				if err != nil {
					return nil, err
				}
				a[i] = v
			}
			return a, nil
		case nbtTagLong:
			a := make([]int64, length)
			for i := int32(0); i < length; i++ {
				v, err := r.readInt64()
				if err != nil {
					return nil, err
				}
				a[i] = v
			}
			return a, nil
		case nbtTagByte:
			a := make([]int8, length)
			for i := int32(0); i < length; i++ {
				v, err := r.readByte()
				if err != nil {
					return nil, err
				}
				a[i] = int8(v)
			}
			return a, nil
		case nbtTagCompound:
			a := make([]map[string]any, length)
			for i := int32(0); i < length; i++ {
				v, err := r.readCompound(depth + 1)
				if err != nil {
					return nil, err
				}
				a[i] = v
			}
			return a, nil
		default:
			// Generic slow-path.
			a := make([]any, length)
			for i := int32(0); i < length; i++ {
				v, err := r.readPayload(elemType, depth+1)
				if err != nil {
					return nil, err
				}
				a[i] = v
			}
			return a, nil
		}
	case nbtTagCompound:
		return r.readCompound(depth + 1)
	case nbtTagIntArray:
		return r.readInt32Array()
	case nbtTagLongArray:
		return r.readInt64Array()
	default:
		return nil, fmt.Errorf("nbt: unsupported tag type %d", tagType)
	}
}

func (r *nbtReader) readCompound(depth int) (map[string]any, error) {
	m := map[string]any{}
	for {
		t, err := r.readByte()
		if err != nil {
			return nil, err
		}
		if t == nbtTagEnd {
			return m, nil
		}
		name, err := r.readTagName()
		if err != nil {
			return nil, err
		}
		v, err := r.readPayload(t, depth)
		if err != nil {
			return nil, err
		}
		m[name] = v
	}
}

// DecodeNBT decodes a (decompressed) Java Edition NBT payload into a root compound.
func DecodeNBT(b []byte) (map[string]any, error) {
	r := newNBTReader(b)
	rootType, err := r.readByte()
	if err != nil {
		return nil, err
	}
	if rootType != nbtTagCompound {
		return nil, fmt.Errorf("nbt: root tag type %d, expected compound", rootType)
	}
	// Root name is ignored.
	if _, err := r.readTagName(); err != nil {
		return nil, err
	}
	return r.readCompound(1)
}
