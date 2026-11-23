package bedrock

import (
	"encoding/binary"
	mathbits "math/bits"
	"sync"
)

// deflateFinalEmpty closes a stream of sync-flushed non-final DEFLATE blocks
// with a final empty stored block (BFINAL=1, BTYPE=00, LEN=0, NLEN=0xffff).
var deflateFinalEmpty = [5]byte{0x01, 0x00, 0x00, 0xFF, 0xFF}

// fixedDeflate appends src to dst as raw DEFLATE (RFC 1951) in one final
// block of the fixed Huffman codes. Building the code tables of a dynamic
// block costs compress/flate more than finding the matches on the small batches
// a server sends every tick, and on those the tables a dynamic block carries
// take most of what they save.
func fixedDeflate(dst, src []byte) []byte {
	return fixedDeflateBlock(dst, src, true)
}

// fixedDeflateBlock appends src to dst as a raw DEFLATE fixed-Huffman block.
// When final is false, it emits BFINAL=0, BTYPE=01 followed by a non-final empty
// sync-flush stored block (BFINAL=0, BTYPE=00, 0-padded to byte boundary,
// 0x00, 0x00, 0xFF, 0xFF) so blocks can be concatenated on byte boundaries.
func fixedDeflateBlock(dst, src []byte, final bool) []byte {
	// Every literal takes at most 9 bits and every match (length >= 4) takes at
	// most 31 bits (< 4*9), so the worst-case output is 9*len(src)+20 bits plus
	// 4 bytes for non-final sync-flush and 8 bytes slack for 64-bit stores.
	maxOut := len(src) + len(src)>>3 + 24
	start := len(dst)
	if cap(dst)-start < maxOut {
		grown := make([]byte, start, start+maxOut)
		copy(grown, dst)
		dst = grown
	}
	out := dst[:start+maxOut]
	pos := start
	// Fixed Huffman block: BTYPE 01, with BFINAL 1 when final or 0 otherwise.
	var bits uint64 = 0b010
	if final {
		bits = 0b011
	}
	var nbits uint = 3

	if len(src) < minMatch {
		pos, bits, nbits = emitLiterals(out, pos, bits, nbits, src)
		return finishFixedDeflate(out, pos, bits, nbits, final)
	}

	m, _ := matchers.Get().(*matcher)
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
		if uint(cand) >= uint(i) || i-cand > window || binary.LittleEndian.Uint32(src[cand:]) != cur {
			// Data that does not match goes faster the longer it does not.
			i += skip >> 5
			skip++
			continue
		}
		end := min(len(src), i+maxMatch)
		n := minMatch + matchLen(src[cand+minMatch:], src[i+minMatch:end])
		if lit < i {
			pos, bits, nbits = emitLiterals(out, pos, bits, nbits, src[lit:i])
		}
		pos, bits, nbits = emitMatch(out, pos, bits, nbits, n, i-cand)
		i += n
		lit, skip = i, 32
	}
	matchers.Put(m)

	if lit < len(src) {
		pos, bits, nbits = emitLiterals(out, pos, bits, nbits, src[lit:])
	}
	return finishFixedDeflate(out, pos, bits, nbits, final)
}

func finishFixedDeflate(out []byte, pos int, bits uint64, nbits uint, final bool) []byte {
	// The end of the block, code 256: seven zero bits.
	nbits += 7
	if final {
		binary.LittleEndian.PutUint64(out[pos:], bits)
		pos += int((nbits + 7) >> 3)
		return out[:pos]
	}
	// Non-final empty sync-flush stored block: BFINAL=0, BTYPE=00 (3 zero bits),
	// zero-padded to byte boundary, then LEN=0x0000 and NLEN=0xFFFF.
	nbits += 3
	binary.LittleEndian.PutUint64(out[pos:], bits)
	pos += int((nbits + 7) >> 3)
	binary.LittleEndian.PutUint32(out[pos:], 0xffff0000)
	return out[:pos+4]
}

