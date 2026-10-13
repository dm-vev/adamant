package block

import (
	"math/rand/v2"
	"sync/atomic"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/world"
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
