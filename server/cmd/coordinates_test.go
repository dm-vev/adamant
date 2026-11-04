package cmd

import (
	"math"
	"reflect"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/go-gl/mathgl/mgl64"
)

// near compares vectors per axis to within rounding of the trigonometry.
func near(a, b mgl64.Vec3) bool {
	for i := range 3 {
		if math.Abs(a[i]-b[i]) > 1e-9 {
			return false
		}
	}
	return true
}

// Vec3Argument reads WorldCoordinates (each axis absolute or `~` relative to
// the source position) or LocalCoordinates (all three axes `^`, relative to
// the source rotation). Mixing `^` with other kinds is
// argument.pos.mixed; a coordinate reads at most one prefix.
func TestCoordinateArguments(t *testing.T) {
	src := testSource{pos: mgl64.Vec3{10, 64, -20}}
	for _, c := range []struct {
		args []string
		want mgl64.Vec3
		ok   bool
	}{
		{[]string{"1", "2", "3"}, mgl64.Vec3{1, 2, 3}, true},
		{[]string{"~", "~", "~"}, mgl64.Vec3{10, 64, -20}, true},
		{[]string{"~1", "~-2", "~0.5"}, mgl64.Vec3{11, 62, -19.5}, true},
		{[]string{"5", "~", "-5"}, mgl64.Vec3{5, 64, -5}, true},
		{[]string{"^", "^", "^"}, mgl64.Vec3{10, 64, -20}, true},
		{[]string{"^", "~", "^"}, mgl64.Vec3{}, false},
		{[]string{"^1", "2", "3"}, mgl64.Vec3{}, false},
		{[]string{"~~", "0", "0"}, mgl64.Vec3{}, false},
		{[]string{"~^", "0", "0"}, mgl64.Vec3{}, false},
		{[]string{"x", "0", "0"}, mgl64.Vec3{}, false},
		{[]string{"~NaN", "0", "0"}, mgl64.Vec3{}, false},
		{[]string{"0", "Inf", "0"}, mgl64.Vec3{}, false},
		{[]string{"0", "0", "1e400"}, mgl64.Vec3{}, false},
	} {
		got, err := ResolveCoordinates(c.args, src)
		if (err == nil) != c.ok || !near(got, c.want) {
			t.Errorf("%q = %v, %v; want %v, ok=%v", c.args, got, err, c.want, c.ok)
		}
	}
}

// LocalCoordinates.getPosition: `^left ^up ^forwards` relative to the
// rotation, where yaw 0 faces south (+Z), yaw 90 faces west (-X) and pitch
// -90 faces up. Left of south is east (+X).
func TestLocalCoordinatesFollowRotation(t *testing.T) {
	for _, c := range []struct {
		name string
		rot  cube.Rotation
		args []string
		want mgl64.Vec3
	}{
		{"forwards facing south", cube.Rotation{0, 0}, []string{"^", "^", "^2"}, mgl64.Vec3{0, 0, 2}},
		{"forwards facing west", cube.Rotation{90, 0}, []string{"^", "^", "^2"}, mgl64.Vec3{-2, 0, 0}},
		{"forwards facing north", cube.Rotation{180, 0}, []string{"^", "^", "^1"}, mgl64.Vec3{0, 0, -1}},
		{"forwards facing east", cube.Rotation{-90, 0}, []string{"^", "^", "^1"}, mgl64.Vec3{1, 0, 0}},
		{"left facing south", cube.Rotation{0, 0}, []string{"^1", "^", "^"}, mgl64.Vec3{1, 0, 0}},
		{"up facing south", cube.Rotation{0, 0}, []string{"^", "^3", "^"}, mgl64.Vec3{0, 3, 0}},
		{"forwards looking up", cube.Rotation{0, -90}, []string{"^", "^", "^1"}, mgl64.Vec3{0, 1, 0}},
		{"up looking up is north", cube.Rotation{0, -90}, []string{"^", "^1", "^"}, mgl64.Vec3{0, 0, -1}},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, err := ResolveCoordinates(c.args, testSource{rot: c.rot})
			if err != nil {
				t.Fatal(err)
			}
			if !near(got, c.want) {
				t.Fatalf("%q at %v = %v, want %v", c.args, c.rot, got, c.want)
			}
		})
	}
}

func TestLineConsumesArguments(t *testing.T) {
	line := &Line{args: []string{"a", "b", "c"}, seen: []string{"/x"}}
	if v, ok := line.NextN(4); ok || v != nil {
		t.Fatalf("NextN(4) of 3 = %v, %v", v, ok)
	}
	line.RemoveNext()
	if v, _ := line.Next(); v != "b" || line.Len() != 2 {
		t.Fatalf("after RemoveNext: next %q, len %d", v, line.Len())
	}
	if err := line.SyntaxError(); err == nil || err.Error() == "" {
		t.Fatal("no syntax error text")
	}
	if got := line.Leftover(); !reflect.DeepEqual(got, []string{"b", "c"}) || line.Len() != 0 {
		t.Fatalf("Leftover = %v, len %d", got, line.Len())
	}
	line = &Line{args: []string{"a"}}
	line.RemoveN(2)
	if line.Len() != 0 {
		t.Fatal("RemoveN past the end left arguments")
	}
	if _, ok := line.Next(); ok {
		t.Fatal("Next on an empty line succeeded")
	}
}

// wrapDegrees ports Mth.wrapDegrees: the result lies in [-180, 180).
func TestWrapDegrees(t *testing.T) {
	for in, want := range map[float64]float64{0: 0, 180: -180, -180: -180, 190: -170, -190: 170, 360: 0, 540: -180, 359: -1, -720: 0} {
		if got := wrapDegrees(in); got != want {
			t.Errorf("wrapDegrees(%v) = %v, want %v", in, got, want)
		}
	}
	if !math.IsNaN(wrapDegrees(math.NaN())) {
		t.Error("NaN did not stay NaN")
	}
}
