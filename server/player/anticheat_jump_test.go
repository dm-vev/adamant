package player

import (
	"fmt"
	"log/slog"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/client/clienttest"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player/anticheat"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// The false-positive suite of the jumps of Bedrock clients: a client sends
// the position of its eyes as float32, claims to stand on the ground with
// the VerticalCollision flag of its input, and stands on shapes its blocks
// have that the server's may lack. None of its legitimate jumps may violate
// a check.

// The lanes of jumpWorld: each runs along X from x = -40 to 40, three
// blocks wide, at the Z passed.
const (
	jumpStone = -44 + 4*iota
	jumpSlab
	jumpFarmland
	jumpPath
	jumpSoulSand
	jumpSnow
	jumpCarpet
	jumpHoney
	jumpSlime
	jumpIce
	jumpStairs
	jumpStep
	jumpGap
	jumpSnowOne
)

// jumpLane is a lane of jumpWorld: the block it is made of, put in the cell
// at the height of the floor or on it, and the height of the feet of a
// player standing on it.
type jumpLane struct {
	name string
	z    int
	// put makes the block of the lane at x, in tx.
	put func(tx *world.Tx, x, z int)
	// top is the height of the feet of a player on the lane.
	top float64
}

// flatLane returns a lane of a block that fills the cell below the floor.
func flatLane(name string, z int, b world.Block, top float64) jumpLane {
	return jumpLane{name: name, z: z, top: top, put: func(tx *world.Tx, x, z int) {
		tx.SetBlock(cube.Pos{x, floorY - 1, z}, b, nil)
	}}
}

// jumpLanes returns the lanes of jumpWorld that are flat: walked, sprinted
// and jumped along.
func jumpLanes() []jumpLane {
	snow := func(layers uint8) jumpLane {
		return jumpLane{name: fmt.Sprintf("%d layers of snow", layers), z: jumpSnow, top: floorY + float64(layers-1)/8, put: func(tx *world.Tx, x, z int) {
			tx.SetBlock(cube.Pos{x, floorY, z}, block.SnowLayer{Layers: layers}, nil)
		}}
	}
	return []jumpLane{
		flatLane("stone", jumpStone, block.Stone{}, floorY),
		flatLane("slabs", jumpSlab, block.Slab{Block: block.Stone{}}, floorY-0.5),
		flatLane("farmland", jumpFarmland, block.Farmland{}, floorY-0.0625),
		flatLane("dirt paths", jumpPath, block.DirtPath{}, floorY-0.0625),
		flatLane("soul sand", jumpSoulSand, block.SoulSand{}, floorY-0.125),
		snow(3),
		{name: "carpets", z: jumpCarpet, top: floorY + 0.0625, put: func(tx *world.Tx, x, z int) {
			tx.SetBlock(cube.Pos{x, floorY, z}, block.Carpet{Colour: item.ColourWhite()}, nil)
		}},
		flatLane("honey", jumpHoney, block.HoneyBlock{}, floorY),
		flatLane("slime", jumpSlime, block.Slime{}, floorY),
		flatLane("ice", jumpIce, block.Ice{}, floorY),
	}
}

// jumpWorld returns a synchronous world with the courses of the jumps: a
// stone floor with the lanes of jumpLanes, a staircase, a wall one block
// high, a gap two blocks wide and a lane of single layers of snow.
func jumpWorld(t testing.TB) *world.World {
	t.Helper()
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Log: slog.New(slog.DiscardHandler), Entities: entity.DefaultRegistry}.New()
	viewer := clienttest.New()
	loader := world.NewLoader(5, w, viewer)
	t.Cleanup(func() {
		<-w.Exec(func(tx *world.Tx) { loader.Close(tx) })
		_ = w.Close()
	})
	<-w.Exec(func(tx *world.Tx) {
		loader.Move(tx, mgl64.Vec3{0.5, floorY, 0.5})
		loader.Load(tx, 121)
		for x := -48; x <= 48; x++ {
			for z := -48; z <= 48; z++ {
				tx.SetBlock(cube.Pos{x, floorY - 1, z}, block.Stone{}, nil)
			}
		}
		for _, lane := range jumpLanes() {
			for x := -40; x <= 40; x++ {
				for dz := -1; dz <= 1; dz++ {
					lane.put(tx, x, lane.z+dz)
				}
			}
		}
		for x := -40; x <= 40; x++ {
			for dz := -1; dz <= 1; dz++ {
				tx.SetBlock(cube.Pos{x, floorY, jumpSnowOne + dz}, block.SnowLayer{Layers: 1}, nil)
			}
		}
		for k := range 8 {
			for dz := -1; dz <= 1; dz++ {
				tx.SetBlock(cube.Pos{-30 + k, floorY + k, jumpStairs + dz}, block.Stairs{Block: block.Stone{}, Facing: cube.East}, nil)
			}
		}
		for dz := -1; dz <= 1; dz++ {
			tx.SetBlock(cube.Pos{-20, floorY, jumpStep + dz}, block.Stone{}, nil)
			for x := -10; x <= -9; x++ {
				tx.SetBlock(cube.Pos{x, floorY - 1, jumpGap + dz}, nil, nil)
			}
		}
	})
	return w
}

