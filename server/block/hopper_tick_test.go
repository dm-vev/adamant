package block

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

func TestHopperTransferDoesNotLoadNeighbourChunks(t *testing.T) {
	settings := (world.NopProvider{}).Settings()
	settings.CurrentTick = 1 // World.New performs the initial tick, reaching 2.
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Provider: world.NopProvider{Set: settings}}.New()
	defer w.Close()
	<-w.Exec(func(tx *world.Tx) {
		if tx.CurrentTick() != 2 {
			t.Fatal("expected an even hopper transfer step")
		}
		pos := cube.Pos{15, 65, 8}
		h := NewHopper()
		h.Facing = cube.FaceEast
		below := NewHopper()
		below.Facing = cube.FaceEast
		tx.SetBlock(pos, h, nil)
		tx.SetBlock(pos.Side(cube.FaceDown), below, nil)
		if loaded, _ := tx.ChunkState(world.ChunkPos{1, 0}); loaded {
			t.Fatal("neighbour chunk was already loaded")
		}
		h.Tick(tx.CurrentTick(), pos, tx)
		if loaded, _ := tx.ChunkState(world.ChunkPos{1, 0}); loaded {
			t.Fatal("hopper transfer or recursive neighbour lookup loaded an absent chunk")
		}
	})
}

func TestHopperRecursiveTransferHonoursNativeDepthLimit(t *testing.T) {
	settings := (world.NopProvider{}).Settings()
	settings.CurrentTick = 1
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Provider: world.NopProvider{Set: settings}}.New()
	defer w.Close()
	<-w.Exec(func(tx *world.Tx) {
		if tx.CurrentTick() != 2 {
			t.Fatal("expected an even hopper transfer step")
		}
		head := cube.Pos{8, 100, 8}
		for depth := 0; depth < 52; depth++ {
			pos := head.Add(cube.Pos{0, -depth, 0})
			h := NewHopper()
			h.Facing = cube.FaceWest
			if err := h.inventory.SetItem(0, item.NewStack(item.Diamond{}, 1)); err != nil {
				t.Fatal(err)
			}
			tx.SetBlock(pos, h, nil)
			tx.SetBlock(pos.Side(cube.FaceWest), NewBarrel(), nil)
		}
		tx.Block(head).(Hopper).Tick(tx.CurrentTick(), head, tx)
		// Original HopperBlockActor::tick starts with a recursion budget of
		// 50. Depth zero still processes its own actor, giving 51 visits.
		for depth, want := range map[int]int{50: 1, 51: 0} {
			pos := head.Add(cube.Pos{0, -depth, 0}).Side(cube.FaceWest)
			stack, _ := tx.Block(pos).(Barrel).Inventory(tx, pos).Item(0)
			if stack.Count() != want {
				h := tx.Block(head.Add(cube.Pos{0, -depth, 0})).(Hopper)
				t.Errorf("depth %d destination count=%d, want %d; hopper tick=%d cooldown=%d inventory=%v", depth, stack.Count(), want, h.runtime.lastTransferTick, h.TransferCooldown, h.inventory.Items())
			}
		}
		total := 0
		for depth := 0; depth < 52; depth++ {
			pos := head.Add(cube.Pos{0, -depth, 0})
			for _, p := range []cube.Pos{pos, pos.Side(cube.FaceWest)} {
				for _, stack := range tx.Block(p).(Container).Inventory(tx, p).Slots() {
					total += stack.Count()
				}
			}
		}
		if total != 52 {
			t.Fatalf("recursive transfer lost or duplicated items: %d", total)
		}
	})
}
