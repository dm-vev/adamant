package player

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/client/clienttest"
	"github.com/df-mc/dragonfly/server/player/anticheat"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// javaPositions runs a javaMover from start for the ticks passed, with the
// input of in, against the blocks of tx, and returns its positions.
func javaPositions(tx *world.Tx, start mgl64.Vec3, ticks int, in func(int, *javaMover) javaInput) []mgl64.Vec3 {
	h := world.EntitySpawnOpts{Position: start, ID: uuid.New()}.New(Type, Config{Position: start, GameMode: world.GameModeSurvival})
	body := tx.AddEntity(h).(*Player)
	defer func() {
		tx.RemoveEntity(body)
		_ = h.Close()
	}()
	j := &javaMover{pos: start, onGround: math.Abs(start[1]-floorY) < 1e-9}
	positions := make([]mgl64.Vec3, 0, ticks)
	for i := range ticks {
		positions = append(positions, j.step(tx, body, in(i, j)))
	}
	return positions
}

// TestJavaMoverMatchesJava checks the mover of the suites against Java's
// numbers in the world: walking 0.2158 a tick, sprinting 0.2806, the apex of
// a jump 1.2522.
func TestJavaMoverMatchesJava(t *testing.T) {
	w := traceWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		for _, tc := range []struct {
			name   string
			sprint bool
			want   float64
		}{{"walking", false, 0.2158}, {"sprinting", true, 0.2806}} {
			ps := javaPositions(tx, laneStart(laneStone), 60, forward(east, tc.sprint, false))
			if got := ps[59][0] - ps[58][0]; math.Abs(got-tc.want) > 5e-4 {
				t.Errorf("%s: %.4f a tick, want %.4f", tc.name, got, tc.want)
			}
		}
		ps := javaPositions(tx, laneStart(laneStone), 20, func(i int, _ *javaMover) javaInput { return javaInput{jump: i == 0} })
		apex := 0.0
		for _, p := range ps {
			apex = max(apex, p[1]-floorY)
		}
		if math.Abs(apex-1.2522) > 5e-4 {
			t.Errorf("apex of a jump %.4f, want 1.2522", apex)
		}
	})
}

// speedHack returns a trace that sprints on stone k times as fast as Java
// does.
func speedHack(name string, k float64, conf func(c *anticheat.Config)) acTrace {
	start := laneStart(laneStone)
	return acTrace{name: name, start: start, conf: conf, positions: func(tx *world.Tx) []mgl64.Vec3 {
		ps := javaPositions(tx, start, 100, forward(east, true, false))
		for i, p := range ps {
			d := p.Sub(start)
			ps[i] = start.Add(mgl64.Vec3{d[0] * k, d[1], d[2] * k})
		}
		return ps
	}}
}

// TestSpeedDetects checks that sprinting 1.6 and 2 times as fast as Java
// violates Speed, which logs by default and sets back when told to. The
// defaults, a tolerance of 30% and an epsilon of 0.05, allow 1.5 times a
// sprint on stone; a tolerance of 20% catches it.
func TestSpeedDetects(t *testing.T) {
	w := traceWorld(t)
	setback := func(c *anticheat.Config) {
		p := c.Policy(anticheat.Speed)
		p.Action = anticheat.Setback
		c.SetPolicy(anticheat.Speed, p)
	}
	speedViolations := func(res traceResult) int {
		n := 0
		for _, v := range res.violations {
			if v.Check == anticheat.Speed {
				n++
			}
		}
		return n
	}
	for _, k := range []float64{1.6, 2} {
		res := runTrace(t, w, speedHack("speed", k, nil))
		if n := speedViolations(res); n < 50 || res.corrections != 0 {
			t.Errorf("×%v: %d violations of Speed, %d corrections; want most moves logged and none corrected", k, n, res.corrections)
		}
		if res = runTrace(t, w, speedHack("speed", k, setback)); res.corrections == 0 {
			t.Errorf("×%v with Action setback: not set back", k)
		}
	}
	tight := func(c *anticheat.Config) { c.Speed.Tolerance = 0.2 }
	if n := speedViolations(runTrace(t, w, speedHack("speed", 1.5, tight))); n < 50 {
		t.Errorf("×1.5 with a tolerance of 20%%: %d violations of Speed, want most moves", n)
	}
}

// TestSpeedOffAndExempt checks that a disabled Speed, and a spectator, are
// not judged.
func TestSpeedOffAndExempt(t *testing.T) {
	w := traceWorld(t)
	off := func(c *anticheat.Config) {
		p := c.Policy(anticheat.Speed)
		p.Enabled = false
		c.SetPolicy(anticheat.Speed, p)
	}
	if res := runTrace(t, w, speedHack("off", 2, off)); len(res.violations) != 0 {
		t.Errorf("Speed off: %d violations", len(res.violations))
	}
	tr := speedHack("spectator", 2, nil)
	tr.setup = func(_ *world.Tx, p *Player, _ *javaMover) { p.SetGameMode(world.GameModeSpectator) }
	if res := runTrace(t, w, tr); len(res.violations) != 0 {
		t.Errorf("a spectator: %d violations", len(res.violations))
	}
}

// BenchmarkAntiCheatMove measures a client move of 0.2 blocks on stone,
// unchecked and checked by the default anti-cheat. The budget is a checked
// move at most 40% dearer than an unchecked one; run it once, in a quiet
// window.
func BenchmarkAntiCheatMove(b *testing.B) {
	w := traceWorld(b)
	for _, checked := range []bool{false, true} {
		name := "unchecked"
		var conf *anticheat.Config
		if checked {
			name, conf = "checked", anticheat.Default()
		}
		b.Run(name, func(b *testing.B) {
			start := laneStart(laneStone)
			h := world.EntitySpawnOpts{Position: start, ID: uuid.New()}.New(Type, Config{Position: start, GameMode: world.GameModeSurvival, Client: clienttest.New(), AntiCheat: conf})
			<-w.Exec(func(tx *world.Tx) {
				p := tx.AddEntity(h).(*Player)
				defer func() { _ = p.Close() }()
				// Past the grace of the join.
				for i := range 62 {
					p.ClientMove(client.Move{Position: p.Position()})
					p.Tick(tx, int64(i))
				}
				b.ResetTimer()
				for i := range b.N {
					x := 0.2
					if i%400 >= 200 {
						x = -0.2
					}
					p.ClientMove(client.Move{Position: p.Position().Add(mgl64.Vec3{x, 0, 0})})
					p.Tick(tx, int64(62+i))
				}
			})
		})
	}
}
