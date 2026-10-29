package dtls

import (
	"encoding/binary"
	"slices"
)

// Handshake message types.
const (
	typeClientHello        = 1
	typeServerHello        = 2
	typeHelloVerifyRequest = 3
	typeCertificate        = 11
	typeServerKeyExchange  = 12
	typeCertificateRequest = 13
	typeServerHelloDone    = 14
	typeCertificateVerify  = 15
	typeClientKeyExchange  = 16
	typeFinished           = 20
)

// handshakeHeaderSize is the size of a DTLS handshake message header.
const handshakeHeaderSize = 12

// maxHandshakeMessage bounds the size of a handshake message.
const maxHandshakeMessage = 16 << 10

// Extension types.
const (
	extSupportedGroups      = 10
	extPointFormats         = 11
	extSignatureAlgorithms  = 13
	extUseSRTP              = 14
	extExtendedMasterSecret = 23
	extSupportedVersions    = 43
	extRenegotiationInfo    = 0xff01
)

// version13 is DTLS 1.3 as a supported_versions extension names it.
const version13 = 0xfefc

// scsvRenegotiation is the signalling cipher suite of RFC 5746.
const scsvRenegotiation = 0x00ff

// Named groups.
const (
	groupP256   = 23
	groupP384   = 24
	groupX25519 = 29
)

// Signature schemes, as TLS 1.2 SignatureAndHashAlgorithm values.
const (
	sigECDSAP256SHA256 = 0x0403
	sigECDSAP384SHA384 = 0x0503
	sigECDSAP521SHA512 = 0x0603
	sigRSAPSSSHA256    = 0x0804
	sigRSAPSSSHA384    = 0x0805
	sigRSAPSSSHA512    = 0x0806
	sigRSAPKCS1SHA256  = 0x0401
	sigRSAPKCS1SHA384  = 0x0501
	sigRSAPKCS1SHA512  = 0x0601
)

// supportedSignatures lists the schemes accepted from the peer.
var supportedSignatures = []uint16{
	sigECDSAP256SHA256, sigECDSAP384SHA384, sigECDSAP521SHA512,
	sigRSAPSSSHA256, sigRSAPSSSHA384, sigRSAPSSSHA512,
	sigRSAPKCS1SHA256, sigRSAPKCS1SHA384, sigRSAPKCS1SHA512,
}

// supportedGroups lists the ECDHE groups in order of preference.
var supportedGroups = []uint16{groupX25519, groupP256, groupP384}

// SRTP protection profiles (RFC 5764 section 4.1.2).
const (
	srtpAES128CMSHA1_80 = 0x0001
	srtpAES128CMSHA1_32 = 0x0002
	srtpAEADAES128GCM   = 0x0007
	srtpAEADAES256GCM   = 0x0008
)

// srtpProfiles lists the SRTP protection profiles offered and taken, in
// order of preference. Data channels carry no SRTP, but pion and libwebrtc
// fail a handshake that does not settle on a profile.
var srtpProfiles = []uint16{srtpAEADAES128GCM, srtpAEADAES256GCM, srtpAES128CMSHA1_80, srtpAES128CMSHA1_32}

// handshakeHeader is the header of a DTLS handshake message fragment.
type handshakeHeader struct {
	typ            byte
	length         int
	messageSeq     uint16
	fragmentOffset int
	fragmentLength int
}

func parseHandshakeHeader(b []byte) (handshakeHeader, bool) {
	if len(b) < handshakeHeaderSize {
		return handshakeHeader{}, false
	}
	h := handshakeHeader{
		typ:            b[0],
		length:         int(b[1])<<16 | int(b[2])<<8 | int(b[3]),
		messageSeq:     binary.BigEndian.Uint16(b[4:]),
		fragmentOffset: int(b[6])<<16 | int(b[7])<<8 | int(b[8]),
		fragmentLength: int(b[9])<<16 | int(b[10])<<8 | int(b[11]),
	}
	if handshakeHeaderSize+h.fragmentLength > len(b) || h.fragmentOffset+h.fragmentLength > h.length || h.length > maxHandshakeMessage {
		return handshakeHeader{}, false
	}
	return h, true
}

