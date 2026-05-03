package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// Conduit is an underwater utility block.
type Conduit struct {
	transparent
	solid
	sourceWaterDisplacer
}

// BreakInfo ...
func (c Conduit) BreakInfo() BreakInfo {
	return newBreakInfo(3, pickaxeHarvestable, pickaxeEffective, oneOf(c)).withBlastResistance(15)
}

// EncodeItem ...
func (Conduit) EncodeItem() (name string, meta int16) {
	return "minecraft:conduit", 0
}

// EncodeBlock ...
func (Conduit) EncodeBlock() (string, map[string]any) {
	return "minecraft:conduit", nil
}

// RedstoneOre is an ore block that drops redstone dust when mined.
type RedstoneOre struct {
	solid
	bassDrum

	// Deepslate specifies if the ore is the deepslate variant.
	Deepslate bool
	// Lit specifies if the ore is emitting light.
	Lit bool
}

// LightEmissionLevel ...
func (r RedstoneOre) LightEmissionLevel() uint8 {
	if r.Lit {
		return 9
	}
	return 0
}

// BreakInfo ...
func (r RedstoneOre) BreakInfo() BreakInfo {
	return newBreakInfo(3, pickaxeHarvestable, pickaxeEffective, oneOf(r)).withXPDropRange(1, 5).withBlastResistance(15)
}

// SmeltInfo ...
func (r RedstoneOre) SmeltInfo() item.SmeltInfo {
	return newOreSmeltInfo(item.NewStack(RedstoneOre{Deepslate: r.Deepslate}, 1), 0.7)
}

// EncodeItem ...
func (r RedstoneOre) EncodeItem() (name string, meta int16) {
	if r.Deepslate {
		return "minecraft:deepslate_redstone_ore", 0
	}
	return "minecraft:redstone_ore", 0
}

// EncodeBlock ...
func (r RedstoneOre) EncodeBlock() (string, map[string]any) {
	switch {
	case r.Deepslate && r.Lit:
		return "minecraft:lit_deepslate_redstone_ore", nil
	case r.Deepslate:
		return "minecraft:deepslate_redstone_ore", nil
	case r.Lit:
		return "minecraft:lit_redstone_ore", nil
	default:
		return "minecraft:redstone_ore", nil
	}
}

// allRedstoneOre returns all redstone ore states.
func allRedstoneOre() []world.Block {
	return []world.Block{
		RedstoneOre{},
		RedstoneOre{Lit: true},
		RedstoneOre{Deepslate: true},
		RedstoneOre{Deepslate: true, Lit: true},
	}
}

// HoneyBlock is a sticky block made from honey bottles.
type HoneyBlock struct {
	transparent
	solid
}

// BreakInfo ...
func (h HoneyBlock) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, oneOf(h))
}

// EncodeItem ...
func (HoneyBlock) EncodeItem() (name string, meta int16) {
	return "minecraft:honey_block", 0
}

// EncodeBlock ...
func (HoneyBlock) EncodeBlock() (string, map[string]any) {
	return "minecraft:honey_block", nil
}

// FrogSpawn is a decorative block placed on top of water by frogs.
type FrogSpawn struct {
	transparent
	empty
}

// BreakInfo ...
func (FrogSpawn) BreakInfo() BreakInfo {
	return newBreakInfo(0, neverHarvestable, nothingEffective, simpleDrops())
}

// EncodeBlock ...
func (FrogSpawn) EncodeBlock() (string, map[string]any) {
	return "minecraft:frog_spawn", nil
}

// FrostedIce is temporary ice created by the Frost Walker enchantment.
type FrostedIce struct {
	transparent
	solid

	// Age is the crack stage of the frosted ice.
	Age int
}

// BreakInfo ...
func (FrostedIce) BreakInfo() BreakInfo {
	return newBreakInfo(0.5, neverHarvestable, pickaxeEffective, simpleDrops())
}

// EncodeBlock ...
func (f FrostedIce) EncodeBlock() (string, map[string]any) {
	return "minecraft:frosted_ice", map[string]any{"age": int32(f.Age)}
}

// allFrostedIce returns all frosted ice states.
func allFrostedIce() (blocks []world.Block) {
	for age := 0; age < 4; age++ {
		blocks = append(blocks, FrostedIce{Age: age})
	}
	return
}

// NetherReactor is a legacy block kept for old worlds.
type NetherReactor struct {
	solid
	bassDrum
}

// BreakInfo ...
func (n NetherReactor) BreakInfo() BreakInfo {
	return newBreakInfo(3, pickaxeHarvestable, pickaxeEffective, oneOf(n)).withBlastResistance(30)
}

// EncodeItem ...
func (NetherReactor) EncodeItem() (name string, meta int16) {
	return "minecraft:netherreactor", 0
}

