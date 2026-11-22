package block_test

import (
	"sync/atomic"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

type cancelRedstoneHandler struct {
	world.NopHandler
	cancel atomic.Bool
}

func (h *cancelRedstoneHandler) HandleRedstoneUpdate(ctx *world.Context, _ world.RedstoneUpdate) {
	if h.cancel.Load() {
		ctx.Cancel()
	}
}

func advanceTicks(w *world.World, n int) {
	for range n {
		w.AdvanceTick()
	}
}

func TestContainerCircuitPowerSourcesAndQuiescentWakeup(t *testing.T) {
	t.Parallel()
	w := world.Config{Synchronous: true}.New()
	defer w.Close()

	// Layout:
	// 1. Direct source: Hopper at (0,64,0), Lever placed later at (1,64,0).
	// 2. Strong conducted power: Dispenser at (10,64,0), Stone at (11,64,0), Repeater at (12,64,0) facing West, RedstoneBlock placed later at (13,64,0).
	// 3. Weak conducted power: Hopper at (20,64,0), Stone at (21,64,0), Wire at (21,65,0), RedstoneBlock placed later at (22,65,0).
	// 4. Non-conductive glass: Hopper at (30,64,0), Glass at (31,64,0), RedstoneBlock at (32,64,0) -> must stay unpowered!
	// 5. Cross-chunk wake-up: Hopper at (16,64,32) [chunk 1,2], Wire at (15..1,64,32) [chunk 0,2], Stone at (0,64,32), RedstoneBlock placed later at (-1,64,32) [chunk -1,2] via Repeater at (0,64,32).
	// 6. TrappedChest transient ScheduleUpdate (no SetBlock): Hopper at (0,64,64), TrappedChest at (1,64,64).
	// 7. RedstoneLamp settled lit -> 3-step turn-off countdown when power removed: Lamp at (0,64,96), RedstoneBlock at (1,64,96).
	hopperDirect := cube.Pos{0, 64, 0}
	dispenserStrong := cube.Pos{10, 64, 0}
	hopperWeak := cube.Pos{20, 64, 0}
	hopperGlass := cube.Pos{30, 64, 0}
	hopperCross := cube.Pos{16, 64, 32}
	hopperTrapped := cube.Pos{0, 64, 64}
	trappedPos := cube.Pos{1, 64, 64}
	lampPos := cube.Pos{0, 64, 96}

	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(hopperDirect, block.NewHopper(), nil)

		tx.SetBlock(dispenserStrong, block.NewDispenser(), nil)
		tx.SetBlock(cube.Pos{11, 64, 0}, block.Stone{}, nil)
		tx.SetBlock(cube.Pos{12, 63, 0}, block.Stone{}, nil)
		tx.SetBlock(cube.Pos{12, 64, 0}, block.RedstoneRepeater{Facing: cube.East}, nil)

		tx.SetBlock(hopperWeak, block.NewHopper(), nil)
		tx.SetBlock(cube.Pos{21, 64, 0}, block.Stone{}, nil)
		tx.SetBlock(cube.Pos{21, 65, 0}, block.RedstoneWire{}, nil)

		tx.SetBlock(hopperGlass, block.NewHopper(), nil)
		tx.SetBlock(cube.Pos{31, 64, 0}, block.Glass{}, nil)
		tx.SetBlock(cube.Pos{32, 64, 0}, block.RedstoneBlock{}, nil)

		tx.SetBlock(hopperCross, block.NewHopper(), nil)
		for x := 2; x <= 15; x++ {
			tx.SetBlock(cube.Pos{x, 63, 32}, block.Stone{}, nil)
			tx.SetBlock(cube.Pos{x, 64, 32}, block.RedstoneWire{}, nil)
		}
		tx.SetBlock(cube.Pos{1, 63, 32}, block.Stone{}, nil)
		tx.SetBlock(cube.Pos{1, 64, 32}, block.RedstoneRepeater{Facing: cube.West}, nil)
		tx.SetBlock(cube.Pos{0, 63, 32}, block.Stone{}, nil)
		tx.SetBlock(cube.Pos{0, 64, 32}, block.RedstoneWire{}, nil)

		tx.SetBlock(hopperTrapped, block.NewHopper(), nil)
		tx.SetBlock(trappedPos, block.NewTrappedChest(), nil)

		tx.SetBlock(lampPos, block.RedstoneLamp{}, nil)
		tx.SetBlock(cube.Pos{1, 64, 96}, block.RedstoneBlock{}, nil)
	})

	// Let every chunk settle well past lateCircuitSettleTicks (20 ticks = 10 even ticks).
	advanceTicks(w, 20)

	<-w.Exec(func(tx *world.Tx) {
		if tx.Block(hopperDirect).(block.Hopper).Powered {
			t.Fatal("idle direct hopper unexpectedly powered")
		}
		if tx.Block(hopperGlass).(block.Hopper).Powered {
			t.Fatal("hopper behind glass unexpectedly powered")
		}
		if !tx.Block(lampPos).(block.RedstoneLamp).Lit {
			t.Fatal("lamp next to redstone block was not lit")
		}

		// Now trigger all wake-ups from settled state!
		tx.SetBlock(cube.Pos{1, 64, 0}, block.RedstoneBlock{}, nil)
		tx.SetBlock(cube.Pos{13, 64, 0}, block.RedstoneBlock{}, nil)
		tx.SetBlock(cube.Pos{22, 65, 0}, block.RedstoneBlock{}, nil)
		tx.SetBlock(cube.Pos{-1, 64, 32}, block.RedstoneBlock{}, nil)
		tx.Block(trappedPos).(block.TrappedChest).AddViewer(block.MobContainerOpener{Viewer: world.NopViewer{}}, tx, trappedPos)
		tx.SetBlock(cube.Pos{1, 64, 96}, block.Air{}, nil)
	})

	advanceTicks(w, 12)

	<-w.Exec(func(tx *world.Tx) {
		if !tx.Block(hopperDirect).(block.Hopper).Powered {
			t.Fatal("settled direct hopper did not wake up when redstone block placed")
		}
		if !tx.Block(dispenserStrong).(block.Dispenser).Triggered {
			t.Fatal("settled dispenser did not wake up on strong conducted power")
		}
		if !tx.Block(hopperWeak).(block.Hopper).Powered {
			t.Fatal("settled hopper did not wake up on weak conducted wire power")
		}
		if tx.Block(hopperGlass).(block.Hopper).Powered {
			t.Fatal("hopper behind glass should remain unpowered")
		}
		if !tx.Block(hopperCross).(block.Hopper).Powered {
			t.Fatal("settled cross-chunk hopper did not wake up from source two chunks away via wire/repeater")
		}
		if !tx.Block(hopperTrapped).(block.Hopper).Powered {
			t.Fatal("settled hopper did not wake up when adjacent trapped chest opened (ScheduleUpdate only)")
		}
		if tx.Block(lampPos).(block.RedstoneLamp).Lit {
			t.Fatal("settled lit lamp did not turn off after power was removed")
		}

		// Close trapped chest and verify deactivation from settled state too.
		tx.Block(trappedPos).(block.TrappedChest).RemoveViewer(block.MobContainerOpener{Viewer: world.NopViewer{}}, tx, trappedPos)
	})

	advanceTicks(w, 8)

	<-w.Exec(func(tx *world.Tx) {
		if tx.Block(hopperTrapped).(block.Hopper).Powered {
			t.Fatal("hopper did not unpower when trapped chest closed")
		}
	})
}

