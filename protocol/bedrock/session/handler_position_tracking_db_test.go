package session

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

func TestPositionTrackingDBHandlerMissingAndTracked(t *testing.T) {
	t.Parallel()
	w := world.Config{Synchronous: true}.New()
	defer w.Close()
	pos := cube.Pos{4, 65, 6}
	handle := w.TrackPosition(pos, 0)
	c := trackingControllable{inv: inventory.New(36, nil)}
	s := &Session{holdPackets: 1}
	h := &PositionTrackingDBHandler{}

	w.Do(func(tx *world.Tx) {
		if err := h.Handle(&mc.PositionTrackingDBClientRequestPacket{TrackingID: handle}, s, tx, c); err != nil {
			t.Fatal(err)
		}
		c.held = item.NewStack(item.Compass{TrackingHandle: handle}, 1)
		if err := h.Handle(&mc.PositionTrackingDBClientRequestPacket{TrackingID: handle}, s, tx, c); err != nil {
			t.Fatal(err)
		}
	})

	missing := (s.testPacket()).(*mc.PositionTrackingDBServerBroadcastPacket)
	if missing.BroadcastAction != mc.PositionTrackingDBBroadcastActionNotFound || missing.Payload["status"] != byte(positionTrackingStatusMissing) {
		t.Fatalf("missing response = %#v", missing)
	}
	tracked := (s.testPacket()).(*mc.PositionTrackingDBServerBroadcastPacket)
	if tracked.BroadcastAction != mc.PositionTrackingDBBroadcastActionUpdate || tracked.Payload["dim"] != int32(0) {
		t.Fatalf("tracked response = %#v", tracked)
	}
	if got := tracked.Payload["pos"]; !equalPositionPayload(got, pos) {
		t.Fatalf("tracked position = %#v, want %v", got, pos)
	}
}

func equalPositionPayload(value any, pos cube.Pos) bool {
	got, ok := value.([]int32)
	return ok && len(got) == 3 && got[0] == int32(pos[0]) && got[1] == int32(pos[1]) && got[2] == int32(pos[2])
}

type trackingControllable struct {
	Controllable
	held item.Stack
	inv  *inventory.Inventory
}

func (c trackingControllable) HeldItems() (item.Stack, item.Stack) { return c.held, item.Stack{} }
func (c trackingControllable) Inventory() *inventory.Inventory     { return c.inv }
