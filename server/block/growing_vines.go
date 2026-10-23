package block

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
)

// WeepingVines is a Nether vine that grows downwards.
type WeepingVines struct {
	empty
	replaceable
	transparent
	Age int
}

// ScheduledTick destroys weeping vines that no longer survive, matching GrowingPlantBlock.tick.
func (v WeepingVines) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	if survives, hasSourceState := tx.JavaBlockCanSurvive(pos); hasSourceState && !survives {
		breakBlock(v, pos, tx)
	}
}

// BreakInfo returns the source vine's shears, silk-touch, and Fortune drops.
func (v WeepingVines) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, sourceVineDrops(v))
}

// EncodeItem encodes the weeping vine item.
func (WeepingVines) EncodeItem() (string, int16) { return "minecraft:weeping_vines", 0 }

// EncodeBlock encodes the source-facing weeping vine block state.
func (v WeepingVines) EncodeBlock() (string, map[string]any) {
	return "minecraft:weeping_vines", map[string]any{"weeping_vines_age": int32(v.Age)}
}

// TwistingVines is a Nether vine that grows upwards.
type TwistingVines struct {
	empty
	replaceable
	transparent
	Age int
}

// ScheduledTick destroys twisting vines that no longer survive, matching GrowingPlantBlock.tick.
func (v TwistingVines) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	if survives, hasSourceState := tx.JavaBlockCanSurvive(pos); hasSourceState && !survives {
		breakBlock(v, pos, tx)
	}
}

// BreakInfo returns the source vine's shears, silk-touch, and Fortune drops.
func (v TwistingVines) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, sourceVineDrops(v))
}

// EncodeItem encodes the twisting vine item.
func (TwistingVines) EncodeItem() (string, int16) { return "minecraft:twisting_vines", 0 }

// EncodeBlock encodes the source-facing twisting vine block state.
func (v TwistingVines) EncodeBlock() (string, map[string]any) {
	return "minecraft:twisting_vines", map[string]any{"twisting_vines_age": int32(v.Age)}
}

const (
	caveVinesBase uint8 = iota
	caveVinesHeadWithBerries
	caveVinesBodyWithBerries
)

// HasBerries reports if the cave vines carry a glow berry.
func (v CaveVines) HasBerries() bool { return v.Variant != caveVinesBase }

// PickBerries ports CaveVines.use: vines carrying a glow berry drop it and
// grow on without one. It reports if they carried one.
func (v CaveVines) PickBerries(pos cube.Pos, tx *world.Tx) bool {
	if !v.HasBerries() {
		return false
	}
	dropItem(tx, item.NewStack(item.GlowBerries{}, 1), pos.Vec3Centre())
	tx.PlaySound(pos.Vec3Centre(), sound.Custom{Name: "block.cave_vines.pick_berries", Volume: 1, Pitch: .8 + rand.Float64()*.4})
	v.Variant = caveVinesBase
	tx.SetBlock(pos, v, nil)
	return true
}

// Activate picks the glow berry of the vines, as CaveVinesBlock and
// CaveVinesPlantBlock.useWithoutItem do.
func (v CaveVines) Activate(pos cube.Pos, _ cube.Face, tx *world.Tx, _ item.User, _ *item.UseContext) bool {
	return v.PickBerries(pos, tx)
}

// CaveVines are the head and body blocks of cave vines.
type CaveVines struct {
	empty
	replaceable
	transparent
	Age     int
	Variant uint8
}

// ScheduledTick destroys cave vines that no longer survive, matching GrowingPlantBlock.tick.
func (v CaveVines) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	if survives, hasSourceState := tx.JavaBlockCanSurvive(pos); hasSourceState && !survives {
		breakBlock(v, pos, tx)
	}
}

// BreakInfo returns berries only for cave-vine states that carry berries.
func (v CaveVines) BreakInfo() BreakInfo {
	if v.Variant != caveVinesBase {
		return newBreakInfo(0, alwaysHarvestable, nothingEffective, simpleDrops(item.NewStack(item.GlowBerries{}, 1)))
	}
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, simpleDrops())
}

// EncodeBlock encodes the appropriate Bedrock cave-vine palette state.
func (v CaveVines) EncodeBlock() (string, map[string]any) {
	name := "minecraft:cave_vines"
	switch v.Variant {
	case caveVinesHeadWithBerries:
		name = "minecraft:cave_vines_head_with_berries"
	case caveVinesBodyWithBerries:
		name = "minecraft:cave_vines_body_with_berries"
	}
	return name, map[string]any{"growing_plant_age": int32(v.Age)}
}

func sourceVineDrops(v world.Item) func(item.Tool, []item.Enchantment) []item.Stack {
	return func(tool item.Tool, enchantments []item.Enchantment) []item.Stack {
		if tool.ToolType() == item.TypeShears || hasSilkTouch(enchantments) {
			return []item.Stack{item.NewStack(v, 1)}
		}
		chances := [...]float32{0.33, 0.55, 0.77, 1}
		if rand.Float32() < chances[min(fortuneLevel(enchantments), len(chances)-1)] {
			return []item.Stack{item.NewStack(v, 1)}
		}
		return nil
	}
}

func allGrowingVines() (blocks []world.Block) {
	for age := range 26 {
		blocks = append(blocks, WeepingVines{Age: age}, TwistingVines{Age: age})
		blocks = append(blocks, CaveVines{Age: age}, CaveVines{Age: age, Variant: caveVinesHeadWithBerries}, CaveVines{Age: age, Variant: caveVinesBodyWithBerries})
	}
	return blocks
}
