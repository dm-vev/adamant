package anticheat

import (
	"math"
	"testing"
	"time"
)

func TestLevelDecays(t *testing.T) {
	var l Level
	if got := l.Add(0, 1, 1); got != 1 {
		t.Fatalf("first violation: %v", got)
	}
	if got := l.Add(0, 1, 1); got != 2 {
		t.Fatalf("second violation in the same tick: %v", got)
	}
	// A second later, one has decayed.
	if got := l.At(20, 1); math.Abs(got-1) > 1e-9 {
		t.Fatalf("a second later: %v, want 1", got)
	}
	if got := l.Add(30, 1, 1); math.Abs(got-1.5) > 1e-9 {
		t.Fatalf("1.5 seconds later and a violation: %v, want 1.5", got)
	}
	if got := l.At(1000, 1); got != 0 {
		t.Fatalf("long after: %v, want 0", got)
	}
	// Reading the past does not decay.
	if got := l.At(10, 1); math.Abs(got-1.5) > 1e-9 {
		t.Fatalf("read in the past: %v", got)
	}
	l.Reset()
	if got := l.At(30, 1); got != 0 {
		t.Fatalf("after Reset: %v", got)
	}
	// Three violations a second apart with a decay of 1 never pass 1.
	l.Reset()
	for i := range 3 {
		if got := l.Add(int64(i*20), 1, 1); got > 1+1e-9 {
			t.Fatalf("violation %d a second apart: %v", i, got)
		}
	}
}

func TestBucket(t *testing.T) {
	base := time.Unix(1000, 0)
	var b Bucket
	for i := range 10 {
		if !b.Take(base, 20, 10) {
			t.Fatalf("token %d of a burst of 10 refused", i)
		}
	}
	if b.Take(base, 20, 10) {
		t.Fatal("an eleventh token at once")
	}
	// 20 a second: one every 50 ms.
	if !b.Take(base.Add(50*time.Millisecond), 20, 10) {
		t.Fatal("no token 50 ms later")
	}
	if b.Take(base.Add(60*time.Millisecond), 20, 10) {
		t.Fatal("a token 10 ms after the last")
	}
	if got := b.Tokens(base.Add(time.Hour), 20, 10); got != 10 {
		t.Fatalf("an hour later: %v tokens, want the burst of 10", got)
	}
	// A steady rate of 20 a second never runs out.
	var steady Bucket
	for i := range 200 {
		if !steady.Take(base.Add(time.Duration(i)*50*time.Millisecond), 20, 10) {
			t.Fatalf("steady 20 a second refused at %d", i)
		}
	}
	// 40 a second runs out after the burst and half as many again.
	var fast Bucket
	refused := 0
	for i := range 40 {
		if !fast.Take(base.Add(time.Duration(i)*25*time.Millisecond), 20, 10) {
			refused++
		}
	}
	if refused < 10 || refused > 11 {
		t.Fatalf("40 in a second: %d refused, want about 10", refused)
	}
}

// clockRun feeds a Clock moves arriving at the times passed, as the player
// ticks once every 50 ms from the start, and returns how many were ahead
// of time by more than tolerance. Ahead moves are not counted, as Throttle
// holds them back.
func clockRun(arrivals []time.Duration, tolerance, maxBalance float64, tickEvery time.Duration) (ahead int, maxAhead float64) {
	start := time.Unix(5000, 0)
	var c Clock
	c.Reset(start, 0)
	for _, a := range arrivals {
		tick := int64(a / tickEvery)
		now := c.Now(start.Add(a), tick)
		d := c.Ahead(now, maxBalance)
		maxAhead = max(maxAhead, d)
		if d > tolerance {
			ahead++
			continue
		}
		c.Count(now, maxBalance)
	}
	return ahead, maxAhead
}

func steadyArrivals(n int, every time.Duration) []time.Duration {
	a := make([]time.Duration, n)
	for i := range a {
		a[i] = time.Duration(i+1) * every
	}
	return a
}

