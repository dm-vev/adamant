package nethernet

import (
	"bytes"
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"net/netip"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/dm-vev/adamant/net/dtls"
)

const (
	server = 0
	client = 1
)

type datagram struct {
	b    []byte
	from netip.AddrPort
}

// end is one side of a virtual network.
type end struct {
	c    *Conn
	addr netip.AddrPort
	// lastRecord is the last DTLS application data datagram sent to it.
	lastRecord []byte
	inbox      []datagram
	received   [][]byte
	ready      bool
}

// vnet carries the datagrams of a server and a client Conn on a virtual
// clock, losing some if asked.
type vnet struct {
	t    *testing.T
	now  time.Time
	rng  *rand.Rand
	loss float64
	ends [2]*end
	// batched sends DTLS datagrams in batches, as GSO does; batches counts
	// the batches of more than one datagram.
	batched bool
	batches int
}

func mustCert(t *testing.T) dtls.Certificate {
	t.Helper()
	cert, err := dtls.GenerateCertificate()
	if err != nil {
		t.Fatal(err)
	}
	return cert
}

// newVnet connects a server and a client. setup may change the
// descriptions before the connections are made.
func newVnet(t *testing.T, seed uint64, serverDTLSClient bool, setup func(descs [2]*description)) *vnet {
	n := &vnet{t: t, now: time.Unix(1_800_000_000, 0), rng: rand.New(rand.NewPCG(seed, ^seed))}
	addrs := [2]netip.AddrPort{mustAddr("10.0.0.1:19132"), mustAddr("10.0.0.2:50000")}
	certs := [2]dtls.Certificate{mustCert(t), mustCert(t)}
	var descs [2]*description
	for i := range descs {
		descs[i] = &description{
			ufrag: randomString(8), pwd: randomString(24),
			fingerprints:   []Fingerprint{{"sha-256", dtls.Fingerprint(certs[i].Chain[0])}},
			maxMessageSize: maxMessageSize, sctpPort: 5000,
			candidates: []netip.AddrPort{addrs[i]},
		}
		n.ends[i] = &end{addr: addrs[i]}
	}
	if setup != nil {
		setup(descs)
	}
	for i, e := range n.ends {
		other := n.ends[1-i]
		e.c = newConn(connConfig{
			local: descs[i], remote: descs[1-i],
			controlling: i == client,
			dtlsClient:  (i == server) == serverDTLSClient,
			opener:      i == client,
			certificate: certs[i],
			clock:       func() time.Time { return n.now },
			out:         func(b []byte, to netip.AddrPort) { n.carry(other, e.addr, to, b) },
			outBatch: func(b []byte, size int, to netip.AddrPort) {
				n.carryBatch(other, e.addr, to, b, size)
			},
		}, n.now)
		if !n.batched {
			e.c.outBatch = nil
		}
		e.c.onReady = func(*Conn) { e.ready = true }
		e.c.SetReceiver(func(msg []byte) { e.received = append(e.received, msg) })
	}
	return n
}

func (n *vnet) carry(to *end, from, dst netip.AddrPort, b []byte) {
	if dst != to.addr || n.rng.Float64() < n.loss {
		return
	}
	if len(b) > 0 && b[0] == 23 {
		to.lastRecord = bytes.Clone(b)
	}
	to.inbox = append(to.inbox, datagram{bytes.Clone(b), from})
}

// carryBatch checks a batch is what the kernel segments, then carries its
// datagrams.
func (n *vnet) carryBatch(to *end, from, dst netip.AddrPort, b []byte, size int) {
	n.batches++
	if count := (len(b) + size - 1) / size; count < 2 || count > maxBatchedDatagrams || len(b) > maxBatchedBytes {
		n.t.Errorf("batch of %d bytes in datagrams of %d", len(b), size)
	}
	for len(b) > 0 {
		k := min(size, len(b))
		n.carry(to, from, dst, b[:k])
		b = b[k:]
	}
}

func (e *end) deadline() time.Time {
	e.c.mu.Lock()
	defer e.c.mu.Unlock()
	return e.c.nextDeadline()
}

