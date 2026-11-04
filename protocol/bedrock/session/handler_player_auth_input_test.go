package session

import (
	"io"
	"log/slog"
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/world"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/go-gl/mathgl/mgl64"
)

func TestTeleportAcknowledgementClearedForUnchangedMovement(t *testing.T) {
	t.Parallel()
	expected := mgl64.Vec3{}
	s := &Session{conf: Config{Log: slog.New(slog.NewTextHandler(io.Discard, nil))}}
	s.teleportPos.Store(&expected)
	c := newUnchangedControllable()
	pk := &mc.PlayerAuthInputPacket{Position: vec64To32(entityNetworkPosition(c, expected))}

	if err := (PlayerAuthInputHandler{}).handleMovement(pk, s, nil, c); err != nil {
		t.Fatalf("handle movement: %v", err)
	}
	if s.teleportPos.Load() != nil {
		t.Fatal("unchanged teleport acknowledgement left teleport pending")
	}
	if len(c.moves) != 1 || !c.moves[0].Acknowledged || c.moves[0].AwaitingTeleport {
		t.Fatalf("the game was handed %+v, want one move that acknowledged the teleport", c.moves)
	}
}

func TestTeleportAcknowledgedAtLargeCoordinates(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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

			s := &Session{conf: Config{Log: slog.New(slog.NewTextHandler(io.Discard, nil))}}
			s.teleportPos.Store(&pos)
			pk := &mc.PlayerAuthInputPacket{Position: [3]float32{float32(math.NaN()), float32(math.Inf(1)), float32(math.Inf(-1))}}
			if err := (PlayerAuthInputHandler{}).handleMovement(pk, s, nil, c); err != nil {
				t.Fatalf("handle movement: %v", err)
			}

			want := vec32To64(entityBasePosition(c, vec64To32(entityNetworkPosition(c, pos))))
			if len(c.moves) != 1 {
				t.Fatalf("the game was handed %d moves, want 1", len(c.moves))
			}
			if got := c.moves[0].Position; got != want {
				t.Fatalf("fallback position = %v, want %v", got, want)
			}
			if got := c.moves[0].Position[1]; got != pos[1] {
				t.Fatalf("fallback changed base Y to %v", got)
			}
			if s.teleportPos.Load() != nil {
				t.Fatal("malformed teleport acknowledgement left teleport pending")
			}
		})
	}
}

// TestAuthInputIsClampedToTheWorld moves a client as far as a float32
// goes: the session hands the game the move clamped to the world, as Java
// clamps it, and the game refuses it (TestMoveTooFarIsRefused of package
// player).
func TestAuthInputIsClampedToTheWorld(t *testing.T) {
	t.Parallel()
	start := mgl64.Vec3{0.5, 64, 0.5}
	for name, tc := range map[string]struct{ to, want mgl64.Vec3 }{
		"far below": {mgl64.Vec3{0.5, -1e37, 0.5}, mgl64.Vec3{0.5, -2e7, 0.5}},
		"far out":   {mgl64.Vec3{3e38, 64, 0.5}, mgl64.Vec3{3e7, 64, 0.5}},
		"5 blocks":  {mgl64.Vec3{5.5, 64, 0.5}, mgl64.Vec3{5.5, 64, 0.5}},
	} {
		c := newUnchangedControllable()
		c.position = start
		s := &Session{conf: Config{Log: slog.New(slog.NewTextHandler(io.Discard, nil))}}
		pk := &mc.PlayerAuthInputPacket{Position: vec64To32(entityNetworkPosition(c, tc.to)), Tick: 7}
		if err := (PlayerAuthInputHandler{}).handleMovement(pk, s, nil, c); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(c.moves) != 1 || !c.moves[0].Position.ApproxEqualThreshold(tc.want, 1e-3*max(1, tc.want.Len())) || c.moves[0].Tick != 7 {
			t.Errorf("%s: the game was handed %+v, want a move to %v of tick 7", name, c.moves, tc.want)
		}
	}
	if got := clampToWorld(mgl64.Vec3{-1e30, 1e30, 5}); got != (mgl64.Vec3{-3e7, 2e7, 5}) {
		t.Fatalf("clamped to %v", got)
	}
}

