package player

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/client/clienttest"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player/anticheat"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// hoverTrace rises 3 blocks off the stone lane in three moves and stays
// there.
func hoverTrace(name string, moves int) acTrace {
	start := laneStart(laneStone)
	return acTrace{name: name, start: start, positions: func(*world.Tx) []mgl64.Vec3 {
		ps := make([]mgl64.Vec3, moves)
		for i := range ps {
			ps[i] = start.Add(mgl64.Vec3{0, float64(min(i+1, 3)), 0})
		}
		return ps
	}}
}

// TestFlyDetectsHovering checks that hovering for 100 moves violates Fly
// after Java's 80, and sets the player back where it last stood.
func TestFlyDetectsHovering(t *testing.T) {
	w := traceWorld(t)
	res := runTrace(t, w, hoverTrace("hover", 100))
	fly := 0
	for _, v := range res.violations {
		if v.Check == anticheat.Fly {
			fly++
			if v.Kind != "hover" {
				t.Errorf("violation %+v", v)
			}
		}
	}
	// The first violation sets back; the moves after it await the
	// acknowledgement the trace never sends, so it violates once.
	if fly != 1 || res.corrections != 1 {
		t.Fatalf("%d violations of Fly, %d corrections; want one of each", fly, res.corrections)
	}
	// 80 moves floating pass: a rise of a block already floats, as the
	// box reaches 0.55 below the feet.
	for _, v := range runTrace(t, w, hoverTrace("hover", 80)).violations {
		if v.Check == anticheat.Fly {
			t.Fatalf("80 moves floating violated: %+v", v)
		}
	}
}

// TestFlySetsBackToTheSupport checks where the setback puts a hovering
// player: where it last had blocks around it, with the blocks below it
// resent first.
func TestFlySetsBackToTheSupport(t *testing.T) {
	w := traceWorld(t)
	r := clienttest.New()
	r.Answers.ChunkRadius = 8
	start := laneStart(laneStone)
	withCheckedPlayer(t, w, r, anticheat.Default(), start, func(tx *world.Tx, p *Player) {
		standThroughJoin(tx, p)
		for i := 0; len(r.CallsTo("CorrectMove")) == 0 && i < 120; i++ {
			p.ClientMove(clientMoveAt(start.Add(mgl64.Vec3{0, float64(min(i+1, 3)), 0})))
			p.Tick(tx, p.tickCount)
		}
		calls := r.CallsTo("CorrectMove")
		if len(calls) != 1 || !calls[0].Args[1].(mgl64.Vec3).ApproxEqual(start) {
			t.Fatalf("corrections %v, want one to %v", calls, start)
		}
		if !p.Position().ApproxEqual(start) {
			t.Fatalf("the player is at %v after the setback, want %v", p.Position(), start)
		}
		if len(r.CallsTo("ViewBlockUpdate")) == 0 {
			t.Fatal("the blocks below the player were not resent")
		}
	})
}

// TestFlyHealsAGhostBlock checks a client that stands on a block the server
// does not have: it floats, as the server sees it, and is set back once
// the blocks below it were resent.
func TestFlyHealsAGhostBlock(t *testing.T) {
	w := traceWorld(t)
	r := clienttest.New()
	r.Answers.ChunkRadius = 8
	ghost := cube.Pos{0, floorY + 4, -40}
	start := mgl64.Vec3{0.5, float64(ghost[1]) + 1, -39.5}
	withCheckedPlayer(t, w, r, anticheat.Default(), start, func(tx *world.Tx, p *Player) {
		standThroughJoin(tx, p)
		r.Reset()
		feedPositions(tx, p, r, start, 90, func(int) mgl64.Vec3 { return start })
		resent := false
		for _, c := range r.CallsTo("ViewBlockUpdate") {
			if c.Args[0] == ghost {
				if _, air := c.Args[1].(block.Air); air {
					resent = true
				}
			}
		}
		if !resent || len(r.CallsTo("CorrectMove")) != 1 {
			t.Fatalf("ghost block resent %v, corrections %d; want resent, one", resent, len(r.CallsTo("CorrectMove")))
		}
	})
}