func (n *vnet) fire() {
	for _, e := range n.ends {
		if d := e.deadline(); !d.IsZero() && !n.now.Before(d) {
			e.c.mu.Lock()
			e.c.handleTimeout(n.now)
			e.c.release()
		}
	}
}

func (n *vnet) run(limit time.Duration, done func() bool) {
	end := n.now.Add(limit)
	for n.now.Before(end) && !done() {
		moved := false
		for _, e := range n.ends {
			inbox := e.inbox
			e.inbox = nil
			for _, d := range inbox {
				moved = true
				e.c.handleDatagram(d.b, d.from, n.now)
			}
		}
		if moved {
			n.now = n.now.Add(2 * time.Millisecond)
			n.fire()
			continue
		}
		var next time.Time
		for _, e := range n.ends {
			if d := e.deadline(); !d.IsZero() && (next.IsZero() || d.Before(next)) {
				next = d
			}
		}
		if next.IsZero() {
			return
		}
		if next.After(n.now) {
			n.now = next
		}
		n.fire()
	}
}

func (n *vnet) opened() bool { return n.ends[server].ready && n.ends[client].ready }

func (n *vnet) open(t *testing.T) {
	t.Helper()
	n.run(time.Minute, n.opened)
	if !n.opened() {
		t.Fatalf("not open: server %v, client %v", n.ends[server].c.Err(), n.ends[client].c.Err())
	}
}

func message(seed, size int) []byte {
	b := make([]byte, size)
	r := rand.New(rand.NewPCG(uint64(seed), uint64(size)))
	for i := range b {
		b[i] = byte(r.Uint32())
	}
	return b
}

func TestConnOpens(t *testing.T) {
	for _, serverDTLSClient := range []bool{true, false} {
		t.Run(fmt.Sprint("serverDTLSClient=", serverDTLSClient), func(t *testing.T) {
			n := newVnet(t, 1, serverDTLSClient, nil)
			n.open(t)
			srv, cli := n.ends[server].c, n.ends[client].c
			// The DTLS server opens odd streams, the client even ones.
			want := uint16(1)
			if !serverDTLSClient {
				want = 0
			}
			if cli.reliable.stream != want || srv.reliable.stream != want || srv.unreliable.stream != want+2 {
				t.Fatalf("streams: client %d, server %d and %d", cli.reliable.stream, srv.reliable.stream, srv.unreliable.stream)
			}
			if srv.RemoteAddr().String() != n.ends[client].addr.String() || cli.RemoteAddr().String() != n.ends[server].addr.String() {
				t.Fatalf("remote addresses %v and %v", srv.RemoteAddr(), cli.RemoteAddr())
			}
		})
	}
}

// exchange sends messages both ways and checks they arrive whole and in
// order.
func exchange(t *testing.T, n *vnet, sizes []int) {
	t.Helper()
	srv, cli := n.ends[server], n.ends[client]
	for i, size := range sizes {
		if err := cli.c.Send(message(i, size)); err != nil {
			t.Fatalf("client send %d: %v", size, err)
		}
		if err := srv.c.Send(message(1000+i, size)); err != nil {
			t.Fatalf("server send %d: %v", size, err)
		}
	}
	n.run(5*time.Minute, func() bool { return len(srv.received) == len(sizes) && len(cli.received) == len(sizes) })
	if len(srv.received) != len(sizes) || len(cli.received) != len(sizes) {
		t.Fatalf("received %d and %d of %d messages: %v, %v", len(srv.received), len(cli.received), len(sizes), srv.c.Err(), cli.c.Err())
	}
	for i, size := range sizes {
		if !bytes.Equal(srv.received[i], message(i, size)) {
			t.Fatalf("server message %d: %d bytes, want %d", i, len(srv.received[i]), size)
		}
		if !bytes.Equal(cli.received[i], message(1000+i, size)) {
			t.Fatalf("client message %d: %d bytes, want %d", i, len(cli.received[i]), size)
		}
	}
	srv.received, cli.received = nil, nil
}

func TestConnExchange(t *testing.T) {
	n := newVnet(t, 2, true, nil)
	n.open(t)
	exchange(t, n, []int{1, 100, 1200, 10000, maxMessageSize - 2, maxMessageSize - 1, maxMessageSize, 3*maxMessageSize + 5, 4 << 20})
}

