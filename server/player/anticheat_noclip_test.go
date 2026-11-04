package player

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/client/clienttest"
	"github.com/df-mc/dragonfly/server/player/anticheat"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// wallStart is where the traces into the wall start: two blocks west of
// it.
var wallStart = mgl64.Vec3{float64(wall[0]) - 1.5, floorY, float64(wall[2]) + 0.5}

// intoWall returns positions that walk east from wallStart 0.25 a tick to
// depth blocks into the wall and back, times times.
func intoWall(depth float64, times int) []mgl64.Vec3 {
	var ps []mgl64.Vec3
	for range times {
		for x := 0.25; x <= 1.2+depth; x += 0.25 {
			ps = append(ps, wallStart.Add(mgl64.Vec3{x, 0, 0}))
		}
		for x := 1.2 + depth; x > 0; x -= 0.25 {
			ps = append(ps, wallStart.Add(mgl64.Vec3{x, 0, 0}))
		}
	}
	return ps
}

// TestNoClipDetectsWalls checks that a move into a wall violates NoClip:
// the first time the blocks entered are resent and the move only logged,
// the second time, before the level decayed, the player is set back.
func TestNoClipDetectsWalls(t *testing.T) {
	w := traceWorld(t)
	r := clienttest.New()
	r.Answers.ChunkRadius = 8
	withCheckedPlayer(t, w, r, anticheat.Default(), wallStart, func(tx *world.Tx, p *Player) {
		h := &violationRecorder{}
		p.Handle(h)
		standThroughJoin(tx, p)
		r.Reset()
		ps := intoWall(0.3, 1)
		for _, pos := range ps {
			p.ClientMove(clientMoveAt(pos))
			p.Tick(tx, p.tickCount)
		}
		if n := h.count(anticheat.NoClip); n != 1 || len(r.CallsTo("CorrectMove")) != 0 {
			t.Fatalf("into the wall once: %d violations, %d corrections; want one logged", n, len(r.CallsTo("CorrectMove")))
		}
		resent := false
		for _, c := range r.CallsTo("ViewBlockUpdate") {
			if pos := c.Args[0].(cube.Pos); pos[0] == wall[0] {
				resent = true
			}
		}
		if !resent {
			t.Fatal("the blocks of the wall were not resent")
		}
		feedPositions(tx, p, r, wallStart, len(ps), func(i int) mgl64.Vec3 { return ps[i] })
		if len(r.CallsTo("CorrectMove")) == 0 {
			t.Fatal("into the wall again: not set back")
		}
	})
}

// TestNoClipOffAndExempt checks that a disabled NoClip, and a spectator,
// are not judged.
func TestNoClipOffAndExempt(t *testing.T) {
	w := traceWorld(t)
	tr := acTrace{name: "off", start: wallStart, positions: func(*world.Tx) []mgl64.Vec3 { return intoWall(0.3, 3) },
		conf: func(c *anticheat.Config) {
			p := c.Policy(anticheat.NoClip)
			p.Enabled = false
			c.SetPolicy(anticheat.NoClip, p)
		}}
	if n, _ := runTrace(t, w, tr).count(anticheat.NoClip); n != 0 {
		t.Fatalf("NoClip off: %d violations", n)
	}
	tr.conf = nil
	tr.setup = func(_ *world.Tx, p *Player, _ *javaMover) { p.SetGameMode(world.GameModeSpectator) }
	if n, _ := runTrace(t, w, tr).count(anticheat.NoClip); n != 0 {
		t.Fatalf("a spectator: %d violations", n)
	}
}