func appendHandshakeHeader(dst []byte, typ byte, length int, seq uint16, offset, fragment int) []byte {
	dst = append(dst, typ)
	dst = appendUint24(dst, length)
	dst = binary.BigEndian.AppendUint16(dst, seq)
	dst = appendUint24(dst, offset)
	return appendUint24(dst, fragment)
}

func appendUint24(dst []byte, v int) []byte {
	return append(dst, byte(v>>16), byte(v>>8), byte(v))
}

// reader decodes the fields of a handshake message. A failed read sets ok
// to false and returns zero values from then on.
type reader struct {
	b  []byte
	ok bool
}

func newReader(b []byte) *reader { return &reader{b: b, ok: true} }

func (r *reader) bytes(n int) []byte {
	if !r.ok || n < 0 || n > len(r.b) {
		r.ok = false
		return nil
	}
	v := r.b[:n:n]
	r.b = r.b[n:]
	return v
}

func (r *reader) u8() byte {
	v := r.bytes(1)
	if v == nil {
		return 0
	}
	return v[0]
}

func (r *reader) u16() uint16 {
	v := r.bytes(2)
	if v == nil {
		return 0
	}
	return binary.BigEndian.Uint16(v)
}

func (r *reader) u24() int {
	v := r.bytes(3)
	if v == nil {
		return 0
	}
	return int(v[0])<<16 | int(v[1])<<8 | int(v[2])
}

func (r *reader) vec8() []byte  { return r.bytes(int(r.u8())) }
func (r *reader) vec16() []byte { return r.bytes(int(r.u16())) }
func (r *reader) vec24() []byte { return r.bytes(r.u24()) }

func (r *reader) done() bool { return r.ok && len(r.b) == 0 }

func appendVec8(dst, v []byte) []byte { return append(append(dst, byte(len(v))), v...) }
func appendVec16(dst, v []byte) []byte {
	return append(binary.BigEndian.AppendUint16(dst, uint16(len(v))), v...)
}
func appendVec24(dst, v []byte) []byte { return append(appendUint24(dst, len(v)), v...) }

func appendUint16s(dst []byte, vs []uint16) []byte {
	dst = binary.BigEndian.AppendUint16(dst, uint16(2*len(vs)))
	for _, v := range vs {
		dst = binary.BigEndian.AppendUint16(dst, v)
	}
	return dst
}

func parseUint16s(b []byte) ([]uint16, bool) {
	if len(b)%2 != 0 {
		return nil, false
	}
	vs := make([]uint16, len(b)/2)
	for i := range vs {
		vs[i] = binary.BigEndian.Uint16(b[2*i:])
	}
	return vs, true
}

// extensions holds the hello extensions this package understands.
type extensions struct {
	groups               []uint16
	pointFormats         []byte
	signatures           []uint16
	extendedMasterSecret bool
	renegotiationInfo    bool
	// srtpProfiles holds the profiles of a use_srtp extension, if any.
	srtpProfiles []uint16
	// offers13 is set if a supported_versions extension offers DTLS 1.3,
	// which a ClientHello of DTLS 1.2 may carry to say the peer would
	// speak it.
	offers13 bool
}

func parseExtensions(b []byte) (extensions, bool) {
	var e extensions
	r := newReader(b)
	for r.ok && len(r.b) > 0 {
		typ := r.u16()
		data := r.vec16()
		if !r.ok {
			return e, false
		}
		d := newReader(data)
		switch typ {
		case extSupportedGroups:
			groups, ok := parseUint16s(d.vec16())
			if !ok || !d.done() {
				return e, false
			}
			e.groups = groups
		case extPointFormats:
			e.pointFormats = d.vec8()
			if !d.done() {
				return e, false
			}
		case extSignatureAlgorithms:
			sigs, ok := parseUint16s(d.vec16())
			if !ok || !d.done() {
				return e, false
			}
			e.signatures = sigs
		case extExtendedMasterSecret:
			e.extendedMasterSecret = true
		case extUseSRTP:
			profiles, ok := parseUint16s(d.vec16())
			// The MKI is not used: SRTP never runs.
			d.vec8()
			if !ok || len(profiles) == 0 || !d.done() {
				return e, false
			}
			e.srtpProfiles = profiles
		case extSupportedVersions:
			// Only read: the handshake is of DTLS 1.2, which the peer
			// offers beside 1.3.
			versions, ok := parseUint16s(d.vec8())
			e.offers13 = ok && slices.Contains(versions, version13)
		case extRenegotiationInfo:
			// Only an initial handshake happens: the renegotiated
			// connection data must be empty.
			if v := d.vec8(); len(v) != 0 || !d.done() {
				return e, false
			}
			e.renegotiationInfo = true
		}
	}
	return e, r.ok
}