// TestAuthInputAwaitingTeleport checks that a move before the client
// acknowledged a teleport is handed to the game as awaiting it, and does
// not steer what the player rides.
func TestAuthInputAwaitingTeleport(t *testing.T) {
	t.Parallel()
	c := newUnchangedControllable()
	expected := mgl64.Vec3{10, 64, 10}
	s := &Session{conf: Config{Log: slog.New(slog.NewTextHandler(io.Discard, nil))}}
	s.teleportPos.Store(&expected)
	pk := &mc.PlayerAuthInputPacket{Position: vec64To32(entityNetworkPosition(c, mgl64.Vec3{1, 0, 0}))}
	if err := (PlayerAuthInputHandler{}).handleMovement(pk, s, nil, c); err != nil {
		t.Fatal(err)
	}
	if len(c.moves) != 1 || !c.moves[0].AwaitingTeleport || c.steered {
		t.Fatalf("the game was handed %+v, steered %v; want one move awaiting the teleport, no steering", c.moves, c.steered)
	}
	if s.teleportPos.Load() == nil {
		t.Fatal("the teleport is no longer awaited")
	}
}

// TestFlightInputFlags checks that the session asks the game to start and
// stop flying as its client does: the game refuses what the game mode does
// not allow (TestClientFlightIsRefused of package player).
func TestFlightInputFlags(t *testing.T) {
	t.Parallel()
	c := newFlightControllable(world.GameModeSurvival)
	s := newFlightTestSession()
	h := PlayerAuthInputHandler{}
	h.handleInputFlags(inputFlags(mc.InputFlagStartFlying), s, nil, c)
	if !c.askedToFly {
		t.Fatal("the session did not ask the game to start flying")
	}
	c.flying = true
	h.handleInputFlags(inputFlags(mc.InputFlagStopFlying), s, nil, c)
	if c.flying {
		t.Fatal("the session did not ask the game to stop flying")
	}
}

func inputFlags(flag int) mc.InputFlags { return mc.InputFlags{int32(flag)} }

func newFlightTestSession() *Session {
	return &Session{
		conf:        Config{Log: slog.New(slog.NewTextHandler(io.Discard, nil))},
		holdPackets: 1,
	}
}

type flightControllable struct {
	Controllable
	handle     *world.EntityHandle
	mode       world.GameMode
	flying     bool
	askedToFly bool
}

func newFlightControllable(mode world.GameMode) *flightControllable {
	return &flightControllable{
		handle: world.EntitySpawnOpts{}.New(offsetTestType{}, offsetTestConfig{}),
		mode:   mode,
	}
}

func (c *flightControllable) H() *world.EntityHandle     { return c.handle }
func (*flightControllable) Position() mgl64.Vec3         { return mgl64.Vec3{} }
func (*flightControllable) Rotation() cube.Rotation      { return cube.Rotation{} }
func (c *flightControllable) GameMode() world.GameMode   { return c.mode }
func (*flightControllable) Sneaking() bool               { return false }
func (c *flightControllable) StartFlying()               { c.askedToFly = true }
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
	// moves are the moves the game was handed, steered whether the session
	// steered what the controllable rides.
	moves   []client.Move
	steered bool
	// result is what ClientMove returns of a move it applies.
	result client.MoveResult
}

func newUnchangedControllable() *unchangedControllable {
	return &unchangedControllable{handle: world.EntitySpawnOpts{}.New(offsetTestType{}, offsetTestConfig{})}
}

func (c *unchangedControllable) H() *world.EntityHandle { return c.handle }
func (c *unchangedControllable) Position() mgl64.Vec3   { return c.position }
func (*unchangedControllable) Rotation() cube.Rotation {
	return cube.Rotation{}
}

// ClientMove records the move and, unless it awaits a teleport, passes its
// delta to move.
func (c *unchangedControllable) ClientMove(m client.Move) client.MoveResult {
	c.moves = append(c.moves, m)
	if m.AwaitingTeleport {
		return client.MoveIgnored
	}
	if c.result != client.MoveApplied {
		return c.result
	}
	if c.move != nil {
		c.move(m.Position.Sub(c.position))
	}
	return client.MoveApplied
}
func (c *unchangedControllable) Sleeping() (cube.Pos, bool) { return cube.Pos{}, c.sleeping }
func (*unchangedControllable) Gliding() bool                { return false }

// SteerVehicle reports that the controllable rides nothing.
func (c *unchangedControllable) SteerVehicle(float64, float64, bool) bool {
	c.steered = true
	return false
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
