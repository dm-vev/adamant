package client

import (
	"net"
	"time"
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
