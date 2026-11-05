package player

import (
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/client/clienttest"
	"github.com/df-mc/dragonfly/server/player/anticheat"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// The tests of Timer on a server that lags: its ticks come late, so that
// the inputs of the clients pile up, and a client's own stall ends in a
// burst. The timer keeps a balance over time, never a count a tick.

// TestTimerOnALaggingServer checks that a server ticking every 200 ms for
// 10 s, handling the four inputs a client sends in that time a tick, makes
// the timer neither defer nor log anything, however the inputs arrive: at
// their own time, or all at the time of the tick that handles them.
func TestTimerOnALaggingServer(t *testing.T) {
	w := traceWorld(t)
	for _, tc := range []struct {
		name    string
		arrival func(i int) time.Duration
	}{
		{"arriving at their own time", nil},
		{"arriving with the tick", func(i int) time.Duration { return time.Duration(i/4+1) * 4 * anticheatTick }},
	} {
		tr := timerTrace(tc.name, anticheatTick, 200, nil)
		tr.perTick = 4
		if tc.arrival != nil {
			tr.arrival = tc.arrival
		}
		res := runTrace(t, w, tr)
		if len(res.violations) != 0 || res.corrections != 0 || res.deferred != 0 || res.refused != 0 {
			t.Errorf("%s: %+v, %d corrections, %d deferrals, %d refused; want none", tc.name, res.violations, res.corrections, res.deferred, res.refused)
		}
	}
}

// TestTimerAfterAStallOfTwoSeconds checks that a client that stalls for 2 s
// and sends the 40 inputs it held back at once is not throttled: the
// balance of MaxBalance ticks is made for it. Nor is it on a server that
// lags.
func TestTimerAfterAStallOfTwoSeconds(t *testing.T) {
	w := traceWorld(t)
	for _, perTick := range []int{1, 4} {
		tr := timerTrace("stall", anticheatTick, 200, nil)
		tr.perTick = perTick
		tr.arrival = func(i int) time.Duration {
			if i >= 60 && i < 100 {
				// The stall: the inputs of ticks 60 to 99 arrive together.
				return 100 * anticheatTick
			}
			return time.Duration(i+1) * anticheatTick
		}
		res := runTrace(t, w, tr)
		if len(res.violations) != 0 || res.corrections != 0 || res.deferred != 0 {
			t.Errorf("%d moves a tick: %+v, %d corrections, %d deferrals; want none", perTick, res.violations, res.corrections, res.deferred)
		}
	}
}

// TestTimerStillCatchesAFastClientOnALaggingServer checks that a client that
// runs 1.25 and 2 times as fast is caught with a server ticking every 200 ms
// as it is with one ticking every 50.
func TestTimerStillCatchesAFastClientOnALaggingServer(t *testing.T) {
	w := traceWorld(t)
	for _, tc := range []struct {
		name     string
		interval time.Duration
	}{{"×1.25", 40 * time.Millisecond}, {"×2", 25 * time.Millisecond}} {
		tr := timerTrace(tc.name, tc.interval, int(20*time.Second/tc.interval), nil)
		tr.perTick = 4
		res := runTrace(t, w, tr)
		if n, _ := res.count(anticheat.Timer); n == 0 || res.deferred == 0 {
			t.Errorf("%s: %d violations of Timer, %d deferrals; want both", tc.name, n, res.deferred)
		}
	}
}

// TestTimerOnAFreezingServer checks that a server that ticks every 3 s, and
// so hands the 60 inputs of a client at once, does not make the timer defer
// or log them, however late their arrival is stamped: the server lost the
// ticks, not the client. A client that runs twice as fast on such a server
// is still caught.
func TestTimerOnAFreezingServer(t *testing.T) {
	w := traceWorld(t)
	tr := timerTrace("frozen", anticheatTick, 240, nil)
	tr.perTick = 60
	tr.arrival = func(i int) time.Duration { return time.Duration(i/60+1) * 60 * anticheatTick }
	res := runTrace(t, w, tr)
	if len(res.violations) != 0 || res.corrections != 0 || res.deferred != 0 || res.refused != 0 {
		t.Errorf("%+v, %d corrections, %d deferrals, %d refused; want none", res.violations, res.corrections, res.deferred, res.refused)
	}
	fast := timerTrace("frozen, ×2", 25*time.Millisecond, 1600, nil)
	fast.perTick = 60
	res = runTrace(t, w, fast)
	if n, _ := res.count(anticheat.Timer); n == 0 || res.deferred == 0 {
		t.Errorf("a client twice as fast: %d violations of Timer, %d deferrals; want both", n, res.deferred)
	}
}

// breakOnALaggingServer has a client start breaking the block b at pos and
// finish after the client time passed, as a server whose ticks are a second
// apart handles it: both inputs wait in the inbox, and are handled together
// in the tick that follows, though they arrived that far apart. It reports
// whether the block broke.
func breakOnALaggingServer(t *testing.T, b world.Block, took time.Duration) (broke bool, h *violationRecorder) {
	w, _ := clientTestWorld(t)
	r := clienttest.New()
	r.Answers.ChunkRadius = 8
	h = &violationRecorder{}
	withCheckedPlayer(t, w, r, anticheat.Default(), mgl64.Vec3{0.5, 100, 0.5}, func(tx *world.Tx, p *Player) {
		tx.SetBlock(cube.Pos{0, 99, 0}, block.Stone{}, nil)
		wall := time.Unix(2_000_000, 0)
		p.anticheat().now = func() time.Time { return wall }
		p.Handle(h)
		p.Tick(tx, 1)
		r.SetHandlingInput(true)
		pos := cube.Pos{0, 100, 2}
		tx.SetBlock(pos, b, nil)
		// The inputs arrived within the second the server lagged, 100 ms
		// and (100 ms + took) after the last tick.
		begin := wall
		input := func(at time.Duration, tick uint64) {
			p.ClientMove(client.Move{Position: p.Position(), Rotation: p.Rotation(), Arrived: begin.Add(at), Tick: tick})
		}
		wall = wall.Add(time.Second)
		input(100*time.Millisecond, 1)
		p.StartBreaking(pos, cube.FaceNorth)
		input(100*time.Millisecond+took, 1+uint64(took/anticheatTick))
		p.FinishBreaking()
		_, broke = tx.Block(pos).(block.Air)
	})
	return broke, h
}

// TestBreakTimeCountsTheTimeBetweenTheInputs checks that a break is timed by
// when its inputs reached the server, not by when the server handled them:
// dirt by hand (15 ticks) that the client broke in 0.8 s, handled in one
// tick of a server that lags a second, is not cancelled. A break in 0.1 s,
// handled the same, is still refused.
func TestBreakTimeCountsTheTimeBetweenTheInputs(t *testing.T) {
	if broke, h := breakOnALaggingServer(t, block.Dirt{}, 800*time.Millisecond); !broke || h.count(anticheat.BreakTime) != 0 {
		t.Errorf("dirt broken in 0.8 s: broke %v, violations %+v", broke, h.violations)
	}
	if broke, h := breakOnALaggingServer(t, block.Dirt{}, 100*time.Millisecond); broke || h.count(anticheat.BreakTime) != 1 {
		t.Errorf("dirt broken in 0.1 s: broke %v, violations %+v", broke, h.violations)
	}
	if broke, h := breakOnALaggingServer(t, block.Obsidian{}, 100*time.Millisecond); broke || h.count(anticheat.BreakTime) != 1 {
		t.Errorf("obsidian broken in 0.1 s: broke %v, violations %+v", broke, h.violations)
	}
}

// TestAttackRateOnALaggingServer checks that the rate of attacks is the one
// the inputs arrived at: 15 attacks in a second, handled in one tick of a
// server that lags a second, are the 15 a second the client made, within the
// 20 allowed; 40 attacks in a second are still a violation.
func TestAttackRateOnALaggingServer(t *testing.T) {
	for _, tc := range []struct {
		attacks  int
		interval time.Duration
		want     bool
	}{{15, 66 * time.Millisecond, false}, {40, 25 * time.Millisecond, true}} {
		withAttacker(t, anticheat.Default(), 2.5, func(tx *world.Tx, p *Player, cow world.Entity, h *violationRecorder) {
			begin := time.Unix(4_000_000, 0)
			wall := begin
			p.anticheat().now = func() time.Time { return wall }
			p.data.Rot = cube.Rotation{-90, 0}
			p.Tick(tx, p.tickCount)
			// The server lagged a second: its tick handles them all.
			wall = wall.Add(time.Second)
			p.Tick(tx, p.tickCount)
			for i := range tc.attacks {
				p.ClientMove(client.Move{Position: p.Position(), Rotation: p.Rotation(), Arrived: begin.Add(time.Duration(i+1) * tc.interval)})
				p.AttackEntity(cow)
			}
			if got := h.kinds(anticheat.Attack)["rate"] > 0; got != tc.want {
				t.Errorf("%d attacks every %v: violations %+v", tc.attacks, tc.interval, h.violations)
			}
		})
	}
}
