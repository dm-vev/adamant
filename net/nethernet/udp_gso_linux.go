//go:build linux

package nethernet

import (
	"encoding/binary"
	"errors"
	"net"
	"net/netip"
	"sync/atomic"
	"syscall"
	"unsafe"
)

// udpSegment is UDP_SEGMENT (Linux 4.18), which package syscall leaves
// out: the size a send cuts its data into, datagram by datagram.
const udpSegment = 103

// segmentsSupported reports whether datagrams can go out several in a
// call.
const segmentsSupported = true

// segmentsBroken is set once the kernel refused UDP_SEGMENT: datagrams
// then go one per call.
var segmentsBroken atomic.Bool

// writeSegments sends the datagrams back to back in b, each size bytes but
// maybe the last, in one call when the kernel segments them.
func writeSegments(s *net.UDPConn, b []byte, size int, to netip.AddrPort) {
	if len(b) > size && !segmentsBroken.Load() {
		var oob [32]byte
		_, _, err := s.WriteMsgUDPAddrPort(b, segmentControl(oob[:], size), to)
		if err == nil {
			return
		}
		if errors.Is(err, syscall.EINVAL) || errors.Is(err, syscall.EIO) || errors.Is(err, syscall.EOPNOTSUPP) || errors.Is(err, syscall.ENOPROTOOPT) {
			segmentsBroken.Store(true)
		}
	}
	for len(b) > 0 {
		n := min(size, len(b))
		_, _ = s.WriteToUDPAddrPort(b[:n], to)
		b = b[n:]
	}
}

// segmentControl writes the control message of UDP_SEGMENT into b and
// returns it.
func segmentControl(b []byte, size int) []byte {
	b = b[:syscall.CmsgSpace(2)]
	clear(b)
	// The length field of a cmsghdr is a size_t.
	if lenSize := unsafe.Sizeof(syscall.Cmsghdr{}.Len); lenSize == 8 {
		binary.NativeEndian.PutUint64(b, uint64(syscall.CmsgLen(2)))
	} else {
		binary.NativeEndian.PutUint32(b, uint32(syscall.CmsgLen(2)))
	}
	off := int(unsafe.Sizeof(syscall.Cmsghdr{}.Len))
	binary.NativeEndian.PutUint32(b[off:], uint32(syscall.IPPROTO_UDP))
	binary.NativeEndian.PutUint32(b[off+4:], udpSegment)
	binary.NativeEndian.PutUint16(b[syscall.CmsgLen(0):], uint16(size))
	return b
}
