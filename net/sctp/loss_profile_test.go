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
// ms and a large one each second, on one ordered stream as the reliable
// channel of NetherNet carries them, over round trips of 20 and 100 ms on
// the virtual clock of the test network. It reports rather than checks,
// and runs only with SCTP_LOSS_PROFILE set.
func TestLossProfile(t *testing.T) {
	if os.Getenv("SCTP_LOSS_PROFILE") == "" {
		t.Skip("set SCTP_LOSS_PROFILE=1 to measure")
	}
	const (
		step     = 10 * time.Millisecond
		tick     = 50 * time.Millisecond
		duration = 60 * time.Second
		small    = 200
		large    = 64 << 10
	)
	t.Logf("%5s %5s %8s %8s %8s %8s %8s %6s %6s", "rtt", "loss", "messages", "p50", "p99", "p99.9", "max", ">1s", "full")
	for _, rtt := range []time.Duration{20 * time.Millisecond, 100 * time.Millisecond} {
		for _, loss := range []float64{0, 0.01, 0.05, 0.10} {
			lossRow(t, rtt, loss, step, tick, duration, small, large)
		}
	}
}

// lossRow measures and reports one round trip and loss rate over a few
// seeds.
func lossRow(t *testing.T, rtt time.Duration, loss float64, step, tick, duration time.Duration, small, large int) {
	var lat []time.Duration
	full := 0
	for seed := range uint64(5) {
		l, f := lossRun(t, seed, rtt/2, loss, step, tick, duration, small, large)
		lat, full = append(lat, l...), full+f
	}
	slices.Sort(lat)
	q := func(p float64) time.Duration { return lat[min(len(lat)-1, int(p*float64(len(lat))))] }
	stalls := 0
	for _, d := range lat {
		if d > time.Second {
			stalls++
		}
	}
	t.Logf("%5v %4.0f%% %8d %8v %8v %8v %8v %6d %6d", rtt, loss*100, len(lat), q(0.5), q(0.99), q(0.999), lat[len(lat)-1], stalls, full)
}

// lossRun sends the traffic of one seed over a path that takes delay one
// way and returns how late each message arrived, and how many the full
// send buffer refused.
func lossRun(t *testing.T, seed uint64, delay time.Duration, loss float64, step, tick, duration time.Duration, small, large int) (lat []time.Duration, full int) {
	n := newNetwork(t, seed, Config{})
	n.open(t, false)
	n.loss = loss
	a, b := n.peers[0].assoc, n.peers[1].assoc
	sent := map[uint32]time.Time{}
	b.cfg.OnMessage = func(_ uint16, _ uint32, data []byte) {
		seq := binary.BigEndian.Uint32(data)
		lat = append(lat, n.now.Sub(sent[seq]))
	}
	seq := uint32(0)
	send := func(size int) {
		msg := payload(int(seq), size)
		binary.BigEndian.PutUint32(msg, seq)
		if err := a.Send(1, PPIDBinary, msg, SendOptions{}, n.now); err != nil {
			full++
			return
		}
		sent[seq] = n.now
		seq++
	}
	// inFlight holds the packets on the path, by when they arrive.
	type packet struct {
		at time.Time
		to int
		b  []byte
	}
	var inFlight []packet
	for elapsed := time.Duration(0); elapsed < duration || len(lat) < len(sent); elapsed += step {
		if elapsed > duration+time.Minute {
			t.Fatal(fmt.Sprintf("%d of %d messages arrived a minute after the last was sent", len(lat), len(sent)))
		}
		if elapsed < duration && elapsed%tick == 0 {
			send(small)
			if elapsed%time.Second == 0 {
				send(large)
			}
		}
		for i, p := range n.peers {
			for _, b := range p.inbox {
				inFlight = append(inFlight, packet{n.now.Add(delay), i, b})
			}
			p.inbox = nil
		}
		n.now = n.now.Add(step)
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
	}
	return lat, full
}
