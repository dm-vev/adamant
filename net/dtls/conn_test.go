package dtls

import (
	"bytes"
	"errors"
	"math/rand/v2"
	"testing"
	"time"
)

// network carries datagrams between two Conns on a virtual clock, losing,
// duplicating and reordering them as its knobs say.
type network struct {
	t       *testing.T
	now     time.Time
	rng     *rand.Rand
	loss    float64
	dup     float64
	shuffle bool
	ends    [2]*end
}

type end struct {
	conn     *Conn
	inbox    [][]byte
	received [][]byte
	err      error
}

func newNetwork(t *testing.T, seed uint64, client, server Config) *network {
	n := &network{t: t, now: time.Unix(1_700_000_000, 0), rng: rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))}
	n.ends[0] = &end{}
	n.ends[1] = &end{}
	n.ends[0].conn = Client(client, func(b []byte) { n.carry(1, b) })
	n.ends[1].conn = Server(server, func(b []byte) { n.carry(0, b) })
	return n
}

func (n *network) carry(to int, b []byte) {
	if n.rng.Float64() < n.loss {
		return
	}
	copies := 1
	if n.rng.Float64() < n.dup {
		copies = 2
	}
	for range copies {
		n.ends[to].inbox = append(n.ends[to].inbox, bytes.Clone(b))
	}
}

// run delivers datagrams and fires timers until both ends are established
// or failed, or until the virtual deadline.
func (n *network) run(limit time.Duration, done func() bool) {
	end := n.now.Add(limit)
	for n.now.Before(end) && !done() {
		moved := false
		for i, e := range n.ends {
			inbox := e.inbox
			e.inbox = nil
			if n.shuffle {
				n.rng.Shuffle(len(inbox), func(a, b int) { inbox[a], inbox[b] = inbox[b], inbox[a] })
			}
			for _, b := range inbox {
				moved = true
				if err := e.conn.HandleDatagram(b, n.now, func(p []byte) {
					e.received = append(e.received, bytes.Clone(p))
				}); err != nil && e.err == nil {
					e.err = err
				}
			}
			_ = i
		}
		if moved {
			n.now = n.now.Add(5 * time.Millisecond)
			continue
		}
		// Nothing in flight: jump to the earliest timer.
		next := time.Time{}
		for _, e := range n.ends {
			if d := e.conn.Deadline(); !d.IsZero() && (next.IsZero() || d.Before(next)) {
				next = d
			}
		}
		if next.IsZero() {
			return
		}
		if next.After(n.now) {
			n.now = next
		}
		for _, e := range n.ends {
			if d := e.conn.Deadline(); !d.IsZero() && !n.now.Before(d) {
				if err := e.conn.HandleTimeout(n.now); err != nil && e.err == nil {
					e.err = err
				}
			}
		}
	}
}

func (n *network) established() bool {
	return n.ends[0].conn.Established() && n.ends[1].conn.Established()
}

// pairConfigs returns configurations whose peers accept each other's
// certificate by fingerprint, as the signaling of WebRTC arranges.
func pairConfigs(t *testing.T) (Config, Config) {
	t.Helper()
	clientCert, err := GenerateCertificate()
	if err != nil {
		t.Fatal(err)
	}
	serverCert, err := GenerateCertificate()
	if err != nil {
		t.Fatal(err)
	}
	expect := func(fp string) func([][]byte) error {
		return func(chain [][]byte) error {
			if len(chain) == 0 || !MatchFingerprint(chain[0], "sha-256", fp) {
				return errors.New("fingerprint mismatch")
			}
			return nil
		}
	}
	client := Config{Certificate: clientCert, VerifyPeer: expect(Fingerprint(serverCert.Chain[0]))}
	server := Config{Certificate: serverCert, VerifyPeer: expect(Fingerprint(clientCert.Chain[0]))}
	return client, server
}

func (n *network) start() {
	n.ends[1].conn.Start(n.now)
	n.ends[0].conn.Start(n.now)
}

func (n *network) exchange(t *testing.T) {
	t.Helper()
	if err := n.ends[0].conn.Write([]byte("from client")); err != nil {
		t.Fatal(err)
	}
	if err := n.ends[1].conn.Write([]byte("from server")); err != nil {
		t.Fatal(err)
	}
	n.run(time.Second, func() bool { return len(n.ends[0].received) > 0 && len(n.ends[1].received) > 0 })
	if len(n.ends[1].received) != 1 || string(n.ends[1].received[0]) != "from client" {
		t.Fatalf("server received %q", n.ends[1].received)
	}
	if len(n.ends[0].received) != 1 || string(n.ends[0].received[0]) != "from server" {
		t.Fatalf("client received %q", n.ends[0].received)
	}
}

func TestHandshake(t *testing.T) {
	client, server := pairConfigs(t)
	n := newNetwork(t, 1, client, server)
	n.start()
	n.run(time.Minute, n.established)
	if !n.established() {
		t.Fatalf("not established: client %v, server %v", n.ends[0].err, n.ends[1].err)
	}
	n.exchange(t)
}

func TestHandshakeHelloVerify(t *testing.T) {
	client, server := pairConfigs(t)
	server.HelloVerify = true
	n := newNetwork(t, 2, client, server)
	n.start()
	n.run(time.Minute, n.established)
	if !n.established() {
		t.Fatalf("not established: client %v, server %v", n.ends[0].err, n.ends[1].err)
	}
	n.exchange(t)
}

