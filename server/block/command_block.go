package block

import (
	"math/rand/v2"
	"strings"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// CommandBlockMode selects when a command block executes.
type CommandBlockMode uint8

const (
	CommandBlockImpulse CommandBlockMode = iota
	CommandBlockRepeat
	CommandBlockChain
)

const maxCommandBlockChainLength = 65536

var commandBlockHashes = [3]uint64{NextHash(), NextHash(), NextHash()}

// CommandBlock executes a command when activated by redstone, a chain predecessor, or automatic ticking.
type CommandBlock struct {
	solid
	transparent
	Facing                                            cube.Face
	Mode                                              CommandBlockMode
	Conditional, NeedsRedstone, Powered, ConditionMet bool
	Command, CustomName, LastOutput                   string
	TrackOutput                                       bool
	SuccessCount, TickDelay                           int
	ExecuteOnFirstTick                                bool
	lastExecutionTick                                 int64
}

// EncodeItem encodes command blocks as items. They are unobtainable in survival but available in creative.
func (b CommandBlock) EncodeItem() (string, int16) { return b.name(), int16(b.Mode) }
func (b CommandBlock) name() string {
	switch b.Mode {
	case CommandBlockRepeat:
		return "minecraft:repeating_command_block"
	case CommandBlockChain:
		return "minecraft:chain_command_block"
	default:
		return "minecraft:command_block"
	}
}
func (b CommandBlock) EncodeBlock() (string, map[string]any) {
	return b.name(), map[string]any{"facing_direction": int32(b.Facing), "conditional_bit": boolByte(b.Conditional)}
}
func (b CommandBlock) Hash() (uint64, uint64) {
	mode := b.Mode
	if mode > CommandBlockChain {
		mode = CommandBlockImpulse
	}
	v := uint64(b.Facing) | uint64(boolByte(b.Conditional))<<8
	return commandBlockHashes[mode], v
}
func (b CommandBlock) BreakInfo() BreakInfo {
	return newBreakInfo(3.5, pickaxeHarvestable, pickaxeEffective, oneOf(CommandBlock{Mode: b.Mode}))
}
func (b CommandBlock) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, ok := firstReplaceable(tx, pos, face, b)
	if !ok {
		return false
	}
	b.NeedsRedstone, b.TrackOutput = true, true
	place(tx, pos, b, user, ctx)
	return placed(ctx)
}
func (b CommandBlock) Activate(_ cube.Pos, _ cube.Face, _ *world.Tx, u item.User, _ *item.UseContext) bool {
	return canUseGameMasterBlocks(u)
}

// EncodeNBT serializes Java/BDS command actor state using BDS actor keys.
// LastOutput holds the whole message, so its parameters are always empty.
func (b CommandBlock) EncodeNBT() map[string]any {
	return map[string]any{
		"id": "CommandBlock", "Command": b.Command, "CustomName": b.CustomName, "LastOutput": b.LastOutput,
		"LastOutputParams": []string{}, "LastExecution": b.lastExecutionTick,
		"TrackOutput": boolByte(b.TrackOutput), "SuccessCount": int32(b.SuccessCount), "auto": boolByte(!b.NeedsRedstone),
		"powered": boolByte(b.Powered), "conditionMet": boolByte(b.ConditionMet), "TickDelay": int32(b.TickDelay),
		"ExecuteOnFirstTick": boolByte(b.ExecuteOnFirstTick), "LPCommandMode": int32(b.Mode),
		"LPCondionalMode": boolByte(b.Conditional), "LPRedstoneMode": boolByte(!b.NeedsRedstone), "Version": int32(52),
	}
}

// DecodeNBT restores command actor state. Like a fresh Java or BDS command
// block, an actor without TrackOutput tracks its output.
func (b CommandBlock) DecodeNBT(m map[string]any) any {
	b.Command = nbtconv.String(m, "Command")
	b.CustomName = nbtconv.String(m, "CustomName")
	b.LastOutput = nbtconv.String(m, "LastOutput")
	b.TrackOutput = true
	if _, ok := m["TrackOutput"]; ok {
		b.TrackOutput = nbtconv.Bool(m, "TrackOutput")
	}
	b.lastExecutionTick = nbtconv.Int64(m, "LastExecution")
	b.SuccessCount = int(nbtconv.Int32(m, "SuccessCount"))
	b.Powered = nbtconv.Bool(m, "powered")
	b.ConditionMet = nbtconv.Bool(m, "conditionMet")
	b.TickDelay = max(0, int(nbtconv.Int32(m, "TickDelay")))
	// The mode and conditionality are the block and its conditional_bit
	// state: the LP* fields record the last performed configuration, which
	// is impulse and unconditional for a new chain or conditional block.
	// As BDS places them, a new chain block is always active and a new
	// repeating block runs on its first tick.
	b.ExecuteOnFirstTick = b.Mode == CommandBlockRepeat
	if _, ok := m["ExecuteOnFirstTick"]; ok {
		b.ExecuteOnFirstTick = nbtconv.Bool(m, "ExecuteOnFirstTick")
	}
	// auto is the live always-active flag.
	switch _, auto := m["auto"]; {
	case auto:
		b.NeedsRedstone = !nbtconv.Bool(m, "auto")
	case m["LPRedstoneMode"] != nil:
		b.NeedsRedstone = !nbtconv.Bool(m, "LPRedstoneMode")
	default:
		b.NeedsRedstone = b.Mode != CommandBlockChain
	}
	return b
}

