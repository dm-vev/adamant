package sctp

import (
	"encoding/binary"
	"fmt"
	"os"
	"slices"
	"testing"
	"time"
)

// TestLossProfile measures how late the messages of game-like traffic
// arrive over a path that loses packets: a small message each tick of 50
// ms and 64 KiB of chunks each second, on one ordered stream as the
// reliable channel of NetherNet carries them, over round trips of 20 and
// 100 ms on the virtual clock of the test network. 512 KiB of chunks wait
// at the start, as around a player that joined. Chunks go in pieces of 8
// KiB, about what a column of sub-chunks takes compressed. A plain sender
// sends each message at once; an adaptive one defers the pieces while the
// bytes queued and not yet sent reach the bound of its gate, as a server
// defers chunks. Its gates bound that queue by congestion windows, with a
// floor or none; "tick" bounds it by a window, or by what the path carries
// in a tick if more, as a server checks the queue once a tick. It reports
// how late the small messages arrive, the bytes of chunks carried a
// second, when those of the start had all arrived and the KiB of chunks
// deferred at most and at the end. It reports rather than checks, and
// runs only with SCTP_LOSS_PROFILE set.
func TestLossProfile(t *testing.T) {
	if os.Getenv("SCTP_LOSS_PROFILE") == "" {
		t.Skip("set SCTP_LOSS_PROFILE=1 to measure")
	}
	traffic := lossTraffic{
		step:     10 * time.Millisecond,
		tick:     50 * time.Millisecond,
		duration: 60 * time.Second,
		small:    200,
		large:    64 << 10,
		piece:    8 << 10,
		spawn:    512 << 10,
	}
	gates := []*lossGate{
		nil,
		{windows: 0.5},
		{windows: 1},
		{windows: 2},
		{windows: 1, floor: 64 << 10},
		{windows: 1, tick: traffic.tick},
	}
	t.Logf("%5s %5s %9s %8s %8s %8s %8s %6s %7s %7s %9s %6s", "rtt", "loss", "gate", "p50", "p99", "p99.9", "max", ">1s", "KB/s", "loaded", "backlog", "full")
	for _, rtt := range []time.Duration{20 * time.Millisecond, 100 * time.Millisecond} {
		for _, loss := range []float64{0, 0.01, 0.05, 0.10} {
			for _, g := range gates {
				lossRow(t, rtt, loss, traffic, g)
			}
		}
	}
}

// lossTraffic is the traffic of a run: a small message each tick, large
// bytes of chunks each second and spawn bytes at the start, in pieces.
type lossTraffic struct {
	step, tick, duration       time.Duration
	small, large, piece, spawn int
}

// lossGate is the rule of an adaptive sender: a piece of chunks is sent
// while the bytes queued and not yet sent are under windows congestion
// windows, under floor, or, with tick set, under what the path carries in
// a tick at the window and round trip measured.
type lossGate struct {
	windows float64
	floor   int
	tick    time.Duration
}

// open reports whether the gate lets a piece go.
func (g *lossGate) open(st Stats) bool {
	bound := max(int(g.windows*float64(st.Cwnd)), g.floor)
	if g.tick > 0 && st.RTT > 0 {
		bound = max(bound, int(int64(st.Cwnd)*int64(g.tick)/int64(st.RTT)))
	}
	return st.Buffered-st.Flight < bound
}

func (g *lossGate) String() string {
	if g == nil {
		return "plain"
	}
	if g.tick > 0 {
		return "tick"
	}
	return fmt.Sprintf("%gw/%dK", g.windows, g.floor>>10)
}

// lossResult is what a run measured: how late the small messages arrived,
// the bytes of chunks that arrived in the run, when those of the start
// had all arrived, the pieces deferred at most and at the end, and the
// messages the full send buffer refused.
type lossResult struct {
	small               []time.Duration
	chunkBytes          int
	loaded              time.Duration
	maxBacklog, backlog int
	full                int
}

// lossRow measures and reports one round trip, loss rate and gate over a
// few seeds.
func lossRow(t *testing.T, rtt time.Duration, loss float64, traffic lossTraffic, g *lossGate) {
	const seeds = 20
	var sum lossResult
	for seed := range uint64(seeds) {
		r := lossRun(t, seed, rtt/2, loss, traffic, g)
		sum.small = append(sum.small, r.small...)
		sum.chunkBytes += r.chunkBytes
		sum.loaded = max(sum.loaded, r.loaded)
		sum.maxBacklog = max(sum.maxBacklog, r.maxBacklog)
		sum.backlog = max(sum.backlog, r.backlog)
		sum.full += r.full
	}
	lat := sum.small
	slices.Sort(lat)
	q := func(p float64) time.Duration { return lat[min(len(lat)-1, int(p*float64(len(lat))))] }
	stalls := 0
	for _, d := range lat {
		if d > time.Second {
			stalls++
		}
	}
	rate := float64(sum.chunkBytes) / seeds / traffic.duration.Seconds() / 1000
	kib := traffic.piece >> 10
	t.Logf("%5v %4.0f%% %9v %8v %8v %8v %8v %6d %7.1f %7v %4d/%-4d %6d", rtt, loss*100, g, q(0.5), q(0.99), q(0.999), lat[len(lat)-1], stalls, rate, sum.loaded, sum.maxBacklog*kib, sum.backlog*kib, sum.full)
}

