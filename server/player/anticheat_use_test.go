package player

import (
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/client/clienttest"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player/anticheat"
	"github.com/df-mc/dragonfly/server/world"
)

// throwSnowballs has the client of p throw n snowballs, one every interval,
// and returns how many it threw.
func throwSnowballs(tx *world.Tx, p *Player, n int, interval time.Duration) int {
	wall := time.Unix(5_000_000, 0)
	p.anticheat().now = func() time.Time { return wall }
	thrown := 0
	for range n {
		p.SetHeldItems(item.NewStack(item.Snowball{}, 16), item.Stack{})
		p.UseItem()
		if held, _ := p.HeldItems(); held.Count() == 15 {
			thrown++
		}
		wall = wall.Add(interval)
		p.Tick(tx, p.tickCount)
	}
	return thrown
}

// TestFastUseDetects checks that a snowball every tick is a violation of
// FastUse, logged by default and refused with cancel, and one every four
// ticks, Java's right-click delay, is not.
func TestFastUseDetects(t *testing.T) {
	for _, tc := range []struct {
		name     string
		interval time.Duration
		cancel   bool
		thrown   int
		violates bool
	}{
		{"every tick", anticheatTick, false, 20, true},
		// The burst of 2 and one every four ticks after.
		{"every tick, cancel", anticheatTick, true, 6, true},
		{"every four ticks", 4 * anticheatTick, false, 20, false},
	} {
		conf := anticheat.Default()
		if tc.cancel {
			conf = withPolicy(anticheat.FastUse, func(p *anticheat.Policy) { p.Action = anticheat.Cancel })
		}
		withBreaker(t, conf, func(tx *world.Tx, p *Player, r *clienttest.Recorder, h *violationRecorder, _ func(int)) {
			thrown := throwSnowballs(tx, p, 20, tc.interval)
			if got := h.count(anticheat.FastUse) > 0; got != tc.violates || thrown != tc.thrown {
				t.Errorf("%s: threw %d, violations %+v; want %d thrown, violated %v", tc.name, thrown, h.violations, tc.thrown, tc.violates)
			}
		})
	}
}

// TestFastUseOff checks that a disabled FastUse lets a client throw every
// tick.
func TestFastUseOff(t *testing.T) {
	off := withPolicy(anticheat.FastUse, func(p *anticheat.Policy) { p.Enabled = false })
	withBreaker(t, off, func(tx *world.Tx, p *Player, r *clienttest.Recorder, h *violationRecorder, _ func(int)) {
		if thrown := throwSnowballs(tx, p, 20, anticheatTick); thrown != 20 || len(h.violations) != 0 {
			t.Fatalf("threw %d, violations %+v", thrown, h.violations)
		}
	})
}
