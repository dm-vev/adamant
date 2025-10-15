package query

import (
	"bytes"
	"encoding/binary"
	"math/rand"
	"net"
	"strconv"
	"sync"
	"time"
)

// packetConn intercepts query requests and responds directly while delegating
// all other traffic to the wrapped PacketConn.
type packetConn struct {
	net.PacketConn

	log  Logger
	host string
	port int

	mu     sync.Mutex
	tokens map[string]token
	rng    *rand.Rand
}

// Logger provides the logging capabilities used by the query implementation.
type Logger interface {
	Debug(msg string, args ...any)
}

type token struct {
	value  int32
	expiry time.Time
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
		token := c.newToken(addr.String())
		c.writeHandshake(addr, sequence, token)
		return true
	case queryTypeInformation:
		if len(b) <= 7 {
			return true
		}
		token, ok := parseTokenValue(b[7:])
		if !ok {
			return true
		}
		if !c.validateToken(addr.String(), token) {
			return true
		}
		c.writeInfo(addr, sequence)
		return true
	default:
		return false
	}
}

// newToken issues a temporary token for the provided address. The token is
// required by the query protocol to guard against amplification attacks.
func (c *packetConn) newToken(addr string) int32 {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.tokens == nil {
		c.tokens = make(map[string]token)
	}
	if c.rng == nil {
		c.rng = rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	value := int32(c.rng.Int31())
	c.tokens[addr] = token{
		value:  value,
		expiry: time.Now().Add(30 * time.Second),
	}
	return value
}

// validateToken checks whether a previously issued token remains valid for the
// provided address.
func (c *packetConn) validateToken(addr string, value int32) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	token, ok := c.tokens[addr]
	if !ok || time.Now().After(token.expiry) || token.value != value {
		delete(c.tokens, addr)
		return false
	}
	return true
}

// writeHandshake constructs the handshake response that contains the issued
// token.
func (c *packetConn) writeHandshake(addr net.Addr, sequence, token int32) {
	buf := bytes.NewBuffer(make([]byte, 0, 1+4+12))
	buf.WriteByte(queryTypeHandshake)
	_ = binary.Write(buf, binary.BigEndian, sequence)

	tokenStr := strconv.FormatInt(int64(token), 10)
	if len(tokenStr) > 12 {
		tokenStr = tokenStr[:12]
	}
	buf.WriteString(tokenStr)
	if padding := 12 - len(tokenStr); padding > 0 {
		buf.Write(make([]byte, padding))
	}
	if _, err := c.PacketConn.WriteTo(buf.Bytes(), addr); err != nil {
		c.log.Debug("query handshake write failed", "err", err, "raddr", addr.String())
	}
}

// writeInfo renders the full server information payload for a validated query
// request.
func (c *packetConn) writeInfo(addr net.Addr, sequence int32) {
	data := collectData(c.host, c.port)

	buf := bytes.NewBuffer(make([]byte, 0, 256))
	buf.WriteByte(queryTypeInformation)
	_ = binary.Write(buf, binary.BigEndian, sequence)
	buf.Write(querySplitNum[:])
	buf.WriteByte(0x80)
	buf.WriteByte(0x00)

	for _, kv := range data.keyValues() {
		buf.WriteString(kv.key)
		buf.WriteByte(0x00)
		buf.WriteString(kv.value)
		buf.WriteByte(0x00)
	}
	buf.WriteByte(0x00)
	buf.Write(queryPlayerKey[:])
	for _, name := range data.PlayerNames {
		buf.WriteString(name)
		buf.WriteByte(0x00)
	}
	buf.WriteByte(0x00)

	if _, err := c.PacketConn.WriteTo(buf.Bytes(), addr); err != nil {
		c.log.Debug("query info write failed", "err", err, "raddr", addr.String())
	}
}

func parseTokenValue(payload []byte) (int32, bool) {
	trimmed := payload
	if len(trimmed) >= 4 {
		if i := bytes.Index(trimmed, []byte{0xff, 0xff, 0xff, 0x01}); i >= 0 {
			trimmed = trimmed[:i]
		}
	}
	trimmed = bytes.TrimRight(trimmed, "\x00")
	if len(trimmed) > 0 {
		if value, err := strconv.ParseInt(string(trimmed), 10, 32); err == nil {
			return int32(value), true
		}
	}
	if len(payload) >= 4 {
		return int32(binary.BigEndian.Uint32(payload[:4])), true
	}
	return 0, false
}
