package player

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/client/clienttest"
	"github.com/df-mc/dragonfly/server/player/anticheat"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestBlockReachDefaults checks that BlockReach keeps the reach of before
// the anti-cheat: 8 blocks from the eyes to the centre of a block, 14 in
// creative; that only a client's action out of reach counts as a
// violation; and that its numbers are those of the config.
func TestBlockReachDefaults(t *testing.T) {
	w, _ := clientTestWorld(t)
	r := clienttest.New()
	at := mgl64.Vec3{0.5, 100, 0.5}
	withCheckedPlayer(t, w, r, anticheat.Default(), at, func(tx *world.Tx, p *Player) {
		h := &violationRecorder{}
		p.Handle(h)
		// The eyes are at 101.62; a block whose centre is 7.9 away and one
		// 8.1 away.
		near, far := cube.Pos{0, 101, 7}, cube.Pos{0, 101, 8}
		if !p.canReachBlock(near) || p.canReachBlock(far) {
			t.Fatalf("reach of 8: near %v, far %v", p.canReachBlock(near), p.canReachBlock(far))
		}
		if len(h.violations) != 0 {
			t.Fatalf("a plugin's reach counted as a violation: %+v", h.violations)
		}
		r.SetHandlingInput(true)
		if p.canReachBlock(far) || h.count(anticheat.BlockReach) != 1 {
			t.Fatalf("a client's block out of reach: reached, or %d violations", h.count(anticheat.BlockReach))
		}
		r.SetHandlingInput(false)
		p.SetGameMode(world.GameModeCreative)
		if !p.canReachBlock(cube.Pos{0, 101, 13}) || p.canReachBlock(cube.Pos{0, 101, 15}) {
			t.Fatal("the creative reach is not 14")
		}
	})
	// Java's rule, 4.5 blocks to the box and 1 of buffer, and a disabled
	// check.
	java := anticheat.Default()
	java.BlockReach = anticheat.ReachSettings{Measure: "box", Survival: 4.5, Creative: 5, Buffer: 1}
	withCheckedPlayer(t, w, clienttest.New(), java, at, func(tx *world.Tx, p *Player) {
		if !p.canReachBlock(cube.Pos{0, 101, 5}) || p.canReachBlock(cube.Pos{0, 101, 7}) {
			t.Fatalf("Java's reach to the box: 5 away %v, 7 away %v", p.canReachBlock(cube.Pos{0, 101, 5}), p.canReachBlock(cube.Pos{0, 101, 7}))
		}
	})
	off := withPolicy(anticheat.BlockReach, func(p *anticheat.Policy) { p.Enabled = false })
	withCheckedPlayer(t, w, clienttest.New(), off, at, func(tx *world.Tx, p *Player) {
		if !p.canReachBlock(cube.Pos{0, 101, 30}) {
			t.Fatal("with BlockReach off, a block 30 away is out of reach")
		}
	})
}

// TestBreakingTheSameBlockKeepsItsStart checks that a client that starts
// breaking the block it breaks, as ContinueDestroyBlock does, keeps the
// break as it was rather than starting it again.
func TestBreakingTheSameBlockKeepsItsStart(t *testing.T) {
	w, viewer := clientTestWorld(t)
	withCheckedPlayer(t, w, clienttest.New(), anticheat.Default(), mgl64.Vec3{0.5, 100, 0.5}, func(tx *world.Tx, p *Player) {
		pos := cube.Pos{0, 100, 2}
		tx.SetBlock(pos, block.Stone{}, nil)
		viewer.Reset()
		p.StartBreaking(pos, cube.FaceNorth)
		p.ContinueBreaking(cube.FaceNorth)
		counter := p.breakCounter
		p.StartBreaking(pos, cube.FaceEast)
		if !p.breaking || p.breakCounter != counter || p.breakingFace != cube.FaceEast {
			t.Fatalf("breaking %v, counter %d (was %d), face %v", p.breaking, p.breakCounter, counter, p.breakingFace)
		}
		starts := 0
		for _, c := range viewer.CallsTo("ViewBlockAction") {
			if _, ok := c.Args[1].(block.StartCrackAction); ok {
				starts++
			}
		}
		if starts != 1 {
			t.Fatalf("the crack started %d times, want once", starts)
		}
	})
}
