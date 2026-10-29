package session

import (
	"io"
	"log/slog"
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/go-gl/mathgl/mgl64"
)

func TestTeleportAcknowledgementClearedForUnchangedMovement(t *testing.T) {
	expected := mgl64.Vec3{}
	s := &Session{conf: Config{Log: slog.New(slog.NewTextHandler(io.Discard, nil))}}
	s.teleportPos.Store(&expected)
	c := newUnchangedControllable()
	pk := &v2193.PlayerAuthInputPacket{Position: vec64To32(entityNetworkPosition(c, expected))}

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
			pk := &v2193.PlayerAuthInputPacket{Position: [3]float32{float32(math.NaN()), float32(math.Inf(1)), float32(math.Inf(-1))}}
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

// TestMoveTooFarIsRefused moves a client farther than a player goes in a
// tick, as far as a float32 goes: the move is refused, and the client put
// back, rather than taking the player there, which froze its world as it
// looked for ground along the whole way.
func TestMoveTooFarIsRefused(t *testing.T) {
	start := mgl64.Vec3{0.5, 64, 0.5}
	for name, to := range map[string]mgl64.Vec3{
		"far below": {0.5, -1e37, 0.5},
		"far out":   {3e38, 64, 0.5},
		"11 blocks": {11.5, 64, 0.5},
	} {
		c := newUnchangedControllable()
		c.position = start
		moved := false
		c.move = func(mgl64.Vec3) { moved = true }
		s := &Session{conf: Config{Log: slog.New(slog.NewTextHandler(io.Discard, nil))}}
		pk := &v2193.PlayerAuthInputPacket{Position: vec64To32(entityNetworkPosition(c, to))}
		if err := (PlayerAuthInputHandler{}).handleMovement(pk, s, nil, c); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if moved {
			t.Errorf("%s: the player moved", name)
		}
	}
	c := newUnchangedControllable()
	c.position = start
	var delta mgl64.Vec3
	c.move = func(d mgl64.Vec3) { delta = d }
	s := &Session{conf: Config{Log: slog.New(slog.NewTextHandler(io.Discard, nil))}}
	pk := &v2193.PlayerAuthInputPacket{Position: vec64To32(entityNetworkPosition(c, start.Add(mgl64.Vec3{5, 0, 0})))}
	if err := (PlayerAuthInputHandler{}).handleMovement(pk, s, nil, c); err != nil || math.Abs(delta[0]-5) > 1e-3 {
		t.Fatalf("a move of 5 blocks: %v, moved by %v", err, delta)
	}
	if got := clampToWorld(mgl64.Vec3{-1e30, 1e30, 5}); got != (mgl64.Vec3{-3e7, 2e7, 5}) {
		t.Fatalf("clamped to %v", got)
	}
}

func TestFlightInputFlags(t *testing.T) {
	tests := []struct {
		name        string
		mode        world.GameMode
		startFlying bool
		stopFlying  bool
	}{
		{name: "spectator", mode: world.GameModeSpectator, startFlying: true},
		{name: "survival", mode: world.GameModeSurvival, stopFlying: true},
		{name: "creative", mode: world.GameModeCreative, startFlying: true, stopFlying: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c := newFlightControllable(test.mode)
			s := newFlightTestSession()
			h := PlayerAuthInputHandler{}

			h.handleInputFlags(inputFlags(v2193.InputFlagStartFlying), s, nil, c)
			if c.flying != test.startFlying {
				t.Fatalf("start flying = %v, want %v", c.flying, test.startFlying)
			}
			if !test.mode.AllowsFlying() {
				assertFlightAbilities(t, s.testPacket(), test.mode, false)
			}

			c.flying = true
			h.handleInputFlags(inputFlags(v2193.InputFlagStopFlying), s, nil, c)
			if c.flying != !test.stopFlying {
				t.Fatalf("stop flying = %v, want %v", c.flying, !test.stopFlying)
			}
			if test.mode == world.GameModeSpectator {
				for range 2 {
					s.testPacket()
				}
				assertFlightAbilities(t, s.testPacket(), test.mode, true)
			}
		})
	}
}

func inputFlags(flag int) v2193.InputFlags { return v2193.InputFlags{int32(flag)} }

func newFlightTestSession() *Session {
	return &Session{
		conf:        Config{Log: slog.New(slog.NewTextHandler(io.Discard, nil))},
		holdPackets: 1,
	}
}

func assertFlightAbilities(t *testing.T, pk proto.Packet, mode world.GameMode, flying bool) {
	t.Helper()
	abilities, ok := pk.(*v2193.UpdateAbilitiesPacket)
	if !ok {
		t.Fatalf("packet type = %T, want *v2193.UpdateAbilitiesPacket", pk)
	}
	values := abilities.AbilityData.Layers[0].Values
	if got := values&v2193.AbilityFlying != 0; got != flying {
		t.Fatalf("flying ability = %v, want %v", got, flying)
	}
	if got := values&v2193.AbilityNoClip != 0; got != !mode.HasCollision() {
		t.Fatalf("no-clip ability = %v, want %v", got, !mode.HasCollision())
	}
}

type flightControllable struct {
	Controllable
	handle *world.EntityHandle
	mode   world.GameMode
	flying bool
}

func newFlightControllable(mode world.GameMode) *flightControllable {
	return &flightControllable{
		handle: world.EntitySpawnOpts{}.New(offsetTestType{}, offsetTestConfig{}),
		mode:   mode,
	}
}

func (c *flightControllable) H() *world.EntityHandle   { return c.handle }
func (*flightControllable) Position() mgl64.Vec3       { return mgl64.Vec3{} }
func (*flightControllable) Rotation() cube.Rotation    { return cube.Rotation{} }
func (c *flightControllable) GameMode() world.GameMode { return c.mode }
func (*flightControllable) Sneaking() bool             { return false }
func (c *flightControllable) StartFlying() {
	if c.mode.AllowsFlying() {
		c.flying = true
	}
}
func (c *flightControllable) Flying() bool               { return c.flying }
func (c *flightControllable) StopFlying()                { c.flying = false }
func (*flightControllable) FlightSpeed() float64         { return 0.05 }
func (*flightControllable) VerticalFlightSpeed() float64 { return 1 }

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
func (*unchangedControllable) Gliding() bool                { return false }

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
