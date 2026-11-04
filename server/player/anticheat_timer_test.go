package player

import (
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/player/anticheat"
)

// timerTrace walks the stone lane with moves arriving every interval: a
// client that runs its ticks that much faster.
func timerTrace(name string, interval time.Duration, moves int, conf func(c *anticheat.Config)) acTrace {
	return acTrace{name: name, start: laneStart(laneStone), ticks: moves, conf: conf, input: forward(east, false, false),
		arrival: func(i int) time.Duration { return time.Duration(i+1) * interval }}
}

// TestTimerDetects checks that a client running its ticks 1.25 and 2 times
// faster for 10 s violates Timer and is throttled: its moves are deferred,
// none applied ahead of time. With the action setback, it is set back.
func TestTimerDetects(t *testing.T) {
	w := traceWorld(t)
	for _, tc := range []struct {
		name     string
		interval time.Duration
	}{{"×1.25", 40 * time.Millisecond}, {"×2", 25 * time.Millisecond}} {
		res := runTrace(t, w, timerTrace(tc.name, tc.interval, int(10*time.Second/tc.interval), nil))
		if n, _ := res.count(anticheat.Timer); n == 0 || res.deferred == 0 {
			t.Errorf("%s: %d violations of Timer, %d deferrals; want both", tc.name, n, res.deferred)
		}
		setback := func(c *anticheat.Config) {
			p := c.Policy(anticheat.Timer)
			p.Action = anticheat.Setback
			c.SetPolicy(anticheat.Timer, p)
		}
		if res = runTrace(t, w, timerTrace(tc.name, tc.interval, int(10*time.Second/tc.interval), setback)); res.corrections == 0 || res.deferred != 0 {
			t.Errorf("%s with setback: %d corrections, %d deferrals; want set back, none deferred", tc.name, res.corrections, res.deferred)
		}
	}
}

// TestTimerAfterAStall checks that after a stall of 3 s, longer than the
// balance and the tolerance, the last moves of its burst wait a few ticks
// and are applied: no violation, no setback.
func TestTimerAfterAStall(t *testing.T) {
	w := traceWorld(t)
	tr := timerTrace("stall", anticheatTick, 200, nil)
	tr.arrival = func(i int) time.Duration {
		if i >= 60 && i < 120 {
			return 120 * anticheatTick
		}
		return time.Duration(i+1) * anticheatTick
	}
	res := runTrace(t, w, tr)
	if len(res.violations) != 0 || res.corrections != 0 || res.deferred == 0 {
		t.Fatalf("%+v, %d corrections, %d deferrals; want none, none, some", res.violations, res.corrections, res.deferred)
	}
}

// TestTimerOff checks that a disabled Timer lets a client run ahead.
func TestTimerOff(t *testing.T) {
	w := traceWorld(t)
	res := runTrace(t, w, timerTrace("off", 25*time.Millisecond, 400, func(c *anticheat.Config) {
		p := c.Policy(anticheat.Timer)
		p.Enabled = false
		c.SetPolicy(anticheat.Timer, p)
	}))
	if n, _ := res.count(anticheat.Timer); n != 0 || res.deferred != 0 {
		t.Fatalf("Timer off: %d violations, %d deferrals", n, res.deferred)
	}
}