func appendExtension(dst []byte, typ uint16, data []byte) []byte {
	dst = binary.BigEndian.AppendUint16(dst, typ)
	return appendVec16(dst, data)
}

// clientHello is a ClientHello message.
type clientHello struct {
	version      uint16
	random       [32]byte
	sessionID    []byte
	cookie       []byte
	cipherSuites []uint16
	compression  []byte
	ext          extensions
}

func parseClientHello(b []byte) (*clientHello, bool) {
	m := &clientHello{}
	r := newReader(b)
	m.version = r.u16()
	copy(m.random[:], r.bytes(32))
	m.sessionID = r.vec8()
	m.cookie = r.vec8()
	suites := r.vec16()
	m.compression = r.vec8()
	var ext []byte
	if len(r.b) > 0 {
		ext = r.vec16()
	}
	if !r.done() || len(m.sessionID) > 32 || len(m.cookie) > 255 {
		return nil, false
	}
	var ok bool
	if m.cipherSuites, ok = parseUint16s(suites); !ok {
		return nil, false
	}
	if m.ext, ok = parseExtensions(ext); !ok {
		return nil, false
	}
	return m, true
}

func (m *clientHello) marshal() []byte {
	b := binary.BigEndian.AppendUint16(nil, m.version)
	b = append(b, m.random[:]...)
	b = appendVec8(b, m.sessionID)
	b = appendVec8(b, m.cookie)
	b = appendUint16s(b, m.cipherSuites)
	b = appendVec8(b, m.compression)
	var ext []byte
	ext = appendExtension(ext, extSupportedGroups, appendUint16s(nil, m.ext.groups))
	ext = appendExtension(ext, extPointFormats, appendVec8(nil, []byte{0}))
	ext = appendExtension(ext, extSignatureAlgorithms, appendUint16s(nil, m.ext.signatures))
	ext = appendExtension(ext, extExtendedMasterSecret, nil)
	ext = appendExtension(ext, extRenegotiationInfo, []byte{0})
	if len(m.ext.srtpProfiles) > 0 {
		ext = appendExtension(ext, extUseSRTP, appendVec8(appendUint16s(nil, m.ext.srtpProfiles), nil))
	}
	return appendVec16(b, ext)
}

// serverHello is a ServerHello message.
type serverHello struct {
	version     uint16
	random      [32]byte
	sessionID   []byte
	cipherSuite uint16
	compression byte
	ext         extensions
}

func parseServerHello(b []byte) (*serverHello, bool) {
	m := &serverHello{}
	r := newReader(b)
	m.version = r.u16()
	copy(m.random[:], r.bytes(32))
	m.sessionID = r.vec8()
	m.cipherSuite = r.u16()
	m.compression = r.u8()
	var ext []byte
	if len(r.b) > 0 {
		ext = r.vec16()
	}
	if !r.done() {
		return nil, false
	}
	var ok bool
	m.ext, ok = parseExtensions(ext)
	return m, ok
}

