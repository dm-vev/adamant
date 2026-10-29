package session

import (
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/go-gl/mathgl/mgl32"
	"github.com/go-gl/mathgl/mgl64"
)

// TestSeatMetadataLikeBDS checks the seat keys sent for an entity as BDS
// sends them: the rotation keys always, the seat position only once the
// entity sat somewhere, as a player on a horse does.
func TestSeatMetadataLikeBDS(t *testing.T) {
	never := v2193.NewMetadata()
	writeSeatMetadata(never, world.RiderSeat{})
	if _, ok := never[v2193.EntityDataKeySeatOffset]; ok {
		t.Fatal("seat position sent for an entity that never sat")
	}
	if never[v2193.EntityDataKeySeatLockPassengerRotationDegrees] != float32(0) {
		t.Fatal("seat rotation keys missing for an entity that never sat")
	}

	horse := v2193.NewMetadata()
	writeSeatMetadata(horse, world.RiderSeat{Position: mgl64.Vec3{0, 2.32001, -0.2}, LockRotationDegrees: 181})
	if got := horse[v2193.EntityDataKeySeatOffset]; got != (mgl32.Vec3{0, 2.32001, -0.2}) {
		t.Fatalf("horse rider seat = %v, want [0 2.32001 -0.2]", got)
	}
	if horse[v2193.EntityDataKeySeatLockPassengerRotationDegrees] != float32(181) {
		t.Fatal("horse rider seat rotation lock not sent")
	}
}
