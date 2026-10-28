// Package stun encodes and decodes the STUN messages ICE uses (RFC 8489):
// Binding requests, responses and indications with short-term credentials.
// Decoding refers into the buffer passed and allocates nothing.
package stun

import (
	"crypto/hmac"
	"encoding/binary"
	"errors"
	"hash"
	"hash/crc32"
	"net/netip"
)

// magicCookie is the fixed value of the second word of every STUN message.
const magicCookie = 0x2112A442

// headerSize is the size of the STUN message header.
const headerSize = 20

// fingerprintXOR is XORed into the CRC32 of a FINGERPRINT attribute.
const fingerprintXOR = 0x5354554e

// Type is the type of a STUN message: its method and class.
type Type uint16

const (
	BindingRequest    Type = 0x0001
	BindingIndication Type = 0x0011
	BindingSuccess    Type = 0x0101
	BindingError      Type = 0x0111
)

// Attribute types used by ICE.
const (
	AttrMappedAddress     uint16 = 0x0001
	AttrUsername          uint16 = 0x0006
	AttrMessageIntegrity  uint16 = 0x0008
	AttrErrorCode         uint16 = 0x0009
	AttrUnknownAttributes uint16 = 0x000A
	AttrXORMappedAddress  uint16 = 0x0020
	AttrPriority          uint16 = 0x0024
	AttrUseCandidate      uint16 = 0x0025
	AttrSoftware          uint16 = 0x8022
	AttrFingerprint       uint16 = 0x8028
	AttrICEControlled     uint16 = 0x8029
	AttrICEControlling    uint16 = 0x802A
)

// maxAttributes bounds the attributes a message may hold.
const maxAttributes = 24

var (
	errShort     = errors.New("stun: message too short")
	errNotSTUN   = errors.New("stun: not a STUN message")
	errLength    = errors.New("stun: invalid length")
	errTooMany   = errors.New("stun: too many attributes")
	errAttribute = errors.New("stun: malformed attribute")
)

// IsMessage reports whether b looks like a STUN message: the two top bits of
// its first byte are zero and it carries the magic cookie. RFC 7983 tells
// STUN apart from DTLS on a shared port by the first byte.
func IsMessage(b []byte) bool {
	return len(b) >= headerSize && b[0]&0xc0 == 0 && binary.BigEndian.Uint32(b[4:]) == magicCookie
}

// attribute locates an attribute value in a message.
type attribute struct {
	typ        uint16
	start, end uint16
}

// Message is a STUN message decoded by Parse. Its values refer into the
// buffer passed to Parse, which must not change while the Message is used.
type Message struct {
	Type        Type
	Transaction [12]byte
	raw         []byte
	attrs       [maxAttributes]attribute
	n           int
	// integrity and fingerprint are the offsets of the MESSAGE-INTEGRITY and
	// FINGERPRINT attributes, or -1.
	integrity, fingerprint int
}

// Parse decodes the STUN message in b into m. Attributes after
// MESSAGE-INTEGRITY other than FINGERPRINT are ignored, as RFC 8489 asks.
func Parse(b []byte, m *Message) error {
	if len(b) < headerSize {
		return errShort
	}
	if !IsMessage(b) {
		return errNotSTUN
	}
	length := int(binary.BigEndian.Uint16(b[2:]))
	if length%4 != 0 || headerSize+length > len(b) {
		return errLength
	}
	b = b[:headerSize+length]
	m.Type = Type(binary.BigEndian.Uint16(b))
	copy(m.Transaction[:], b[8:20])
	m.raw = b
	m.n = 0
	m.integrity, m.fingerprint = -1, -1
	for off := headerSize; off < len(b); {
		if off+4 > len(b) {
			return errAttribute
		}
		typ := binary.BigEndian.Uint16(b[off:])
		size := int(binary.BigEndian.Uint16(b[off+2:]))
		start := off + 4
		end := start + size
		if end > len(b) {
			return errAttribute
		}
		switch {
		case m.fingerprint >= 0:
			// Nothing may follow FINGERPRINT.
			return errAttribute
		case typ == AttrFingerprint:
			if size != 4 {
				return errAttribute
			}
			m.fingerprint = off
		case m.integrity >= 0:
			// Attributes after MESSAGE-INTEGRITY are not covered by it.
		case typ == AttrMessageIntegrity:
			if size != 20 {
				return errAttribute
			}
			m.integrity = off
		default:
			if m.n == maxAttributes {
				return errTooMany
			}
			m.attrs[m.n] = attribute{typ: typ, start: uint16(start), end: uint16(end)}
			m.n++
		}
		off = start + (size+3)&^3
	}
	return nil
}

