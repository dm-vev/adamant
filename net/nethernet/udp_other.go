//go:build !linux || mips || mipsle || mips64 || mips64le || sparc64

package nethernet

import "syscall"

// reusePortSupported reports whether sockets can share a port, the kernel
// spreading datagrams over them by the address of the peer.
const reusePortSupported = false

func reusePort(_, _ string, _ syscall.RawConn) error { return nil }
