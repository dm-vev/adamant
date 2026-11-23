package bedrock

import (
	"encoding/binary"
	"errors"
	"math"
	"slices"
	"sync"
)

const (
	snappyTagLiteral = 0x00
	snappyTagCopy1   = 0x01
	snappyTagCopy2   = 0x02
	snappyTagCopy4   = 0x03

	snappyMaxOffset = 65535
)

var errSnappyCorrupt = errors.New("bedrock: corrupt snappy batch")

// Snappy compresses and decompresses batches in Google Snappy block format.
type Snappy struct{}

// Algorithm returns AlgorithmSnappy.
func (Snappy) Algorithm() uint16 { return AlgorithmSnappy }

// snappyMatchers pools [1<<matchBits]int32 hash tables with monotonic base
// offsets so steady-state Snappy compression does not allocate or zero tables.
var snappyMatchers sync.Pool

// Compress appends src compressed in Snappy block format to dst.
func (Snappy) Compress(dst, src []byte) ([]byte, error) {
	if uint64(len(src)) > math.MaxUint32 {
		return nil, ErrTooLarge
	}
	// Worst case: 10B varint + 5B literal header + src + 1B per 60B literal overhead + slack.
	maxOut := 32 + len(src) + len(src)/6
	start := len(dst)
	if cap(dst)-start < maxOut {
		grown := make([]byte, start, start+maxOut)
		copy(grown, dst)
		dst = grown
	}
	out := dst[:start+maxOut]
	pos := start + binary.PutUvarint(out[start:], uint64(len(src)))
	if len(src) < minMatch {
		if len(src) > 0 {
			pos = emitSnappyLiteral(out, pos, src)
		}
		return out[:pos], nil
	}
	pos = compressSnappyBody(out, pos, src)
	return out[:pos], nil
}

func compressSnappyBody(out []byte, pos int, src []byte) int {
	m, _ := snappyMatchers.Get().(*matcher)
	if m == nil {
		m = new(matcher)
	}
	m.start(len(src))
	base := m.base
	table := &m.table

	lit := 0
	limit := len(src) - minMatch
	for i, skip := 0, 32; i <= limit; {
		cur := binary.LittleEndian.Uint32(src[i:])
		h := (cur * 0x1e35a7bd) >> (32 - matchBits)
		cand := int(table[h]) - base
		table[h] = int32(i + base)
		if uint(cand) >= uint(i) || i-cand > snappyMaxOffset || binary.LittleEndian.Uint32(src[cand:]) != cur {
			i += skip >> 5
			skip++
			continue
		}
		for cand > 0 && i > lit && src[cand-1] == src[i-1] {
			cand--
			i--
		}
		n := minMatch + matchLen(src[cand+minMatch:], src[i+minMatch:])
		if lit < i {
			pos = emitSnappyLiteral(out, pos, src[lit:i])
		}
		pos = emitSnappyCopy(out, pos, n, i-cand)
		i += n
		lit, skip = i, 32
		if i <= limit {
			prev := binary.LittleEndian.Uint32(src[i-1:])
			table[(prev*0x1e35a7bd)>>(32-matchBits)] = int32(i - 1 + base)
		}
	}
	snappyMatchers.Put(m)

	if lit < len(src) {
		pos = emitSnappyLiteral(out, pos, src[lit:])
	}
	return pos
}

func emitSnappyLiteral(out []byte, pos int, lit []byte) int {
	n := len(lit) - 1
	switch {
	case n < 60:
		out[pos] = byte(n<<2) | snappyTagLiteral
		pos++
	case n < 1<<8:
		out[pos] = 60<<2 | snappyTagLiteral
		out[pos+1] = byte(n)
		pos += 2
	case n < 1<<16:
		out[pos] = 61<<2 | snappyTagLiteral
		binary.LittleEndian.PutUint16(out[pos+1:], uint16(n))
		pos += 3
	case n < 1<<24:
		out[pos] = 62<<2 | snappyTagLiteral
		out[pos+1] = byte(n)
		out[pos+2] = byte(n >> 8)
		out[pos+3] = byte(n >> 16)
		pos += 4
	default:
		out[pos] = 63<<2 | snappyTagLiteral
		binary.LittleEndian.PutUint32(out[pos+1:], uint32(n))
		pos += 5
	}
	return pos + copy(out[pos:], lit)
}

