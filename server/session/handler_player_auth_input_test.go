package session

import (
	"io"
	"log/slog"
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

func TestTeleportAcknowledgementClearedForUnchangedMovement(t *testing.T) {
	expected := mgl64.Vec3{}
	s := &Session{conf: Config{Log: slog.New(slog.NewTextHandler(io.Discard, nil))}}
	s.teleportPos.Store(&expected)
	c := newUnchangedControllable()
	pk := &packet.PlayerAuthInput{Position: vec64To32(entityNetworkPosition(c, expected))}

	if err := (PlayerAuthInputHandler{}).handleMovement(pk, s, nil, c); err != nil {
		t.Fatalf("handle movement: %v", err)
	}
	if s.teleportPos.Load() != nil {
		t.Fatal("unchanged teleport acknowledgement left teleport pending")
	}
}

func TestTeleportAcknowledgedAtLargeCoordinates(t *testing.T) {
	expected := mgl64.Vec3{29_999_999, 100, -29_999_999}
	c := newUnchangedControllable()
	ack := vec32To64(entityBasePosition(c, vec64To32(entityNetworkPosition(c, expected))))
	if !teleportAcknowledged(ack, expected) {
		t.Fatal("float32-quantised teleport position was not acknowledged")
	}

	moved := ack
	moved[0] = float64(math.Nextafter32(float32(moved[0]), float32(math.Inf(1))))
	if teleportAcknowledged(moved, expected) {
		t.Fatal("meaningful movement was accepted as a teleport acknowledgement")
	}
}

type unchangedControllable struct {
	Controllable
	handle *world.EntityHandle
}

func newUnchangedControllable() unchangedControllable {
	return unchangedControllable{handle: world.EntitySpawnOpts{}.New(offsetTestType{}, offsetTestConfig{})}
}

func (c unchangedControllable) H() *world.EntityHandle { return c.handle }
func (unchangedControllable) Position() mgl64.Vec3     { return mgl64.Vec3{} }
func (unchangedControllable) Rotation() cube.Rotation {
	return cube.Rotation{}
}
func (unchangedControllable) Move(mgl64.Vec3, float64, float64) {
	panic("Move called for unchanged movement")
}

type offsetTestType struct{}

func (offsetTestType) Open(*world.Tx, *world.EntityHandle, *world.EntityData) world.Entity {
	panic("not used")
}
func (offsetTestType) EncodeEntity() string                        { return "minecraft:player" }
func (offsetTestType) NetworkOffset() float64                      { return 1.62001 }
func (offsetTestType) BBox(world.Entity) cube.BBox                 { return cube.BBox{} }
func (offsetTestType) DecodeNBT(map[string]any, *world.EntityData) {}
func (offsetTestType) EncodeNBT(*world.EntityData) map[string]any  { return nil }

type offsetTestConfig struct{}

func (offsetTestConfig) Apply(*world.EntityData) {}
