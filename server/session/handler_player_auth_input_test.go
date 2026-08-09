package session

import (
	"io"
	"log/slog"
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/go-gl/mathgl/mgl32"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

func TestTeleportAcknowledgementClearedForUnchangedMovement(t *testing.T) {
	expected := mgl64.Vec3{}
	s := &Session{conf: Config{Log: slog.New(slog.NewTextHandler(io.Discard, nil))}}
	s.teleportPos.Store(&expected)
	c := unchangedControllable{}
	pk := &packet.PlayerAuthInput{Position: mgl32.Vec3{0, 1.62, 0}}

	if err := (PlayerAuthInputHandler{}).handleMovement(pk, s, nil, c); err != nil {
		t.Fatalf("handle movement: %v", err)
	}
	if s.teleportPos.Load() != nil {
		t.Fatal("unchanged teleport acknowledgement left teleport pending")
	}
}

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

type unchangedControllable struct{ Controllable }

func (unchangedControllable) Position() mgl64.Vec3 { return mgl64.Vec3{} }
func (unchangedControllable) Rotation() cube.Rotation {
	return cube.Rotation{}
}
func (unchangedControllable) Move(mgl64.Vec3, float64, float64) {
	panic("Move called for unchanged movement")
}
