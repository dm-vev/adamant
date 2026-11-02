package nethernet

import (
	"net/netip"
	"testing"
	"time"

	"github.com/dm-vev/adamant/net/sctp"
)

// TestLossyPeerKeptWithChunksHeld sends game traffic to a peer over a path
// of 100 ms that loses 10% of the datagrams: a small message each tick of
// 50 ms and 64 KiB of chunks a second in pieces of 8 KiB, more than the
// path carries. Sent as they come, the chunks pile up in the send buffer
// toward the bound that cuts a slow peer. Held while the bytes queued
// behind the flight reach a congestion window, or what the path carries in
// a tick if more, as a server holds them, the buffer stays short and the
// peer is kept. A peer that then takes nothing is still cut.
func TestLossyPeerKeptWithChunksHeld(t *testing.T) {
	plain := newLossyPath(t, 3, false)
	plain.run(t, time.Minute)
	if plain.maxBuffered < 1<<20 {
		t.Fatalf("chunks sent as they come buffered at most %d bytes", plain.maxBuffered)
	}
	held := newLossyPath(t, 3, true)
	held.run(t, time.Minute)
	if held.maxBuffered > 256<<10 || held.backlog == 0 {
		t.Fatalf("chunks held buffered up to %d bytes with %d pieces held", held.maxBuffered, held.backlog)
	}
	held.n.loss = 1
	held.run(t, 5*time.Minute)
	if held.c.Err() == nil {
		t.Fatal("a peer that takes nothing is not cut")
	}
}

// lossyPath carries the game traffic of a server to a peer over a path of
// 100 ms that loses 10% of the datagrams.
type lossyPath struct {
	n    *vnet
	c    *Conn
	hold bool
	// backlog counts the pieces of chunks held, maxBuffered the most the
	// connection buffered.
	backlog, maxBuffered int
	elapsed              time.Duration
	inFlight             []delayedDatagram
}

// delayedDatagram is a datagram on the path, to arrive at at an end.
type delayedDatagram struct {
	at   time.Time
	to   *end
	b    []byte
	from netip.AddrPort
}

func newLossyPath(t *testing.T, seed uint64, hold bool) *lossyPath {
	n := newVnet(t, seed, true, nil)
	n.open(t)
	n.loss = 0.1
	return &lossyPath{n: n, c: n.ends[server].c, hold: hold}
}

const (
	lossyStep  = 10 * time.Millisecond
	lossyTick  = 50 * time.Millisecond
	lossyDelay = 50 * time.Millisecond
)

// run sends the traffic of the server for d, until its connection fails.
func (p *lossyPath) run(t *testing.T, d time.Duration) {
	for end := p.elapsed + d; p.elapsed < end && p.c.Err() == nil; p.elapsed += lossyStep {
		if p.elapsed%lossyTick == 0 {
			p.tick(t)
		}
		p.maxBuffered = max(p.maxBuffered, p.c.Buffered())
		p.carry()
	}
}

// tick sends a small message, takes the chunks of a second each second and
// sends those the queue has room for.
func (p *lossyPath) tick(t *testing.T) {
	p.write(t, 200)
	if p.elapsed%time.Second == 0 {
		p.backlog += 8
	}
	for p.backlog > 0 && (!p.hold || chunkRoom(p.c.Stats().SCTP)) {
		p.backlog--
		p.write(t, 8<<10)
	}
}

// chunkRoom reports whether the bytes queued behind the flight are under a
// congestion window, or what the path carries in a tick if more.
func chunkRoom(st sctp.Stats) bool {
	bound := st.Cwnd
	if st.RTT > 0 {
		bound = max(bound, int(int64(st.Cwnd)*int64(lossyTick)/int64(st.RTT)))
	}
	return st.Buffered-st.Flight < bound
}

// write sends a message of size bytes, as a session flushes a batch.
func (p *lossyPath) write(t *testing.T, size int) {
	if _, err := p.c.Write(message(int(p.elapsed), size)); err != nil && p.n.loss < 1 {
		t.Fatalf("write after %v: %v", p.elapsed, err)
	}
}

// carry puts the datagrams sent on the path, moves the clock a step on and
// hands the datagrams due to their ends.
func (p *lossyPath) carry() {
	n := p.n
	for _, e := range n.ends {
		for _, d := range e.inbox {
			p.inFlight = append(p.inFlight, delayedDatagram{n.now.Add(lossyDelay), e, d.b, d.from})
		}
		e.inbox = nil
	}
	n.now = n.now.Add(lossyStep)
	arrived := p.inFlight
	p.inFlight = nil
	for _, d := range arrived {
		if d.at.After(n.now) {
			p.inFlight = append(p.inFlight, d)
			continue
		}
		d.to.c.handleDatagram(d.b, d.from, n.now)
	}
	n.fire()
}
