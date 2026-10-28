package sctp

import (
	"bytes"
	"errors"
	"fmt"
	"math/rand/v2"
	"testing"
	"time"
)

type message struct {
	stream uint16
	ppid   uint32
	data   []byte
}

type peer struct {
	assoc    *Association
	inbox    [][]byte
	received []message
	err      error
}

// network carries packets between two associations on a virtual clock.
type network struct {
	t       *testing.T
	now     time.Time
	rng     *rand.Rand
	loss    float64
	dup     float64
	shuffle bool
	// drop, if set, drops the packets it returns true for.
	drop  func(to int, b []byte) bool
	peers [2]*peer
	sent  int
	max   int
}

func newNetwork(t *testing.T, seed uint64, cfg Config) *network {
	n := &network{t: t, now: time.Unix(1_700_000_000, 0), rng: rand.New(rand.NewPCG(seed, ^seed)), max: cfg.MaxPacketSize}
	if n.max == 0 {
		n.max = 1163
	}
	for i := range n.peers {
		p := &peer{}
		c := cfg
		c.OnMessage = func(stream uint16, ppid uint32, data []byte) {
			p.received = append(p.received, message{stream, ppid, bytes.Clone(data)})
		}
		other := 1 - i
		p.assoc = New(c, func(b []byte) { n.carry(other, b) })
		n.peers[i] = p
	}
	return n
}

func (n *network) carry(to int, b []byte) {
	n.sent++
	if len(b) > n.max {
		n.t.Errorf("packet of %d bytes over the limit of %d", len(b), n.max)
	}
	if n.drop != nil && n.drop(to, b) {
		return
	}
	if n.rng.Float64() < n.loss {
		return
	}
	copies := 1
	if n.rng.Float64() < n.dup {
		copies = 2
	}
	for range copies {
		n.peers[to].inbox = append(n.peers[to].inbox, bytes.Clone(b))
	}
}

func (n *network) run(limit time.Duration, done func() bool) {
	end := n.now.Add(limit)
	for n.now.Before(end) && !done() {
		moved := false
		for _, p := range n.peers {
			inbox := p.inbox
			p.inbox = nil
			if n.shuffle {
				n.rng.Shuffle(len(inbox), func(a, b int) { inbox[a], inbox[b] = inbox[b], inbox[a] })
			}
			for _, b := range inbox {
				moved = true
				if err := p.assoc.HandlePacket(b, n.now); err != nil && p.err == nil {
					p.err = err
				}
			}
		}
		if moved {
			n.now = n.now.Add(10 * time.Millisecond)
			n.fireTimers()
			continue
		}
		next := time.Time{}
		for _, p := range n.peers {
			if d := p.assoc.Deadline(); !d.IsZero() && (next.IsZero() || d.Before(next)) {
				next = d
			}
		}
		if next.IsZero() {
			return
		}
		if next.After(n.now) {
			n.now = next
		}
		n.fireTimers()
	}
}

func (n *network) fireTimers() {
	for _, p := range n.peers {
		if d := p.assoc.Deadline(); !d.IsZero() && !n.now.Before(d) {
			if err := p.assoc.HandleTimeout(n.now); err != nil && p.err == nil {
				p.err = err
			}
		}
	}
}

func (n *network) established() bool {
	return n.peers[0].assoc.Established() && n.peers[1].assoc.Established()
}

func (n *network) open(t *testing.T, collide bool) {
	t.Helper()
	n.peers[0].assoc.Connect(n.now)
	if collide {
		n.peers[1].assoc.Connect(n.now)
	}
	n.run(time.Minute, n.established)
	if !n.established() {
		t.Fatalf("not established: %v, %v", n.peers[0].err, n.peers[1].err)
	}
}

func payload(seed, size int) []byte {
	b := make([]byte, size)
	r := rand.New(rand.NewPCG(uint64(seed), uint64(size)))
	for i := range b {
		b[i] = byte(r.Uint32())
	}
	return b
}

