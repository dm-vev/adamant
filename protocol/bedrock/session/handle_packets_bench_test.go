package session_test

import (
	"log/slog"
	"testing"

	"github.com/df-mc/dragonfly/protocol/bedrock/session"
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/login"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
	"github.com/go-gl/mathgl/mgl32"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// benchConn is the connection of a client of a benchmark: it drops what
// the session writes, so that only the handling of packets is measured.
type benchConn struct{ *fuzzConn }

func (*benchConn) WritePacket(proto.Packet) error { return nil }

// BenchmarkSessionHandlePackets measures handlePacket on a recorded stream
// of what a client sends, with a real player in survival on a stone floor:
// four PlayerAuthInputs a tenth of a block east and four back, a click on
// the floor with the empty hand, and two item stack requests that swap the
// stacks of two slots and back. An op is the stream of eleven packets,
// which leaves the player and its inventory as it found them.
func BenchmarkSessionHandlePackets(b *testing.B) {
	log := slog.New(slog.DiscardHandler)
	w := world.Config{Log: log}.New()
	b.Cleanup(func() { _ = w.Close() })
	conn := &benchConn{&fuzzConn{identity: login.Identity{Identity: uuid.NewString(), DisplayName: "Bench", XUID: "2535400000000002"}, closed: make(chan struct{})}}
	sess := session.Config{Log: log, MaxChunkRadius: 1}.New(conn)
	b.Cleanup(sess.CloseConnection)
	start := mgl64.Vec3{0.5, 64, 0.5}
	cfg := player.Config{Client: sess, Position: start, UUID: uuid.MustParse(conn.identity.Identity), Name: conn.identity.DisplayName, Log: log, GameMode: world.GameModeSurvival}
	h := world.EntitySpawnOpts{Position: start, ID: cfg.UUID}.New(player.Type, cfg)
	sess.Bind(h, cfg.Skin)
	const held = 8
	<-w.Exec(func(tx *world.Tx) {
		for x := -4; x <= 4; x++ {
			for z := -4; z <= 4; z++ {
				tx.SetBlock(cube.Pos{x, 63, z}, block.Stone{}, nil)
			}
		}
		p := tx.AddEntity(h).(*player.Player)
		sess.Spawn(p, tx)
		if err := p.SetHeldSlot(held); err != nil {
			b.Error(err)
		}
		_ = p.Inventory().SetItem(0, item.NewStack(item.Apple{}, 16))
		_ = p.Inventory().SetItem(1, item.NewStack(block.Stone{}, 4))
	})
	if b.Failed() {
		return
	}

	<-w.Exec(func(tx *world.Tx) {
		e, _ := h.Entity(tx)
		p := e.(*player.Player)
		inv := p.Inventory()
		apples, _ := inv.Item(0)
		stone, _ := inv.Item(1)
		stream, renumber := recordedStream(start, session.StackNetworkID(apples), session.StackNetworkID(stone))
		pass := func() bool {
			renumber()
			for _, pk := range stream {
				if err := sess.HandlePacket(pk, tx, p); err != nil {
					b.Errorf("%T: %v", pk, err)
					return false
				}
			}
			return true
		}
		// The first pass checks that the player walks and the stacks swap.
		renumber()
		for i, pk := range stream {
			if err := sess.HandlePacket(pk, tx, p); err != nil {
				b.Errorf("%T: %v", pk, err)
				return
			}
			if want := start.Add(mgl64.Vec3{0.4, 0, 0}); i == 3 && p.Position().Sub(want).Len() > 1e-3 {
				b.Errorf("after four inputs the player is at %v, want %v", p.Position(), want)
				return
			}
			if got, _ := inv.Item(0); i == 9 && !got.Equal(stone) {
				b.Errorf("after the swap slot 0 holds %v, want %v", got, stone)
				return
			}
		}
		check := func() bool {
			if got, _ := inv.Item(0); !got.Equal(apples) || got.Count() != 16 {
				b.Errorf("after the stream slot 0 holds %v, want %v", got, apples)
				return false
			}
			if got := p.Position(); got.Sub(start).Len() > 1e-3 {
				b.Errorf("after the stream the player is at %v, want %v", got, start)
				return false
			}
			return true
		}
		if !check() {
			return
		}
		b.ReportAllocs()
		for b.Loop() {
			if !pass() {
				return
			}
		}
		check()
	})
}

// recordedStream returns the packets BenchmarkSessionHandlePackets sends,
// for a player standing at start with the stacks of the network IDs passed
// in slots 0 and 1, and a function that numbers the ticks of the inputs and
// the IDs of the requests of the next pass on.
func recordedStream(start mgl64.Vec3, a, b int32) ([]proto.Packet, func()) {
	const inv = mc.ContainerCombinedHotBarAndInventory
	eyes := start.Add(mgl64.Vec3{0, player.Type.NetworkOffset(), 0})
	var stream []proto.Packet
	var inputs []*mc.PlayerAuthInputPacket
	for i, x := range []float64{0.1, 0.2, 0.3, 0.4, 0.3, 0.2, 0.1, 0} {
		dx, yaw := float32(0.1), float32(-90)
		if i >= 4 {
			dx, yaw = -0.1, 90
		}
		pk := &mc.PlayerAuthInputPacket{
			Yaw: yaw, HeadYaw: yaw,
			Position:   [3]float32{float32(eyes[0] + x), float32(eyes[1]), float32(eyes[2])},
			MoveVector: [2]float32{0, 1},
			Delta:      [3]float32{dx, 0, 0},
			InputMode:  mc.InputModeMouse,
			PlayMode:   mc.PlayModeNormal,
		}
		inputs = append(inputs, pk)
		stream = append(stream, pk)
	}
	stream = append(stream, &mc.InventoryTransactionPacket{TransactionData: &mc.UseItemTransactionData{
		ActionType:       mc.UseItemActionClickBlock,
		TriggerType:      mc.TriggerTypePlayerInput,
		BlockPosition:    [3]int32{0, 63, 0},
		BlockFace:        int32(cube.FaceUp),
		HotBarSlot:       8,
		Position:         mgl32.Vec3{float32(eyes[0]), float32(eyes[1]), float32(eyes[2])},
		ClickedPosition:  mgl32.Vec3{0.5, 1, 0.5},
		ClientPrediction: mc.ClientPredictionSuccess,
	}})
	slot := func(i int, id int32) mc.StackRequestSlotInfo {
		return mc.StackRequestSlotInfo{Container: mc.FullContainerName{ContainerID: inv}, Slot: uint8(i), StackNetworkID: id}
	}
	swap := &mc.ItemStackRequestPacket{Requests: []mc.ItemStackRequest{{Actions: []mc.StackRequestAction{
		&mc.SwapStackRequestAction{Source: slot(0, a), Destination: slot(1, b)}}}}}
	back := &mc.ItemStackRequestPacket{Requests: []mc.ItemStackRequest{{Actions: []mc.StackRequestAction{
		&mc.SwapStackRequestAction{Source: slot(0, b), Destination: slot(1, a)}}}}}
	stream = append(stream, swap, back)

	var tick uint64
	request := int32(-1)
	return stream, func() {
		for _, pk := range inputs {
			tick++
			pk.Tick = tick
		}
		for _, pk := range []*mc.ItemStackRequestPacket{swap, back} {
			pk.Requests[0].RequestID = request
			request -= 2
		}
	}
}