// Get returns the value of the first attribute of type t before
// MESSAGE-INTEGRITY.
func (m *Message) Get(t uint16) ([]byte, bool) {
	for _, a := range m.attrs[:m.n] {
		if a.typ == t {
			return m.raw[a.start:a.end], true
		}
	}
	return nil, false
}

// Has reports whether the message holds an attribute of type t.
func (m *Message) Has(t uint16) bool {
	_, ok := m.Get(t)
	return ok
}

// Uint32 returns the value of a four byte attribute of type t.
func (m *Message) Uint32(t uint16) (uint32, bool) {
	v, ok := m.Get(t)
	if !ok || len(v) != 4 {
		return 0, false
	}
	return binary.BigEndian.Uint32(v), true
}

// Uint64 returns the value of an eight byte attribute of type t.
func (m *Message) Uint64(t uint16) (uint64, bool) {
	v, ok := m.Get(t)
	if !ok || len(v) != 8 {
		return 0, false
	}
	return binary.BigEndian.Uint64(v), true
}

// XORAddress returns the address of the XOR-MAPPED-ADDRESS attribute.
func (m *Message) XORAddress() (netip.AddrPort, bool) {
	v, ok := m.Get(AttrXORMappedAddress)
	if !ok {
		return netip.AddrPort{}, false
	}
	return decodeXORAddress(v, m.Transaction)
}

// HasIntegrity reports whether the message carries MESSAGE-INTEGRITY.
func (m *Message) HasIntegrity() bool { return m.integrity >= 0 }

// CheckFingerprint reports whether the message carries a FINGERPRINT
// attribute that matches its contents.
func (m *Message) CheckFingerprint() bool {
	if m.fingerprint < 0 {
		return false
	}
	want := binary.BigEndian.Uint32(m.raw[m.fingerprint+4:])
	return fingerprint(m.raw[:m.fingerprint]) == want
}

// CheckIntegrity reports whether the MESSAGE-INTEGRITY attribute of the
// message matches the HMAC-SHA1 h computes. h is keyed with the
// short-term password and reset before use.
func (m *Message) CheckIntegrity(h hash.Hash) bool {
	if m.integrity < 0 {
		return false
	}
	var sum [20]byte
	integrity(h, m.raw[:m.integrity], sum[:0])
	return hmac.Equal(sum[:], m.raw[m.integrity+4:m.integrity+24])
}

// integrity appends to dst the HMAC h computes over msg, a message up to
// (excluding) its MESSAGE-INTEGRITY attribute, with the length in its header
// counting that attribute, as RFC 8489 section 14.5 asks.
func integrity(h hash.Hash, msg, dst []byte) []byte {
	var header [headerSize]byte
	copy(header[:], msg)
	binary.BigEndian.PutUint16(header[2:], uint16(len(msg)-headerSize+24))
	h.Reset()
	h.Write(header[:])
	h.Write(msg[headerSize:])
	return h.Sum(dst)
}

// fingerprint returns the FINGERPRINT value of msg, a message up to
// (excluding) its FINGERPRINT attribute, with the length in its header
// counting that attribute.
func fingerprint(msg []byte) uint32 {
	var header [4]byte
	copy(header[:2], msg)
	binary.BigEndian.PutUint16(header[2:], uint16(len(msg)-headerSize+8))
	crc := crc32.Update(0, crc32.IEEETable, header[:])
	crc = crc32.Update(crc, crc32.IEEETable, msg[4:])
	return crc ^ fingerprintXOR
}

// decodeXORAddress decodes the value of an XOR-MAPPED-ADDRESS attribute.
func decodeXORAddress(v []byte, tx [12]byte) (netip.AddrPort, bool) {
	if len(v) < 4 {
		return netip.AddrPort{}, false
	}
	port := binary.BigEndian.Uint16(v[2:]) ^ magicCookie>>16
	switch v[1] {
	case 1:
		if len(v) != 8 {
			return netip.AddrPort{}, false
		}
		var ip [4]byte
		binary.BigEndian.PutUint32(ip[:], binary.BigEndian.Uint32(v[4:])^magicCookie)
		return netip.AddrPortFrom(netip.AddrFrom4(ip), port), true
	case 2:
		if len(v) != 20 {
			return netip.AddrPort{}, false
		}
		var key, ip [16]byte
		binary.BigEndian.PutUint32(key[:], magicCookie)
		copy(key[4:], tx[:])
		for i := range ip {
			ip[i] = v[4+i] ^ key[i]
		}
		return netip.AddrPortFrom(netip.AddrFrom16(ip), port), true
	}
	return netip.AddrPort{}, false
}

