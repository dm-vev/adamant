package bedrock

import (
	"encoding/binary"
	"math/bits"
	"sync"
)

// fixedDeflate appends src to dst as raw DEFLATE (RFC 1951) in one block
// of the fixed Huffman codes. Building the code tables of a dynamic block
// costs compress/flate more than finding the matches on the small batches
// a server sends every tick, and on those the tables a dynamic block
// carries take most of what they save.
func fixedDeflate(dst, src []byte) []byte {
	m, _ := matchers.Get().(*matcher)
	if m == nil {
		m = new(matcher)
	}
	defer matchers.Put(m)
	m.start(len(src))
	w := bitWriter{dst: dst}
	// The last block, of the fixed codes: BFINAL 1, BTYPE 01.
	w.write(0b011, 3)
	lit := 0
	for i, skip := 0, 32; i+minMatch <= len(src); {
		cur := binary.LittleEndian.Uint32(src[i:])
		h := cur * 0x1e35a7bd >> (32 - matchBits)
		cand := int(m.table[h]) - m.base
		m.table[h] = int32(i + m.base)
		if cand < 0 || i-cand > window || binary.LittleEndian.Uint32(src[cand:]) != cur {
			// Data that does not match goes faster the longer it does not.
			i += skip >> 5
			skip++
			continue
		}
		n := minMatch + matchLen(src[cand+minMatch:], src[i+minMatch:min(len(src), i+maxMatch)])
		for _, b := range src[lit:i] {
			w.write(literalCodes[b], uint(literalBits[b]))
		}
		w.match(n, i-cand)
		i += n
		lit, skip = i, 32
	}
	for _, b := range src[lit:] {
		w.write(literalCodes[b], uint(literalBits[b]))
	}
	// The end of the block, code 256: seven zero bits.
	w.write(0, 7)
	return w.flush()
}

const (
	minMatch  = 4
	maxMatch  = 258
	window    = 32768
	matchBits = 14
)

// matcher finds earlier occurrences of four bytes by a hash table of their
// last positions. Positions are stored plus base, which grows with each
// input, so that entries of earlier inputs read as negative without the
// table being cleared.
type matcher struct {
	table [1 << matchBits]int32
	base  int
	next  int
}

var matchers sync.Pool

func (m *matcher) start(n int) {
	if m.next > 1<<30-n {
		clear(m.table[:])
		m.next = 0
	}
	m.base = m.next + 1
	m.next = m.base + n
}

// matchLen returns how many bytes a and b share from their start, up to
// the length of b.
func matchLen(a, b []byte) int {
	n := 0
	for len(b)-n >= 8 && len(a)-n >= 8 {
		x := binary.LittleEndian.Uint64(a[n:]) ^ binary.LittleEndian.Uint64(b[n:])
		if x != 0 {
			return n + bits.TrailingZeros64(x)/8
		}
		n += 8
	}
	for n < len(b) && n < len(a) && a[n] == b[n] {
		n++
	}
	return n
}

// bitWriter packs bits from the lowest, as DEFLATE does.
type bitWriter struct {
	dst   []byte
	bits  uint64
	nbits uint
}

// write writes the n low bits of v; n is at most 32.
func (w *bitWriter) write(v uint32, n uint) {
	w.bits |= uint64(v) << w.nbits
	w.nbits += n
	if w.nbits >= 32 {
		w.dst = binary.LittleEndian.AppendUint32(w.dst, uint32(w.bits))
		w.bits >>= 32
		w.nbits -= 32
	}
}

// match writes a match of n bytes at a distance: the code of the length
// and its extra bits, then those of the distance.
func (w *bitWriter) match(n, dist int) {
	w.write(lengthCodes[n-3], uint(lengthBits[n-3]))
	d := uint32(dist - 1)
	if d < 4 {
		w.write(distanceCodes[d], 5)
		return
	}
	extra := uint(bits.Len32(d)) - 2
	code := 2*extra + 2 + uint(d>>extra)&1
	w.write(distanceCodes[code]|(d&(1<<extra-1))<<5, 5+extra)
}

func (w *bitWriter) flush() []byte {
	for w.nbits > 0 {
		w.dst = append(w.dst, byte(w.bits))
		w.bits >>= 8
		w.nbits -= min(w.nbits, 8)
	}
	return w.dst
}

// The fixed codes, bit-reversed to be written from the lowest bit:
// literalCodes for bytes, lengthCodes for match lengths from 3 with their
// extra bits above the code, distanceCodes for the 30 distance codes.
var (
	literalCodes  [256]uint32
	literalBits   [256]uint8
	lengthCodes   [maxMatch - 2]uint32
	lengthBits    [maxMatch - 2]uint8
	distanceCodes [30]uint32
)

func init() {
	reverse := func(code uint32, n uint) uint32 { return uint32(bits.Reverse16(uint16(code)) >> (16 - n)) }
	for b := range 256 {
		if b < 144 {
			literalCodes[b], literalBits[b] = reverse(0x30+uint32(b), 8), 8
		} else {
			literalCodes[b], literalBits[b] = reverse(0x190+uint32(b-144), 9), 9
		}
	}
	// symbol returns the code of a length symbol 257 to 285.
	symbol := func(s int) (uint32, uint) {
		if s < 280 {
			return reverse(uint32(s-256), 7), 7
		}
		return reverse(0xc0+uint32(s-280), 8), 8
	}
	for n := 3; n <= maxMatch; n++ {
		l := n - 3
		var s, extra int
		switch {
		case n == maxMatch:
			s = 285
		case l < 8:
			s = 257 + l
		default:
			extra = bits.Len(uint(l)) - 3
			s = 257 + 4*extra + 4 + l>>extra&3
		}
		code, cn := symbol(s)
		lengthCodes[l] = code | uint32(l&(1<<extra-1))<<cn
		lengthBits[l] = uint8(cn) + uint8(extra)
	}
	for d := range distanceCodes {
		distanceCodes[d] = reverse(uint32(d), 5)
	}
}