func TestConnLossy(t *testing.T) {
	for seed := range uint64(4) {
		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			n := newVnet(t, seed, seed%2 == 0, nil)
			n.loss = 0.1
			n.open(t)
			r := rand.New(rand.NewPCG(seed, 7))
			sizes := make([]int, 40)
			for i := range sizes {
				sizes[i] = 1 + r.IntN(100000)
			}
			exchange(t, n, sizes)
		})
	}
}

// TestConnSegments splits messages into many small segments: each
// segment counts the ones still to come.
func TestConnSegments(t *testing.T) {
	n := newVnet(t, 3, true, nil)
	n.open(t)
	for _, e := range n.ends {
		e.c.maxSegment = 7
	}
	exchange(t, n, []int{1, 6, 7, 8, 14, 15, 100, 255 * 7})
	if err := n.ends[client].c.Send(make([]byte, 255*7+1)); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("message of 256 segments: %v", err)
	}
}

func TestConnRejectsMalformedSegments(t *testing.T) {
	for _, tc := range []struct {
		name     string
		segments [][]byte
		want     []string
		fail     bool
	}{
		{"whole", [][]byte{{0, 'a'}}, []string{"a"}, false},
		{"split", [][]byte{{2, 'a'}, {1, 'b'}, {0, 'c'}, {0, 'd'}}, []string{"abc", "d"}, false},
		{"empty payload", [][]byte{{0}, {1}, {0, 'x'}}, []string{"x"}, false},
		{"skipped count", [][]byte{{2, 'a'}, {0, 'b'}}, nil, true},
		{"repeated count", [][]byte{{1, 'a'}, {1, 'b'}}, nil, true},
		{"empty segment", [][]byte{{}}, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n := newVnet(t, 4, true, nil)
			n.open(t)
			c := n.ends[server].c
			for _, seg := range tc.segments {
				c.mu.Lock()
				c.handleSegment(seg)
				c.release()
			}
			var got []string
			for _, msg := range n.ends[server].received {
				got = append(got, string(msg))
			}
			if fmt.Sprint(got) != fmt.Sprint(tc.want) {
				t.Fatalf("delivered %q, want %q", got, tc.want)
			}
			if err := c.Err(); tc.fail != errors.Is(err, errSegment) {
				t.Fatalf("error %v", err)
			}
		})
	}
}

func TestConnUnreliable(t *testing.T) {
	n := newVnet(t, 5, true, nil)
	n.open(t)
	if err := n.ends[client].c.SendUnreliable([]byte("move")); err != nil {
		t.Fatal(err)
	}
	if err := n.ends[server].c.SendUnreliable([]byte("tick")); err != nil {
		t.Fatal(err)
	}
	n.run(time.Second, func() bool { return len(n.ends[server].received) == 1 && len(n.ends[client].received) == 1 })
	if len(n.ends[server].received) != 1 || string(n.ends[server].received[0]) != "move" || len(n.ends[client].received) != 1 || string(n.ends[client].received[0]) != "tick" {
		t.Fatalf("received %q and %q", n.ends[server].received, n.ends[client].received)
	}
}

// TestConnCloseDeliversLastMessage closes right after a send: the message
// still arrives, then the peer sees the close.
func TestConnCloseDeliversLastMessage(t *testing.T) {
	for seed := range uint64(4) {
		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			n := newVnet(t, seed, true, nil)
			n.open(t)
			n.loss = 0.1 * float64(seed%2)
			srv, cli := n.ends[server], n.ends[client]
			last := message(9, 50000)
			if err := srv.c.Send(last); err != nil {
				t.Fatal(err)
			}
			_ = srv.c.Close()
			if err := srv.c.Send(last); !errors.Is(err, net.ErrClosed) {
				t.Fatalf("send after close: %v", err)
			}
			n.run(time.Minute, func() bool { return cli.c.Err() != nil && srv.c.finished && cli.c.finished })
			if len(cli.received) != 1 || !bytes.Equal(cli.received[0], last) {
				t.Fatalf("client received %d messages", len(cli.received))
			}
			if !errors.Is(cli.c.Err(), errPeerClosed) || !errors.Is(srv.c.Err(), net.ErrClosed) {
				t.Fatalf("errors %v and %v", cli.c.Err(), srv.c.Err())
			}
			if !srv.c.finished || !cli.c.finished {
				t.Fatalf("transports still running: %v, %v", srv.c.finished, cli.c.finished)
			}
		})
	}
}

