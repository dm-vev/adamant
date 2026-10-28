package interop

import (
	"bytes"
	"math/rand/v2"
	"net"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/dm-vev/adamant/net/sctp"
	"github.com/pion/logging"
	pionsctp "github.com/pion/sctp"
)

type sctpMessage struct {
	stream uint16
	ppid   uint32
	data   []byte
}

// ourSCTP drives an Association of this module over a UDP socket, losing
// packets in either direction as asked.
type ourSCTP struct {
	pc       *net.UDPConn
	peer     netip.AddrPort
	mu       sync.Mutex
	assoc    *sctp.Association
	received chan sctpMessage
	rng      *rand.Rand
	dropIn   float64
	dropOut  float64
	stop     chan struct{}
	stopped  chan struct{}
}

func runOurSCTP(t *testing.T, pc *net.UDPConn, peer netip.AddrPort, connect bool, dropIn, dropOut float64) *ourSCTP {
	o := &ourSCTP{
		pc: pc, peer: peer, received: make(chan sctpMessage, 4096),
		rng: rand.New(rand.NewPCG(1, 2)), dropIn: dropIn, dropOut: dropOut,
		stop: make(chan struct{}), stopped: make(chan struct{}),
	}
	o.assoc = sctp.New(sctp.Config{
		OnMessage: func(stream uint16, ppid uint32, data []byte) {
			o.received <- sctpMessage{stream, ppid, bytes.Clone(data)}
		},
	}, func(b []byte) {
		if o.rng.Float64() < o.dropOut {
			return
		}
		_, _ = pc.WriteToUDPAddrPort(b, peer)
	})
	if connect {
		o.mu.Lock()
		o.assoc.Connect(time.Now())
		o.mu.Unlock()
	}
	go o.loop()
	t.Cleanup(func() {
		close(o.stop)
		<-o.stopped
	})
	return o
}

func (o *ourSCTP) loop() {
	defer close(o.stopped)
	buf := make([]byte, 2048)
	for {
		select {
		case <-o.stop:
			return
		default:
		}
		o.mu.Lock()
		deadline := o.assoc.Deadline()
		o.mu.Unlock()
		read := time.Now().Add(10 * time.Millisecond)
		if !deadline.IsZero() && deadline.Before(read) {
			read = deadline
		}
		_ = o.pc.SetReadDeadline(read)
		n, from, err := o.pc.ReadFromUDPAddrPort(buf)
		now := time.Now()
		o.mu.Lock()
		if err == nil && from == o.peer && o.rng.Float64() >= o.dropIn {
			_ = o.assoc.HandlePacket(buf[:n], now)
		}
		if d := o.assoc.Deadline(); !d.IsZero() && !now.Before(d) {
			_ = o.assoc.HandleTimeout(now)
		}
		o.mu.Unlock()
	}
}

func (o *ourSCTP) send(t *testing.T, stream uint16, data []byte, unordered bool, maxRetransmits int) {
	t.Helper()
	opts := sctp.SendOptions{Unordered: unordered, Partial: maxRetransmits >= 0, MaxRetransmits: maxRetransmits}
	for end := time.Now().Add(10 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		o.mu.Lock()
		err := o.assoc.Send(stream, sctp.PPIDBinary, data, opts, time.Now())
		o.mu.Unlock()
		if err == nil {
			return
		}
		if err != sctp.ErrBufferFull || time.Now().After(end) {
			t.Fatalf("send: %v", err)
		}
	}
}

func (o *ourSCTP) waitEstablished(t *testing.T) {
	t.Helper()
	for end := time.Now().Add(10 * time.Second); time.Now().Before(end); time.Sleep(5 * time.Millisecond) {
		o.mu.Lock()
		ok, err := o.assoc.Established(), o.assoc.Err()
		o.mu.Unlock()
		if ok {
			return
		}
		if err != nil {
			t.Fatalf("association failed: %v", err)
		}
	}
	t.Fatal("association did not open")
}

// pionConn returns a connected UDP socket for pion and the socket of
// this module's side.
func sctpSockets(t *testing.T) (*net.UDPConn, *net.UDPConn) {
	t.Helper()
	ours := listenLoopback(t)
	pion, err := net.DialUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)}, ours.LocalAddr().(*net.UDPAddr))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pion.Close() })
	return ours, pion
}

func pionSCTPConfig(conn net.Conn) pionsctp.Config {
	return pionsctp.Config{NetConn: conn, LoggerFactory: logging.NewDefaultLoggerFactory(), MaxReceiveBufferSize: 1 << 20}
}

func messageData(i int) []byte {
	size := 1 + (i*7919)%60000
	b := make([]byte, size)
	r := rand.New(rand.NewPCG(uint64(i), uint64(size)))
	for j := range b {
		b[j] = byte(r.Uint32())
	}
	return b
}

func expectMessages(t *testing.T, o *ourSCTP, count int, stream uint16) {
	t.Helper()
	for i := range count {
		select {
		case m := <-o.received:
			if m.stream != stream || !bytes.Equal(m.data, messageData(i)) {
				t.Fatalf("message %d: stream %d, %d bytes", i, m.stream, len(m.data))
			}
		case <-time.After(20 * time.Second):
			t.Fatalf("received %d of %d messages", i, count)
		}
	}
}

func readPion(t *testing.T, s *pionsctp.Stream, count int) {
	t.Helper()
	buf := make([]byte, 1<<17)
	for i := range count {
		_ = s.SetReadDeadline(time.Now().Add(20 * time.Second))
		n, _, err := s.ReadSCTP(buf)
		if err != nil {
			t.Fatalf("pion read %d: %v", i, err)
		}
		if !bytes.Equal(buf[:n], messageData(i)) {
			t.Fatalf("pion message %d: %d bytes", i, n)
		}
	}
}