func TestOpenAndExchange(t *testing.T) {
	for _, collide := range []bool{false, true} {
		t.Run(fmt.Sprint("collide=", collide), func(t *testing.T) {
			n := newNetwork(t, 1, Config{})
			n.open(t, collide)
			a, b := n.peers[0], n.peers[1]
			sizes := []int{1, 100, 1135, 1136, 5000, 300000}
			for i, size := range sizes {
				if err := a.assoc.Send(1, PPIDBinary, payload(i, size), SendOptions{}, n.now); err != nil {
					t.Fatal(err)
				}
				if err := b.assoc.Send(3, PPIDString, payload(100+i, size), SendOptions{Unordered: true}, n.now); err != nil {
					t.Fatal(err)
				}
			}
			n.run(time.Minute, func() bool {
				return len(a.received) == len(sizes) && len(b.received) == len(sizes) && a.assoc.Buffered() == 0 && b.assoc.Buffered() == 0
			})
			for i, size := range sizes {
				if got := b.received[i]; got.stream != 1 || got.ppid != PPIDBinary || !bytes.Equal(got.data, payload(i, size)) {
					t.Fatalf("message %d to b: stream %d ppid %d size %d", i, got.stream, got.ppid, len(got.data))
				}
			}
			if len(a.received) != len(sizes) {
				t.Fatalf("a received %d unordered messages", len(a.received))
			}
			for _, m := range a.received {
				if m.stream != 3 || m.ppid != PPIDString {
					t.Fatalf("unordered message on stream %d ppid %d", m.stream, m.ppid)
				}
			}
			if a.assoc.Buffered() != 0 || b.assoc.Buffered() != 0 {
				t.Fatalf("buffered %d and %d after everything was acknowledged", a.assoc.Buffered(), b.assoc.Buffered())
			}
		})
	}
}

func TestReliableOverLossyNetwork(t *testing.T) {
	for seed := range uint64(12) {
		n := newNetwork(t, seed, Config{})
		n.loss, n.dup, n.shuffle = 0.15, 0.05, true
		n.open(t, seed%2 == 1)
		a, b := n.peers[0], n.peers[1]
		const count = 300
		for i := range count {
			size := 50 + (i*733)%4000
			for a.assoc.Send(1, PPIDBinary, payload(i, size), SendOptions{}, n.now) == ErrBufferFull {
				n.run(100*time.Millisecond, func() bool { return false })
			}
		}
		n.run(10*time.Minute, func() bool { return len(b.received) == count || a.err != nil || b.err != nil })
		if len(b.received) != count {
			t.Fatalf("seed %d: received %d of %d: %v %v", seed, len(b.received), count, a.err, b.err)
		}
		for i, m := range b.received {
			if !bytes.Equal(m.data, payload(i, 50+(i*733)%4000)) {
				t.Fatalf("seed %d: message %d out of order or corrupt", seed, i)
			}
		}
		n.loss, n.dup = 0, 0
		n.run(time.Minute, func() bool { return a.assoc.Buffered() == 0 })
		if a.assoc.Buffered() != 0 {
			t.Fatalf("seed %d: %d bytes still buffered", seed, a.assoc.Buffered())
		}
	}
}

func TestPartialReliabilityKeepsGoing(t *testing.T) {
	n := newNetwork(t, 7, Config{})
	n.open(t, false)
	n.loss = 0.3
	a, b := n.peers[0], n.peers[1]
	const count = 400
	for i := range count {
		if err := a.assoc.Send(3, PPIDBinary, payload(i, 200), SendOptions{Unordered: true, Partial: true}, n.now); err != nil {
			t.Fatal(err)
		}
		if i%10 == 0 {
			if err := a.assoc.Send(1, PPIDBinary, []byte{byte(i / 10)}, SendOptions{}, n.now); err != nil {
				t.Fatal(err)
			}
		}
		n.run(20*time.Millisecond, func() bool { return false })
	}
	n.loss = 0
	n.run(5*time.Minute, func() bool { return a.assoc.Buffered() == 0 })
	if a.assoc.Buffered() != 0 {
		t.Fatalf("%d bytes still buffered: abandoned chunks were not skipped", a.assoc.Buffered())
	}
	var lossy, reliable int
	for _, m := range b.received {
		if m.stream == 3 {
			lossy++
		} else {
			if int(m.data[0]) != reliable {
				t.Fatalf("reliable message %d out of order", m.data[0])
			}
			reliable++
		}
	}
	if reliable != count/10 {
		t.Fatalf("reliable messages %d of %d", reliable, count/10)
	}
	if lossy == 0 || lossy == count {
		t.Fatalf("unreliable messages %d of %d", lossy, count)
	}
	if a.err != nil || b.err != nil {
		t.Fatalf("errors %v %v", a.err, b.err)
	}
}

