//go:build !linux

package nethernet

import (
	"net"
	"net/netip"
)

// segmentsSupported reports whether datagrams can go out several in a
// call.
const segmentsSupported = false

// writeSegments sends the datagrams back to back in b, each size bytes but
// maybe the last, one per call.
func writeSegments(s *net.UDPConn, b []byte, size int, to netip.AddrPort) {
	for len(b) > 0 {
		n := min(size, len(b))
		_, _ = s.WriteToUDPAddrPort(b[:n], to)
		b = b[n:]
	}
}
