package player

import (
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/client/clienttest"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player/anticheat"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestPlaceDetectsFastPlacing checks that placing 40 blocks a second is a
// violation of Place, logged by default, and refused with cancel; 20 a
// second, the rate of Place, are not.
func TestPlaceDetectsFastPlacing(t *testing.T) {
	for _, tc := range []struct {
		name     string
		interval time.Duration
		cancel   bool
	}{{"20 a second", 50 * time.Millisecond, false}, {"40 a second", 25 * time.Millisecond, false}, {"40 a second, cancel", 25 * time.Millisecond, true}} {
		conf := anticheat.Default()
		if tc.cancel {
			conf = withPolicy(anticheat.Place, func(p *anticheat.Policy) { p.Action = anticheat.Cancel })
		}
		withBreaker(t, conf, func(tx *world.Tx, p *Player, r *clienttest.Recorder, h *violationRecorder, _ func(int)) {
			wall := time.Unix(3_000_000, 0)
			p.anticheat().now = func() time.Time { return wall }
			placed := 0
			for i := range 40 {
				p.SetHeldItems(item.NewStack(block.Stone{}, 64), item.Stack{})
				at := cube.Pos{i%8 - 4, 99, 2 + i/8}
				tx.SetBlock(at, block.Stone{}, nil)
				p.UseItemOnBlock(at, cube.FaceUp, mgl64.Vec3{0.5, 1, 0.5})
				if _, ok := tx.Block(at.Side(cube.FaceUp)).(block.Stone); ok {
					placed++
				}
				wall = wall.Add(tc.interval)
			}
			n := h.count(anticheat.Place)
			switch {
			case tc.interval == 50*time.Millisecond && n != 0:
				t.Errorf("%s: %d violations", tc.name, n)
			case tc.interval < 50*time.Millisecond && n == 0:
				t.Errorf("%s: no violation", tc.name)
			case tc.cancel && placed == 40, !tc.cancel && placed != 40:
				t.Errorf("%s: placed %d of 40", tc.name, placed)
			}
		})
	}
}

// TestPlaceRefusesClicksOutsideTheBlock checks that a point clicked outside
// the block is refused, as Java does, and is no violation; and that a click
// on a face the eyes are behind is one.
func TestPlaceRefusesClicksOutsideTheBlock(t *testing.T) {
	withBreaker(t, anticheat.Default(), func(tx *world.Tx, p *Player, r *clienttest.Recorder, h *violationRecorder, _ func(int)) {
		at := cube.Pos{0, 99, 2}
		tx.SetBlock(at, block.Stone{}, nil)
		p.SetHeldItems(item.NewStack(block.Stone{}, 64), item.Stack{})
		p.UseItemOnBlock(at, cube.FaceUp, mgl64.Vec3{0.5, 3, 0.5})
		if _, air := tx.Block(at.Side(cube.FaceUp)).(block.Air); !air || len(h.violations) != 0 {
			t.Fatalf("a click outside the block placed %T, violations %+v", tx.Block(at.Side(cube.FaceUp)), h.violations)
		}
		// The eyes, at 101.62, are behind the face below the block.
		p.UseItemOnBlock(at, cube.FaceDown, mgl64.Vec3{0.5, 0, 0.5})
		if n, kinds := (traceResult{violations: h.violations}).count(anticheat.Place); n != 1 || kinds["face"] != 1 {
			t.Fatalf("a click on a face the eyes are behind: %+v", h.violations)
		}
	})
}

// TestPlaceLineOfSight checks that with LineOfSight a place on a point the
// eyes cannot see past a full block is a violation.
func TestPlaceLineOfSight(t *testing.T) {
	conf := anticheat.Default()
	conf.Place.LineOfSight = true
	withBreaker(t, conf, func(tx *world.Tx, p *Player, r *clienttest.Recorder, h *violationRecorder, _ func(int)) {
		at := cube.Pos{0, 101, 4}
		tx.SetBlock(at, block.Stone{}, nil)
		tx.SetBlock(cube.Pos{0, 101, 2}, block.Stone{}, nil)
		p.SetHeldItems(item.NewStack(block.Stone{}, 64), item.Stack{})
		p.UseItemOnBlock(at, cube.FaceNorth, mgl64.Vec3{0.5, 0.5, 0})
		if n, kinds := (traceResult{violations: h.violations}).count(anticheat.Place); n != 1 || kinds["sight"] != 1 {
			t.Fatalf("a place through a wall: %+v", h.violations)
		}
	})
}

// TestPlaceOff checks that a disabled Place lets fast placing and clicks
// outside the block through.
func TestPlaceOff(t *testing.T) {
	off := withPolicy(anticheat.Place, func(p *anticheat.Policy) { p.Enabled = false })
	withBreaker(t, off, func(tx *world.Tx, p *Player, r *clienttest.Recorder, h *violationRecorder, _ func(int)) {
		at := cube.Pos{0, 99, 2}
		tx.SetBlock(at, block.Stone{}, nil)
		p.SetHeldItems(item.NewStack(block.Stone{}, 64), item.Stack{})
		p.UseItemOnBlock(at, cube.FaceUp, mgl64.Vec3{0.5, 3, 0.5})
		if _, stone := tx.Block(at.Side(cube.FaceUp)).(block.Stone); !stone || len(h.violations) != 0 {
			t.Fatal("with Place off, a click outside the block was refused")
		}
	})
}
