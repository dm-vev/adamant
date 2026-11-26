package world

import (
	"testing"

	"github.com/dm-vev/adamant/server/block/cube"
)

var redstoneDirtyTickBenchmarkPower int

func BenchmarkRedstoneDirtyTickLongLineWithClocks(b *testing.B) {
	const lineLength = 96

	w := Config{Synchronous: true, Blocks: redstoneSignalLossTestRegistry()}.New()
	defer w.Close()

	runWorld(w, func(tx *Tx) {
		clockA := cube.Pos{-1, 64, 0}
		clockB := cube.Pos{lineLength / 2, 64, 1}
		line := make([]cube.Pos, lineLength)
		for x := range lineLength {
			line[x] = cube.Pos{x, 64, 0}
			tx.SetBlock(line[x], redstoneLossRelayer{}, nil)
		}
		tx.SetBlock(clockA, redstoneLossSource{Power: 15}, nil)
		tx.SetBlock(clockB, redstoneLossSource{Power: 15}, nil)
		tx.SetBlock(cube.Pos{lineLength, 64, 0}, redstoneLossConsumer{}, nil)
		tx.World().redstone.tick(tx, 0)

		b.ReportAllocs()
		b.ResetTimer()
		for tick := range b.N {
			tx.World().redstone.invalidateAround(clockA, clockA, RedstoneUpdateCauseBlockUpdate, tx.Range())
			tx.World().redstone.invalidateAround(clockB, clockB, RedstoneUpdateCauseBlockUpdate, tx.Range())
			tx.World().redstone.tick(tx, int64(tick+1))
		}
		b.StopTimer()

		if consumer, ok := tx.Block(cube.Pos{lineLength, 64, 0}).(redstoneLossConsumer); ok {
			redstoneDirtyTickBenchmarkPower = consumer.Power
		}
	})
}

type benchLateCircuitBlock struct{}

func (benchLateCircuitBlock) EncodeBlock() (string, map[string]any) {
	return "test:bench_late_circuit", nil
}
func (benchLateCircuitBlock) Hash() (uint64, uint64)                { return 0xBEEF1234, 0xCAFE5678 }
func (benchLateCircuitBlock) Model() BlockModel                     { return unknownModel{} }
func (benchLateCircuitBlock) RedstoneCircuit() RedstoneCircuitState { return benchLateCircuitState{} }
func (benchLateCircuitBlock) RedstoneCircuitOutput(b Block) Block   { return b }

type benchLateCircuitState struct {
	input, prev int
}

func (s benchLateCircuitState) Cache(cube.Pos, *Tx) RedstoneCircuitState { return s }
func (s benchLateCircuitState) CacheAfterSources(pos cube.Pos, tx *Tx) RedstoneCircuitState {
	s.prev, s.input = s.input, 0
	for _, face := range cube.Faces() {
		s.input = max(s.input, tx.Redstone().PowerFrom(pos, face))
	}
	return s
}
func (s benchLateCircuitState) Evaluate() (RedstoneCircuitState, Block) { return s, nil }
func (s benchLateCircuitState) CircuitInputChange() (int, int)          { return s.prev, s.input }
func (s benchLateCircuitState) Refresh(_, after Block) RedstoneCircuitState {
	if _, ok := after.(benchLateCircuitBlock); !ok {
		return nil
	}
	return s
}

func BenchmarkRedstoneTickQuiescentLateCircuits200(b *testing.B) {
	reg := NewBlockRegistry()
	reg.RegisterBlockState(BlockState{Name: "test:bench_late_circuit", Properties: map[string]any{}})
	reg.RegisterBlock(benchLateCircuitBlock{})
	w := Config{Synchronous: true, Blocks: reg}.New()
	defer w.Close()

	runWorld(w, func(tx *Tx) {
		for i := range 200 {
			x := (i % 20) * 4
			z := (i / 20) * 4
			tx.SetBlock(cube.Pos{x, 64, z}, benchLateCircuitBlock{}, nil)
		}
		for tick := range int64(12) {
			tx.World().redstone.tick(tx, tick)
		}
		b.ReportAllocs()
		b.ResetTimer()
		for tick := range b.N {
			tx.World().redstone.tick(tx, int64(12+tick))
		}
	})
}