// bedrockSprint returns an input that sprints east, jumping as jump says.
func bedrockSprint(jump func(i int, j *javaMover) bool) func(int, *javaMover) javaInput {
	return func(i int, j *javaMover) javaInput {
		return javaInput{forward: 1, yaw: east, sprint: true, jump: jump(i, j)}
	}
}

// always and never are jump inputs.
func always(int, *javaMover) bool { return true }
func never(int, *javaMover) bool  { return false }

// laneStartAt returns where a trace along the lane at z starts, at the
// height passed.
func laneStartAt(z int, top float64) mgl64.Vec3 {
	return mgl64.Vec3{-38.5, top, float64(z) + 0.5}
}

// jumpTraces returns the legitimate jumps of a Bedrock client.
func jumpTraces() []acTrace {
	var traces []acTrace
	for _, lane := range jumpLanes() {
		start := laneStartAt(lane.z, lane.top)
		traces = append(traces,
			acTrace{name: "standing jumps on " + lane.name, start: start, ticks: 120, input: func(i int, _ *javaMover) javaInput {
				return javaInput{jump: i%30 < 3}
			}},
			acTrace{name: "a sprint, then one sprint-jump on " + lane.name, start: start, ticks: 80,
				input: bedrockSprint(func(i int, _ *javaMover) bool { return i == 30 })},
			acTrace{name: "repeated sprint-jumps on " + lane.name, start: start, ticks: 160, input: bedrockSprint(always)},
			acTrace{name: "walking jumps on " + lane.name, start: start, ticks: 150, input: func(int, *javaMover) javaInput {
				return javaInput{forward: 1, yaw: east, jump: true}
			}},
		)
	}
	traces = append(traces,
		acTrace{name: "sprinting up a staircase", start: laneStartAt(jumpStairs, floorY), ticks: 100,
			input: bedrockSprint(never)},
		acTrace{name: "sprint-jumping up a staircase", start: laneStartAt(jumpStairs, floorY), ticks: 100,
			input: bedrockSprint(always)},
		acTrace{name: "auto-jumping up a step of one block", start: laneStartAt(jumpStep, floorY), ticks: 100,
			input: bedrockSprint(func(_ int, j *javaMover) bool { return j.onGround && j.horizontalCollision })},
		acTrace{name: "sprint-jumping up a step of one block", start: laneStartAt(jumpStep, floorY), ticks: 100,
			input: bedrockSprint(func(_ int, j *javaMover) bool { return j.onGround && j.pos[0] > -22 })},
		// The jump begins as the last of the box leaves the edge.
		acTrace{name: "sprint-jumping over a gap from its edge", start: laneStartAt(jumpGap, floorY), ticks: 100,
			input: bedrockSprint(func(_ int, j *javaMover) bool { return j.onGround && j.pos[0] > -10.25 })},
		acTrace{name: "jumping off the edge of a gap", start: laneStartAt(jumpGap, floorY), ticks: 100,
			input: bedrockSprint(func(_ int, j *javaMover) bool { return j.onGround && j.pos[0] > -9.72 })},
	)
	return traces
}

