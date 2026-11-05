package player

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/go-gl/mathgl/mgl64"
)

// The bodies below are the ones the sleep and tick of Player held before
// they moved to sleepState and bedStandUpRotation (P3c). The tests run both
// over the same inputs and require equal results.

func oldSleepBegin(s *sleepState, pos cube.Pos) {
	s.sleeping = true
	s.timeSinceRest = 0
	s.sleepTimer = 0
	s.sleepPos = pos
}

func oldSleepEnd(s *sleepState) {
	s.sleeping = false
	s.sleepPos = cube.Pos{}
}

func oldSleepTick(s *sleepState) {
	if s.sleeping {
		s.timeSinceRest = 0
		s.sleepTimer = min(s.sleepTimer+1, 100)
	} else if s.timeSinceRest < int32(^uint32(0)>>1) {
		s.timeSinceRest++
	}
}

func oldSleeping(s *sleepState) (cube.Pos, bool) {
	if !s.sleeping {
		return cube.Pos{}, false
	}
	return s.sleepPos, true
}

func TestSleepStateMatchesItsOldBodies(t *testing.T) {
	rng := rand.New(rand.NewPCG(5, 6))
	for run := 0; run < 200; run++ {
		var got, want sleepState
		// Runs start with the timers anywhere, among them the top of the
		// counter of the ticks awake.
		got.timeSinceRest = []int32{0, 1, 99, math.MaxInt32 - 3, math.MaxInt32}[rng.IntN(5)]
		got.sleepTimer = rng.IntN(120)
		want = got
		for step := 0; step < 80; step++ {
			switch rng.IntN(4) {
			case 0:
				pos := cube.Pos{rng.IntN(100) - 50, rng.IntN(40), rng.IntN(100) - 50}
				got.sleepBegin(pos)
				oldSleepBegin(&want, pos)
			case 1:
				got.sleepEnd()
				oldSleepEnd(&want)
			case 2, 3:
				got.sleepTick()
				oldSleepTick(&want)
			}
			if got != want {
				t.Fatalf("step %d: state\n got %+v\nwant %+v", step, got, want)
			}
			gp, gok := got.sleepingAt()
			wp, wok := oldSleeping(&want)
			if gp != wp || gok != wok {
				t.Fatalf("step %d: sleepingAt = %v, %v, want %v, %v", step, gp, gok, wp, wok)
			}
		}
	}
}

func oldStandUpRotation(pos cube.Pos, stand mgl64.Vec3) cube.Rotation {
	look := pos.Vec3Middle().Sub(stand)
	if length := look.Len(); length >= float64(float32(1e-5)) {
		look = mgl64.Vec3{look[0] / length, look[1] / length, look[2] / length}
	} else {
		look = mgl64.Vec3{}
	}
	yaw := vanilla.Atan2(look[2], look[0])*180/float64(float32(math.Pi)) - 90
	yaw = math.Mod(yaw, 360)
	if yaw >= 180 {
		yaw -= 360
	}
	if yaw < -180 {
		yaw += 360
	}
	return cube.Rotation{float64(float32(yaw)), 0}
}

func TestBedStandUpRotationMatchesItsOldBody(t *testing.T) {
	pos := cube.Pos{10, 64, -3}
	middle := pos.Vec3Middle()
	// The eight neighbours of the bed and the bed itself, where the look is
	// along an axis, diagonal or zero; then random offsets.
	var offsets []mgl64.Vec3
	for x := -1.0; x <= 1; x++ {
		for z := -1.0; z <= 1; z++ {
			offsets = append(offsets, mgl64.Vec3{x, 0.1, z}, mgl64.Vec3{x, 1.1, z}, mgl64.Vec3{x * 2, 0, z * 2})
		}
	}
	offsets = append(offsets, mgl64.Vec3{}, mgl64.Vec3{1e-6, 0, 0}, mgl64.Vec3{0, 0, -1e-5})
	rng := rand.New(rand.NewPCG(7, 8))
	for range 2000 {
		offsets = append(offsets, mgl64.Vec3{rng.Float64()*8 - 4, rng.Float64()*3 - 1, rng.Float64()*8 - 4})
	}
	for _, o := range offsets {
		stand := middle.Add(o)
		if got, want := bedStandUpRotation(pos, stand), oldStandUpRotation(pos, stand); got != want {
			t.Fatalf("stand %v: rotation = %v, want %v", stand, got, want)
		}
	}
}