type countingLateBlock struct {
	calls *int
}

func (countingLateBlock) EncodeBlock() (string, map[string]any) {
	return "test:counting_late", nil
}
func (countingLateBlock) Hash() (uint64, uint64) { return 0xABCD1234, 0x1234ABCD }
func (countingLateBlock) Model() BlockModel      { return unknownModel{} }
func (b countingLateBlock) RedstoneCircuit() RedstoneCircuitState {
	return countingLateState(b)
}
func (countingLateBlock) RedstoneCircuitOutput(out Block) Block { return out }

type countingLateState struct {
	calls *int
}

func (s countingLateState) Cache(cube.Pos, *Tx) RedstoneCircuitState { return s }
func (s countingLateState) CacheAfterSources(cube.Pos, *Tx) RedstoneCircuitState {
	if s.calls != nil {
		*s.calls++
	}
	return s
}
func (s countingLateState) Evaluate() (RedstoneCircuitState, Block) { return s, nil }
func (s countingLateState) Refresh(_, after Block) RedstoneCircuitState {
	if b, ok := after.(countingLateBlock); ok {
		s.calls = b.calls
		return s
	}
	return nil
}

func TestQuiescentLateCircuitSkipAndWakeup(t *testing.T) {
	t.Parallel()
	var calls int
	blk := countingLateBlock{calls: &calls}
	reg := NewBlockRegistry()
	reg.RegisterBlockState(BlockState{Name: "test:counting_late", Properties: map[string]any{}})
	reg.RegisterBlock(blk)
	reg.RegisterBlockState(BlockState{Name: "test:redstone_loss_source", Properties: map[string]any{"power": int32(15)}})
	reg.RegisterBlock(redstoneLossSource{Power: 15})
	w := Config{Synchronous: true, Blocks: reg}.New()
	defer w.Close()

	runWorld(w, func(tx *Tx) {
		tx.SetBlock(cube.Pos{8, 64, 8}, blk, nil)
		// Run 12 ticks (6 even ticks): component should settle after 4 quiescent even ticks.
		for tick := range int64(12) {
			tx.World().redstone.tick(tx, tick)
		}
		settledCalls := calls
		for tick := int64(12); tick < 22; tick++ {
			tx.World().redstone.tick(tx, tick)
		}
		if calls != settledCalls {
			t.Fatalf("quiescent late circuit kept running CacheAfterSources: got %d -> %d calls", settledCalls, calls)
		}

		// A block change 2 chunks away (outside 3x3 neighborhood) must NOT wake it up.
		tx.SetBlock(cube.Pos{40, 64, 8}, redstoneLossSource{Power: 15}, &SetOpts{DisableRedstoneUpdates: true})
		for tick := int64(22); tick < 26; tick++ {
			tx.World().redstone.tick(tx, tick)
		}
		if calls != settledCalls {
			t.Fatalf("distant block change outside 3x3 woke settled late circuit: %d -> %d", settledCalls, calls)
		}

		// A SetBlock with DisableRedstoneUpdates in adjacent chunk (1,0) MUST wake it up on the next even tick.
		tx.SetBlock(cube.Pos{16, 64, 8}, redstoneLossSource{Power: 15}, &SetOpts{DisableRedstoneUpdates: true})
		tx.World().redstone.tick(tx, 26)
		if calls <= settledCalls {
			t.Fatalf("SetBlock(DisableRedstoneUpdates) in neighbor chunk did not wake settled late circuit")
		}

		// Re-settle, then verify transient ScheduleUpdate (without SetBlock) wakes it up.
		for tick := int64(27); tick < 40; tick++ {
			tx.World().redstone.tick(tx, tick)
		}
		beforeSchedule := calls
		tx.World().redstone.tick(tx, 40)
		if calls != beforeSchedule {
			t.Fatalf("late circuit did not re-settle: %d -> %d", beforeSchedule, calls)
		}
		tx.Redstone().ScheduleUpdate(cube.Pos{16, 64, 8})
		tx.World().redstone.tick(tx, 41)
		tx.World().redstone.tick(tx, 42)
		if calls <= beforeSchedule {
			t.Fatalf("ScheduleUpdate in neighbor chunk did not wake settled late circuit")
		}
	})
}