// ComparatorOutput exposes the previous command result.
func (b CommandBlock) ComparatorOutput(*world.Tx, cube.Pos) uint8 {
	return uint8(min(max(b.SuccessCount, 0), 15))
}

func (b CommandBlock) RedstonePowerUpdate(_ cube.Pos, _ *world.Tx, power int) (world.Block, bool) {
	powered := power > 0
	if powered == b.Powered {
		return b, false
	}
	b.Powered = powered
	return b, true
}
func (b CommandBlock) RedstonePowerPostUpdate(pos cube.Pos, tx *world.Tx, _, after world.Block, oldPower, newPower int) {
	updated, ok := after.(CommandBlock)
	if !ok || oldPower != 0 || newPower == 0 {
		return
	}
	// Java schedules the rising-edge execution one tick later; Bedrock's
	// TickDelay postpones it further.
	if updated.Mode == CommandBlockImpulse && updated.NeedsRedstone {
		tx.ScheduleBlockUpdate(pos, updated, time.Duration(max(updated.TickDelay, 1))*time.Second/20)
	}
}
func (b CommandBlock) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	b.execute(pos, tx)
}
func (b CommandBlock) Tick(tick int64, pos cube.Pos, tx *world.Tx) {
	// Only repeating blocks run every tick; always-active impulse blocks run
	// once when configured, and chain blocks run only through their chain.
	if b.Mode != CommandBlockRepeat || b.NeedsRedstone && !b.Powered || b.TickDelay > 0 && tick-b.lastExecutionTick < int64(b.TickDelay) {
		return
	}
	b.execute(pos, tx)
}

// execute ports CommandBlock.execute: the block runs its own command, then
// executeChain runs every powered or always-active chain block in front of
// it, following each block's facing. Failed commands do not stop the chain.
func (b CommandBlock) execute(pos cube.Pos, tx *world.Tx) {
	w := tx.World()
	if !w.GameRuleBool("command_blocks_work") {
		return
	}
	facing := b.run(pos, tx).Facing
	for range w.GameRuleInt("max_command_sequence_length") {
		pos = pos.Side(facing)
		next, ok := tx.Block(pos).(CommandBlock)
		if !ok || next.Mode != CommandBlockChain {
			return
		}
		if next.Powered || !next.NeedsRedstone {
			next.run(pos, tx)
		}
		facing = next.Facing
	}
}

// run ports markConditionMet followed by performCommand: a conditional block
// runs only when the command block behind it last succeeded, and otherwise
// resets its success count.
func (b CommandBlock) run(pos cube.Pos, tx *world.Tx) CommandBlock {
	b.ConditionMet = true
	if b.Conditional {
		behind, ok := tx.Block(pos.Side(b.Facing.Opposite())).(CommandBlock)
		b.ConditionMet = ok && behind.SuccessCount > 0
	}
	switch {
	case b.ConditionMet:
		b = b.perform(pos, tx)
	case b.Conditional:
		b.SuccessCount = 0
	}
	tx.SetBlock(pos, b, nil)
	return b
}

// perform runs the stored command and records its result and output.
func (b CommandBlock) perform(pos cube.Pos, tx *world.Tx) CommandBlock {
	b.lastExecutionTick = tx.World().CurrentTick()
	b.SuccessCount = 0
	line := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(b.Command), "/"))
	words := strings.Fields(line)
	if len(words) == 0 {
		return b
	}
	command, ok := cmd.ByAlias(words[0])
	if !ok {
		return b
	}
	source := cmd.NewBlockSource(pos, tx.World(), b.CustomName)
	command.Execute(strings.TrimSpace(strings.TrimPrefix(line, words[0])), source, tx)
	if output := source.Output(); output.Success() {
		b.SuccessCount = output.Result()
	}
	if b.TrackOutput && tx.World().GameRuleBool("command_block_output") {
		b.LastOutput = source.Text()
	}
	return b
}