// Builder appends a STUN message to a buffer.
type Builder struct {
	buf   []byte
	start int
	tx    [12]byte
}

// NewBuilder starts a message of type t with the transaction ID passed at
// the end of dst.
func NewBuilder(dst []byte, t Type, tx [12]byte) Builder {
	start := len(dst)
	dst = binary.BigEndian.AppendUint16(dst, uint16(t))
	dst = append(dst, 0, 0)
	dst = binary.BigEndian.AppendUint32(dst, magicCookie)
	dst = append(dst, tx[:]...)
	return Builder{buf: dst, start: start, tx: tx}
}

// Add appends an attribute, padding its value to four bytes.
func (b *Builder) Add(t uint16, v []byte) {
	b.buf = binary.BigEndian.AppendUint16(b.buf, t)
	b.buf = binary.BigEndian.AppendUint16(b.buf, uint16(len(v)))
	b.buf = append(b.buf, v...)
	for len(b.buf)%4 != b.start%4 {
		b.buf = append(b.buf, 0)
	}
	b.setLength()
}

// AddString appends an attribute with a string value.
func (b *Builder) AddString(t uint16, v string) {
	b.buf = binary.BigEndian.AppendUint16(b.buf, t)
	b.buf = binary.BigEndian.AppendUint16(b.buf, uint16(len(v)))
	b.buf = append(b.buf, v...)
	for len(b.buf)%4 != b.start%4 {
		b.buf = append(b.buf, 0)
	}
	b.setLength()
}

// AddUint32 appends a four byte attribute.
func (b *Builder) AddUint32(t uint16, v uint32) {
	var raw [4]byte
	binary.BigEndian.PutUint32(raw[:], v)
	b.Add(t, raw[:])
}

// AddUint64 appends an eight byte attribute.
func (b *Builder) AddUint64(t uint16, v uint64) {
	var raw [8]byte
	binary.BigEndian.PutUint64(raw[:], v)
	b.Add(t, raw[:])
}

// AddFlag appends an attribute without a value, such as USE-CANDIDATE.
func (b *Builder) AddFlag(t uint16) {
	b.Add(t, nil)
}

// AddXORAddress appends an XOR-MAPPED-ADDRESS attribute.
func (b *Builder) AddXORAddress(addr netip.AddrPort) {
	ip := addr.Addr().Unmap()
	var v [20]byte
	binary.BigEndian.PutUint16(v[2:], addr.Port()^magicCookie>>16)
	if ip.Is4() {
		v[1] = 1
		a := ip.As4()
		binary.BigEndian.PutUint32(v[4:], binary.BigEndian.Uint32(a[:])^magicCookie)
		b.Add(AttrXORMappedAddress, v[:8])
		return
	}
	v[1] = 2
	var key [16]byte
	binary.BigEndian.PutUint32(key[:], magicCookie)
	copy(key[4:], b.tx[:])
	a := ip.As16()
	for i := range a {
		v[4+i] = a[i] ^ key[i]
	}
	b.Add(AttrXORMappedAddress, v[:])
}

// AddErrorCode appends an ERROR-CODE attribute.
func (b *Builder) AddErrorCode(code int, reason string) {
	v := make([]byte, 4, 4+len(reason))
	v[2] = byte(code / 100)
	v[3] = byte(code % 100)
	b.Add(AttrErrorCode, append(v, reason...))
}

// AddIntegrity appends a MESSAGE-INTEGRITY attribute computed by h, an
// HMAC-SHA1 keyed with the short-term password.
func (b *Builder) AddIntegrity(h hash.Hash) {
	msg := b.buf[b.start:]
	b.buf = binary.BigEndian.AppendUint16(b.buf, AttrMessageIntegrity)
	b.buf = binary.BigEndian.AppendUint16(b.buf, 20)
	b.buf = integrity(h, msg, b.buf)
	b.setLength()
}

// AddFingerprint appends a FINGERPRINT attribute. It must come last.
func (b *Builder) AddFingerprint() {
	crc := fingerprint(b.buf[b.start:])
	b.buf = binary.BigEndian.AppendUint16(b.buf, AttrFingerprint)
	b.buf = binary.BigEndian.AppendUint16(b.buf, 4)
	b.buf = binary.BigEndian.AppendUint32(b.buf, crc)
	b.setLength()
}

// Bytes returns the buffer passed to NewBuilder with the message appended.
func (b *Builder) Bytes() []byte { return b.buf }

func (b *Builder) setLength() {
	binary.BigEndian.PutUint16(b.buf[b.start+2:], uint16(len(b.buf)-b.start-headerSize))
}
