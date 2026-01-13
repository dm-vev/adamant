package query

import (
	"bytes"
	"crypto/md5"
	"encoding/binary"
	"net"
)

// packetConn intercepts query requests and responds directly while delegating
// all other traffic to the wrapped PacketConn.
type packetConn struct {
	net.PacketConn

	log  Logger
	host string
	port int

	token     [16]byte
	lastToken [16]byte
}

// Logger provides the logging capabilities used by the query implementation.
type Logger interface {
	Debug(msg string, args ...any)
}

// ReadFrom inspects incoming datagrams and filters out query packets so that
// they can be processed independently.
func (c *packetConn) ReadFrom(p []byte) (int, net.Addr, error) {
	for {
		n, addr, err := c.PacketConn.ReadFrom(p)
		if err != nil || n == 0 {
			return n, addr, err
		}
		if c.handleQuery(p[:n], addr) {
			continue
		}
		return n, addr, nil
	}
}

// handleQuery recognises and processes query requests. Non-query traffic is
// ignored so that it can proceed through the regular RakNet pipeline.
func (c *packetConn) handleQuery(b []byte, addr net.Addr) bool {
	if len(b) < 7 || b[0] != queryVersion[0] || b[1] != queryVersion[1] {
		return false
	}
	reqType := b[2]
	sequence := int32(binary.BigEndian.Uint32(b[3:7]))
	switch reqType {
	case queryTypeHandshake:
		c.writeHandshake(addr, sequence)
		return true
	case queryTypeInformation:
		// The statistics request must include the 4-byte token. If it doesn't, it is ignored.
		if len(b) < 11 {
			return true
		}
		if !c.validateToken(addr, b[7:11]) {
			return true
		}
		// Lumi decides between long and short responses by checking that 8 bytes remain after the token.
		long := len(b[11:]) == 8
		c.writeInfo(addr, sequence, long)
		return true
	default:
		return false
	}
}

func (c *packetConn) tokenDigest(addr net.Addr, token [16]byte) [4]byte {
	javaAddr := javaInetAddressString(addr)
	hash := md5.New()
	_, _ = hash.Write([]byte(javaAddr))
	_, _ = hash.Write(token[:])
	sum := hash.Sum(nil)

	var out [4]byte
	copy(out[:], sum[:4])
	return out
}

// javaInetAddressString mimics InetAddress#toString for raw IP addresses used by Lumi.
//
// The method includes a leading slash (for example "/127.0.0.1"), which is part of the token hash input.
func javaInetAddressString(addr net.Addr) string {
	if addr == nil {
		return ""
	}
	if udpAddr, ok := addr.(*net.UDPAddr); ok && udpAddr.IP != nil {
		return "/" + udpAddr.IP.String()
	}
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil || host == "" {
		return ""
	}
	return "/" + host
}

// writeHandshake constructs the handshake response that contains the issued
// token.
func (c *packetConn) writeHandshake(addr net.Addr, sequence int32) {
	digest := c.tokenDigest(addr, c.token)

	var resp [10]byte
	resp[0] = queryTypeHandshake
	binary.BigEndian.PutUint32(resp[1:5], uint32(sequence))
	copy(resp[5:9], digest[:])
	resp[9] = 0x00

	if _, err := c.PacketConn.WriteTo(resp[:], addr); err != nil {
		c.log.Debug("query handshake write failed", "err", err, "raddr", addr.String())
	}
}

func (c *packetConn) validateToken(addr net.Addr, payload []byte) bool {
	if len(payload) < 4 {
		return false
	}
	token := payload[:4]

	want := c.tokenDigest(addr, c.token)
	if bytes.Equal(token, want[:]) {
		return true
	}
	want = c.tokenDigest(addr, c.lastToken)
	return bytes.Equal(token, want[:])
}

// writeInfo renders the server information payload for a validated query request.
func (c *packetConn) writeInfo(addr net.Addr, sequence int32, long bool) {
	payload := collectPayload(c.host, c.port, long)

	resp := make([]byte, 1+4+len(payload))
	resp[0] = queryTypeInformation
	binary.BigEndian.PutUint32(resp[1:5], uint32(sequence))
	copy(resp[5:], payload)

	if _, err := c.PacketConn.WriteTo(resp, addr); err != nil {
		c.log.Debug("query info write failed", "err", err, "raddr", addr.String())
	}
}
