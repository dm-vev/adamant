package mc112

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
)

const (
	javaNBTTagEnd       = 0
	javaNBTTagByte      = 1
	javaNBTTagShort     = 2
	javaNBTTagInt       = 3
	javaNBTTagLong      = 4
	javaNBTTagFloat     = 5
	javaNBTTagDouble    = 6
	javaNBTTagByteArray = 7
	javaNBTTagString    = 8
	javaNBTTagList      = 9
	javaNBTTagCompound  = 10
	javaNBTTagIntArray  = 11
	javaNBTTagLongArray = 12
)

type javaNBTReader struct {
	r *bytes.Reader
}

func newJavaNBTReader(b []byte) *javaNBTReader {
	return &javaNBTReader{r: bytes.NewReader(b)}
}

func (r *javaNBTReader) readByte() (byte, error) {
	b, err := r.r.ReadByte()
	if err != nil {
		return 0, err
	}
	return b, nil
}

func (r *javaNBTReader) readInt16() (int16, error) {
	var b [2]byte
	if _, err := io.ReadFull(r.r, b[:]); err != nil {
		return 0, err
	}
	return int16(binary.BigEndian.Uint16(b[:])), nil
}

func (r *javaNBTReader) readInt32() (int32, error) {
	var b [4]byte
	if _, err := io.ReadFull(r.r, b[:]); err != nil {
		return 0, err
	}
	return int32(binary.BigEndian.Uint32(b[:])), nil
}

func (r *javaNBTReader) readInt64() (int64, error) {
	var b [8]byte
	if _, err := io.ReadFull(r.r, b[:]); err != nil {
		return 0, err
	}
	return int64(binary.BigEndian.Uint64(b[:])), nil
}

func (r *javaNBTReader) readFloat32() (float32, error) {
	v, err := r.readInt32()
	if err != nil {
		return 0, err
	}
	return math.Float32frombits(uint32(v)), nil
}

func (r *javaNBTReader) readFloat64() (float64, error) {
	v, err := r.readInt64()
	if err != nil {
		return 0, err
	}
	return math.Float64frombits(uint64(v)), nil
}

func (r *javaNBTReader) readString() (string, error) {
	n, err := r.readInt16()
	if err != nil {
		return "", err
	}
	if n < 0 {
		return "", fmt.Errorf("java nbt: negative string length %d", n)
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

func (r *javaNBTReader) readByteArray() ([]byte, error) {
	n, err := r.readInt32()
	if err != nil {
		return nil, err
	}
	if n < 0 {
		return nil, fmt.Errorf("java nbt: negative byte array length %d", n)
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(r.r, b); err != nil {
		return nil, err
	}
	return b, nil
}

func (r *javaNBTReader) readInt32Array() ([]int32, error) {
	n, err := r.readInt32()
	if err != nil {
		return nil, err
	}
	if n < 0 {
		return nil, fmt.Errorf("java nbt: negative int array length %d", n)
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

func (r *javaNBTReader) readInt64Array() ([]int64, error) {
	n, err := r.readInt32()
	if err != nil {
		return nil, err
	}
	if n < 0 {
		return nil, fmt.Errorf("java nbt: negative long array length %d", n)
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

func (r *javaNBTReader) readTagName() (string, error) {
	return r.readString()
}

func (r *javaNBTReader) readPayload(tagType byte, depth int) (any, error) {
	if depth > 512 {
		return nil, fmt.Errorf("java nbt: maximum nesting depth exceeded")
	}

	switch tagType {
	case javaNBTTagEnd:
		return nil, nil
	case javaNBTTagByte:
		v, err := r.readByte()
		return int8(v), err
	case javaNBTTagShort:
		v, err := r.readInt16()
		return v, err
	case javaNBTTagInt:
		v, err := r.readInt32()
		return v, err
	case javaNBTTagLong:
		v, err := r.readInt64()
		return v, err
	case javaNBTTagFloat:
		v, err := r.readFloat32()
		return v, err
	case javaNBTTagDouble:
		v, err := r.readFloat64()
		return v, err
	case javaNBTTagByteArray:
		return r.readByteArray()
	case javaNBTTagString:
		return r.readString()
	case javaNBTTagList:
		elemType, err := r.readByte()
		if err != nil {
			return nil, err
		}
		length, err := r.readInt32()
		if err != nil {
			return nil, err
		}
		if length < 0 {
			return nil, fmt.Errorf("java nbt: negative list length %d", length)
		}

		switch elemType {
		case javaNBTTagEnd:
			// An empty list may use TAG_End as its element type.
			if length != 0 {
				return nil, fmt.Errorf("java nbt: non-empty list with end element type")
			}
			return []any{}, nil
		case javaNBTTagInt:
			a := make([]int32, length)
			for i := int32(0); i < length; i++ {
				v, err := r.readInt32()
				if err != nil {
					return nil, err
				}
				a[i] = v
			}
			return a, nil
		case javaNBTTagLong:
			a := make([]int64, length)
			for i := int32(0); i < length; i++ {
				v, err := r.readInt64()
				if err != nil {
					return nil, err
				}
				a[i] = v
			}
			return a, nil
		case javaNBTTagByte:
			a := make([]int8, length)
			for i := int32(0); i < length; i++ {
				v, err := r.readByte()
				if err != nil {
					return nil, err
				}
				a[i] = int8(v)
			}
			return a, nil
		case javaNBTTagCompound:
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
	case javaNBTTagCompound:
		return r.readCompound(depth + 1)
	case javaNBTTagIntArray:
		return r.readInt32Array()
	case javaNBTTagLongArray:
		return r.readInt64Array()
	default:
		return nil, fmt.Errorf("java nbt: unsupported tag type %d", tagType)
	}
}

func (r *javaNBTReader) readCompound(depth int) (map[string]any, error) {
	m := map[string]any{}
	for {
		t, err := r.readByte()
		if err != nil {
			return nil, err
		}
		if t == javaNBTTagEnd {
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

// decodeJavaNBT decodes a (decompressed) Java Edition NBT payload into a root compound.
func decodeJavaNBT(b []byte) (map[string]any, error) {
	r := newJavaNBTReader(b)
	rootType, err := r.readByte()
	if err != nil {
		return nil, err
	}
	if rootType != javaNBTTagCompound {
		return nil, fmt.Errorf("java nbt: root tag type %d, expected compound", rootType)
	}
	// Root name is ignored.
	if _, err := r.readTagName(); err != nil {
		return nil, err
	}
	return r.readCompound(1)
}

