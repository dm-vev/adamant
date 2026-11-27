package block

import (
	"math/rand/v2"
	"sync/atomic"
	"testing"

	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/cmd"
	"github.com/dm-vev/adamant/server/world"
)

var commandBlockExecutionCount atomic.Int32

type commandBlockTestCommand struct{}

func (commandBlockTestCommand) Run(_ cmd.Source, out *cmd.Output, tx *world.Tx) {
	commandBlockExecutionCount.Add(1)
	tx.SetBlock(cube.Pos{1, 64, 1}, Stone{}, nil)
	out.Print("set")
}

type commandBlockTestFailure struct{}

func (commandBlockTestFailure) Run(_ cmd.Source, out *cmd.Output, _ *world.Tx) { out.Error("failed") }

func registerCommandBlockTestCommands() {
	cmd.Register(cmd.New("test_command_block_set", "", nil, commandBlockTestCommand{}))
	cmd.Register(cmd.New("test_command_block_fail", "", nil, commandBlockTestFailure{}))
}

func TestCommandBlockNBTAndComparatorRoundTrip(t *testing.T) {
	t.Parallel()
	b := CommandBlock{Mode: CommandBlockRepeat, Facing: cube.FaceNorth, Conditional: true, NeedsRedstone: false, Powered: true, ConditionMet: true,
		Command: "say hi", CustomName: "test", LastOutput: "ok", TrackOutput: true, SuccessCount: 9, TickDelay: 4, ExecuteOnFirstTick: true}
	decoded := b.DecodeNBT(b.EncodeNBT()).(CommandBlock)
	if decoded.Mode != b.Mode || decoded.Facing != b.Facing || decoded.Conditional != b.Conditional || decoded.NeedsRedstone != b.NeedsRedstone || decoded.Powered != b.Powered || decoded.ConditionMet != b.ConditionMet || decoded.Command != b.Command || decoded.CustomName != b.CustomName || decoded.LastOutput != b.LastOutput || decoded.TrackOutput != b.TrackOutput || decoded.SuccessCount != b.SuccessCount || decoded.TickDelay != b.TickDelay || decoded.ExecuteOnFirstTick != b.ExecuteOnFirstTick {
		t.Fatalf("NBT round trip: got %+v want %+v", decoded, b)
	}
	if got := decoded.ComparatorOutput(nil, cube.Pos{}); got != 9 {
		t.Fatalf("comparator output=%d, want 9", got)
	}
}

func TestImpulseCommandBlockRunsOncePerRedstoneEdge(t *testing.T) {
	registerCommandBlockTestCommands()
	commandBlockExecutionCount.Store(0)
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	pos := cube.Pos{0, 64, 0}
	<-w.Exec(func(tx *world.Tx) {
		b := CommandBlock{Command: "test_command_block_set", NeedsRedstone: true}
		tx.SetBlock(pos, b, nil)
		powered, changed := b.RedstonePowerUpdate(pos, tx, 15)
		if !changed {
			t.Fatal("rising edge was not recorded")
		}
		tx.SetBlock(pos, powered, nil)
		b.RedstonePowerPostUpdate(pos, tx, b, powered, 0, 15)
		powered.(CommandBlock).ScheduledTick(pos, tx, rand.New(rand.NewPCG(1, 2)))
		if _, changed = powered.(CommandBlock).RedstonePowerUpdate(pos, tx, 15); changed {
			t.Fatal("continuous power created another rising edge")
		}
		if commandBlockExecutionCount.Load() != 1 {
			t.Fatalf("command executions=%d, want one", commandBlockExecutionCount.Load())
		}
		if _, ok := tx.Block(cube.Pos{1, 64, 1}).(Stone); !ok {
			t.Fatal("command did not set its target block")
		}
	})
}

func TestRepeatingAndChainCommandBlocks(t *testing.T) {
	registerCommandBlockTestCommands()
	commandBlockExecutionCount.Store(0)
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	<-w.Exec(func(tx *world.Tx) {
		repeatPos := cube.Pos{0, 64, 0}
		repeating := CommandBlock{Mode: CommandBlockRepeat, Command: "test_command_block_set", NeedsRedstone: true, Powered: true}
		tx.SetBlock(repeatPos, repeating, nil)
		repeating.Tick(1, repeatPos, tx)
		repeating = tx.Block(repeatPos).(CommandBlock)
		repeating.Tick(2, repeatPos, tx)
		if commandBlockExecutionCount.Load() != 2 {
			t.Fatalf("powered repeating executions=%d, want 2", commandBlockExecutionCount.Load())
		}

		commandBlockExecutionCount.Store(0)
		impulsePos := cube.Pos{4, 64, 0}
		impulse := CommandBlock{Command: "test_command_block_set", NeedsRedstone: true, Facing: cube.FaceEast}
		chain := CommandBlock{Mode: CommandBlockChain, Command: "test_command_block_set", NeedsRedstone: false, Facing: cube.FaceEast}
		tx.SetBlock(impulsePos, impulse, nil)
		tx.SetBlock(impulsePos.Side(cube.FaceEast), chain, nil)
		impulse.ScheduledTick(impulsePos, tx, rand.New(rand.NewPCG(3, 4)))
		if commandBlockExecutionCount.Load() != 2 {
			t.Fatalf("impulse+chain executions=%d, want 2", commandBlockExecutionCount.Load())
		}
	})
}

