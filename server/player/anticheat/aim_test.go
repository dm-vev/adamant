package anticheat

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/go-gl/mathgl/mgl64"
)

// TestRayHitsBox checks the ray against a box of 1×2×1 at x = 4: through
// its middle, past its side by less and more than the margin, behind the
// origin and too short to reach it.
func TestRayHitsBox(t *testing.T) {
	box := cube.Box(4, 0, 0, 5, 2, 1)
	origin := mgl64.Vec3{0, 1, 0.5}
	for _, tc := range []struct {
		name   string
		dir    mgl64.Vec3
		margin float64
		length float64
		want   bool
	}{
		{"through the middle", mgl64.Vec3{1, 0, 0}, 0, 10, true},
		{"not a unit vector", mgl64.Vec3{7, 0, 0}, 0, 10, true},
		{"past the side, no margin", mgl64.Vec3{1, 0, -0.2}, 0, 10, false},
		{"past the side, by less than the margin", mgl64.Vec3{1, 0, -0.2}, 0.5, 10, true},
		{"past the side, by more than the margin", mgl64.Vec3{1, 0, -1}, 0.5, 10, false},
		{"over the top", mgl64.Vec3{1, 1, 0}, 0, 10, false},
		{"the other way", mgl64.Vec3{-1, 0, 0}, 0, 10, false},
		{"too short", mgl64.Vec3{1, 0, 0}, 0, 3, false},
		{"zero", mgl64.Vec3{}, 0, 10, false},
		{"along a face", mgl64.Vec3{0, 0, 1}, 0, 10, false},
	} {
		if got := RayHitsBox(origin, tc.dir, box, tc.margin, tc.length); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}
