package block

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/go-gl/mathgl/mgl64"
)

func TestHoneySlideVelocity(t *testing.T) {
	tests := []struct {
		name     string
		position mgl64.Vec3
		velocity mgl64.Vec3
		want     mgl64.Vec3
		sliding  bool
	}{
		{name: "fast edge fall", position: mgl64.Vec3{0.95, 0, 0.5}, velocity: mgl64.Vec3{0.4, -0.2, -0.2}, want: mgl64.Vec3{0.1, -0.05, -0.05}, sliding: true},
		{name: "slow edge fall", position: mgl64.Vec3{0.5, 0, 0.05}, velocity: mgl64.Vec3{0.4, -0.1, -0.2}, want: mgl64.Vec3{0.4, -0.05, -0.2}, sliding: true},
		{name: "centre fall", position: mgl64.Vec3{0.5, 0, 0.5}, velocity: mgl64.Vec3{0.4, -0.2, -0.2}, want: mgl64.Vec3{0.4, -0.2, -0.2}},
		{name: "rising at edge", position: mgl64.Vec3{0.95, 0, 0.5}, velocity: mgl64.Vec3{0.4, 0.1, -0.2}, want: mgl64.Vec3{0.4, 0.1, -0.2}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, sliding := honeySlideVelocity(cube.Pos{}, test.position, test.velocity)
			if got != test.want || sliding != test.sliding {
				t.Fatalf("honeySlideVelocity() = %v, %t; want %v, %t", got, sliding, test.want, test.sliding)
			}
		})
	}
}

func TestHoneyBlockInterfacesAndFriction(t *testing.T) {
	var honey any = HoneyBlock{}
	if _, ok := honey.(EntityInsider); !ok {
		t.Fatal("HoneyBlock does not implement EntityInsider")
	}
	frictional, ok := honey.(Frictional)
	if !ok {
		t.Fatal("HoneyBlock does not implement Frictional")
	}
	if got := frictional.Friction(); got != 0.8 {
		t.Fatalf("Friction() = %v, want 0.8", got)
	}
}
