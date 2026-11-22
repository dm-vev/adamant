package nethernet

import (
	"net/netip"
	"testing"
	"time"

	"github.com/dm-vev/adamant/net/dtls"
)

func BenchmarkConnSendRecv(b *testing.B) {
	now := time.Unix(1_800_000_000, 0)
	addrs := [2]netip.AddrPort{mustAddr("10.0.0.1:19132"), mustAddr("10.0.0.2:50000")}
	var certs [2]dtls.Certificate
	var descs [2]*description
	for i := range certs {
		c, err := dtls.GenerateCertificate()
		if err != nil {
			b.Fatal(err)
		}
		certs[i] = c
		descs[i] = &description{
			ufrag: randomString(8), pwd: randomString(24),
			fingerprints:   []Fingerprint{{"sha-256", dtls.Fingerprint(c.Chain[0])}},
			maxMessageSize: maxMessageSize, sctpPort: 5000,
			candidates: []netip.AddrPort{addrs[i]},
		}
	}

	type queuedPkt struct {
		to   int
		from netip.AddrPort
		data []byte
	}
	var q []queuedPkt
	qHead := 0
	enqueue := func(to int, from netip.AddrPort, pkt []byte) {
		var dst []byte
		if len(q) < cap(q) {
			dst = q[:len(q)+1][len(q)].data[:0]
		}
		dst = append(dst[:0], pkt...)
		q = append(q, queuedPkt{to: to, from: from, data: dst})
	}

	var conns [2]*Conn
	var ready [2]bool
	var recvCount int
	for i := range conns {
		other := 1 - i
		from := addrs[i]
		conns[i] = newConn(connConfig{
			local: descs[i], remote: descs[other],
			controlling: i == client,
			dtlsClient:  i == client,
			opener:      i == client,
			certificate: certs[i],
			clock:       func() time.Time { return now },
			out: func(pkt []byte, _ netip.AddrPort) {
				enqueue(other, from, pkt)
			},
			outBatch: func(pkt []byte, size int, _ netip.AddrPort) {
				for len(pkt) > 0 {
					n := min(size, len(pkt))
					enqueue(other, from, pkt[:n])
					pkt = pkt[n:]
				}
			},
		}, now)
		conns[i].onReady = func(*Conn) { ready[i] = true }
		conns[i].SetReceiver(func([]byte) { recvCount++ })
	}

	drain := func() {
		for qHead < len(q) {
			p := q[qHead]
			qHead++
			conns[p.to].handleDatagram(p.data, p.from, now)
		}
		q, qHead = q[:0], 0
	}
	for !ready[0] || !ready[1] {
		drain()
		if ready[0] && ready[1] {
			break
		}
		now = now.Add(5 * time.Millisecond)
		for _, c := range conns {
			c.mu.Lock()
			c.handleTimeout(now)
			c.release()
		}
	}

	payload := make([]byte, 512)
	for i := range payload {
		payload[i] = byte(i)
	}
	// Warm up.
	for range 4 {
		if err := conns[server].Send(payload); err != nil {
			b.Fatal(err)
		}
		drain()
	}

	b.ReportAllocs()
	b.SetBytes(int64(len(payload)))
	b.ResetTimer()
	for b.Loop() {
		if err := conns[server].Send(payload); err != nil {
			b.Fatal(err)
		}
		drain()
	}
	if recvCount == 0 {
		b.Fatal("no messages delivered")
	}
}