func TestSendBufferLimit(t *testing.T) {
	n := newNetwork(t, 3, Config{SendBuffer: 10000})
	n.open(t, false)
	n.loss = 1
	a := n.peers[0]
	if err := a.assoc.Send(1, PPIDBinary, make([]byte, 6000), SendOptions{}, n.now); err != nil {
		t.Fatal(err)
	}
	if err := a.assoc.Send(1, PPIDBinary, make([]byte, 6000), SendOptions{}, n.now); !errors.Is(err, ErrBufferFull) {
		t.Fatalf("second message: %v, want ErrBufferFull", err)
	}
	if a.assoc.Available() != 4000 {
		t.Fatalf("available %d", a.assoc.Available())
	}
}

func TestAbort(t *testing.T) {
	n := newNetwork(t, 4, Config{})
	n.open(t, false)
	n.peers[0].assoc.Abort()
	n.run(time.Second, func() bool { return n.peers[1].assoc.Err() != nil })
	if !errors.Is(n.peers[1].assoc.Err(), ErrAborted) {
		t.Fatalf("peer error %v", n.peers[1].assoc.Err())
	}
}

func TestShutdown(t *testing.T) {
	n := newNetwork(t, 8, Config{})
	n.open(t, false)
	n.peers[0].assoc.Shutdown(n.now)
	n.run(time.Minute, func() bool { return n.peers[0].assoc.Err() != nil && n.peers[1].assoc.Err() != nil })
	if !errors.Is(n.peers[0].assoc.Err(), ErrClosed) || !errors.Is(n.peers[1].assoc.Err(), ErrClosed) {
		t.Fatalf("errors %v %v", n.peers[0].assoc.Err(), n.peers[1].assoc.Err())
	}
}

func TestPeerStopsAnswering(t *testing.T) {
	n := newNetwork(t, 5, Config{})
	n.open(t, false)
	n.loss = 1
	a := n.peers[0]
	if err := a.assoc.Send(1, PPIDBinary, []byte("hello"), SendOptions{}, n.now); err != nil {
		t.Fatal(err)
	}
	n.run(time.Hour, func() bool { return a.err != nil })
	if !errors.Is(a.err, ErrTimeout) {
		t.Fatalf("error %v, want ErrTimeout", a.err)
	}
}

func TestChannelOpenRoundTrip(t *testing.T) {
	o := ChannelOpen{Type: ChannelPartialRexmitUnordered, Reliability: 0, Label: "UnreliableDataChannel"}
	got, err := ParseChannelOpen(o.Marshal())
	if err != nil || got != o {
		t.Fatalf("round trip %+v, %v", got, err)
	}
	if opts := got.SendOptions(); !opts.Unordered || !opts.Partial || opts.MaxRetransmits != 0 {
		t.Fatalf("options %+v", opts)
	}
	if (ChannelOpen{Type: ChannelReliable}).SendOptions().Partial {
		t.Fatal("reliable channel is partially reliable")
	}
	timed := ChannelOpen{Type: ChannelPartialTimed, Reliability: 150}.SendOptions()
	if !timed.Partial || timed.Lifetime != 150*time.Millisecond || timed.Unordered {
		t.Fatalf("timed options %+v", timed)
	}
}

func FuzzHandlePacket(f *testing.F) {
	f.Add([]byte{0x13, 0x88, 0x13, 0x88, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 20})
	f.Fuzz(func(t *testing.T, b []byte) {
		a := New(Config{}, func([]byte) {})
		now := time.Unix(1, 0)
		_ = a.HandlePacket(b, now)
		c := New(Config{}, func([]byte) {})
		c.Connect(now)
		_ = c.HandlePacket(b, now)
	})
}