func TestConditionalChainDoesNotRunAfterFailedCommand(t *testing.T) {
	registerCommandBlockTestCommands()
	commandBlockExecutionCount.Store(0)
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	<-w.Exec(func(tx *world.Tx) {
		pos := cube.Pos{0, 64, 0}
		impulse := CommandBlock{Command: "test_command_block_fail", NeedsRedstone: true, Facing: cube.FaceEast}
		chain := CommandBlock{Mode: CommandBlockChain, Command: "test_command_block_set", Conditional: true, NeedsRedstone: false, Facing: cube.FaceEast}
		tx.SetBlock(pos, impulse, nil)
		tx.SetBlock(pos.Side(cube.FaceEast), chain, nil)
		impulse.ScheduledTick(pos, tx, rand.New(rand.NewPCG(5, 6)))
		if commandBlockExecutionCount.Load() != 0 {
			t.Fatalf("conditional command ran after failed predecessor")
		}
	})
}

var (
	deferredCmdRuns   atomic.Int32
	deferredCmdGate   chan struct{}
	deferredCmdResult atomic.Int32
)

type commandBlockDeferredCommand struct{}

func (commandBlockDeferredCommand) Run(src cmd.Source, _ *cmd.Output, _ *world.Tx) {
	deferredCmdRuns.Add(1)
	send := cmd.Later(src)
	gate := deferredCmdGate
	res := int(deferredCmdResult.Load())
	go func() {
		<-gate
		out := &cmd.Output{}
		out.SetResult(res)
		out.Print("late ok")
		send(nil, out)
	}()
}

func TestCommandBlockLateResult(t *testing.T) {
	cmd.Register(cmd.New("test_command_block_deferred", "", nil, commandBlockDeferredCommand{}))
	w := world.Config{RandomTickSpeed: -1}.New()
	defer w.Close()

	// Impulse command block records deferred SuccessCount and LastOutput.
	deferredCmdRuns.Store(0)
	deferredCmdResult.Store(3)
	deferredCmdGate = make(chan struct{})
	impulsePos := cube.Pos{2, 64, 0}
	<-w.Exec(func(tx *world.Tx) {
		b := CommandBlock{Command: "test_command_block_deferred", NeedsRedstone: true, TrackOutput: true}
		tx.SetBlock(impulsePos, b, nil)
		b.ScheduledTick(impulsePos, tx, rand.New(rand.NewPCG(7, 8)))
	})
	close(deferredCmdGate)

	deadline := make(chan struct{})
	go func() {
		defer close(deadline)
		for range 100 {
			var got CommandBlock
			<-w.Exec(func(tx *world.Tx) { got, _ = tx.Block(impulsePos).(CommandBlock) })
			if got.SuccessCount == 3 && got.LastOutput == "late ok" {
				return
			}
		}
	}()
	<-deadline
	var got CommandBlock
	<-w.Exec(func(tx *world.Tx) { got = tx.Block(impulsePos).(CommandBlock) })
	if got.SuccessCount != 3 || got.LastOutput != "late ok" {
		t.Fatalf("impulse deferred state = (%d, %q), want (3, %q)", got.SuccessCount, got.LastOutput, "late ok")
	}

	// Repeating command block skips overlapping ticks while one cross-world run is in flight.
	deferredCmdRuns.Store(0)
	deferredCmdResult.Store(2)
	deferredCmdGate = make(chan struct{})
	repeatPos := cube.Pos{6, 64, 0}
	<-w.Exec(func(tx *world.Tx) {
		b := CommandBlock{Mode: CommandBlockRepeat, Command: "test_command_block_deferred", NeedsRedstone: true, Powered: true, TrackOutput: true}
		tx.SetBlock(repeatPos, b, nil)
		b.Tick(1, repeatPos, tx)
		b = tx.Block(repeatPos).(CommandBlock)
		b.Tick(2, repeatPos, tx)
		b.Tick(3, repeatPos, tx)
	})
	if runs := deferredCmdRuns.Load(); runs != 1 {
		t.Fatalf("in-flight repeating runs = %d, want 1", runs)
	}
	close(deferredCmdGate)
	for range 100 {
		var cur CommandBlock
		<-w.Exec(func(tx *world.Tx) { cur, _ = tx.Block(repeatPos).(CommandBlock) })
		if cur.SuccessCount == 2 {
			break
		}
	}
	// Once finished, a new tick starts another run.
	deferredCmdGate = make(chan struct{})
	<-w.Exec(func(tx *world.Tx) {
		b := tx.Block(repeatPos).(CommandBlock)
		b.Tick(4, repeatPos, tx)
	})
	close(deferredCmdGate)
	if runs := deferredCmdRuns.Load(); runs != 2 {
		t.Fatalf("post-completion repeating runs = %d, want 2", runs)
	}
}
