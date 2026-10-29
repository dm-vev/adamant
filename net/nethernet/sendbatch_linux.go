//go:build linux && (amd64 || arm64)

package nethernet

import (
	"errors"
	"net"
	"strconv"
	"syscall"
	"unsafe"
)

// sendBatchSupported reports whether a SendBatch sends the datagrams of a
// socket in one call.
const sendBatchSupported = true

// mmsghdr is struct mmsghdr of Linux on 64-bit architectures.
type mmsghdr struct {
	hdr syscall.Msghdr
	len uint32
	_   [4]byte
}

// sysBatch holds the headers of sendmmsg a socket keeps between flushes,
// and the family of the socket, zero until read.
type sysBatch struct {
	family uint16
	hdrs   []mmsghdr
	iovs   []syscall.Iovec
	names  []syscall.RawSockaddrInet6
	oob    []byte
}

// segmentSpace is the room the control message of UDP_SEGMENT takes.
var segmentSpace = syscall.CmsgSpace(2)

// send sends the datagrams gathered for the socket with sendmmsg, a
// message a connection, the datagrams of each segmented by the kernel.
func (sb *sockBatch) send() {
	rc, err := sb.sock.SyscallConn()
	if err != nil {
		sb.sendEach(0)
		return
	}
	s := &sb.sys
	if s.family == 0 {
		_ = rc.Control(func(fd uintptr) {
			switch sa, _ := syscall.Getsockname(int(fd)); sa.(type) {
			case *syscall.SockaddrInet4:
				s.family = syscall.AF_INET
			case *syscall.SockaddrInet6:
				s.family = syscall.AF_INET6
			}
		})
		if s.family == 0 {
			sb.sendEach(0)
			return
		}
	}
	n := len(sb.msgs)
	if cap(s.hdrs) < n {
		s.hdrs = make([]mmsghdr, n)
		s.iovs = make([]syscall.Iovec, n)
		s.names = make([]syscall.RawSockaddrInet6, n)
		s.oob = make([]byte, n*segmentSpace)
	}
	hdrs, iovs, names := s.hdrs[:n], s.iovs[:n], s.names[:n]
	segmented := !segmentsBroken.Load()
	for i, m := range sb.msgs {
		iovs[i] = syscall.Iovec{Base: &sb.buf[m.off]}
		iovs[i].SetLen(m.n)
		hdrs[i] = mmsghdr{}
		h := &hdrs[i].hdr
		h.Iov = &iovs[i]
		h.Iovlen = 1
		names[i] = syscall.RawSockaddrInet6{}
		h.Name = (*byte)(unsafe.Pointer(&names[i]))
		h.Namelen = sockaddr(&names[i], s.family, m)
		if m.n > m.size && segmented {
			oob := segmentControl(s.oob[i*segmentSpace:(i+1)*segmentSpace], m.size)
			h.Control = &oob[0]
			h.SetControllen(len(oob))
		}
	}
	sent := 0
	err = rc.Write(func(fd uintptr) bool {
		for sent < n {
			if hdrs[sent].hdr.Namelen == 0 {
				// An address the socket cannot send to, as one of IPv6 on
				// a socket of IPv4: dropped, as a write of it fails.
				sent++
				continue
			}
			r, _, errno := syscall.Syscall6(sysSendmmsg, fd, uintptr(unsafe.Pointer(&hdrs[sent])), uintptr(n-sent), 0, 0, 0)
			switch {
			case errno == syscall.EAGAIN:
				return false
			case errno == syscall.EINTR:
				continue
			case errno != 0:
				// The message at sent failed: segmented, the kernel may
				// not take UDP_SEGMENT, and it goes as a connection
				// sends it; else it is lost, as a datagram may be.
				if hdrs[sent].hdr.Controllen != 0 && (errors.Is(errno, syscall.EINVAL) || errors.Is(errno, syscall.EIO) ||
					errors.Is(errno, syscall.EOPNOTSUPP) || errors.Is(errno, syscall.ENOPROTOOPT)) {
					segmentsBroken.Store(true)
					sb.sendOne(sb.msgs[sent])
				}
				sent++
			default:
				sent += int(r)
			}
		}
		return true
	})
	if err != nil && sent < n {
		sb.sendEach(sent)
	}
	// The headers point into the buffer of the batch, which is kept.
	clear(iovs)
	clear(hdrs)
}

// netInterfaceIndex returns the index of the interface of an IPv6 zone.
func netInterfaceIndex(zone string) (uint32, error) {
	if n, err := strconv.ParseUint(zone, 10, 32); err == nil {
		return uint32(n), nil
	}
	ifi, err := net.InterfaceByName(zone)
	if err != nil {
		return 0, err
	}
	return uint32(ifi.Index), nil
}

// sendEach sends the messages from the one at i on their own.
func (sb *sockBatch) sendEach(i int) {
	for _, m := range sb.msgs[i:] {
		sb.sendOne(m)
	}
}

// sockaddr writes the address of a message as the socket of a family
// takes it into sa and returns its length, zero if the socket cannot send
// to it.
func sockaddr(sa *syscall.RawSockaddrInet6, family uint16, m batchMsg) uint32 {
	addr, port := m.to.Addr(), m.to.Port()
	if family == syscall.AF_INET {
		if addr = addr.Unmap(); !addr.Is4() {
			return 0
		}
		sa4 := (*syscall.RawSockaddrInet4)(unsafe.Pointer(sa))
		sa4.Family = syscall.AF_INET
		p := (*[2]byte)(unsafe.Pointer(&sa4.Port))
		p[0], p[1] = byte(port>>8), byte(port)
		sa4.Addr = addr.As4()
		return syscall.SizeofSockaddrInet4
	}
	sa.Family = syscall.AF_INET6
	p := (*[2]byte)(unsafe.Pointer(&sa.Port))
	p[0], p[1] = byte(port>>8), byte(port)
	sa.Addr = addr.As16()
	if z := addr.Zone(); z != "" {
		if ifi, err := netInterfaceIndex(z); err == nil {
			sa.Scope_id = ifi
		}
	}
	return syscall.SizeofSockaddrInet6
}
