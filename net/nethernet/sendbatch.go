package nethernet

import (
	"net"
	"net/netip"
)

// maxKeptSendBatch bounds the bytes of datagrams a SendBatch keeps the
// buffer of between flushes.
const maxKeptSendBatch = 1 << 20

// SendBatch gathers the datagrams that the connections it is set on send,
// so that those of each socket go out in one call, sendmmsg on Linux, the
// datagrams a connection sends at once segmented by the kernel as they are
// alone. A goroutine that writes to many connections sets it on each in
// turn while it writes, then calls Flush, rather than make a call of its
// own for each connection. It is set on one connection at a time, which
// orders what goes into it, and Flush is called once it is set on none.
type SendBatch struct {
	socks []*sockBatch
}

// sockBatch holds the datagrams gathered for a socket.
type sockBatch struct {
	sock *net.UDPConn
	buf  []byte
	msgs []batchMsg
	sys  sysBatch
}

// batchMsg is what a connection sent at once: n bytes at off of datagrams
// of size bytes each but maybe the last, to an address.
type batchMsg struct {
	off, n, size int
	to           netip.AddrPort
}

// NewSendBatch returns a SendBatch that holds nothing.
func NewSendBatch() *SendBatch { return &SendBatch{} }

// add gathers datagrams of size bytes back to back in p, the last maybe
// shorter, sent to to on sock. p is copied.
func (b *SendBatch) add(sock *net.UDPConn, p []byte, size int, to netip.AddrPort) {
	var sb *sockBatch
	for _, s := range b.socks {
		if s.sock == sock {
			sb = s
			break
		}
	}
	if sb == nil {
		sb = &sockBatch{sock: sock}
		b.socks = append(b.socks, sb)
	}
	sb.msgs = append(sb.msgs, batchMsg{off: len(sb.buf), n: len(p), size: size, to: to})
	sb.buf = append(sb.buf, p...)
}

// Flush sends the datagrams gathered.
func (b *SendBatch) Flush() {
	for _, sb := range b.socks {
		if len(sb.msgs) == 0 {
			continue
		}
		sb.send()
		sendBatches.Inc()
		sendBatchMessages.Observe(float64(len(sb.msgs)))
		clear(sb.msgs)
		sb.msgs = sb.msgs[:0]
		if cap(sb.buf) > maxKeptSendBatch {
			sb.buf = nil
		} else {
			sb.buf = sb.buf[:0]
		}
	}
}

// sendOne sends a message of a batch on its own, as a connection does.
func (sb *sockBatch) sendOne(m batchMsg) {
	b := sb.buf[m.off : m.off+m.n]
	if m.n > m.size {
		writeSegments(sb.sock, b, m.size, m.to)
		return
	}
	_, _ = sb.sock.WriteToUDPAddrPort(b, m.to)
}