func emitSnappyCopy(out []byte, pos, length, offset int) int {
	for length >= 68 {
		out[pos] = 63<<2 | snappyTagCopy2
		binary.LittleEndian.PutUint16(out[pos+1:], uint16(offset))
		pos += 3
		length -= 64
	}
	if length > 64 {
		out[pos] = 59<<2 | snappyTagCopy2
		binary.LittleEndian.PutUint16(out[pos+1:], uint16(offset))
		pos += 3
		length -= 60
	}
	if length < 12 && offset < 2048 {
		out[pos] = byte((offset>>8)<<5) | byte((length-4)<<2) | snappyTagCopy1
		out[pos+1] = byte(offset)
		return pos + 2
	}
	out[pos] = byte((length-1)<<2) | snappyTagCopy2
	binary.LittleEndian.PutUint16(out[pos+1:], uint16(offset))
	return pos + 3
}

// Decompress appends Snappy block src decompressed to dst, failing with
// ErrTooLarge if that would exceed limit bytes.
func (Snappy) Decompress(dst, src []byte, limit int) ([]byte, error) {
	uLen, hdr := binary.Uvarint(src)
	if hdr <= 0 || uLen > math.MaxUint32 {
		return nil, errSnappyCorrupt
	}
	if limit < 0 || uLen > uint64(limit) {
		return nil, ErrTooLarge
	}
	dLen := int(uLen)
	// Maximum copy expansion is 64 bytes per 3-byte copy2 tag (< 22x payload).
	if uint64(dLen) > uint64(len(src)-hdr)*22 {
		return nil, errSnappyCorrupt
	}
	start := len(dst)
	dst = slices.Grow(dst, dLen)[:start+dLen]
	if err := decompressSnappy(dst[start:start+dLen], src[hdr:]); err != nil {
		return nil, err
	}
	return dst, nil
}

func decompressSnappy(out, src []byte) error {
	d, s := 0, 0
	for s < len(src) {
		tag := src[s]
		var length int
		var offset uint64
		switch tag & 0x03 {
		case snappyTagLiteral:
			n, next, ok := decodeSnappyLiteralLen(tag, src, s)
			if !ok || n > uint64(len(out)-d) || n > uint64(len(src)-next) {
				return errSnappyCorrupt
			}
			ln := int(n)
			copy(out[d:], src[next:next+ln])
			d += ln
			s = next + ln
			continue
		case snappyTagCopy1:
			if len(src)-s < 2 {
				return errSnappyCorrupt
			}
			length = 4 + int(tag>>2&0x7)
			offset = uint64(uint32(tag&0xe0)<<3 | uint32(src[s+1]))
			s += 2
		case snappyTagCopy2:
			if len(src)-s < 3 {
				return errSnappyCorrupt
			}
			length = 1 + int(tag>>2)
			offset = uint64(binary.LittleEndian.Uint16(src[s+1:]))
			s += 3
		case snappyTagCopy4:
			if len(src)-s < 5 {
				return errSnappyCorrupt
			}
			length = 1 + int(tag>>2)
			offset = uint64(binary.LittleEndian.Uint32(src[s+1:]))
			s += 5
		}
		if offset == 0 || offset > uint64(d) || length > len(out)-d {
			return errSnappyCorrupt
		}
		snappyCopyBack(out, d, int(offset), length)
		d += length
	}
	if d != len(out) {
		return errSnappyCorrupt
	}
	return nil
}

func decodeSnappyLiteralLen(tag byte, src []byte, s int) (uint64, int, bool) {
	x := uint64(tag >> 2)
	s++
	if x < 60 {
		return x + 1, s, true
	}
	nbytes := int(x - 59)
	if len(src)-s < nbytes {
		return 0, 0, false
	}
	var v uint64
	for i := range nbytes {
		v |= uint64(src[s+i]) << (8 * i)
	}
	return v + 1, s + nbytes, true
}

func snappyCopyBack(out []byte, d, offset, length int) {
	if offset >= length {
		copy(out[d:d+length], out[d-offset:])
		return
	}
	dst := out[d : d+length]
	src := out[d-offset:]
	src = src[:len(dst)]
	for i := range dst {
		dst[i] = src[i]
	}
}