// lossSender sends the traffic of a run on an association and keeps what
// arrives.
type lossSender struct {
	n       *network
	a       *Association
	traffic lossTraffic
	gate    *lossGate
	start   time.Time
	seq     uint32
	// sent holds when each message was sent, piece whether it was a piece
	// of chunks.
	sent  map[uint32]time.Time
	piece map[uint32]bool
	// backlog counts the pieces deferred, arrived the messages that
	// arrived and spawned the bytes of chunks among them.
	backlog, arrived, spawned int
	r                         lossResult
}

// lossRun sends the traffic of one seed over a path that takes delay one
// way and returns what it measured.
func lossRun(t *testing.T, seed uint64, delay time.Duration, loss float64, traffic lossTraffic, g *lossGate) lossResult {
	n := newNetwork(t, seed, Config{})
	n.open(t, false)
	n.loss = loss
	s := &lossSender{n: n, a: n.peers[0].assoc, traffic: traffic, gate: g, start: n.now, sent: map[uint32]time.Time{}, piece: map[uint32]bool{}}
	n.peers[1].assoc.cfg.OnMessage = s.receive
	// inFlight holds the packets on the path, by when they arrive.
	var inFlight []lossPacket
	for elapsed := time.Duration(0); elapsed < traffic.duration || s.arrived < len(s.sent); elapsed += traffic.step {
		if elapsed > traffic.duration+time.Minute {
			t.Fatalf("%d of %d messages arrived a minute after the last was sent", s.arrived, len(s.sent))
		}
		if elapsed < traffic.duration && elapsed%traffic.tick == 0 {
			s.tick(elapsed)
		}
		inFlight = s.carry(inFlight, delay)
	}
	s.r.backlog = s.backlog
	return s.r
}

// lossPacket is a packet on the path, to arrive at at peer to.
type lossPacket struct {
	at time.Time
	to int
	b  []byte
}

// carry puts the packets sent on the path, moves the clock a step on and
// hands the packets due to their peers.
func (s *lossSender) carry(inFlight []lossPacket, delay time.Duration) []lossPacket {
	n := s.n
	for i, p := range n.peers {
		for _, b := range p.inbox {
			inFlight = append(inFlight, lossPacket{n.now.Add(delay), i, b})
		}
		p.inbox = nil
	}
	n.now = n.now.Add(s.traffic.step)
	arrived := inFlight
	inFlight = nil
	for _, pkt := range arrived {
		if pkt.at.After(n.now) {
			inFlight = append(inFlight, pkt)
			continue
		}
		_ = n.peers[pkt.to].assoc.HandlePacket(pkt.b, n.now)
	}
	n.fireTimers()
	return inFlight
}

// tick sends what a tick sends: the chunks of the start, a small message,
// the chunks of a second each second, and the pieces the gate lets go.
func (s *lossSender) tick(elapsed time.Duration) {
	if elapsed == 0 {
		s.offer(s.traffic.spawn)
	}
	s.send(s.traffic.small)
	if elapsed%time.Second == 0 {
		s.offer(s.traffic.large)
	}
	for s.backlog > 0 && s.gate.open(s.a.Stats()) {
		s.backlog--
		s.send(s.traffic.piece)
	}
}

// offer sends bytes of chunks in pieces, or defers them with a gate.
func (s *lossSender) offer(bytes int) {
	pieces := bytes / s.traffic.piece
	if s.gate != nil {
		s.backlog += pieces
		s.r.maxBacklog = max(s.r.maxBacklog, s.backlog)
		return
	}
	for range pieces {
		s.send(s.traffic.piece)
	}
}

// send sends a message of size bytes, counting it as full if the send
// buffer refused it.
func (s *lossSender) send(size int) {
	msg := payload(int(s.seq), size)
	binary.BigEndian.PutUint32(msg, s.seq)
	if err := s.a.Send(1, PPIDBinary, msg, SendOptions{}, s.n.now); err != nil {
		s.r.full++
		return
	}
	s.sent[s.seq] = s.n.now
	s.piece[s.seq] = size == s.traffic.piece
	s.seq++
}

// receive records a message that arrived.
func (s *lossSender) receive(_ uint16, _ uint32, data []byte) {
	seq := binary.BigEndian.Uint32(data)
	s.arrived++
	if !s.piece[seq] {
		s.r.small = append(s.r.small, s.n.now.Sub(s.sent[seq]))
		return
	}
	if s.n.now.Sub(s.start) <= s.traffic.duration {
		s.r.chunkBytes += len(data)
	}
	if s.spawned += len(data); s.spawned == s.traffic.spawn {
		s.r.loaded = s.n.now.Sub(s.start)
	}
}
