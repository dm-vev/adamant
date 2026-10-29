package sctp

import (
	"encoding/binary"
	"errors"
	"hash/crc32"
)

// Chunk types.
const (
	chunkData             = 0
	chunkInit             = 1
	chunkInitAck          = 2
	chunkSack             = 3
	chunkHeartbeat        = 4
	chunkHeartbeatAck     = 5
	chunkAbort            = 6
	chunkShutdown         = 7
	chunkShutdownAck      = 8
	chunkError            = 9
	chunkCookieEcho       = 10
	chunkCookieAck        = 11
	chunkShutdownComplete = 14
	chunkReconfig         = 130
	chunkForwardTSN       = 192
)

// DATA chunk flags.
const (
	flagEnd       = 0x01
	flagBegin     = 0x02
	flagUnordered = 0x04
	flagImmediate = 0x08
)

// Parameter types.
const (
	paramStateCookie        = 7
	paramOutgoingReset      = 13
	paramReconfigResponse   = 16
	paramSupportedExtension = 0x8008
	paramForwardTSN         = 0xc000
)

// Sizes of the headers of a packet and of chunks.
const (
	commonHeaderSize = 12
	chunkHeaderSize  = 4
	dataHeaderSize   = 16
)

var castagnoli = crc32.MakeTable(crc32.Castagnoli)

var errPacket = errors.New("sctp: malformed packet")

// header is the common header of an SCTP packet.
type header struct {
	srcPort, dstPort uint16
	tag              uint32
}

// chunk is a chunk of a received packet; value refers into the packet.
type chunk struct {
	typ   byte
	flags byte
	value []byte
}

// parsePacket checks the CRC32c of a packet and splits it into chunks.
func parsePacket(b []byte, chunks []chunk) (header, []chunk, error) {
	if len(b) < commonHeaderSize+chunkHeaderSize {
		return header{}, nil, errPacket
	}
	want := binary.LittleEndian.Uint32(b[8:])
	sum := crc32.Update(0, castagnoli, b[:8])
	sum = crc32.Update(sum, castagnoli, []byte{0, 0, 0, 0})
	sum = crc32.Update(sum, castagnoli, b[12:])
	if sum != want {
		return header{}, nil, errPacket
	}
	h := header{
		srcPort: binary.BigEndian.Uint16(b),
		dstPort: binary.BigEndian.Uint16(b[2:]),
		tag:     binary.BigEndian.Uint32(b[4:]),
	}
	chunks = chunks[:0]
	for off := commonHeaderSize; off < len(b); {
		if off+chunkHeaderSize > len(b) {
			return header{}, nil, errPacket
		}
		length := int(binary.BigEndian.Uint16(b[off+2:]))
		if length < chunkHeaderSize || off+length > len(b) {
			return header{}, nil, errPacket
		}
		chunks = append(chunks, chunk{typ: b[off], flags: b[off+1], value: b[off+chunkHeaderSize : off+length]})
		off += (length + 3) &^ 3
	}
	return h, chunks, nil
}

// packetBuilder appends chunks to a packet.
type packetBuilder struct {
	buf []byte
}

func (p *packetBuilder) start(h header) {
	p.buf = binary.BigEndian.AppendUint16(p.buf[:0], h.srcPort)
	p.buf = binary.BigEndian.AppendUint16(p.buf, h.dstPort)
	p.buf = binary.BigEndian.AppendUint32(p.buf, h.tag)
	p.buf = append(p.buf, 0, 0, 0, 0)
}

func (p *packetBuilder) empty() bool { return len(p.buf) <= commonHeaderSize }

func (p *packetBuilder) size() int { return len(p.buf) }

// addChunk appends a chunk whose value is the concatenation of parts.
func (p *packetBuilder) addChunk(typ, flags byte, parts ...[]byte) {
	length := chunkHeaderSize
	for _, v := range parts {
		length += len(v)
	}
	p.buf = append(p.buf, typ, flags)
	p.buf = binary.BigEndian.AppendUint16(p.buf, uint16(length))
	for _, v := range parts {
		p.buf = append(p.buf, v...)
	}
	for len(p.buf)%4 != 0 {
		p.buf = append(p.buf, 0)
	}
}

// finish writes the checksum and returns the packet.
func (p *packetBuilder) finish() []byte {
	binary.LittleEndian.PutUint32(p.buf[8:], crc32.Checksum(p.buf, castagnoli))
	return p.buf
}

// chunkSize returns the padded size of a chunk with a value of n bytes.
func chunkSize(n int) int { return (chunkHeaderSize + n + 3) &^ 3 }

// param is a parameter of an INIT or INIT ACK chunk.
type param struct {
	typ   uint16
	value []byte
}

func parseParams(b []byte) ([]param, bool) {
	var params []param
	for off := 0; off < len(b); {
		if off+4 > len(b) {
			return nil, false
		}
		typ := binary.BigEndian.Uint16(b[off:])
		length := int(binary.BigEndian.Uint16(b[off+2:]))
		if length < 4 || off+length > len(b) {
			return nil, false
		}
		params = append(params, param{typ: typ, value: b[off+4 : off+length]})
		off += (length + 3) &^ 3
	}
	return params, true
}

