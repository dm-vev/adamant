package nethernet

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"net/netip"
	"strconv"
	"strings"
)

// Setup is the DTLS role a description offers or takes (RFC 8842).
type Setup string

const (
	SetupActpass Setup = "actpass"
	SetupActive  Setup = "active"
	SetupPassive Setup = "passive"
)

// description is what this package reads from and writes to an SDP offer
// or answer of a NetherNet connection.
type description struct {
	ufrag, pwd     string
	fingerprints   []Fingerprint
	setup          Setup
	maxMessageSize int
	sctpPort       int
	identity       string
	candidates     []netip.AddrPort
}

// maxDescription bounds the size of a description.
const maxDescription = 64 << 10

var errDescription = errors.New("nethernet: malformed session description")

// parseDescription reads an SDP offer or answer. It keeps the attributes
// of the single data channel media section and the session identity.
func parseDescription(sdp string) (*description, error) {
	if len(sdp) > maxDescription {
		return nil, errDescription
	}
	d := &description{}
	media := 0
	for line := range strings.Lines(sdp) {
		line = strings.TrimRight(line, "\r\n")
		if len(line) < 2 || line[1] != '=' {
			continue
		}
		if line[0] == 'm' {
			media++
			if !strings.HasPrefix(line, "m=application ") || !strings.Contains(line, "webrtc-datachannel") {
				return nil, fmt.Errorf("nethernet: unexpected media section %q", line)
			}
			continue
		}
		if line[0] != 'a' {
			continue
		}
		key, value, _ := strings.Cut(line[2:], ":")
		switch key {
		case "ice-ufrag":
			d.ufrag = value
		case "ice-pwd":
			d.pwd = value
		case "fingerprint":
			alg, digest, ok := strings.Cut(value, " ")
			if !ok {
				return nil, errDescription
			}
			d.fingerprints = append(d.fingerprints, Fingerprint{Algorithm: strings.ToLower(alg), Digest: strings.TrimSpace(digest)})
		case "setup":
			d.setup = Setup(value)
		case "max-message-size":
			n, err := strconv.Atoi(value)
			if err != nil || n <= 1 {
				return nil, errDescription
			}
			d.maxMessageSize = n
		case "sctp-port":
			n, err := strconv.Atoi(value)
			if err != nil || n <= 0 || n > 65535 {
				return nil, errDescription
			}
			d.sctpPort = n
		case "identity":
			d.identity = value
		case "candidate":
			if addr, ok := parseCandidate(value); ok && len(d.candidates) < 32 {
				d.candidates = append(d.candidates, addr)
			}
		}
	}
	switch {
	case media != 1:
		return nil, fmt.Errorf("nethernet: %d media sections, want 1", media)
	case len(d.ufrag) < 4 || len(d.pwd) < 22 || len(d.ufrag) > 256 || len(d.pwd) > 256:
		return nil, errors.New("nethernet: missing or invalid ICE credentials")
	case len(d.fingerprints) == 0:
		return nil, errors.New("nethernet: missing fingerprint")
	case d.setup != SetupActpass && d.setup != SetupActive && d.setup != SetupPassive:
		return nil, fmt.Errorf("nethernet: invalid setup %q", d.setup)
	}
	if d.maxMessageSize == 0 {
		// RFC 8841: an absent attribute means 64 KiB.
		d.maxMessageSize = 65536
	}
	if d.sctpPort == 0 {
		d.sctpPort = 5000
	}
	return d, nil
}

// parseCandidate reads the address of a UDP host, server reflexive or peer
// reflexive candidate. Some offers repeat the "candidate:" prefix.
func parseCandidate(v string) (netip.AddrPort, bool) {
	v = strings.TrimPrefix(v, "candidate:")
	f := strings.Fields(v)
	if len(f) < 8 || !strings.EqualFold(f[2], "udp") || f[6] != "typ" {
		return netip.AddrPort{}, false
	}
	addr, err := netip.ParseAddr(f[4])
	if err != nil {
		// An mDNS name cannot be resolved here.
		return netip.AddrPort{}, false
	}
	port, err := strconv.Atoi(f[5])
	if err != nil || port <= 0 || port > 65535 {
		return netip.AddrPort{}, false
	}
	return netip.AddrPortFrom(addr.Unmap(), uint16(port)), true
}

// randomString returns n characters of the ICE character set.
func randomString(n int) string {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	b := make([]byte, n)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(b)
}

// encodeDescription writes an SDP offer or answer in the form BDS uses.
func encodeDescription(d *description) string {
	var sessionID [8]byte
	_, _ = rand.Read(sessionID[:])
	var b strings.Builder
	line := func(format string, args ...any) {
		fmt.Fprintf(&b, format, args...)
		b.WriteString("\r\n")
	}
	line("v=0")
	line("o=- %d 2 IN IP4 127.0.0.1", binary.BigEndian.Uint64(sessionID[:])>>1)
	line("s=-")
	line("t=0 0")
	line("a=group:BUNDLE 0")
	line("a=extmap-allow-mixed")
	line("a=msid-semantic: WMS")
	if d.identity != "" {
		line("a=identity:%s", d.identity)
	}
	port, conn := 9, "0.0.0.0"
	for _, c := range d.candidates {
		if c.Addr().Is4() {
			port, conn = int(c.Port()), c.Addr().String()
			break
		}
	}
	line("m=application %d UDP/DTLS/SCTP webrtc-datachannel", port)
	line("c=IN IP4 %s", conn)
	for i, c := range d.candidates {
		// Host candidates with descending local preference, as libwebrtc
		// numbers them.
		prio := 126<<24 | (65535-i)<<8 | 255
		foundation := crc32.ChecksumIEEE([]byte(c.Addr().String()))
		line("a=candidate:%d 1 udp %d %s %d typ host generation 0 network-id %d network-cost 50", foundation, prio, c.Addr().String(), c.Port(), i+1)
	}
	line("a=ice-ufrag:%s", d.ufrag)
	line("a=ice-pwd:%s", d.pwd)
	line("a=ice-options:trickle")
	for _, fp := range d.fingerprints {
		line("a=fingerprint:%s %s", fp.Algorithm, fp.Digest)
	}
	line("a=setup:%s", d.setup)
	line("a=mid:0")
	line("a=sctp-port:%d", d.sctpPort)
	line("a=max-message-size:%d", d.maxMessageSize)
	return b.String()
}
