package player

import (
	"testing"

	"github.com/df-mc/dragonfly/server/player/anticheat"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// throughWall returns a trace that steps up to the wall, then through it in
// one move of 1.8 blocks, from a box clear in front of it to one clear
// behind it: NoClip sees no box in the wall.
func throughWall(conf func(c *anticheat.Config)) acTrace {
	front := mgl64.Vec3{float64(wall[0]) - 0.4, floorY, float64(wall[2]) + 0.5}
	return acTrace{name: "through the wall", start: front, conf: conf, positions: func(*world.Tx) []mgl64.Vec3 {
		return []mgl64.Vec3{front, front.Add(mgl64.Vec3{1.8, 0, 0}), front.Add(mgl64.Vec3{1.8, 0, 0})}
	}}
}

// TestPhaseDetectsMovesThroughWalls checks that a move through a wall that
// starts and ends clear of it violates Phase, which logs by default.
func TestPhaseDetectsMovesThroughWalls(t *testing.T) {
	w := traceWorld(t)
	res := runTrace(t, w, throughWall(nil))
	if n, _ := res.count(anticheat.Phase); n != 1 || res.corrections != 0 {
		t.Fatalf("%d violations of Phase, %d corrections; want one logged: %+v", n, res.corrections, res.violations)
	}
	if n, _ := res.count(anticheat.NoClip); n != 0 {
		t.Fatalf("NoClip saw the move: %+v", res.violations)
	}
}

// TestPhaseOffAndExempt checks that a disabled Phase, and a creative
// player, as Java leaves alone, are not judged.
func TestPhaseOffAndExempt(t *testing.T) {
	w := traceWorld(t)
	off := throughWall(func(c *anticheat.Config) {
		p := c.Policy(anticheat.Phase)
		p.Enabled = false
		c.SetPolicy(anticheat.Phase, p)
	})
	if n, _ := runTrace(t, w, off).count(anticheat.Phase); n != 0 {
		t.Fatalf("Phase off: %d violations", n)
	}
	creative := throughWall(nil)
	creative.setup = func(_ *world.Tx, p *Player, _ *javaMover) { p.SetGameMode(world.GameModeCreative) }
	if n, _ := runTrace(t, w, creative).count(anticheat.Phase); n != 0 {
		t.Fatalf("a creative player: %d violations", n)
	}
}