func TestClock(t *testing.T) {
	const tolerance, maxBalance = 10, 40
	for _, tc := range []struct {
		name     string
		arrivals []time.Duration
		ahead    bool
		// tickEvery is how often the player ticks: 50 ms, or slower on a
		// lagging server.
		tickEvery time.Duration
	}{
		{"steady 20 a second", steadyArrivals(400, 50*time.Millisecond), false, 0},
		{"jitter of 200 ms", func() []time.Duration {
			a := steadyArrivals(400, 50*time.Millisecond)
			for i := range a {
				switch i % 4 {
				case 1:
					a[i] += 200 * time.Millisecond
				case 3:
					a[i] -= 40 * time.Millisecond
				}
			}
			return a
		}(), false, 0},
		{"bursts of 5 after 250 ms", func() []time.Duration {
			var a []time.Duration
			for burst := range 80 {
				for range 5 {
					a = append(a, time.Duration(burst+1)*250*time.Millisecond)
				}
			}
			return a
		}(), false, 0},
		{"a stall of 2 s and its burst", func() []time.Duration {
			a := steadyArrivals(100, 50*time.Millisecond)
			for range 40 {
				a = append(a, 7*time.Second)
			}
			for _, d := range steadyArrivals(100, 50*time.Millisecond) {
				a = append(a, 7*time.Second+d)
			}
			return a
		}(), false, 0},
		// The moves arrive 50 ms apart, and the server ticks every 150 ms,
		// handling them in threes: the ticks lag, the arrivals do not.
		{"server lag: 3 inputs a tick", steadyArrivals(300, 50*time.Millisecond), false, 150 * time.Millisecond},
		{"timer ×1.1", steadyArrivals(400, time.Second/22), true, 0},
		{"timer ×1.25", steadyArrivals(400, time.Second/25), true, 0},
		{"timer ×2", steadyArrivals(400, time.Second/40), true, 0},
	} {
		if tc.tickEvery == 0 {
			tc.tickEvery = tickDuration
		}
		ahead, maxAhead := clockRun(tc.arrivals, tolerance, maxBalance, tc.tickEvery)
		if got := ahead > 0; got != tc.ahead {
			t.Errorf("%s: %d moves ahead of time (at most %.1f ticks), want ahead %v", tc.name, ahead, maxAhead, tc.ahead)
		}
	}
}

// TestClockAfterAStall checks that after a stall longer than the balance,
// the moves of its burst beyond the balance and the tolerance wait, and
// only those.
func TestClockAfterAStall(t *testing.T) {
	start := time.Unix(5000, 0)
	var c Clock
	c.Reset(start, 0)
	// 3 s of nothing, then 60 moves at once: 40 of balance and 10 of
	// tolerance go through, 10 wait.
	now := c.Now(start.Add(3*time.Second), 60)
	passed := 0
	for range 60 {
		if c.Ahead(now, 40) > 10 {
			break
		}
		c.Count(now, 40)
		passed++
	}
	if passed != 50 {
		t.Fatalf("%d moves of the burst passed, want 50", passed)
	}
	// Each later tick lets one more through.
	now = c.Now(start.Add(3*time.Second), 61)
	if c.Ahead(now, 40) > 10 {
		t.Fatal("a tick later, the next move still waits")
	}
}

func TestClockNow(t *testing.T) {
	start := time.Unix(5000, 0)
	var c Clock
	c.Reset(start, 100)
	if got := c.Now(start.Add(time.Second), 100); got != 20 {
		t.Errorf("a second of wall clock: now %v, want 20", got)
	}
	if got := c.Now(start, 140); got != 40 {
		t.Errorf("40 ticks of the player: now %v, want 40", got)
	}
	if got := c.Now(start.Add(-time.Second), 100); got != 0 {
		t.Errorf("an arrival before the reference: now %v, want 0", got)
	}
}