func TestContainerCircuitCancelledPublicationRetries(t *testing.T) {
	t.Parallel()
	h := &cancelRedstoneHandler{}
	w := world.Config{Synchronous: true}.New()
	defer w.Close()
	w.Handle(h)

	pos := cube.Pos{0, 64, 0}
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(pos, block.NewHopper(), nil)
	})
	advanceTicks(w, 16)

	// Cancel all redstone publications while powering the hopper for 12 ticks (well past settle threshold).
	h.cancel.Store(true)
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{1, 64, 0}, block.RedstoneBlock{}, nil)
	})
	advanceTicks(w, 12)
	<-w.Exec(func(tx *world.Tx) {
		if tx.Block(pos).(block.Hopper).Powered {
			t.Fatal("hopper powered while redstone updates were cancelled")
		}
	})

	// Re-allow publications without touching any block: the hopper must still publish!
	h.cancel.Store(false)
	advanceTicks(w, 6)
	<-w.Exec(func(tx *world.Tx) {
		if !tx.Block(pos).(block.Hopper).Powered {
			t.Fatal("hopper failed to retry publication after cancellation ended")
		}
	})
}

func BenchmarkContainerCircuitCacheAfterSourcesIdle(b *testing.B) {
	w := world.Config{Synchronous: true}.New()
	defer w.Close()

	positions := make([]cube.Pos, 0, 100)
	states := make([]world.RedstoneCircuitState, 0, 100)
	<-w.Exec(func(tx *world.Tx) {
		for i := range 100 {
			x := (i % 10) * 3
			z := (i / 10) * 3
			pos := cube.Pos{x, 64, z}
			// Typical town storage room: stone floor under container, air on other sides.
			tx.SetBlock(cube.Pos{x, 63, z}, block.Stone{}, nil)
			var blk world.RedstoneCircuitComponent
			switch i % 4 {
			case 0:
				h := block.NewHopper()
				tx.SetBlock(pos, h, nil)
				blk = h
			case 1:
				d := block.NewDispenser()
				tx.SetBlock(pos, d, nil)
				blk = d
			case 2:
				d := block.NewDropper()
				tx.SetBlock(pos, d, nil)
				blk = d
			default:
				n := block.Note{}
				tx.SetBlock(pos, n, nil)
				blk = n
			}
			positions = append(positions, pos)
			states = append(states, blk.RedstoneCircuit())
		}
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			for i, pos := range positions {
				states[i] = states[i].(world.RedstoneCircuitLateInput).CacheAfterSources(pos, tx)
			}
		}
	})
}

func BenchmarkWorldTickIdleContainersTown200(b *testing.B) {
	w := world.Config{Synchronous: true}.New()
	defer w.Close()

	<-w.Exec(func(tx *world.Tx) {
		for i := range 200 {
			x := (i % 20) * 4
			z := (i / 20) * 4
			tx.SetBlock(cube.Pos{x, 63, z}, block.Stone{}, nil)
			pos := cube.Pos{x, 64, z}
			switch i % 5 {
			case 0:
				tx.SetBlock(pos, block.NewHopper(), nil)
			case 1:
				tx.SetBlock(pos, block.NewDispenser(), nil)
			case 2:
				tx.SetBlock(pos, block.NewDropper(), nil)
			case 3:
				tx.SetBlock(pos, block.Note{}, nil)
			default:
				tx.SetBlock(pos, block.NewCrafter(), nil)
			}
		}
	})
	// Warm up past initial settling ticks.
	for range 12 {
		w.AdvanceTick()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		w.AdvanceTick()
	}
}