func appendParam(dst []byte, typ uint16, value []byte) []byte {
	dst = binary.BigEndian.AppendUint16(dst, typ)
	dst = binary.BigEndian.AppendUint16(dst, uint16(4+len(value)))
	dst = append(dst, value...)
	for len(dst)%4 != 0 {
		dst = append(dst, 0)
	}
	return dst
}

// initChunk is the value of an INIT or INIT ACK chunk.
type initChunk struct {
	tag        uint32
	window     uint32
	outStreams uint16
	inStreams  uint16
	tsn        uint32
	forwardTSN bool
	reconfig   bool
	cookie     []byte
	// extensions and params are the chunk types of its Supported
	// Extensions and the types of its parameters, as the peer offered
	// them.
	extensions []byte
	params     []uint16
}

// maxPeerFeatures bounds the parameters and extensions of an INIT kept.
const maxPeerFeatures = 32

func parseInit(v []byte) (initChunk, bool) {
	if len(v) < 16 {
		return initChunk{}, false
	}
	c := initChunk{
		tag:        binary.BigEndian.Uint32(v),
		window:     binary.BigEndian.Uint32(v[4:]),
		outStreams: binary.BigEndian.Uint16(v[8:]),
		inStreams:  binary.BigEndian.Uint16(v[10:]),
		tsn:        binary.BigEndian.Uint32(v[12:]),
	}
	if c.tag == 0 || c.outStreams == 0 || c.inStreams == 0 {
		return initChunk{}, false
	}
	params, ok := parseParams(v[16:])
	if !ok {
		return initChunk{}, false
	}
	for _, p := range params {
		if len(c.params) < maxPeerFeatures {
			c.params = append(c.params, p.typ)
		}
		switch p.typ {
		case paramForwardTSN:
			c.forwardTSN = true
		case paramSupportedExtension:
			c.extensions = append(c.extensions, p.value[:min(len(p.value), maxPeerFeatures)]...)
			for _, t := range p.value {
				switch t {
				case chunkForwardTSN:
					c.forwardTSN = true
				case chunkReconfig:
					c.reconfig = true
				}
			}
		case paramStateCookie:
			c.cookie = p.value
		}
	}
	return c, true
}

func (c initChunk) marshal() []byte {
	v := make([]byte, 16, 64+len(c.cookie))
	binary.BigEndian.PutUint32(v, c.tag)
	binary.BigEndian.PutUint32(v[4:], c.window)
	binary.BigEndian.PutUint16(v[8:], c.outStreams)
	binary.BigEndian.PutUint16(v[10:], c.inStreams)
	binary.BigEndian.PutUint32(v[12:], c.tsn)
	v = appendParam(v, paramSupportedExtension, []byte{chunkReconfig, chunkForwardTSN})
	v = appendParam(v, paramForwardTSN, nil)
	if c.cookie != nil {
		v = appendParam(v, paramStateCookie, c.cookie)
	}
	return v
}

// sackChunk is the value of a SACK chunk.
type sackChunk struct {
	cumTSN uint32
	window uint32
	// gaps holds the gap ack blocks as start and end offsets from cumTSN.
	gaps []uint16
	dups []uint32
}

func parseSack(v []byte, gaps []uint16) (sackChunk, bool) {
	if len(v) < 12 {
		return sackChunk{}, false
	}
	s := sackChunk{cumTSN: binary.BigEndian.Uint32(v), window: binary.BigEndian.Uint32(v[4:])}
	nGaps := int(binary.BigEndian.Uint16(v[8:]))
	nDups := int(binary.BigEndian.Uint16(v[10:]))
	if len(v) < 12+4*nGaps+4*nDups {
		return sackChunk{}, false
	}
	s.gaps = gaps[:0]
	for i := range nGaps {
		start := binary.BigEndian.Uint16(v[12+4*i:])
		end := binary.BigEndian.Uint16(v[14+4*i:])
		if start == 0 || end < start {
			return sackChunk{}, false
		}
		s.gaps = append(s.gaps, start, end)
	}
	return s, true
}

func appendSack(dst []byte, cumTSN, window uint32, gaps []uint16, dups []uint32) []byte {
	dst = binary.BigEndian.AppendUint32(dst, cumTSN)
	dst = binary.BigEndian.AppendUint32(dst, window)
	dst = binary.BigEndian.AppendUint16(dst, uint16(len(gaps)/2))
	dst = binary.BigEndian.AppendUint16(dst, uint16(len(dups)))
	for _, g := range gaps {
		dst = binary.BigEndian.AppendUint16(dst, g)
	}
	for _, d := range dups {
		dst = binary.BigEndian.AppendUint32(dst, d)
	}
	return dst
}

// tsnLess compares TSNs in serial number arithmetic.
func tsnLess(a, b uint32) bool { return int32(a-b) < 0 }

func tsnLessEq(a, b uint32) bool { return int32(a-b) <= 0 }

// ssnLess compares stream sequence numbers in serial number arithmetic.
func ssnLess(a, b uint16) bool { return int16(a-b) < 0 }