func testSCTPWithPion(t *testing.T, ourConnects bool, drop float64) {
	oursSock, pionSock := sctpSockets(t)
	ours := runOurSCTP(t, oursSock, addrPort(pionSock.LocalAddr()), ourConnects, drop, drop)
	var assoc *pionsctp.Association
	var err error
	if ourConnects {
		assoc, err = pionsctp.Server(pionSCTPConfig(pionSock))
	} else {
		assoc, err = pionsctp.Client(pionSCTPConfig(pionSock))
	}
	if err != nil {
		t.Fatal(err)
	}
	defer assoc.Close()
	ours.waitEstablished(t)

	const count = 60
	var stream *pionsctp.Stream
	if ourConnects {
		ours.send(t, 1, messageData(0), false, -1)
		if stream, err = assoc.AcceptStream(); err != nil {
			t.Fatal(err)
		}
		readPion(t, stream, 1)
		for i := 1; i < count; i++ {
			ours.send(t, 1, messageData(i), false, -1)
		}
		buf := make([]byte, 1<<17)
		for i := 1; i < count; i++ {
			_ = stream.SetReadDeadline(time.Now().Add(20 * time.Second))
			n, _, err := stream.ReadSCTP(buf)
			if err != nil || !bytes.Equal(buf[:n], messageData(i)) {
				t.Fatalf("pion message %d: %d bytes, %v", i, n, err)
			}
		}
	} else {
		if stream, err = assoc.OpenStream(1, pionsctp.PayloadTypeWebRTCBinary); err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		for i := range count {
			if _, err := stream.WriteSCTP(messageData(i), pionsctp.PayloadTypeWebRTCBinary); err != nil {
				t.Fatal(err)
			}
		}
		t.Logf("pion wrote in %v", time.Since(start))
		expectMessages(t, ours, count, 1)
		t.Logf("received from pion in %v", time.Since(start))
		for i := range count {
			ours.send(t, 1, messageData(i), false, -1)
		}
		t.Logf("queued for pion in %v", time.Since(start))
		readPion(t, stream, count)
		t.Logf("pion read in %v", time.Since(start))
	}
}

func TestSCTPPionClient(t *testing.T)      { testSCTPWithPion(t, false, 0) }
func TestSCTPPionServer(t *testing.T)      { testSCTPWithPion(t, true, 0) }
func TestSCTPPionClientLossy(t *testing.T) { testSCTPWithPion(t, false, 0.1) }
func TestSCTPPionServerLossy(t *testing.T) { testSCTPWithPion(t, true, 0.1) }

// TestSCTPPartialReliabilityWithPion sends unordered messages without
// retransmissions both ways over a lossy path, then a reliable one, which
// must arrive: FORWARD TSN moves past the lost messages.
func TestSCTPPartialReliabilityWithPion(t *testing.T) {
	oursSock, pionSock := sctpSockets(t)
	ours := runOurSCTP(t, oursSock, addrPort(pionSock.LocalAddr()), false, 0, 0)
	assoc, err := pionsctp.Client(pionSCTPConfig(pionSock))
	if err != nil {
		t.Fatal(err)
	}
	defer assoc.Close()
	ours.waitEstablished(t)
	lossy, err := assoc.OpenStream(3, pionsctp.PayloadTypeWebRTCBinary)
	if err != nil {
		t.Fatal(err)
	}
	lossy.SetReliabilityParams(true, pionsctp.ReliabilityTypeRexmit, 0)
	reliable, err := assoc.OpenStream(1, pionsctp.PayloadTypeWebRTCBinary)
	if err != nil {
		t.Fatal(err)
	}

	ours.mu.Lock()
	ours.dropIn, ours.dropOut = 0.3, 0.3
	ours.mu.Unlock()
	for i := range 200 {
		if _, err := lossy.WriteSCTP([]byte{byte(i)}, pionsctp.PayloadTypeWebRTCBinary); err != nil {
			t.Fatal(err)
		}
		ours.send(t, 3, []byte{byte(i)}, true, 0)
		time.Sleep(time.Millisecond)
	}
	ours.mu.Lock()
	ours.dropIn, ours.dropOut = 0, 0
	ours.mu.Unlock()
	if _, err := reliable.WriteSCTP([]byte("done"), pionsctp.PayloadTypeWebRTCBinary); err != nil {
		t.Fatal(err)
	}
	ours.send(t, 1, []byte("done"), false, -1)

	got := 0
	for deadline := time.After(20 * time.Second); ; {
		select {
		case m := <-ours.received:
			if m.stream == 3 {
				got++
				continue
			}
			if string(m.data) != "done" {
				t.Fatalf("reliable message %q", m.data)
			}
		case <-deadline:
			t.Fatal("the reliable message after the lossy ones did not arrive")
		}
		break
	}
	if got == 0 || got == 200 {
		t.Fatalf("received %d of 200 unreliable messages", got)
	}
	buf := make([]byte, 64)
	pionGot := 0
	for {
		_ = reliable.SetReadDeadline(time.Now().Add(20 * time.Second))
		n, _, err := reliable.ReadSCTP(buf)
		if err != nil {
			t.Fatalf("pion did not receive the reliable message: %v", err)
		}
		if string(buf[:n]) == "done" {
			break
		}
	}
	_ = lossy.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	for {
		if _, _, err := lossy.ReadSCTP(buf); err != nil {
			break
		}
		pionGot++
	}
	if pionGot == 200 {
		t.Fatal("pion received every unreliable message over a lossy path")
	}
}