// shapeTraces returns the jumps of a client on blocks whose shapes differ
// between it and the server: a single layer of snow it stands on, 0.125
// above the block below, where the server's has no collision; honey it
// stands lower on, where the server's is a full block.
func shapeTraces() []acTrace {
	var traces []acTrace
	for _, tc := range []struct {
		name string
		z    int
		top  float64
		lift float64
	}{
		{"a single layer of snow, stood on", jumpSnowOne, floorY, 0.125},
		{"honey 15 pixels high", jumpHoney, floorY, -0.0625},
	} {
		start := laneStartAt(tc.z, tc.top)
		traces = append(traces,
			acTrace{name: "standing jumps on " + tc.name, start: start, ticks: 120, lift: tc.lift, input: func(i int, _ *javaMover) javaInput {
				return javaInput{jump: i%30 < 3}
			}},
			acTrace{name: "sprint-jumps on " + tc.name, start: start, ticks: 160, lift: tc.lift, input: bedrockSprint(always)},
			acTrace{name: "a sprint, then a sprint-jump on " + tc.name, start: start, ticks: 80, lift: tc.lift,
				input: bedrockSprint(func(i int, _ *javaMover) bool { return i == 30 })},
		)
	}
	return traces
}

// TestBedrockJumpsAreLegitimate runs the jumps of a Bedrock client through
// the default anti-cheat: none may violate a check or be corrected.
func TestBedrockJumpsAreLegitimate(t *testing.T) {
	w := jumpWorld(t)
	for _, tr := range append(jumpTraces(), shapeTraces()...) {
		tr.bedrock = true
		res := runTrace(t, w, tr)
		if len(res.violations) != 0 || res.corrections != 0 {
			first := res.violations
			if len(first) > 2 {
				first = first[:2]
			}
			t.Errorf("%s: %d violations, %d corrections: %+v", tr.name, len(res.violations), res.corrections, first)
		}
	}
}

// TestBedrockJumpsWithoutTheClaim runs the jumps again with a client that
// never says it collided below (VerticalCollision): the server reads its
// ground from the blocks and from the client having stopped falling.
func TestBedrockJumpsWithoutTheClaim(t *testing.T) {
	w := jumpWorld(t)
	for _, tr := range append(jumpTraces(), shapeTraces()...) {
		tr.bedrock = true
		tr.claims = func(int) bool { return false }
		res := runTrace(t, w, tr)
		if len(res.violations) != 0 || res.corrections != 0 {
			first := res.violations
			if len(first) > 2 {
				first = first[:2]
			}
			t.Errorf("%s: %d violations, %d corrections: %+v", tr.name, len(res.violations), res.corrections, first)
		}
	}
}

// jumpArc returns the heights above the start of the ticks of a jump: Java's
// jumpFromGround, then gravity, until the player is back on the ground,
// where the last of them is 0.
func jumpArc() []float64 {
	var arc []float64
	y, vy := 0.0, 0.42
	for y+vy > 0 {
		y += vy
		arc = append(arc, y)
		vy = (vy - 0.08) * 0.98
	}
	return append(arc, 0)
}

