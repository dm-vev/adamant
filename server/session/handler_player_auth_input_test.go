package session

import (
	"math"
	"testing"

	"github.com/go-gl/mathgl/mgl32"
	"github.com/go-gl/mathgl/mgl64"
)

func TestTeleportAcknowledgedAtLargeCoordinates(t *testing.T) {
	expected := mgl64.Vec3{29_999_999, 100, -29_999_999}
	ack := vec32To64(vec64To32(expected.Add(mgl64.Vec3{0, 1.621})).Sub(mgl32.Vec3{0, 1.62}))
	if !teleportAcknowledged(ack, expected) {
		t.Fatal("float32-quantised teleport position was not acknowledged")
	}

	moved := ack
	moved[0] = float64(math.Nextafter32(float32(moved[0]), float32(math.Inf(1))))
	if teleportAcknowledged(moved, expected) {
		t.Fatal("meaningful movement was accepted as a teleport acknowledgement")
	}
}