func TestConnExpiresUnopened(t *testing.T) {
	n := newVnet(t, 6, true, nil)
	n.loss = 1
	n.run(time.Minute, func() bool { return n.ends[server].c.Err() != nil && n.ends[client].c.Err() != nil })
	for i, e := range n.ends {
		if !errors.Is(e.c.Err(), errOpenTimeout) {
			t.Fatalf("end %d: %v", i, e.c.Err())
		}
	}
}

func TestConnRejectsWrongFingerprint(t *testing.T) {
	n := newVnet(t, 7, true, func(descs [2]*description) {
		descs[server].fingerprints = []Fingerprint{{"sha-256", dtls.Fingerprint([]byte("another certificate"))}}
	})
	n.run(time.Minute, func() bool { return n.ends[server].c.Err() != nil && n.ends[client].c.Err() != nil })
	if n.opened() || n.ends[client].c.Err() == nil {
		t.Fatalf("opened with a certificate that does not match: %v", n.ends[client].c.Err())
	}
}

func TestConnPeerStopsAnswering(t *testing.T) {
	n := newVnet(t, 8, true, nil)
	n.open(t)
	n.loss = 1
	if err := n.ends[server].c.Send([]byte("lost")); err != nil {
		t.Fatal(err)
	}
	n.run(5*time.Minute, func() bool { return n.ends[server].c.Err() != nil && n.ends[client].c.Err() != nil })
	if n.ends[server].c.Err() == nil || n.ends[client].c.Err() == nil {
		t.Fatalf("still open: %v, %v", n.ends[server].c.Err(), n.ends[client].c.Err())
	}
}

// TestConnReceiverKeepsOrder queues messages before a receiver is set:
// they reach it first, in order.
func TestConnReceiverKeepsOrder(t *testing.T) {
	n := newVnet(t, 9, true, nil)
	srv := n.ends[server]
	srv.c.SetReceiver(nil)
	n.open(t)
	for i := range 5 {
		if err := n.ends[client].c.Send([]byte{byte(i)}); err != nil {
			t.Fatal(err)
		}
	}
	n.run(time.Second, func() bool { return len(srv.c.queue)-srv.c.head == 5 })
	if msg, err := srv.c.ReadMessage(); err != nil || !bytes.Equal(msg, []byte{0}) {
		t.Fatalf("read %v, %v", msg, err)
	}
	srv.c.SetReceiver(func(msg []byte) { srv.received = append(srv.received, msg) })
	if err := n.ends[client].c.Send([]byte{5}); err != nil {
		t.Fatal(err)
	}
	n.run(time.Second, func() bool { return len(srv.received) == 5 })
	if !bytes.Equal(bytes.Join(srv.received, nil), []byte{1, 2, 3, 4, 5}) {
		t.Fatalf("received %v", srv.received)
	}
}

func TestConnReadDeadline(t *testing.T) {
	n := newVnet(t, 10, true, nil)
	n.open(t)
	c := n.ends[server].c
	c.SetReceiver(nil)
	_ = c.SetReadDeadline(time.Now().Add(10 * time.Millisecond))
	if _, err := c.ReadMessage(); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("read past the deadline: %v", err)
	}
	// A deadline set while a read waits ends the read: so a context ends a
	// login.
	_ = c.SetReadDeadline(time.Time{})
	done := make(chan error, 1)
	go func() {
		_, err := c.ReadMessage()
		done <- err
	}()
	time.Sleep(10 * time.Millisecond)
	_ = c.SetReadDeadline(time.Now())
	select {
	case err := <-done:
		if !errors.Is(err, os.ErrDeadlineExceeded) {
			t.Fatalf("waiting read ended with %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a deadline set while the read waited did not end it")
	}
	_ = c.SetReadDeadline(time.Time{})
	_ = c.Close()
	if _, err := c.ReadMessage(); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("read after close: %v", err)
	}
}