// EncodeBlock ...
func (NetherReactor) EncodeBlock() (string, map[string]any) {
	return "minecraft:netherreactor", nil
}

// UnderwaterTNT is an Education Edition TNT variant.
type UnderwaterTNT struct {
	solid

	// Explode specifies if the TNT is primed to explode.
	Explode bool
}

// BreakInfo ...
func (t UnderwaterTNT) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, oneOf(t))
}

// EncodeItem ...
func (UnderwaterTNT) EncodeItem() (name string, meta int16) {
	return "minecraft:underwater_tnt", 0
}

// EncodeBlock ...
func (t UnderwaterTNT) EncodeBlock() (string, map[string]any) {
	return "minecraft:underwater_tnt", map[string]any{"explode_bit": boolByte(t.Explode)}
}

// allUnderwaterTNT returns all underwater TNT states.
func allUnderwaterTNT() []world.Block {
	return []world.Block{UnderwaterTNT{}, UnderwaterTNT{Explode: true}}
}

// PowderSnow is a soft snow block.
type PowderSnow struct {
	transparent
	empty
}

// BreakInfo ...
func (PowderSnow) BreakInfo() BreakInfo {
	return newBreakInfo(0.25, neverHarvestable, shovelEffective, simpleDrops())
}

// EncodeBlock ...
func (PowderSnow) EncodeBlock() (string, map[string]any) {
	return "minecraft:powder_snow", nil
}

// Sculk is a decorative block found in the deep dark.
type Sculk struct {
	solid
	bassDrum
}

// BreakInfo ...
func (s Sculk) BreakInfo() BreakInfo {
	return newBreakInfo(0.2, alwaysHarvestable, hoeEffective, oneOf(s)).withXPDropRange(1, 1).withBlastResistance(0.2)
}

// EncodeItem ...
func (Sculk) EncodeItem() (name string, meta int16) {
	return "minecraft:sculk", 0
}

// EncodeBlock ...
func (Sculk) EncodeBlock() (string, map[string]any) {
	return "minecraft:sculk", nil
}

// Target is a redstone component block used for ranged practice.
type Target struct {
	solid
	bassDrum
}

// BreakInfo ...
func (t Target) BreakInfo() BreakInfo {
	return newBreakInfo(0.5, alwaysHarvestable, hoeEffective, oneOf(t))
}

// EncodeItem ...
func (Target) EncodeItem() (name string, meta int16) {
	return "minecraft:target", 0
}

// EncodeBlock ...
func (Target) EncodeBlock() (string, map[string]any) {
	return "minecraft:target", nil
}

// Torchflower is a decorative flower.
type Torchflower struct {
	transparent
	empty
}

// BreakInfo ...
func (t Torchflower) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, oneOf(t))
}

// CompostChance ...
func (Torchflower) CompostChance() float64 {
	return 0.65
}

// EncodeItem ...
func (Torchflower) EncodeItem() (name string, meta int16) {
	return "minecraft:torchflower", 0
}

// EncodeBlock ...
func (Torchflower) EncodeBlock() (string, map[string]any) {
	return "minecraft:torchflower", nil
}

// Fungus is a Nether fungus plant.
type Fungus struct {
	transparent
	empty

	// Warped specifies if the fungus is warped instead of crimson.
	Warped bool
}

// BreakInfo ...
func (f Fungus) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, oneOf(f))
}

// CompostChance ...
func (Fungus) CompostChance() float64 {
	return 0.65
}

// EncodeItem ...
func (f Fungus) EncodeItem() (name string, meta int16) {
	if f.Warped {
		return "minecraft:warped_fungus", 0
	}
	return "minecraft:crimson_fungus", 0
}

// EncodeBlock ...
func (f Fungus) EncodeBlock() (string, map[string]any) {
	if f.Warped {
		return "minecraft:warped_fungus", nil
	}
	return "minecraft:crimson_fungus", nil
}

// allFungus returns all fungus variants.
func allFungus() []world.Block {
	return []world.Block{Fungus{}, Fungus{Warped: true}}
}

// LegacyStonecutter is the old stonecutter block kept for compatibility with old worlds.
type LegacyStonecutter struct {
	solid
	bassDrum
}

// BreakInfo ...
func (s LegacyStonecutter) BreakInfo() BreakInfo {
	return newBreakInfo(3.5, pickaxeHarvestable, pickaxeEffective, oneOf(s))
}

// EncodeItem ...
func (LegacyStonecutter) EncodeItem() (name string, meta int16) {
	return "minecraft:stonecutter", 0
}

// EncodeBlock ...
func (LegacyStonecutter) EncodeBlock() (string, map[string]any) {
	return "minecraft:stonecutter", nil
}
