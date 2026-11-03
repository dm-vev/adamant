package query

import (
	"crypto/rand"
	"fmt"
	"io"
	"net"
	"net/netip"
	"sync"
)

const (
	queryTypeHandshake   = 0x09
	queryTypeInformation = 0x00
)

var (
	// querySplitNumPrefix matches the "splitnum" header used by Lumi/Nukkit.
	// It is case sensitive and must remain lowercase for protocol compatibility.
	querySplitNumPrefix = [...]byte{'s', 'p', 'l', 'i', 't', 'n', 'u', 'm', 0x00, 0x80, 0x00}
	queryPlayerKey      = [...]byte{0x00, 0x01, 'p', 'l', 'a', 'y', 'e', 'r', '_', 0x00, 0x00}
	queryVersion        = [...]byte{0xfe, 0xfd}
)

// Responder answers the query requests that arrive on the UDP socket of
// another protocol, from which the listener of that protocol hands it the
// datagrams it does not take: query requests start with 0xfe, a byte no
// protocol of WebRTC begins with.
type Responder struct {
	mu sync.Mutex
	c  packetConn
}

// NewResponder returns a Responder for a socket bound to local, which the
// answers name as the address of the server.
func NewResponder(local netip.AddrPort, log Logger) (*Responder, error) {
	token, err := newQueryToken()
	if err != nil {
		return nil, err
	}
	host := "0.0.0.0"
	if a := local.Addr(); a.IsValid() && !a.IsUnspecified() {
		host = a.Unmap().String()
	}
	return &Responder{c: packetConn{log: log, host: host, port: int(local.Port()), token: token, lastToken: token}}, nil
}

// Answer answers b if it is a query request, sending the answer with
// reply, and reports whether it was one.
func (r *Responder) Answer(b []byte, from netip.AddrPort, reply func([]byte)) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.c.PacketConn = replyWriter(reply)
	defer func() { r.c.PacketConn = nil }()
	return r.c.handleQuery(b, net.UDPAddrFromAddrPort(from))
}

// replyWriter sends what is written to it as the answer to a datagram.
type replyWriter func([]byte)

func (w replyWriter) WriteTo(b []byte, _ net.Addr) (int, error) {
	w(b)
	return len(b), nil
}

func newQueryToken() ([16]byte, error) {
	var token [16]byte
	if _, err := io.ReadFull(rand.Reader, token[:]); err != nil {
		return [16]byte{}, fmt.Errorf("read query token: %w", err)
	}
	return token, nil
}
