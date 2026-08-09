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

func TestMalformedAuthInputUsesNetworkPositionFallback(t *testing.T) {
	for _, test := range []struct {
		name     string
		sleeping bool
	}{
		{name: "standing"},
		{name: "sleeping", sleeping: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			pos := mgl64.Vec3{29_999_999, 100, -29_999_999}
			c := newUnchangedControllable()
			c.position, c.sleeping = pos, test.sleeping
			var delta mgl64.Vec3
			c.move = func(d mgl64.Vec3) { delta = d }

			s := &Session{conf: Config{Log: slog.New(slog.NewTextHandler(io.Discard, nil))}}
			s.teleportPos.Store(&pos)
			pk := &packet.PlayerAuthInput{Position: [3]float32{float32(math.NaN()), float32(math.Inf(1)), float32(math.Inf(-1))}}
			if err := (PlayerAuthInputHandler{}).handleMovement(pk, s, nil, c); err != nil {
				t.Fatalf("handle movement: %v", err)
			}

			want := vec32To64(entityBasePosition(c, vec64To32(entityNetworkPosition(c, pos))))
			if got := pos.Add(delta); got != want {
				t.Fatalf("fallback position = %v, want %v", got, want)
			}
			if delta[1] != 0 {
				t.Fatalf("fallback changed base Y by %v", delta[1])
			}
			if s.teleportPos.Load() != nil {
				t.Fatal("malformed teleport acknowledgement left teleport pending")
			}
		})
	}
}

type unchangedControllable struct {
	Controllable
	handle   *world.EntityHandle
	position mgl64.Vec3
	sleeping bool
	move     func(mgl64.Vec3)
}

func newUnchangedControllable() *unchangedControllable {
	return &unchangedControllable{handle: world.EntitySpawnOpts{}.New(offsetTestType{}, offsetTestConfig{})}
}

func (c *unchangedControllable) H() *world.EntityHandle { return c.handle }
func (c *unchangedControllable) Position() mgl64.Vec3   { return c.position }
func (*unchangedControllable) Rotation() cube.Rotation {
	return cube.Rotation{}
}
func (c *unchangedControllable) Move(delta mgl64.Vec3, _, _ float64) {
	if c.move != nil {
		c.move(delta)
		return
	}
	panic("Move called for unchanged movement")
}
func (c *unchangedControllable) Sleeping() (cube.Pos, bool) { return cube.Pos{}, c.sleeping }

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
