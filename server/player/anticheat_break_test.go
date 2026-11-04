package player

import (
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/client/clienttest"
	"github.com/df-mc/dragonfly/server/player/anticheat"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// withBreaker runs f with a checked player standing on stone, its client
// handling input, a wall clock that moves 50 ms a tick, and a function that
// ticks it.
func withBreaker(t *testing.T, conf *anticheat.Config, f func(tx *world.Tx, p *Player, r *clienttest.Recorder, h *violationRecorder, tick func(n int))) {
	w, _ := clientTestWorld(t)
	r := clienttest.New()
	r.Answers.ChunkRadius = 8
	withCheckedPlayer(t, w, r, conf, mgl64.Vec3{0.5, 100, 0.5}, func(tx *world.Tx, p *Player) {
		tx.SetBlock(cube.Pos{0, 99, 0}, block.Stone{}, nil)
		wall := time.Unix(2_000_000, 0)
		p.anticheat().now = func() time.Time { return wall }
		tick := func(n int) {
			for range n {
				wall = wall.Add(anticheatTick)
				p.Tick(tx, p.tickCount)
			}
		}
		tick(1)
		h := &violationRecorder{}
		p.Handle(h)
		r.SetHandlingInput(true)
		f(tx, p, r, h, tick)
	})
}

// breakAfter starts breaking the block b at pos, ticks n times and
// finishes, and reports whether the block broke.
func breakAfter(tx *world.Tx, p *Player, tick func(int), pos cube.Pos, b world.Block, n int) bool {
	tx.SetBlock(pos, b, nil)
	p.StartBreaking(pos, cube.FaceNorth)
	tick(n)
	p.FinishBreaking()
	_, air := tx.Block(pos).(block.Air)
	return air
}

// TestBreakTimeRefusesEarlyFinishes checks that a finish earlier than 70%
// of the time a block takes is refused and the block resent, and one later
// breaks it: obsidian after 2 ticks, dirt by hand (15 ticks) after 5 and
// after 11.
func TestBreakTimeRefusesEarlyFinishes(t *testing.T) {
	withBreaker(t, anticheat.Default(), func(tx *world.Tx, p *Player, r *clienttest.Recorder, h *violationRecorder, tick func(int)) {
		pos := cube.Pos{0, 100, 2}
		if breakAfter(tx, p, tick, pos, block.Obsidian{}, 2) {
			t.Fatal("obsidian broke after 2 ticks")
		}
		if h.count(anticheat.BreakTime) != 1 || len(r.CallsTo("ViewBlockUpdate")) == 0 {
			t.Fatalf("violations %+v, resends %d", h.violations, len(r.CallsTo("ViewBlockUpdate")))
		}
		if breakAfter(tx, p, tick, pos, block.Dirt{}, 5) {
			t.Fatal("dirt broke by hand after 5 of its 15 ticks")
		}
		if !breakAfter(tx, p, tick, pos, block.Dirt{}, 11) {
			t.Fatal("dirt did not break by hand after 11 of its 15 ticks")
		}
	})
}

// TestBreakTimeHoldsOnALaggingServer checks that a break the wall clock
// served is accepted though the server ticked fewer times.
func TestBreakTimeHoldsOnALaggingServer(t *testing.T) {
	w, _ := clientTestWorld(t)
	r := clienttest.New()
	withCheckedPlayer(t, w, r, anticheat.Default(), mgl64.Vec3{0.5, 100, 0.5}, func(tx *world.Tx, p *Player) {
		tx.SetBlock(cube.Pos{0, 99, 0}, block.Stone{}, nil)
		wall := time.Unix(2_000_000, 0)
		p.anticheat().now = func() time.Time { return wall }
		p.Tick(tx, 1)
		r.SetHandlingInput(true)
		pos := cube.Pos{0, 100, 2}
		tx.SetBlock(pos, block.Dirt{}, nil)
		p.StartBreaking(pos, cube.FaceNorth)
		// 15 ticks of the client, three of a server lagging fivefold.
		for range 3 {
			wall = wall.Add(5 * anticheatTick)
			p.Tick(tx, p.tickCount)
		}
		p.FinishBreaking()
		if _, air := tx.Block(pos).(block.Air); !air {
			t.Fatal("a break the wall clock served was refused")
		}
	})
}

// TestBreakTimeDestroyBlock checks the instant breaks a client asks for: in
// survival only of blocks that break in a tick, at most 20 a second with a
// burst of 5; in creative of any block.
func TestBreakTimeDestroyBlock(t *testing.T) {
	withBreaker(t, anticheat.Default(), func(tx *world.Tx, p *Player, r *clienttest.Recorder, h *violationRecorder, tick func(int)) {
		pos := cube.Pos{0, 100, 2}
		tx.SetBlock(pos, block.Stone{}, nil)
		p.DestroyBlock(pos)
		if _, stone := tx.Block(pos).(block.Stone); !stone || h.count(anticheat.BreakTime) != 1 {
			t.Fatalf("stone destroyed at once in survival: %T, violations %+v", tx.Block(pos), h.violations)
		}
		broken := 0
		for i := range 8 {
			at := cube.Pos{i - 4, 100, 3}
			tx.SetBlock(at, block.ShortGrass{}, nil)
			p.DestroyBlock(at)
			if _, air := tx.Block(at).(block.Air); air {
				broken++
			}
		}
		if broken != 5 {
			t.Fatalf("broke %d of 8 grass at once, want the burst of 5", broken)
		}
		p.SetGameMode(world.GameModeCreative)
		tx.SetBlock(pos, block.Obsidian{}, nil)
		p.DestroyBlock(pos)
		if _, air := tx.Block(pos).(block.Air); !air {
			t.Fatal("obsidian not destroyed at once in creative")
		}
	})
}

// TestBreakTimeDelay checks Java's delayed destroy: an early finish breaks
// the block once its time is served.
func TestBreakTimeDelay(t *testing.T) {
	conf := anticheat.Default()
	conf.BreakTime.Delay = true
	withBreaker(t, conf, func(tx *world.Tx, p *Player, r *clienttest.Recorder, h *violationRecorder, tick func(int)) {
		pos := cube.Pos{0, 100, 2}
		if breakAfter(tx, p, tick, pos, block.Dirt{}, 5) {
			t.Fatal("dirt broke at once after 5 ticks")
		}
		tick(12)
		if _, air := tx.Block(pos).(block.Air); !air {
			t.Fatal("dirt did not break once its time was served")
		}
	})
}

// TestBreakTimeOff checks that a disabled BreakTime breaks at once, as
// before.
func TestBreakTimeOff(t *testing.T) {
	off := withPolicy(anticheat.BreakTime, func(p *anticheat.Policy) { p.Enabled = false })
	withBreaker(t, off, func(tx *world.Tx, p *Player, r *clienttest.Recorder, h *violationRecorder, tick func(int)) {
		if !breakAfter(tx, p, tick, cube.Pos{0, 100, 2}, block.Obsidian{}, 0) {
			t.Fatal("obsidian did not break at once with BreakTime off")
		}
	})
}