// TestBedrockJumpFromTheEdgeOfABlock checks that a client whose box is
// 0.00005 blocks off the edge of the block it stands on, as a float32
// position puts it, still jumps: three jumps in a row, landing and taking
// off at once.
func TestBedrockJumpFromTheEdgeOfABlock(t *testing.T) {
	w := jumpWorld(t)
	// The block ends at x = -10: a box centred 0.3 + 0.00005 past it does
	// not touch it.
	edge := mgl64.Vec3{-9.7 + 5e-5, floorY, float64(jumpGap) + 0.5}
	var ys []float64
	for range 5 {
		ys = append(ys, 0)
	}
	for range 3 {
		ys = append(ys, jumpArc()...)
	}
	ys = append(ys, 0, 0, 0)
	tr := acTrace{name: "jumping from the edge", start: edge, bedrock: true,
		positions: func(*world.Tx) []mgl64.Vec3 {
			ps := make([]mgl64.Vec3, len(ys))
			for i, y := range ys {
				ps[i] = edge.Add(mgl64.Vec3{0, y, 0})
			}
			return ps
		},
		claims: func(i int) bool { return ys[i] == 0 }}
	res := runTrace(t, w, tr)
	if len(res.violations) != 0 || res.corrections != 0 {
		t.Fatalf("%d violations, %d corrections: %+v", len(res.violations), res.corrections, res.violations)
	}
}

// TestJumpCheatsAreStillFlagged checks that the lenient ground does not give
// a jump to a client that is not near a block: jumping in mid-air, a
// player 0.3 blocks above the floor, and jumping again as it rises, all
// with the claim of a collision below.
func TestJumpCheatsAreStillFlagged(t *testing.T) {
	w := jumpWorld(t)
	for _, tc := range []struct {
		name  string
		start mgl64.Vec3
		ys    func(i int) float64
		min   int
	}{
		{"jumping in mid-air, every tick", mgl64.Vec3{-38.5, floorY + 5, float64(jumpStone) + 0.5}, func(i int) float64 { return 0.42 * float64(i+1) }, 15},
		{"jumping 0.3 above the floor", mgl64.Vec3{-38.5, floorY + 0.3, float64(jumpStone) + 0.5}, func(i int) float64 { return 0.42 * float64(i+1) }, 15},
		{"jumping from 0.2 above the floor, then again", mgl64.Vec3{-38.5, floorY + 0.2, float64(jumpStone) + 0.5}, func(i int) float64 { return 0.42 * float64(i+1) }, 14},
		{"rising 0.2 a tick off the floor", mgl64.Vec3{-38.5, floorY, float64(jumpStone) + 0.5}, func(i int) float64 { return 0.5 * float64(i+1) }, 15},
	} {
		tc := tc
		tr := acTrace{name: tc.name, start: tc.start, bedrock: true,
			positions: func(*world.Tx) []mgl64.Vec3 {
				ps := make([]mgl64.Vec3, 20)
				for i := range ps {
					ps[i] = tc.start.Add(mgl64.Vec3{0, tc.ys(i), 0})
				}
				return ps
			},
			claims: func(int) bool { return true }}
		res := runTrace(t, w, tr)
		if n, _ := res.count(anticheat.Ascend); n < tc.min {
			t.Errorf("%s: %d violations of Ascend, want at least %d", tc.name, n, tc.min)
		}
	}
}

// bedrockWire returns the feet the server reads of the position of a
// client at feet: the client sends its eye, feet + 1.62, as float32, and
// the session takes NetworkOffset (1.62001) off it in float32.
func bedrockWire(feet mgl64.Vec3) mgl64.Vec3 {
	var eye [3]float32
	for i, v := range feet {
		eye[i] = float32(v)
	}
	eye[1] += float32(1.62)
	off := float32(Type.NetworkOffset())
	return mgl64.Vec3{float64(eye[0]), float64(eye[1] - off), float64(eye[2])}
}

// TestBedrockPositionsOfLegitimateTraces runs the legitimate traces of the
// false-positive suites again with the positions of a Bedrock client, float32
// eyes: none may violate a check.
func TestBedrockPositionsOfLegitimateTraces(t *testing.T) {
	w := traceWorld(t)
	for _, tr := range legitimateTraces() {
		tr.bedrock = true
		res := runTrace(t, w, tr)
		if len(res.violations) != 0 || res.corrections != 0 {
			first := res.violations
			if len(first) > 3 {
				first = first[:3]
			}
			t.Errorf("%s: %d violations, %d corrections: %+v", tr.name, len(res.violations), res.corrections, first)
		}
	}
}
