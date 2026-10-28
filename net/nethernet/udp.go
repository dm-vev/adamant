package nethernet

import (
	"context"
	"net"
	"net/netip"
)

// socketBuffer is the kernel buffer asked for each socket, so that a burst
// of datagrams waits for the reading goroutine instead of being dropped.
const socketBuffer = 4 << 20

// listenUDP opens n UDP sockets bound to address. Where the kernel spreads
// the datagrams of different peers over sockets sharing a port, they share
// it; elsewhere one socket is opened.
func listenUDP(address string, n int) ([]*net.UDPConn, error) {
	if !reusePortSupported || n < 1 {
		n = 1
	}
	var lc net.ListenConfig
	if n > 1 {
		lc.Control = reusePort
	}
	socks := make([]*net.UDPConn, 0, n)
	for range n {
		pc, err := lc.ListenPacket(context.Background(), "udp", address)
		if err != nil {
			for _, s := range socks {
				_ = s.Close()
			}
			return nil, err
		}
		s := pc.(*net.UDPConn)
		_ = s.SetReadBuffer(socketBuffer)
		_ = s.SetWriteBuffer(socketBuffer)
		socks = append(socks, s)
	}
	return socks, nil
}

// batchWriter returns the writer of batches of datagrams of a socket, nil
// where they cannot go out several in a call.
func batchWriter(s *net.UDPConn) func(b []byte, size int, to netip.AddrPort) {
	if !segmentsSupported {
		return nil
	}
	return func(b []byte, size int, to netip.AddrPort) { writeSegments(s, b, size, to) }
}