// TestConnIgnoresUnprovenAddress replays a DTLS datagram of the client
// from an address ICE did not prove: the server keeps sending to the
// client.
func TestConnIgnoresUnprovenAddress(t *testing.T) {
	n := newVnet(t, 11, true, nil)
	n.open(t)
	srv, cli := n.ends[server], n.ends[client]
	// Let the delayed acknowledgements go: the client sends nothing after.
	n.run(time.Second, func() bool { return false })
	if srv.lastRecord == nil {
		t.Fatal("no DTLS record reached the server")
	}
	srv.c.handleDatagram(bytes.Clone(srv.lastRecord), mustAddr("10.0.0.3:1"), n.now)
	if err := srv.c.Send([]byte("after")); err != nil {
		t.Fatal(err)
	}
	n.run(3*time.Second, func() bool { return len(cli.received) == 1 })
	if len(cli.received) != 1 {
		t.Fatalf("client received %d messages; server sends to %v", len(cli.received), srv.c.RemoteAddr())
	}
}

// TestConnMemory bounds the memory of an open connection: the plan of the
// stack allows 100 KB, where pion took about 2 MB.
func TestConnMemory(t *testing.T) {
	const pairs = 50
	nets := make([]*vnet, 0, pairs)
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	for i := range pairs {
		n := newVnet(t, uint64(i), true, nil)
		n.open(t)
		exchange(t, n, []int{100, 3000})
		nets = append(nets, n)
	}
	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	perConn := (int64(after.HeapAlloc) - int64(before.HeapAlloc)) / (2 * pairs)
	t.Logf("%d bytes per open connection", perConn)
	if perConn > 100<<10 {
		t.Fatalf("an open connection holds %d bytes", perConn)
	}
	runtime.KeepAlive(nets)
}

// TestConnBatched sends the datagrams of each event in batches, as GSO
// does, over a lossy network.
func TestConnBatched(t *testing.T) {
	for seed := range uint64(3) {
		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			n := newVnetBatched(t, seed)
			n.loss = 0.05 * float64(seed)
			n.open(t)
			exchange(t, n, []int{1, 3000, 100000, 1 << 20})
			if n.batches == 0 {
				t.Fatal("no event sent more than one datagram at once")
			}
			t.Logf("%d batches", n.batches)
		})
	}
}

// newVnetBatched is newVnet with the datagrams of each event batched.
func newVnetBatched(t *testing.T, seed uint64) *vnet {
	n := newVnet(t, seed, seed%2 == 0, nil)
	n.batched = true
	for i, e := range n.ends {
		other, from := n.ends[1-i], e.addr
		e.c.outBatch = func(b []byte, size int, to netip.AddrPort) { n.carryBatch(other, from, to, b, size) }
	}
	return n
}

// TestConnOnClose tells of the close once, after the messages a receiver
// was handed, and at once to a closer set after the close.
func TestConnOnClose(t *testing.T) {
	n := newVnet(t, 12, true, nil)
	srv := n.ends[server]
	n.open(t)
	var events []string
	srv.c.SetReceiver(func(msg []byte) { events = append(events, "msg") })
	srv.c.OnClose(func(err error) { events = append(events, "closed") })
	if err := n.ends[client].c.Send([]byte{1}); err != nil {
		t.Fatal(err)
	}
	n.run(time.Second, func() bool { return len(events) == 1 })
	_ = n.ends[client].c.Close()
	n.run(5*time.Second, func() bool { return len(events) == 2 })
	_ = srv.c.Close()
	if len(events) != 2 || events[0] != "msg" || events[1] != "closed" {
		t.Fatalf("events %v", events)
	}
	late := make(chan error, 1)
	srv.c.OnClose(func(err error) { late <- err })
	select {
	case err := <-late:
		if err == nil {
			t.Fatal("closed without a reason")
		}
	default:
		t.Fatal("a closer set after the close was not called at once")
	}
}