func TestHandshakeFragmented(t *testing.T) {
	client, server := pairConfigs(t)
	client.MTU, server.MTU = 120, 150
	n := newNetwork(t, 3, client, server)
	n.start()
	n.run(time.Minute, n.established)
	if !n.established() {
		t.Fatalf("not established: client %v, server %v", n.ends[0].err, n.ends[1].err)
	}
	n.exchange(t)
}

func TestHandshakeLossyNetwork(t *testing.T) {
	for seed := range uint64(40) {
		client, server := pairConfigs(t)
		client.MTU, server.MTU = 300, 300
		server.HelloVerify = seed%2 == 0
		n := newNetwork(t, seed, client, server)
		n.loss, n.dup, n.shuffle = 0.25, 0.2, true
		n.start()
		n.run(10*time.Minute, func() bool {
			return n.established() || n.ends[0].err != nil || n.ends[1].err != nil
		})
		if !n.established() {
			t.Fatalf("seed %d: not established: client %v, server %v", seed, n.ends[0].err, n.ends[1].err)
		}
		// The server may still be waiting to learn that its last flight
		// arrived; data from the client confirms it.
		n.loss = 0
		n.exchange(t)
	}
}

func TestHandshakeRejectsWrongFingerprint(t *testing.T) {
	client, server := pairConfigs(t)
	other, _ := pairConfigs(t)
	server.VerifyPeer = other.VerifyPeer
	n := newNetwork(t, 4, client, server)
	n.start()
	n.run(time.Minute, func() bool { return n.ends[0].err != nil && n.ends[1].err != nil })
	if n.established() {
		t.Fatal("established with a certificate the server does not expect")
	}
	var alert *AlertError
	if !errors.As(n.ends[1].err, &alert) || alert.Description != alertBadCertificate {
		t.Fatalf("server error %v", n.ends[1].err)
	}
	if !errors.As(n.ends[0].err, &alert) || !alert.Remote {
		t.Fatalf("client error %v", n.ends[0].err)
	}
}

func TestHandshakeTimesOut(t *testing.T) {
	client, server := pairConfigs(t)
	n := newNetwork(t, 5, client, server)
	n.loss = 1
	n.start()
	n.run(time.Hour, func() bool { return n.ends[0].err != nil })
	if !errors.Is(n.ends[0].err, errTimeout) {
		t.Fatalf("client error %v, want a timeout", n.ends[0].err)
	}
}

func TestReplayedRecordDeliveredOnce(t *testing.T) {
	client, server := pairConfigs(t)
	n := newNetwork(t, 6, client, server)
	n.start()
	n.run(time.Minute, n.established)
	if !n.established() {
		t.Fatal("not established")
	}
	n.dup = 1
	n.exchange(t)
}

func TestCloseNotify(t *testing.T) {
	client, server := pairConfigs(t)
	n := newNetwork(t, 7, client, server)
	n.start()
	n.run(time.Minute, n.established)
	n.ends[0].conn.Close()
	n.run(time.Second, func() bool { return n.ends[1].err != nil })
	if !errors.Is(n.ends[1].err, ErrClosed) {
		t.Fatalf("server error %v, want ErrClosed", n.ends[1].err)
	}
	if err := n.ends[0].conn.Write([]byte("late")); !errors.Is(err, ErrClosed) {
		t.Fatalf("write after close: %v", err)
	}
}

func TestReplayWindow(t *testing.T) {
	var w replayWindow
	for _, seq := range []uint64{5, 3, 66, 7, 6} {
		if !w.fresh(seq) {
			t.Fatalf("%d rejected", seq)
		}
		w.accept(seq)
	}
	for _, seq := range []uint64{5, 66, 3, 6, 2} {
		if w.fresh(seq) {
			t.Fatalf("%d accepted twice or too old", seq)
		}
	}
	if !w.fresh(65) || !w.fresh(67) || !w.fresh(4) {
		t.Fatal("fresh sequence numbers rejected")
	}
}

func FuzzHandleDatagram(f *testing.F) {
	f.Add([]byte{22, 0xfe, 0xfd, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0})
	clientCert, _ := GenerateCertificate()
	f.Fuzz(func(t *testing.T, b []byte) {
		for _, client := range []bool{false, true} {
			cfg := Config{Certificate: clientCert, VerifyPeer: func([][]byte) error { return nil }}
			var c *Conn
			if client {
				c = Client(cfg, func([]byte) {})
			} else {
				c = Server(cfg, func([]byte) {})
			}
			now := time.Unix(1, 0)
			c.Start(now)
			_ = c.HandleDatagram(bytes.Clone(b), now, func([]byte) {})
		}
	})
}

// TestHandshakeSettlesSRTPProfile negotiates use_srtp: WebRTC peers fail
// a handshake without a profile even when no SRTP runs.
func TestHandshakeSettlesSRTPProfile(t *testing.T) {
	client, server := pairConfigs(t)
	n := newNetwork(t, 9, client, server)
	n.start()
	n.run(time.Minute, n.established)
	c, s := n.ends[0].conn, n.ends[1].conn
	if c.srtpProfile != srtpAEADAES128GCM || s.srtpProfile != srtpAEADAES128GCM {
		t.Fatalf("profiles %#x and %#x", c.srtpProfile, s.srtpProfile)
	}
}