// emitLiterals writes fixed Huffman codes for lit into out starting at pos.
// On entry and exit nbits < 8, and out has sufficient slack for PutUint64.
func emitLiterals(out []byte, pos int, bits uint64, nbits uint, lit []byte) (int, uint64, uint) {
	i := 0
	for ; i+6 <= len(lit); i += 6 {
		s := lit[i : i+6]
		c0, c1, c2 := litTable[s[0]], litTable[s[1]], litTable[s[2]]
		l0, l1, l2 := uint(c0&0xf), uint(c1&0xf), uint(c2&0xf)
		vA := uint64(c0>>4) | uint64(c1>>4)<<l0 | uint64(c2>>4)<<(l0+l1)
		wA := l0 + l1 + l2

		c3, c4, c5 := litTable[s[3]], litTable[s[4]], litTable[s[5]]
		l3, l4, l5 := uint(c3&0xf), uint(c4&0xf), uint(c5&0xf)
		vB := uint64(c3>>4) | uint64(c4>>4)<<l3 | uint64(c5>>4)<<(l3+l4)
		wB := l3 + l4 + l5

		bits |= (vA | vB<<wA) << nbits
		nbits += wA + wB
		binary.LittleEndian.PutUint64(out[pos:], bits)
		n := nbits >> 3
		pos += int(n)
		bits >>= n * 8
		nbits &= 7
	}
	for ; i < len(lit); i++ {
		c := litTable[lit[i]]
		bits |= uint64(c>>4) << nbits
		nbits += uint(c & 0xf)
	}
	if nbits >= 8 {
		binary.LittleEndian.PutUint64(out[pos:], bits)
		n := nbits >> 3
		pos += int(n)
		bits >>= n * 8
		nbits &= 7
	}
	return pos, bits, nbits
}

// emitMatch writes a match of length n at distance dist.
// On entry and exit nbits < 8; length (<=13 bits) + distance (<=18 bits) + 7 <= 38 <= 64.
func emitMatch(out []byte, pos int, bits uint64, nbits uint, n, dist int) (int, uint64, uint) {
	lc := lengthTable[n-3]
	bits |= uint64(lc>>4) << nbits
	nbits += uint(lc & 0xf)
	d := uint32(dist - 1)
	if d < 4 {
		bits |= uint64(distanceCodes[d]) << nbits
		nbits += 5
	} else {
		extra := uint(mathbits.Len32(d)) - 2
		code := 2*extra + 2 + uint(d>>extra)&1
		bits |= uint64(distanceCodes[code]|(d&(1<<extra-1))<<5) << nbits
		nbits += 5 + extra
	}
	binary.LittleEndian.PutUint64(out[pos:], bits)
	nb := nbits >> 3
	pos += int(nb)
	bits >>= nb * 8
	nbits &= 7
	return pos, bits, nbits
}

const (
	minMatch  = 4
	maxMatch  = 258
	window    = 32768
	matchBits = 12
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
// the shorter of the two slices.
func matchLen(a, b []byte) int {
	if len(a) < len(b) {
		b = b[:len(a)]
	}
	n := 0
	for n+8 <= len(b) {
		x := binary.LittleEndian.Uint64(a[n:]) ^ binary.LittleEndian.Uint64(b[n:])
		if x != 0 {
			return n + mathbits.TrailingZeros64(x)>>3
		}
		n += 8
	}
	for n < len(b) && a[n] == b[n] {
		n++
	}
	return n
}

// The fixed codes, bit-reversed to be written from the lowest bit:
// litTable packs code<<4 | nbits for bytes 0..255, lengthTable packs
// (code|extraBits)<<4 | totalBits for match lengths 3..258, and
// distanceCodes holds the 30 5-bit distance codes.
var (
	litTable      [256]uint16
	lengthTable   [maxMatch - 2]uint32
	distanceCodes [30]uint32
)

func init() {
	reverse := func(code uint32, n uint) uint32 { return uint32(mathbits.Reverse16(uint16(code)) >> (16 - n)) }
	for b := range 256 {
		if b < 144 {
			litTable[b] = uint16(reverse(0x30+uint32(b), 8)<<4 | 8)
		} else {
			litTable[b] = uint16(reverse(0x190+uint32(b-144), 9)<<4 | 9)
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
			extra = mathbits.Len(uint(l)) - 3
			s = 257 + 4*extra + 4 + l>>extra&3
		}
		code, cn := symbol(s)
		combined := code | uint32(l&(1<<extra-1))<<cn
		lengthTable[l] = combined<<4 | uint32(cn+uint(extra))
	}
	for d := range distanceCodes {
		distanceCodes[d] = reverse(uint32(d), 5)
	}
}
