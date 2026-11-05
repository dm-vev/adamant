package client

import (
	"net"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/go-gl/mathgl/mgl64"
)

// Move is what a client sent of one of its ticks: where it moved its player
// and what it held down while it did. A field the protocol of the client
// lacks is its zero value. Only Position and Rotation move the player; the
// rest tells the game how the client says it moved, which the game never
// takes for its own state.
type Move struct {
	// Position is where the feet of the player are, clamped to the world.
	Position mgl64.Vec3
	// Rotation is where the player looks, and HeadYaw where its head
	// turns.
	Rotation cube.Rotation
	HeadYaw  float64
	// Aim is the direction the client interacts in, such as the ray of a
	// tap on a touch screen; zero if it is the look.
	Aim mgl64.Vec3
	// Camera is the direction the camera of the client looks in, a unit
	// vector, which differs from the look in third person; zero if the
	// client did not say.
	Camera mgl64.Vec3
	// Tick is the tick of the client the move is of, 0 if unknown.
	Tick uint64
	// Arrived is when the move reached the server, before it waited for a
	// tick of the world; zero if unknown.
	Arrived time.Time
	// Input is the movement input held: strafe, forward.
	Input mgl64.Vec2
	// ClientVelocity is the velocity the client predicts for its next
	// move.
	ClientVelocity mgl64.Vec3
	// Sprinting, Sneaking, Jumping, Swimming, Crawling, Gliding and
	// SpinAttack are what the client holds or starts in the move.
	Sprinting, Sneaking, Jumping, Swimming, Crawling, Gliding, SpinAttack bool
	// ClaimsGround is set if the client says its player collided below.
	ClaimsGround bool
	// Riding is set if the player steers what it rides: the server moves
	// both, and the move only turns the player.
	Riding bool
	// AwaitingTeleport is set while the client has not acknowledged a
	// teleport: the position is stale, and the move is not applied.
	AwaitingTeleport bool
	// Acknowledged is set if this move acknowledged a teleport.
	Acknowledged bool
	// Loading is set while the client shows a loading screen, as it
	// changes world.
	Loading bool
}

// MoveResult is what the game made of a Move.
type MoveResult uint8

const (
	// MoveApplied is a move the player made.
	MoveApplied MoveResult = iota
	// MoveIgnored is a move with nothing to apply: one awaiting a teleport,
	// of a player that rides, is dead or immobile, or that changed nothing.
	MoveIgnored
	// MoveRefused is a move the game refused: the client was told where its
	// player is, with Self.CorrectMove.
	MoveRefused
	// MoveDeferred is a move of a client ahead of time: the client handles
	// it again, with what came after it, in a later tick.
	MoveDeferred
)

// Profile is what a client told of the device it runs on when it joined.
// Fields it did not tell are empty.
type Profile struct {
	// DeviceID identifies the device, as its platform does.
	DeviceID string
	// DeviceModel is the model of the device.
	DeviceModel string
	// SelfSignedID is an ID the client made for itself, which stays the same
	// across its logins.
	SelfSignedID string
}

// NetStats is the state of the connection of a client, as a netstat shows
// it.
type NetStats struct {
	// Addr is the address of the client.
	Addr net.Addr
	// Latency is half the round trip time.
	Latency time.Duration
	// Queued counts what waits to be sent to the client, Inbox what the
	// client sent that waits for the world of its player.
	Queued, Inbox int
	// Transport describes the transport of the connection, one line each,
	// in the words of the adapter of its protocol; empty if it tells nothing.
	Transport []string
}

// Diagnostics are the latest diagnostics a client sent of how it runs.
// Times are in milliseconds.
type Diagnostics struct {
	// AverageFramesPerSecond is the average amount of frames per second that the client has been
	// running at.
	AverageFramesPerSecond float64
	// AverageServerSimTickTime is the average time that the server spends simulating a single tick
	// in milliseconds.
	AverageServerSimTickTime float64
	// AverageClientSimTickTime is the average time that the client spends simulating a single tick
	// in milliseconds.
	AverageClientSimTickTime float64
	// AverageBeginFrameTime is the average time that the client spends beginning a frame in
	// milliseconds.
	AverageBeginFrameTime float64
	// AverageInputTime is the average time that the client spends processing input in milliseconds.
	AverageInputTime float64
	// AverageRenderTime is the average time that the client spends rendering in milliseconds.
	AverageRenderTime float64
	// AverageEndFrameTime is the average time that the client spends ending a frame in milliseconds.
	AverageEndFrameTime float64
	// AverageRemainderTimePercent is the average percentage of time that the client spends on
	// tasks that are not accounted for.
	AverageRemainderTimePercent float64
	// AverageUnaccountedTimePercent is the average percentage of time that the client spends on
	// unaccounted tasks.
	AverageUnaccountedTimePercent float64
}
