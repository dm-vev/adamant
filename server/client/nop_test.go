package client_test

import (
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/player/hud"
	"github.com/df-mc/dragonfly/server/player/input"
	"github.com/df-mc/dragonfly/server/world"
)

// Nop answers as a client that is not there: zero values, but for the time
// of its last activity, which a player without a client always reported as
// the Unix epoch.
func TestNopAnswersAsNoClient(t *testing.T) {
	var c client.Client = client.Nop{}
	if c.Profile() != (client.Profile{}) {
		t.Errorf("Profile() = %+v, want zero", c.Profile())
	}
	if c.Addr() != nil {
		t.Errorf("Addr() = %v, want nil", c.Addr())
	}
	if c.Latency() != 0 || c.ChunkRadius() != 0 || c.ClientMoving() {
		t.Errorf("Latency, ChunkRadius, ClientMoving = %v, %v, %v; want zero", c.Latency(), c.ChunkRadius(), c.ClientMoving())
	}
	if s := c.NetStats(); s.Addr != nil || s.Latency != 0 || s.Queued != 0 || s.Inbox != 0 || s.Transport != nil {
		t.Errorf("NetStats() = %+v, want zero", s)
	}
	if !c.LastActivity().Equal(time.Unix(0, 0)) {
		t.Errorf("LastActivity() = %v, want the Unix epoch", c.LastActivity())
	}
	c.HideHudElement(hud.Health())
	if c.HudElementHidden(hud.Health()) {
		t.Error("HudElementHidden after HideHudElement, want false")
	}
	c.LockInput(input.Camera())
	if c.InputLocked(input.Camera()) {
		t.Error("InputLocked after LockInput, want false")
	}
	if shapes := c.VisibleDebugShapes(); len(shapes) != 0 {
		t.Errorf("VisibleDebugShapes() = %v, want none", shapes)
	}
	c.ViewRecipesUnlocked([]string{"minecraft:stick"})
	if c.ViewLayer() != nil {
		t.Error("ViewLayer() is not nil")
	}
	if err := c.ViewMapData(world.MapData{Dimension: -1}); err != nil {
		t.Errorf("ViewMapData() = %v, want nil", err)
	}
	// Close must not touch the world: a player without a client removes
	// itself.
	c.Close(nil)
}

// Returning Nop as a Client must not allocate: a player without a client
// does it for every call it makes to its client.
func TestNopAsClientDoesNotAllocate(t *testing.T) {
	var c client.Client
	if n := testing.AllocsPerRun(100, func() { c = client.Nop{} }); n != 0 {
		t.Errorf("converting Nop to Client allocates %v times, want 0", n)
	}
	_ = c
}
