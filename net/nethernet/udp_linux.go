//go:build linux && !mips && !mipsle && !mips64 && !mips64le && !sparc64

package nethernet

import "syscall"

// soReusePort is SO_REUSEPORT, which package syscall leaves out. It is 15
// on every Linux architecture but MIPS and SPARC.
const soReusePort = 0xf

// reusePortSupported reports whether sockets can share a port, the kernel
// spreading datagrams over them by the address of the peer.
const reusePortSupported = true

func reusePort(_, _ string, c syscall.RawConn) error {
	var err error
	if cerr := c.Control(func(fd uintptr) {
		err = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, soReusePort, 1)
	}); cerr != nil {
		return cerr
	}
	return err
}