// TestFlyToggleIsCounted checks that a flight asked for in survival is
// refused and counted as a violation of Fly, unless the game mode just
// changed.
func TestFlyToggleIsCounted(t *testing.T) {
	w := traceWorld(t)
	r := clienttest.New()
	withCheckedPlayer(t, w, r, anticheat.Default(), laneStart(laneStone), func(tx *world.Tx, p *Player) {
		h := &violationRecorder{}
		p.Handle(h)
		standThroughJoin(tx, p)
		r.SetHandlingInput(true)
		p.StartFlying()
		r.SetHandlingInput(false)
		if p.Flying() || h.count(anticheat.Fly) != 1 || h.violations[0].Kind != "toggle" || len(r.CallsTo("SendAbilities")) != 1 {
			t.Fatalf("flying %v, violations %+v, abilities resent %d; want refused, one toggle, resent", p.Flying(), h.violations, len(r.CallsTo("SendAbilities")))
		}
		p.SetGameMode(world.GameModeAdventure)
		r.SetHandlingInput(true)
		p.StartFlying()
		r.SetHandlingInput(false)
		if h.count(anticheat.Fly) != 1 {
			t.Fatal("a toggle right after a change of game mode was counted")
		}
	})
}

// TestFlyOffAndExempt checks that a disabled Fly, and a player who may fly,
// are not judged.
func TestFlyOffAndExempt(t *testing.T) {
	w := traceWorld(t)
	tr := hoverTrace("off", 120)
	tr.conf = func(c *anticheat.Config) {
		p := c.Policy(anticheat.Fly)
		p.Enabled = false
		c.SetPolicy(anticheat.Fly, p)
	}
	for _, v := range runTrace(t, w, tr).violations {
		if v.Check == anticheat.Fly {
			t.Fatalf("Fly off: %+v", v)
		}
	}
	tr = hoverTrace("creative", 120)
	tr.setup = func(_ *world.Tx, p *Player, _ *javaMover) { p.SetGameMode(world.GameModeCreative) }
	for _, v := range runTrace(t, w, tr).violations {
		if v.Check == anticheat.Fly {
			t.Fatalf("a player who may fly: %+v", v)
		}
	}
}

// TestFlyAllowsGliding checks a glide with firework boosts: not judged.
func TestFlyAllowsGliding(t *testing.T) {
	w := traceWorld(t)
	start := laneStart(laneStone).Add(mgl64.Vec3{0, 40, 0})
	tr := acTrace{name: "gliding", start: start,
		setup: func(_ *world.Tx, p *Player, _ *javaMover) {
			p.Armour().SetChestplate(item.NewStack(item.Elytra{}, 1))
			p.StartGliding()
		},
		positions: func(*world.Tx) []mgl64.Vec3 {
			var ps []mgl64.Vec3
			pos, vel := start, mgl64.Vec3{0.8, -0.1, 0}
			for i := range 150 {
				if i%40 == 0 {
					// A firework.
					vel = mgl64.Vec3{1.7, 0.2, 0}
				}
				vel = mgl64.Vec3{vel[0]*0.99 + 0.01, vel[1]*0.98 - 0.01, 0}
				pos = pos.Add(vel)
				ps = append(ps, pos)
			}
			return ps
		}}
	if res := runTrace(t, w, tr); len(res.violations) != 0 || res.corrections != 0 {
		t.Fatalf("gliding: %+v, %d corrections", res.violations, res.corrections)
	}
}

// standThroughJoin has the client of p stand still through the grace of
// its join.
func standThroughJoin(tx *world.Tx, p *Player) {
	for range p.anticheat().conf.Grace.Join + 1 {
		p.ClientMove(clientMoveAt(p.Position()))
		p.Tick(tx, p.tickCount)
	}
}

// feedPositions feeds the client moves to the positions of at for the moves
// passed, a tick each, from start, acknowledging a correction as a client
// does once it is sent.
func feedPositions(tx *world.Tx, p *Player, r *clienttest.Recorder, start mgl64.Vec3, moves int, at func(i int) mgl64.Vec3) {
	corrected := len(r.CallsTo("CorrectMove"))
	for i := range moves {
		m := clientMoveAt(at(i))
		if n := len(r.CallsTo("CorrectMove")); n > corrected {
			corrected = n
			m = clientMoveAt(p.Position())
			m.Acknowledged = true
		}
		p.ClientMove(m)
		p.Tick(tx, p.tickCount)
	}
}
