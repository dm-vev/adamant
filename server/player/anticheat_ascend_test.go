package player

import (
	"testing"

	"github.com/df-mc/dragonfly/server/client/clienttest"
	"github.com/df-mc/dragonfly/server/player/anticheat"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// count returns how many violations of c the result holds, and of what
// kinds.
func (r traceResult) count(c anticheat.Check) (n int, kinds map[string]int) {
	kinds = map[string]int{}
	for _, v := range r.violations {
		if v.Check == c {
			n++
			kinds[v.Kind]++
		}
	}
	return n, kinds
}

// TestAscendDetectsFlyingUp checks that rising 0.5 a tick off the stone
// lane violates Ascend, which logs by default.
func TestAscendDetectsFlyingUp(t *testing.T) {
	w := traceWorld(t)
	start := laneStart(laneStone)
	res := runTrace(t, w, acTrace{name: "flying up", start: start, positions: func(*world.Tx) []mgl64.Vec3 {
		ps := make([]mgl64.Vec3, 20)
		for i := range ps {
			ps[i] = start.Add(mgl64.Vec3{0, 0.5 * float64(i+1), 0})
		}
		return ps
	}})
	if n, _ := res.count(anticheat.Ascend); n < 15 || res.corrections != 0 {
		t.Fatalf("%d violations of Ascend, %d corrections; want most moves logged", n, res.corrections)
	}
}

// noFallTrace falls from 30 blocks up, blipping 0.01 up every fifth move
// while it falls, as NoFall resets the fall distance.
func noFallTrace(conf func(c *anticheat.Config)) acTrace {
	start := mgl64.Vec3{-20.5, floorY + 30, -40.5}
	return acTrace{name: "NoFall", start: start, conf: conf, positions: func(*world.Tx) []mgl64.Vec3 {
		var ps []mgl64.Vec3
		pos, vy := start, 0.0
		for i := 0; pos[1] > floorY+1; i++ {
			if i%5 == 4 {
				pos = pos.Add(mgl64.Vec3{0, 0.01, 0})
			} else {
				vy = (vy - 0.08) * 0.98
				pos = pos.Add(mgl64.Vec3{0, vy, 0})
			}
			ps = append(ps, pos)
		}
		return ps
	}}
}

// TestAscendDetectsNoFall checks that up-blips while falling violate
// Ascend as blips, and that with the action cancel they no longer reset the
// fall distance, which the default leaves alone.
func TestAscendDetectsNoFall(t *testing.T) {
	w := traceWorld(t)
	res := runTrace(t, w, noFallTrace(nil))
	if n, kinds := res.count(anticheat.Ascend); n < 3 || kinds["blip"] != n {
		t.Fatalf("%d violations of Ascend, kinds %v; want the blips", n, kinds)
	}
	for _, cancel := range []bool{false, true} {
		conf := anticheat.Default()
		if cancel {
			p := conf.Policy(anticheat.Ascend)
			p.Action = anticheat.Cancel
			conf.SetPolicy(anticheat.Ascend, p)
		}
		tr := noFallTrace(nil)
		r := clienttest.New()
		withCheckedPlayer(t, w, r, conf, tr.start, func(tx *world.Tx, p *Player) {
			standThroughJoin(tx, p)
			positions := tr.positions(tx)
			most := 0.0
			for _, pos := range positions {
				p.ClientMove(clientMoveAt(pos))
				p.Tick(tx, p.tickCount)
				most = max(most, p.FallDistance())
			}
			if kept := most > 20; kept != cancel {
				t.Errorf("cancel %v: the fall distance reached %.1f", cancel, most)
			}
		})
	}
}

// TestAscendOffAndExempt checks that a disabled Ascend, and a player who
// may fly, are not judged.
func TestAscendOffAndExempt(t *testing.T) {
	w := traceWorld(t)
	off := noFallTrace(func(c *anticheat.Config) {
		p := c.Policy(anticheat.Ascend)
		p.Enabled = false
		c.SetPolicy(anticheat.Ascend, p)
	})
	if n, _ := runTrace(t, w, off).count(anticheat.Ascend); n != 0 {
		t.Fatalf("Ascend off: %d violations", n)
	}
	creative := noFallTrace(nil)
	creative.setup = func(_ *world.Tx, p *Player, _ *javaMover) { p.SetGameMode(world.GameModeCreative) }
	if n, _ := runTrace(t, w, creative).count(anticheat.Ascend); n != 0 {
		t.Fatalf("a player who may fly: %d violations", n)
	}
}