func (m *serverHello) marshal() []byte {
	b := binary.BigEndian.AppendUint16(nil, m.version)
	b = append(b, m.random[:]...)
	b = appendVec8(b, m.sessionID)
	b = binary.BigEndian.AppendUint16(b, m.cipherSuite)
	b = append(b, m.compression)
	var ext []byte
	if m.ext.extendedMasterSecret {
		ext = appendExtension(ext, extExtendedMasterSecret, nil)
	}
	if m.ext.renegotiationInfo {
		ext = appendExtension(ext, extRenegotiationInfo, []byte{0})
	}
	if len(m.ext.pointFormats) > 0 {
		ext = appendExtension(ext, extPointFormats, appendVec8(nil, m.ext.pointFormats))
	}
	if len(m.ext.srtpProfiles) > 0 {
		ext = appendExtension(ext, extUseSRTP, appendVec8(appendUint16s(nil, m.ext.srtpProfiles), nil))
	}
	if len(ext) > 0 {
		b = appendVec16(b, ext)
	}
	return b
}

// marshalHelloVerifyRequest encodes a HelloVerifyRequest. RFC 6347 asks for
// the DTLS 1.0 version in it whatever version follows.
func marshalHelloVerifyRequest(cookie []byte) []byte {
	b := binary.BigEndian.AppendUint16(nil, version10)
	return appendVec8(b, cookie)
}

func parseHelloVerifyRequest(b []byte) ([]byte, bool) {
	r := newReader(b)
	r.u16()
	cookie := r.vec8()
	return cookie, r.done()
}

// marshalCertificate encodes a Certificate message holding the chain
// passed, leaf first.
func marshalCertificate(chain [][]byte) []byte {
	var list []byte
	for _, der := range chain {
		list = appendVec24(list, der)
	}
	return appendVec24(nil, list)
}

func parseCertificate(b []byte) ([][]byte, bool) {
	r := newReader(b)
	list := newReader(r.vec24())
	if !r.done() {
		return nil, false
	}
	var chain [][]byte
	for list.ok && len(list.b) > 0 {
		der := list.vec24()
		if !list.ok || len(der) == 0 {
			return nil, false
		}
		chain = append(chain, der)
	}
	return chain, list.ok
}

// serverKeyExchange is an ECDHE ServerKeyExchange message.
type serverKeyExchange struct {
	group     uint16
	public    []byte
	signature uint16
	sig       []byte
	// params is the encoded ECDHE parameters the signature covers.
	params []byte
}

func parseServerKeyExchange(b []byte) (*serverKeyExchange, bool) {
	m := &serverKeyExchange{}
	r := newReader(b)
	if r.u8() != 3 {
		// Only named curves are allowed.
		return nil, false
	}
	m.group = r.u16()
	m.public = r.vec8()
	if !r.ok {
		return nil, false
	}
	m.params = b[:len(b)-len(r.b)]
	m.signature = r.u16()
	m.sig = r.vec16()
	return m, r.done()
}

func marshalECDHEParams(group uint16, public []byte) []byte {
	b := []byte{3}
	b = binary.BigEndian.AppendUint16(b, group)
	return appendVec8(b, public)
}

// certificateRequest is a CertificateRequest message.
type certificateRequest struct {
	types      []byte
	signatures []uint16
}

func parseCertificateRequest(b []byte) (*certificateRequest, bool) {
	m := &certificateRequest{}
	r := newReader(b)
	m.types = r.vec8()
	sigs := r.vec16()
	r.vec16() // Certificate authorities.
	if !r.done() {
		return nil, false
	}
	var ok bool
	m.signatures, ok = parseUint16s(sigs)
	return m, ok
}

func marshalCertificateRequest() []byte {
	// ecdsa_sign and rsa_sign certificates, no named authorities.
	b := appendVec8(nil, []byte{64, 1})
	b = appendUint16s(b, supportedSignatures)
	return binary.BigEndian.AppendUint16(b, 0)
}

// digitallySigned is the signature of a ServerKeyExchange or a
// CertificateVerify message.
type digitallySigned struct {
	scheme uint16
	sig    []byte
}

func parseDigitallySigned(b []byte) (digitallySigned, bool) {
	r := newReader(b)
	d := digitallySigned{scheme: r.u16(), sig: r.vec16()}
	return d, r.done()
}

func (d digitallySigned) marshal() []byte {
	b := binary.BigEndian.AppendUint16(nil, d.scheme)
	return appendVec16(b, d.sig)
}
