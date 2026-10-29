package nethernet

import (
	"net"
	"net/netip"
	"sync/atomic"
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
	// id picks the socket of a listener the batch sends on, so that
	// batches of different goroutines send on different ones.
	id    uint32
	socks []*sockBatch
}

// sendGroup is the sockets of a listener, all bound to its address, so
// that a datagram goes out alike on any of them, with the counters of what
// each sends.
type sendGroup struct {
	socks []*net.UDPConn
	count []func(datagrams, n int)
}

// sockBatch holds the datagrams gathered for the sockets of a group, to
// go out on one of them.
type sockBatch struct {
	group *sendGroup
	sock  *net.UDPConn
	count func(datagrams, n int)
	buf   []byte
	msgs  []batchMsg
	sys   sysBatch
}

// sendBatchIDs numbers the batches made.
var sendBatchIDs atomic.Uint32

// batchMsg is what a connection sent at once: n bytes at off of datagrams
// of size bytes each but maybe the last, to an address.
type batchMsg struct {
	off, n, size int
	to           netip.AddrPort
}

// NewSendBatch returns a SendBatch that holds nothing.
func NewSendBatch() *SendBatch { return &SendBatch{id: sendBatchIDs.Add(1)} }

// add gathers datagrams, count of them of size bytes back to back in p,
// the last maybe shorter, sent to to on a socket of g. p is copied.
func (b *SendBatch) add(g *sendGroup, p []byte, size, count int, to netip.AddrPort) {
	var sb *sockBatch
	for _, s := range b.socks {
		if s.group == g {
			sb = s
			break
		}
	}
	if sb == nil {
		i := int(b.id % uint32(len(g.socks)))
		sb = &sockBatch{group: g, sock: g.socks[i], count: g.count[i]}
		b.socks = append(b.socks, sb)
	}
	sb.count(count, len(p))
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
